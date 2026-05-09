package service

import (
	"strings"
	"testing"

	"github.com/kleinai/backend/pkg/alipay"
)

func TestValidateAlipayNotifyIdentityRequiresConfiguredSellerID(t *testing.T) {
	client, err := alipay.NewClient(alipay.Config{AppID: "app_123", SellerID: "seller_123"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := validateAlipayNotifyIdentity(client, &alipay.NotifyPayload{AppID: "app_123", SellerID: "seller_123"}); err != nil {
		t.Fatalf("validateAlipayNotifyIdentity() error = %v", err)
	}
	if err := validateAlipayNotifyIdentity(client, &alipay.NotifyPayload{AppID: "app_123"}); err == nil || !strings.Contains(err.Error(), "seller_id missing") {
		t.Fatalf("missing seller_id error = %v", err)
	}
	if err := validateAlipayNotifyIdentity(client, &alipay.NotifyPayload{AppID: "app_123", SellerID: "seller_456"}); err == nil || !strings.Contains(err.Error(), "seller_id mismatch") {
		t.Fatalf("mismatched seller_id error = %v", err)
	}
}

func TestValidateAlipayTradeNoRejectsMismatch(t *testing.T) {
	stored := "T1"
	if err := validateAlipayTradeNo(&stored, "T1"); err != nil {
		t.Fatalf("validateAlipayTradeNo() error = %v", err)
	}
	if err := validateAlipayTradeNo(&stored, "T2"); err == nil || !strings.Contains(err.Error(), "trade_no mismatch") {
		t.Fatalf("mismatch error = %v", err)
	}
	if err := validateAlipayTradeNo(&stored, ""); err != nil {
		t.Fatalf("empty notify trade_no should be allowed, got %v", err)
	}
}
