package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/logger"
)

const defaultMihomoController = "http://127.0.0.1:9090"

// ProxySubscriptionService imports Clash/Mihomo subscriptions and exposes every
// tunnel node as a local HTTP proxy port via Mihomo listeners.
type ProxySubscriptionService struct {
	repo   *repo.ProxyRepo
	aes    *crypto.AESGCM
	mihomo *MihomoManager
}

func NewProxySubscriptionService(r *repo.ProxyRepo, aes *crypto.AESGCM, m *MihomoManager) *ProxySubscriptionService {
	return &ProxySubscriptionService{repo: r, aes: aes, mihomo: m}
}

func (s *ProxySubscriptionService) List(ctx context.Context) ([]*dto.ProxySubscriptionResp, error) {
	rows, err := s.repo.ListSubscriptions(ctx)
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.ProxySubscriptionResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxySubscriptionResp(row))
	}
	return out, nil
}

func (s *ProxySubscriptionService) Create(ctx context.Context, adminID uint64, req *dto.ProxySubscriptionCreateReq) (*dto.ProxySubscriptionResp, *dto.ProxySubscriptionSyncResp, error) {
	subURL := strings.TrimSpace(req.URL)
	if !validSubscriptionURL(subURL) {
		return nil, nil, errcode.InvalidParam.WithMsg("订阅 URL 必须是 http(s) 地址")
	}
	portStart := req.PortStart
	if portStart <= 0 {
		portStart = 17001
	}
	interval := req.SyncIntervalMin
	if interval <= 0 {
		interval = 60
	}
	autoSync := true
	if req.AutoSync != nil {
		autoSync = *req.AutoSync
	}
	enc, err := s.aes.Encrypt([]byte(subURL))
	if err != nil {
		return nil, nil, errcode.Internal.Wrap(err)
	}
	sub := &model.ProxySubscription{
		Name:            strings.TrimSpace(req.Name),
		URLEnc:          enc,
		PortStart:       portStart,
		AutoSync:        autoSync,
		SyncIntervalMin: interval,
		Status:          model.ProxyStatusEnabled,
		CreatedBy:       &adminID,
	}
	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return nil, nil, errcode.DBError.Wrap(err)
	}
	syncResp, err := s.Sync(ctx, sub.ID)
	if err != nil {
		return proxySubscriptionResp(sub), nil, err
	}
	refreshed, _ := s.repo.GetSubscriptionByID(ctx, sub.ID)
	if refreshed != nil {
		sub = refreshed
	}
	return proxySubscriptionResp(sub), syncResp, nil
}

func (s *ProxySubscriptionService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.repo.GetSubscriptionByID(ctx, id); err != nil {
		return errcode.ResourceMissing
	}
	if err := s.repo.SoftDeleteSubscription(ctx, id); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *ProxySubscriptionService) Preview(ctx context.Context, rawURL string) (*dto.ProxySubscriptionPreviewResp, error) {
	nodes, err := FetchProxySubscription(ctx, strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	tunnel, direct := splitProxyNodes(nodes)
	return &dto.ProxySubscriptionPreviewResp{
		NodeCount: len(nodes),
		Tunnel:    len(tunnel),
		Direct:    len(direct),
		Nodes:     nodePreview(nodes, 30),
	}, nil
}

func (s *ProxySubscriptionService) Sync(ctx context.Context, id uint64) (*dto.ProxySubscriptionSyncResp, error) {
	sub, err := s.repo.GetSubscriptionByID(ctx, id)
	if err != nil {
		return nil, errcode.ResourceMissing
	}
	return s.SyncSubscription(ctx, sub)
}

func (s *ProxySubscriptionService) SyncSubscription(ctx context.Context, sub *model.ProxySubscription) (*dto.ProxySubscriptionSyncResp, error) {
	if sub == nil {
		return nil, errcode.InvalidParam
	}
	subURL, err := s.decryptURL(sub)
	if err != nil {
		return nil, s.recordSyncError(ctx, sub.ID, err)
	}
	nodes, err := FetchProxySubscription(ctx, subURL)
	if err != nil {
		return nil, s.recordSyncError(ctx, sub.ID, err)
	}
	tunnelNodes, directNodes := splitProxyNodes(nodes)
	if len(tunnelNodes) > 0 {
		config, portMap, err := GenerateMihomoConfig(nodes, sub.PortStart)
		if err != nil {
			return nil, s.recordSyncError(ctx, sub.ID, err)
		}
		if err := s.mihomo.WriteConfig(config); err != nil {
			return nil, s.recordSyncError(ctx, sub.ID, err)
		}
		if err := s.mihomo.Reload(ctx); err != nil {
			return nil, s.recordSyncError(ctx, sub.ID, err)
		}
		_ = portMap
	}

	if err := s.repo.SoftDeleteBySubscriptionID(ctx, sub.ID); err != nil {
		return nil, s.recordSyncError(ctx, sub.ID, err)
	}
	items := make([]*model.Proxy, 0, len(nodes))
	portMap := mihomoPortMap(nodes, sub.PortStart)
	for i, node := range nodes {
		p := proxyFromNode(sub.ID, node, portMap[i])
		if p == nil {
			continue
		}
		items = append(items, p)
	}
	if err := s.repo.CreateMany(ctx, items); err != nil {
		return nil, s.recordSyncError(ctx, sub.ID, err)
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateSubscription(ctx, sub.ID, map[string]any{
		"node_count":   len(nodes),
		"last_sync_at": now,
		"last_error":   nil,
	}); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return &dto.ProxySubscriptionSyncResp{
		NodeCount: len(nodes),
		Tunnel:    len(tunnelNodes),
		Direct:    len(directNodes),
		Created:   len(items),
	}, nil
}

func (s *ProxySubscriptionService) StartAutoSync(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.syncDue(ctx)
				timer.Reset(5 * time.Minute)
			}
		}
	}()
}

func (s *ProxySubscriptionService) syncDue(ctx context.Context) {
	rows, err := s.repo.ListSubscriptions(ctx)
	if err != nil {
		logger.FromCtx(ctx).Warn("proxy_subscription.list_failed", zap.Error(err))
		return
	}
	for _, sub := range rows {
		if sub.Status != model.ProxyStatusEnabled || !sub.AutoSync {
			continue
		}
		interval := time.Duration(sub.SyncIntervalMin) * time.Minute
		if interval <= 0 {
			interval = time.Hour
		}
		if sub.LastSyncAt != nil && time.Since(*sub.LastSyncAt) < interval {
			continue
		}
		if _, err := s.SyncSubscription(ctx, sub); err != nil {
			logger.FromCtx(ctx).Warn("proxy_subscription.sync_failed", zap.Uint64("id", sub.ID), zap.Error(err))
		}
	}
}

func (s *ProxySubscriptionService) decryptURL(sub *model.ProxySubscription) (string, error) {
	if len(sub.URLEnc) == 0 {
		return "", fmt.Errorf("订阅 URL 为空")
	}
	plain, err := s.aes.Decrypt(sub.URLEnc)
	if err != nil {
		return "", fmt.Errorf("解密订阅 URL 失败: %w", err)
	}
	return strings.TrimSpace(string(plain)), nil
}

func (s *ProxySubscriptionService) recordSyncError(ctx context.Context, id uint64, err error) error {
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 500 {
		msg = msg[:500]
	}
	now := time.Now().UTC()
	_ = s.repo.UpdateSubscription(ctx, id, map[string]any{
		"last_sync_at": now,
		"last_error":   msg,
	})
	return errcode.Internal.WithMsg(msg)
}

type clashConfig struct {
	Proxies []map[string]interface{} `yaml:"proxies"`
}

func FetchProxySubscription(ctx context.Context, subURL string) ([]model.ClashNode, error) {
	if !validSubscriptionURL(subURL) {
		return nil, errcode.InvalidParam.WithMsg("订阅 URL 必须是 http(s) 地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return nil, errcode.InvalidParam.Wrap(err)
	}
	req.Header.Set("User-Agent", "clash-verge/v2.2.3")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errcode.Internal.WithMsg("订阅抓取失败: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errcode.Internal.WithMsg(fmt.Sprintf("订阅返回 HTTP %d", resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, errcode.Internal.Wrap(err)
	}
	nodes, err := ParseProxySubscription(raw)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func ParseProxySubscription(raw []byte) ([]model.ClashNode, error) {
	if nodes, err := ParseClashYAML(raw); err == nil && len(nodes) > 0 {
		return nodes, nil
	}
	if nodes, err := parseProxyURILines(raw); err == nil && len(nodes) > 0 {
		return nodes, nil
	}
	decoded, err := decodeMaybeBase64(raw)
	if err == nil {
		if nodes, err := parseProxyURILines(decoded); err == nil && len(nodes) > 0 {
			return nodes, nil
		}
	}
	return nil, errcode.InvalidParam.WithMsg("订阅中未发现可用 Clash proxies 或常见代理 URI")
}

func ParseClashYAML(raw []byte) ([]model.ClashNode, error) {
	var cfg clashConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	nodes := make([]model.ClashNode, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		name := stringFromMap(p, "name")
		typ := strings.ToLower(stringFromMap(p, "type"))
		server := stringFromMap(p, "server")
		port := intFromMap(p, "port")
		if name == "" || typ == "" || server == "" || port <= 0 {
			continue
		}
		nodes = append(nodes, model.ClashNode{Name: name, Type: typ, Server: server, Port: port, Raw: p})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("empty proxies")
	}
	return nodes, nil
}

type mihomoFullConfig struct {
	AllowLan           bool                     `yaml:"allow-lan"`
	BindAddress        string                   `yaml:"bind-address"`
	ExternalController string                   `yaml:"external-controller"`
	LogLevel           string                   `yaml:"log-level"`
	Proxies            []map[string]interface{} `yaml:"proxies"`
	ProxyGroups        []map[string]interface{} `yaml:"proxy-groups"`
	Listeners          []map[string]interface{} `yaml:"listeners"`
	Rules              []string                 `yaml:"rules"`
}

func GenerateMihomoConfig(nodes []model.ClashNode, portStart int) ([]byte, map[int]int, error) {
	cfg := mihomoFullConfig{
		AllowLan:           false,
		BindAddress:        "127.0.0.1",
		ExternalController: "127.0.0.1:9090",
		LogLevel:           "warning",
		Proxies:            []map[string]interface{}{},
		ProxyGroups:        []map[string]interface{}{},
		Listeners:          []map[string]interface{}{},
		Rules:              []string{"MATCH,DIRECT"},
	}
	portMap := map[int]int{}
	for i, node := range nodes {
		if isDirectProxyType(node.Type) {
			continue
		}
		localPort := portStart + i
		groupName := fmt.Sprintf("dapo-sub-%d", i)
		cfg.Proxies = append(cfg.Proxies, node.Raw)
		cfg.ProxyGroups = append(cfg.ProxyGroups, map[string]interface{}{
			"name":    groupName,
			"type":    "select",
			"proxies": []string{node.Name},
		})
		cfg.Listeners = append(cfg.Listeners, map[string]interface{}{
			"name":   fmt.Sprintf("dapo-listener-%d", i),
			"type":   "mixed",
			"listen": "127.0.0.1",
			"port":   localPort,
			"proxy":  groupName,
		})
		portMap[i] = localPort
	}
	if len(cfg.Listeners) == 0 {
		return nil, nil, fmt.Errorf("订阅没有需要 Mihomo 转换的节点")
	}
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return nil, nil, err
	}
	return out, portMap, nil
}

type MihomoManager struct {
	configDir  string
	controller string
}

func NewMihomoManagerFromEnv() *MihomoManager {
	root := strings.TrimSpace(os.Getenv("KLEIN_MIHOMO_HOME"))
	if root == "" {
		root = "/app/private/mihomo"
	}
	controller := strings.TrimSpace(os.Getenv("KLEIN_MIHOMO_CONTROLLER"))
	if controller == "" {
		controller = defaultMihomoController
	}
	return &MihomoManager{configDir: root, controller: strings.TrimRight(controller, "/")}
}

func (m *MihomoManager) WriteConfig(data []byte) error {
	if m == nil {
		return fmt.Errorf("Mihomo 管理器未初始化")
	}
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("创建 Mihomo 配置目录失败: %w", err)
	}
	path := filepath.Join(m.configDir, "config.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入 Mihomo 配置失败: %w", err)
	}
	return nil
}

func (m *MihomoManager) Reload(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("Mihomo 管理器未初始化")
	}
	payload := []byte(fmt.Sprintf(`{"path":%q}`, filepath.Join(m.configDir, "config.yaml")))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, m.controller+"/configs", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("Mihomo 未运行或控制端口不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("Mihomo 重载失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func parseProxyURILines(raw []byte) ([]model.ClashNode, error) {
	lines := strings.Split(string(raw), "\n")
	nodes := make([]model.ClashNode, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if node, ok := parseProxyURI(line); ok {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no uri nodes")
	}
	return nodes, nil
}

func parseProxyURI(line string) (model.ClashNode, bool) {
	u, err := url.Parse(line)
	if err != nil || u.Scheme == "" {
		return model.ClashNode{}, false
	}
	switch strings.ToLower(u.Scheme) {
	case "vless":
		return parseVLESSURI(u)
	case "trojan":
		return parseTrojanURI(u)
	case "vmess":
		return parseVMessURI(u)
	case "ss":
		return parseSSURI(line, u)
	case "hysteria2", "hy2":
		return parseHysteria2URI(u)
	case "http", "https", "socks5":
		return parseDirectURI(u)
	default:
		return model.ClashNode{}, false
	}
}

func parseVLESSURI(u *url.URL) (model.ClashNode, bool) {
	host, port := hostPortFromURL(u)
	uuid := u.User.Username()
	if host == "" || port <= 0 || uuid == "" {
		return model.ClashNode{}, false
	}
	q := u.Query()
	name := nodeName(u, host)
	raw := map[string]interface{}{
		"name":   name,
		"type":   "vless",
		"server": host,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	if v := q.Get("encryption"); v != "" {
		raw["encryption"] = v
	}
	if v := q.Get("flow"); v != "" {
		raw["flow"] = v
	}
	applyTLSOptions(raw, q)
	applyNetworkOptions(raw, q)
	return nodeFromRaw(raw), true
}

func parseTrojanURI(u *url.URL) (model.ClashNode, bool) {
	host, port := hostPortFromURL(u)
	password := u.User.Username()
	if host == "" || port <= 0 || password == "" {
		return model.ClashNode{}, false
	}
	q := u.Query()
	name := nodeName(u, host)
	raw := map[string]interface{}{
		"name":     name,
		"type":     "trojan",
		"server":   host,
		"port":     port,
		"password": password,
		"udp":      true,
	}
	applyTLSOptions(raw, q)
	applyNetworkOptions(raw, q)
	return nodeFromRaw(raw), true
}

func parseHysteria2URI(u *url.URL) (model.ClashNode, bool) {
	host, port := hostPortFromURL(u)
	password := u.User.Username()
	if host == "" || port <= 0 || password == "" {
		return model.ClashNode{}, false
	}
	q := u.Query()
	name := nodeName(u, host)
	raw := map[string]interface{}{
		"name":     name,
		"type":     "hysteria2",
		"server":   host,
		"port":     port,
		"password": password,
	}
	if v := q.Get("sni"); v != "" {
		raw["sni"] = v
	}
	if v := q.Get("insecure"); v == "1" || strings.EqualFold(v, "true") {
		raw["skip-cert-verify"] = true
	}
	return nodeFromRaw(raw), true
}

func parseVMessURI(u *url.URL) (model.ClashNode, bool) {
	rawText := strings.TrimPrefix(u.String(), "vmess://")
	decoded, err := base64.RawStdEncoding.DecodeString(rawText)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(rawText)
	}
	if err != nil {
		return model.ClashNode{}, false
	}
	var src map[string]interface{}
	if err := json.Unmarshal(decoded, &src); err != nil {
		return model.ClashNode{}, false
	}
	host := stringFromAny(src["add"])
	port := intFromAny(src["port"])
	uuid := stringFromAny(src["id"])
	name := stringFromAny(src["ps"])
	if name == "" {
		name = host
	}
	if host == "" || port <= 0 || uuid == "" {
		return model.ClashNode{}, false
	}
	raw := map[string]interface{}{
		"name":    name,
		"type":    "vmess",
		"server":  host,
		"port":    port,
		"uuid":    uuid,
		"alterId": intFromAny(src["aid"]),
		"cipher":  fallbackString(stringFromAny(src["scy"]), "auto"),
		"udp":     true,
	}
	if strings.EqualFold(stringFromAny(src["tls"]), "tls") {
		raw["tls"] = true
		if sni := stringFromAny(src["sni"]); sni != "" {
			raw["servername"] = sni
		}
	}
	if netType := stringFromAny(src["net"]); netType != "" {
		raw["network"] = netType
		if netType == "ws" {
			opts := map[string]interface{}{}
			if path := stringFromAny(src["path"]); path != "" {
				opts["path"] = path
			}
			if hostHeader := stringFromAny(src["host"]); hostHeader != "" {
				opts["headers"] = map[string]string{"Host": hostHeader}
			}
			if len(opts) > 0 {
				raw["ws-opts"] = opts
			}
		}
	}
	return nodeFromRaw(raw), true
}

func parseSSURI(line string, u *url.URL) (model.ClashNode, bool) {
	name := nodeName(u, u.Hostname())
	payload := strings.TrimPrefix(line, "ss://")
	if hash := strings.LastIndex(payload, "#"); hash >= 0 {
		payload = payload[:hash]
	}
	if strings.Contains(payload, "@") {
		if decoded := decodeSSUserInfo(payload); decoded != "" {
			payload = decoded
		}
		u2, err := url.Parse("ss://" + payload)
		if err == nil {
			u = u2
		}
	}
	host, port := hostPortFromURL(u)
	if host == "" || port <= 0 {
		return model.ClashNode{}, false
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	if pass == "" && user != "" && strings.Contains(user, ":") {
		parts := strings.SplitN(user, ":", 2)
		user, pass = parts[0], parts[1]
	}
	if user == "" || pass == "" {
		return model.ClashNode{}, false
	}
	raw := map[string]interface{}{
		"name":     fallbackString(name, host),
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   user,
		"password": pass,
		"udp":      true,
	}
	return nodeFromRaw(raw), true
}

func parseDirectURI(u *url.URL) (model.ClashNode, bool) {
	host, port := hostPortFromURL(u)
	if host == "" || port <= 0 {
		return model.ClashNode{}, false
	}
	name := nodeName(u, host)
	raw := map[string]interface{}{
		"name":   name,
		"type":   strings.ToLower(u.Scheme),
		"server": host,
		"port":   port,
	}
	return nodeFromRaw(raw), true
}

func nodeFromRaw(raw map[string]interface{}) model.ClashNode {
	return model.ClashNode{
		Name:   stringFromMap(raw, "name"),
		Type:   strings.ToLower(stringFromMap(raw, "type")),
		Server: stringFromMap(raw, "server"),
		Port:   intFromMap(raw, "port"),
		Raw:    raw,
	}
}

func applyTLSOptions(raw map[string]interface{}, q url.Values) {
	if sec := strings.ToLower(q.Get("security")); sec == "tls" || sec == "reality" {
		raw["tls"] = true
		if sec == "reality" {
			raw["reality-opts"] = map[string]interface{}{
				"public-key": q.Get("pbk"),
				"short-id":   q.Get("sid"),
			}
		}
	}
	if sni := fallbackString(q.Get("sni"), q.Get("peer")); sni != "" {
		raw["servername"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		raw["client-fingerprint"] = fp
	}
	if insecure := q.Get("allowInsecure"); insecure == "1" || strings.EqualFold(insecure, "true") {
		raw["skip-cert-verify"] = true
	}
}

func applyNetworkOptions(raw map[string]interface{}, q url.Values) {
	netType := strings.ToLower(fallbackString(q.Get("type"), q.Get("network")))
	if netType == "" || netType == "tcp" {
		return
	}
	raw["network"] = netType
	switch netType {
	case "ws":
		opts := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			opts["path"] = path
		}
		if host := q.Get("host"); host != "" {
			opts["headers"] = map[string]string{"Host": host}
		}
		if len(opts) > 0 {
			raw["ws-opts"] = opts
		}
	case "grpc":
		if serviceName := q.Get("serviceName"); serviceName != "" {
			raw["grpc-opts"] = map[string]string{"grpc-service-name": serviceName}
		}
	}
}

func validSubscriptionURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func decodeMaybeBase64(raw []byte) ([]byte, error) {
	text := strings.TrimSpace(string(raw))
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\r", "")
	if text == "" {
		return nil, fmt.Errorf("empty")
	}
	if out, err := base64.StdEncoding.DecodeString(text); err == nil {
		return out, nil
	}
	return base64.RawStdEncoding.DecodeString(text)
}

func decodeSSUserInfo(payload string) string {
	at := strings.LastIndex(payload, "@")
	if at <= 0 {
		return ""
	}
	userInfo := payload[:at]
	if strings.Contains(userInfo, ":") {
		return ""
	}
	if out, err := base64.RawStdEncoding.DecodeString(userInfo); err == nil {
		return string(out) + payload[at:]
	}
	if out, err := base64.StdEncoding.DecodeString(userInfo); err == nil {
		return string(out) + payload[at:]
	}
	return ""
}

func splitProxyNodes(nodes []model.ClashNode) ([]model.ClashNode, []model.ClashNode) {
	tunnel := make([]model.ClashNode, 0)
	direct := make([]model.ClashNode, 0)
	for _, n := range nodes {
		if isDirectProxyType(n.Type) {
			direct = append(direct, n)
		} else {
			tunnel = append(tunnel, n)
		}
	}
	return tunnel, direct
}

func isDirectProxyType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "http", "https", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

func mihomoPortMap(nodes []model.ClashNode, portStart int) map[int]int {
	out := map[int]int{}
	for i, node := range nodes {
		if !isDirectProxyType(node.Type) {
			out[i] = portStart + i
		}
	}
	return out
}

func proxyFromNode(subscriptionID uint64, node model.ClashNode, localPort int) *model.Proxy {
	id := subscriptionID
	name := strings.TrimSpace(node.Name)
	if name == "" {
		name = fmt.Sprintf("%s:%d", node.Server, node.Port)
	}
	remark := fmt.Sprintf("[订阅/%s] %s", strings.ToUpper(node.Type), name)
	if isDirectProxyType(node.Type) {
		proto := strings.ToLower(node.Type)
		if proto == "socks5" {
			proto = model.ProxyProtoSOCKS5H
		}
		if node.Server == "" || node.Port <= 0 || node.Port > 65535 {
			return nil
		}
		return &model.Proxy{
			Name:           name,
			Protocol:       proto,
			Host:           node.Server,
			Port:           uint16(node.Port),
			Status:         model.ProxyStatusEnabled,
			Remark:         &remark,
			SubscriptionID: &id,
			SubNodeName:    name,
		}
	}
	if localPort <= 0 || localPort > 65535 {
		return nil
	}
	return &model.Proxy{
		Name:           name,
		Protocol:       model.ProxyProtoHTTP,
		Host:           "127.0.0.1",
		Port:           uint16(localPort),
		Status:         model.ProxyStatusEnabled,
		Remark:         &remark,
		SubscriptionID: &id,
		SubNodeName:    name,
	}
}

func nodePreview(nodes []model.ClashNode, limit int) []dto.ClashNodeResp {
	if limit <= 0 || limit > len(nodes) {
		limit = len(nodes)
	}
	out := make([]dto.ClashNodeResp, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, dto.ClashNodeResp{
			Name:   nodes[i].Name,
			Type:   nodes[i].Type,
			Server: nodes[i].Server,
			Port:   nodes[i].Port,
		})
	}
	return out
}

func proxySubscriptionResp(row *model.ProxySubscription) *dto.ProxySubscriptionResp {
	resp := &dto.ProxySubscriptionResp{
		ID:              row.ID,
		Name:            row.Name,
		PortStart:       row.PortStart,
		NodeCount:       row.NodeCount,
		AutoSync:        row.AutoSync,
		SyncIntervalMin: row.SyncIntervalMin,
		Status:          row.Status,
		CreatedAt:       row.CreatedAt.Unix(),
		UpdatedAt:       row.UpdatedAt.Unix(),
	}
	if row.LastSyncAt != nil {
		resp.LastSyncAt = row.LastSyncAt.Unix()
	}
	if row.LastError != nil {
		resp.LastError = *row.LastError
	}
	return resp
}

func hostPortFromURL(u *url.URL) (string, int) {
	host := strings.TrimSpace(u.Hostname())
	port, _ := strconv.Atoi(u.Port())
	return host, port
}

func nodeName(u *url.URL, fallback string) string {
	if u.Fragment != "" {
		if v, err := url.QueryUnescape(u.Fragment); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return strings.TrimSpace(u.Fragment)
	}
	return fallback
}

func stringFromMap(m map[string]interface{}, key string) string {
	return stringFromAny(m[key])
}

func intFromMap(m map[string]interface{}, key string) int {
	return intFromAny(m[key])
}

func stringFromAny(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func intFromAny(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}
