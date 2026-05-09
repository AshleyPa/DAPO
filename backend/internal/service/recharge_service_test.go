package service

import (
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
)

func TestIsExpiredPendingRecharge(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  *model.RechargeRecord
		want bool
	}{
		{
			name: "pending order older than ttl expires",
			row:  &model.RechargeRecord{Status: model.RechargeStatusPending, CreatedAt: now.Add(-rechargeOrderTTL - time.Second)},
			want: true,
		},
		{
			name: "pending order inside ttl stays pending",
			row:  &model.RechargeRecord{Status: model.RechargeStatusPending, CreatedAt: now.Add(-rechargeOrderTTL + time.Second)},
			want: false,
		},
		{
			name: "paid order never expires locally",
			row:  &model.RechargeRecord{Status: model.RechargeStatusPaid, CreatedAt: now.Add(-24 * time.Hour)},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExpiredPendingRecharge(tc.row, now); got != tc.want {
				t.Fatalf("isExpiredPendingRecharge() = %v, want %v", got, tc.want)
			}
		})
	}
}
