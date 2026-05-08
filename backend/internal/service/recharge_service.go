package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/alipay"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/logger"
)

const (
	SettingRechargePackages    = "recharge.packages"
	SettingPaymentEnabled      = "payment.enabled"
	SettingPaymentProvider     = "payment.provider"
	SettingPaymentNotifyURL    = "payment.notify_url"
	SettingAlipayAppID         = "payment.alipay_app_id"
	SettingAlipayPrivateKey    = "payment.alipay_private_key"
	SettingAlipayPublicKey     = "payment.alipay_public_key"
	SettingAlipayGatewayURL    = "payment.alipay_gateway_url"
	SettingAlipaySubjectPrefix = "payment.alipay_subject_prefix"
)

type RechargeService struct {
	db   *gorm.DB
	repo *repo.RechargeRepo
	sys  *SystemConfigService
}

type rechargePackage struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Amount      float64 `json:"amount"`
	Points      int64   `json:"points"`
	BonusPoints int64   `json:"bonus_points"`
	Enabled     bool    `json:"enabled"`
	SortOrder   int     `json:"sort_order"`
	Badge       string  `json:"badge"`
	Remark      string  `json:"remark"`
}

func NewRechargeService(db *gorm.DB, rechargeRepo *repo.RechargeRepo, sys *SystemConfigService) *RechargeService {
	return &RechargeService{db: db, repo: rechargeRepo, sys: sys}
}

func (s *RechargeService) ListPackages(ctx context.Context) ([]dto.RechargePackageResp, error) {
	pkgs, err := s.packages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.RechargePackageResp, 0, len(pkgs))
	for _, p := range pkgs {
		if !p.Enabled {
			continue
		}
		out = append(out, packageResp(p))
	}
	return out, nil
}

func (s *RechargeService) CreateOrder(ctx context.Context, userID uint64, req *dto.CreateRechargeOrderReq, ip, idemKey string) (*dto.RechargeOrderResp, error) {
	idemKey = normalizeIdemKey(idemKey)
	if idemKey != "" {
		if existing, err := s.repo.GetByIdem(ctx, userID, idemKey); err == nil && existing != nil {
			return orderResp(existing), nil
		} else if err != nil && err != repo.ErrNotFound {
			return nil, errcode.DBError.Wrap(err)
		}
	}
	if !s.paymentEnabled(ctx) {
		return nil, errcode.Forbidden.WithMsg("支付通道未启用")
	}
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(s.paymentProvider(ctx)))
	}
	if channel != model.RechargeChannelAlipay {
		return nil, errcode.InvalidParam.WithMsg("当前仅支持支付宝")
	}
	pkg, err := s.findPackage(ctx, req.PackageID)
	if err != nil {
		return nil, err
	}
	client, err := s.alipayClient(ctx)
	if err != nil {
		return nil, err
	}
	if !client.Enabled() {
		return nil, errcode.Forbidden.WithMsg("支付宝未配置完整")
	}

	orderNo, err := genRechargeOrderNo()
	if err != nil {
		return nil, errcode.Internal.Wrap(err)
	}
	amount := amountCents(pkg.Amount)
	if amount <= 0 || pkg.Points+pkg.BonusPoints <= 0 {
		return nil, errcode.InvalidParam.WithMsg("充值套餐配置无效")
	}
	extra := map[string]any{
		"package_id":   pkg.ID,
		"package_name": pkg.Name,
		"badge":        pkg.Badge,
		"remark":       pkg.Remark,
	}
	extraRaw := mustJSON(extra)
	clientIP := strings.TrimSpace(ip)
	row := &model.RechargeRecord{
		OrderNo:     orderNo,
		UserID:      userID,
		Channel:     model.RechargeChannelAlipay,
		Amount:      amount,
		Points:      pkg.Points,
		BonusPoints: pkg.BonusPoints,
		Status:      model.RechargeStatusPending,
		ClientIP:    &clientIP,
		IdemKey:     &idemKey,
		Extra:       &extraRaw,
	}
	if clientIP == "" {
		row.ClientIP = nil
	}
	if idemKey == "" {
		row.IdemKey = nil
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}

	pre, err := client.Precreate(ctx, alipay.PrecreateInput{
		OutTradeNo: orderNo,
		Subject:    pkg.Name,
		AmountFen:  amount,
		Timeout:    30 * time.Minute,
	})
	if err != nil {
		failExtra := mergeExtra(row.Extra, map[string]any{"alipay_error": err.Error()})
		_ = s.repo.Update(ctx, row.ID, map[string]any{
			"status": model.RechargeStatusFailed,
			"extra":  failExtra,
		})
		return nil, errcode.Internal.WithMsg("支付宝下单失败")
	}
	extraRaw = mergeExtra(row.Extra, map[string]any{
		"qr_code":         pre.QRCode,
		"alipay_trade_no": pre.TradeNo,
	})
	tradeNo := strings.TrimSpace(pre.TradeNo)
	row.ChannelTradeNo = &tradeNo
	row.Extra = &extraRaw
	if tradeNo == "" {
		row.ChannelTradeNo = nil
	}
	if err := s.repo.Update(ctx, row.ID, map[string]any{
		"channel_trade_no": row.ChannelTradeNo,
		"extra":            row.Extra,
	}); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return orderResp(row), nil
}

func (s *RechargeService) GetUserOrder(ctx context.Context, userID uint64, orderNo string) (*dto.RechargeOrderResp, error) {
	row, err := s.repo.GetUserOrder(ctx, userID, strings.TrimSpace(orderNo))
	if err != nil {
		if err == repo.ErrNotFound {
			return nil, errcode.ResourceMissing
		}
		return nil, errcode.DBError.Wrap(err)
	}
	return orderResp(row), nil
}

func (s *RechargeService) ListUserOrders(ctx context.Context, userID uint64, page, pageSize int) ([]*dto.RechargeOrderResp, int64, error) {
	rows, total, err := s.repo.ListUserOrders(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.RechargeOrderResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, orderResp(row))
	}
	return out, total, nil
}

func (s *RechargeService) HandleAlipayNotify(ctx context.Context, form url.Values) (string, error) {
	client, err := s.alipayClient(ctx)
	if err != nil {
		return "fail", err
	}
	pl, err := client.ParseNotify(form)
	if err != nil {
		logger.FromCtx(ctx).Warn("alipay.notify.invalid_signature", zap.String("order_no", form.Get("out_trade_no")), zap.Error(err))
		return "fail", err
	}
	if pl.AppID != "" && pl.AppID != client.AppID() {
		return "fail", fmt.Errorf("alipay app_id mismatch")
	}
	if pl.TradeStatus != "TRADE_SUCCESS" && pl.TradeStatus != "TRADE_FINISHED" {
		return "success", nil
	}
	if err := s.settleAlipay(ctx, pl); err != nil {
		logger.FromCtx(ctx).Error("alipay.notify.settle_failed", zap.String("order_no", pl.OutTradeNo), zap.Error(err))
		return "fail", err
	}
	return "success", nil
}

func (s *RechargeService) settleAlipay(ctx context.Context, pl *alipay.NotifyPayload) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.RechargeRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_no = ?", pl.OutTradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.Status == model.RechargeStatusPaid {
			return nil
		}
		if order.Status != model.RechargeStatusPending {
			return nil
		}
		if order.Channel != model.RechargeChannelAlipay {
			return fmt.Errorf("channel mismatch: %s", order.Channel)
		}
		if err := verifyAmount(pl.TotalAmount, order.Amount); err != nil {
			return err
		}
		var u model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", order.UserID).First(&u).Error; err != nil {
			return err
		}
		credit := order.Points + order.BonusPoints
		if credit <= 0 {
			return fmt.Errorf("invalid credit amount")
		}
		before := u.Points
		after := before + credit
		now := time.Now().UTC()
		extra := mergeExtra(order.Extra, map[string]any{"notify": pl.Raw, "buyer_id": pl.BuyerID})
		res := tx.Model(&model.RechargeRecord{}).Where("id = ? AND status = ?", order.ID, model.RechargeStatusPending).
			Updates(map[string]any{
				"status":           model.RechargeStatusPaid,
				"paid_at":          now,
				"channel_trade_no": pl.TradeNo,
				"extra":            extra,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		if err := tx.Model(&model.User{}).Where("id = ?", order.UserID).
			UpdateColumns(map[string]any{
				"points":         after,
				"total_recharge": gorm.Expr("total_recharge + ?", credit),
			}).Error; err != nil {
			return err
		}
		remark := fmt.Sprintf("支付宝充值:%s", order.OrderNo)
		log := &model.WalletLog{
			UserID:       order.UserID,
			Direction:    1,
			BizType:      model.BizRecharge,
			BizID:        order.OrderNo,
			Points:       credit,
			PointsBefore: before,
			PointsAfter:  after,
			Remark:       &remark,
		}
		return tx.Create(log).Error
	})
}

func (s *RechargeService) packages(ctx context.Context) ([]rechargePackage, error) {
	raw := s.sys.GetString(ctx, SettingRechargePackages, "")
	if strings.TrimSpace(raw) == "" {
		return defaultRechargePackages(), nil
	}
	var pkgs []rechargePackage
	if err := json.Unmarshal([]byte(raw), &pkgs); err != nil {
		return nil, errcode.InvalidParam.WithMsg("充值套餐配置格式错误")
	}
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].SortOrder < pkgs[j].SortOrder })
	return pkgs, nil
}

func (s *RechargeService) findPackage(ctx context.Context, packageID string) (*rechargePackage, error) {
	packageID = strings.TrimSpace(packageID)
	pkgs, err := s.packages(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if p.ID == packageID && p.Enabled {
			return &p, nil
		}
	}
	return nil, errcode.ResourceMissing.WithMsg("充值套餐不可用")
}

func (s *RechargeService) paymentEnabled(ctx context.Context) bool {
	if val := strings.TrimSpace(os.Getenv("KLEIN_PAYMENT_ENABLED")); val != "" {
		return envBool(val)
	}
	return s.sys.GetBool(ctx, SettingPaymentEnabled, false)
}

func (s *RechargeService) paymentProvider(ctx context.Context) string {
	return envString("KLEIN_PAYMENT_PROVIDER", s.sys.GetString(ctx, SettingPaymentProvider, "alipay"))
}

func (s *RechargeService) alipayClient(ctx context.Context) (*alipay.Client, error) {
	cfg := alipay.Config{
		AppID:        envString("KLEIN_ALIPAY_APP_ID", s.sys.GetString(ctx, SettingAlipayAppID, "")),
		PrivateKey:   envString("KLEIN_ALIPAY_PRIVATE_KEY", s.sys.GetString(ctx, SettingAlipayPrivateKey, "")),
		AlipayPubKey: envString("KLEIN_ALIPAY_PUBLIC_KEY", s.sys.GetString(ctx, SettingAlipayPublicKey, "")),
		GatewayURL:   envString("KLEIN_ALIPAY_GATEWAY_URL", s.sys.GetString(ctx, SettingAlipayGatewayURL, "")),
		NotifyURL:    envString("KLEIN_PAYMENT_NOTIFY_URL", s.sys.GetString(ctx, SettingPaymentNotifyURL, "")),
		SubjectPref:  envString("KLEIN_ALIPAY_SUBJECT_PREFIX", s.sys.GetString(ctx, SettingAlipaySubjectPrefix, "")),
	}
	client, err := alipay.NewClient(cfg)
	if err != nil {
		return nil, errcode.InvalidParam.WithMsg("支付宝配置无效")
	}
	return client, nil
}

func packageResp(p rechargePackage) dto.RechargePackageResp {
	amount := amountCents(p.Amount)
	return dto.RechargePackageResp{
		ID:          p.ID,
		Name:        p.Name,
		Amount:      amount,
		Points:      p.Points,
		BonusPoints: p.BonusPoints,
		TotalPoints: p.Points + p.BonusPoints,
		Badge:       p.Badge,
		Remark:      p.Remark,
		SortOrder:   p.SortOrder,
	}
}

func orderResp(row *model.RechargeRecord) *dto.RechargeOrderResp {
	if row == nil {
		return nil
	}
	extra := parseExtra(row.Extra)
	paidAt := int64(0)
	if row.PaidAt != nil {
		paidAt = row.PaidAt.Unix()
	}
	return &dto.RechargeOrderResp{
		ID:          row.ID,
		OrderNo:     row.OrderNo,
		Channel:     row.Channel,
		Amount:      row.Amount,
		Points:      row.Points,
		BonusPoints: row.BonusPoints,
		TotalPoints: row.Points + row.BonusPoints,
		Status:      row.Status,
		QRCode:      stringFromExtra(extra, "qr_code"),
		PaidAt:      paidAt,
		CreatedAt:   row.CreatedAt.Unix(),
	}
}

func defaultRechargePackages() []rechargePackage {
	return []rechargePackage{
		{ID: "p100", Name: "100 点套餐", Amount: 10, Points: 10000, BonusPoints: 0, Enabled: true, SortOrder: 10},
		{ID: "p500", Name: "500 点套餐", Amount: 45, Points: 50000, BonusPoints: 5000, Enabled: true, SortOrder: 20, Badge: "推荐"},
	}
}

func amountCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

func verifyAmount(got string, wantFen int64) error {
	v, err := strconv.ParseFloat(strings.TrimSpace(got), 64)
	if err != nil {
		return err
	}
	gotFen := int64(math.Round(v * 100))
	if gotFen != wantFen {
		return fmt.Errorf("amount mismatch got=%d want=%d", gotFen, wantFen)
	}
	return nil
}

func genRechargeOrderNo() (string, error) {
	s, err := crypto.RandomString(10)
	if err != nil {
		return "", err
	}
	return "R" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(s[:8]), nil
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func parseExtra(raw *string) map[string]any {
	out := map[string]any{}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(*raw), &out)
	return out
}

func mergeExtra(raw *string, patch map[string]any) string {
	out := parseExtra(raw)
	for k, v := range patch {
		out[k] = v
	}
	return mustJSON(out)
}

func stringFromExtra(extra map[string]any, key string) string {
	if v, ok := extra[key].(string); ok {
		return v
	}
	return ""
}

func normalizeIdemKey(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 64 {
		return v[:64]
	}
	return v
}
