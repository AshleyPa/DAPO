package service

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
)

const (
	SettingBillingFreeInitialPoints = "billing.free_initial_points"
	SettingInviteEnabled            = "invite.enabled"
	SettingInviteNewUserPoints      = "invite.new_user_points"
	SettingInviteInviterRegister    = "invite.inviter_register_reward"
	SettingInviteFirstRecharge      = "invite.first_recharge_reward"
	SettingInviteLifetimeSharePct   = "invite.lifetime_share_pct"
)

const (
	inviteKindInviteeRegister = "invitee_register"
	inviteKindInviterRegister = "inviter_register"
	inviteKindFirstRecharge   = "first_recharge"
	inviteKindRechargeShare   = "recharge_share"
)

// InviteRewardSettings 是后台可配置的邀请奖励规则。积分字段均为内部单位（点 *100）。
type InviteRewardSettings struct {
	Enabled                  bool  `json:"enabled"`
	FreeInitialPoints        int64 `json:"free_initial_points"`
	InviteeRegisterPoints    int64 `json:"invitee_register_points"`
	InviterRegisterReward    int64 `json:"inviter_register_reward"`
	FirstRechargeReward      int64 `json:"first_recharge_reward"`
	LifetimeRechargeSharePct int64 `json:"lifetime_share_pct"`
}

// InvitePublicRules 是前台邀请中心可展示的公开规则。
type InvitePublicRules struct {
	Enabled                  bool  `json:"enabled"`
	FreeInitialPoints        int64 `json:"free_initial_points"`
	InviteeRegisterPoints    int64 `json:"invitee_register_points"`
	InviterRegisterReward    int64 `json:"inviter_register_reward"`
	FirstRechargeReward      int64 `json:"first_recharge_reward"`
	LifetimeRechargeSharePct int64 `json:"lifetime_share_pct"`
}

// InviteRewardService 负责邀请关系相关奖励发放。
type InviteRewardService struct {
	db     *gorm.DB
	wallet *repo.WalletRepo
	sys    *SystemConfigService
}

// NewInviteRewardService 构造。
func NewInviteRewardService(db *gorm.DB, wallet *repo.WalletRepo, sys *SystemConfigService) *InviteRewardService {
	return &InviteRewardService{db: db, wallet: wallet, sys: sys}
}

// Settings 读取邀请奖励规则。
func (s *InviteRewardService) Settings(ctx context.Context) InviteRewardSettings {
	if s == nil || s.sys == nil {
		return InviteRewardSettings{}
	}
	return InviteRewardSettings{
		Enabled:                  s.sys.GetBool(ctx, SettingInviteEnabled, false),
		FreeInitialPoints:        nonNegative(s.sys.GetInt(ctx, SettingBillingFreeInitialPoints, 0)),
		InviteeRegisterPoints:    nonNegative(s.sys.GetInt(ctx, SettingInviteNewUserPoints, 0)),
		InviterRegisterReward:    nonNegative(s.sys.GetInt(ctx, SettingInviteInviterRegister, 0)),
		FirstRechargeReward:      nonNegative(s.sys.GetInt(ctx, SettingInviteFirstRecharge, 0)),
		LifetimeRechargeSharePct: clampPct(s.sys.GetInt(ctx, SettingInviteLifetimeSharePct, 0)),
	}
}

// PublicRules 返回前台公开规则。
func (s *InviteRewardService) PublicRules(ctx context.Context) InvitePublicRules {
	cfg := s.Settings(ctx)
	return InvitePublicRules{
		Enabled:                  cfg.Enabled,
		FreeInitialPoints:        cfg.FreeInitialPoints,
		InviteeRegisterPoints:    cfg.InviteeRegisterPoints,
		InviterRegisterReward:    cfg.InviterRegisterReward,
		FirstRechargeReward:      cfg.FirstRechargeReward,
		LifetimeRechargeSharePct: cfg.LifetimeRechargeSharePct,
	}
}

// GrantRegistrationBonusesTx 发放注册赠点和邀请注册奖励。
func (s *InviteRewardService) GrantRegistrationBonusesTx(ctx context.Context, tx *gorm.DB, userID uint64, inviterID *uint64) error {
	if s == nil || s.wallet == nil {
		return nil
	}
	cfg := s.Settings(ctx)
	if cfg.FreeInitialPoints > 0 {
		if _, err := s.wallet.IncomeTx(ctx, tx, userID, model.BizGift, registerBizID(userID), cfg.FreeInitialPoints, "注册赠送"); err != nil {
			return err
		}
	}
	if !cfg.Enabled || inviterID == nil || *inviterID == 0 || *inviterID == userID {
		return nil
	}
	if cfg.InviteeRegisterPoints > 0 {
		if _, err := s.wallet.IncomeTx(ctx, tx, userID, model.BizInvite, inviteeRegisterBizID(userID), cfg.InviteeRegisterPoints, "使用邀请码注册奖励"); err != nil {
			return err
		}
		if err := s.createRewardIfMissing(ctx, tx, *inviterID, userID, inviteKindInviteeRegister, cfg.InviteeRegisterPoints, nil); err != nil {
			return err
		}
	}
	if cfg.InviterRegisterReward > 0 {
		if _, err := s.wallet.IncomeTx(ctx, tx, *inviterID, model.BizInvite, inviterRegisterBizID(userID), cfg.InviterRegisterReward, "邀请好友注册奖励"); err != nil {
			return err
		}
		if err := s.createRewardIfMissing(ctx, tx, *inviterID, userID, inviteKindInviterRegister, cfg.InviterRegisterReward, nil); err != nil {
			return err
		}
	}
	return nil
}

// GrantRechargeRewardsTx 在充值到账后发放首充奖励和分润。
func (s *InviteRewardService) GrantRechargeRewardsTx(ctx context.Context, tx *gorm.DB, order *model.RechargeRecord, invitee *model.User, creditedPoints int64) error {
	if s == nil || s.wallet == nil || order == nil || invitee == nil || invitee.InviterID == nil || *invitee.InviterID == 0 {
		return nil
	}
	cfg := s.Settings(ctx)
	if !cfg.Enabled || *invitee.InviterID == invitee.ID || creditedPoints <= 0 {
		return nil
	}
	inviterID := *invitee.InviterID
	var priorPaid int64
	if err := tx.WithContext(ctx).Model(&model.RechargeRecord{}).
		Where("user_id = ? AND status = ? AND id <> ?", invitee.ID, model.RechargeStatusPaid, order.ID).
		Count(&priorPaid).Error; err != nil {
		return err
	}
	orderNo := order.OrderNo
	if priorPaid == 0 && cfg.FirstRechargeReward > 0 {
		if _, err := s.wallet.IncomeTx(ctx, tx, inviterID, model.BizInvite, firstRechargeBizID(orderNo), cfg.FirstRechargeReward, "邀请好友首充奖励"); err != nil {
			return err
		}
		if err := s.createRewardIfMissing(ctx, tx, inviterID, invitee.ID, inviteKindFirstRecharge, cfg.FirstRechargeReward, &orderNo); err != nil {
			return err
		}
	}
	share := creditedPoints * cfg.LifetimeRechargeSharePct / 100
	if share > 0 {
		if _, err := s.wallet.IncomeTx(ctx, tx, inviterID, model.BizInvite, rechargeShareBizID(orderNo), share, "邀请好友充值分润"); err != nil {
			return err
		}
		if err := s.createRewardIfMissing(ctx, tx, inviterID, invitee.ID, inviteKindRechargeShare, share, &orderNo); err != nil {
			return err
		}
	}
	return nil
}

func (s *InviteRewardService) createRewardIfMissing(ctx context.Context, tx *gorm.DB, inviterID, inviteeID uint64, kind string, points int64, fromOrder *string) error {
	q := tx.WithContext(ctx).Where("inviter_id = ? AND invitee_id = ? AND kind = ?", inviterID, inviteeID, kind)
	if fromOrder == nil {
		q = q.Where("from_order IS NULL")
	} else {
		q = q.Where("from_order = ?", *fromOrder)
	}
	var existing model.InvitationReward
	if err := q.First(&existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.WithContext(ctx).Create(&model.InvitationReward{
		InviterID: inviterID,
		InviteeID: inviteeID,
		Kind:      kind,
		Points:    points,
		FromOrder: fromOrder,
	}).Error
}

func registerBizID(userID uint64) string        { return "register:" + uintID(userID) }
func inviteeRegisterBizID(userID uint64) string { return "invite:invitee_register:" + uintID(userID) }
func inviterRegisterBizID(userID uint64) string { return "invite:inviter_register:" + uintID(userID) }
func firstRechargeBizID(orderNo string) string  { return "invite:first_recharge:" + orderNo }
func rechargeShareBizID(orderNo string) string  { return "invite:recharge_share:" + orderNo }

func uintID(id uint64) string { return strconv.FormatUint(id, 10) }

func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func clampPct(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
