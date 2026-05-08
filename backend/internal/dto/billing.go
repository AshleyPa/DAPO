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
}

type RechargeOrderResp struct {
	ID          uint64 `json:"id"`
	OrderNo     string `json:"order_no"`
	Channel     string `json:"channel"`
	Amount      int64  `json:"amount"`
	Points      int64  `json:"points"`
	BonusPoints int64  `json:"bonus_points"`
	TotalPoints int64  `json:"total_points"`
	Status      int8   `json:"status"`
	QRCode      string `json:"qr_code,omitempty"`
	PaidAt      int64  `json:"paid_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
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
