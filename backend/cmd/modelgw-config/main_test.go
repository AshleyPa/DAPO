package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
)

func TestPostGenerationProofCommandIsSanitized(t *testing.T) {
	cmd := postGenerationProofCommand(providerSeed{
		APIKeyEnv:    "DAPO_MIMO_API_KEY",
		ChannelCode:  "mimo-official",
		PublicModel:  "mimo-v2.5-pro",
		EntryKind:    "text",
		ProviderName: "mimo",
		PricingMode:  model.ModelCatalogPricingToken,
	})
	required := []string{
		"go run ./cmd/modelgw-smoke",
		"--model 'mimo-v2.5-pro'",
		"--entry-kind 'text'",
		"--api-channel 'mimo-official'",
		"--task-id '<task_id>'",
		"--require-openai-auth",
		"--require-post-generation-proof",
		"--pricing-mode 'token'",
	}
	for _, item := range required {
		if !strings.Contains(cmd, item) {
			t.Fatalf("expected command to contain %q, got %q", item, cmd)
		}
	}
	forbidden := []string{"DAPO_MIMO_API_KEY", "api_key", "credential", "access_token", "refresh_token", "secret"}
	for _, item := range forbidden {
		if strings.Contains(strings.ToLower(cmd), strings.ToLower(item)) {
			t.Fatalf("expected command not to contain sensitive marker %q, got %q", item, cmd)
		}
	}
}

func TestPreGenerationSmokeCommandIsCompleteAndSanitized(t *testing.T) {
	cmd := preGenerationSmokeCommand(providerSeed{
		APIKeyEnv:    "DAPO_MIMO_API_KEY",
		ChannelCode:  "mimo-official",
		PublicModel:  "mimo-v2.5-pro",
		EntryKind:    "text",
		ProviderName: "mimo",
		PricingMode:  model.ModelCatalogPricingToken,
	})
	required := []string{
		"go run ./cmd/modelgw-smoke",
		"--model 'mimo-v2.5-pro'",
		"--entry-kind 'text'",
		"--api-channel 'mimo-official'",
		"--require-openai-auth",
		"--require-admin",
		"--require-model",
		"--require-catalog-model",
		"--require-parameter-schema",
		"--require-pricing-mode",
		"--pricing-mode 'token'",
		"--require-api-channel",
		"--require-api-channel-health",
		"--require-key-pool",
		"--forbid-legacy-channel-key",
		"--require-source-mapping",
		"--require-no-source-conflicts",
		"--require-route-channel",
		"--forbid-account-pool-route",
	}
	for _, item := range required {
		if !strings.Contains(cmd, item) {
			t.Fatalf("expected command to contain %q, got %q", item, cmd)
		}
	}
	forbidden := []string{"DAPO_MIMO_API_KEY", "api_key", "credential", "access_token", "refresh_token", "secret"}
	for _, item := range forbidden {
		if strings.Contains(strings.ToLower(cmd), strings.ToLower(item)) {
			t.Fatalf("expected command not to contain sensitive marker %q, got %q", item, cmd)
		}
	}
}

func TestPreLaunchEvidenceTemplateCommandIsSanitized(t *testing.T) {
	cmd := preLaunchEvidenceTemplateCommand(providerSeed{
		APIKeyEnv:    "DAPO_MIMO_API_KEY",
		ChannelCode:  "mimo-official",
		PublicModel:  "mimo-v2.5-pro",
		EntryKind:    "text",
		ProviderName: "mimo",
		PricingMode:  model.ModelCatalogPricingChar,
	})
	required := []string{
		"go run ./cmd/modelgw-smoke",
		"--evidence-template",
		"--target-env '<target_env>'",
		"--model 'mimo-v2.5-pro'",
		"--entry-kind 'text'",
		"--api-channel 'mimo-official'",
		"--task-id '<task_id>'",
		"--pricing-mode 'char'",
	}
	for _, item := range required {
		if !strings.Contains(cmd, item) {
			t.Fatalf("expected command to contain %q, got %q", item, cmd)
		}
	}
	forbidden := []string{"DAPO_MIMO_API_KEY", "api_key", "credential", "access_token", "refresh_token", "secret"}
	for _, item := range forbidden {
		if strings.Contains(strings.ToLower(cmd), strings.ToLower(item)) {
			t.Fatalf("expected command not to contain sensitive marker %q, got %q", item, cmd)
		}
	}
}

func TestPrintPostConfigureNextStepsUsesProofCommand(t *testing.T) {
	var buf bytes.Buffer
	err := printPostConfigureNextSteps(&buf, []providerSeed{{
		ChannelCode: "mimo-official",
		PublicModel: "mimo-v2.5-pro",
		EntryKind:   "text",
		PricingMode: model.ModelCatalogPricingToken,
	}})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "recommended pre-launch evidence template:") {
		t.Fatalf("expected evidence template heading, got %q", out)
	}
	if !strings.Contains(out, "--evidence-template") {
		t.Fatalf("expected evidence template command, got %q", out)
	}
	if !strings.Contains(out, "recommended pre-generation smoke:") {
		t.Fatalf("expected pre-generation heading, got %q", out)
	}
	if !strings.Contains(out, "--require-route-channel") || !strings.Contains(out, "--forbid-account-pool-route") {
		t.Fatalf("expected pre-generation route gates, got %q", out)
	}
	if !strings.Contains(out, "recommended post-generation smoke:") {
		t.Fatalf("expected next-step heading, got %q", out)
	}
	if !strings.Contains(out, "--require-post-generation-proof") {
		t.Fatalf("expected post-generation proof command, got %q", out)
	}
	if !strings.Contains(out, "--task-id '<task_id>'") {
		t.Fatalf("expected task-id handoff hint, got %q", out)
	}
	if !strings.Contains(out, "--pricing-mode 'token'") {
		t.Fatalf("expected catalog-rooted pricing mode gate, got %q", out)
	}
	forbidden := []string{"DAPO_MIMO_API_KEY", "api_key", "credential", "access_token", "refresh_token", "secret"}
	for _, item := range forbidden {
		if strings.Contains(strings.ToLower(out), strings.ToLower(item)) {
			t.Fatalf("expected next steps not to contain sensitive marker %q, got %q", item, out)
		}
	}
}

func TestEnsureWriteConfirmedRequiresExplicitFlag(t *testing.T) {
	err := ensureWriteConfirmed(commandOptions{})
	if err == nil {
		t.Fatal("expected missing --confirm-write to fail")
	}
	if !strings.Contains(err.Error(), "--confirm-write") || !strings.Contains(err.Error(), "--schema-check") {
		t.Fatalf("unexpected confirmation error: %v", err)
	}
	if err := ensureWriteConfirmed(commandOptions{ConfirmWrite: true}); err != nil {
		t.Fatalf("expected confirm-write to pass, got %v", err)
	}
}

func TestEnsureWriteConfirmedBeforeBootstrapAllowsReadOnlyModes(t *testing.T) {
	if err := ensureWriteConfirmedBeforeBootstrap(commandOptions{AuditOnly: true}); err != nil {
		t.Fatalf("audit-only should not require write confirmation: %v", err)
	}
	if err := ensureWriteConfirmedBeforeBootstrap(commandOptions{SchemaCheck: true}); err != nil {
		t.Fatalf("schema-check should not require write confirmation: %v", err)
	}
	if err := ensureWriteConfirmedBeforeBootstrap(commandOptions{}); err == nil || !strings.Contains(err.Error(), "--confirm-write") {
		t.Fatalf("write path should fail before bootstrap without --confirm-write, got %v", err)
	}
}

func TestBuildMigrationInventoryRequiresModelGatewayMigrations(t *testing.T) {
	dir := t.TempDir()
	for _, name := range modelGatewayRequiredMigrationFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "20260427130001_init_user.sql"), []byte("-- init\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := buildMigrationInventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.OK || len(inv.RequiredMissing) != 0 {
		t.Fatalf("expected inventory ok, got %#v", inv)
	}
	if inv.Total != len(modelGatewayRequiredMigrationFiles)+1 {
		t.Fatalf("unexpected migration total: %d", inv.Total)
	}
	if len(inv.RequiredPresent) != len(modelGatewayRequiredMigrationFiles) {
		t.Fatalf("unexpected required present list: %#v", inv.RequiredPresent)
	}
	for _, item := range inv.Migrations {
		if item.File == "" || item.SHA256 == "" || strings.Contains(item.SHA256, "secret") {
			t.Fatalf("unexpected inventory item: %#v", item)
		}
	}
}

func TestPrintMigrationInventoryFailsWhenRequiredMigrationMissing(t *testing.T) {
	dir := t.TempDir()
	for _, name := range modelGatewayRequiredMigrationFiles[:len(modelGatewayRequiredMigrationFiles)-1] {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	err := printMigrationInventory(&buf, dir)
	if err == nil {
		t.Fatal("expected missing migration inventory to fail")
	}
	if !strings.Contains(err.Error(), modelGatewayRequiredMigrationFiles[len(modelGatewayRequiredMigrationFiles)-1]) {
		t.Fatalf("missing migration name not reported: %v", err)
	}
	if !strings.Contains(buf.String(), `"ok": false`) || !strings.Contains(buf.String(), `"required_missing"`) {
		t.Fatalf("expected failing inventory JSON, got %s", buf.String())
	}
}

func TestDBTargetCheckAllowsDryRunNamedDatabase(t *testing.T) {
	report := checkDBTargetSafety("staging", "dapo:super-secret-password@tcp(127.0.0.1:3306)/dapo_modelgw_dryrun_20260531?parseTime=true")
	if !report.OK || !report.MigrationDryRunAllowed || report.RiskLevel != "low" {
		t.Fatalf("expected dryrun target to pass: %#v", report)
	}
	if !report.DSNPresent || report.User != "dapo" || report.Host != "127.0.0.1" || report.Port != "3306" || report.Address != "127.0.0.1:3306" || report.Database != "dapo_modelgw_dryrun_20260531" {
		t.Fatalf("unexpected parsed target: %#v", report)
	}
	if !strings.Contains(report.SanitizedDSN, "dapo:[redacted]@tcp") || !strings.Contains(report.SanitizedDSN, "?[params]") {
		t.Fatalf("expected sanitized dsn with redacted password and params, got %q", report.SanitizedDSN)
	}
	if strings.Contains(report.SanitizedDSN, "super-secret-password") {
		t.Fatalf("sanitized dsn leaked password: %q", report.SanitizedDSN)
	}
	if len(report.RequiredNameMarkers) == 0 || len(report.ForbiddenNameMarkers) == 0 {
		t.Fatalf("expected marker policy in report: %#v", report)
	}
}

func TestDBTargetCheckRejectsProductionLikeTarget(t *testing.T) {
	report := checkDBTargetSafety("staging", "dapo:secret@tcp(prod-db.example.com:3306)/dapo_prod?parseTime=true")
	if report.OK || report.MigrationDryRunAllowed || report.RiskLevel != "high" {
		t.Fatalf("expected production-like target to fail: %#v", report)
	}
	joined := strings.Join(report.Reasons, " ")
	if !strings.Contains(joined, "production-like marker") {
		t.Fatalf("expected production marker reason, got %#v", report.Reasons)
	}
	if strings.Contains(report.SanitizedDSN, "secret") {
		t.Fatalf("sanitized dsn leaked password: %q", report.SanitizedDSN)
	}
}

func TestDBTargetCheckRejectsAmbiguousTarget(t *testing.T) {
	report := checkDBTargetSafety("staging", "dapo:secret@tcp(127.0.0.1:3306)/dapo?parseTime=true")
	if report.OK || report.MigrationDryRunAllowed || report.RiskLevel != "medium" {
		t.Fatalf("expected ambiguous target to fail: %#v", report)
	}
	if !strings.Contains(strings.Join(report.Reasons, " "), "database name must include") {
		t.Fatalf("expected naming guard reason, got %#v", report.Reasons)
	}
}

func TestDBTargetCheckRejectsProductionEnvEvenWithSafeName(t *testing.T) {
	report := checkDBTargetSafety("production", "dapo:secret@tcp(127.0.0.1:3306)/dapo_modelgw_dryrun?parseTime=true")
	if report.OK || report.MigrationDryRunAllowed || report.RiskLevel != "high" {
		t.Fatalf("expected production env to fail even with safe db name: %#v", report)
	}
	if !strings.Contains(strings.Join(report.Reasons, " "), "target-env production") {
		t.Fatalf("expected target-env production reason, got %#v", report.Reasons)
	}
}

func TestPrintDBTargetCheckRedactsDSN(t *testing.T) {
	var buf bytes.Buffer
	err := printDBTargetCheck(&buf, "staging", "dapo:secret-password@tcp(127.0.0.1:3306)/dapo_modelgw_staging_clone?parseTime=true&loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "secret-password") || strings.Contains(out, "loc=Local") {
		t.Fatalf("db target check leaked sensitive DSN detail: %s", out)
	}
	if !strings.Contains(out, `"ok": true`) || !strings.Contains(out, `"migration_dry_run_allowed": true`) || !strings.Contains(out, `"sanitized_dsn"`) {
		t.Fatalf("expected successful sanitized output, got %s", out)
	}
}

func TestFilterProviderSeedsByProviderScope(t *testing.T) {
	seeds, err := filterProviderSeeds(defaultProviderSeeds(), "mimo")
	if err != nil {
		t.Fatalf("expected mimo scope to pass: %v", err)
	}
	if len(seeds) != 1 || seeds[0].ProviderName != "mimo" {
		t.Fatalf("expected only mimo seed, got %#v", seeds)
	}
	seeds, err = filterProviderSeeds(defaultProviderSeeds(), "deepseek-official")
	if err != nil {
		t.Fatalf("expected channel-code scope to pass: %v", err)
	}
	if len(seeds) != 1 || seeds[0].ProviderName != "deepseek" {
		t.Fatalf("expected only deepseek seed, got %#v", seeds)
	}
	seeds, err = filterProviderSeeds(defaultProviderSeeds(), "all")
	if err != nil {
		t.Fatalf("expected all scope to pass: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("expected both default seeds, got %#v", seeds)
	}
}

func TestFilterProviderSeedsRejectsUnknownScope(t *testing.T) {
	_, err := filterProviderSeeds(defaultProviderSeeds(), "unknown-provider")
	if err == nil {
		t.Fatal("expected unknown provider scope to fail")
	}
	if !strings.Contains(err.Error(), "--provider") {
		t.Fatalf("unexpected provider scope error: %v", err)
	}
}

func TestEnabledProviderSeedsForScopeIgnoresOtherEnvKeys(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "mimo-secret")
	t.Setenv("DAPO_DEEPSEEK_API_KEY", "deepseek-secret")
	seeds, err := enabledProviderSeedsForScope("mimo")
	if err != nil {
		t.Fatalf("expected scoped enabled seeds to pass: %v", err)
	}
	if len(seeds) != 1 || seeds[0].ProviderName != "mimo" {
		t.Fatalf("expected only mimo seed with both env keys present, got %#v", seeds)
	}
}

func TestNoProviderKeyErrorMentionsScopedEnvOnly(t *testing.T) {
	seeds, err := filterProviderSeeds(defaultProviderSeeds(), "mimo")
	if err != nil {
		t.Fatal(err)
	}
	err = noProviderKeyError("mimo", seeds)
	if err == nil {
		t.Fatal("expected missing scoped key to fail")
	}
	if !strings.Contains(err.Error(), "DAPO_MIMO_API_KEY") || strings.Contains(err.Error(), "DAPO_DEEPSEEK_API_KEY") {
		t.Fatalf("expected scoped key error to mention only MiMo env, got %v", err)
	}
}

func TestKeyStdinRequiresSingleProviderAndAction(t *testing.T) {
	if err := validateKeyStdinOption(commandOptions{KeyStdin: true, ProviderProbe: true, ProviderScope: "mimo"}, []providerSeed{{ProviderName: "mimo"}}); err != nil {
		t.Fatalf("expected single provider probe stdin key to pass: %v", err)
	}
	if err := validateKeyStdinOption(commandOptions{KeyStdin: true, PlanOnly: true, ProviderScope: "mimo"}, []providerSeed{{ProviderName: "mimo"}}); err == nil {
		t.Fatal("expected --key-stdin with --plan to fail")
	}
	if err := validateKeyStdinOption(commandOptions{KeyStdin: true, ProviderScope: "mimo"}, []providerSeed{{ProviderName: "mimo"}}); err == nil {
		t.Fatal("expected --key-stdin without consuming action to fail")
	}
	if err := validateKeyStdinOption(commandOptions{KeyStdin: true, ProviderProbe: true, ProviderScope: "all"}, []providerSeed{{ProviderName: "mimo"}, {ProviderName: "deepseek"}}); err == nil {
		t.Fatal("expected --key-stdin with multiple providers to fail")
	}
}

func TestReadProviderKeyFromStdinSingleLine(t *testing.T) {
	key, err := readProviderKeyFromStdin(strings.NewReader("  stdin-secret-value\n"))
	if err != nil {
		t.Fatal(err)
	}
	if key != "stdin-secret-value" {
		t.Fatalf("unexpected stdin key: %q", key)
	}
	if _, err := readProviderKeyFromStdin(strings.NewReader("first\nsecond\n")); err == nil {
		t.Fatal("expected multiline stdin key to fail")
	}
	if _, err := readProviderKeyFromStdin(strings.NewReader(" \n")); err == nil {
		t.Fatal("expected empty stdin key to fail")
	}
}

func TestEnabledProviderSeedsForScopeWithInputUsesStdinWithoutEnv(t *testing.T) {
	seeds, err := enabledProviderSeedsForScopeWithInput("mimo", true, strings.NewReader("stdin-secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || seeds[0].ProviderName != "mimo" {
		t.Fatalf("expected one MiMo seed, got %#v", seeds)
	}
	if providerAPIKey(seeds[0]) != "stdin-secret" || providerAPIKeySource(seeds[0]) != "stdin" {
		t.Fatalf("expected stdin key source, got key=%q source=%q", providerAPIKey(seeds[0]), providerAPIKeySource(seeds[0]))
	}
}

func TestShellQuoteEscapesSingleQuote(t *testing.T) {
	got := shellQuote("mimo's-model")
	want := "'mimo'\"'\"'s-model'"
	if got != want {
		t.Fatalf("unexpected shell quote: got %q want %q", got, want)
	}
}

func TestAPIChannelSeedRequestsDoNotStoreProviderKeyOnChannel(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "secret-value")
	seed := providerSeed{
		APIKeyEnv:      "DAPO_MIMO_API_KEY",
		ChannelCode:    "mimo-official",
		ChannelName:    "MiMo 官方 API",
		ProviderName:   "mimo",
		BaseURL:        "https://token-plan-cn.xiaomimimo.com/v1",
		PublicModel:    "mimo-v2.5-pro",
		UpstreamModel:  "mimo-v2.5-pro",
		Capabilities:   []string{"chat"},
		Priority:       20,
		TimeoutSeconds: 120,
	}

	createReq := apiChannelCreateReq(seed)
	if createReq.APIKey != "" {
		t.Fatalf("provider key must be written to Key Pool, not API Channel create request: %#v", createReq.APIKey)
	}
	if createReq.Code != seed.ChannelCode || createReq.Adapter != model.APIChannelAdapterOpenAIChat {
		t.Fatalf("unexpected create request: %#v", createReq)
	}

	updateReq := apiChannelUpdateReq(seed)
	if updateReq.APIKey != nil {
		t.Fatalf("provider key must be written to Key Pool, not API Channel update request: %#v", updateReq.APIKey)
	}
	if updateReq.Code == nil || *updateReq.Code != seed.ChannelCode {
		t.Fatalf("unexpected update request: %#v", updateReq)
	}
}

func TestLegacyAPIChannelCredentialEntryDoesNotExposeSecret(t *testing.T) {
	entry := legacyAPIChannelCredentialEntry(&model.APIChannel{
		ID:            42,
		Code:          "mimo-official",
		Name:          "MiMo 官方 API",
		ProviderName:  "mimo",
		Adapter:       model.APIChannelAdapterOpenAIChat,
		Status:        model.APIChannelStatusEnabled,
		CredentialEnc: []byte("encrypted-secret-bytes"),
	}, 2, 1)

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if strings.Contains(out, "encrypted-secret-bytes") {
		t.Fatalf("legacy credential audit entry must not expose encrypted credential bytes, got %s", out)
	}
	if entry.Code != "mimo-official" || entry.KeyCount != 2 || entry.EnabledKeyCount != 1 {
		t.Fatalf("unexpected legacy credential audit entry: %#v", entry)
	}
	if !strings.Contains(entry.Reason, "legacy channel-level credential") {
		t.Fatalf("expected cleanup reason, got %q", entry.Reason)
	}
}

func TestConfigurationAuditFindingSummary(t *testing.T) {
	empty := configurationAuditReport{}
	if configurationAuditHasFindings(empty) {
		t.Fatalf("empty audit report should not have findings")
	}
	if got := configurationAuditFindingSummary(empty); got != "account_pool_conflicts=0 api_channel_legacy_credentials=0 source_duplicates=0" {
		t.Fatalf("unexpected empty audit summary: %q", got)
	}

	report := configurationAuditReport{
		AccountPoolConflicts: []conflictEntry{{
			ModelCode: "mimo-v2.5-pro",
			Reason:    "official API model is mapped to GPT account pool",
		}},
		APIChannelLegacyCredentials: []legacyCredentialEntry{{
			Code:   "mimo-official",
			Reason: "legacy channel-level credential",
		}},
		SourceDuplicates: []sourceDuplicateEntry{{
			ModelCode:   "mimo-v2.5-pro",
			SourceCode:  "mimo-official",
			FirstID:     101,
			DuplicateID: 102,
			Reason:      "duplicate model source mapping route signature",
		}},
	}
	if !configurationAuditHasFindings(report) {
		t.Fatalf("audit report with conflict, legacy credential and duplicate source should have findings")
	}
	if got := configurationAuditFindingSummary(report); got != "account_pool_conflicts=1 api_channel_legacy_credentials=1 source_duplicates=1" {
		t.Fatalf("unexpected audit summary: %q", got)
	}
}

func TestAuditScopeForProviderTargetsSelectedSeedOnly(t *testing.T) {
	scope, err := auditScopeForProvider("mimo")
	if err != nil {
		t.Fatal(err)
	}
	if scope.Global {
		t.Fatalf("mimo provider scope should be scoped, got %#v", scope)
	}
	if !scope.ModelCodes["mimo-v2.5-pro"] || !scope.APIChannelCodes["mimo-official"] || !scope.ProviderNames["mimo"] {
		t.Fatalf("mimo audit scope missing expected targets: %#v", scope)
	}
	if scope.ModelCodes["deepseek-chat"] || scope.APIChannelCodes["deepseek-official"] {
		t.Fatalf("mimo audit scope should not include deepseek targets: %#v", scope)
	}
}

func TestAuditScopeForProviderAllIsGlobal(t *testing.T) {
	scope, err := auditScopeForProvider("all")
	if err != nil {
		t.Fatal(err)
	}
	if !scope.Global {
		t.Fatalf("all provider scope should be global, got %#v", scope)
	}
}

func TestAuditScopeMatchesTargetModelSourceAndChannel(t *testing.T) {
	scope, err := auditScopeForProvider("mimo")
	if err != nil {
		t.Fatal(err)
	}
	mimoSource := &model.ModelSourceMapping{
		ModelCode:  "mimo-v2.5-pro",
		SourceType: model.ModelSourceTypeAccountPool,
		SourceCode: model.ProviderGPT,
	}
	if !auditScopeMatchesModelSource(scope, &model.ModelCatalog{ModelCode: "mimo-v2.5-pro"}, mimoSource) {
		t.Fatal("expected scoped audit to match MiMo model source")
	}
	deepSeekSource := &model.ModelSourceMapping{
		ModelCode:  "deepseek-chat",
		SourceType: model.ModelSourceTypeAccountPool,
		SourceCode: model.ProviderGPT,
	}
	if auditScopeMatchesModelSource(scope, &model.ModelCatalog{ModelCode: "deepseek-chat"}, deepSeekSource) {
		t.Fatal("expected scoped audit to ignore DeepSeek model source")
	}
	if !auditScopeMatchesAPIChannel(scope, &model.APIChannel{Code: "mimo-official", ProviderName: "mimo"}) {
		t.Fatal("expected scoped audit to match MiMo API Channel")
	}
	if auditScopeMatchesAPIChannel(scope, &model.APIChannel{Code: "deepseek-official", ProviderName: "deepseek"}) {
		t.Fatal("expected scoped audit to ignore DeepSeek API Channel")
	}
}

func TestDuplicateModelSourceEntriesDetectsDisabledHistoricalDuplicate(t *testing.T) {
	models := map[string]*model.ModelCatalog{
		"mimo-v2.5-pro": {
			ModelCode:            "mimo-v2.5-pro",
			UpstreamDefaultModel: "mimo-v2.5-pro",
		},
	}
	sources := []*model.ModelSourceMapping{
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
	}

	got := duplicateModelSourceEntries(models, sources)
	if len(got) != 1 {
		t.Fatalf("duplicate entries = %#v, want one", got)
	}
	if got[0].FirstID != 101 || got[0].DuplicateID != 102 {
		t.Fatalf("unexpected duplicate IDs: %#v", got[0])
	}
	if got[0].UpstreamModel != "mimo-v2.5-pro" || got[0].SourceType != model.ModelSourceTypeAPIChannel {
		t.Fatalf("unexpected duplicate entry: %#v", got[0])
	}
}

func TestAuditScopeMatchesDuplicateEntry(t *testing.T) {
	scope, err := auditScopeForProvider("mimo")
	if err != nil {
		t.Fatal(err)
	}
	if !auditScopeMatchesDuplicateEntry(scope, sourceDuplicateEntry{
		ModelCode:  "mimo-v2.5-pro",
		SourceCode: "mimo-official",
	}) {
		t.Fatal("expected MiMo duplicate entry to match scoped audit")
	}
	if auditScopeMatchesDuplicateEntry(scope, sourceDuplicateEntry{
		ModelCode:  "deepseek-chat",
		SourceCode: "deepseek-official",
	}) {
		t.Fatal("expected DeepSeek duplicate entry to be ignored by MiMo scoped audit")
	}
}

func TestUpsertAPIChannelKeyCreatesNamedKeyPoolRow(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "secret-value")
	fake := &fakeAPIChannelKeyService{}

	err := upsertAPIChannelKey(context.Background(), fake, providerSeed{
		APIKeyEnv: "DAPO_MIMO_API_KEY",
		KeyName:   "primary",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if fake.created == nil {
		t.Fatal("expected key-pool row to be created")
	}
	if fake.created.channelID != 42 || fake.created.req.Name != "primary" || fake.created.req.APIKey != "secret-value" {
		t.Fatalf("unexpected create request: %#v", fake.created)
	}
	if fake.created.req.Priority != 1 || fake.created.req.Weight != 100 {
		t.Fatalf("unexpected priority/weight: %#v", fake.created.req)
	}
	if fake.created.req.Status == nil || *fake.created.req.Status != model.APIChannelKeyStatusEnabled {
		t.Fatalf("expected enabled key status, got %#v", fake.created.req.Status)
	}
}

func TestUpsertAPIChannelKeyUsesSeedKeyValueWithoutEnv(t *testing.T) {
	fake := &fakeAPIChannelKeyService{}

	err := upsertAPIChannelKey(context.Background(), fake, providerSeed{
		APIKeyEnv:    "DAPO_MIMO_API_KEY",
		APIKeyValue:  "stdin-secret-value",
		APIKeySource: "stdin",
		KeyName:      "primary",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if fake.created == nil || fake.created.req.APIKey != "stdin-secret-value" {
		t.Fatalf("expected key-pool create to use seed APIKeyValue, got %#v", fake.created)
	}
}

func TestUpsertAPIChannelKeyUpdatesExistingNamedKeyPoolRow(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "rotated-secret")
	fake := &fakeAPIChannelKeyService{
		keys: []*dto.APIChannelKeyResp{{ID: 7, ChannelID: 42, Name: "primary"}},
	}

	err := upsertAPIChannelKey(context.Background(), fake, providerSeed{
		APIKeyEnv: "DAPO_MIMO_API_KEY",
		KeyName:   "primary",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if fake.updated == nil {
		t.Fatal("expected existing key-pool row to be updated")
	}
	if fake.updated.channelID != 42 || fake.updated.keyID != 7 {
		t.Fatalf("unexpected update target: %#v", fake.updated)
	}
	if fake.updated.req.APIKey == nil || *fake.updated.req.APIKey != "rotated-secret" {
		t.Fatalf("expected rotated API key in update request, got %#v", fake.updated.req.APIKey)
	}
	if fake.created != nil {
		t.Fatalf("did not expect create when named key exists: %#v", fake.created)
	}
}

func TestDefaultOfficialTextPricingUsesTokenDefaults(t *testing.T) {
	got := defaultOfficialTextPricing("DAPO_UNITTEST")
	if got.Mode != model.ModelCatalogPricingToken {
		t.Fatalf("mode = %q, want token", got.Mode)
	}
	if got.InputUnitPoints != 100 || got.OutputUnitPoints != 300 || got.UnitPoints != 0 {
		t.Fatalf("unexpected default pricing: %#v", got)
	}
	if got.Explicit {
		t.Fatalf("default pricing should not be explicit: %#v", got)
	}
}

func TestDefaultOfficialTextPricingAcceptsHumanPointEnv(t *testing.T) {
	t.Setenv("DAPO_UNITTEST_INPUT_POINTS", "1.25")
	t.Setenv("DAPO_UNITTEST_OUTPUT_POINTS", "4.5")
	got := defaultOfficialTextPricing("DAPO_UNITTEST")
	if got.InputUnitPoints != 125 || got.OutputUnitPoints != 450 {
		t.Fatalf("unexpected env pricing conversion: %#v", got)
	}
	if !got.Explicit {
		t.Fatalf("expected env pricing to be explicit")
	}
}

func TestPricingForUpsertRepairsEmptyCatalogPricing(t *testing.T) {
	got := pricingForUpsert(&model.ModelCatalog{
		EntryKind:   model.ModelCatalogKindText,
		PricingMode: model.ModelCatalogPricingManual,
	}, providerSeed{
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  100,
		OutputUnitPoints: 300,
		PricingExplicit:  false,
	})
	if got.Mode != model.ModelCatalogPricingToken || got.InputUnitPoints != 100 || got.OutputUnitPoints != 300 {
		t.Fatalf("expected empty pricing to be repaired from seed, got %#v", got)
	}
}

func TestPricingForUpsertPreservesExistingEffectivePricing(t *testing.T) {
	got := pricingForUpsert(&model.ModelCatalog{
		EntryKind:        model.ModelCatalogKindText,
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  220,
		OutputUnitPoints: 880,
	}, providerSeed{
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  100,
		OutputUnitPoints: 300,
		PricingExplicit:  false,
	})
	if got.InputUnitPoints != 220 || got.OutputUnitPoints != 880 {
		t.Fatalf("expected existing effective pricing to be preserved, got %#v", got)
	}
}

func TestPricingForUpsertExplicitSeedOverridesExistingPricing(t *testing.T) {
	got := pricingForUpsert(&model.ModelCatalog{
		EntryKind:        model.ModelCatalogKindText,
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  220,
		OutputUnitPoints: 880,
	}, providerSeed{
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  100,
		OutputUnitPoints: 300,
		PricingExplicit:  true,
	})
	if got.InputUnitPoints != 100 || got.OutputUnitPoints != 300 {
		t.Fatalf("expected explicit seed pricing to override, got %#v", got)
	}
}

func TestDefaultProviderSeedsIncludeParametersSchema(t *testing.T) {
	seeds := defaultProviderSeeds()
	if len(seeds) == 0 {
		t.Fatal("expected default provider seeds")
	}
	for _, seed := range seeds {
		if seed.ParametersSchema == nil {
			t.Fatalf("expected %s to include parameter schema", seed.Label)
		}
	}
}

func TestParametersSchemaForUpsertPreservesExisting(t *testing.T) {
	existing := `{"controls":[{"key":"custom"}]}`
	got := parametersSchemaForUpsert(&model.ModelCatalog{ParametersSchema: &existing}, providerSeed{
		ParametersSchema: defaultOfficialTextParametersSchema(),
	})
	if got != nil {
		t.Fatalf("expected existing schema to be preserved, got %#v", got)
	}
}

func TestParametersSchemaForUpsertRepairsMissing(t *testing.T) {
	got := parametersSchemaForUpsert(&model.ModelCatalog{}, providerSeed{
		ParametersSchema: defaultOfficialTextParametersSchema(),
	})
	if got == nil {
		t.Fatal("expected missing schema to be repaired from seed")
	}
}

func TestPrintPlanIncludesParameterSchemaWithoutSecret(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "secret-value")
	var buf bytes.Buffer
	if err := printPlan(&buf, []providerSeed{{
		Label:            "MiMo",
		APIKeyEnv:        "DAPO_MIMO_API_KEY",
		ChannelCode:      "mimo-official",
		ChannelName:      "MiMo 官方 API",
		ProviderName:     "mimo",
		BaseURL:          "https://token-plan-cn.xiaomimimo.com/v1",
		KeyName:          "primary",
		PublicModel:      "mimo-v2.5-pro",
		UpstreamModel:    "mimo-v2.5-pro",
		EntryKind:        model.ModelCatalogKindText,
		Capabilities:     []string{"chat"},
		ParametersSchema: defaultOfficialTextParametersSchema(),
		PricingMode:      model.ModelCatalogPricingToken,
		InputUnitPoints:  100,
		OutputUnitPoints: 300,
	}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"parameters_schema"`) {
		t.Fatalf("expected plan to include parameter schema, got %q", out)
	}
	if strings.Contains(out, "secret-value") {
		t.Fatalf("expected plan not to leak env secret, got %q", out)
	}
}

func TestProviderProbeModelsEndpointSuccessSanitized(t *testing.T) {
	const secret = "unit-secret-models"
	t.Setenv("DAPO_UNITTEST_API_KEY", secret)
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Fatalf("unexpected Authorization header %q", got)
		}
		return fakeHTTPResponse(http.StatusOK, `{"data":[{"id":"mimo-v2.5-pro"}]}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:         "Unit",
		APIKeyEnv:     "DAPO_UNITTEST_API_KEY",
		ChannelCode:   "unit-official",
		BaseURL:       "https://provider.test/v1",
		PublicModel:   "mimo-v2.5-pro",
		UpstreamModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("provider probe output leaked secret: %s", out)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.Proof != "models_endpoint_2xx_model_listed" || got.ModelsStatus != 200 || got.ModelVisibility != "matched_upstream_model" {
		t.Fatalf("unexpected probe result: %#v", got)
	}
}

func TestProviderProbeUsesSeedKeyValueWithoutEnv(t *testing.T) {
	const secret = "stdin-secret-models"
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Fatalf("unexpected Authorization header %q", got)
		}
		return fakeHTTPResponse(http.StatusOK, `{"data":[{"id":"mimo-v2.5-pro"}]}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:         "Unit",
		APIKeyEnv:     "DAPO_UNITTEST_API_KEY",
		APIKeyValue:   secret,
		APIKeySource:  "stdin",
		ChannelCode:   "unit-official",
		BaseURL:       "https://provider.test/v1",
		PublicModel:   "mimo-v2.5-pro",
		UpstreamModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("provider probe output leaked stdin secret: %s", out)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.APIKeySource != "stdin" || !got.APIKeyPresent {
		t.Fatalf("unexpected stdin probe result: %#v", got)
	}
}

func TestProviderProbeRetriesTransientModelsFailure(t *testing.T) {
	withoutProviderProbeRetrySleep(t)
	const secret = "unit-secret-retry"
	t.Setenv("DAPO_UNITTEST_API_KEY", secret)
	attempts := 0
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		attempts++
		if attempts < providerProbeMaxAttempts {
			return nil, errors.New("dial tcp: lookup provider.test: no such host")
		}
		return fakeHTTPResponse(http.StatusOK, `{"data":[{"id":"mimo-v2.5-pro"}]}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:         "Unit",
		APIKeyEnv:     "DAPO_UNITTEST_API_KEY",
		ChannelCode:   "unit-official",
		BaseURL:       "https://provider.test/v1",
		PublicModel:   "mimo-v2.5-pro",
		UpstreamModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != providerProbeMaxAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, providerProbeMaxAttempts)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.ModelsAttempts != providerProbeMaxAttempts || got.Proof != "models_endpoint_2xx_model_listed" {
		t.Fatalf("unexpected retry success result: %#v", got)
	}
}

func TestProviderProbeRetriesTransientProtocolFailure(t *testing.T) {
	withoutProviderProbeRetrySleep(t)
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-protocol-retry")
	protocolAttempts := 0
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusNotFound, `not found`), nil
		case "/v1/chat/completions":
			protocolAttempts++
			if protocolAttempts < providerProbeMaxAttempts {
				return fakeHTTPResponse(http.StatusServiceUnavailable, `{"error":"temporary"}`), nil
			}
			return fakeHTTPResponse(http.StatusBadRequest, `{"error":{"message":"messages is required"}}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	if protocolAttempts != providerProbeMaxAttempts {
		t.Fatalf("protocol attempts = %d, want %d", protocolAttempts, providerProbeMaxAttempts)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.ProtocolAttempts != providerProbeMaxAttempts || got.Proof != "chat_protocol_validation_400" {
		t.Fatalf("unexpected protocol retry result: %#v", got)
	}
}

func TestProviderProbeFailsWhenModelsEndpointOmitsTargetModel(t *testing.T) {
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-missing-model")
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusOK, `{"data":[{"id":"other-model"}]}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:         "Unit",
		APIKeyEnv:     "DAPO_UNITTEST_API_KEY",
		ChannelCode:   "unit-official",
		BaseURL:       "https://provider.test/v1",
		PublicModel:   "mimo-v2.5-pro",
		UpstreamModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected missing target model to fail provider probe")
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || got.ModelVisibility != "target_model_not_listed" || !strings.Contains(got.ErrorSummary, "not listed") {
		t.Fatalf("unexpected missing model result: %#v", got)
	}
}

func TestProviderProbeModelIDParserSupportsCommonShapes(t *testing.T) {
	raw := []byte(`{"data":[{"id":"mimo-v2.5-pro"},{"model":"deepseek-chat"}],"models":["gpt-image-2"]}`)
	ids := parseProviderProbeModelIDs(raw)
	for _, want := range []string{"mimo-v2.5-pro", "deepseek-chat", "gpt-image-2"} {
		found := false
		for _, got := range ids {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected ids to contain %q, got %#v", want, ids)
		}
	}
}

func TestProviderProbeModelsEndpointWithoutIDsRequiresProtocolProbe(t *testing.T) {
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-no-ids")
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusOK, `{"object":"list"}`), nil
		case "/v1/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"model":"mimo-v2.5-pro"`) {
				t.Fatalf("expected protocol probe to include target model, got %s", raw)
			}
			return fakeHTTPResponse(http.StatusBadRequest, `{"error":{"message":"messages is required"}}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:         "Unit",
		APIKeyEnv:     "DAPO_UNITTEST_API_KEY",
		ChannelCode:   "unit-official",
		BaseURL:       "https://provider.test/v1",
		PublicModel:   "mimo-v2.5-pro",
		UpstreamModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.Proof != "chat_protocol_validation_400" || got.ModelVisibility != "models_endpoint_no_parseable_model_ids_protocol_probe" {
		t.Fatalf("unexpected no-id protocol result: %#v", got)
	}
}

func TestProviderProbeRejectsProtocolModelFailure(t *testing.T) {
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-model-fail")
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusNotFound, `not found`), nil
		case "/v1/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"model":"mimo-v2.5-pro"`) {
				t.Fatalf("expected protocol probe to include target model, got %s", raw)
			}
			return fakeHTTPResponse(http.StatusBadRequest, `{"error":{"message":"model not found: mimo-v2.5-pro"}}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected protocol model failure to fail provider probe")
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || !strings.Contains(got.ErrorSummary, "target model rejected") {
		t.Fatalf("unexpected model failure result: %#v", got)
	}
}

func TestProviderProbeFallsBackToProtocolValidation(t *testing.T) {
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-protocol")
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusNotFound, `not found`), nil
		case "/v1/chat/completions":
			return fakeHTTPResponse(http.StatusBadRequest, `{"error":{"message":"model is required"}}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if !got.OK || got.Proof != "chat_protocol_validation_400" || got.ModelsStatus != 404 || got.ProtocolStatus != 400 {
		t.Fatalf("unexpected probe result: %#v", got)
	}
}

func TestProviderProbeAuthFailureDoesNotPassAndRedacts(t *testing.T) {
	const secret = "unit-secret-auth"
	t.Setenv("DAPO_UNITTEST_API_KEY", secret)
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusNotFound, `not found`), nil
		case "/v1/chat/completions":
			return fakeHTTPResponse(http.StatusUnauthorized, `{"error":"invalid api_key `+secret+`"}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected provider probe auth failure to return an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("provider probe error leaked secret: %s", err)
	}
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("provider probe output leaked secret: %s", out)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || got.ProtocolStatus != http.StatusUnauthorized || !strings.Contains(got.ErrorSummary, "[redacted]") {
		t.Fatalf("unexpected auth failure result: %#v", got)
	}
}

func TestProviderProbeRedactsBearerAndRawSecretEcho(t *testing.T) {
	const secret = "unit-secret-bearer-echo"
	t.Setenv("DAPO_UNITTEST_API_KEY", secret)
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/models":
			return fakeHTTPResponse(http.StatusNotFound, `not found`), nil
		case "/v1/chat/completions":
			return fakeHTTPResponse(http.StatusUnauthorized, `{"error":"Authorization: Bearer `+secret+` raw `+secret+`"}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected provider probe auth failure to return an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("provider probe error leaked secret: %s", err)
	}
	out := buf.String()
	if strings.Contains(out, secret) || strings.Contains(strings.ToLower(out), "bearer "+strings.ToLower(secret)) {
		t.Fatalf("provider probe output leaked bearer or raw secret: %s", out)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || got.ProtocolStatus != http.StatusUnauthorized || !strings.Contains(got.ErrorSummary, "[redacted]") {
		t.Fatalf("unexpected bearer redaction result: %#v", got)
	}
}

func TestProviderProbeRejectsCredentialBearingBaseURL(t *testing.T) {
	const secret = "unit-secret-in-url"
	t.Setenv("DAPO_UNITTEST_API_KEY", secret)
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("provider probe should reject unsafe base_url before HTTP request")
		return fakeHTTPResponse(http.StatusInternalServerError, `{}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://user:" + secret + "@provider.test/v1?api_key=" + secret + "#frag",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected unsafe base_url to fail provider probe")
	}
	out := buf.String()
	if strings.Contains(out, secret) || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe base_url leaked secret: err=%v out=%s", err, out)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || !strings.Contains(got.ErrorSummary, "must not include userinfo") {
		t.Fatalf("unexpected unsafe base_url result: %#v", got)
	}
	if strings.Contains(got.BaseURL, "@") || strings.Contains(got.BaseURL, "?") || strings.Contains(got.BaseURL, "#") {
		t.Fatalf("expected sanitized base_url in output, got %#v", got.BaseURL)
	}
}

func TestProviderProbeFailsClosedAfterWritingSanitizedJSON(t *testing.T) {
	withoutProviderProbeRetrySleep(t)
	t.Setenv("DAPO_UNITTEST_API_KEY", "unit-secret-fail-closed")
	client := &http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		return fakeHTTPResponse(http.StatusInternalServerError, `{"error":"temporary upstream error"}`), nil
	})}

	var buf bytes.Buffer
	err := runProviderProbe(context.Background(), &buf, []providerSeed{{
		Label:       "Unit",
		APIKeyEnv:   "DAPO_UNITTEST_API_KEY",
		ChannelCode: "unit-official",
		BaseURL:     "https://provider.test/v1",
		PublicModel: "mimo-v2.5-pro",
	}}, client)
	if err == nil {
		t.Fatal("expected failed provider probe to return an error")
	}
	if !strings.Contains(err.Error(), "provider probe failed for Unit") {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string][]providerProbeResult
	if err := json.Unmarshal(buf.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body["provider_probes"][0]
	if got.OK || got.ModelsStatus != http.StatusInternalServerError || got.ModelsAttempts != providerProbeMaxAttempts {
		t.Fatalf("unexpected fail-closed result: %#v", got)
	}
}

func TestSchemaCheckPassesWhenRequiredColumnsExist(t *testing.T) {
	inspector := newFakeSchemaInspector(requiredSchemaTables())
	report := checkRequiredSchema(inspector, requiredSchemaTables())
	if !report.OK {
		t.Fatalf("expected schema check to pass, got %#v", report)
	}
	for _, table := range report.Tables {
		if !table.OK || len(table.MissingColumns) != 0 {
			t.Fatalf("unexpected table check: %#v", table)
		}
	}
}

func TestSchemaCheckFailsMissingTableAndColumn(t *testing.T) {
	inspector := newFakeSchemaInspector(requiredSchemaTables())
	delete(inspector.tables, "api_channel_key")
	delete(inspector.columns["model_catalog"], "parameters_schema")

	var buf bytes.Buffer
	err := printSchemaCheck(&buf, inspector)
	if err == nil {
		t.Fatal("expected schema check to fail")
	}
	if !strings.Contains(err.Error(), "schema check failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	var report schemaCheckReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected failed report, got %#v", report)
	}
	if !schemaReportMissing(report, "api_channel_key", "*") {
		t.Fatalf("expected missing api_channel_key table, got %#v", report.Tables)
	}
	if !schemaReportMissing(report, "model_catalog", "parameters_schema") {
		t.Fatalf("expected missing parameters_schema column, got %#v", report.Tables)
	}
}

func TestSchemaCheckRequiresRefundRecordForBillingProof(t *testing.T) {
	requirements := requiredSchemaTables()
	inspector := newFakeSchemaInspector(requirements)
	delete(inspector.tables, "refund_record")

	report := checkRequiredSchema(inspector, requirements)
	if report.OK {
		t.Fatalf("expected missing refund_record to fail, got %#v", report)
	}
	if !schemaReportMissing(report, "refund_record", "*") {
		t.Fatalf("expected missing refund_record table, got %#v", report.Tables)
	}
}

func TestSchemaCheckRequiresBillingProofColumns(t *testing.T) {
	requirements := requiredSchemaTables()
	inspector := newFakeSchemaInspector(requirements)
	delete(inspector.columns["consume_record"], "unit_points")
	delete(inspector.columns["wallet_log"], "points_before")

	report := checkRequiredSchema(inspector, requirements)
	if report.OK {
		t.Fatalf("expected missing billing proof columns to fail, got %#v", report)
	}
	if !schemaReportMissing(report, "consume_record", "unit_points") {
		t.Fatalf("expected missing consume_record.unit_points, got %#v", report.Tables)
	}
	if !schemaReportMissing(report, "wallet_log", "points_before") {
		t.Fatalf("expected missing wallet_log.points_before, got %#v", report.Tables)
	}
}

func TestSchemaCheckRequiresOperationalModelGatewayColumns(t *testing.T) {
	requirements := requiredSchemaTables()
	inspector := newFakeSchemaInspector(requirements)
	delete(inspector.columns["api_channel"], "proxy_id")
	delete(inspector.columns["api_channel"], "deleted_at")
	delete(inspector.columns["api_channel_key"], "deleted_at")
	delete(inspector.columns["model_catalog"], "sort_order")
	delete(inspector.columns["model_source_mapping"], "deleted_at")

	report := checkRequiredSchema(inspector, requirements)
	if report.OK {
		t.Fatalf("expected missing operational columns to fail, got %#v", report)
	}
	for _, tc := range []struct {
		table  string
		column string
	}{
		{table: "api_channel", column: "proxy_id"},
		{table: "api_channel", column: "deleted_at"},
		{table: "api_channel_key", column: "deleted_at"},
		{table: "model_catalog", column: "sort_order"},
		{table: "model_source_mapping", column: "deleted_at"},
	} {
		if !schemaReportMissing(report, tc.table, tc.column) {
			t.Fatalf("expected missing %s.%s, got %#v", tc.table, tc.column, report.Tables)
		}
	}
}

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withoutProviderProbeRetrySleep(t *testing.T) {
	t.Helper()
	old := providerProbeSleep
	providerProbeSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() {
		providerProbeSleep = old
	})
}

func fakeHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type fakeAPIChannelKeyService struct {
	keys    []*dto.APIChannelKeyResp
	created *capturedKeyCreate
	updated *capturedKeyUpdate
}

type capturedKeyCreate struct {
	channelID uint64
	req       *dto.APIChannelKeyCreateReq
}

type capturedKeyUpdate struct {
	channelID uint64
	keyID     uint64
	req       *dto.APIChannelKeyUpdateReq
}

func (s *fakeAPIChannelKeyService) ListKeys(ctx context.Context, channelID uint64) ([]*dto.APIChannelKeyResp, error) {
	return s.keys, nil
}

func (s *fakeAPIChannelKeyService) CreateKey(ctx context.Context, channelID uint64, req *dto.APIChannelKeyCreateReq) (*model.APIChannelKey, error) {
	s.created = &capturedKeyCreate{channelID: channelID, req: req}
	return &model.APIChannelKey{ID: 99, ChannelID: channelID}, nil
}

func (s *fakeAPIChannelKeyService) UpdateKey(ctx context.Context, channelID, keyID uint64, req *dto.APIChannelKeyUpdateReq) error {
	s.updated = &capturedKeyUpdate{channelID: channelID, keyID: keyID, req: req}
	return nil
}

type fakeSchemaInspector struct {
	tables  map[string]bool
	columns map[string]map[string]bool
}

func newFakeSchemaInspector(requirements []schemaTableRequirement) *fakeSchemaInspector {
	out := &fakeSchemaInspector{tables: map[string]bool{}, columns: map[string]map[string]bool{}}
	for _, req := range requirements {
		out.tables[req.Table] = true
		out.columns[req.Table] = map[string]bool{}
		for _, column := range req.Columns {
			out.columns[req.Table][column] = true
		}
	}
	return out
}

func (s *fakeSchemaInspector) HasTable(dst any) bool {
	table, _ := dst.(string)
	return s.tables[table]
}

func (s *fakeSchemaInspector) HasColumn(dst any, field string) bool {
	table, _ := dst.(string)
	return s.columns[table][field]
}

func schemaReportMissing(report schemaCheckReport, table, column string) bool {
	for _, item := range report.Tables {
		if item.Table != table {
			continue
		}
		for _, missing := range item.MissingColumns {
			if missing == column {
				return true
			}
		}
	}
	return false
}
