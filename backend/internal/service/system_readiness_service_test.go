package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/pkg/config"
)

func TestSystemReadinessReportsMissingPaymentSecrets(t *testing.T) {
	svc := NewSystemReadinessService(validReadinessConfig(), testSystemConfig(map[string]string{
		SettingPaymentEnabled:  "true",
		SettingPaymentProvider: `"alipay"`,
	}))

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if resp.Overall != readinessError {
		t.Fatalf("overall = %q, want %q", resp.Overall, readinessError)
	}
	if got := readinessStatus(resp, "payment", "alipay_private_key"); got != readinessError {
		t.Fatalf("payment.alipay_private_key status = %q, want error", got)
	}
	if got := readinessStatus(resp, "payment", "alipay_public_key"); got != readinessError {
		t.Fatalf("payment.alipay_public_key status = %q, want error", got)
	}
	if got := readinessStatus(resp, "payment", "alipay_seller_id"); got != readinessError {
		t.Fatalf("payment.alipay_seller_id status = %q, want error", got)
	}
}

func TestSystemReadinessAcceptsProviderRouteCoverage(t *testing.T) {
	routes := `[
		{"kind":"image","model_code":"*","routes":[{"provider":"gpt","weight":1}]},
		{"kind":"text","model_code":"*","routes":[{"provider":"gpt","weight":1}]},
		{"kind":"video","model_code":"*","routes":[{"provider":"grok","weight":1}]}
	]`
	svc := NewSystemReadinessService(validReadinessConfig(), testSystemConfig(map[string]string{
		SettingProviderRoutes: routes,
	}))

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	for _, key := range []string{"kind_image", "kind_text", "kind_video"} {
		if got := readinessStatus(resp, "provider_routes", key); got != readinessOK {
			t.Fatalf("provider_routes.%s status = %q, want ok", key, got)
		}
	}
}

func TestSystemReadinessAcceptsSMTPFromSystemConfig(t *testing.T) {
	cfg := validReadinessConfig()
	cfg.SMTP = config.SMTP{}
	svc := NewSystemReadinessService(cfg, testSystemConfig(map[string]string{
		SettingSMTPHost:      `"smtp.qiye.aliyun.com"`,
		SettingSMTPPort:      `465`,
		SettingSMTPUsername:  `"sender@example.com"`,
		SettingSMTPPassword:  `"smtp-secret"`,
		SettingSMTPFromEmail: `"sender@example.com"`,
		SettingSMTPFromName:  `"DAPO达波显影"`,
	}))

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	for _, key := range []string{"host", "port", "username", "password", "from_email", "from_name"} {
		check := readinessCheck(resp, "smtp", key)
		if check.Status != readinessOK {
			t.Fatalf("smtp.%s status = %q, want ok", key, check.Status)
		}
		if check.Source != "system_config" {
			t.Fatalf("smtp.%s source = %q, want system_config", key, check.Source)
		}
	}
}

func TestEmailVerificationEffectiveSMTPUsesSystemConfig(t *testing.T) {
	svc := NewEmailVerificationService(nil, nil, config.SMTP{
		Host:        "smtp.config",
		Port:        25,
		Username:    "config-user",
		Password:    "config-pass",
		FromEmail:   "config@example.com",
		FromName:    "Config",
		UseSSL:      true,
		UseStartTLS: true,
	}, testSystemConfig(map[string]string{
		SettingSMTPHost:        `"smtp.system"`,
		SettingSMTPPort:        `465`,
		SettingSMTPUsername:    `"system-user"`,
		SettingSMTPPassword:    `"system-pass"`,
		SettingSMTPFromEmail:   `"system@example.com"`,
		SettingSMTPFromName:    `"System"`,
		SettingSMTPUseSSL:      `false`,
		SettingSMTPUseStartTLS: `false`,
	}))
	t.Setenv("KLEIN_SMTP_USERNAME", "env-user")

	got := svc.effectiveSMTP(context.Background())
	if got.Host != "smtp.system" || got.Port != 465 || got.Username != "env-user" || got.Password != "system-pass" {
		t.Fatalf("effective SMTP basic fields = %#v", got)
	}
	if got.FromEmail != "system@example.com" || got.FromName != "System" {
		t.Fatalf("effective SMTP sender fields = %#v", got)
	}
	if got.UseSSL || got.UseStartTLS {
		t.Fatalf("effective SMTP TLS flags = ssl:%v starttls:%v, want false/false", got.UseSSL, got.UseStartTLS)
	}
}

func validReadinessConfig() *config.Config {
	return &config.Config{
		App:    config.App{Env: "prod"},
		MySQL:  config.MySQL{DSN: "root@tcp(127.0.0.1:3306)/dapo"},
		Redis:  config.Redis{Addr: "127.0.0.1:6379"},
		JWT:    config.JWT{Secret: strings.Repeat("a", 32), RefreshSecret: strings.Repeat("b", 32)},
		AESKey: strings.Repeat("c", 32),
		SMTP: config.SMTP{
			Host:      "smtp.example.com",
			Port:      465,
			Username:  "no-reply@example.com",
			Password:  "smtp-password",
			FromEmail: "no-reply@example.com",
			FromName:  "DAPO",
		},
	}
}

func testSystemConfig(values map[string]string) *SystemConfigService {
	return &SystemConfigService{
		cache:  values,
		loaded: time.Now(),
		ttl:    time.Hour,
	}
}

func readinessStatus(resp *dto.AdminSystemReadinessResp, category, key string) string {
	return readinessCheck(resp, category, key).Status
}

func readinessCheck(resp *dto.AdminSystemReadinessResp, category, key string) dto.AdminSystemReadinessCheck {
	if resp == nil {
		return dto.AdminSystemReadinessCheck{}
	}
	for _, check := range resp.Checks {
		if check.Category == category && check.Key == key {
			return check
		}
	}
	return dto.AdminSystemReadinessCheck{}
}
