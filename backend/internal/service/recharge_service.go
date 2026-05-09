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
	SettingAlipaySellerID      = "payment.alipay_seller_id"
	SettingAlipayGatewayURL    = "payment.alipay_gateway_url"
	SettingAlipaySubjectPrefix = "payment.alipay_subject_prefix"

	rechargeOrderTTL = 30 * time.Minute
)

type RechargeService struct {
	db   *gorm.DB
	repo *repo.RechargeRepo
	sys  *SystemConfigService
}

type RechargeReconcileStats struct {
	Scanned   int
	Paid      int
	Expired   int
	Unchanged int
	Errors    int
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
			existing, err = s.refreshPendingAlipayOrder(ctx, existing)
			if err != nil {
				return nil, err
			}
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
	promo, err := s.preparePromo(ctx, userID, req.PromoCode, pkg.ID, amount)
	if err != nil {
		return nil, err
	}
	payAmount := amount
	bonusPoints := pkg.BonusPoints
	if promo != nil {
		payAmount = amount - promo.DiscountAmount
		bonusPoints += promo.GiftPoints
		if payAmount <= 0 {
			return nil, errcode.InvalidParam.WithMsg("优惠后支付金额必须大于 0")
		}
	}
	extra := map[string]any{
		"package_id":   pkg.ID,
		"package_name": pkg.Name,
		"badge":        pkg.Badge,
		"remark":       pkg.Remark,
	}
	if promo != nil {
		extra["promo_id"] = promo.ID
		extra["promo_code"] = promo.Code
		extra["promo_discount"] = promo.DiscountAmount
		extra["promo_gift_points"] = promo.GiftPoints
		extra["original_amount"] = amount
	}
	extraRaw := mustJSON(extra)
	clientIP := strings.TrimSpace(ip)
	row := &model.RechargeRecord{
		OrderNo:     orderNo,
		UserID:      userID,
		Channel:     model.RechargeChannelAlipay,
		Amount:      payAmount,
		Points:      pkg.Points,
		BonusPoints: bonusPoints,
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
		AmountFen:  payAmount,
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
	if promo != nil {
		if err := s.reservePromo(ctx, userID, row.OrderNo, promo); err != nil {
			_ = s.repo.Update(ctx, row.ID, map[string]any{
				"status": model.RechargeStatusFailed,
				"extra":  mergeExtra(row.Extra, map[string]any{"promo_error": err.Error()}),
			})
			return nil, err
		}
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
	row, err = s.refreshPendingAlipayOrder(ctx, row)
	if err != nil {
		return nil, err
	}
	return orderResp(row), nil
}

func (s *RechargeService) CancelUserOrder(ctx context.Context, userID uint64, orderNo string) (*dto.RechargeOrderResp, error) {
	row, err := s.repo.GetUserOrder(ctx, userID, strings.TrimSpace(orderNo))
	if err != nil {
		if err == repo.ErrNotFound {
			return nil, errcode.ResourceMissing
		}
		return nil, errcode.DBError.Wrap(err)
	}
	row, err = s.refreshPendingAlipayOrder(ctx, row)
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case model.RechargeStatusCanceled, model.RechargeStatusExpired, model.RechargeStatusFailed:
		return orderResp(row), nil
	case model.RechargeStatusPaid:
		return nil, errcode.InvalidParam.WithMsg("订单已支付，不能取消")
	}
	if row.Status != model.RechargeStatusPending {
		return orderResp(row), nil
	}

	extra := map[string]any{
		"cancelled_at": time.Now().UTC().Format(time.RFC3339),
		"cancelled_by": "user",
	}
	if row.Channel == model.RechargeChannelAlipay {
		client, err := s.alipayClient(ctx)
		if err != nil {
			return nil, errcode.Internal.WithMsg("支付宝订单取消失败，请稍后重试")
		}
		if !client.Enabled() {
			return nil, errcode.Forbidden.WithMsg("支付宝未配置完整")
		}
		closeRes, err := client.Close(ctx, row.OrderNo)
		if err != nil {
			logger.FromCtx(ctx).Warn("alipay.cancel.close_failed", zap.String("order_no", row.OrderNo), zap.Error(err))
			return nil, errcode.Internal.WithMsg("支付宝订单取消失败，请稍后重试")
		}
		extra["alipay_close_at"] = time.Now().UTC().Format(time.RFC3339)
		extra["alipay_close_out_trade_no"] = closeRes.OutTradeNo
		extra["alipay_close_trade_no"] = closeRes.TradeNo
	}

	merged := mergeExtra(row.Extra, extra)
	affected, err := s.repo.UpdateIfStatus(ctx, row.ID, model.RechargeStatusPending, map[string]any{
		"status": model.RechargeStatusCanceled,
		"extra":  merged,
	})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	if affected == 0 {
		refreshed, err := s.repo.GetByOrderNo(ctx, row.OrderNo)
		if err != nil {
			return nil, errcode.DBError.Wrap(err)
		}
		return orderResp(refreshed), nil
	}
	_ = s.releasePromoReservation(ctx, row)
	row.Status = model.RechargeStatusCanceled
	row.Extra = &merged
	row.UpdatedAt = time.Now().UTC()
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

func (s *RechargeService) ReconcilePendingAlipayOrders(ctx context.Context, limit int) (*RechargeReconcileStats, error) {
	stats := &RechargeReconcileStats{}
	if s == nil || s.repo == nil {
		return stats, fmt.Errorf("recharge service not initialized")
	}
	before := time.Now().UTC().Add(-1 * time.Minute)
	rows, err := s.repo.ListPendingByChannelBefore(ctx, model.RechargeChannelAlipay, before, limit)
	if err != nil {
		return stats, errcode.DBError.Wrap(err)
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		stats.Scanned++
		originalStatus := row.Status
		refreshed, err := s.refreshPendingAlipayOrder(ctx, row)
		if err != nil {
			stats.Errors++
			logger.FromCtx(ctx).Warn("alipay.reconcile.order_failed", zap.String("order_no", row.OrderNo), zap.Error(err))
			continue
		}
		switch {
		case refreshed == nil:
			stats.Unchanged++
		case refreshed.Status == model.RechargeStatusPaid && originalStatus != model.RechargeStatusPaid:
			stats.Paid++
		case refreshed.Status == model.RechargeStatusExpired && originalStatus != model.RechargeStatusExpired:
			stats.Expired++
		default:
			stats.Unchanged++
		}
	}
	return stats, nil
}

func (s *RechargeService) StartAlipayReconcileLoop(ctx context.Context, interval time.Duration, limit int) {
	if s == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := s.ReconcilePendingAlipayOrders(ctx, limit)
				if err != nil {
					logger.FromCtx(ctx).Warn("alipay.reconcile.failed", zap.Error(err))
					continue
				}
				if stats.Scanned > 0 || stats.Errors > 0 {
					logger.FromCtx(ctx).Info("alipay.reconcile.done",
						zap.Int("scanned", stats.Scanned),
						zap.Int("paid", stats.Paid),
						zap.Int("expired", stats.Expired),
						zap.Int("unchanged", stats.Unchanged),
						zap.Int("errors", stats.Errors),
					)
				}
			}
		}
	}()
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
	if err := validateAlipayNotifyIdentity(client, pl); err != nil {
		logger.FromCtx(ctx).Warn("alipay.notify.identity_mismatch", zap.String("order_no", pl.OutTradeNo), zap.Error(err))
		return "fail", err
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
		if err := validateAlipayTradeNo(order.ChannelTradeNo, pl.TradeNo); err != nil {
			return err
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
		extra := mergeExtra(order.Extra, map[string]any{"notify": pl.Raw, "buyer_id": pl.BuyerID, "seller_id": pl.SellerID})
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

type promoApplyResult struct {
	ID             uint64
	Code           string
	DiscountAmount int64
	GiftPoints     int64
}

func (s *RechargeService) preparePromo(ctx context.Context, userID uint64, code, packageID string, amount int64) (*promoApplyResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, nil
	}
	var promo model.PromoCode
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&promo).Error; err != nil {
		return nil, errcode.InvalidParam.WithMsg("优惠码无效")
	}
	now := time.Now().UTC()
	if !promo.Active(now) {
		return nil, errcode.InvalidParam.WithMsg("优惠码不可用或已过期")
	}
	if promo.MinAmount > 0 && amount < promo.MinAmount {
		return nil, errcode.InvalidParam.WithMsg("未达到优惠码最低金额")
	}
	if promo.ApplyTo != "" && promo.ApplyTo != "all" && promo.ApplyTo != packageID {
		return nil, errcode.InvalidParam.WithMsg("优惠码不适用于当前套餐")
	}
	if promo.TotalQty > 0 && promo.UsedQty >= promo.TotalQty {
		return nil, errcode.InvalidParam.WithMsg("优惠码已领完")
	}
	if promo.PerUserLimit > 0 {
		var used int64
		if err := s.db.WithContext(ctx).Model(&model.PromoCodeUse{}).
			Where("promo_id = ? AND user_id = ?", promo.ID, userID).
			Count(&used).Error; err != nil {
			return nil, errcode.DBError.Wrap(err)
		}
		if int(used) >= promo.PerUserLimit {
			return nil, errcode.InvalidParam.WithMsg("已达到优惠码使用上限")
		}
	}
	out := &promoApplyResult{ID: promo.ID, Code: promo.Code}
	switch promo.DiscountType {
	case model.PromoTypeAmount:
		out.DiscountAmount = promo.DiscountVal
	case model.PromoTypeDiscount:
		if promo.DiscountVal <= 0 || promo.DiscountVal >= 100 {
			return nil, errcode.InvalidParam.WithMsg("折扣优惠码配置无效")
		}
		out.DiscountAmount = amount * (100 - promo.DiscountVal) / 100
	case model.PromoTypeGift:
		out.GiftPoints = promo.DiscountVal
	default:
		return nil, errcode.InvalidParam.WithMsg("优惠码类型不支持")
	}
	if out.DiscountAmount < 0 {
		out.DiscountAmount = 0
	}
	if out.DiscountAmount >= amount {
		out.DiscountAmount = amount - 1
	}
	return out, nil
}

func (s *RechargeService) reservePromo(ctx context.Context, userID uint64, orderNo string, promo *promoApplyResult) error {
	if promo == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.PromoCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", promo.ID).First(&row).Error; err != nil {
			return errcode.InvalidParam.WithMsg("优惠码无效")
		}
		if row.TotalQty > 0 && row.UsedQty >= row.TotalQty {
			return errcode.InvalidParam.WithMsg("优惠码已领完")
		}
		if row.PerUserLimit > 0 {
			var used int64
			if err := tx.Model(&model.PromoCodeUse{}).
				Where("promo_id = ? AND user_id = ?", promo.ID, userID).
				Count(&used).Error; err != nil {
				return errcode.DBError.Wrap(err)
			}
			if int(used) >= row.PerUserLimit {
				return errcode.InvalidParam.WithMsg("已达到优惠码使用上限")
			}
		}
		use := &model.PromoCodeUse{
			PromoID:  promo.ID,
			Code:     promo.Code,
			UserID:   userID,
			OrderNo:  &orderNo,
			Discount: promo.DiscountAmount,
		}
		if err := tx.Create(use).Error; err != nil {
			return errcode.DBError.Wrap(err)
		}
		return tx.Model(&model.PromoCode{}).
			Where("id = ?", promo.ID).
			UpdateColumn("used_qty", gorm.Expr("used_qty + 1")).Error
	})
}

func (s *RechargeService) releasePromoReservation(ctx context.Context, row *model.RechargeRecord) error {
	if row == nil {
		return nil
	}
	extra := parseExtra(row.Extra)
	promoID := uint64(int64FromExtra(extra, "promo_id"))
	if promoID == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("promo_id = ? AND user_id = ? AND order_no = ?", promoID, row.UserID, row.OrderNo).
			Delete(&model.PromoCodeUse{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		return tx.Model(&model.PromoCode{}).
			Where("id = ? AND used_qty > 0", promoID).
			UpdateColumn("used_qty", gorm.Expr("used_qty - 1")).Error
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
		SellerID:     envString("KLEIN_ALIPAY_SELLER_ID", s.sys.GetString(ctx, SettingAlipaySellerID, "")),
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
		ID:              row.ID,
		OrderNo:         row.OrderNo,
		Channel:         row.Channel,
		Amount:          row.Amount,
		OriginalAmount:  int64FromExtra(extra, "original_amount"),
		DiscountAmount:  int64FromExtra(extra, "promo_discount"),
		PromoCode:       stringFromExtra(extra, "promo_code"),
		PromoGiftPoints: int64FromExtra(extra, "promo_gift_points"),
		Points:          row.Points,
		BonusPoints:     row.BonusPoints,
		TotalPoints:     row.Points + row.BonusPoints,
		Status:          row.Status,
		QRCode:          stringFromExtra(extra, "qr_code"),
		PaidAt:          paidAt,
		CreatedAt:       row.CreatedAt.Unix(),
	}
}

func (s *RechargeService) refreshPendingAlipayOrder(ctx context.Context, row *model.RechargeRecord) (*model.RechargeRecord, error) {
	if row == nil || row.Status != model.RechargeStatusPending || row.Channel != model.RechargeChannelAlipay {
		return row, nil
	}
	client, err := s.alipayClient(ctx)
	if err != nil {
		logger.FromCtx(ctx).Warn("alipay.query.client_unavailable", zap.String("order_no", row.OrderNo), zap.Error(err))
		return row, nil
	}
	if !client.Enabled() {
		return row, nil
	}
	query, err := client.Query(ctx, row.OrderNo)
	if err != nil {
		logger.FromCtx(ctx).Warn("alipay.query.failed", zap.String("order_no", row.OrderNo), zap.Error(err))
		return row, nil
	}
	status := strings.TrimSpace(query.TradeStatus)
	queryOutTradeNo := strings.TrimSpace(query.OutTradeNo)
	if queryOutTradeNo != "" && queryOutTradeNo != row.OrderNo {
		logger.FromCtx(ctx).Warn("alipay.query.out_trade_no_mismatch", zap.String("order_no", row.OrderNo), zap.String("got", queryOutTradeNo))
		return row, nil
	}
	extra := map[string]any{
		"alipay_query_at":           time.Now().UTC().Format(time.RFC3339),
		"alipay_query_trade_status": status,
	}
	switch status {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		pl := &alipay.NotifyPayload{
			AppID:       client.AppID(),
			OutTradeNo:  strings.TrimSpace(query.OutTradeNo),
			TradeNo:     strings.TrimSpace(query.TradeNo),
			TradeStatus: status,
			TotalAmount: strings.TrimSpace(query.TotalAmount),
			BuyerID:     strings.TrimSpace(query.BuyerID),
			Raw: map[string]string{
				"source":       "trade_query",
				"out_trade_no": strings.TrimSpace(query.OutTradeNo),
				"trade_no":     strings.TrimSpace(query.TradeNo),
				"trade_status": status,
				"total_amount": strings.TrimSpace(query.TotalAmount),
				"buyer_id":     strings.TrimSpace(query.BuyerID),
			},
		}
		if pl.OutTradeNo == "" {
			pl.OutTradeNo = row.OrderNo
		}
		if err := s.settleAlipay(ctx, pl); err != nil {
			return nil, err
		}
		refreshed, err := s.repo.GetByOrderNo(ctx, row.OrderNo)
		if err != nil {
			return nil, errcode.DBError.Wrap(err)
		}
		return refreshed, nil
	case "TRADE_CLOSED":
		return s.expirePendingOrder(ctx, row, extra)
	}
	if isExpiredPendingRecharge(row, time.Now()) {
		return s.closeExpiredPendingAlipayOrder(ctx, row, client, extra)
	}
	return row, nil
}

func (s *RechargeService) closeExpiredPendingAlipayOrder(ctx context.Context, row *model.RechargeRecord, client *alipay.Client, extra map[string]any) (*model.RechargeRecord, error) {
	now := time.Now().UTC()
	if extra == nil {
		extra = map[string]any{}
	}
	closeRes, err := client.Close(ctx, row.OrderNo)
	if err != nil {
		logger.FromCtx(ctx).Warn("alipay.close.failed", zap.String("order_no", row.OrderNo), zap.Error(err))
		extra["alipay_close_failed_at"] = now.Format(time.RFC3339)
		extra["alipay_close_error"] = err.Error()
		merged := mergeExtra(row.Extra, extra)
		_, updateErr := s.repo.UpdateIfStatus(ctx, row.ID, model.RechargeStatusPending, map[string]any{"extra": merged})
		if updateErr != nil {
			return nil, errcode.DBError.Wrap(updateErr)
		}
		row.Extra = &merged
		return row, nil
	}
	extra["alipay_close_at"] = now.Format(time.RFC3339)
	extra["alipay_close_out_trade_no"] = closeRes.OutTradeNo
	extra["alipay_close_trade_no"] = closeRes.TradeNo
	return s.expirePendingOrder(ctx, row, extra)
}

func (s *RechargeService) expirePendingOrder(ctx context.Context, row *model.RechargeRecord, extraFields map[string]any) (*model.RechargeRecord, error) {
	force := false
	if extraFields != nil && strings.TrimSpace(fmt.Sprint(extraFields["alipay_query_trade_status"])) == "TRADE_CLOSED" {
		force = true
	}
	if !force && !isExpiredPendingRecharge(row, time.Now()) {
		return row, nil
	}
	now := time.Now().UTC()
	if extraFields == nil {
		extraFields = map[string]any{}
	}
	extraFields["expired_at"] = now.Format(time.RFC3339)
	extra := mergeExtra(row.Extra, extraFields)
	affected, err := s.repo.UpdateIfStatus(ctx, row.ID, model.RechargeStatusPending, map[string]any{
		"status": model.RechargeStatusExpired,
		"extra":  extra,
	})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	if affected == 0 {
		refreshed, err := s.repo.GetByOrderNo(ctx, row.OrderNo)
		if err != nil {
			return nil, errcode.DBError.Wrap(err)
		}
		return refreshed, nil
	}
	_ = s.releasePromoReservation(ctx, row)
	row.Status = model.RechargeStatusExpired
	row.Extra = &extra
	row.UpdatedAt = now
	return row, nil
}

func validateAlipayNotifyIdentity(client *alipay.Client, pl *alipay.NotifyPayload) error {
	if client == nil || pl == nil {
		return fmt.Errorf("alipay notify payload missing")
	}
	appID := strings.TrimSpace(client.AppID())
	if appID != "" {
		got := strings.TrimSpace(pl.AppID)
		if got == "" {
			return fmt.Errorf("alipay app_id missing")
		}
		if got != appID {
			return fmt.Errorf("alipay app_id mismatch")
		}
	}
	sellerID := strings.TrimSpace(client.SellerID())
	if sellerID == "" {
		return nil
	}
	gotSellerID := strings.TrimSpace(pl.SellerID)
	if gotSellerID == "" {
		return fmt.Errorf("alipay seller_id missing")
	}
	if gotSellerID != sellerID {
		return fmt.Errorf("alipay seller_id mismatch")
	}
	return nil
}

func validateAlipayTradeNo(stored *string, got string) error {
	storedValue := ""
	if stored != nil {
		storedValue = strings.TrimSpace(*stored)
	}
	got = strings.TrimSpace(got)
	if storedValue != "" && got != "" && storedValue != got {
		return fmt.Errorf("alipay trade_no mismatch")
	}
	return nil
}

func isExpiredPendingRecharge(row *model.RechargeRecord, now time.Time) bool {
	return row != nil &&
		row.Status == model.RechargeStatusPending &&
		!row.CreatedAt.IsZero() &&
		!row.CreatedAt.Add(rechargeOrderTTL).After(now)
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

func int64FromExtra(extra map[string]any, key string) int64 {
	switch v := extra[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func normalizeIdemKey(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 64 {
		return v[:64]
	}
	return v
}
