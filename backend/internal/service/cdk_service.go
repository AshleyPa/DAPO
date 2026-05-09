// Package service 兑换码（CDK） / 优惠码（Promo） 服务。
//
// 仅支持 reward_type=points 的最小实现：reward_value JSON 形如 {"points": 10000}（10000 = 100 点）。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/errcode"
)

// CDKService 兑换码服务。
type CDKService struct {
	db      *gorm.DB
	billing *BillingService
}

// NewCDKService 构造。
func NewCDKService(db *gorm.DB, b *BillingService) *CDKService {
	return &CDKService{db: db, billing: b}
}

// ListBatches 管理后台 CDK 批次列表。
func (s *CDKService) ListBatches(ctx context.Context, req *dto.CDKBatchListReq) ([]*dto.CDKBatchResp, int64, error) {
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	q := s.db.WithContext(ctx).Model(&model.RedeemCodeBatch{})
	if req.Status != nil {
		q = q.Where("status = ?", *req.Status)
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("CAST(id AS CHAR) = ? OR batch_no LIKE ? OR name LIKE ?", kw, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}

	var rows []*model.RedeemCodeBatch
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}

	out := make([]*dto.CDKBatchResp, 0, len(rows))
	for _, row := range rows {
		points, err := parsePointsReward(row.RewardType, row.RewardValue)
		if err != nil {
			return nil, 0, errcode.Internal.Wrap(err)
		}
		resp := &dto.CDKBatchResp{
			ID:           row.ID,
			BatchNo:      row.BatchNo,
			Name:         row.Name,
			RewardType:   row.RewardType,
			Points:       points,
			TotalQty:     row.TotalQty,
			UsedQty:      row.UsedQty,
			PerUserLimit: row.PerUserLimit,
			Status:       row.Status,
			CreatedAt:    row.CreatedAt.Unix(),
		}
		if row.ExpireAt != nil {
			resp.ExpireAt = row.ExpireAt.Unix()
		}
		if row.CreatedBy != nil {
			resp.CreatedBy = *row.CreatedBy
		}
		out = append(out, resp)
	}
	return out, total, nil
}

// ListCodes 管理后台 CDK 明细列表。
func (s *CDKService) ListCodes(ctx context.Context, batchID uint64, req *dto.CDKCodeListReq) ([]*dto.CDKCodeResp, int64, error) {
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 5000 {
		pageSize = 200
	}

	q := s.db.WithContext(ctx).Model(&model.RedeemCode{}).Where("batch_id = ?", batchID)
	if req.Status != nil {
		q = q.Where("status = ?", *req.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}

	var rows []*model.RedeemCode
	if err := q.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}

	out := make([]*dto.CDKCodeResp, 0, len(rows))
	for _, row := range rows {
		resp := &dto.CDKCodeResp{
			ID:        row.ID,
			BatchID:   row.BatchID,
			Code:      row.Code,
			Status:    row.Status,
			UsedBy:    row.UsedBy,
			CreatedAt: row.CreatedAt.Unix(),
		}
		if row.UsedAt != nil {
			resp.UsedAt = row.UsedAt.Unix()
		}
		out = append(out, resp)
	}
	return out, total, nil
}

// Redeem 用户兑换 CDK。
func (s *CDKService) Redeem(ctx context.Context, userID uint64, code string) (int64, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return 0, errcode.InvalidParam
	}

	var grantedPoints int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.RedeemCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code = ?", code).First(&c).Error; err != nil {
			return errcode.CDKInvalid
		}
		var batch model.RedeemCodeBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", c.BatchID).First(&batch).Error; err != nil {
			return errcode.CDKInvalid
		}
		if c.Status != model.CDKStatusUnused {
			if c.Status == model.CDKStatusUsed && c.UsedBy != nil && *c.UsedBy == userID {
				points, err := parsePointsReward(batch.RewardType, batch.RewardValue)
				if err != nil {
					return errcode.Internal.Wrap(err)
				}
				grantedPoints = points
				return nil
			}
			return errcode.CDKUsed
		}
		now := time.Now().UTC()
		if batch.Status != model.PromoStatusEnabled {
			return errcode.CDKInvalid
		}
		if batch.ExpireAt != nil && now.After(*batch.ExpireAt) {
			return errcode.CDKInvalid.WithMsg("兑换码已过期")
		}

		// per_user_limit：同一用户在该批次最多兑换 N 次
		if batch.PerUserLimit > 0 {
			var used int64
			if err := tx.Model(&model.RedeemCode{}).
				Where("batch_id = ? AND used_by = ?", batch.ID, userID).
				Count(&used).Error; err != nil {
				return errcode.DBError.Wrap(err)
			}
			if int(used) >= batch.PerUserLimit {
				return errcode.CDKUsed.WithMsg("已达每用户兑换上限")
			}
		}

		// 解析 reward_value
		points, err := parsePointsReward(batch.RewardType, batch.RewardValue)
		if err != nil {
			return errcode.Internal.Wrap(err)
		}
		if points <= 0 {
			return errcode.Internal.WithMsg("invalid reward")
		}

		// 标记已使用
		res := tx.Model(&model.RedeemCode{}).
			Where("id = ? AND status = ?", c.ID, model.CDKStatusUnused).
			Updates(map[string]any{
				"status":  model.CDKStatusUsed,
				"used_by": userID,
				"used_at": now,
			})
		if res.Error != nil {
			return errcode.DBError.Wrap(res.Error)
		}
		if res.RowsAffected != 1 {
			return errcode.CDKUsed
		}
		// 更新 batch.used_qty
		if err := tx.Model(&model.RedeemCodeBatch{}).
			Where("id = ?", batch.ID).
			UpdateColumn("used_qty", gorm.Expr("used_qty + 1")).Error; err != nil {
			return errcode.DBError.Wrap(err)
		}
		grantedPoints = points
		bizID := fmt.Sprintf("cdk:%s", code)
		return s.billing.GrantPointsTx(ctx, tx, userID, model.BizCDK, bizID, grantedPoints, "redeem code")
	})
	if err != nil {
		return 0, err
	}
	return grantedPoints, nil
}

// GenerateBatch 管理后台生成 CDK 批次。
func (s *CDKService) GenerateBatch(ctx context.Context, adminID uint64, batchNo, name string, points int64, qty, perUserLimit int, expireAt *time.Time) (*model.RedeemCodeBatch, error) {
	if points <= 0 || qty <= 0 || qty > 100000 {
		return nil, errcode.InvalidParam
	}
	rewardJSON, _ := json.Marshal(map[string]any{"points": points})

	batch := &model.RedeemCodeBatch{
		BatchNo:      batchNo,
		Name:         name,
		RewardType:   "points",
		RewardValue:  string(rewardJSON),
		TotalQty:     qty,
		PerUserLimit: perUserLimit,
		ExpireAt:     expireAt,
		Status:       model.PromoStatusEnabled,
		CreatedBy:    &adminID,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		codes := make([]*model.RedeemCode, 0, qty)
		for i := 0; i < qty; i++ {
			c, _ := generateCDKCode()
			codes = append(codes, &model.RedeemCode{BatchID: batch.ID, Code: c})
		}
		return tx.CreateInBatches(codes, 500).Error
	})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return batch, nil
}

// === helpers ===

func parsePointsReward(rewardType, value string) (int64, error) {
	if rewardType != "points" {
		return 0, fmt.Errorf("unsupported reward_type: %s", rewardType)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return 0, err
	}
	switch p := v["points"].(type) {
	case float64:
		return int64(p), nil
	case int64:
		return p, nil
	}
	return 0, fmt.Errorf("invalid points reward")
}

// generateCDKCode 生成 16 位 base32（避开易混字符）。
func generateCDKCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b, err := crypto.RandomBytes(16)
	if err != nil {
		return "", err
	}
	out := make([]byte, 16)
	for i := 0; i < 16; i++ {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}
