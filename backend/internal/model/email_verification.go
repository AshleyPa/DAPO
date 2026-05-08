package model

import "time"

const (
	EmailVerificationSceneRegister      = "register"
	EmailVerificationSceneResetPassword = "reset_password"

	EmailVerificationStatusPending int8 = 0
	EmailVerificationStatusUsed    int8 = 1
	EmailVerificationStatusExpired int8 = 2
)

type EmailVerificationCode struct {
	ID        uint64     `gorm:"primaryKey;column:id" json:"id"`
	Email     string     `gorm:"column:email;size:128;not null;index:idx_email_scene_status,priority:1" json:"email"`
	Scene     string     `gorm:"column:scene;size:32;not null;index:idx_email_scene_status,priority:2" json:"scene"`
	CodeHash  string     `gorm:"column:code_hash;size:64;not null" json:"-"`
	Salt      string     `gorm:"column:salt;size:32;not null" json:"-"`
	Status    int8       `gorm:"column:status;not null;default:0;index:idx_email_scene_status,priority:3" json:"status"`
	SendIP    *string    `gorm:"column:send_ip;size:45" json:"-"`
	Attempts  int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null;index" json:"expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at" json:"used_at,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (EmailVerificationCode) TableName() string { return "email_verification_code" }
