// Package alipay implements the subset of Alipay OpenAPI needed by face-to-face recharge orders.
package alipay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGateway = "https://openapi.alipay.com/gateway.do"
	defaultCharset = "utf-8"
	defaultVersion = "1.0"
	defaultSignTyp = "RSA2"
)

var (
	ErrDisabled    = errors.New("alipay: disabled")
	ErrInvalidSign = errors.New("alipay: invalid sign")
)

type Config struct {
	AppID        string
	SellerID     string
	PrivateKey   string
	AlipayPubKey string
	GatewayURL   string
	NotifyURL    string
	SignType     string
	SubjectPref  string
}

type Client struct {
	cfg        Config
	httpClient *http.Client
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = defaultGateway
	}
	if cfg.SignType == "" {
		cfg.SignType = defaultSignTyp
	}
	c := &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	if !c.Enabled() {
		return c, nil
	}
	privateKey, err := parsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	publicKey, err := parsePublicKey(cfg.AlipayPubKey)
	if err != nil {
		return nil, err
	}
	c.privateKey = privateKey
	c.publicKey = publicKey
	return c, nil
}

func (c *Client) Enabled() bool {
	return c != nil &&
		c.cfg.AppID != "" &&
		c.cfg.PrivateKey != "" &&
		c.cfg.AlipayPubKey != "" &&
		c.cfg.NotifyURL != ""
}

func (c *Client) AppID() string {
	if c == nil {
		return ""
	}
	return c.cfg.AppID
}

func (c *Client) SellerID() string {
	if c == nil {
		return ""
	}
	return c.cfg.SellerID
}

type PrecreateInput struct {
	OutTradeNo string
	Subject    string
	AmountFen  int64
	StoreID    string
	Timeout    time.Duration
}

type PrecreateResult struct {
	OutTradeNo string
	TradeNo    string
	QRCode     string
}

func (c *Client) Precreate(ctx context.Context, in PrecreateInput) (*PrecreateResult, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	if in.OutTradeNo == "" {
		return nil, errors.New("alipay: out_trade_no required")
	}
	if in.Subject == "" {
		in.Subject = "DAPO 点数充值"
	}
	if c.cfg.SubjectPref != "" {
		in.Subject = c.cfg.SubjectPref + in.Subject
	}
	if in.AmountFen <= 0 {
		return nil, errors.New("alipay: amount must be positive")
	}
	timeoutExpress := "30m"
	if in.Timeout > 0 {
		timeoutExpress = fmt.Sprintf("%dm", int(in.Timeout.Minutes()))
	}
	biz := map[string]any{
		"out_trade_no":    in.OutTradeNo,
		"total_amount":    amountYuan(in.AmountFen),
		"subject":         in.Subject,
		"timeout_express": timeoutExpress,
	}
	if in.StoreID != "" {
		biz["store_id"] = in.StoreID
	}
	respRaw, err := c.openAPI(ctx, "alipay.trade.precreate", "alipay_trade_precreate_response", biz, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
		OutTradeNo string `json:"out_trade_no"`
		TradeNo    string `json:"trade_no"`
		QRCode     string `json:"qr_code"`
	}
	if err := json.Unmarshal(respRaw, &response); err != nil {
		return nil, fmt.Errorf("alipay: decode response: %w", err)
	}
	if response.Code != "10000" {
		return nil, apiError("precreate", response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	if response.QRCode == "" {
		return nil, errors.New("alipay: empty qr_code")
	}
	return &PrecreateResult{
		OutTradeNo: response.OutTradeNo,
		TradeNo:    response.TradeNo,
		QRCode:     response.QRCode,
	}, nil
}

type TradeQueryResult struct {
	OutTradeNo  string
	TradeNo     string
	TradeStatus string
	TotalAmount string
	BuyerID     string
}

func (c *Client) Query(ctx context.Context, outTradeNo string) (*TradeQueryResult, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return nil, errors.New("alipay: out_trade_no required")
	}
	respRaw, err := c.openAPI(ctx, "alipay.trade.query", "alipay_trade_query_response", map[string]any{
		"out_trade_no": outTradeNo,
	}, false)
	if err != nil {
		return nil, err
	}
	var response struct {
		Code        string         `json:"code"`
		Msg         string         `json:"msg"`
		SubCode     string         `json:"sub_code"`
		SubMsg      string         `json:"sub_msg"`
		OutTradeNo  string         `json:"out_trade_no"`
		TradeNo     string         `json:"trade_no"`
		TradeStatus string         `json:"trade_status"`
		TotalAmount stringOrNumber `json:"total_amount"`
		BuyerUserID string         `json:"buyer_user_id"`
		BuyerID     string         `json:"buyer_id"`
	}
	if err := json.Unmarshal(respRaw, &response); err != nil {
		return nil, fmt.Errorf("alipay: decode response: %w", err)
	}
	if response.Code != "10000" {
		return nil, apiError("query", response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	buyerID := response.BuyerUserID
	if buyerID == "" {
		buyerID = response.BuyerID
	}
	return &TradeQueryResult{
		OutTradeNo:  response.OutTradeNo,
		TradeNo:     response.TradeNo,
		TradeStatus: response.TradeStatus,
		TotalAmount: response.TotalAmount.String(),
		BuyerID:     buyerID,
	}, nil
}

type TradeCloseResult struct {
	OutTradeNo string
	TradeNo    string
}

func (c *Client) Close(ctx context.Context, outTradeNo string) (*TradeCloseResult, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return nil, errors.New("alipay: out_trade_no required")
	}
	respRaw, err := c.openAPI(ctx, "alipay.trade.close", "alipay_trade_close_response", map[string]any{
		"out_trade_no": outTradeNo,
	}, false)
	if err != nil {
		return nil, err
	}
	var response struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
		OutTradeNo string `json:"out_trade_no"`
		TradeNo    string `json:"trade_no"`
	}
	if err := json.Unmarshal(respRaw, &response); err != nil {
		return nil, fmt.Errorf("alipay: decode response: %w", err)
	}
	if response.Code != "10000" {
		return nil, apiError("close", response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return &TradeCloseResult{OutTradeNo: response.OutTradeNo, TradeNo: response.TradeNo}, nil
}

type NotifyPayload struct {
	AppID       string
	SellerID    string
	OutTradeNo  string
	TradeNo     string
	TradeStatus string
	TotalAmount string
	BuyerID     string
	Raw         map[string]string
}

func (c *Client) ParseNotify(form url.Values) (*NotifyPayload, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	params := make(map[string]string, len(form))
	for k := range form {
		params[k] = form.Get(k)
	}
	if !c.Verify(params) {
		return nil, ErrInvalidSign
	}
	return &NotifyPayload{
		AppID:       params["app_id"],
		SellerID:    params["seller_id"],
		OutTradeNo:  params["out_trade_no"],
		TradeNo:     params["trade_no"],
		TradeStatus: params["trade_status"],
		TotalAmount: params["total_amount"],
		BuyerID:     params["buyer_id"],
		Raw:         params,
	}, nil
}

func (c *Client) Verify(params map[string]string) bool {
	if c == nil || c.publicKey == nil {
		return false
	}
	sign := params["sign"]
	if sign == "" {
		return false
	}
	canonical := canonicalString(params)
	return c.verifyContent(canonical, sign)
}

func (c *Client) openAPI(ctx context.Context, method, responseKey string, biz map[string]any, includeNotify bool) (json.RawMessage, error) {
	bizJSON, err := json.Marshal(biz)
	if err != nil {
		return nil, err
	}
	params := map[string]string{
		"app_id":      c.cfg.AppID,
		"method":      method,
		"format":      "JSON",
		"charset":     defaultCharset,
		"sign_type":   c.cfg.SignType,
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     defaultVersion,
		"biz_content": string(bizJSON),
	}
	if includeNotify && c.cfg.NotifyURL != "" {
		params["notify_url"] = c.cfg.NotifyURL
	}
	sign, err := c.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	body := url.Values{}
	for k, v := range params {
		body.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.GatewayURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("alipay: http status %d: %s", resp.StatusCode, string(raw))
	}
	respRaw, respSign, err := signedResponse(raw, responseKey)
	if err != nil {
		return nil, err
	}
	if !c.verifyContent(string(respRaw), respSign) {
		return nil, ErrInvalidSign
	}
	return respRaw, nil
}

func (c *Client) verifyContent(content, sign string) bool {
	if c == nil || c.publicKey == nil || strings.TrimSpace(sign) == "" {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(sign, " ", "+"))
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(c.publicKey, crypto.SHA256, sum[:], sig) == nil
}

func (c *Client) sign(params map[string]string) (string, error) {
	if c.privateKey == nil {
		return "", ErrDisabled
	}
	sum := sha256.Sum256([]byte(canonicalString(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func canonicalString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

func signedResponse(raw []byte, responseKey string) (json.RawMessage, string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, "", fmt.Errorf("alipay: decode response: %w", err)
	}
	respRaw := envelope[responseKey]
	if len(respRaw) == 0 || string(respRaw) == "null" {
		return nil, "", fmt.Errorf("alipay: missing response node %s", responseKey)
	}
	var sign string
	if rawSign := envelope["sign"]; len(rawSign) > 0 {
		_ = json.Unmarshal(rawSign, &sign)
	}
	if strings.TrimSpace(sign) == "" {
		return nil, "", errors.New("alipay: empty response sign")
	}
	return respRaw, sign, nil
}

type stringOrNumber string

func (s *stringOrNumber) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		*s = ""
		return nil
	}
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		*s = stringOrNumber(quoted)
		return nil
	}
	*s = stringOrNumber(text)
	return nil
}

func (s stringOrNumber) String() string {
	return string(s)
}

func apiError(op, code, msg, subCode, subMsg string) error {
	text := subMsg
	if text == "" {
		text = msg
	}
	if subCode != "" {
		text = subCode + ": " + text
	}
	if text == "" {
		text = code
	}
	return fmt.Errorf("alipay: %s failed: %s", op, text)
}

func amountYuan(fen int64) string {
	return strconv.FormatFloat(float64(fen)/100, 'f', 2, 64)
}

func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	raw = normalizePEM(raw, "PRIVATE KEY")
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, errors.New("alipay: invalid app private key pem")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("alipay: app private key is not rsa")
	}
	rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err2 == nil {
		return rsaKey, nil
	}
	return nil, err
}

func parsePublicKey(raw string) (*rsa.PublicKey, error) {
	raw = normalizePEM(raw, "PUBLIC KEY")
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, errors.New("alipay: invalid public key pem")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("alipay: public key is not rsa")
	}
	return pub, nil
}

func normalizePEM(raw, label string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "-----BEGIN ") {
		return raw
	}
	var b bytes.Buffer
	b.WriteString("-----BEGIN " + label + "-----\n")
	for len(raw) > 64 {
		b.WriteString(raw[:64] + "\n")
		raw = raw[64:]
	}
	if raw != "" {
		b.WriteString(raw + "\n")
	}
	b.WriteString("-----END " + label + "-----\n")
	return b.String()
}
