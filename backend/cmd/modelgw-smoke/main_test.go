package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func containsDetail(details []string, needle string) bool {
	for _, detail := range details {
		if strings.Contains(detail, needle) {
			return true
		}
	}
	return false
}

func flattenDetails(results []checkResult) []string {
	var out []string
	for _, result := range results {
		out = append(out, result.Details...)
	}
	return out
}

func TestNormalizeOptionsExpandsPostGenerationProof(t *testing.T) {
	opts := normalizeOptions(options{
		ModelCode:                  "mimo-v2.5-pro",
		EntryKind:                  "text",
		APIChannelCode:             "mimo-official",
		RequirePostGenerationProof: true,
	})

	required := map[string]bool{
		"RequireAdmin":             opts.RequireAdmin,
		"RequireOpenAIAuth":        opts.RequireOpenAIAuth,
		"RequireModel":             opts.RequireModel,
		"RequireCatalogModel":      opts.RequireCatalogModel,
		"RequireAPIChannel":        opts.RequireAPIChannel,
		"RequireAPIChannelHealth":  opts.RequireAPIChannelHealth,
		"RequireKeyPool":           opts.RequireKeyPool,
		"ForbidLegacyChannelKey":   opts.ForbidLegacyChannelKey,
		"RequireSourceMapping":     opts.RequireSourceMapping,
		"RequireRouteChannel":      opts.RequireRouteChannel,
		"RequireNoSourceConflicts": opts.RequireNoSourceConflicts,
		"ForbidAccountPoolRoute":   opts.ForbidAccountPoolRoute,
		"RequirePricingMode":       opts.RequirePricingMode,
		"RequireAuditRoute":        opts.RequireAuditRoute,
		"RequireAuditPricing":      opts.RequireAuditPricing,
		"RequireParameterSchema":   opts.RequireParameterSchema,
		"RequireOutputProof":       opts.RequireOutputProof,
		"RequireUpstreamLog":       opts.RequireUpstreamLog,
		"RequireKeyUsageFeedback":  opts.RequireKeyUsageFeedback,
		"RequireBillingProof":      opts.RequireBillingProof,
	}
	for name, enabled := range required {
		if !enabled {
			t.Fatalf("expected %s to be enabled by post-generation proof", name)
		}
	}
	if opts.RequireVideoJobProof {
		t.Fatalf("expected text post-generation proof not to require video job proof")
	}
	if opts.AuditPricingSource != "model_catalog" {
		t.Fatalf("expected default audit pricing source model_catalog, got %q", opts.AuditPricingSource)
	}
}

func TestNormalizeOptionsExpandsVideoPostGenerationProof(t *testing.T) {
	opts := normalizeOptions(options{
		ModelCode:                  "mimo-video",
		EntryKind:                  "video",
		APIChannelCode:             "mimo-official",
		TaskID:                     "task-video-proof",
		RequirePostGenerationProof: true,
	})
	if !opts.RequireVideoJobProof {
		t.Fatalf("expected video post-generation proof to require video job proof")
	}
	if opts.RequireParameterSchema {
		t.Fatalf("expected video post-generation proof not to require text parameter schema")
	}

	res := checkOptionRequirements(opts)
	if len(res) != 1 || res[0].Status != "ok" {
		t.Fatalf("expected complete video proof options to pass, got %#v", res)
	}
	if !containsDetail(res[0].Details, "require-video-job-proof") {
		t.Fatalf("expected expanded proof details to mention video job proof, got %#v", res[0].Details)
	}
	if !containsDetail(res[0].Details, "require-audit-video-filter") {
		t.Fatalf("expected expanded proof details to mention video audit filter, got %#v", res[0].Details)
	}
}

func TestNormalizeOptionsPreservesExplicitAuditPricingSource(t *testing.T) {
	opts := normalizeOptions(options{
		RequirePostGenerationProof: true,
		AuditPricingSource:         "billing_model_prices",
	})
	if opts.AuditPricingSource != "billing_model_prices" {
		t.Fatalf("expected explicit audit pricing source to be preserved, got %q", opts.AuditPricingSource)
	}
}

func TestNormalizeOptionsExpandsExpectedPricingMode(t *testing.T) {
	opts := normalizeOptions(options{
		ModelCode:           "mimo-v2.5-pro",
		ExpectedPricingMode: "char",
	})
	if !opts.RequireAdmin {
		t.Fatalf("expected pricing mode to require admin checks")
	}
	if !opts.RequireCatalogModel {
		t.Fatalf("expected pricing mode to require Model Catalog check")
	}
	if !opts.RequirePricingMode {
		t.Fatalf("expected pricing mode to require public pricing mode checks")
	}
}

func TestCheckOptionRequirementsForPostGenerationProof(t *testing.T) {
	res := checkOptionRequirements(normalizeOptions(options{RequirePostGenerationProof: true}))
	if len(res) != 1 || res[0].Status != "error" {
		t.Fatalf("expected missing proof options to fail, got %#v", res)
	}
	if got := strings.Join(res[0].Details, " "); !strings.Contains(got, "--model") || !strings.Contains(got, "--entry-kind") || !strings.Contains(got, "--api-channel") || !strings.Contains(got, "--task-id") {
		t.Fatalf("expected missing options to be reported, got %q", got)
	}

	res = checkOptionRequirements(normalizeOptions(options{
		ModelCode:                  "mimo-v2.5-pro",
		EntryKind:                  "text",
		APIChannelCode:             "mimo-official",
		TaskID:                     "task-proof",
		RequirePostGenerationProof: true,
	}))
	if len(res) != 1 || res[0].Status != "ok" {
		t.Fatalf("expected complete proof options to pass, got %#v", res)
	}
}

func TestCheckStandaloneOptionRequirementsForbidLegacyNeedsAPIChannel(t *testing.T) {
	res := checkOptionRequirements(normalizeOptions(options{ForbidLegacyChannelKey: true}))
	if len(res) != 1 || res[0].Status != "error" {
		t.Fatalf("expected missing api-channel to fail, got %#v", res)
	}
	if !containsDetail(res[0].Details, "--forbid-legacy-channel-key needs --api-channel") {
		t.Fatalf("expected forbid legacy option detail, got %#v", res[0].Details)
	}
}

func TestEvidenceTemplateIncludesPreLaunchGates(t *testing.T) {
	tpl := buildEvidenceTemplate(options{
		ModelCode:           "mimo-v2.5-pro",
		EntryKind:           "text",
		APIChannelCode:      "mimo-official",
		ExpectedPricingMode: "char",
		TaskID:              "task-proof",
		TargetEnv:           "staging",
	}, 123)
	if tpl.TargetEnv != "staging" || tpl.Model != "mimo-v2.5-pro" || tpl.APIChannel != "mimo-official" || tpl.Provider != "mimo" {
		t.Fatalf("unexpected template identity: %#v", tpl)
	}
	required := map[string]bool{
		"local_preflight":             false,
		"provider_probe":              false,
		"config_plan":                 false,
		"db_target_check":             false,
		"audit_only":                  false,
		"schema_check":                false,
		"migration_dry_run":           false,
		"authorized_write":            false,
		"pre_generation_smoke":        false,
		"controlled_generation_task":  false,
		"post_generation_proof":       false,
		"secret_scan":                 false,
		"backend_tests":               false,
		"frontend_builds":             false,
		"frontend_preview_smoke":      false,
		"admin_protected_pages_smoke": false,
		"release_manifest":            false,
		"deployment_runbook":          false,
		"deployment_approval":         false,
	}
	for _, gate := range tpl.RequiredGates {
		if _, ok := required[gate.ID]; ok {
			required[gate.ID] = true
		}
		if gate.Status != "pending" {
			t.Fatalf("gate %s status = %q, want pending", gate.ID, gate.Status)
		}
	}
	for id, present := range required {
		if !present {
			t.Fatalf("missing evidence gate %s in %#v", id, tpl.RequiredGates)
		}
	}
	expectedOrder := []string{
		"local_preflight",
		"provider_probe",
		"config_plan",
		"db_target_check",
		"audit_only",
		"schema_check",
		"migration_dry_run",
		"authorized_write",
		"pre_generation_smoke",
		"controlled_generation_task",
		"post_generation_proof",
		"secret_scan",
		"backend_tests",
		"frontend_builds",
		"frontend_preview_smoke",
		"admin_protected_pages_smoke",
		"release_manifest",
		"deployment_runbook",
		"deployment_approval",
	}
	if len(tpl.RequiredGates) != len(expectedOrder) {
		t.Fatalf("unexpected evidence gate count = %d, want %d: %#v", len(tpl.RequiredGates), len(expectedOrder), tpl.RequiredGates)
	}
	for i, want := range expectedOrder {
		if got := tpl.RequiredGates[i].ID; got != want {
			t.Fatalf("gate order[%d] = %q, want %q; gates=%#v", i, got, want, tpl.RequiredGates)
		}
	}
	var postCommand string
	var preCommand string
	for _, gate := range tpl.RequiredGates {
		if gate.ID == "pre_generation_smoke" {
			preCommand = gate.Command
		}
		if gate.ID == "post_generation_proof" {
			postCommand = gate.Command
		}
	}
	for _, requiredFlag := range []string{"--require-openai-auth", "--require-admin", "--require-catalog-model", "--require-parameter-schema", "--require-pricing-mode", "--require-api-channel-health", "--forbid-legacy-channel-key", "--forbid-account-pool-route"} {
		if !strings.Contains(preCommand, requiredFlag) {
			t.Fatalf("pre-generation command missing %s: %q", requiredFlag, preCommand)
		}
	}
	if !strings.Contains(postCommand, "--task-id 'task-proof'") || !strings.Contains(postCommand, "--require-openai-auth") || !strings.Contains(postCommand, "--require-post-generation-proof") || !strings.Contains(postCommand, "--pricing-mode 'char'") {
		t.Fatalf("post-generation command missing hard gates: %q", postCommand)
	}
	var localPreflightGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "local_preflight" {
			localPreflightGate = &tpl.RequiredGates[i]
			break
		}
	}
	if localPreflightGate == nil {
		t.Fatalf("missing local preflight gate")
	}
	if !containsDetail(localPreflightGate.Collect, "git status --short output") || !containsDetail(localPreflightGate.Collect, "merge conflict marker scan output") || !containsDetail(localPreflightGate.PassWhen, "intentionally in scope") {
		t.Fatalf("local preflight evidence is incomplete: collect=%#v pass=%#v", localPreflightGate.Collect, localPreflightGate.PassWhen)
	}
	var dbTargetGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "db_target_check" {
			dbTargetGate = &tpl.RequiredGates[i]
			break
		}
	}
	if dbTargetGate == nil {
		t.Fatalf("missing DB target check gate")
	}
	if !containsDetail(dbTargetGate.Collect, "sanitized target DB address") || !containsDetail(dbTargetGate.PassWhen, "migration_dry_run_allowed=true") || !containsDetail(dbTargetGate.PassWhen, "production/live/online markers") || !strings.Contains(dbTargetGate.Command, "--db-target-check") {
		t.Fatalf("DB target check evidence is incomplete: command=%q collect=%#v pass=%#v", dbTargetGate.Command, dbTargetGate.Collect, dbTargetGate.PassWhen)
	}
	var previewGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "frontend_preview_smoke" {
			previewGate = &tpl.RequiredGates[i]
			break
		}
	}
	if previewGate == nil {
		t.Fatalf("missing frontend preview smoke gate")
	}
	if !containsDetail(previewGate.PassWhen, "user frontend root mounts") || !containsDetail(previewGate.PassWhen, "admin login root mounts") {
		t.Fatalf("frontend preview smoke pass criteria are too weak: %#v", previewGate.PassWhen)
	}
	var protectedGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "admin_protected_pages_smoke" {
			protectedGate = &tpl.RequiredGates[i]
			break
		}
	}
	if protectedGate == nil {
		t.Fatalf("missing admin protected pages smoke gate")
	}
	if !containsDetail(protectedGate.Collect, "API Channels page screenshot") || !containsDetail(protectedGate.Collect, "Generation log detail screenshot") || !containsDetail(protectedGate.PassWhen, "real target-environment data") {
		t.Fatalf("admin protected pages smoke evidence is incomplete: collect=%#v pass=%#v", protectedGate.Collect, protectedGate.PassWhen)
	}
	var migrationGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "migration_dry_run" {
			migrationGate = &tpl.RequiredGates[i]
			break
		}
	}
	if migrationGate == nil {
		t.Fatalf("missing migration dry-run gate")
	}
	if !containsDetail(migrationGate.Collect, "migration-inventory") || !containsDetail(migrationGate.Collect, "post-migration schema-check output") || !containsDetail(migrationGate.PassWhen, "required Model Gateway migrations") || !containsDetail(migrationGate.PassWhen, "production primary") || !containsDetail(migrationGate.PassWhen, "rollback or DB restore") {
		t.Fatalf("migration dry-run evidence is incomplete: collect=%#v pass=%#v", migrationGate.Collect, migrationGate.PassWhen)
	}
	var releaseGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "release_manifest" {
			releaseGate = &tpl.RequiredGates[i]
			break
		}
	}
	if releaseGate == nil {
		t.Fatalf("missing release manifest gate")
	}
	if !containsDetail(releaseGate.Collect, "build artifact or image tag") || !containsDetail(releaseGate.Collect, "migration inventory") || !containsDetail(releaseGate.Collect, "target DB backup point") || !containsDetail(releaseGate.PassWhen, "checksums") || !containsDetail(releaseGate.PassWhen, "rollback artifact") {
		t.Fatalf("release manifest evidence is incomplete: collect=%#v pass=%#v", releaseGate.Collect, releaseGate.PassWhen)
	}
	var deploymentRunbookGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "deployment_runbook" {
			deploymentRunbookGate = &tpl.RequiredGates[i]
			break
		}
	}
	if deploymentRunbookGate == nil {
		t.Fatalf("missing deployment runbook gate")
	}
	if !containsDetail(deploymentRunbookGate.Collect, "ordered deployment steps") || !containsDetail(deploymentRunbookGate.Collect, "health check URLs") || !containsDetail(deploymentRunbookGate.Collect, "rollback trigger conditions") || !containsDetail(deploymentRunbookGate.PassWhen, "measurable") {
		t.Fatalf("deployment runbook evidence is incomplete: collect=%#v pass=%#v", deploymentRunbookGate.Collect, deploymentRunbookGate.PassWhen)
	}
	var deploymentApprovalGate *evidenceGate
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "deployment_approval" {
			deploymentApprovalGate = &tpl.RequiredGates[i]
			break
		}
	}
	if deploymentApprovalGate == nil {
		t.Fatalf("missing deployment approval gate")
	}
	if !containsDetail(deploymentApprovalGate.Collect, "pre-deployment evidence verification output") || !containsDetail(deploymentApprovalGate.PassWhen, "ready_for_deployment_approval=true") {
		t.Fatalf("deployment approval evidence is incomplete: collect=%#v pass=%#v", deploymentApprovalGate.Collect, deploymentApprovalGate.PassWhen)
	}
}

func TestPrintEvidenceTemplateOutputsSanitizedJSON(t *testing.T) {
	t.Setenv("DAPO_MIMO_API_KEY", "tp-secret-provider-key-1234567890")
	t.Setenv("DAPO_SMOKE_OPENAI_API_KEY", "sk-secret-openai-key-1234567890")
	var buf strings.Builder
	if err := printEvidenceTemplate(&buf, options{
		ModelCode:      "mimo-v2.5-pro",
		EntryKind:      "text",
		APIChannelCode: "mimo-official",
		TargetEnv:      "staging",
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "tp-secret-provider-key-1234567890") || strings.Contains(out, "sk-secret-openai-key-1234567890") {
		t.Fatalf("evidence template leaked a provider key: %s", out)
	}
	var tpl evidenceTemplate
	if err := json.Unmarshal([]byte(out), &tpl); err != nil {
		t.Fatalf("invalid evidence template JSON: %v\n%s", err, out)
	}
	if len(tpl.RequiredGates) == 0 || tpl.RequiredGates[0].ID != "local_preflight" {
		t.Fatalf("unexpected evidence gates: %#v", tpl.RequiredGates)
	}
}

func TestVerifyEvidenceTemplateFailsPendingGates(t *testing.T) {
	tpl := buildEvidenceTemplate(options{
		TargetEnv:      "staging",
		ModelCode:      "mimo-v2.5-pro",
		EntryKind:      "text",
		APIChannelCode: "mimo-official",
	}, 123)
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || report.ReadyForDeploymentApproval {
		t.Fatalf("pending template must not verify: %#v", report)
	}
	if !containsString(report.IncompleteGateIDs, "local_preflight") || !containsString(report.IncompleteGateIDs, "deployment_runbook") {
		t.Fatalf("expected pending gates to be incomplete, got %#v", report.IncompleteGateIDs)
	}
	if containsString(report.IncompleteGateIDs, "deployment_approval") {
		t.Fatalf("deployment approval should be allowed pending unless explicitly required: %#v", report.IncompleteGateIDs)
	}
}

func TestVerifyEvidenceTemplateAllowsPendingDeploymentApprovalByDefault(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if !report.OK || !report.ReadyForDeploymentApproval {
		t.Fatalf("expected all pre-deployment gates to verify: %#v", report)
	}
	if report.PassedGateCount != report.RequiredGateCount-1 {
		t.Fatalf("expected all but deployment approval passed, got %#v", report)
	}
	report = verifyEvidenceTemplate(tpl, "memory.json", true)
	if report.OK || !containsString(report.IncompleteGateIDs, "deployment_approval") {
		t.Fatalf("expected explicit deployment approval requirement to fail: %#v", report)
	}
	tpl = completedEvidenceTemplate(true)
	report = verifyEvidenceTemplate(tpl, "memory.json", true)
	if !report.OK || report.PassedGateCount != report.RequiredGateCount {
		t.Fatalf("expected full evidence including deployment approval to verify: %#v", report)
	}
}

func TestVerifyEvidenceTemplateRejectsMissingOrOutOfOrderGate(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	filtered := tpl.RequiredGates[:0]
	for _, gate := range tpl.RequiredGates {
		if gate.ID != "db_target_check" {
			filtered = append(filtered, gate)
		}
	}
	tpl.RequiredGates = filtered
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || !containsString(report.MissingGateIDs, "db_target_check") {
		t.Fatalf("expected missing DB target gate to fail: %#v", report)
	}

	tpl = completedEvidenceTemplate(false)
	tpl.RequiredGates[2], tpl.RequiredGates[3] = tpl.RequiredGates[3], tpl.RequiredGates[2]
	report = verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || len(report.OutOfOrderGateIDs) == 0 {
		t.Fatalf("expected out-of-order gates to fail: %#v", report)
	}
}

func TestVerifyEvidenceTemplateRejectsPassedGateWithoutEvidence(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	tpl.RequiredGates[0].Evidence = nil
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || !containsString(report.MissingEvidenceGateIDs, tpl.RequiredGates[0].ID) {
		t.Fatalf("expected passed gate without evidence to fail: %#v", report)
	}
}

func TestVerifyEvidenceTemplateRejectsInvalidEvidenceRef(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	tpl.RequiredGates[0].Evidence = []string{"just some words"}
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || len(report.InvalidEvidenceRefs) == 0 {
		t.Fatalf("expected invalid evidence ref to fail: %#v", report)
	}
}

func TestVerifyEvidenceTemplateChecksLocalEvidenceFiles(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "local-preflight.log")
	if err := os.WriteFile(logPath, []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tpl := completedEvidenceTemplate(false)
	tpl.RequiredGates[0].Evidence = []string{"log:" + logPath}
	report := verifyEvidenceTemplate(tpl, filepath.Join(dir, "evidence.json"), false)
	if !report.OK {
		t.Fatalf("expected existing local evidence path to pass: %#v", report)
	}
	tpl.RequiredGates[0].Evidence = []string{"log:" + filepath.Join(dir, "missing.log")}
	report = verifyEvidenceTemplate(tpl, filepath.Join(dir, "evidence.json"), false)
	if report.OK || len(report.MissingEvidenceArtifactRefs) == 0 {
		t.Fatalf("expected missing local evidence path to fail: %#v", report)
	}
}

func TestVerifyEvidenceTemplateRequiresGateSpecificEvidenceKinds(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "controlled_generation_task" {
			tpl.RequiredGates[i].Evidence = []string{"log:" + testEvidenceFile(t, "controlled-generation.log")}
			break
		}
	}
	report := verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || !containsString(report.InsufficientEvidenceGateIDs, "controlled_generation_task") {
		t.Fatalf("expected controlled generation without task_id evidence to fail: %#v", report)
	}

	tpl = completedEvidenceTemplate(false)
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "release_manifest" {
			tpl.RequiredGates[i].Evidence = []string{
				"commit:abc123",
				"artifact:image-tag",
				"log:" + testEvidenceFile(t, "release-manifest.log"),
			}
			break
		}
	}
	report = verifyEvidenceTemplate(tpl, "memory.json", false)
	if report.OK || !containsString(report.InsufficientEvidenceGateIDs, "release_manifest") {
		t.Fatalf("expected release manifest without backup evidence to fail: %#v", report)
	}
}

func TestPrintEvidenceVerificationRejectsSecretAndPendingGates(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	raw, err := json.Marshal(tpl)
	if err != nil {
		t.Fatal(err)
	}
	var withSecret map[string]any
	if err := json.Unmarshal(raw, &withSecret); err != nil {
		t.Fatal(err)
	}
	withSecret["api_key"] = "tp-abc123456789012345678901"
	raw, err = json.Marshal(withSecret)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	err = printEvidenceVerification(&buf, path, false)
	if err == nil {
		t.Fatal("expected secret-shaped evidence file to fail verification")
	}
	if !strings.Contains(buf.String(), "secret") && !strings.Contains(buf.String(), "api_key") {
		t.Fatalf("expected secret finding in verification output, got %s", buf.String())
	}
}

func TestPrintEvidenceVerificationPassesCompleteEvidenceFile(t *testing.T) {
	tpl := completedEvidenceTemplate(false)
	raw, err := json.Marshal(tpl)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := printEvidenceVerification(&buf, path, false); err != nil {
		t.Fatalf("expected complete pre-deployment evidence to pass: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), `"ready_for_deployment_approval": true`) {
		t.Fatalf("expected ready_for_deployment_approval=true, got %s", buf.String())
	}
}

func completedEvidenceTemplate(includeDeploymentApproval bool) evidenceTemplate {
	dir, err := os.MkdirTemp("", "modelgw-evidence-test-*")
	if err != nil {
		panic(err)
	}
	logPath := filepath.Join(dir, "evidence.log")
	if err := os.WriteFile(logPath, []byte("ok\n"), 0o600); err != nil {
		panic(err)
	}
	screenshotPath := filepath.Join(dir, "screenshot.png")
	if err := os.WriteFile(screenshotPath, []byte("png\n"), 0o600); err != nil {
		panic(err)
	}
	tpl := buildEvidenceTemplate(options{
		TargetEnv:      "staging",
		ModelCode:      "mimo-v2.5-pro",
		EntryKind:      "text",
		APIChannelCode: "mimo-official",
		TaskID:         "task-proof",
	}, 123)
	for i := range tpl.RequiredGates {
		if tpl.RequiredGates[i].ID == "deployment_approval" && !includeDeploymentApproval {
			tpl.RequiredGates[i].Status = "pending"
			continue
		}
		tpl.RequiredGates[i].Status = "passed"
		tpl.RequiredGates[i].Evidence = testEvidenceRefsForGate(tpl.RequiredGates[i].ID, logPath, screenshotPath)
	}
	return tpl
}

func testEvidenceRefsForGate(gateID, logPath, screenshotPath string) []string {
	switch gateID {
	case "controlled_generation_task":
		return []string{"task_id:task-proof", "log:" + logPath}
	case "frontend_preview_smoke", "admin_protected_pages_smoke":
		return []string{"screenshot:" + screenshotPath, "log:" + logPath}
	case "release_manifest":
		return []string{"commit:abc123", "artifact:image-tag", "backup:backup-point", "log:" + logPath}
	case "deployment_approval":
		return []string{"review:approved-by-user", "backup:backup-point"}
	default:
		return []string{"log:" + logPath}
	}
}

func testEvidenceFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestCheckOpenAIModelsUsesAPIKeyEnvWithoutLeakingIt(t *testing.T) {
	t.Setenv("DAPO_SMOKE_TEST_OPENAI_KEY", "sk-klein-secret-smoke-key")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-klein-secret-smoke-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model"}]}`), nil
	})}
	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:      "http://unit.test/v1",
		OpenAIAPIKeyEnv: "DAPO_SMOKE_TEST_OPENAI_KEY",
		ModelCode:       "mimo-v2.5-pro",
		RequireModel:    true,
	})
	if res.Status != "ok" {
		t.Fatalf("expected openai models check to pass, got %#v", res)
	}
	joined := strings.Join(res.Details, " ")
	if !strings.Contains(joined, "auth=provided") {
		t.Fatalf("expected auth marker without key value, got %q", joined)
	}
	if strings.Contains(joined, "sk-klein-secret-smoke-key") {
		t.Fatalf("expected details not to leak API key, got %q", joined)
	}
}

func TestCheckOpenAIModelsExplainsMissingAPIKeyOnUnauthorized(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected no authorization header, got %q", got)
		}
		return jsonResponse(401, `{"code":401104,"msg":"API Key 无效"}`), nil
	})}
	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:      "http://unit.test/v1",
		OpenAIAPIKeyEnv: "DAPO_SMOKE_TEST_OPENAI_KEY",
	})
	if res.Status != "error" {
		t.Fatalf("expected unauthorized check to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "DAPO_SMOKE_TEST_OPENAI_KEY is empty") {
		t.Fatalf("expected missing env hint, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresExpectedKindAndEndpoint(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"image","endpoint":"/v1/images/generations"}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:   "http://unit.test/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "text",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected wrong OpenAI-compatible model kind to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "openai model kind mismatch") {
		t.Fatalf("expected kind mismatch detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresEndpointWhenEntryKindProvided(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text"}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:   "http://unit.test/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "text",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing OpenAI-compatible endpoint to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "openai model endpoint missing") {
		t.Fatalf("expected missing endpoint detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsAcceptsExpectedKindAndEndpoint(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions"}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:   "http://unit.test/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "chat",
		RequireModel: true,
	})
	if res.Status != "ok" {
		t.Fatalf("expected OpenAI-compatible text model to pass, got %#v", res)
	}
}

func TestCheckOpenAIModelsRejectsSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"api_key":"should-not-leak"}}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:   "http://unit.test/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "text",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected OpenAI-compatible model secret leak to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "openai model leaks sensitive fields") {
		t.Fatalf("expected OpenAI-compatible secret leak detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresParameterSchemaWhenRequested(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions"}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:             "http://unit.test/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "text",
		RequireModel:           true,
		RequireParameterSchema: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing OpenAI-compatible parameters_schema to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "openai model parameters_schema missing") {
		t.Fatalf("expected parameters_schema detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsAcceptsParameterSchemaInMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"parameters_schema":{"type":"object","properties":{"temperature":{"type":"number"}}}}}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:             "http://unit.test/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "chat",
		RequireModel:           true,
		RequireParameterSchema: true,
	})
	if res.Status != "ok" {
		t.Fatalf("expected OpenAI-compatible parameters_schema to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "expected_model_parameters_schema_present=true") {
		t.Fatalf("expected parameters_schema presence detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresAPIKeyWhenConfigured(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("expected missing OpenAI-compatible API key to fail before HTTP request")
		return nil, nil
	})}
	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:        "http://unit.test/v1",
		OpenAIAPIKeyEnv:   "DAPO_SMOKE_TEST_OPENAI_KEY_MISSING",
		RequireOpenAIAuth: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing OpenAI-compatible key to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "auth=missing") || !containsDetail(res.Details, "DAPO_SMOKE_TEST_OPENAI_KEY_MISSING is empty") {
		t.Fatalf("expected missing auth details, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresPricingModeWhenRequested(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions"}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:         "http://unit.test/v1",
		ModelCode:          "mimo-v2.5-pro",
		RequireModel:       true,
		RequirePricingMode: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing OpenAI-compatible pricing_mode to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "meta.pricing_mode missing") {
		t.Fatalf("expected pricing_mode detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsRequiresExpectedPricingMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"pricing_mode":"token"}}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:          "http://unit.test/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequireModel:        true,
		RequirePricingMode:  true,
		ExpectedPricingMode: "char",
	})
	if res.Status != "error" {
		t.Fatalf("expected OpenAI-compatible pricing_mode mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "pricing_mode mismatch") {
		t.Fatalf("expected pricing_mode mismatch detail, got %#v", res.Details)
	}
}

func TestCheckOpenAIModelsAcceptsPricingModeInMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"pricing_mode":"char"}}]}`), nil
	})}

	res := checkOpenAIModels(context.Background(), client, options{
		OpenAIBase:          "http://unit.test/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequireModel:        true,
		RequirePricingMode:  true,
		ExpectedPricingMode: "char",
	})
	if res.Status != "ok" {
		t.Fatalf("expected OpenAI-compatible pricing_mode to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "expected_model_pricing_mode=char") {
		t.Fatalf("expected pricing_mode detail, got %#v", res.Details)
	}
}

func TestCheckPublicPricingModeConsistencyRequiresMatchingSurfaces(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/models":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","pricing_mode":"char"}]}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"pricing_mode":"token"}}]}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkPublicPricingModeConsistency(context.Background(), client, options{
		APIBase:            "http://unit.test/api/v1",
		OpenAIBase:         "http://unit.test/v1",
		ModelCode:          "mimo-v2.5-pro",
		RequirePricingMode: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected public pricing_mode mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "public pricing_mode mismatch") {
		t.Fatalf("expected public pricing_mode mismatch detail, got %#v", res.Details)
	}
}

func TestCheckPublicPricingModeConsistencyAcceptsMatchingExpectedMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/models":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","pricing_mode":"char"}]}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			return jsonResponse(200, `{"object":"list","data":[{"id":"mimo-v2.5-pro","object":"model","kind":"text","endpoint":"/v1/chat/completions","meta":{"pricing_mode":"char"}}]}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkPublicPricingModeConsistency(context.Background(), client, options{
		APIBase:             "http://unit.test/api/v1",
		OpenAIBase:          "http://unit.test/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequirePricingMode:  true,
		ExpectedPricingMode: "char",
	})
	if res.Status != "ok" {
		t.Fatalf("expected public pricing_mode consistency to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "api_pricing_mode=char") || !containsDetail(res.Details, "openai_pricing_mode=char") {
		t.Fatalf("expected both pricing mode details, got %#v", res.Details)
	}
}

func TestCheckPublicModelsRequiresExpectedModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"gpt-image-2","name":"GPT Image","kind":"image"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:      "http://unit.test/api/v1",
		ModelCode:    "mimo-v2.5-pro",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing model to fail, got %#v", res)
	}
}

func TestCheckPublicModelsRequiresExpectedKind(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"image"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:      "http://unit.test/api/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "text",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected wrong public model kind to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "public model kind mismatch") {
		t.Fatalf("expected kind mismatch detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsAcceptsChatEntryAsTextKind(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:      "http://unit.test/api/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "chat",
		RequireModel: true,
	})
	if res.Status != "ok" {
		t.Fatalf("expected chat entry to accept public text kind, got %#v", res)
	}
}

func TestCheckPublicModelsRejectsSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","api_key":"should-not-leak"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:      "http://unit.test/api/v1",
		ModelCode:    "mimo-v2.5-pro",
		EntryKind:    "text",
		RequireModel: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected public model secret leak to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "public model leaks sensitive fields") {
		t.Fatalf("expected public secret leak detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsRequiresParameterSchemaWhenRequested(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:                "http://unit.test/api/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "text",
		RequireModel:           true,
		RequireParameterSchema: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing public parameters_schema to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "public model parameters_schema missing") {
		t.Fatalf("expected parameters_schema detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsAcceptsParameterSchema(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","parameters_schema":{"type":"object","properties":{"temperature":{"type":"number"}}}}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:                "http://unit.test/api/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "chat",
		RequireModel:           true,
		RequireParameterSchema: true,
	})
	if res.Status != "ok" {
		t.Fatalf("expected public parameters_schema to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "expected_model_parameters_schema_present=true") {
		t.Fatalf("expected parameters_schema presence detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsRequiresPricingModeWhenRequested(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:            "http://unit.test/api/v1",
		ModelCode:          "mimo-v2.5-pro",
		RequireModel:       true,
		RequirePricingMode: true,
	})
	if res.Status != "error" {
		t.Fatalf("expected missing public pricing_mode to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "public model pricing_mode missing") {
		t.Fatalf("expected pricing_mode detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsRequiresExpectedPricingMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","pricing_mode":"token"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:             "http://unit.test/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequireModel:        true,
		RequirePricingMode:  true,
		ExpectedPricingMode: "char",
	})
	if res.Status != "error" {
		t.Fatalf("expected public pricing_mode mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "pricing_mode mismatch") {
		t.Fatalf("expected pricing_mode mismatch detail, got %#v", res.Details)
	}
}

func TestCheckPublicModelsAcceptsPricingMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"model_code":"mimo-v2.5-pro","name":"MiMo","kind":"text","pricing_mode":"char"}]}}`), nil
	})}

	res := checkPublicModels(context.Background(), client, options{
		APIBase:             "http://unit.test/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequireModel:        true,
		RequirePricingMode:  true,
		ExpectedPricingMode: "char",
	})
	if res.Status != "ok" {
		t.Fatalf("expected public pricing_mode to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "expected_model_pricing_mode=char") {
		t.Fatalf("expected pricing_mode detail, got %#v", res.Details)
	}
}

func TestCheckAdminAuditValidatesReadOnlyPageShape(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.URL.Query().Get("model_code"); got != "mimo-v2.5-pro" {
			t.Fatalf("unexpected model_code query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[],"total":0,"page":1,"page_size":5}}`), nil
	})}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res := checkAdminAudit(ctx, client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected admin audit to pass, got %#v", res)
	}
}

func TestCheckAdminAuditRouteSnapshotRequiresExpectedAPIChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("audit_type"); got != "route" {
			t.Fatalf("unexpected audit_type query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		APIChannelCode:      "mimo-official",
		RequireAuditRoute:   true,
		RequireAuditPricing: true,
	}, "test-token", "route")
	if res.Status != "ok" {
		t.Fatalf("expected route audit snapshot to pass, got %#v", res)
	}
}

func TestCheckAdminAuditRouteSnapshotFailsWrongSelectedChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", "route")
	if res.Status != "error" {
		t.Fatalf("expected wrong selected route to fail, got %#v", res)
	}
}

func TestCheckAdminAuditPricingSnapshotRequiresExpectedSource(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("audit_type"); got != "pricing" {
			t.Fatalf("unexpected audit_type query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-2","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:          "http://unit.test/admin/api/v1",
		ModelCode:          "mimo-v2.5-pro",
		AuditPricingSource: "model_catalog",
	}, "test-token", "pricing")
	if res.Status != "ok" {
		t.Fatalf("expected pricing audit snapshot to pass, got %#v", res)
	}
}

func TestCheckAdminAuditOutputSnapshotUsesOutputFilter(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("audit_type"); got != "output" {
			t.Fatalf("unexpected audit_type query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-output","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":18,"completion_tokens":9},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
		TaskID:         "task-output",
	}, "test-token", "output")
	if res.Status != "ok" {
		t.Fatalf("expected output audit filter proof to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "output_filter_sample=task-output") {
		t.Fatalf("expected output filter detail, got %#v", res.Details)
	}
}

func TestCheckAdminAuditVideoSnapshotUsesVideoFilter(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("audit_type"); got != "video" {
			t.Fatalf("unexpected audit_type query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","source_code":"mimo-official","adapter":"openai_compatible_video","upstream_model":"mimo-video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
		TaskID:         "task-video",
	}, "test-token", "video")
	if res.Status != "ok" {
		t.Fatalf("expected video audit filter proof to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "video_filter_sample=task-video") {
		t.Fatalf("expected video filter detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresSameTaskPricing(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("audit_type"); got != "route" {
			t.Fatalf("unexpected audit_type query %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-2","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":18,"completion_tokens":9},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:          "http://unit.test/admin/api/v1",
		ModelCode:          "mimo-v2.5-pro",
		APIChannelCode:     "mimo-official",
		AuditPricingSource: "model_catalog",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected same-task pricing snapshot to pass, got %#v", res)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotHonorsTaskID(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("keyword"); got != "task-proof" {
			t.Fatalf("expected task_id keyword filter, got %q", got)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-running","created_at":2,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"estimated_until_usage","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"estimated_until_usage","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}},{"task_id":"task-proof","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":12,"completion_tokens":6},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":2,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:          "http://unit.test/admin/api/v1",
		ModelCode:          "mimo-v2.5-pro",
		APIChannelCode:     "mimo-official",
		TaskID:             "task-proof",
		AuditPricingSource: "model_catalog",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected exact task_id proof to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "same_task_status=task-proof status=2") {
		t.Fatalf("expected proof details to reference task-proof, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresSucceededTask(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-2","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"estimated_until_usage","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"estimated_until_usage","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected running route sample to fail post-generation proof, got %#v", res)
	}
	if !containsDetail(res.Details, "not succeeded") {
		t.Fatalf("expected not-succeeded detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresTextOutputProof(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-text","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing text output proof to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no output_snapshot") {
		t.Fatalf("expected output snapshot detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotFailsWhenSelectedRouteSampleHasNoPricing(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-2","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing same-task pricing snapshot to fail, got %#v", res)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresCostToMatchActualPoints(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-cost","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":500,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":20,"completion_tokens":10},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected mismatched task cost and pricing actual points to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "cost_points mismatch") {
		t.Fatalf("expected cost mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresExpectedPricingMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-price-mode","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":20,"completion_tokens":10},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		APIChannelCode:      "mimo-official",
		AuditPricingSource:  "model_catalog",
		ExpectedPricingMode: "char",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected pricing mode mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "pricing_mode mismatch") {
		t.Fatalf("expected pricing mode mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresCharUsageEvidence(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-char-missing","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"char","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":20,"completion_tokens":10},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"char","settlement":"settled","actual_points":483,"unit_basis":"per_1k_tokens"},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		APIChannelCode:      "mimo-official",
		AuditPricingSource:  "model_catalog",
		ExpectedPricingMode: "char",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected char pricing without char usage evidence to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "unit_basis mismatch") {
		t.Fatalf("expected unit_basis detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotAcceptsCharUsageEvidence(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-char-ok","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"char","settlement":"settled","output_snapshot":{"kind":"chat","stream":false,"output_present":true,"content_chars":20,"completion_tokens":10},"pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"char","settlement":"settled","actual_points":483,"unit_basis":"per_1k_chars","estimated_prompt_chars":42,"completion_chars":20},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		APIChannelCode:      "mimo-official",
		AuditPricingSource:  "model_catalog",
		ExpectedPricingMode: "char",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected char pricing usage evidence to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "char_pricing_usage=task-char-ok") {
		t.Fatalf("expected char usage detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresPreviewForImageVideo(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","actual_points":700},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected succeeded image row without preview_url to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no preview_url") {
		t.Fatalf("expected preview_url detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotAcceptsPreviewForImageVideo(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"preview_url":"/admin/api/v1/logs/generations/task-img/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":1,"request_mode":"t2i","resolution":"1K","quality":"high","actual_points":700,"matched_rule":{"model_code":"gpt-image-2","mode":"t2i","resolution":"1K","quality":"high","unit_points":700,"enabled":true}},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected succeeded image row with preview_url to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "matrix_pricing_rule=task-img") {
		t.Fatalf("expected matrix pricing rule detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRejectsMatrixQualityMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img-quality-mismatch","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"preview_url":"/admin/api/v1/logs/generations/task-img-quality-mismatch/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":1,"request_mode":"t2i","resolution":"1K","quality":"high","actual_points":700,"matched_rule":{"model_code":"gpt-image-2","mode":"t2i","resolution":"1K","quality":"draft","unit_points":700,"enabled":true}},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected matrix pricing quality mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "quality mismatch") {
		t.Fatalf("expected quality mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRejectsMatrixResolutionMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img-resolution-mismatch","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"preview_url":"/admin/api/v1/logs/generations/task-img-resolution-mismatch/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":1,"request_mode":"t2i","resolution":"2K","quality":"high","actual_points":700,"matched_rule":{"model_code":"gpt-image-2","mode":"t2i","resolution":"1K","quality":"high","unit_points":700,"enabled":true}},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected matrix pricing resolution mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "resolution mismatch") {
		t.Fatalf("expected resolution mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRequiresMatrixMatchedRule(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img-missing-rule","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"preview_url":"/admin/api/v1/logs/generations/task-img-missing-rule/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":1,"actual_points":700},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected matrix pricing without matched_rule to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing matched_rule") {
		t.Fatalf("expected matched_rule detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRejectsMatrixActualMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img-price-mismatch","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-img-price-mismatch/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":2,"request_mode":"t2i","resolution":"1K","quality":"high","actual_points":900,"matched_rule":{"model_code":"gpt-image-2","mode":"t2i","resolution":"1K","quality":"high","unit_points":700,"enabled":true}},"model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected matrix pricing actual mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "actual_points mismatch") {
		t.Fatalf("expected actual_points mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminRouteSamplePricingSnapshotRejectsMatrixDurationMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video-duration-mismatch","created_at":1,"user_id":22,"kind":"video","model_code":"sora2","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video-duration-mismatch/preview","selected_source_type":"api_channel","selected_source_code":"video-official","selected_upstream_model":"sora2","pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"matrix","settlement":"pre_deduct_fixed","count":1,"request_mode":"t2v","duration_sec":6,"quality":"standard","actual_points":900,"matched_rule":{"model_code":"sora2","mode":"t2v","duration_sec":10,"quality":"standard","unit_points":900,"enabled":true}},"model_gateway_route_snapshot":{"model_code":"sora2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"video-official","upstream_model":"sora2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminRouteSamplePricingSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "sora2",
		APIChannelCode: "video-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected matrix pricing duration mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "duration_sec mismatch") {
		t.Fatalf("expected duration_sec mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofRequiresSnapshot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing video job snapshot to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no video_job_snapshot") {
		t.Fatalf("expected video job snapshot detail, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofAcceptsTerminalSuccess(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","source_code":"mimo-official","adapter":"openai_compatible_video","upstream_model":"mimo-video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected terminal video job proof to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "phase=terminal_success") || !containsDetail(res.Details, "remote_task_id=remote-video-1") {
		t.Fatalf("expected video job proof details, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofRequiresSnapshotSourceCode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","adapter":"openai_compatible_video","upstream_model":"mimo-video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing video job source_code to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing video job source_code") {
		t.Fatalf("expected missing source_code detail, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofRejectsSnapshotSourceMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","source_code":"other-channel","adapter":"openai_compatible_video","upstream_model":"mimo-video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected video job source mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "video job source mismatch") {
		t.Fatalf("expected source mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofRequiresSnapshotUpstreamModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","source_code":"mimo-official","adapter":"openai_compatible_video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing video job upstream_model to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing video job upstream_model") {
		t.Fatalf("expected missing upstream_model detail, got %#v", res.Details)
	}
}

func TestCheckAdminVideoJobProofRejectsSnapshotUpstreamMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-video","created_at":1,"user_id":22,"kind":"video","model_code":"mimo-video","status":2,"cost_points":900,"preview_url":"/admin/api/v1/logs/generations/task-video/preview","selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-video","video_job_snapshot":{"source_type":"api_channel","source_code":"mimo-official","adapter":"openai_compatible_video","upstream_model":"other-video","remote_task_id":"remote-video-1","phase":"terminal_success","poll_attempts":4,"fallback_locked":true},"model_gateway_route_snapshot":{"model_code":"mimo-video","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-video"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminVideoJobProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-video",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected video job upstream_model mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "video job upstream_model mismatch") {
		t.Fatalf("expected upstream_model mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminOutputProofRequiresTextSnapshot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-text","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminOutputProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing text output proof to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no output_snapshot") {
		t.Fatalf("expected output snapshot detail, got %#v", res.Details)
	}
}

func TestCheckAdminOutputProofAcceptsPreviewForImageVideo(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-img","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":2,"cost_points":700,"preview_url":"/admin/api/v1/logs/generations/task-img/preview","selected_source_type":"api_channel","selected_source_code":"image-official","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"image-official","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminOutputProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		APIChannelCode: "image-official",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected succeeded image row with preview_url to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "preview=true") {
		t.Fatalf("expected preview proof detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofMatchesConsumeAndWalletSpend(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-billing","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pre_deduct_points":500,"actual_points":483,"refund_points":17,"extra_points":0},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-billing/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-billing","consume_record":{"id":1,"task_id":"task-billing","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":483,"total_points":483,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-billing","points":-500,"points_before":1000,"points_after":500,"created_at":1},{"id":11,"user_id":22,"direction":1,"biz_type":"refund","biz_id":"task-billing","points":17,"points_before":500,"points_after":517,"remark":"chat usage refund","created_at":2}],"refund_records":[{"id":20,"task_id":"task-billing","user_id":22,"points":17,"reason":"chat usage refund","operator":"system","created_at":2}],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":483,"wallet_log_count":2,"refund_record_count":1,"wallet_net_points":-483,"wallet_spend_points":483,"wallet_refund_points":17}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected billing proof to pass, got %#v", res)
	}
	if !containsDetail(res.Details, "wallet_spend_points=483") {
		t.Fatalf("expected wallet spend detail, got %#v", res.Details)
	}
	if !containsDetail(res.Details, "wallet_refund_points=17") || !containsDetail(res.Details, "refund_records=1") {
		t.Fatalf("expected refund proof detail, got %#v", res.Details)
	}
	if !containsDetail(res.Details, "pricing_refund_points=17") || !containsDetail(res.Details, "pricing_extra_points=0") {
		t.Fatalf("expected pricing refund proof detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofFailsWalletMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-billing","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"settled","actual_points":483},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-billing/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-billing","consume_record":{"id":1,"task_id":"task-billing","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":483,"total_points":483,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-billing","points":-500,"points_before":1000,"points_after":500,"created_at":1}],"refund_records":[],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":483,"wallet_log_count":1,"wallet_spend_points":500}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected wallet mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "wallet net spend mismatch") {
		t.Fatalf("expected wallet mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofRequiresRefundRecordForRefundWalletLog(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-refund-missing","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","actual_points":483,"refund_points":17},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-refund-missing/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-refund-missing","consume_record":{"id":1,"task_id":"task-refund-missing","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":483,"total_points":483,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-refund-missing","points":-500,"points_before":1000,"points_after":500,"created_at":1},{"id":11,"user_id":22,"direction":1,"biz_type":"refund","biz_id":"task-refund-missing","points":17,"points_before":500,"points_after":517,"remark":"chat usage refund","created_at":2}],"refund_records":[],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":483,"wallet_log_count":2,"refund_record_count":0,"wallet_net_points":-483,"wallet_spend_points":483,"wallet_refund_points":17}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing refund record to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no refund_records") {
		t.Fatalf("expected refund record detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofRejectsRefundRecordTotalMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-refund-mismatch","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","actual_points":483,"refund_points":17},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-refund-mismatch/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-refund-mismatch","consume_record":{"id":1,"task_id":"task-refund-mismatch","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":483,"total_points":483,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-refund-mismatch","points":-500,"points_before":1000,"points_after":500,"created_at":1},{"id":11,"user_id":22,"direction":1,"biz_type":"refund","biz_id":"task-refund-mismatch","points":17,"points_before":500,"points_after":517,"remark":"chat usage refund","created_at":2}],"refund_records":[{"id":20,"task_id":"task-refund-mismatch","user_id":22,"points":12,"reason":"chat usage refund","operator":"system","created_at":2}],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":483,"wallet_log_count":2,"refund_record_count":1,"wallet_net_points":-483,"wallet_spend_points":483,"wallet_refund_points":17}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected refund total mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "refund_record total mismatch") {
		t.Fatalf("expected refund total mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofRejectsPricingRefundMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-pricing-refund-mismatch","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"partial_refund","pre_deduct_points":500,"actual_points":483,"refund_points":0,"extra_points":0},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-pricing-refund-mismatch/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-pricing-refund-mismatch","consume_record":{"id":1,"task_id":"task-pricing-refund-mismatch","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":483,"total_points":483,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-pricing-refund-mismatch","points":-500,"points_before":1000,"points_after":500,"created_at":1},{"id":11,"user_id":22,"direction":1,"biz_type":"refund","biz_id":"task-pricing-refund-mismatch","points":17,"points_before":500,"points_after":517,"remark":"chat usage refund","created_at":2}],"refund_records":[{"id":20,"task_id":"task-pricing-refund-mismatch","user_id":22,"points":17,"reason":"chat usage refund","operator":"system","created_at":2}],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":483,"wallet_log_count":2,"refund_record_count":1,"wallet_net_points":-483,"wallet_spend_points":483,"wallet_refund_points":17}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected pricing refund mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "pricing snapshot refund_points mismatch") {
		t.Fatalf("expected pricing refund mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminBillingProofRejectsPricingExtraMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-pricing-extra-mismatch","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":2,"cost_points":620,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","pricing_source":"model_catalog","pricing_mode":"token","settlement":"extra_charged","pricing_snapshot":{"pricing_source":"model_catalog","pricing_mode":"token","settlement":"extra_charged","pre_deduct_points":500,"actual_points":620,"refund_points":0,"extra_points":0},"model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-pricing-extra-mismatch/billing":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"task_id":"task-pricing-extra-mismatch","consume_record":{"id":1,"task_id":"task-pricing-extra-mismatch","user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","count":1,"unit_points":620,"total_points":620,"status":1,"created_at":1,"updated_at":2},"wallet_logs":[{"id":10,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-pricing-extra-mismatch","points":-500,"points_before":1000,"points_after":500,"created_at":1},{"id":11,"user_id":22,"direction":-1,"biz_type":"consume","biz_id":"task-pricing-extra-mismatch:extra","points":-120,"points_before":500,"points_after":380,"remark":"extra usage","created_at":2}],"refund_records":[],"summary":{"consume_record_found":true,"consume_status":1,"consume_total_points":620,"wallet_log_count":2,"refund_record_count":0,"wallet_net_points":-620,"wallet_spend_points":620,"wallet_refund_points":0}}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminBillingProof(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected pricing extra mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "pricing snapshot extra_points mismatch") {
		t.Fatalf("expected pricing extra mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminAuditSnapshotRejectsSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"api_key":"should-not-leak","candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", "route")
	if res.Status != "error" {
		t.Fatalf("expected secret leak to fail, got %#v", res)
	}
}

func TestFindAdminAuditRouteSampleRejectsSummarySnapshotSourceMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected source mismatch to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot source_type mismatch") {
		t.Fatalf("expected source mismatch detail, got %#v", errResult.Details)
	}
}

func TestCheckAdminAuditSnapshotRejectsSummarySnapshotSourceMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	res := checkAdminAuditSnapshot(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", "route")
	if res.Status != "error" {
		t.Fatalf("expected source mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "route snapshot source_type mismatch") {
		t.Fatalf("expected source mismatch detail, got %#v", res.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsSummarySnapshotUpstreamMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"other-model"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected upstream mismatch to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot upstream_model mismatch") {
		t.Fatalf("expected upstream mismatch detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsMissingSelectedIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected missing selected_index to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot missing selected candidate") {
		t.Fatalf("expected missing selected candidate detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsInvalidSelectedIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":9,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected invalid selected_index to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot missing selected candidate") {
		t.Fatalf("expected missing selected candidate detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsCandidateMissingIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected missing candidate index to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot candidate 0 missing index") {
		t.Fatalf("expected missing candidate index detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsMissingCandidateCount(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected missing candidate_count to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot missing candidate_count") {
		t.Fatalf("expected missing candidate_count detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsCandidateCountMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":2,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected candidate_count mismatch to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot candidate_count mismatch") {
		t.Fatalf("expected candidate_count mismatch detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsSkippedCountMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}],"skipped_count":2,"skipped_candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","skip_reason":"account_pool_mismatch"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected skipped_count mismatch to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "route snapshot skipped_count mismatch") {
		t.Fatalf("expected skipped_count mismatch detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsSkippedCandidateMissingReason(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}],"skipped_count":1,"skipped_candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected skipped candidate missing reason to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "missing skip_reason") {
		t.Fatalf("expected missing skip_reason detail, got %#v", errResult.Details)
	}
}

func TestFindAdminAuditRouteSampleRejectsSkippedCandidateMissingSourceCode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/audit" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}],"skipped_count":1,"skipped_candidates":[{"index":1,"source_type":"account_pool","skip_reason":"account_pool_mismatch"}]}}],"total":1,"page":1,"page_size":20}}`), nil
	})}

	sample, _, errResult := findAdminAuditRouteSample(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if errResult == nil || errResult.Status != "error" {
		t.Fatalf("expected skipped candidate missing source_code to fail, sample=%#v err=%#v", sample, errResult)
	}
	if !containsDetail(errResult.Details, "missing source_code") {
		t.Fatalf("expected missing source_code detail, got %#v", errResult.Details)
	}
}

func TestCheckAdminUpstreamLogMatchesSelectedAPIChannelRoute(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			if got := r.URL.Query().Get("audit_type"); got != "route" {
				t.Fatalf("unexpected audit_type query %q", got)
			}
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro","strategy":"weighted_rr","auth_type":"api_key"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\",\"strategy\":\"weighted_rr\",\"auth_type\":\"api_key\",\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:          "http://unit.test/admin/api/v1",
		ModelCode:          "mimo-v2.5-pro",
		APIChannelCode:     "mimo-official",
		RequireUpstreamLog: true,
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected upstream log check to pass, got %#v", res)
	}
}

func TestCheckAdminUpstreamLogRejectsMissingRouteAttemptMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing route attempt meta to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing model_gateway_route_index") {
		t.Fatalf("expected missing route_index detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsAPIChannelMissingRouteMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing api_channel route meta to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing model_gateway_source_type") {
		t.Fatalf("expected missing api_channel route meta detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsAPIChannelStrategyMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro","strategy":"weighted_rr","auth_type":"api_key"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\",\"strategy\":\"round_robin\",\"auth_type\":\"api_key\",\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected api_channel strategy mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "api_channel strategy mismatch") {
		t.Fatalf("expected api_channel strategy mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogFailsWrongProvider(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"gpt","stage":"chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected wrong provider to fail, got %#v", res)
	}
}

func TestCheckAdminUpstreamLogRejectsUpstreamModelMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"other-model\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected upstream model mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "upstream_model mismatch") {
		t.Fatalf("expected upstream_model mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsAccountPoolProviderMismatchWithStage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":1,"cost_points":700,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"grok","stage":"images_edits.failed","method":"POST","status_code":503,"duration_ms":123,"meta":"{}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "gpt-image-2",
		UpstreamStage:  "images_edits.failed",
		APIChannelCode: "",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected account_pool provider mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no upstream log matched selected route account_pool/gpt stage=images_edits.failed") {
		t.Fatalf("expected no matching account_pool detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogMatchesSelectedAccountPoolRouteMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":1,"cost_points":700,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"gpt-image-2","strategy":"round_robin","auth_type":"api_key"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"gpt","stage":"images_edits.failed","method":"POST","status_code":503,"duration_ms":123,"meta":"{\"model_gateway_source_type\":\"account_pool\",\"model_gateway_source_code\":\"gpt\",\"upstream_model\":\"gpt-image-2\",\"strategy\":\"round_robin\",\"auth_type\":\"api_key\",\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:     "http://unit.test/admin/api/v1",
		ModelCode:     "gpt-image-2",
		UpstreamStage: "images_edits.failed",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected account_pool upstream log check to pass, got %#v", res)
	}
}

func TestCheckAdminUpstreamLogRejectsAccountPoolMissingRouteMeta(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":1,"cost_points":700,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"gpt","stage":"images_edits.failed","method":"POST","status_code":503,"duration_ms":123,"meta":"{}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:     "http://unit.test/admin/api/v1",
		ModelCode:     "gpt-image-2",
		UpstreamStage: "images_edits.failed",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing account_pool route meta to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing model_gateway_source_type") {
		t.Fatalf("expected missing account_pool route meta detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsAccountPoolUpstreamModelMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":1,"cost_points":700,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"gpt-image-2"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"gpt","stage":"images_edits.failed","method":"POST","status_code":503,"duration_ms":123,"meta":"{\"model_gateway_source_type\":\"account_pool\",\"model_gateway_source_code\":\"gpt\",\"upstream_model\":\"other-model\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:     "http://unit.test/admin/api/v1",
		ModelCode:     "gpt-image-2",
		UpstreamStage: "images_edits.failed",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected account_pool upstream_model mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "account_pool upstream_model mismatch") {
		t.Fatalf("expected account_pool upstream_model mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsAccountPoolAuthTypeMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"image","model_code":"gpt-image-2","status":1,"cost_points":700,"selected_source_type":"account_pool","selected_source_code":"gpt","selected_upstream_model":"gpt-image-2","model_gateway_route_snapshot":{"model_code":"gpt-image-2","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","upstream_model":"gpt-image-2","strategy":"round_robin","auth_type":"api_key"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"gpt","stage":"images_edits.failed","method":"POST","status_code":503,"duration_ms":123,"meta":"{\"model_gateway_source_type\":\"account_pool\",\"model_gateway_source_code\":\"gpt\",\"upstream_model\":\"gpt-image-2\",\"strategy\":\"round_robin\",\"auth_type\":\"oauth\",\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:     "http://unit.test/admin/api/v1",
		ModelCode:     "gpt-image-2",
		UpstreamStage: "images_edits.failed",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected account_pool auth_type mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "account_pool auth_type mismatch") {
		t.Fatalf("expected account_pool auth_type mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminUpstreamLogRejectsMetaSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"api_key\":\"should-not-leak\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminUpstreamLog(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected upstream meta secret leak to fail, got %#v", res)
	}
}

func TestCheckAdminKeyUsageFeedbackRequiresKeyPoolUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\",\"api_channel_credential_source\":\"key_pool\",\"api_channel_key_id\":7,\"api_channel_key_name\":\"primary\",\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/api-channels/42/keys":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":7,"channel_id":42,"name":"primary","has_api_key":true,"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"status":1,"last_used_at":123,"created_at":1,"updated_at":2}]}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminKeyUsageFeedback(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", 42)
	if res.Status != "ok" {
		t.Fatalf("expected key usage feedback to pass, got %#v", res)
	}
}

func TestCheckAdminKeyUsageFeedbackFailsLegacyCredential(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\",\"api_channel_credential_source\":\"legacy\"}","created_at":1}]}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminKeyUsageFeedback(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", 42)
	if res.Status != "error" {
		t.Fatalf("expected legacy credential usage to fail, got %#v", res)
	}
}

func TestCheckAdminKeyUsageFeedbackFailsMissingLastUsedAt(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/audit":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"task_id":"task-1","created_at":1,"user_id":22,"kind":"chat","model_code":"mimo-v2.5-pro","status":1,"cost_points":483,"selected_source_type":"api_channel","selected_source_code":"mimo-official","selected_upstream_model":"mimo-v2.5-pro","model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"candidate_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro"}]}}],"total":1,"page":1,"page_size":20}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/logs/generations/task-1/upstream":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":9,"task_id":"task-1","provider":"api_channel","stage":"api_channel.chat.completions","method":"POST","status_code":200,"duration_ms":123,"meta":"{\"api_channel_code\":\"mimo-official\",\"model_gateway_source_type\":\"api_channel\",\"model_gateway_source_code\":\"mimo-official\",\"upstream_model\":\"mimo-v2.5-pro\",\"api_channel_credential_source\":\"key_pool\",\"api_channel_key_id\":7,\"model_gateway_route_index\":1,\"model_gateway_attempt\":1}","created_at":1}]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/api-channels/42/keys":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":7,"channel_id":42,"name":"primary","has_api_key":true,"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"status":1,"created_at":1,"updated_at":2}]}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	res := checkAdminKeyUsageFeedback(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		ModelCode:      "mimo-v2.5-pro",
		APIChannelCode: "mimo-official",
	}, "test-token", 42)
	if res.Status != "error" {
		t.Fatalf("expected missing last_used_at to fail, got %#v", res)
	}
}

func TestCheckAdminCatalogModelAndSourcesValidateReadOnlyShapes(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/models":
			if got := r.URL.Query().Get("keyword"); got != "mimo-v2.5-pro" {
				t.Fatalf("unexpected keyword query %q", got)
			}
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":11,"model_code":"mimo-v2.5-pro","display_name":"MiMo 2.5 Pro","entry_kind":"text","provider_hint":"mimo","upstream_default_model":"mimo-v2.5-pro","capabilities":["text"],"parameters_schema":{"controls":[{"key":"temperature","type":"number"}]},"pricing_mode":"token","unit_points":0,"input_unit_points":1,"output_unit_points":3,"min_plan":"free","tags":["official_api"],"sort_order":10,"visible":1,"status":1,"created_at":1,"updated_at":2}],"total":1,"page":1,"page_size":50}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/model-gateway/sources":
			if got := r.URL.Query().Get("model_code"); got != "mimo-v2.5-pro" {
				t.Fatalf("unexpected model_code query %q", got)
			}
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":21,"model_code":"mimo-v2.5-pro","source_type":"api_channel","source_code":"mimo-official","upstream_model":"mimo-v2.5-pro","adapter":"openai_compatible_chat","auth_type":"api_key","strategy":"weighted","priority":1,"weight":100,"status":1,"created_at":1,"updated_at":2}],"total":1,"page":1,"page_size":100}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	opts := options{
		AdminBase:              "http://unit.test/admin/api/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "text",
		APIChannelCode:         "mimo-official",
		RequireCatalogModel:    true,
		RequireParameterSchema: true,
		RequireSourceMapping:   true,
	}
	modelRes := checkAdminCatalogModel(context.Background(), client, opts, "test-token")
	if modelRes.Status != "ok" {
		t.Fatalf("expected catalog model check to pass, got %#v", modelRes)
	}
	sourceRes := checkAdminModelSources(context.Background(), client, opts, "test-token")
	if sourceRes.Status != "ok" {
		t.Fatalf("expected source mapping check to pass, got %#v", sourceRes)
	}
}

func TestCheckAdminCatalogModelRequiresExpectedModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":11,"model_code":"deepseek-chat","display_name":"DeepSeek Chat","entry_kind":"text","pricing_mode":"token","visible":1,"status":1}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res := checkAdminCatalogModel(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		RequireCatalogModel: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing catalog model to fail, got %#v", res)
	}
}

func TestCheckAdminCatalogModelRequiresExpectedPricingMode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":11,"model_code":"mimo-v2.5-pro","display_name":"MiMo 2.5 Pro","entry_kind":"text","pricing_mode":"token","unit_points":0,"input_unit_points":1,"output_unit_points":3,"visible":1,"status":1}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res := checkAdminCatalogModel(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		EntryKind:           "text",
		ExpectedPricingMode: "char",
		RequireCatalogModel: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected catalog pricing_mode mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "catalog pricing_mode mismatch") {
		t.Fatalf("expected catalog pricing_mode mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminCatalogModelRequiresEffectivePricingForPostGenerationProof(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":11,"model_code":"mimo-v2.5-pro","display_name":"MiMo 2.5 Pro","entry_kind":"text","pricing_mode":"manual","unit_points":0,"input_unit_points":0,"output_unit_points":0,"visible":1,"status":1}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res := checkAdminCatalogModel(context.Background(), client, options{
		AdminBase:                  "http://unit.test/admin/api/v1",
		ModelCode:                  "mimo-v2.5-pro",
		EntryKind:                  "text",
		RequireCatalogModel:        true,
		RequirePostGenerationProof: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected empty catalog pricing to fail post-generation proof, got %#v", res)
	}
	if !containsDetail(res.Details, "Model Catalog pricing") {
		t.Fatalf("expected effective pricing detail, got %#v", res.Details)
	}
}

func TestCheckAdminCatalogModelRequiresParameterSchema(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":11,"model_code":"mimo-v2.5-pro","display_name":"MiMo 2.5 Pro","entry_kind":"text","pricing_mode":"token","unit_points":0,"input_unit_points":1,"output_unit_points":3,"visible":1,"status":1}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res := checkAdminCatalogModel(context.Background(), client, options{
		AdminBase:              "http://unit.test/admin/api/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "text",
		RequireCatalogModel:    true,
		RequireParameterSchema: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing catalog parameter schema to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "parameters_schema") {
		t.Fatalf("expected parameters_schema detail, got %#v", res.Details)
	}
}

func TestCheckAdminModelSourcesRequiresExpectedMapping(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/sources" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":21,"model_code":"mimo-v2.5-pro","source_type":"account_pool","source_code":"gpt","upstream_model":"mimo-v2.5-pro","strategy":"weighted","priority":1,"weight":100,"status":1}],"total":1,"page":1,"page_size":100}}`), nil
	})}

	res := checkAdminModelSources(context.Background(), client, options{
		AdminBase:            "http://unit.test/admin/api/v1",
		ModelCode:            "mimo-v2.5-pro",
		APIChannelCode:       "mimo-official",
		RequireSourceMapping: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing API Channel source mapping to fail, got %#v", res)
	}
}

func TestCheckAdminSourceConflictsAllowsEmptyList(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/source-conflicts" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":[]}`), nil
	})}

	res := checkAdminSourceConflicts(context.Background(), client, options{
		AdminBase:                "http://unit.test/admin/api/v1",
		ModelCode:                "mimo-v2.5-pro",
		RequireNoSourceConflicts: true,
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected empty source-conflicts to pass, got %#v", res)
	}
}

func TestCheckAdminSourceConflictsFailsForExpectedModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/source-conflicts" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":31,"model_code":"mimo-v2.5-pro","source_type":"account_pool","source_code":"gpt","upstream_model":"mimo-v2.5-pro","status":1,"reason":"MiMo official API model should not bind to GPT account pool"}]}`), nil
	})}

	res := checkAdminSourceConflicts(context.Background(), client, options{
		AdminBase:                "http://unit.test/admin/api/v1",
		ModelCode:                "mimo-v2.5-pro",
		RequireNoSourceConflicts: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected source conflict for model to fail, got %#v", res)
	}
}

func TestCheckAdminSourceConflictsIgnoresOtherModelWhenModelScoped(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/model-gateway/source-conflicts" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":[{"id":31,"model_code":"deepseek-chat","source_type":"account_pool","source_code":"gpt","upstream_model":"deepseek-chat","status":1,"reason":"DeepSeek official API model should not bind to GPT account pool"}]}`), nil
	})}

	res := checkAdminSourceConflicts(context.Background(), client, options{
		AdminBase:                "http://unit.test/admin/api/v1",
		ModelCode:                "mimo-v2.5-pro",
		RequireNoSourceConflicts: true,
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected other model conflict to pass in model-scoped mode, got %#v", res)
	}
}

func TestCheckAdminDryRunValidatesCandidateShape(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected dry-run to pass, got %#v", res)
	}
}

func TestCheckAdminDryRunRejectsSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true,"api_key":"should-not-leak"}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected dry-run secret leak to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "dry-run response leaks sensitive fields") {
		t.Fatalf("expected secret leak detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsEntryKindMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"image","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected entry_kind mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "entry_kind mismatch") {
		t.Fatalf("expected entry_kind mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsCandidateMissingIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing candidate index to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing positive index") {
		t.Fatalf("expected missing index detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsInvalidSelectedIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":9,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected invalid selected_index to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "does not match any candidate") {
		t.Fatalf("expected selected index mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsDuplicateCandidateIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":2,"available_count":2,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true},{"index":1,"source_type":"api_channel","source_code":"deepseek-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected duplicate candidate index to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "duplicate candidate index") {
		t.Fatalf("expected duplicate index detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsCandidateIndexOutOfOrder(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":2,"candidate_count":1,"available_count":1,"candidates":[{"index":2,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected out-of-order candidate index to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "does not match expected 1") {
		t.Fatalf("expected out-of-order index detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsMissingRequiredCounters(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":0,"available_count":0,"candidates":[]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing candidate_count to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing required field candidate_count") {
		t.Fatalf("expected missing candidate_count detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsMissingCandidatesArray(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":0,"candidate_count":0,"available_count":0}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected missing candidates to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing required field candidates") {
		t.Fatalf("expected missing candidates detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsNonnumericAvailableCount(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":0,"candidate_count":0,"available_count":"0","candidates":[]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected nonnumeric available_count to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "field available_count must be numeric") {
		t.Fatalf("expected nonnumeric available_count detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsNegativeSelectedIndex(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":-1,"candidate_count":0,"available_count":0,"candidates":[]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected negative selected_index to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "selected_index must be zero or positive") {
		t.Fatalf("expected negative selected_index detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsAvailableCountMismatch(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":2,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected available_count mismatch to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "available_count mismatch") {
		t.Fatalf("expected available_count mismatch detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsUnavailableCandidateMissingSkipReason(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":2,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true},{"index":2,"source_type":"account_pool","source_code":"gpt","available":false}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected unavailable candidate missing skip_reason to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "missing skip_reason") {
		t.Fatalf("expected missing skip_reason detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRejectsAvailableCandidateWithSkipReason(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true,"skip_reason":"should-not-exist"}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
		ModelCode: "mimo-v2.5-pro",
		EntryKind: "text",
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected available candidate with skip_reason to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "available but has skip_reason") {
		t.Fatalf("expected available skip_reason detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunRequiresSelectedAPIChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		EntryKind:           "text",
		APIChannelCode:      "mimo-official",
		RequireRouteChannel: true,
	}, "test-token")
	if res.Status != "ok" {
		t.Fatalf("expected required route channel to pass, got %#v", res)
	}
}

func TestCheckAdminDryRunFailsWhenSelectedRouteIsWrongChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":1,"candidates":[{"index":1,"source_type":"account_pool","source_code":"gpt","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		EntryKind:           "text",
		APIChannelCode:      "mimo-official",
		RequireRouteChannel: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected wrong selected route to fail, got %#v", res)
	}
}

func TestCheckAdminDryRunFailsWhenSelectedAPIChannelUnavailable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":1,"available_count":0,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":false,"skip_reason":"api_channel health check failed"}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase:           "http://unit.test/admin/api/v1",
		ModelCode:           "mimo-v2.5-pro",
		EntryKind:           "text",
		APIChannelCode:      "mimo-official",
		RequireRouteChannel: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected unavailable selected API Channel to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "points to unavailable candidate") {
		t.Fatalf("expected unavailable candidate detail, got %#v", res.Details)
	}
}

func TestCheckAdminDryRunForbidsAvailableAccountPoolCandidate(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/api/v1/model-gateway/dry-run" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"model_code":"mimo-v2.5-pro","entry_kind":"text","matched_model":true,"selected_index":1,"candidate_count":2,"available_count":2,"candidates":[{"index":1,"source_type":"api_channel","source_code":"mimo-official","available":true},{"index":2,"source_type":"account_pool","source_code":"gpt","available":true}]}}`), nil
	})}

	res := checkAdminDryRun(context.Background(), client, options{
		AdminBase:              "http://unit.test/admin/api/v1",
		ModelCode:              "mimo-v2.5-pro",
		EntryKind:              "text",
		ForbidAccountPoolRoute: true,
	}, "test-token")
	if res.Status != "error" {
		t.Fatalf("expected available account_pool candidate to fail, got %#v", res)
	}
}

func TestCheckAdminAPIChannelsAndKeysValidateReadOnlyShapes(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/api-channels":
			if got := r.URL.Query().Get("keyword"); got != "mimo-official" {
				t.Fatalf("unexpected keyword query %q", got)
			}
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","provider_name":"mimo","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":true,"key_count":1,"enabled_key_count":1,"models":["mimo-v2.5-pro"],"capabilities":["text"],"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"timeout_seconds":300,"status":1,"created_at":1,"updated_at":2}],"total":1,"page":1,"page_size":50}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/admin/api/v1/api-channels/42/keys":
			return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":7,"channel_id":42,"name":"primary","has_api_key":true,"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"status":1,"last_used_at":123,"last_error":"upstream 429","created_at":1,"updated_at":2}]}}`), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	opts := options{
		AdminBase:         "http://unit.test/admin/api/v1",
		APIChannelCode:    "mimo-official",
		RequireAPIChannel: true,
		RequireKeyPool:    true,
	}
	res, channelID := checkAdminAPIChannels(context.Background(), client, opts, "test-token")
	if res.Status != "ok" || channelID != 42 {
		t.Fatalf("expected API channel list to pass and select channel 42, got %#v id=%d", res, channelID)
	}
	keyRes := checkAdminAPIChannelKeys(context.Background(), client, opts, "test-token", channelID)
	if keyRes.Status != "ok" {
		t.Fatalf("expected API channel key list to pass, got %#v", keyRes)
	}
}

func TestCheckAdminAPIChannelsForbidsLegacyChannelKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":true,"key_count":1,"enabled_key_count":1,"status":1,"last_test_status":1,"last_test_at":1770000000}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res, channelID := checkAdminAPIChannels(context.Background(), client, options{
		AdminBase:              "http://unit.test/admin/api/v1",
		APIChannelCode:         "mimo-official",
		ForbidLegacyChannelKey: true,
	}, "test-token")
	if res.Status != "error" || channelID != 0 {
		t.Fatalf("expected legacy channel key to fail, got %#v id=%d", res, channelID)
	}
	if !containsDetail(res.Details, "still has legacy channel API key") {
		t.Fatalf("expected legacy key failure detail, got %#v", res.Details)
	}
}

func TestCheckAdminAPIChannelsAcceptsKeyPoolOnlyChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":false,"key_count":1,"enabled_key_count":1,"status":1,"last_test_status":1,"last_test_at":1770000000}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res, channelID := checkAdminAPIChannels(context.Background(), client, options{
		AdminBase:              "http://unit.test/admin/api/v1",
		APIChannelCode:         "mimo-official",
		ForbidLegacyChannelKey: true,
	}, "test-token")
	if res.Status != "ok" || channelID != 42 {
		t.Fatalf("expected key-pool-only API channel to pass, got %#v id=%d", res, channelID)
	}
	if !containsDetail(res.Details, "legacy_channel_key=false") {
		t.Fatalf("expected key-pool-only detail, got %#v", res.Details)
	}
}

func TestCheckAdminAPIChannelsRequiresHealthOK(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":true,"key_count":1,"enabled_key_count":1,"status":1,"last_test_status":1,"last_test_at":1770000000}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res, channelID := checkAdminAPIChannels(context.Background(), client, options{
		AdminBase:               "http://unit.test/admin/api/v1",
		APIChannelCode:          "mimo-official",
		RequireAPIChannelHealth: true,
	}, "test-token")
	if res.Status != "ok" || channelID != 42 {
		t.Fatalf("expected healthy API channel to pass, got %#v id=%d", res, channelID)
	}
}

func TestCheckAdminAPIChannelsRejectsUnhealthyRequiredChannel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":true,"key_count":1,"enabled_key_count":1,"status":1,"last_test_status":2,"last_test_at":1770000000}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res, channelID := checkAdminAPIChannels(context.Background(), client, options{
		AdminBase:               "http://unit.test/admin/api/v1",
		APIChannelCode:          "mimo-official",
		RequireAPIChannelHealth: true,
	}, "test-token")
	if res.Status != "error" || channelID != 0 {
		t.Fatalf("expected unhealthy API channel to fail, got %#v id=%d", res, channelID)
	}
	if !containsDetail(res.Details, "health check not OK") {
		t.Fatalf("expected health failure detail, got %#v", res.Details)
	}
}

func TestCheckOptionRequirementsRequiresAPIChannelForHealthGate(t *testing.T) {
	res := checkOptionRequirements(options{RequireAPIChannelHealth: true})
	if len(res) != 1 || res[0].Status != "error" {
		t.Fatalf("expected missing api-channel to fail, got %#v", res)
	}
	if !containsDetail(res[0].Details, "--require-api-channel-health needs --api-channel") {
		t.Fatalf("expected missing api-channel detail, got %#v", res[0].Details)
	}
}

func TestCheckOptionRequirementsRequiresTargetsForStandaloneGates(t *testing.T) {
	res := checkOptionRequirements(options{
		RequireAPIChannel:       true,
		RequireKeyPool:          true,
		RequireSourceMapping:    true,
		RequireRouteChannel:     true,
		ForbidAccountPoolRoute:  true,
		RequirePricingMode:      true,
		RequireKeyUsageFeedback: true,
		RequireOutputProof:      true,
	})
	if len(res) == 0 {
		t.Fatalf("expected missing standalone target options to fail")
	}
	joined := strings.Join(flattenDetails(res), " ")
	for _, want := range []string{
		"--require-api-channel needs --api-channel",
		"--require-key-pool needs --api-channel",
		"--require-source-mapping needs --model and --api-channel",
		"--require-route-channel needs --model and --api-channel",
		"--forbid-account-pool-route needs --model",
		"--require-pricing-mode or --pricing-mode needs --model",
		"--require-key-usage-feedback needs --api-channel and --model or --task-id",
		"proof gates need --model or --task-id",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in option errors, got %q", want, joined)
		}
	}
}

func TestCheckOptionRequirementsAllowsTaskBoundStandaloneAuditGates(t *testing.T) {
	res := checkOptionRequirements(options{
		TaskID:                  "task-proof",
		APIChannelCode:          "mimo-official",
		RequireKeyUsageFeedback: true,
		RequireOutputProof:      true,
		RequireUpstreamLog:      true,
		RequireBillingProof:     true,
	})
	if len(res) != 0 {
		t.Fatalf("expected task-bound standalone proof options to pass, got %#v", res)
	}
}

func TestCheckAdminAPIChannelsRejectsSecretLeak(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":42,"code":"mimo-official","name":"MiMo Official","adapter":"openai_compatible_chat","base_url":"https://unit.test/v1","has_api_key":true,"status":1,"api_key":"should-not-leak"}],"total":1,"page":1,"page_size":50}}`), nil
	})}

	res, channelID := checkAdminAPIChannels(context.Background(), client, options{
		AdminBase: "http://unit.test/admin/api/v1",
	}, "test-token")
	if res.Status != "error" || channelID != 0 {
		t.Fatalf("expected leaked API key to fail, got %#v id=%d", res, channelID)
	}
}

func TestCheckAdminAPIChannelKeysRequiresConfiguredPool(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels/42/keys" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[]}}`), nil
	})}

	res := checkAdminAPIChannelKeys(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		RequireKeyPool: true,
	}, "test-token", 42)
	if res.Status != "error" {
		t.Fatalf("expected empty key pool to fail when required, got %#v", res)
	}
}

func TestCheckAdminAPIChannelKeysRequiresEnabledUsableKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/api/v1/api-channels/42/keys" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return jsonResponse(200, `{"code":0,"msg":"ok","data":{"list":[{"id":7,"channel_id":42,"name":"disabled","has_api_key":true,"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"status":0},{"id":8,"channel_id":42,"name":"missing-key","has_api_key":false,"priority":1,"weight":100,"rpm_limit":60,"tpm_limit":120000,"status":1}]}}`), nil
	})}

	res := checkAdminAPIChannelKeys(context.Background(), client, options{
		AdminBase:      "http://unit.test/admin/api/v1",
		RequireKeyPool: true,
	}, "test-token", 42)
	if res.Status != "error" {
		t.Fatalf("expected key pool without enabled usable key to fail, got %#v", res)
	}
	if !containsDetail(res.Details, "no enabled key-pool rows with api key") {
		t.Fatalf("expected enabled usable key detail, got %#v", res.Details)
	}
}
