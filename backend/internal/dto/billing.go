// Package dto 计费相关 DTO。
package dto

// CDKRedeemReq 兑换 CDK。
type CDKRedeemReq struct {
	Code string `json:"code" binding:"required,min=4,max=32"`
}

type RechargePackageResp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      int64  `json:"amount"` // 分
	Points      int64  `json:"points"`
	BonusPoints int64  `json:"bonus_points"`
	TotalPoints int64  `json:"total_points"`
	Badge       string `json:"badge,omitempty"`
	Remark      string `json:"remark,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

type CreateRechargeOrderReq struct {
	PackageID string `json:"package_id" binding:"required,min=1,max=64"`
	Channel   string `json:"channel"    binding:"omitempty,max=32"`
	PromoCode string `json:"promo_code" binding:"omitempty,max=32"`
}

type RechargeOrderResp struct {
	ID              uint64 `json:"id"`
	OrderNo         string `json:"order_no"`
	Channel         string `json:"channel"`
	Amount          int64  `json:"amount"`
	OriginalAmount  int64  `json:"original_amount,omitempty"`
	DiscountAmount  int64  `json:"discount_amount,omitempty"`
	PromoCode       string `json:"promo_code,omitempty"`
	PromoGiftPoints int64  `json:"promo_gift_points,omitempty"`
	Points          int64  `json:"points"`
	BonusPoints     int64  `json:"bonus_points"`
	TotalPoints     int64  `json:"total_points"`
	Status          int8   `json:"status"`
	QRCode          string `json:"qr_code,omitempty"`
	PaidAt          int64  `json:"paid_at,omitempty"`
	CreatedAt       int64  `json:"created_at"`
}

// CDKBatchListReq 管理后台 CDK 批次列表。
type CDKBatchListReq struct {
	Keyword  string `form:"keyword" binding:"omitempty,max=128"`
	Status   *int   `form:"status" binding:"omitempty,oneof=0 1 2"`
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=200"`
}

// CDKBatchResp 管理后台 CDK 批次响应。
type CDKBatchResp struct {
	ID           uint64 `json:"id"`
	BatchNo      string `json:"batch_no"`
	Name         string `json:"name"`
	RewardType   string `json:"reward_type"`
	Points       int64  `json:"points"`
	TotalQty     int    `json:"total_qty"`
	UsedQty      int    `json:"used_qty"`
	PerUserLimit int    `json:"per_user_limit"`
	ExpireAt     int64  `json:"expire_at,omitempty"`
	Status       int8   `json:"status"`
	CreatedBy    uint64 `json:"created_by,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// CDKCodeListReq 管理后台 CDK 明细列表。
type CDKCodeListReq struct {
	Status   *int `form:"status" binding:"omitempty,oneof=0 1 2"`
	Page     int  `form:"page" binding:"omitempty,min=1"`
	PageSize int  `form:"page_size" binding:"omitempty,min=1,max=5000"`
}

// CDKCodeResp 管理后台 CDK 明细响应。
type CDKCodeResp struct {
	ID        uint64  `json:"id"`
	BatchID   uint64  `json:"batch_id"`
	Code      string  `json:"code"`
	Status    int8    `json:"status"`
	UsedBy    *uint64 `json:"used_by,omitempty"`
	UsedAt    int64   `json:"used_at,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

// CDKBatchCreateResp 管理后台创建 CDK 批次响应。
type CDKBatchCreateResp struct {
	ID       uint64 `json:"id"`
	BatchNo  string `json:"batch_no"`
	TotalQty int    `json:"total_qty"`
}

// WalletLogResp 钱包流水响应（一行）。
type WalletLogResp struct {
	ID           uint64 `json:"id"`
	Direction    int8   `json:"direction"`
	BizType      string `json:"biz_type"`
	BizID        string `json:"biz_id"`
	Points       int64  `json:"points"`
	PointsBefore int64  `json:"points_before"`
	PointsAfter  int64  `json:"points_after"`
	Remark       string `json:"remark,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// CDKBatchCreateReq 管理后台创建 CDK 批次。
type CDKBatchCreateReq struct {
	BatchNo      string `json:"batch_no"       binding:"required,min=4,max=32"`
	Name         string `json:"name"           binding:"required,min=1,max=64"`
	Points       int64  `json:"points"         binding:"required,min=1"`
	Qty          int    `json:"qty"            binding:"required,min=1,max=100000"`
	PerUserLimit int    `json:"per_user_limit" binding:"omitempty,min=0"`
	ExpireAt     int64  `json:"expire_at"      binding:"omitempty,min=0"` // unix
}
