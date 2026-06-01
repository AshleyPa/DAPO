package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
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

func TestSystemReadinessReportsModelGatewayCoverageAndDemotesLegacyRoutes(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{ModelCode: "mimo-v2.5-pro", EntryKind: "text", Status: status, Visible: visible},
				{ModelCode: "gpt-image-2", EntryKind: "image", Status: status, Visible: visible},
				{ModelCode: "sora2", EntryKind: "video", Status: status, Visible: visible},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{items: []*model.ModelSourceMapping{
				{ModelCode: "mimo-v2.5-pro", SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "mimo-official", Status: model.ModelSourceStatusEnabled},
				{ModelCode: "gpt-image-2", SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "image-official", Status: model.ModelSourceStatusEnabled},
				{ModelCode: "sora2", SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "video-official", Status: model.ModelSourceStatusEnabled},
			}},
			APIRepo: fakeReadinessAPIChannelRepo{
				channels: map[string]*model.APIChannel{
					"mimo-official":  healthyReadinessAPIChannel(1, "mimo-official"),
					"image-official": healthyReadinessAPIChannel(2, "image-official"),
					"video-official": healthyReadinessAPIChannel(3, "video-official"),
				},
				keys: map[uint64][]*model.APIChannelKey{
					1: {{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
					2: {{ID: 21, ChannelID: 2, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
					3: {{ID: 31, ChannelID: 3, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
				},
			},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	for _, key := range []string{"catalog", "sources", "api_channel_sources", "source_conflicts", "source_duplicates", "api_channel_health", "api_channel_credentials", "api_channel_key_pool", "kind_image", "kind_text", "kind_video"} {
		if got := readinessStatus(resp, "model_gateway", key); got != readinessOK {
			t.Fatalf("model_gateway.%s status = %q, want ok", key, got)
		}
	}
	providerConfigured := readinessCheck(resp, "provider_routes", "configured")
	if providerConfigured.Status != readinessOK {
		t.Fatalf("provider_routes.configured status = %q, want ok", providerConfigured.Status)
	}
	if !strings.Contains(providerConfigured.Message, "Model Gateway") {
		t.Fatalf("expected provider_routes message to mention Model Gateway, got %q", providerConfigured.Message)
	}
}

func TestSystemReadinessWarnsWhenModelGatewaySourceConflictExists(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{
					ModelCode:            "mimo-v2.5-pro",
					EntryKind:            "text",
					ProviderHint:         "mimo",
					UpstreamDefaultModel: "mimo-v2.5-pro",
					Status:               status,
					Visible:              visible,
				},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{items: []*model.ModelSourceMapping{
				{
					ModelCode:     "mimo-v2.5-pro",
					SourceType:    model.ModelSourceTypeAccountPool,
					SourceCode:    model.ProviderGPT,
					UpstreamModel: "mimo-v2.5-pro",
					Status:        model.ModelSourceStatusEnabled,
				},
			}},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	check := readinessCheck(resp, "model_gateway", "source_conflicts")
	if check.Status != readinessWarn {
		t.Fatalf("model_gateway.source_conflicts status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Message, "mimo-v2.5-pro") {
		t.Fatalf("expected source conflict message to mention model, got %q", check.Message)
	}
}

func TestSystemReadinessWarnsWhenModelGatewaySourceDuplicateExists(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{
					ModelCode:            "mimo-v2.5-pro",
					EntryKind:            "text",
					ProviderHint:         "mimo",
					UpstreamDefaultModel: "mimo-v2.5-pro",
					Status:               status,
					Visible:              visible,
				},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{items: []*model.ModelSourceMapping{
				{
					ID:         101,
					ModelCode:  "mimo-v2.5-pro",
					SourceType: model.ModelSourceTypeAPIChannel,
					SourceCode: "mimo-official",
					Adapter:    model.APIChannelAdapterOpenAIChat,
					AuthType:   model.AuthTypeAPIKey,
					Status:     model.ModelSourceStatusEnabled,
				},
				{
					ID:            102,
					ModelCode:     "mimo-v2.5-pro",
					SourceType:    model.ModelSourceTypeAPIChannel,
					SourceCode:    "mimo-official",
					UpstreamModel: "mimo-v2.5-pro",
					Adapter:       model.APIChannelAdapterOpenAIChat,
					AuthType:      model.AuthTypeAPIKey,
					Status:        model.ModelSourceStatusDisabled,
				},
			}},
			APIRepo: fakeReadinessAPIChannelRepo{
				channels: map[string]*model.APIChannel{
					"mimo-official": healthyReadinessAPIChannel(1, "mimo-official"),
				},
				keys: map[uint64][]*model.APIChannelKey{
					1: {{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
				},
			},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	check := readinessCheck(resp, "model_gateway", "source_duplicates")
	if check.Status != readinessWarn {
		t.Fatalf("model_gateway.source_duplicates status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Message, "ID 101 与 102") {
		t.Fatalf("expected source duplicate message to mention duplicate IDs, got %q", check.Message)
	}
}

func TestSystemReadinessWarnsWhenAPIChannelSourceIsNotOperational(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	failedAt := time.Now()
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{ModelCode: "mimo-v2.5-pro", EntryKind: "text", Status: status, Visible: visible},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{items: []*model.ModelSourceMapping{
				{ModelCode: "mimo-v2.5-pro", SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "mimo-official", Status: model.ModelSourceStatusEnabled},
			}},
			APIRepo: fakeReadinessAPIChannelRepo{
				channels: map[string]*model.APIChannel{
					"mimo-official": {
						ID:             1,
						Code:           "mimo-official",
						Status:         model.APIChannelStatusEnabled,
						LastTestAt:     &failedAt,
						LastTestStatus: 2,
					},
				},
				keys: map[uint64][]*model.APIChannelKey{
					1: {{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled}},
				},
			},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got := readinessStatus(resp, "model_gateway", "api_channel_health"); got != readinessWarn {
		t.Fatalf("model_gateway.api_channel_health status = %q, want warn", got)
	}
	if got := readinessStatus(resp, "model_gateway", "api_channel_credentials"); got != readinessWarn {
		t.Fatalf("model_gateway.api_channel_credentials status = %q, want warn", got)
	}
	if got := readinessStatus(resp, "model_gateway", "api_channel_key_pool"); got != readinessWarn {
		t.Fatalf("model_gateway.api_channel_key_pool status = %q, want warn", got)
	}
}

func TestSystemReadinessWarnsWhenAPIChannelStillUsesLegacyCredential(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	ch := healthyReadinessAPIChannel(1, "mimo-official")
	ch.CredentialEnc = []byte("legacy")
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{ModelCode: "mimo-v2.5-pro", EntryKind: "text", Status: status, Visible: visible},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{items: []*model.ModelSourceMapping{
				{ModelCode: "mimo-v2.5-pro", SourceType: model.ModelSourceTypeAPIChannel, SourceCode: "mimo-official", Status: model.ModelSourceStatusEnabled},
			}},
			APIRepo: fakeReadinessAPIChannelRepo{
				channels: map[string]*model.APIChannel{"mimo-official": ch},
			},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got := readinessStatus(resp, "model_gateway", "api_channel_credentials"); got != readinessOK {
		t.Fatalf("model_gateway.api_channel_credentials status = %q, want ok", got)
	}
	if got := readinessStatus(resp, "model_gateway", "api_channel_key_pool"); got != readinessWarn {
		t.Fatalf("model_gateway.api_channel_key_pool status = %q, want warn", got)
	}
}

func TestSystemReadinessWarnsWhenModelGatewayModelHasNoSource(t *testing.T) {
	status := int8(model.ModelCatalogStatusEnabled)
	visible := int8(1)
	svc := NewSystemReadinessService(
		validReadinessConfig(),
		testSystemConfig(map[string]string{}),
		SystemReadinessModelGatewayDeps{
			ModelRepo: fakeReadinessModelCatalogRepo{items: []*model.ModelCatalog{
				{ModelCode: "mimo-v2.5-pro", EntryKind: "text", Status: status, Visible: visible},
			}},
			SourceRepo: fakeReadinessModelSourceRepo{},
		},
	)

	resp, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got := readinessStatus(resp, "model_gateway", "sources"); got != readinessWarn {
		t.Fatalf("model_gateway.sources status = %q, want warn", got)
	}
	if got := readinessStatus(resp, "model_gateway", "kind_text"); got != readinessWarn {
		t.Fatalf("model_gateway.kind_text status = %q, want warn", got)
	}
	if got := readinessStatus(resp, "provider_routes", "configured"); got != readinessWarn {
		t.Fatalf("provider_routes.configured status = %q, want warn when Model Gateway has no source mapping", got)
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

type fakeReadinessModelCatalogRepo struct {
	items []*model.ModelCatalog
	err   error
}

func (f fakeReadinessModelCatalogRepo) List(ctx context.Context, filter repo.ModelCatalogListFilter) ([]*model.ModelCatalog, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	filtered := make([]*model.ModelCatalog, 0, len(f.items))
	for _, item := range f.items {
		if item == nil {
			continue
		}
		if filter.EntryKind != "" && item.EntryKind != filter.EntryKind {
			continue
		}
		if filter.Status != nil && item.Status != *filter.Status {
			continue
		}
		if filter.Visible != nil && item.Visible != *filter.Visible {
			continue
		}
		filtered = append(filtered, item)
	}
	return pageReadinessItems(filtered, filter.Page, filter.PageSize), int64(len(filtered)), nil
}

type fakeReadinessModelSourceRepo struct {
	items []*model.ModelSourceMapping
	err   error
}

func (f fakeReadinessModelSourceRepo) List(ctx context.Context, filter repo.ModelSourceListFilter) ([]*model.ModelSourceMapping, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	filtered := make([]*model.ModelSourceMapping, 0, len(f.items))
	for _, item := range f.items {
		if item == nil {
			continue
		}
		if filter.ModelCode != "" && item.ModelCode != filter.ModelCode {
			continue
		}
		if filter.SourceType != "" && item.SourceType != filter.SourceType {
			continue
		}
		if filter.Status != nil && item.Status != *filter.Status {
			continue
		}
		filtered = append(filtered, item)
	}
	return pageReadinessItems(filtered, filter.Page, filter.PageSize), int64(len(filtered)), nil
}

type fakeReadinessAPIChannelRepo struct {
	channels map[string]*model.APIChannel
	keys     map[uint64][]*model.APIChannelKey
	err      error
	keyErr   error
}

func (f fakeReadinessAPIChannelRepo) GetByCode(ctx context.Context, code string) (*model.APIChannel, error) {
	if f.err != nil {
		return nil, f.err
	}
	item := f.channels[code]
	if item == nil {
		return nil, repo.ErrNotFound
	}
	return item, nil
}

func (f fakeReadinessAPIChannelRepo) ListKeys(ctx context.Context, filter repo.APIChannelKeyListFilter) ([]*model.APIChannelKey, int64, error) {
	if f.keyErr != nil {
		return nil, 0, f.keyErr
	}
	filtered := make([]*model.APIChannelKey, 0, len(f.keys[filter.ChannelID]))
	for _, item := range f.keys[filter.ChannelID] {
		if item == nil {
			continue
		}
		if filter.Status != nil && item.Status != *filter.Status {
			continue
		}
		filtered = append(filtered, item)
	}
	return pageReadinessItems(filtered, filter.Page, filter.PageSize), int64(len(filtered)), nil
}

func healthyReadinessAPIChannel(id uint64, code string) *model.APIChannel {
	now := time.Now()
	return &model.APIChannel{
		ID:             id,
		Code:           code,
		Status:         model.APIChannelStatusEnabled,
		LastTestAt:     &now,
		LastTestStatus: 1,
	}
}

func pageReadinessItems[T any](items []T, page, pageSize int) []T {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = len(items)
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
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
