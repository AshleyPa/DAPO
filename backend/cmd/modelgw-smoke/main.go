// Command modelgw-smoke verifies the read-only Model Gateway API surface.
//
// It never creates generation tasks, never calls an upstream model, and never
// prints admin or OpenAI-compatible tokens. Admin checks run only when a Bearer
// token is supplied via an environment variable.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type options struct {
	APIBase                    string
	OpenAIBase                 string
	OpenAIAPIKeyEnv            string
	RequireOpenAIAuth          bool
	AdminBase                  string
	AdminTokenEnv              string
	ModelCode                  string
	EntryKind                  string
	APIChannelCode             string
	TaskID                     string
	RequireAdmin               bool
	RequireModel               bool
	RequireCatalogModel        bool
	RequireAPIChannel          bool
	RequireAPIChannelHealth    bool
	RequireKeyPool             bool
	ForbidLegacyChannelKey     bool
	RequireSourceMapping       bool
	RequireRouteChannel        bool
	RequireNoSourceConflicts   bool
	ForbidAccountPoolRoute     bool
	RequirePricingMode         bool
	ExpectedPricingMode        string
	RequireAuditRoute          bool
	RequireAuditPricing        bool
	AuditPricingSource         string
	RequireParameterSchema     bool
	RequireOutputProof         bool
	RequireVideoJobProof       bool
	RequireUpstreamLog         bool
	RequireKeyUsageFeedback    bool
	RequireBillingProof        bool
	RequirePostGenerationProof bool
	EvidenceTemplate           bool
	EvidenceVerifyPath         string
	RequireDeploymentApproval  bool
	TargetEnv                  string
	UpstreamStage              string
	Timeout                    time.Duration
}

type apiBody struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type checkResult struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Details []string `json:"details,omitempty"`
}

type report struct {
	OK        bool          `json:"ok"`
	StartedAt int64         `json:"started_at"`
	Checks    []checkResult `json:"checks"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseOptions()
	if opts.EvidenceTemplate {
		return printEvidenceTemplate(os.Stdout, opts)
	}
	if opts.EvidenceVerifyPath != "" {
		return printEvidenceVerification(os.Stdout, opts.EvidenceVerifyPath, opts.RequireDeploymentApproval)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	client := &http.Client{Timeout: opts.Timeout}
	rep := report{OK: true, StartedAt: time.Now().Unix(), Checks: []checkResult{}}

	for _, validation := range checkOptionRequirements(opts) {
		rep.add(validation)
	}
	rep.add(checkPublicModels(ctx, client, opts))
	rep.add(checkOpenAIModels(ctx, client, opts))
	if opts.ModelCode != "" && (opts.RequirePricingMode || opts.ExpectedPricingMode != "") {
		rep.add(checkPublicPricingModeConsistency(ctx, client, opts))
	}

	token := strings.TrimSpace(os.Getenv(opts.AdminTokenEnv))
	if token == "" {
		status := "warn"
		if opts.RequireAdmin ||
			opts.RequireCatalogModel ||
			opts.RequireAPIChannel ||
			opts.RequireAPIChannelHealth ||
			opts.RequireKeyPool ||
			opts.ForbidLegacyChannelKey ||
			opts.RequireSourceMapping ||
			opts.RequireRouteChannel ||
			opts.RequireNoSourceConflicts ||
			opts.ForbidAccountPoolRoute ||
			opts.RequireAuditRoute ||
			opts.RequireAuditPricing ||
			opts.RequireParameterSchema ||
			opts.RequireOutputProof ||
			opts.RequireVideoJobProof ||
			opts.RequireUpstreamLog ||
			opts.RequireKeyUsageFeedback ||
			opts.RequireBillingProof ||
			opts.RequirePostGenerationProof {
			status = "error"
		}
		rep.add(checkResult{
			Name:   "admin_token",
			Status: status,
			Details: []string{
				fmt.Sprintf("%s is empty; skipped admin-only audit, Model Catalog, Model Source Mapping, API Channel, key-pool and dry-run checks", opts.AdminTokenEnv),
			},
		})
	} else {
		rep.add(checkAdminAudit(ctx, client, opts, token))
		if opts.RequireAuditRoute {
			rep.add(checkAdminAuditSnapshot(ctx, client, opts, token, "route"))
		}
		if opts.RequireAuditPricing {
			rep.add(checkAdminAuditSnapshot(ctx, client, opts, token, "pricing"))
		}
		if opts.RequirePostGenerationProof {
			rep.add(checkAdminRouteSamplePricingSnapshot(ctx, client, opts, token))
		}
		if opts.RequireOutputProof {
			rep.add(checkAdminAuditSnapshot(ctx, client, opts, token, "output"))
			rep.add(checkAdminOutputProof(ctx, client, opts, token))
		}
		if opts.RequireVideoJobProof {
			rep.add(checkAdminAuditSnapshot(ctx, client, opts, token, "video"))
			rep.add(checkAdminVideoJobProof(ctx, client, opts, token))
		}
		if opts.RequireUpstreamLog {
			rep.add(checkAdminUpstreamLog(ctx, client, opts, token))
		}
		apiChannels, selectedChannelID := checkAdminAPIChannels(ctx, client, opts, token)
		rep.add(apiChannels)
		if selectedChannelID > 0 {
			rep.add(checkAdminAPIChannelKeys(ctx, client, opts, token, selectedChannelID))
		} else if opts.RequireKeyPool {
			rep.add(checkResult{
				Name:   "admin_api_channel_keys",
				Status: "error",
				Details: []string{
					"cannot check required key pool because no API channel was selected",
					"pass --api-channel or create at least one API channel",
				},
			})
		}
		if opts.RequireKeyUsageFeedback {
			rep.add(checkAdminKeyUsageFeedback(ctx, client, opts, token, selectedChannelID))
		}
		if opts.RequireBillingProof {
			rep.add(checkAdminBillingProof(ctx, client, opts, token))
		}
		rep.add(checkAdminSourceConflicts(ctx, client, opts, token))
		if strings.TrimSpace(opts.ModelCode) != "" {
			rep.add(checkAdminCatalogModel(ctx, client, opts, token))
			rep.add(checkAdminModelSources(ctx, client, opts, token))
			rep.add(checkAdminDryRun(ctx, client, opts, token))
		}
	}

	for _, c := range rep.Checks {
		if c.Status == "error" {
			rep.OK = false
			break
		}
	}

	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	if !rep.OK {
		return errors.New("model gateway smoke failed")
	}
	return nil
}

func parseOptions() options {
	var opts options
	flag.StringVar(&opts.APIBase, "api-base", envOrDefault("DAPO_SMOKE_API_BASE", "http://127.0.0.1:17180/api/v1"), "user API base URL")
	flag.StringVar(&opts.OpenAIBase, "openai-base", envOrDefault("DAPO_SMOKE_OPENAI_BASE", "http://127.0.0.1:17200/v1"), "OpenAI-compatible API base URL")
	flag.StringVar(&opts.OpenAIAPIKeyEnv, "openai-api-key-env", envOrDefault("DAPO_SMOKE_OPENAI_API_KEY_ENV", "DAPO_SMOKE_OPENAI_API_KEY"), "environment variable that contains an OpenAI-compatible API key for /v1 checks")
	flag.BoolVar(&opts.RequireOpenAIAuth, "require-openai-auth", false, "fail when OpenAI-compatible /v1 checks are not performed with a provided API key")
	flag.StringVar(&opts.AdminBase, "admin-base", envOrDefault("DAPO_SMOKE_ADMIN_BASE", "http://127.0.0.1:17188/admin/api/v1"), "admin API base URL")
	flag.StringVar(&opts.AdminTokenEnv, "admin-token-env", envOrDefault("DAPO_SMOKE_ADMIN_TOKEN_ENV", "DAPO_ADMIN_TOKEN"), "environment variable that contains an admin Bearer token")
	flag.StringVar(&opts.ModelCode, "model", envOrDefault("DAPO_SMOKE_MODEL", ""), "optional model code expected in public model lists and admin dry-run")
	flag.StringVar(&opts.EntryKind, "entry-kind", envOrDefault("DAPO_SMOKE_ENTRY_KIND", ""), "optional entry kind for admin dry-run")
	flag.StringVar(&opts.APIChannelCode, "api-channel", envOrDefault("DAPO_SMOKE_API_CHANNEL", ""), "optional API Channel code expected in admin channel list and key-pool checks")
	flag.StringVar(&opts.TaskID, "task-id", envOrDefault("DAPO_SMOKE_TASK_ID", ""), "exact generation task_id for task-bound audit, upstream and output proof; required with --require-post-generation-proof")
	flag.BoolVar(&opts.RequireAdmin, "require-admin", false, "fail when the admin token env is missing")
	flag.BoolVar(&opts.RequireModel, "require-model", false, "fail when --model is not present in public model lists")
	flag.BoolVar(&opts.RequireCatalogModel, "require-catalog-model", false, "fail when --model is not present in the admin Model Catalog")
	flag.BoolVar(&opts.RequireAPIChannel, "require-api-channel", false, "fail when --api-channel is not present in admin API Channel list")
	flag.BoolVar(&opts.RequireAPIChannelHealth, "require-api-channel-health", false, "fail when --api-channel is disabled, untested, or last health check did not pass")
	flag.BoolVar(&opts.RequireKeyPool, "require-key-pool", false, "fail when the selected API Channel has no enabled key-pool row with API key")
	flag.BoolVar(&opts.ForbidLegacyChannelKey, "forbid-legacy-channel-key", false, "fail when the selected API Channel still has a channel-level legacy API key")
	flag.BoolVar(&opts.RequireSourceMapping, "require-source-mapping", false, "fail when admin Model Source Mapping does not bind --model to --api-channel")
	flag.BoolVar(&opts.RequireRouteChannel, "require-route-channel", false, "fail when admin dry-run does not select --api-channel as the API Channel route")
	flag.BoolVar(&opts.RequireNoSourceConflicts, "require-no-source-conflicts", false, "fail when admin source-conflicts reports mismatched account-pool mappings")
	flag.BoolVar(&opts.ForbidAccountPoolRoute, "forbid-account-pool-route", false, "fail when admin dry-run contains an available account_pool candidate")
	flag.BoolVar(&opts.RequirePricingMode, "require-pricing-mode", false, "fail when --model has no pricing_mode on public model surfaces")
	flag.StringVar(&opts.ExpectedPricingMode, "pricing-mode", envOrDefault("DAPO_SMOKE_PRICING_MODE", ""), "optional expected pricing_mode for --model across Model Catalog, public model surfaces and same-task pricing proof")
	flag.BoolVar(&opts.RequireAuditRoute, "require-audit-route-snapshot", false, "fail when admin Model Gateway audit has no route snapshot sample for --model")
	flag.BoolVar(&opts.RequireAuditPricing, "require-audit-pricing-snapshot", false, "fail when admin Model Gateway audit has no pricing snapshot sample for --model")
	flag.StringVar(&opts.AuditPricingSource, "audit-pricing-source", envOrDefault("DAPO_SMOKE_AUDIT_PRICING_SOURCE", ""), "optional expected pricing_source in required audit pricing samples")
	flag.BoolVar(&opts.RequireParameterSchema, "require-parameter-schema", false, "fail when --model has no Model Catalog parameters_schema")
	flag.BoolVar(&opts.RequireOutputProof, "require-output-proof", false, "fail when the selected succeeded audit route sample has no text output_snapshot or media preview_url proof")
	flag.BoolVar(&opts.RequireVideoJobProof, "require-video-job-proof", false, "fail when the selected video audit route sample has no _model_gateway_video_job proof")
	flag.BoolVar(&opts.RequireUpstreamLog, "require-upstream-log", false, "fail when the selected audit route sample has no matching upstream log")
	flag.BoolVar(&opts.RequireKeyUsageFeedback, "require-key-usage-feedback", false, "fail when the matched upstream API Channel log is not backed by a used key-pool row")
	flag.BoolVar(&opts.RequireBillingProof, "require-billing-proof", false, "fail when the selected audit route sample has no matching consume_record and wallet_log proof")
	flag.BoolVar(&opts.RequirePostGenerationProof, "require-post-generation-proof", false, "fail unless config, dry-run, succeeded same-task route/pricing/output audit, upstream log, key-pool usage and billing all prove the selected API Channel handled --model")
	flag.BoolVar(&opts.EvidenceTemplate, "evidence-template", false, "print a sanitized pre-launch Model Gateway evidence checklist without making HTTP requests")
	flag.StringVar(&opts.EvidenceVerifyPath, "evidence-verify", envOrDefault("DAPO_SMOKE_EVIDENCE_VERIFY", ""), "verify a completed pre-launch evidence checklist JSON; fails when any pre-deployment gate is missing or not passed")
	flag.BoolVar(&opts.RequireDeploymentApproval, "require-deployment-approval", false, "when used with --evidence-verify, also require deployment_approval to be passed")
	flag.StringVar(&opts.TargetEnv, "target-env", envOrDefault("DAPO_SMOKE_TARGET_ENV", ""), "optional target environment label for --evidence-template")
	flag.StringVar(&opts.UpstreamStage, "upstream-stage", envOrDefault("DAPO_SMOKE_UPSTREAM_STAGE", ""), "optional expected upstream log stage")
	flag.DurationVar(&opts.Timeout, "timeout", 15*time.Second, "overall smoke timeout")
	flag.Parse()
	opts.APIBase = strings.TrimRight(opts.APIBase, "/")
	opts.OpenAIBase = strings.TrimRight(opts.OpenAIBase, "/")
	opts.AdminBase = strings.TrimRight(opts.AdminBase, "/")
	opts.ModelCode = strings.TrimSpace(opts.ModelCode)
	opts.EntryKind = strings.TrimSpace(opts.EntryKind)
	opts.APIChannelCode = strings.TrimSpace(opts.APIChannelCode)
	opts.TaskID = strings.TrimSpace(opts.TaskID)
	opts.ExpectedPricingMode = normalizePricingMode(opts.ExpectedPricingMode)
	opts = normalizeOptions(opts)
	return opts
}

type evidenceTemplate struct {
	GeneratedAt    int64             `json:"generated_at"`
	TargetEnv      string            `json:"target_env"`
	Model          string            `json:"model"`
	EntryKind      string            `json:"entry_kind"`
	APIChannel     string            `json:"api_channel"`
	Provider       string            `json:"provider"`
	PricingMode    string            `json:"pricing_mode,omitempty"`
	TaskID         string            `json:"task_id,omitempty"`
	RequiredGates  []evidenceGate    `json:"required_gates"`
	StopConditions []string          `json:"stop_conditions"`
	Notes          []string          `json:"notes"`
	Placeholders   map[string]string `json:"placeholders"`
}

type evidenceGate struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	Collect  []string `json:"collect"`
	Evidence []string `json:"evidence,omitempty"`
	Command  string   `json:"command,omitempty"`
	PassWhen []string `json:"pass_when"`
}

type evidenceVerificationReport struct {
	OK                          bool     `json:"ok"`
	EvidencePath                string   `json:"evidence_path"`
	TargetEnv                   string   `json:"target_env"`
	Model                       string   `json:"model"`
	EntryKind                   string   `json:"entry_kind"`
	APIChannel                  string   `json:"api_channel"`
	RequireDeploymentApproval   bool     `json:"require_deployment_approval"`
	ReadyForDeploymentApproval  bool     `json:"ready_for_deployment_approval"`
	RequiredGateCount           int      `json:"required_gate_count"`
	PassedGateCount             int      `json:"passed_gate_count"`
	MissingGateIDs              []string `json:"missing_gate_ids,omitempty"`
	IncompleteGateIDs           []string `json:"incomplete_gate_ids,omitempty"`
	MissingEvidenceGateIDs      []string `json:"missing_evidence_gate_ids,omitempty"`
	InsufficientEvidenceGateIDs []string `json:"insufficient_evidence_gate_ids,omitempty"`
	InvalidEvidenceRefs         []string `json:"invalid_evidence_refs,omitempty"`
	MissingEvidenceArtifactRefs []string `json:"missing_evidence_artifact_refs,omitempty"`
	UnexpectedGateIDs           []string `json:"unexpected_gate_ids,omitempty"`
	OutOfOrderGateIDs           []string `json:"out_of_order_gate_ids,omitempty"`
	IdentityIssues              []string `json:"identity_issues,omitempty"`
	SecretLeakFindings          []string `json:"secret_leak_findings,omitempty"`
}

func printEvidenceTemplate(w io.Writer, opts options) error {
	tpl := buildEvidenceTemplate(opts, time.Now().Unix())
	raw, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(raw))
	return err
}

func printEvidenceVerification(w io.Writer, path string, requireDeploymentApproval bool) error {
	report, err := verifyEvidenceFile(path, requireDeploymentApproval)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	if !report.OK {
		return errors.New("pre-launch evidence verification failed")
	}
	return nil
}

func verifyEvidenceFile(path string, requireDeploymentApproval bool) (evidenceVerificationReport, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return evidenceVerificationReport{}, errors.New("--evidence-verify requires a JSON file path")
	}
	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		return evidenceVerificationReport{}, fmt.Errorf("read evidence checklist: %w", err)
	}
	var tpl evidenceTemplate
	if err := json.Unmarshal(raw, &tpl); err != nil {
		return evidenceVerificationReport{}, fmt.Errorf("parse evidence checklist JSON: %w", err)
	}
	report := verifyEvidenceTemplate(tpl, cleanPath, requireDeploymentApproval)
	report.SecretLeakFindings = append(report.SecretLeakFindings, evidenceSecretLeakFindings(raw)...)
	sort.Strings(report.SecretLeakFindings)
	report.OK = len(report.MissingGateIDs) == 0 &&
		len(report.IncompleteGateIDs) == 0 &&
		len(report.MissingEvidenceGateIDs) == 0 &&
		len(report.InsufficientEvidenceGateIDs) == 0 &&
		len(report.InvalidEvidenceRefs) == 0 &&
		len(report.MissingEvidenceArtifactRefs) == 0 &&
		len(report.UnexpectedGateIDs) == 0 &&
		len(report.OutOfOrderGateIDs) == 0 &&
		len(report.IdentityIssues) == 0 &&
		len(report.SecretLeakFindings) == 0
	report.ReadyForDeploymentApproval = report.OK && !requireDeploymentApproval
	return report, nil
}

func verifyEvidenceTemplate(tpl evidenceTemplate, path string, requireDeploymentApproval bool) evidenceVerificationReport {
	expected := canonicalEvidenceGateIDs()
	expectedIndex := map[string]int{}
	for i, id := range expected {
		expectedIndex[id] = i
	}
	report := evidenceVerificationReport{
		EvidencePath:              path,
		TargetEnv:                 tpl.TargetEnv,
		Model:                     tpl.Model,
		EntryKind:                 tpl.EntryKind,
		APIChannel:                tpl.APIChannel,
		RequireDeploymentApproval: requireDeploymentApproval,
		RequiredGateCount:         len(expected),
	}
	report.IdentityIssues = append(report.IdentityIssues, evidenceIdentityIssues(tpl)...)

	seen := map[string]bool{}
	lastIndex := -1
	evidenceBaseDir := evidenceReferenceBaseDir(path)
	for _, gate := range tpl.RequiredGates {
		id := strings.TrimSpace(gate.ID)
		idx, expectedGate := expectedIndex[id]
		if !expectedGate {
			report.UnexpectedGateIDs = append(report.UnexpectedGateIDs, id)
			continue
		}
		if idx < lastIndex {
			report.OutOfOrderGateIDs = append(report.OutOfOrderGateIDs, id)
		}
		if idx > lastIndex {
			lastIndex = idx
		}
		seen[id] = true
		if evidenceGatePassed(gate.Status) {
			report.PassedGateCount++
			if !evidenceGateHasRefs(gate) {
				report.MissingEvidenceGateIDs = append(report.MissingEvidenceGateIDs, id)
			} else {
				invalidRefs, missingArtifacts := evidenceGateReferenceIssues(id, gate.Evidence, evidenceBaseDir)
				report.InvalidEvidenceRefs = append(report.InvalidEvidenceRefs, invalidRefs...)
				report.MissingEvidenceArtifactRefs = append(report.MissingEvidenceArtifactRefs, missingArtifacts...)
				if !evidenceGateSatisfiesKindRequirements(id, gate.Evidence) {
					report.InsufficientEvidenceGateIDs = append(report.InsufficientEvidenceGateIDs, id)
				}
			}
			continue
		}
		if id == "deployment_approval" && !requireDeploymentApproval {
			continue
		}
		report.IncompleteGateIDs = append(report.IncompleteGateIDs, id)
	}
	for _, id := range expected {
		if !seen[id] {
			report.MissingGateIDs = append(report.MissingGateIDs, id)
		}
	}
	sort.Strings(report.MissingGateIDs)
	sort.Strings(report.IncompleteGateIDs)
	sort.Strings(report.MissingEvidenceGateIDs)
	sort.Strings(report.InsufficientEvidenceGateIDs)
	sort.Strings(report.InvalidEvidenceRefs)
	sort.Strings(report.MissingEvidenceArtifactRefs)
	sort.Strings(report.UnexpectedGateIDs)
	sort.Strings(report.OutOfOrderGateIDs)
	sort.Strings(report.IdentityIssues)
	sort.Strings(report.SecretLeakFindings)
	report.OK = len(report.MissingGateIDs) == 0 &&
		len(report.IncompleteGateIDs) == 0 &&
		len(report.MissingEvidenceGateIDs) == 0 &&
		len(report.InsufficientEvidenceGateIDs) == 0 &&
		len(report.InvalidEvidenceRefs) == 0 &&
		len(report.MissingEvidenceArtifactRefs) == 0 &&
		len(report.UnexpectedGateIDs) == 0 &&
		len(report.OutOfOrderGateIDs) == 0 &&
		len(report.IdentityIssues) == 0 &&
		len(report.SecretLeakFindings) == 0
	report.ReadyForDeploymentApproval = report.OK && !requireDeploymentApproval
	return report
}

func evidenceGateSatisfiesKindRequirements(gateID string, refs []string) bool {
	required := evidenceKindRequirementsForGate(gateID)
	if len(required) == 0 {
		return true
	}
	kinds := evidenceKinds(refs)
	for _, alternatives := range required {
		matched := false
		for _, kind := range alternatives {
			if kinds[kind] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func evidenceKindRequirementsForGate(gateID string) [][]string {
	switch gateID {
	case "local_preflight":
		return [][]string{{"log", "file"}}
	case "provider_probe", "config_plan", "db_target_check", "audit_only", "schema_check", "migration_dry_run", "authorized_write", "pre_generation_smoke", "post_generation_proof", "secret_scan", "backend_tests", "frontend_builds", "deployment_runbook":
		return [][]string{{"log", "file"}}
	case "controlled_generation_task":
		return [][]string{{"task_id"}, {"log", "file", "url", "screenshot", "image"}}
	case "frontend_preview_smoke":
		return [][]string{{"screenshot", "image"}, {"log", "file"}}
	case "admin_protected_pages_smoke":
		return [][]string{{"screenshot", "image"}, {"log", "file"}}
	case "release_manifest":
		return [][]string{{"commit", "release"}, {"artifact", "build", "release"}, {"backup", "db_backup"}, {"log", "file"}}
	case "deployment_approval":
		return [][]string{{"review", "note"}, {"backup", "db_backup", "release"}}
	default:
		return nil
	}
}

func evidenceKinds(refs []string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range refs {
		kind, _, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(kind))] = true
	}
	return out
}

func evidenceGateHasRefs(gate evidenceGate) bool {
	for _, item := range gate.Evidence {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

func evidenceReferenceBaseDir(path string) string {
	clean := strings.TrimSpace(path)
	if clean == "" || strings.Contains(clean, "://") {
		return "."
	}
	dir := filepath.Dir(clean)
	if dir == "" || dir == "." {
		return "."
	}
	return dir
}

func evidenceGateReferenceIssues(gateID string, refs []string, baseDir string) ([]string, []string) {
	var invalid []string
	var missing []string
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" || strings.HasPrefix(ref, "<") {
			invalid = append(invalid, gateID+":"+fallbackString(ref, "<empty>"))
			continue
		}
		kind, value, ok := strings.Cut(ref, ":")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(value) == "" {
			invalid = append(invalid, gateID+":"+ref)
			continue
		}
		kind = strings.ToLower(strings.TrimSpace(kind))
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "<") {
			invalid = append(invalid, gateID+":"+ref)
			continue
		}
		switch kind {
		case "file", "log", "screenshot", "image":
			path, err := evidenceLocalPath(kind, value, baseDir)
			if err != nil {
				invalid = append(invalid, gateID+":"+ref)
				continue
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				missing = append(missing, gateID+":"+ref)
			}
		case "url":
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				invalid = append(invalid, gateID+":"+ref)
			}
		case "task_id", "artifact", "backup", "command", "commit", "release", "review", "note", "build", "db_backup":
			// Non-file evidence refs are intentionally not opened by this local verifier.
		default:
			invalid = append(invalid, gateID+":"+ref)
		}
	}
	return invalid, missing
}

func evidenceLocalPath(kind, value, baseDir string) (string, error) {
	if kind == "file" && strings.HasPrefix(value, "//") {
		u, err := url.Parse("file:" + value)
		if err != nil || u.Path == "" {
			return "", errors.New("invalid file URL")
		}
		return u.Path, nil
	}
	if strings.HasPrefix(value, "//") {
		return "", errors.New("unexpected URL-style local evidence path")
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Clean(path), nil
}

func canonicalEvidenceGateIDs() []string {
	tpl := buildEvidenceTemplate(options{
		TargetEnv:      "staging",
		ModelCode:      "mimo-v2.5-pro",
		EntryKind:      "text",
		APIChannelCode: "mimo-official",
	}, 0)
	ids := make([]string, 0, len(tpl.RequiredGates))
	for _, gate := range tpl.RequiredGates {
		ids = append(ids, gate.ID)
	}
	return ids
}

func evidenceGatePassed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed", "pass", "ok", "complete", "completed":
		return true
	default:
		return false
	}
}

func evidenceIdentityIssues(tpl evidenceTemplate) []string {
	var issues []string
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "target_env", value: tpl.TargetEnv},
		{name: "model", value: tpl.Model},
		{name: "entry_kind", value: tpl.EntryKind},
		{name: "api_channel", value: tpl.APIChannel},
	} {
		value := strings.TrimSpace(item.value)
		if value == "" || strings.HasPrefix(value, "<") {
			issues = append(issues, item.name+" is missing or still a placeholder")
		}
	}
	if len(tpl.StopConditions) == 0 {
		issues = append(issues, "stop_conditions missing")
	}
	return issues
}

var evidenceSecretShapeRE = regexp.MustCompile(`(?i)(tp-[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{20,}|Bearer [A-Za-z0-9._-]{20,})`)

func evidenceSecretLeakFindings(raw []byte) []string {
	var findings []string
	if evidenceSecretShapeRE.Match(raw) {
		findings = append(findings, "evidence file contains provider key or bearer-token shaped text")
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err == nil {
		findings = append(findings, forbiddenSecretPaths(parsed, "evidence")...)
	}
	return findings
}

func buildEvidenceTemplate(opts options, generatedAt int64) evidenceTemplate {
	model := fallbackString(strings.TrimSpace(opts.ModelCode), "<model_code>")
	entryKind := fallbackString(strings.TrimSpace(opts.EntryKind), "<entry_kind>")
	apiChannel := fallbackString(strings.TrimSpace(opts.APIChannelCode), "<api_channel_code>")
	taskID := strings.TrimSpace(opts.TaskID)
	provider := inferProviderFromAPIChannel(apiChannel)
	pricingMode := normalizePricingMode(opts.ExpectedPricingMode)
	targetEnv := fallbackString(strings.TrimSpace(opts.TargetEnv), "<target_env>")

	preGenerationSmoke := strings.Join([]string{
		"go run ./cmd/modelgw-smoke",
		"--model " + shellQuote(model),
		"--entry-kind " + shellQuote(entryKind),
		"--api-channel " + shellQuote(apiChannel),
		"--require-openai-auth",
		"--require-admin",
		"--require-model",
		"--require-catalog-model",
		"--require-parameter-schema",
		"--require-api-channel",
		"--require-api-channel-health",
		"--require-key-pool",
		"--forbid-legacy-channel-key",
		"--require-source-mapping",
		"--require-no-source-conflicts",
		"--require-route-channel",
		"--forbid-account-pool-route",
		"--require-pricing-mode",
		optionalPricingModeFlag(pricingMode),
	}, " ")
	preGenerationSmoke = compactCommand(preGenerationSmoke)

	postGenerationSmokeParts := []string{
		"go run ./cmd/modelgw-smoke",
		"--model " + shellQuote(model),
		"--entry-kind " + shellQuote(entryKind),
		"--api-channel " + shellQuote(apiChannel),
		"--task-id " + shellQuote(fallbackString(taskID, "<task_id>")),
		"--require-openai-auth",
		"--require-post-generation-proof",
	}
	if pricingMode != "" {
		postGenerationSmokeParts = append(postGenerationSmokeParts, "--pricing-mode "+shellQuote(pricingMode))
	}
	postGenerationSmoke := compactCommand(strings.Join(postGenerationSmokeParts, " "))

	return evidenceTemplate{
		GeneratedAt: generatedAt,
		TargetEnv:   targetEnv,
		Model:       model,
		EntryKind:   entryKind,
		APIChannel:  apiChannel,
		Provider:    provider,
		PricingMode: pricingMode,
		TaskID:      taskID,
		RequiredGates: []evidenceGate{
			{
				ID:     "local_preflight",
				Status: "pending",
				Collect: []string{
					"git status --short output",
					"git diff --check output",
					"merge conflict marker scan output",
					"working tree scope review note",
				},
				Command: "git -C /Users/ashley/Desktop/DAPOV2/source status --short && git -C /Users/ashley/Desktop/DAPOV2/source diff --check && rg -n '^(<<<<<<<|=======|>>>>>>>)' /Users/ashley/Desktop/DAPOV2/AGENTS.md /Users/ashley/Desktop/DAPOV2/CURRENT_STATE.yaml /Users/ashley/Desktop/DAPOV2/WORK_PLAN.md /Users/ashley/Desktop/DAPOV2/PROJECT_LOG.md /Users/ashley/Desktop/DAPOV2/docs /Users/ashley/Desktop/DAPOV2/decision-log /Users/ashley/Desktop/DAPOV2/source/backend /Users/ashley/Desktop/DAPOV2/source/frontend",
				PassWhen: []string{
					"working tree changes are reviewed and intentionally in scope for this release",
					"git diff --check reports no whitespace errors",
					"merge conflict marker scan has no matches",
				},
			},
			{
				ID:      "provider_probe",
				Status:  "pending",
				Collect: []string{"sanitized provider probe JSON"},
				Command: "read -rs MODEL_GATEWAY_PROVIDER_KEY; printf '%s' \"$MODEL_GATEWAY_PROVIDER_KEY\" | go run ./cmd/modelgw-config --provider " + shellQuote(provider) + " --provider-probe --key-stdin; unset MODEL_GATEWAY_PROVIDER_KEY",
				PassWhen: []string{
					"probe ok=true for the target provider",
					"target model is visible or protocol probe passes",
					"output contains no plaintext provider key",
				},
			},
			{
				ID:       "config_plan",
				Status:   "pending",
				Collect:  []string{"sanitized configuration plan JSON"},
				Command:  "go run ./cmd/modelgw-config --provider " + shellQuote(provider) + " --plan",
				PassWhen: []string{"plan contains only the target provider", "plan creates or updates API Channel, Key Pool, Model Catalog and Source Mapping"},
			},
			{
				ID:     "db_target_check",
				Status: "pending",
				Collect: []string{
					"modelgw-config db-target-check output",
					"sanitized target DB address and database name",
					"operator note confirming the DSN points to a disposable clone, staging, shadow, sandbox or test database",
				},
				Command: "KLEIN_DB_DSN='<dryrun_or_staging_dsn>' go run ./cmd/modelgw-config --db-target-check --target-env " + shellQuote(targetEnv),
				PassWhen: []string{
					"db-target-check ok=true and migration_dry_run_allowed=true",
					"sanitized_dsn does not include a plaintext password or query secrets",
					"target-env and database name do not contain production/live/online markers",
				},
			},
			{
				ID:       "audit_only",
				Status:   "pending",
				Collect:  []string{"scoped audit-only output"},
				Command:  "go run ./cmd/modelgw-config --provider " + shellQuote(provider) + " --audit-only",
				PassWhen: []string{"no target-model account-pool conflicts", "no duplicate target source mapping", "no legacy channel-level key for acceptance"},
			},
			{
				ID:       "schema_check",
				Status:   "pending",
				Collect:  []string{"schema-check output"},
				Command:  "go run ./cmd/modelgw-config --schema-check",
				PassWhen: []string{"all Model Gateway, audit, billing and refund proof tables and columns exist"},
			},
			{
				ID:     "migration_dry_run",
				Status: "pending",
				Collect: []string{
					"migration list for this release",
					"modelgw-config migration-inventory output",
					"disposable clone or staging migration output",
					"post-migration schema-check output",
					"rollback or restore rehearsal note",
				},
				Command: "go run ./cmd/modelgw-config --migration-inventory, then apply the listed backend/migrations against the db-target-check-approved disposable clone or staging DB, then rerun modelgw-config --schema-check; never use the production primary for rehearsal",
				PassWhen: []string{
					"migration inventory ok=true and required Model Gateway migrations are present",
					"all release migrations apply cleanly outside the production primary",
					"post-migration schema-check is ok",
					"rollback or DB restore path is verified before production write or deploy",
				},
			},
			{
				ID:       "authorized_write",
				Status:   "pending",
				Collect:  []string{"target environment confirmation", "DB backup note", "write command output"},
				Command:  "read -rs MODEL_GATEWAY_PROVIDER_KEY; printf '%s' \"$MODEL_GATEWAY_PROVIDER_KEY\" | go run ./cmd/modelgw-config --provider " + shellQuote(provider) + " --confirm-write --key-stdin; unset MODEL_GATEWAY_PROVIDER_KEY",
				PassWhen: []string{"user has confirmed target environment and backup window", "Key Pool row is written or updated", "no plaintext key appears in output"},
			},
			{
				ID:       "pre_generation_smoke",
				Status:   "pending",
				Collect:  []string{"pre-generation smoke JSON"},
				Command:  preGenerationSmoke,
				PassWhen: []string{"public model surfaces include target model", "dry-run selects the target API Channel", "no available account-pool candidate remains"},
			},
			{
				ID:       "controlled_generation_task",
				Status:   "pending",
				Collect:  []string{"controlled request payload summary", "resulting task_id", "visible output proof"},
				PassWhen: []string{"the request uses the target public model", "task reaches success", "task_id belongs to this controlled run"},
			},
			{
				ID:       "post_generation_proof",
				Status:   "pending",
				Collect:  []string{"task-bound post-generation proof JSON"},
				Command:  postGenerationSmoke,
				PassWhen: []string{"route, pricing, output, upstream log, Key Pool usage and billing proof all match the same task_id"},
			},
			{
				ID:       "secret_scan",
				Status:   "pending",
				Collect:  []string{"secret scan output"},
				Command:  "rg -n 'tp-[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{20,}|Bearer [A-Za-z0-9._-]{20,}' . -g '!**/*_test.go' -g '!upstream/**' -g '!source/frontend/node_modules/**' -g '!source/frontend/**/dist/**'",
				PassWhen: []string{"no plaintext provider key or bearer token is found in source, docs, logs or evidence files"},
			},
			{
				ID:       "backend_tests",
				Status:   "pending",
				Collect:  []string{"Go test output"},
				Command:  "GOCACHE=/private/tmp/dapo-go-build-cache go test ./...",
				PassWhen: []string{"all backend tests pass"},
			},
			{
				ID:       "frontend_builds",
				Status:   "pending",
				Collect:  []string{"admin build output", "user build output"},
				Command:  "pnpm --filter @kleinai/admin build && pnpm --filter @kleinai/user build",
				PassWhen: []string{"both frontend builds pass"},
			},
			{
				ID:      "frontend_preview_smoke",
				Status:  "pending",
				Collect: []string{"user preview URL", "admin preview URL", "user screenshot", "admin login screenshot", "browser error log summary", "preview process cleanup note"},
				Command: "start @kleinai/user preview on 127.0.0.1:5173 and @kleinai/admin preview on 127.0.0.1:5174, then browser-smoke /create/image and /login",
				PassWhen: []string{
					"user frontend root mounts and target entry renders without fatal browser errors",
					"admin login root mounts without fatal browser errors",
					"local preview processes are stopped after evidence is collected",
				},
			},
			{
				ID:     "admin_protected_pages_smoke",
				Status: "pending",
				Collect: []string{
					"authorized admin session note",
					"API Channels page screenshot",
					"Model Gateway page screenshot",
					"Model Gateway Audit page screenshot",
					"Generation log detail screenshot for the controlled task_id",
					"browser error log summary",
				},
				Command: "with an authorized admin session, browser-smoke /admin/api-channels, /admin/model-gateway, /admin/model-gateway-audit, and the controlled task log detail",
				PassWhen: []string{
					"protected admin pages render with real target-environment data",
					"target API Channel, Model Catalog, source mapping, audit route/pricing/output proof and billing proof are visible where expected",
					"no blank page, fatal browser error or unauthorized redirect occurs",
				},
			},
			{
				ID:      "release_manifest",
				Status:  "pending",
				Collect: []string{"git commit or diff summary", "build artifact or image tag", "migration inventory with checksums", "target DB backup point", "rollback artifact or image tag"},
				Command: "git status --short && git rev-parse --short HEAD && git diff --stat && go run ./cmd/modelgw-config --migration-inventory",
				PassWhen: []string{
					"release artifact is traceable to the reviewed source state",
					"database migrations for this release are listed with checksums before deployment",
					"rollback artifact and target DB backup point are recorded",
				},
			},
			{
				ID:     "deployment_runbook",
				Status: "pending",
				Collect: []string{
					"deployment operator and final target environment",
					"ordered deployment steps or command references",
					"traffic or maintenance window plan",
					"health check URLs and expected pass criteria",
					"rollback trigger conditions",
					"rollback command or artifact reference",
					"no-deploy window and stop-condition note",
				},
				PassWhen: []string{
					"deployment steps are explicit enough for another operator to execute",
					"health checks and rollback triggers are measurable before traffic is trusted",
					"rollback command or artifact is tied to the release manifest",
				},
			},
			{
				ID:      "deployment_approval",
				Status:  "pending",
				Collect: []string{"pre-deployment evidence verification output", "human deployment approval", "rollback point", "final target environment"},
				PassWhen: []string{
					"modelgw-smoke --evidence-verify reports ready_for_deployment_approval=true before approval",
					"deployment is explicitly approved after all prior gates pass",
				},
			},
		},
		StopConditions: []string{
			"local preflight finds unreviewed changes, whitespace errors or merge conflict markers",
			"provider probe fails or target model is not visible",
			"db-target-check fails, leaks a plaintext DSN secret or points at a production/live/online target",
			"schema-check fails",
			"migration dry-run fails or rollback/restore rehearsal is missing",
			"dry-run selects an account_pool candidate for the target official API model",
			"task stays generating or succeeds without output proof",
			"post-generation proof cannot bind route, pricing, upstream, key usage and billing to the same task_id",
			"secret scan finds a plaintext provider key",
			"frontend preview smoke is blank or has fatal browser errors",
			"protected admin pages are blank, redirect unexpectedly or have fatal browser errors",
			"release artifact, migration list, rollback artifact or DB backup point is missing",
			"deployment runbook is missing ordered steps, measurable health checks or rollback triggers",
		},
		Notes: []string{
			"this template is local-only and does not make HTTP requests",
			"when marking a gate passed, add at least one evidence entry such as a file path, command log path, screenshot path, task_id, artifact tag, or review note reference",
			"feed provider keys through stdin only; do not paste keys into command arguments, source files, docs, logs or screenshots",
			"do not deploy until every required gate is collected and reviewed",
		},
		Placeholders: map[string]string{
			"<model_code>":       "public model selected by users, for example mimo-v2.5-pro",
			"<entry_kind>":       "text, image, or video",
			"<api_channel_code>": "target API Channel code, for example mimo-official",
			"<task_id>":          "controlled generation task_id from the current run",
			"<target_env>":       "staging or production target label",
			"evidence":           "add this JSON field to each passed gate with one or more evidence references",
		},
	}
}

func inferProviderFromAPIChannel(apiChannel string) string {
	value := strings.ToLower(strings.TrimSpace(apiChannel))
	switch {
	case strings.Contains(value, "mimo"):
		return "mimo"
	case strings.Contains(value, "deepseek"):
		return "deepseek"
	case value == "" || strings.HasPrefix(value, "<"):
		return "<provider>"
	default:
		return value
	}
}

func optionalPricingModeFlag(mode string) string {
	if mode == "" {
		return ""
	}
	return "--pricing-mode " + shellQuote(mode)
}

func compactCommand(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func shellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "<value>"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func normalizeOptions(opts options) options {
	if opts.ExpectedPricingMode != "" {
		opts.RequireAdmin = true
		opts.RequireCatalogModel = true
		opts.RequirePricingMode = true
	}
	if opts.RequirePostGenerationProof {
		opts.RequireAdmin = true
		opts.RequireOpenAIAuth = true
		opts.RequireModel = true
		opts.RequireCatalogModel = true
		opts.RequireAPIChannel = true
		opts.RequireAPIChannelHealth = true
		opts.RequireKeyPool = true
		opts.ForbidLegacyChannelKey = true
		opts.RequireSourceMapping = true
		opts.RequireRouteChannel = true
		opts.RequireNoSourceConflicts = true
		opts.ForbidAccountPoolRoute = true
		opts.RequirePricingMode = true
		opts.RequireAuditRoute = true
		opts.RequireAuditPricing = true
		if isTextLikeKind(opts.EntryKind) {
			opts.RequireParameterSchema = true
		}
		opts.RequireOutputProof = true
		opts.RequireUpstreamLog = true
		opts.RequireKeyUsageFeedback = true
		opts.RequireBillingProof = true
		if strings.EqualFold(strings.TrimSpace(opts.EntryKind), "video") {
			opts.RequireVideoJobProof = true
		}
		if strings.TrimSpace(opts.AuditPricingSource) == "" {
			opts.AuditPricingSource = "model_catalog"
		}
	}
	return opts
}

func checkOptionRequirements(opts options) []checkResult {
	var checks []checkResult
	if opts.RequireAPIChannelHealth && !opts.RequirePostGenerationProof && strings.TrimSpace(opts.APIChannelCode) == "" {
		checks = append(checks, checkResult{
			Name:    "options_api_channel_health",
			Status:  "error",
			Details: []string{"--require-api-channel-health needs --api-channel"},
		})
	}
	if !opts.RequirePostGenerationProof {
		checks = append(checks, checkStandaloneOptionRequirements(opts)...)
		return checks
	}
	details := []string{
		"mode=require-post-generation-proof",
		"expanded=require-openai-auth,require-model,require-catalog-model,require-api-channel,require-api-channel-health,require-key-pool,forbid-legacy-channel-key,require-source-mapping,require-no-source-conflicts,require-route-channel,forbid-account-pool-route,require-pricing-mode,require-audit-route-snapshot,require-audit-pricing-snapshot,require-audit-output-filter,require-output-proof,succeeded-same-task-pricing-snapshot,require-upstream-log,require-key-usage-feedback,require-billing-proof",
		"audit_pricing_source=" + opts.AuditPricingSource,
	}
	if opts.RequireVideoJobProof {
		details[1] += ",require-audit-video-filter,require-video-job-proof"
	}
	if opts.RequireParameterSchema {
		details[1] += ",require-parameter-schema"
	}
	if strings.TrimSpace(opts.TaskID) != "" {
		details = append(details, "task_id="+strings.TrimSpace(opts.TaskID))
	}
	var missing []string
	if strings.TrimSpace(opts.ModelCode) == "" {
		missing = append(missing, "--model")
	}
	if strings.TrimSpace(opts.EntryKind) == "" {
		missing = append(missing, "--entry-kind")
	}
	if strings.TrimSpace(opts.APIChannelCode) == "" {
		missing = append(missing, "--api-channel")
	}
	if strings.TrimSpace(opts.TaskID) == "" {
		missing = append(missing, "--task-id")
	}
	if len(missing) > 0 {
		return append(checks, checkResult{
			Name:    "options_post_generation_proof",
			Status:  "error",
			Details: append(details, "missing required options: "+strings.Join(missing, ", ")),
		})
	}
	return append(checks, checkResult{
		Name:    "options_post_generation_proof",
		Status:  "ok",
		Details: details,
	})
}

func checkStandaloneOptionRequirements(opts options) []checkResult {
	var checks []checkResult
	model := strings.TrimSpace(opts.ModelCode)
	apiChannel := strings.TrimSpace(opts.APIChannelCode)
	taskID := strings.TrimSpace(opts.TaskID)

	if opts.RequireModel && model == "" {
		checks = append(checks, optionError("options_model", "--require-model needs --model"))
	}
	if opts.RequireCatalogModel && model == "" {
		checks = append(checks, optionError("options_catalog_model", "--require-catalog-model needs --model"))
	}
	if opts.RequireParameterSchema && model == "" {
		checks = append(checks, optionError("options_parameter_schema", "--require-parameter-schema needs --model"))
	}
	if (opts.RequirePricingMode || strings.TrimSpace(opts.ExpectedPricingMode) != "") && model == "" {
		checks = append(checks, optionError("options_pricing_mode", "--require-pricing-mode or --pricing-mode needs --model"))
	}
	if opts.RequireAPIChannel && apiChannel == "" {
		checks = append(checks, optionError("options_api_channel", "--require-api-channel needs --api-channel"))
	}
	if opts.RequireKeyPool && apiChannel == "" {
		checks = append(checks, optionError("options_key_pool", "--require-key-pool needs --api-channel"))
	}
	if opts.ForbidLegacyChannelKey && apiChannel == "" {
		checks = append(checks, optionError("options_legacy_channel_key", "--forbid-legacy-channel-key needs --api-channel"))
	}
	if opts.RequireSourceMapping && (model == "" || apiChannel == "") {
		checks = append(checks, optionError("options_source_mapping", "--require-source-mapping needs --model and --api-channel"))
	}
	if opts.RequireRouteChannel && (model == "" || apiChannel == "") {
		checks = append(checks, optionError("options_route_channel", "--require-route-channel needs --model and --api-channel"))
	}
	if opts.ForbidAccountPoolRoute && model == "" {
		checks = append(checks, optionError("options_forbid_account_pool_route", "--forbid-account-pool-route needs --model"))
	}
	if opts.RequireKeyUsageFeedback {
		var missing []string
		if apiChannel == "" {
			missing = append(missing, "--api-channel")
		}
		if model == "" && taskID == "" {
			missing = append(missing, "--model or --task-id")
		}
		if len(missing) > 0 {
			checks = append(checks, optionError("options_key_usage_feedback", "--require-key-usage-feedback needs "+strings.Join(missing, " and ")))
		}
	}
	if opts.RequireAuditRoute ||
		opts.RequireAuditPricing ||
		opts.RequireOutputProof ||
		opts.RequireVideoJobProof ||
		opts.RequireUpstreamLog ||
		opts.RequireBillingProof {
		if model == "" && taskID == "" {
			checks = append(checks, optionError("options_audit_target", "audit/output/upstream/billing proof gates need --model or --task-id"))
		}
	}
	return checks
}

func optionError(name, detail string) checkResult {
	return checkResult{Name: name, Status: "error", Details: []string{detail}}
}

func checkPublicModels(ctx context.Context, client *http.Client, opts options) checkResult {
	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.APIBase+"/models", "", nil)
	if err != nil {
		return failed("public_models", "GET /api/v1/models failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("public_models", "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed("public_models", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var data struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		return failed("public_models", "invalid data.list", status, err)
	}
	details := []string{fmt.Sprintf("models=%d", len(data.List))}
	if len(data.List) == 0 {
		return checkResult{Name: "public_models", Status: "error", Details: append(details, "empty model list")}
	}
	kinds := map[string]int{}
	found := false
	foundKind := ""
	foundPricingMode := ""
	foundParametersSchema := false
	expectedKind := expectedPublicModelKind(opts.EntryKind)
	for i, item := range data.List {
		if forbidden := forbiddenSecretPaths(item, fmt.Sprintf("model[%d]", i)); len(forbidden) > 0 {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, fmt.Sprintf("public model leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
		}
		code := stringField(item, "model_code")
		kind := stringField(item, "kind")
		if code == "" || kind == "" {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, fmt.Sprintf("model[%d] missing model_code or kind", i))}
		}
		kinds[kind]++
		if code == opts.ModelCode {
			found = true
			foundKind = kind
			foundPricingMode = normalizePricingMode(stringField(item, "pricing_mode"))
			foundParametersSchema = publicModelItemHasParameterSchema(item)
		}
	}
	details = append(details, "kinds="+compactCounts(kinds))
	if opts.ModelCode != "" {
		details = append(details, fmt.Sprintf("expected_model=%s present=%v", opts.ModelCode, found))
		if foundKind != "" {
			details = append(details, "expected_model_kind="+foundKind)
		}
		if foundPricingMode != "" {
			details = append(details, "expected_model_pricing_mode="+foundPricingMode)
		}
		if expectedKind != "" {
			details = append(details, "expected_entry_kind="+expectedKind)
		}
		if opts.RequireParameterSchema {
			details = append(details, fmt.Sprintf("expected_model_parameters_schema_present=%t", foundParametersSchema))
		}
		if opts.RequireModel && !found {
			return checkResult{Name: "public_models", Status: "error", Details: details}
		}
		if opts.RequireModel && found && expectedKind != "" && !publicModelKindMatches(foundKind, expectedKind) {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, fmt.Sprintf("public model kind mismatch: got %s want %s", foundKind, expectedKind))}
		}
		if opts.RequireParameterSchema && found && !foundParametersSchema {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, "public model parameters_schema missing")}
		}
		if found && opts.RequirePricingMode && foundPricingMode == "" {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, "public model pricing_mode missing")}
		}
		if found && opts.ExpectedPricingMode != "" && foundPricingMode != opts.ExpectedPricingMode {
			return checkResult{Name: "public_models", Status: "error", Details: append(details, fmt.Sprintf("public model pricing_mode mismatch: got %s want %s", foundPricingMode, opts.ExpectedPricingMode))}
		}
	}
	return checkResult{Name: "public_models", Status: "ok", Details: details}
}

func expectedPublicModelKind(entryKind string) string {
	switch strings.ToLower(strings.TrimSpace(entryKind)) {
	case "", "all":
		return ""
	case "chat", "text":
		return "text"
	case "image":
		return "image"
	case "video":
		return "video"
	default:
		return strings.ToLower(strings.TrimSpace(entryKind))
	}
}

func publicModelKindMatches(got, want string) bool {
	return expectedPublicModelKind(got) == expectedPublicModelKind(want)
}

func checkOpenAIModels(ctx context.Context, client *http.Client, opts options) checkResult {
	token := strings.TrimSpace(os.Getenv(opts.OpenAIAPIKeyEnv))
	if opts.RequireOpenAIAuth && token == "" {
		return checkResult{
			Name:   "openai_models",
			Status: "error",
			Details: []string{
				"auth=missing",
				fmt.Sprintf("%s is empty; --require-openai-auth needs an OpenAI-compatible API key for /v1 checks", opts.OpenAIAPIKeyEnv),
			},
		}
	}
	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.OpenAIBase+"/models", token, nil)
	if err != nil {
		res := failed("openai_models", "GET /v1/models failed", status, err)
		if status == http.StatusUnauthorized && token == "" {
			res.Details = append(res.Details, fmt.Sprintf("%s is empty; protected /v1/models checks need an OpenAI-compatible API key", opts.OpenAIAPIKeyEnv))
		}
		return res
	}
	var data struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return failed("openai_models", "invalid response JSON", status, err)
	}
	details := []string{fmt.Sprintf("models=%d", len(data.Data))}
	if token != "" {
		details = append(details, "auth=provided")
	}
	if data.Object != "list" {
		return checkResult{Name: "openai_models", Status: "error", Details: append(details, "object is not list")}
	}
	if len(data.Data) == 0 {
		return checkResult{Name: "openai_models", Status: "error", Details: append(details, "empty model list")}
	}
	found := false
	foundKind := ""
	foundEndpoint := ""
	foundPricingMode := ""
	foundParametersSchema := false
	expectedKind := expectedPublicModelKind(opts.EntryKind)
	expectedEndpoint := expectedOpenAIModelEndpoint(expectedKind)
	for i, item := range data.Data {
		if forbidden := forbiddenSecretPaths(item, fmt.Sprintf("model[%d]", i)); len(forbidden) > 0 {
			return checkResult{Name: "openai_models", Status: "error", Details: append(details, fmt.Sprintf("openai model leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
		}
		id := stringField(item, "id")
		if id == "" {
			return checkResult{Name: "openai_models", Status: "error", Details: append(details, fmt.Sprintf("model[%d] missing id", i))}
		}
		if id == opts.ModelCode {
			found = true
			foundKind = stringField(item, "kind")
			foundEndpoint = stringField(item, "endpoint")
			foundPricingMode = openAIModelItemPricingMode(item)
			foundParametersSchema = openAIModelItemHasParameterSchema(item)
		}
	}
	if opts.ModelCode != "" {
		details = append(details, fmt.Sprintf("expected_model=%s present=%v", opts.ModelCode, found))
		if foundKind != "" {
			details = append(details, "expected_model_kind="+foundKind)
		}
		if foundEndpoint != "" {
			details = append(details, "expected_model_endpoint="+foundEndpoint)
		}
		if foundPricingMode != "" {
			details = append(details, "expected_model_pricing_mode="+foundPricingMode)
		}
		if expectedKind != "" {
			details = append(details, "expected_entry_kind="+expectedKind)
		}
		if opts.RequireParameterSchema {
			details = append(details, fmt.Sprintf("expected_model_parameters_schema_present=%t", foundParametersSchema))
		}
		if opts.RequireModel && !found {
			return checkResult{Name: "openai_models", Status: "error", Details: details}
		}
		if opts.RequireParameterSchema && found && !foundParametersSchema {
			return checkResult{Name: "openai_models", Status: "error", Details: append(details, "openai model parameters_schema missing")}
		}
		if found && opts.RequirePricingMode && foundPricingMode == "" {
			return checkResult{Name: "openai_models", Status: "error", Details: append(details, "openai model meta.pricing_mode missing")}
		}
		if found && opts.ExpectedPricingMode != "" && foundPricingMode != opts.ExpectedPricingMode {
			return checkResult{Name: "openai_models", Status: "error", Details: append(details, fmt.Sprintf("openai model pricing_mode mismatch: got %s want %s", foundPricingMode, opts.ExpectedPricingMode))}
		}
		if opts.RequireModel && found && expectedKind != "" {
			if foundKind == "" {
				return checkResult{Name: "openai_models", Status: "error", Details: append(details, "openai model kind missing")}
			}
			if !publicModelKindMatches(foundKind, expectedKind) {
				return checkResult{Name: "openai_models", Status: "error", Details: append(details, fmt.Sprintf("openai model kind mismatch: got %s want %s", foundKind, expectedKind))}
			}
			if expectedEndpoint != "" {
				if foundEndpoint == "" {
					return checkResult{Name: "openai_models", Status: "error", Details: append(details, "openai model endpoint missing")}
				}
				if strings.TrimSpace(foundEndpoint) != expectedEndpoint {
					return checkResult{Name: "openai_models", Status: "error", Details: append(details, fmt.Sprintf("openai model endpoint mismatch: got %s want %s", foundEndpoint, expectedEndpoint))}
				}
			}
		}
	}
	return checkResult{Name: "openai_models", Status: "ok", Details: details}
}

func expectedOpenAIModelEndpoint(kind string) string {
	switch expectedPublicModelKind(kind) {
	case "text":
		return "/v1/chat/completions"
	case "image":
		return "/v1/images/generations"
	case "video":
		return "/v1/video/generations"
	default:
		return ""
	}
}

func checkPublicPricingModeConsistency(ctx context.Context, client *http.Client, opts options) checkResult {
	const name = "public_pricing_mode_consistency"
	publicMode, publicFound, publicDetails, publicErr := fetchPublicModelPricingMode(ctx, client, opts)
	if publicErr != nil {
		publicErr.Name = name
		return *publicErr
	}
	openAIMode, openAIFound, openAIDetails, openAIErr := fetchOpenAIModelPricingMode(ctx, client, opts)
	if openAIErr != nil {
		openAIErr.Name = name
		return *openAIErr
	}
	details := []string{
		"expected_model=" + opts.ModelCode,
		fmt.Sprintf("public_present=%t", publicFound),
		fmt.Sprintf("openai_present=%t", openAIFound),
	}
	details = append(details, publicDetails...)
	details = append(details, openAIDetails...)
	if !publicFound || !openAIFound {
		return checkResult{Name: name, Status: "error", Details: append(details, "target model missing from one or more public model surfaces")}
	}
	if publicMode == "" || openAIMode == "" {
		return checkResult{Name: name, Status: "error", Details: append(details, "target model pricing_mode missing from one or more public model surfaces")}
	}
	if opts.ExpectedPricingMode != "" {
		details = append(details, "expected_pricing_mode="+opts.ExpectedPricingMode)
		if publicMode != opts.ExpectedPricingMode || openAIMode != opts.ExpectedPricingMode {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("public pricing_mode expected mismatch: api=%s openai=%s want=%s", publicMode, openAIMode, opts.ExpectedPricingMode))}
		}
	}
	if publicMode != openAIMode {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("public pricing_mode mismatch: api=%s openai=%s", publicMode, openAIMode))}
	}
	return checkResult{Name: name, Status: "ok", Details: details}
}

func fetchPublicModelPricingMode(ctx context.Context, client *http.Client, opts options) (string, bool, []string, *checkResult) {
	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.APIBase+"/models", "", nil)
	if err != nil {
		res := failed("public_pricing_mode_consistency", "GET /api/v1/models failed", status, err)
		return "", false, nil, &res
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		res := failed("public_pricing_mode_consistency", "invalid /api/v1/models envelope", status, err)
		return "", false, nil, &res
	}
	if body.Code != 0 {
		res := failed("public_pricing_mode_consistency", fmt.Sprintf("/api/v1/models response code=%d msg=%s", body.Code, body.Msg), status, nil)
		return "", false, nil, &res
	}
	var data struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		res := failed("public_pricing_mode_consistency", "invalid /api/v1/models data.list", status, err)
		return "", false, nil, &res
	}
	for _, item := range data.List {
		if stringField(item, "model_code") != opts.ModelCode {
			continue
		}
		mode := normalizePricingMode(stringField(item, "pricing_mode"))
		return mode, true, []string{"api_pricing_mode=" + valueOrPlaceholder(mode)}, nil
	}
	return "", false, []string{"api_pricing_mode=<missing_model>"}, nil
}

func fetchOpenAIModelPricingMode(ctx context.Context, client *http.Client, opts options) (string, bool, []string, *checkResult) {
	token := strings.TrimSpace(os.Getenv(opts.OpenAIAPIKeyEnv))
	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.OpenAIBase+"/models", token, nil)
	if err != nil {
		res := failed("public_pricing_mode_consistency", "GET /v1/models failed", status, err)
		if status == http.StatusUnauthorized && token == "" {
			res.Details = append(res.Details, fmt.Sprintf("%s is empty; protected /v1/models checks need an OpenAI-compatible API key", opts.OpenAIAPIKeyEnv))
		}
		return "", false, nil, &res
	}
	var data struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		res := failed("public_pricing_mode_consistency", "invalid /v1/models response JSON", status, err)
		return "", false, nil, &res
	}
	if data.Object != "list" {
		return "", false, []string{"openai_pricing_mode=<invalid_object>"}, &checkResult{Name: "public_pricing_mode_consistency", Status: "error", Details: []string{"openai /v1/models object is not list"}}
	}
	for _, item := range data.Data {
		if stringField(item, "id") != opts.ModelCode {
			continue
		}
		mode := openAIModelItemPricingMode(item)
		return mode, true, []string{"openai_pricing_mode=" + valueOrPlaceholder(mode)}, nil
	}
	return "", false, []string{"openai_pricing_mode=<missing_model>"}, nil
}

func valueOrPlaceholder(value string) string {
	if value == "" {
		return "<empty>"
	}
	return value
}

func checkAdminAudit(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	u, err := url.Parse(opts.AdminBase + "/model-gateway/audit")
	if err != nil {
		return failed("admin_audit", "invalid admin audit URL", 0, err)
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "5")
	if opts.ModelCode != "" {
		q.Set("model_code", opts.ModelCode)
	}
	if opts.TaskID != "" {
		q.Set("keyword", opts.TaskID)
	}
	u.RawQuery = q.Encode()
	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		return failed("admin_audit", "GET /admin/api/v1/model-gateway/audit failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("admin_audit", "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed("admin_audit", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		return failed("admin_audit", "invalid page data", status, err)
	}
	if page.Page <= 0 || page.PageSize <= 0 || page.Total < 0 {
		return checkResult{Name: "admin_audit", Status: "error", Details: []string{"invalid pagination fields"}}
	}
	return checkResult{
		Name:   "admin_audit",
		Status: "ok",
		Details: []string{
			fmt.Sprintf("total=%d", page.Total),
			fmt.Sprintf("page=%d", page.Page),
			fmt.Sprintf("page_size=%d", page.PageSize),
			fmt.Sprintf("sample_rows=%d", len(page.List)),
		},
	}
}

func checkAdminAuditSnapshot(ctx context.Context, client *http.Client, opts options, token string, auditType string) checkResult {
	name := "admin_audit_" + auditType + "_snapshot"
	u, err := url.Parse(opts.AdminBase + "/model-gateway/audit")
	if err != nil {
		return failed(name, "invalid admin audit URL", 0, err)
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "20")
	q.Set("audit_type", auditType)
	if opts.ModelCode != "" {
		q.Set("model_code", opts.ModelCode)
	}
	if opts.TaskID != "" {
		q.Set("keyword", opts.TaskID)
	}
	u.RawQuery = q.Encode()

	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		return failed(name, "GET /admin/api/v1/model-gateway/audit failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed(name, "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed(name, fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		return failed(name, "invalid page data", status, err)
	}
	details := []string{
		fmt.Sprintf("total=%d", page.Total),
		fmt.Sprintf("page=%d", page.Page),
		fmt.Sprintf("page_size=%d", page.PageSize),
		fmt.Sprintf("sample_rows=%d", len(page.List)),
	}
	if page.Page <= 0 || page.PageSize <= 0 || page.Total < 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, "invalid pagination fields")}
	}
	if len(page.List) == 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("empty %s audit sample list", auditType))}
	}

	foundExpectedRoute := false
	foundExpectedPricing := false
	foundExpectedOutput := false
	foundExpectedVideo := false
	for i, item := range page.List {
		if forbidden := forbiddenSecretPaths(item, fmt.Sprintf("row[%d]", i)); len(forbidden) > 0 {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("audit row leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
		}
		taskID := stringField(item, "task_id")
		modelCode := stringField(item, "model_code")
		kind := stringField(item, "kind")
		if taskID == "" || modelCode == "" || kind == "" {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] missing task_id/model_code/kind", i))}
		}
		if opts.TaskID != "" && taskID != opts.TaskID {
			continue
		}
		if opts.ModelCode != "" && modelCode != opts.ModelCode {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] model_code mismatch: got %s want %s", i, modelCode, opts.ModelCode))}
		}
		if auditType == "route" {
			snapshot, ok := mapField(item, "model_gateway_route_snapshot")
			if !ok {
				continue
			}
			if _, ok := intField(snapshot, "selected_index"); !ok {
				return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] route snapshot missing selected_index", i))}
			}
			if _, ok := intField(snapshot, "candidate_count"); !ok {
				if len(sliceField(snapshot, "candidates")) == 0 {
					return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] route snapshot missing candidate_count/candidates", i))}
				}
			}
			sourceType := stringField(item, "selected_source_type")
			sourceCode := stringField(item, "selected_source_code")
			upstreamModel := stringField(item, "selected_upstream_model")
			if sourceType == "" || sourceCode == "" || upstreamModel == "" {
				return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] missing selected source summary", i))}
			}
			selectedRoute := selectedRouteSnapshotCandidate(item)
			if msg, mismatch := routeSnapshotSummaryMismatch(item, selectedRoute); mismatch {
				return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] %s", i, msg))}
			}
			if opts.APIChannelCode == "" || (sourceType == "api_channel" && sourceCode == opts.APIChannelCode) {
				foundExpectedRoute = true
				details = append(details,
					fmt.Sprintf("route_sample=%s selected=%s/%s upstream=%s", taskID, sourceType, sourceCode, upstreamModel),
					fmt.Sprintf("route_snapshot_selected=%s/%s upstream=%s", stringField(selectedRoute, "source_type"), stringField(selectedRoute, "source_code"), stringField(selectedRoute, "upstream_model")),
				)
			}
		}
		if auditType == "pricing" {
			snapshot, ok := mapField(item, "pricing_snapshot")
			if !ok {
				continue
			}
			pricingSource := firstNonEmpty(stringField(item, "pricing_source"), stringField(snapshot, "pricing_source"))
			pricingMode := firstNonEmpty(stringField(item, "pricing_mode"), stringField(snapshot, "pricing_mode"))
			settlement := firstNonEmpty(stringField(item, "settlement"), stringField(snapshot, "settlement"))
			if pricingSource == "" || pricingMode == "" || settlement == "" {
				return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("row[%d] pricing snapshot missing pricing_source/pricing_mode/settlement", i))}
			}
			if opts.AuditPricingSource != "" && pricingSource != opts.AuditPricingSource {
				continue
			}
			foundExpectedPricing = true
			details = append(details, fmt.Sprintf("pricing_sample=%s source=%s mode=%s settlement=%s", taskID, pricingSource, pricingMode, settlement))
		}
		if auditType == "output" {
			if opts.APIChannelCode != "" && !(stringField(item, "selected_source_type") == "api_channel" && stringField(item, "selected_source_code") == opts.APIChannelCode) {
				continue
			}
			nextDetails, errResult := validateAdminAuditOutputProof(item, details, name)
			if errResult != nil {
				return *errResult
			}
			foundExpectedOutput = true
			details = append(nextDetails, fmt.Sprintf("output_filter_sample=%s", taskID))
		}
		if auditType == "video" {
			nextDetails, errResult := validateAdminAuditVideoJobProof(item, details, name, opts)
			if errResult != nil {
				return *errResult
			}
			foundExpectedVideo = true
			details = append(nextDetails, fmt.Sprintf("video_filter_sample=%s", taskID))
		}
	}
	if auditType == "route" && !foundExpectedRoute {
		want := "any route snapshot"
		if opts.APIChannelCode != "" {
			want = "api_channel/" + opts.APIChannelCode
		}
		return checkResult{Name: name, Status: "error", Details: append(details, "no audit route sample matched "+want)}
	}
	if auditType == "pricing" && !foundExpectedPricing {
		want := "any pricing snapshot"
		if opts.AuditPricingSource != "" {
			want = "pricing_source=" + opts.AuditPricingSource
		}
		return checkResult{Name: name, Status: "error", Details: append(details, "no audit pricing sample matched "+want)}
	}
	if auditType == "output" && !foundExpectedOutput {
		want := "output proof"
		if opts.APIChannelCode != "" {
			want = "api_channel/" + opts.APIChannelCode + " output proof"
		}
		return checkResult{Name: name, Status: "error", Details: append(details, "no audit output sample matched "+want)}
	}
	if auditType == "video" && !foundExpectedVideo {
		want := "video job proof"
		if opts.APIChannelCode != "" {
			want = "api_channel/" + opts.APIChannelCode + " video job proof"
		}
		return checkResult{Name: name, Status: "error", Details: append(details, "no audit video sample matched "+want)}
	}
	return checkResult{Name: name, Status: "ok", Details: details}
}

func checkAdminUpstreamLog(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	const name = "admin_upstream_log"
	_, _, _, details, errResult := findMatchingAdminUpstreamLog(ctx, client, opts, token, name)
	if errResult != nil {
		return *errResult
	}
	return checkResult{Name: name, Status: "ok", Details: details}
}

func checkAdminOutputProof(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	const name = "admin_audit_output_proof"
	sample, details, errResult := findAdminAuditRouteSample(ctx, client, opts, token)
	if errResult != nil {
		errResult.Name = name
		return *errResult
	}
	details, errResult = validateAdminAuditOutputProof(sample, details, name)
	if errResult != nil {
		return *errResult
	}
	return checkResult{Name: name, Status: "ok", Details: details}
}

func checkAdminVideoJobProof(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	const name = "admin_audit_video_job_proof"
	sample, details, errResult := findAdminAuditRouteSample(ctx, client, opts, token)
	if errResult != nil {
		errResult.Name = name
		return *errResult
	}
	details, errResult = validateAdminAuditVideoJobProof(sample, details, name, opts)
	if errResult != nil {
		return *errResult
	}
	return checkResult{Name: name, Status: "ok", Details: details}
}

func checkAdminRouteSamplePricingSnapshot(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	const name = "admin_audit_same_task_pricing_snapshot"
	sample, details, errResult := findAdminAuditRouteSample(ctx, client, opts, token)
	if errResult != nil {
		errResult.Name = name
		return *errResult
	}
	details, errResult = validateAdminAuditOutputProof(sample, details, name)
	if errResult != nil {
		return *errResult
	}
	taskID := stringField(sample, "task_id")
	snapshot, ok := mapField(sample, "pricing_snapshot")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s has no pricing_snapshot", taskID))}
	}
	if forbidden := forbiddenSecretPaths(snapshot, "pricing_snapshot"); len(forbidden) > 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
	}
	pricingSource := firstNonEmpty(stringField(sample, "pricing_source"), stringField(snapshot, "pricing_source"))
	pricingMode := firstNonEmpty(stringField(sample, "pricing_mode"), stringField(snapshot, "pricing_mode"))
	settlement := firstNonEmpty(stringField(sample, "settlement"), stringField(snapshot, "settlement"))
	if pricingSource == "" || pricingMode == "" || settlement == "" {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s pricing snapshot missing pricing_source/pricing_mode/settlement", taskID))}
	}
	if opts.AuditPricingSource != "" && pricingSource != opts.AuditPricingSource {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s pricing_source mismatch: got %s want %s", taskID, pricingSource, opts.AuditPricingSource))}
	}
	if opts.ExpectedPricingMode != "" && normalizePricingMode(pricingMode) != opts.ExpectedPricingMode {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s pricing_mode mismatch: got %s want %s", taskID, pricingMode, opts.ExpectedPricingMode))}
	}
	if nextDetails, errResult := validatePricingBasisProof(sample, snapshot, pricingSource, normalizePricingMode(pricingMode), details, name); errResult != nil {
		return *errResult
	} else {
		details = nextDetails
	}
	actualPoints, ok := intField(snapshot, "actual_points")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s pricing snapshot missing actual_points", taskID))}
	}
	costPoints, ok := intField(sample, "cost_points")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s missing cost_points", taskID))}
	}
	if actualPoints != costPoints {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s cost_points mismatch: task=%d pricing_actual=%d", taskID, costPoints, actualPoints))}
	}
	return checkResult{
		Name:   name,
		Status: "ok",
		Details: append(details,
			fmt.Sprintf("same_task_cost=%s cost_points=%d actual_points=%d", taskID, costPoints, actualPoints),
			fmt.Sprintf("same_task_pricing_sample=%s source=%s mode=%s settlement=%s", taskID, pricingSource, pricingMode, settlement),
		),
	}
}

func validatePricingBasisProof(sample, snapshot map[string]any, pricingSource, pricingMode string, details []string, name string) ([]string, *checkResult) {
	kind := strings.ToLower(stringField(sample, "kind"))
	if kind == "image" || kind == "video" {
		return validateMatrixPricingRuleProof(sample, snapshot, pricingSource, pricingMode, details, name)
	}
	if kind != "chat" && kind != "text" {
		return details, nil
	}
	taskID := stringField(sample, "task_id")
	switch pricingMode {
	case "char":
		unitBasis := stringField(snapshot, "unit_basis")
		if unitBasis != "per_1k_chars" {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s char pricing snapshot unit_basis mismatch: got %s want per_1k_chars", taskID, valueOrPlaceholder(unitBasis)))}
		}
		promptChars, ok := intField(snapshot, "estimated_prompt_chars")
		if !ok || promptChars <= 0 {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s char pricing snapshot missing estimated_prompt_chars", taskID))}
		}
		completionChars, ok := intField(snapshot, "completion_chars")
		if !ok || completionChars <= 0 {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s char pricing snapshot missing completion_chars", taskID))}
		}
		return append(details, fmt.Sprintf("char_pricing_usage=%s prompt_chars=%d completion_chars=%d", taskID, promptChars, completionChars)), nil
	default:
		return details, nil
	}
}

func validateMatrixPricingRuleProof(sample, snapshot map[string]any, pricingSource, pricingMode string, details []string, name string) ([]string, *checkResult) {
	if pricingSource != "model_catalog" || pricingMode != "matrix" {
		return details, nil
	}
	taskID := stringField(sample, "task_id")
	matchedRule, ok := mapField(snapshot, "matched_rule")
	if !ok {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing snapshot missing matched_rule", taskID))}
	}
	if enabled, ok := boolField(matchedRule, "enabled"); ok && !enabled {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing matched_rule is disabled", taskID))}
	}
	unitPoints, ok := intField(matchedRule, "unit_points")
	if !ok || unitPoints <= 0 {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing matched_rule missing unit_points", taskID))}
	}
	count := intFieldOrZero(snapshot, "count")
	if count <= 0 {
		count = 1
	}
	actualPoints, ok := intField(snapshot, "actual_points")
	if !ok {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s pricing snapshot missing actual_points", taskID))}
	}
	expectedPoints := unitPoints * count
	if actualPoints != expectedPoints {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing actual_points mismatch: matched_rule=%d count=%d actual=%d", taskID, unitPoints, count, actualPoints))}
	}
	if err := validateOptionalMatchedRuleDimension(snapshot, matchedRule, "request_mode", "mode"); err != "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing %s", taskID, err))}
	}
	if err := validateOptionalMatchedRuleDimension(snapshot, matchedRule, "resolution", "resolution"); err != "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing %s", taskID, err))}
	}
	if err := validateOptionalMatchedRuleDimension(snapshot, matchedRule, "quality", "quality"); err != "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing %s", taskID, err))}
	}
	if err := validateOptionalMatchedRuleIntDimension(snapshot, matchedRule, "duration_sec", "duration_sec"); err != "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s matrix pricing %s", taskID, err))}
	}
	return append(details, fmt.Sprintf("matrix_pricing_rule=%s unit_points=%d count=%d", taskID, unitPoints, count)), nil
}

func validateOptionalMatchedRuleDimension(snapshot, matchedRule map[string]any, snapshotKey, ruleKey string) string {
	requestValue := normalizeProofToken(stringField(snapshot, snapshotKey))
	ruleValue := normalizeProofToken(stringField(matchedRule, ruleKey))
	if requestValue == "" && ruleValue == "" {
		return ""
	}
	if ruleValue == "" {
		return ""
	}
	if requestValue != ruleValue {
		return fmt.Sprintf("%s mismatch: request=%s matched_rule=%s", ruleKey, valueOrPlaceholder(requestValue), ruleValue)
	}
	return ""
}

func validateOptionalMatchedRuleIntDimension(snapshot, matchedRule map[string]any, snapshotKey, ruleKey string) string {
	ruleValue, ok := intField(matchedRule, ruleKey)
	if !ok || ruleValue == 0 {
		return ""
	}
	requestValue, ok := intField(snapshot, snapshotKey)
	if !ok || requestValue != ruleValue {
		return fmt.Sprintf("%s mismatch: request=%s matched_rule=%d", ruleKey, valueOrPlaceholder(intFieldString(snapshot, snapshotKey)), ruleValue)
	}
	return ""
}

func intFieldString(v map[string]any, key string) string {
	if n, ok := intField(v, key); ok {
		return strconv.Itoa(n)
	}
	return ""
}

func normalizeProofToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateAdminAuditOutputProof(sample map[string]any, details []string, name string) ([]string, *checkResult) {
	taskID := stringField(sample, "task_id")
	if taskID == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, "selected audit route sample missing task_id")}
	}
	statusValue, ok := intField(sample, "status")
	if !ok {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s missing task status", taskID))}
	}
	if statusValue != 2 {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s is not succeeded: status=%d", taskID, statusValue))}
	}
	details = append(details, fmt.Sprintf("same_task_status=%s status=%d", taskID, statusValue))
	kind := strings.ToLower(stringField(sample, "kind"))
	switch kind {
	case "image", "video":
		if stringField(sample, "preview_url") == "" {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s is succeeded but has no preview_url output proof", taskID))}
		}
		details = append(details, fmt.Sprintf("same_task_output=%s kind=%s preview=true", taskID, kind))
	case "chat", "text":
		outputSnapshot, ok := mapField(sample, "output_snapshot")
		if !ok {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s has no output_snapshot text proof", taskID))}
		}
		if forbidden := forbiddenSecretPaths(outputSnapshot, "output_snapshot"); len(forbidden) > 0 {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("output snapshot leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
		}
		outputPresent, ok := boolField(outputSnapshot, "output_present")
		if !ok || !outputPresent {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s output_snapshot does not prove text output", taskID))}
		}
		contentChars := intFieldOrZero(outputSnapshot, "content_chars")
		completionTokens := intFieldOrZero(outputSnapshot, "completion_tokens")
		if contentChars <= 0 && completionTokens <= 0 {
			return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s output_snapshot missing content_chars/completion_tokens", taskID))}
		}
		details = append(details, fmt.Sprintf("same_task_text_output=%s content_chars=%d completion_tokens=%d", taskID, contentChars, completionTokens))
	default:
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s has unsupported output proof kind %q", taskID, kind))}
	}
	return details, nil
}

func validateAdminAuditVideoJobProof(sample map[string]any, details []string, name string, opts options) ([]string, *checkResult) {
	taskID := stringField(sample, "task_id")
	if taskID == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, "selected audit route sample missing task_id")}
	}
	if kind := strings.ToLower(stringField(sample, "kind")); kind != "video" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s is not video: kind=%q", taskID, kind))}
	}
	if sourceType := stringField(sample, "selected_source_type"); sourceType != "api_channel" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s is not an API Channel route: source_type=%q", taskID, sourceType))}
	}
	videoJob, ok := mapField(sample, "video_job_snapshot")
	if !ok {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s has no video_job_snapshot", taskID))}
	}
	if forbidden := forbiddenSecretPaths(videoJob, "video_job_snapshot"); len(forbidden) > 0 {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("video job snapshot leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
	}
	jobSourceType := stringField(videoJob, "source_type")
	if jobSourceType != "api_channel" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s has invalid video job source_type=%q", taskID, jobSourceType))}
	}
	sourceCode := stringField(videoJob, "source_code")
	if sourceCode == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s missing video job source_code", taskID))}
	}
	selectedSourceCode := stringField(sample, "selected_source_code")
	if selectedSourceCode != "" && sourceCode != selectedSourceCode {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s video job source mismatch: got %s want selected %s", taskID, sourceCode, selectedSourceCode))}
	}
	if opts.APIChannelCode != "" && sourceCode != opts.APIChannelCode {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s video job source mismatch: got %s want %s", taskID, sourceCode, opts.APIChannelCode))}
	}
	upstreamModel := stringField(videoJob, "upstream_model")
	if upstreamModel == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s missing video job upstream_model", taskID))}
	}
	selectedUpstreamModel := stringField(sample, "selected_upstream_model")
	if selectedUpstreamModel != "" && upstreamModel != selectedUpstreamModel {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s video job upstream_model mismatch: got %s want selected %s", taskID, upstreamModel, selectedUpstreamModel))}
	}
	adapter := stringField(videoJob, "adapter")
	if adapter == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s missing video job adapter", taskID))}
	}
	phase := stringField(videoJob, "phase")
	if phase == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s missing video job phase", taskID))}
	}
	remoteTaskID := stringField(videoJob, "remote_task_id")
	if remoteTaskID == "" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s missing remote_task_id", taskID))}
	}
	fallbackLocked, ok := boolField(videoJob, "fallback_locked")
	if !ok || !fallbackLocked {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected video route sample %s does not prove fallback_locked=true", taskID))}
	}
	if statusValue, ok := intField(sample, "status"); ok && statusValue == 2 && phase != "terminal_success" {
		return details, &checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected succeeded video route sample %s has non-terminal-success phase=%s", taskID, phase))}
	}
	pollAttempts := intFieldOrZero(videoJob, "poll_attempts")
	return append(details, fmt.Sprintf("video_job=%s source=%s upstream=%s adapter=%s phase=%s remote_task_id=%s poll_attempts=%d fallback_locked=true", taskID, sourceCode, upstreamModel, adapter, phase, remoteTaskID, pollAttempts)), nil
}

func checkAdminBillingProof(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	const name = "admin_billing_proof"
	sample, details, errResult := findAdminAuditRouteSample(ctx, client, opts, token)
	if errResult != nil {
		errResult.Name = name
		return *errResult
	}
	taskID := stringField(sample, "task_id")
	if taskID == "" {
		return checkResult{Name: name, Status: "error", Details: append(details, "selected audit route sample missing task_id")}
	}
	statusValue, ok := intField(sample, "status")
	if !ok || statusValue != 2 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s is not succeeded: status=%d", taskID, statusValue))}
	}
	costPoints, ok := intField(sample, "cost_points")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s missing cost_points", taskID))}
	}
	pricingSnapshot, ok := mapField(sample, "pricing_snapshot")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("selected route sample %s has no pricing_snapshot for billing proof", taskID))}
	}
	if forbidden := forbiddenSecretPaths(pricingSnapshot, "pricing_snapshot"); len(forbidden) > 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
	}

	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.AdminBase+"/logs/generations/"+url.PathEscape(taskID)+"/billing", token, nil)
	if err != nil {
		return failed(name, "GET /admin/api/v1/logs/generations/:task_id/billing failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed(name, "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed(name, fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var data map[string]any
	if err := json.Unmarshal(body.Data, &data); err != nil {
		return failed(name, "invalid billing proof data", status, err)
	}
	if forbidden := forbiddenSecretPaths(data, "billing"); len(forbidden) > 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("billing proof leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
	}
	if got := stringField(data, "task_id"); got != taskID {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("billing proof task_id mismatch: got %s want %s", got, taskID))}
	}
	consume, ok := mapField(data, "consume_record")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("task %s has no consume_record", taskID))}
	}
	if got := stringField(consume, "task_id"); got != taskID {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("consume_record task_id mismatch: got %s want %s", got, taskID))}
	}
	consumeStatus, ok := intField(consume, "status")
	if !ok || consumeStatus != 1 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("consume_record status mismatch: got %d want 1(settled)", consumeStatus))}
	}
	consumeTotal, ok := intField(consume, "total_points")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, "consume_record missing total_points")}
	}
	if consumeTotal != costPoints {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("consume_record total_points mismatch: consume=%d task_cost=%d", consumeTotal, costPoints))}
	}

	walletLogs := sliceField(data, "wallet_logs")
	if len(walletLogs) == 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("task %s has no wallet_logs", taskID))}
	}
	var walletNet int
	var walletDebit int
	var walletExtra int
	var walletRefund int
	for i, rawLog := range walletLogs {
		log, ok := rawLog.(map[string]any)
		if !ok {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet_logs[%d] is not an object", i))}
		}
		bizType := stringField(log, "biz_type")
		bizID := stringField(log, "biz_id")
		points, ok := intField(log, "points")
		if bizType == "" || bizID == "" || !ok {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet_logs[%d] missing biz_type/biz_id/points", i))}
		}
		if bizType != "consume" && bizType != "refund" {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet_logs[%d] unexpected biz_type=%s", i, bizType))}
		}
		if bizID != taskID && bizID != taskID+":extra" {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet_logs[%d] unexpected biz_id=%s", i, bizID))}
		}
		walletNet += points
		if bizType == "consume" && points < 0 {
			debit := -points
			walletDebit += debit
			if bizID == taskID+":extra" {
				walletExtra += debit
			}
		}
		if bizType == "refund" && points > 0 {
			walletRefund += points
		}
	}
	walletSpend := -walletNet
	if walletSpend != costPoints {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet net spend mismatch: wallet=%d task_cost=%d", walletSpend, costPoints))}
	}
	refundRecords := sliceField(data, "refund_records")
	var refundTotal int
	for i, rawRefund := range refundRecords {
		refund, ok := rawRefund.(map[string]any)
		if !ok {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("refund_records[%d] is not an object", i))}
		}
		if got := stringField(refund, "task_id"); got != taskID {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("refund_records[%d] task_id mismatch: got %s want %s", i, got, taskID))}
		}
		points, ok := intField(refund, "points")
		if !ok || points <= 0 {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("refund_records[%d] missing positive points", i))}
		}
		refundTotal += points
	}
	if walletRefund > 0 && refundTotal == 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("wallet refund points=%d but no refund_records found", walletRefund))}
	}
	if refundTotal != walletRefund {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("refund_record total mismatch: refund_records=%d wallet_refund=%d", refundTotal, walletRefund))}
	}
	pricingActual, ok := intField(pricingSnapshot, "actual_points")
	if !ok {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot for %s missing actual_points", taskID))}
	}
	if pricingActual != costPoints {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot actual_points mismatch: pricing=%d task_cost=%d", pricingActual, costPoints))}
	}
	pricingRefund := intFieldOrZero(pricingSnapshot, "refund_points")
	if pricingRefund != walletRefund {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot refund_points mismatch: pricing=%d wallet_refund=%d", pricingRefund, walletRefund))}
	}
	pricingExtra := intFieldOrZero(pricingSnapshot, "extra_points")
	if pricingExtra != walletExtra {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot extra_points mismatch: pricing=%d wallet_extra=%d", pricingExtra, walletExtra))}
	}
	if preDeduct, ok := intField(pricingSnapshot, "pre_deduct_points"); ok {
		walletBaseDebit := walletDebit - walletExtra
		if preDeduct != walletBaseDebit {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("pricing snapshot pre_deduct_points mismatch: pricing=%d wallet_base_debit=%d", preDeduct, walletBaseDebit))}
		}
	}
	if summary, ok := mapField(data, "summary"); ok {
		if spend, ok := intField(summary, "wallet_spend_points"); ok && spend != costPoints {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("billing summary wallet_spend_points mismatch: summary=%d task_cost=%d", spend, costPoints))}
		}
		if refund, ok := intField(summary, "wallet_refund_points"); ok && refund != walletRefund {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("billing summary wallet_refund_points mismatch: summary=%d wallet_refund=%d", refund, walletRefund))}
		}
		if count, ok := intField(summary, "refund_record_count"); ok && count != len(refundRecords) {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("billing summary refund_record_count mismatch: summary=%d records=%d", count, len(refundRecords)))}
		}
	}
	return checkResult{
		Name:   name,
		Status: "ok",
		Details: append(details,
			fmt.Sprintf("billing_task=%s", taskID),
			fmt.Sprintf("consume_status=%d", consumeStatus),
			fmt.Sprintf("consume_total_points=%d", consumeTotal),
			fmt.Sprintf("wallet_logs=%d", len(walletLogs)),
			fmt.Sprintf("wallet_spend_points=%d", walletSpend),
			fmt.Sprintf("wallet_refund_points=%d", walletRefund),
			fmt.Sprintf("wallet_extra_points=%d", walletExtra),
			fmt.Sprintf("pricing_refund_points=%d", pricingRefund),
			fmt.Sprintf("pricing_extra_points=%d", pricingExtra),
			fmt.Sprintf("refund_records=%d", len(refundRecords)),
		),
	}
}

func findMatchingAdminUpstreamLog(ctx context.Context, client *http.Client, opts options, token, name string) (map[string]any, map[string]any, map[string]any, []string, *checkResult) {
	sample, details, errResult := findAdminAuditRouteSample(ctx, client, opts, token)
	if errResult != nil {
		errResult.Name = name
		return nil, nil, nil, nil, errResult
	}
	taskID := stringField(sample, "task_id")
	sourceType := stringField(sample, "selected_source_type")
	sourceCode := stringField(sample, "selected_source_code")
	upstreamModel := stringField(sample, "selected_upstream_model")
	selectedRoute := selectedRouteSnapshotCandidate(sample)
	if taskID == "" || sourceType == "" || sourceCode == "" {
		res := checkResult{Name: name, Status: "error", Details: append(details, "selected audit route sample missing task_id/source summary")}
		return nil, nil, nil, details, &res
	}

	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.AdminBase+"/logs/generations/"+url.PathEscape(taskID)+"/upstream", token, nil)
	if err != nil {
		res := failed(name, "GET /admin/api/v1/logs/generations/:task_id/upstream failed", status, err)
		return nil, nil, nil, details, &res
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		res := failed(name, "invalid response envelope", status, err)
		return nil, nil, nil, details, &res
	}
	if body.Code != 0 {
		res := failed(name, fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
		return nil, nil, nil, details, &res
	}
	var rows []map[string]any
	if err := json.Unmarshal(body.Data, &rows); err != nil {
		res := failed(name, "invalid upstream log list", status, err)
		return nil, nil, nil, details, &res
	}
	details = append(details, fmt.Sprintf("upstream_logs=%d", len(rows)))
	if len(rows) == 0 {
		res := checkResult{Name: name, Status: "error", Details: append(details, "empty upstream log list for selected audit task")}
		return nil, nil, nil, details, &res
	}

	for i, row := range rows {
		if forbidden := forbiddenSecretPaths(row, fmt.Sprintf("upstream[%d]", i)); len(forbidden) > 0 {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("upstream row leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
			return nil, nil, nil, details, &res
		}
		if metaForbidden := forbiddenMetaSecretPaths(row, fmt.Sprintf("upstream[%d].meta", i)); len(metaForbidden) > 0 {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("upstream meta leaks sensitive fields: %s", strings.Join(metaForbidden, ",")))}
			return nil, nil, nil, details, &res
		}
		if stringField(row, "task_id") != taskID {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("upstream[%d] task_id mismatch", i))}
			return nil, nil, nil, details, &res
		}
		stage := stringField(row, "stage")
		provider := stringField(row, "provider")
		if stage == "" || provider == "" {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("upstream[%d] missing provider/stage", i))}
			return nil, nil, nil, details, &res
		}
		if opts.UpstreamStage != "" && stage != opts.UpstreamStage {
			continue
		}
		if sourceType == "api_channel" {
			if provider != "api_channel" {
				continue
			}
			if opts.UpstreamStage == "" && !strings.HasPrefix(stage, "api_channel.") {
				continue
			}
			meta := parseJSONMapString(stringField(row, "meta"))
			metaSource := firstNonEmpty(stringField(meta, "api_channel_code"), stringField(meta, "model_gateway_source_code"))
			if opts.APIChannelCode != "" && metaSource != "" && metaSource != opts.APIChannelCode {
				continue
			}
			if opts.APIChannelCode != "" && metaSource == "" {
				continue
			}
			metaSourceType := stringField(meta, "model_gateway_source_type")
			if metaSourceType == "" {
				details = append(details, fmt.Sprintf("upstream[%d] matched api_channel provider but missing model_gateway_source_type in meta", i))
				continue
			}
			if metaSourceType != sourceType {
				details = append(details, fmt.Sprintf("upstream[%d] api_channel source_type mismatch: got %s want %s", i, metaSourceType, sourceType))
				continue
			}
			metaUpstreamModel := firstNonEmpty(stringField(meta, "upstream_model"), stringField(meta, "model"))
			if upstreamModel != "" {
				if metaUpstreamModel == "" {
					details = append(details, fmt.Sprintf("upstream[%d] matched channel but missing upstream_model in meta", i))
					continue
				}
				if metaUpstreamModel != upstreamModel {
					details = append(details, fmt.Sprintf("upstream[%d] upstream_model mismatch: got %s want %s", i, metaUpstreamModel, upstreamModel))
					continue
				}
			}
			if msg, mismatch := routeSnapshotMetaMismatch(meta, selectedRoute, "api_channel"); mismatch {
				details = append(details, fmt.Sprintf("upstream[%d] %s", i, msg))
				continue
			}
			details = append(details, fmt.Sprintf("matched_upstream=%s provider=%s stage=%s upstream=%s", taskID, provider, stage, upstreamModel))
			return sample, row, meta, details, nil
		}
		if sourceType == "account_pool" {
			if provider != sourceCode {
				continue
			}
			meta := parseJSONMapString(stringField(row, "meta"))
			metaSourceType := stringField(meta, "model_gateway_source_type")
			if metaSourceType == "" {
				details = append(details, fmt.Sprintf("upstream[%d] matched account_pool provider but missing model_gateway_source_type in meta", i))
				continue
			}
			if metaSourceType != sourceType {
				details = append(details, fmt.Sprintf("upstream[%d] account_pool source_type mismatch: got %s want %s", i, metaSourceType, sourceType))
				continue
			}
			metaSourceCode := stringField(meta, "model_gateway_source_code")
			if metaSourceCode == "" {
				details = append(details, fmt.Sprintf("upstream[%d] matched account_pool provider but missing model_gateway_source_code in meta", i))
				continue
			}
			if metaSourceCode != sourceCode {
				details = append(details, fmt.Sprintf("upstream[%d] account_pool source_code mismatch: got %s want %s", i, metaSourceCode, sourceCode))
				continue
			}
			metaUpstreamModel := firstNonEmpty(stringField(meta, "upstream_model"), stringField(meta, "model"))
			if upstreamModel != "" {
				if metaUpstreamModel == "" {
					details = append(details, fmt.Sprintf("upstream[%d] matched account_pool provider but missing upstream_model in meta", i))
					continue
				}
				if metaUpstreamModel != upstreamModel {
					details = append(details, fmt.Sprintf("upstream[%d] account_pool upstream_model mismatch: got %s want %s", i, metaUpstreamModel, upstreamModel))
					continue
				}
			}
			if msg, mismatch := routeSnapshotMetaMismatch(meta, selectedRoute, "account_pool"); mismatch {
				details = append(details, fmt.Sprintf("upstream[%d] %s", i, msg))
				continue
			}
			details = append(details, fmt.Sprintf("matched_upstream=%s provider=%s stage=%s upstream=%s", taskID, provider, stage, upstreamModel))
			return sample, row, meta, details, nil
		}
	}

	want := sourceType + "/" + sourceCode
	if opts.UpstreamStage != "" {
		want += " stage=" + opts.UpstreamStage
	}
	res := checkResult{Name: name, Status: "error", Details: append(details, "no upstream log matched selected route "+want)}
	return nil, nil, nil, details, &res
}

func selectedRouteSnapshotCandidate(sample map[string]any) map[string]any {
	snapshot, ok := mapField(sample, "model_gateway_route_snapshot")
	if !ok {
		return nil
	}
	candidates := sliceField(snapshot, "candidates")
	if len(candidates) == 0 {
		return nil
	}
	selectedIndex, hasSelectedIndex := intField(snapshot, "selected_index")
	if !hasSelectedIndex {
		return nil
	}
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if idx, ok := intField(candidate, "index"); ok && idx == selectedIndex {
			return candidate
		}
	}
	return nil
}

func routeSnapshotMetaMismatch(meta, selected map[string]any, label string) (string, bool) {
	if len(selected) == 0 {
		return "", false
	}
	if expected, ok := intField(selected, "index"); ok && expected > 0 {
		got, hasGot := intField(meta, "model_gateway_route_index")
		if !hasGot {
			return fmt.Sprintf("matched %s route but missing model_gateway_route_index in meta", label), true
		}
		if got != expected {
			return fmt.Sprintf("%s route_index mismatch: got %d want %d", label, got, expected), true
		}
		attempt, hasAttempt := intField(meta, "model_gateway_attempt")
		if !hasAttempt {
			return fmt.Sprintf("matched %s route but missing model_gateway_attempt in meta", label), true
		}
		if attempt <= 0 {
			return fmt.Sprintf("%s attempt must be positive: got %d", label, attempt), true
		}
	}
	if expected := normalizeRouteMetaValue(stringField(selected, "strategy")); expected != "" {
		got := normalizeRouteMetaValue(stringField(meta, "strategy"))
		if got == "" {
			return fmt.Sprintf("matched %s route but missing strategy in meta", label), true
		}
		if got != expected {
			return fmt.Sprintf("%s strategy mismatch: got %s want %s", label, got, expected), true
		}
	}
	if expected := normalizeRouteMetaValue(stringField(selected, "auth_type")); expected != "" {
		got := normalizeRouteMetaValue(stringField(meta, "auth_type"))
		if got == "" {
			return fmt.Sprintf("matched %s route but missing auth_type in meta", label), true
		}
		if got != expected {
			return fmt.Sprintf("%s auth_type mismatch: got %s want %s", label, got, expected), true
		}
	}
	return "", false
}

func routeSnapshotSummaryMismatch(sample, selected map[string]any) (string, bool) {
	snapshot, ok := mapField(sample, "model_gateway_route_snapshot")
	if !ok {
		return "route snapshot missing", true
	}
	candidates := sliceField(snapshot, "candidates")
	candidateCount, ok := intField(snapshot, "candidate_count")
	if !ok {
		return "route snapshot missing candidate_count", true
	}
	if candidateCount != len(candidates) {
		return fmt.Sprintf("route snapshot candidate_count mismatch: got %d actual %d", candidateCount, len(candidates)), true
	}
	skippedCandidates := sliceField(snapshot, "skipped_candidates")
	skippedCount, hasSkippedCount := intField(snapshot, "skipped_count")
	if len(skippedCandidates) > 0 {
		if !hasSkippedCount {
			return "route snapshot missing skipped_count", true
		}
		if skippedCount != len(skippedCandidates) {
			return fmt.Sprintf("route snapshot skipped_count mismatch: got %d actual %d", skippedCount, len(skippedCandidates)), true
		}
	} else if hasSkippedCount && skippedCount != 0 {
		return fmt.Sprintf("route snapshot skipped_count mismatch: got %d actual 0", skippedCount), true
	}
	for i, raw := range skippedCandidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			return fmt.Sprintf("route snapshot skipped candidate %d is not an object", i), true
		}
		idx, ok := intField(candidate, "index")
		if !ok {
			return fmt.Sprintf("route snapshot skipped candidate %d missing index", i), true
		}
		if idx != i+1 {
			return fmt.Sprintf("route snapshot skipped candidate %d index %d does not match expected %d", i, idx, i+1), true
		}
		if strings.TrimSpace(stringField(candidate, "source_type")) == "" {
			return fmt.Sprintf("route snapshot skipped candidate %d missing source_type", i), true
		}
		if strings.TrimSpace(stringField(candidate, "source_code")) == "" {
			return fmt.Sprintf("route snapshot skipped candidate %d missing source_code", i), true
		}
		if strings.TrimSpace(stringField(candidate, "skip_reason")) == "" {
			return fmt.Sprintf("route snapshot skipped candidate %d missing skip_reason", i), true
		}
	}
	for i, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			return fmt.Sprintf("route snapshot candidate %d is not an object", i), true
		}
		idx, ok := intField(candidate, "index")
		if !ok {
			return fmt.Sprintf("route snapshot candidate %d missing index", i), true
		}
		if idx != i+1 {
			return fmt.Sprintf("route snapshot candidate %d index %d does not match expected %d", i, idx, i+1), true
		}
	}
	if len(selected) == 0 {
		return "route snapshot missing selected candidate", true
	}
	summarySourceType := strings.TrimSpace(stringField(sample, "selected_source_type"))
	summarySourceCode := strings.TrimSpace(stringField(sample, "selected_source_code"))
	summaryUpstreamModel := strings.TrimSpace(stringField(sample, "selected_upstream_model"))
	selectedSourceType := strings.TrimSpace(stringField(selected, "source_type"))
	selectedSourceCode := strings.TrimSpace(stringField(selected, "source_code"))
	selectedUpstreamModel := strings.TrimSpace(stringField(selected, "upstream_model"))
	if selectedSourceType == "" {
		return "route snapshot selected candidate missing source_type", true
	}
	if summarySourceType != "" && selectedSourceType != summarySourceType {
		return fmt.Sprintf("route snapshot source_type mismatch: selected=%s summary=%s", selectedSourceType, summarySourceType), true
	}
	if selectedSourceCode == "" {
		return "route snapshot selected candidate missing source_code", true
	}
	if summarySourceCode != "" && selectedSourceCode != summarySourceCode {
		return fmt.Sprintf("route snapshot source_code mismatch: selected=%s summary=%s", selectedSourceCode, summarySourceCode), true
	}
	if summaryUpstreamModel != "" || selectedUpstreamModel != "" {
		if selectedUpstreamModel == "" {
			return "route snapshot selected candidate missing upstream_model", true
		}
		if summaryUpstreamModel == "" {
			return "audit summary missing selected_upstream_model", true
		}
		if selectedUpstreamModel != summaryUpstreamModel {
			return fmt.Sprintf("route snapshot upstream_model mismatch: selected=%s summary=%s", selectedUpstreamModel, summaryUpstreamModel), true
		}
	}
	return "", false
}

func normalizeRouteMetaValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func findAdminAuditRouteSample(ctx context.Context, client *http.Client, opts options, token string) (map[string]any, []string, *checkResult) {
	u, err := url.Parse(opts.AdminBase + "/model-gateway/audit")
	if err != nil {
		res := failed("admin_audit_route_sample", "invalid admin audit URL", 0, err)
		return nil, nil, &res
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "20")
	q.Set("audit_type", "route")
	if opts.ModelCode != "" {
		q.Set("model_code", opts.ModelCode)
	}
	if opts.TaskID != "" {
		q.Set("keyword", opts.TaskID)
	}
	u.RawQuery = q.Encode()

	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		res := failed("admin_audit_route_sample", "GET /admin/api/v1/model-gateway/audit failed", status, err)
		return nil, nil, &res
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		res := failed("admin_audit_route_sample", "invalid response envelope", status, err)
		return nil, nil, &res
	}
	if body.Code != 0 {
		res := failed("admin_audit_route_sample", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
		return nil, nil, &res
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		res := failed("admin_audit_route_sample", "invalid page data", status, err)
		return nil, nil, &res
	}
	details := []string{
		fmt.Sprintf("audit_total=%d", page.Total),
		fmt.Sprintf("audit_sample_rows=%d", len(page.List)),
	}
	for i, item := range page.List {
		if forbidden := forbiddenSecretPaths(item, fmt.Sprintf("row[%d]", i)); len(forbidden) > 0 {
			res := checkResult{Name: "admin_audit_route_sample", Status: "error", Details: append(details, fmt.Sprintf("audit row leaks sensitive fields: %s", strings.Join(forbidden, ",")))}
			return nil, nil, &res
		}
		if _, ok := mapField(item, "model_gateway_route_snapshot"); !ok {
			continue
		}
		if opts.ModelCode != "" && stringField(item, "model_code") != opts.ModelCode {
			continue
		}
		if opts.TaskID != "" && stringField(item, "task_id") != opts.TaskID {
			continue
		}
		sourceType := stringField(item, "selected_source_type")
		sourceCode := stringField(item, "selected_source_code")
		if opts.APIChannelCode != "" && !(sourceType == "api_channel" && sourceCode == opts.APIChannelCode) {
			continue
		}
		selectedRoute := selectedRouteSnapshotCandidate(item)
		if msg, mismatch := routeSnapshotSummaryMismatch(item, selectedRoute); mismatch {
			res := checkResult{Name: "admin_audit_route_sample", Status: "error", Details: append(details, msg)}
			return nil, nil, &res
		}
		details = append(details,
			fmt.Sprintf("audit_task=%s selected=%s/%s", stringField(item, "task_id"), sourceType, sourceCode),
			fmt.Sprintf("route_snapshot_selected=%s/%s upstream=%s", stringField(selectedRoute, "source_type"), stringField(selectedRoute, "source_code"), stringField(selectedRoute, "upstream_model")),
		)
		return item, details, nil
	}
	want := "route audit sample"
	if opts.APIChannelCode != "" {
		want = "api_channel/" + opts.APIChannelCode + " route audit sample"
	}
	if opts.TaskID != "" {
		want += " for task_id=" + opts.TaskID
	}
	res := checkResult{Name: "admin_audit_route_sample", Status: "error", Details: append(details, "no "+want+" found")}
	return nil, details, &res
}

func checkAdminCatalogModel(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	u, err := url.Parse(opts.AdminBase + "/model-gateway/models")
	if err != nil {
		return failed("admin_catalog_model", "invalid admin Model Catalog URL", 0, err)
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "50")
	q.Set("keyword", opts.ModelCode)
	u.RawQuery = q.Encode()

	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		return failed("admin_catalog_model", "GET /admin/api/v1/model-gateway/models failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("admin_catalog_model", "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed("admin_catalog_model", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		return failed("admin_catalog_model", "invalid page data", status, err)
	}
	details := []string{
		fmt.Sprintf("total=%d", page.Total),
		fmt.Sprintf("page=%d", page.Page),
		fmt.Sprintf("page_size=%d", page.PageSize),
		fmt.Sprintf("sample_rows=%d", len(page.List)),
	}
	if page.Page <= 0 || page.PageSize <= 0 || page.Total < 0 {
		return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, "invalid pagination fields")}
	}

	found := false
	for i, item := range page.List {
		if forbidden := forbiddenSecretFields(item); len(forbidden) > 0 {
			return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("model[%d] leaks sensitive fields: %s", i, strings.Join(forbidden, ",")))}
		}
		id := uint64Field(item, "id")
		code := stringField(item, "model_code")
		displayName := stringField(item, "display_name")
		entryKind := stringField(item, "entry_kind")
		pricingMode := stringField(item, "pricing_mode")
		if id == 0 || code == "" || displayName == "" || entryKind == "" || pricingMode == "" {
			return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("model[%d] missing required id/model_code/display_name/entry_kind/pricing_mode", i))}
		}
		if _, ok := intField(item, "visible"); !ok {
			return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("model[%d] missing visible number", i))}
		}
		if _, ok := intField(item, "status"); !ok {
			return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("model[%d] missing status number", i))}
		}
		if code == opts.ModelCode {
			found = true
			details = append(details,
				fmt.Sprintf("expected_catalog_model=%s present=true", opts.ModelCode),
				fmt.Sprintf("catalog_entry_kind=%s", entryKind),
				fmt.Sprintf("catalog_pricing_mode=%s", pricingMode),
				fmt.Sprintf("catalog_parameters_schema_present=%t", catalogItemHasParameterSchema(item)),
				fmt.Sprintf("catalog_unit_points=%d", intFieldOrZero(item, "unit_points")),
				fmt.Sprintf("catalog_input_unit_points=%d", intFieldOrZero(item, "input_unit_points")),
				fmt.Sprintf("catalog_output_unit_points=%d", intFieldOrZero(item, "output_unit_points")),
				fmt.Sprintf("catalog_visible=%d", intFieldOrZero(item, "visible")),
				fmt.Sprintf("catalog_status=%d", intFieldOrZero(item, "status")),
			)
			if opts.EntryKind != "" && entryKind != opts.EntryKind {
				return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("catalog entry_kind mismatch: got %s want %s", entryKind, opts.EntryKind))}
			}
			if opts.ExpectedPricingMode != "" && normalizePricingMode(pricingMode) != opts.ExpectedPricingMode {
				return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, fmt.Sprintf("catalog pricing_mode mismatch: got %s want %s", pricingMode, opts.ExpectedPricingMode))}
			}
			if opts.RequirePostGenerationProof && !catalogItemHasEffectivePricing(item, entryKind) {
				return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, "post-generation proof requires Model Catalog pricing to be effective; set token/char input/output, fixed unit_points, or matrix price_rules")}
			}
			if opts.RequireParameterSchema && !catalogItemHasParameterSchema(item) {
				return checkResult{Name: "admin_catalog_model", Status: "error", Details: append(details, "required Model Catalog parameters_schema is missing or empty; set controls for frontend model parameters")}
			}
		}
	}
	if !found {
		details = append(details, fmt.Sprintf("expected_catalog_model=%s present=false", opts.ModelCode))
		if opts.RequireCatalogModel {
			return checkResult{Name: "admin_catalog_model", Status: "error", Details: details}
		}
	}
	return checkResult{Name: "admin_catalog_model", Status: "ok", Details: details}
}

func checkAdminModelSources(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	u, err := url.Parse(opts.AdminBase + "/model-gateway/sources")
	if err != nil {
		return failed("admin_model_sources", "invalid admin Model Source Mapping URL", 0, err)
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "100")
	q.Set("model_code", opts.ModelCode)
	u.RawQuery = q.Encode()

	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		return failed("admin_model_sources", "GET /admin/api/v1/model-gateway/sources failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("admin_model_sources", "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed("admin_model_sources", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		return failed("admin_model_sources", "invalid page data", status, err)
	}
	details := []string{
		fmt.Sprintf("total=%d", page.Total),
		fmt.Sprintf("page=%d", page.Page),
		fmt.Sprintf("page_size=%d", page.PageSize),
		fmt.Sprintf("sample_rows=%d", len(page.List)),
	}
	if page.Page <= 0 || page.PageSize <= 0 || page.Total < 0 {
		return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, "invalid pagination fields")}
	}
	if opts.RequireSourceMapping && (strings.TrimSpace(opts.ModelCode) == "" || strings.TrimSpace(opts.APIChannelCode) == "") {
		return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, "--require-source-mapping needs --model and --api-channel")}
	}

	foundMapping := false
	for i, item := range page.List {
		if forbidden := forbiddenSecretFields(item); len(forbidden) > 0 {
			return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, fmt.Sprintf("source[%d] leaks sensitive fields: %s", i, strings.Join(forbidden, ",")))}
		}
		id := uint64Field(item, "id")
		modelCode := stringField(item, "model_code")
		sourceType := stringField(item, "source_type")
		sourceCode := stringField(item, "source_code")
		strategy := stringField(item, "strategy")
		if id == 0 || modelCode == "" || sourceType == "" || sourceCode == "" || strategy == "" {
			return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, fmt.Sprintf("source[%d] missing required id/model_code/source_type/source_code/strategy", i))}
		}
		for _, field := range []string{"priority", "weight", "status"} {
			if _, ok := intField(item, field); !ok {
				return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, fmt.Sprintf("source[%d] missing %s number", i, field))}
			}
		}
		if modelCode == opts.ModelCode && sourceType == "api_channel" && sourceCode == opts.APIChannelCode {
			foundMapping = true
			details = append(details,
				fmt.Sprintf("expected_source_mapping=%s->api_channel/%s present=true", opts.ModelCode, opts.APIChannelCode),
				fmt.Sprintf("mapping_adapter=%s", stringField(item, "adapter")),
				fmt.Sprintf("mapping_upstream_model=%s", stringField(item, "upstream_model")),
				fmt.Sprintf("mapping_priority=%d", intFieldOrZero(item, "priority")),
				fmt.Sprintf("mapping_weight=%d", intFieldOrZero(item, "weight")),
				fmt.Sprintf("mapping_status=%d", intFieldOrZero(item, "status")),
			)
		}
	}
	if opts.APIChannelCode != "" && !foundMapping {
		details = append(details, fmt.Sprintf("expected_source_mapping=%s->api_channel/%s present=false", opts.ModelCode, opts.APIChannelCode))
		if opts.RequireSourceMapping {
			return checkResult{Name: "admin_model_sources", Status: "error", Details: details}
		}
	}
	if len(page.List) == 0 && opts.RequireSourceMapping {
		return checkResult{Name: "admin_model_sources", Status: "error", Details: append(details, "empty Model Source Mapping list")}
	}
	return checkResult{Name: "admin_model_sources", Status: "ok", Details: details}
}

func checkAdminSourceConflicts(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	raw, status, err := requestJSON(ctx, client, http.MethodGet, opts.AdminBase+"/model-gateway/source-conflicts", token, nil)
	if err != nil {
		return failed("admin_source_conflicts", "GET /admin/api/v1/model-gateway/source-conflicts failed", status, err)
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("admin_source_conflicts", "invalid response envelope", status, err)
	}
	if body.Code != 0 {
		return failed("admin_source_conflicts", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
	}
	var items []map[string]any
	if err := json.Unmarshal(body.Data, &items); err != nil {
		return failed("admin_source_conflicts", "invalid data list", status, err)
	}

	modelConflicts := 0
	details := []string{fmt.Sprintf("conflicts=%d", len(items))}
	for i, item := range items {
		if forbidden := forbiddenSecretFields(item); len(forbidden) > 0 {
			return checkResult{Name: "admin_source_conflicts", Status: "error", Details: append(details, fmt.Sprintf("conflict[%d] leaks sensitive fields: %s", i, strings.Join(forbidden, ",")))}
		}
		id := uint64Field(item, "id")
		modelCode := stringField(item, "model_code")
		sourceType := stringField(item, "source_type")
		sourceCode := stringField(item, "source_code")
		reason := stringField(item, "reason")
		if id == 0 || modelCode == "" || sourceType == "" || sourceCode == "" || reason == "" {
			return checkResult{Name: "admin_source_conflicts", Status: "error", Details: append(details, fmt.Sprintf("conflict[%d] missing required id/model_code/source_type/source_code/reason", i))}
		}
		if _, ok := intField(item, "status"); !ok {
			return checkResult{Name: "admin_source_conflicts", Status: "error", Details: append(details, fmt.Sprintf("conflict[%d] missing status number", i))}
		}
		if opts.ModelCode != "" && modelCode == opts.ModelCode {
			modelConflicts++
			details = append(details, fmt.Sprintf("model_conflict[%d]=%s/%s reason=%s", modelConflicts, sourceType, sourceCode, reason))
		}
	}
	if opts.ModelCode != "" {
		details = append(details, fmt.Sprintf("model_conflicts=%d", modelConflicts))
	}
	if opts.RequireNoSourceConflicts {
		if opts.ModelCode != "" {
			if modelConflicts > 0 {
				return checkResult{Name: "admin_source_conflicts", Status: "error", Details: append(details, "source-conflicts contains expected model")}
			}
		} else if len(items) > 0 {
			return checkResult{Name: "admin_source_conflicts", Status: "error", Details: append(details, "source-conflicts is not empty")}
		}
	}
	return checkResult{Name: "admin_source_conflicts", Status: "ok", Details: details}
}

func checkAdminAPIChannels(ctx context.Context, client *http.Client, opts options, token string) (checkResult, uint64) {
	u, err := url.Parse(opts.AdminBase + "/api-channels")
	if err != nil {
		return failed("admin_api_channels", "invalid admin API Channel URL", 0, err), 0
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("page_size", "50")
	if opts.APIChannelCode != "" {
		q.Set("keyword", opts.APIChannelCode)
	}
	u.RawQuery = q.Encode()

	raw, status, err := requestJSON(ctx, client, http.MethodGet, u.String(), token, nil)
	if err != nil {
		return failed("admin_api_channels", "GET /admin/api/v1/api-channels failed", status, err), 0
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return failed("admin_api_channels", "invalid response envelope", status, err), 0
	}
	if body.Code != 0 {
		return failed("admin_api_channels", fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil), 0
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		return failed("admin_api_channels", "invalid page data", status, err), 0
	}
	details := []string{
		fmt.Sprintf("total=%d", page.Total),
		fmt.Sprintf("page=%d", page.Page),
		fmt.Sprintf("page_size=%d", page.PageSize),
		fmt.Sprintf("sample_rows=%d", len(page.List)),
	}
	if page.Page <= 0 || page.PageSize <= 0 || page.Total < 0 {
		return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, "invalid pagination fields")}, 0
	}

	var selectedID uint64
	found := false
	for i, item := range page.List {
		if forbidden := forbiddenSecretFields(item); len(forbidden) > 0 {
			return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("channel[%d] leaks sensitive fields: %s", i, strings.Join(forbidden, ",")))}, 0
		}
		id := uint64Field(item, "id")
		code := stringField(item, "code")
		name := stringField(item, "name")
		adapter := stringField(item, "adapter")
		baseURL := stringField(item, "base_url")
		if id == 0 || code == "" || name == "" || adapter == "" || baseURL == "" {
			return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("channel[%d] missing required id/code/name/adapter/base_url", i))}, 0
		}
		if _, ok := boolField(item, "has_api_key"); !ok {
			return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("channel[%d] missing has_api_key boolean", i))}, 0
		}
		hasLegacyKey, _ := boolField(item, "has_api_key")
		channelStatus, ok := intField(item, "status")
		if !ok {
			return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("channel[%d] missing status number", i))}, 0
		}
		lastTestStatus, hasLastTestStatus := intField(item, "last_test_status")
		lastTestAt := intFieldOrZero(item, "last_test_at")
		if selectedID == 0 {
			selectedID = id
			details = append(details, fmt.Sprintf("selected_channel=%s", code))
		}
		if opts.APIChannelCode != "" && code == opts.APIChannelCode {
			selectedID = id
			found = true
			details = append(details,
				fmt.Sprintf("expected_channel=%s present=true", opts.APIChannelCode),
				fmt.Sprintf("adapter=%s", adapter),
				fmt.Sprintf("key_count=%d", intFieldOrZero(item, "key_count")),
				fmt.Sprintf("enabled_key_count=%d", intFieldOrZero(item, "enabled_key_count")),
				fmt.Sprintf("legacy_channel_key=%t", hasLegacyKey),
				fmt.Sprintf("channel_status=%d", channelStatus),
				fmt.Sprintf("last_test_status=%d", lastTestStatus),
				fmt.Sprintf("last_test_at=%d", lastTestAt),
			)
			if opts.ForbidLegacyChannelKey && hasLegacyKey {
				return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("expected_channel=%s still has legacy channel API key", opts.APIChannelCode))}, 0
			}
			if opts.RequireAPIChannelHealth {
				if channelStatus != 1 {
					return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("expected_channel=%s disabled: status=%d", opts.APIChannelCode, channelStatus))}, 0
				}
				if !hasLastTestStatus || lastTestStatus != 1 {
					return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("expected_channel=%s health check not OK: last_test_status=%d", opts.APIChannelCode, lastTestStatus))}, 0
				}
				if lastTestAt <= 0 {
					return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, fmt.Sprintf("expected_channel=%s health check has no last_test_at", opts.APIChannelCode))}, 0
				}
			}
		}
	}
	if opts.APIChannelCode != "" && !found {
		details = append(details, fmt.Sprintf("expected_channel=%s present=false", opts.APIChannelCode))
		if opts.RequireAPIChannel || opts.RequireAPIChannelHealth || opts.ForbidLegacyChannelKey {
			return checkResult{Name: "admin_api_channels", Status: "error", Details: details}, 0
		}
		selectedID = 0
	}
	if len(page.List) == 0 && (opts.RequireAPIChannel || opts.RequireAPIChannelHealth || opts.RequireKeyPool || opts.ForbidLegacyChannelKey) {
		return checkResult{Name: "admin_api_channels", Status: "error", Details: append(details, "empty API Channel list")}, 0
	}
	return checkResult{Name: "admin_api_channels", Status: "ok", Details: details}, selectedID
}

func checkAdminAPIChannelKeys(ctx context.Context, client *http.Client, opts options, token string, channelID uint64) checkResult {
	keys, details, errResult := fetchAdminAPIChannelKeys(ctx, client, opts, token, channelID, "admin_api_channel_keys")
	if errResult != nil {
		return *errResult
	}
	if len(keys) == 0 {
		if opts.RequireKeyPool {
			return checkResult{Name: "admin_api_channel_keys", Status: "error", Details: append(details, "selected channel has no key-pool rows")}
		}
		return checkResult{Name: "admin_api_channel_keys", Status: "warn", Details: append(details, "selected channel has no key-pool rows")}
	}
	enabledUsable := 0
	for _, key := range keys {
		status, _ := intField(key, "status")
		hasAPIKey, _ := boolField(key, "has_api_key")
		if status == 1 && hasAPIKey {
			enabledUsable++
		}
	}
	details = append(details, fmt.Sprintf("enabled_usable_keys=%d", enabledUsable))
	if opts.RequireKeyPool && enabledUsable == 0 {
		return checkResult{Name: "admin_api_channel_keys", Status: "error", Details: append(details, "selected channel has no enabled key-pool rows with api key")}
	}
	return checkResult{Name: "admin_api_channel_keys", Status: "ok", Details: details}
}

func fetchAdminAPIChannelKeys(ctx context.Context, client *http.Client, opts options, token string, channelID uint64, name string) ([]map[string]any, []string, *checkResult) {
	target := fmt.Sprintf("%s/api-channels/%d/keys", opts.AdminBase, channelID)
	raw, status, err := requestJSON(ctx, client, http.MethodGet, target, token, nil)
	if err != nil {
		res := failed(name, "GET /admin/api/v1/api-channels/:id/keys failed", status, err)
		return nil, nil, &res
	}
	var body apiBody
	if err := json.Unmarshal(raw, &body); err != nil {
		res := failed(name, "invalid response envelope", status, err)
		return nil, nil, &res
	}
	if body.Code != 0 {
		res := failed(name, fmt.Sprintf("response code=%d msg=%s", body.Code, body.Msg), status, nil)
		return nil, nil, &res
	}
	var data struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		res := failed(name, "invalid data.list", status, err)
		return nil, nil, &res
	}
	details := []string{fmt.Sprintf("channel_id=%d", channelID), fmt.Sprintf("keys=%d", len(data.List))}
	for i, item := range data.List {
		if forbidden := forbiddenSecretFields(item); len(forbidden) > 0 {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("key[%d] leaks sensitive fields: %s", i, strings.Join(forbidden, ",")))}
			return nil, details, &res
		}
		if uint64Field(item, "id") == 0 || uint64Field(item, "channel_id") == 0 {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("key[%d] missing id or channel_id", i))}
			return nil, details, &res
		}
		if _, ok := boolField(item, "has_api_key"); !ok {
			res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("key[%d] missing has_api_key boolean", i))}
			return nil, details, &res
		}
		for _, field := range []string{"priority", "weight", "rpm_limit", "tpm_limit", "status"} {
			if _, ok := intField(item, field); !ok {
				res := checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("key[%d] missing %s number", i, field))}
				return nil, details, &res
			}
		}
	}
	return data.List, details, nil
}

func checkAdminKeyUsageFeedback(ctx context.Context, client *http.Client, opts options, token string, channelID uint64) checkResult {
	const name = "admin_key_usage_feedback"
	if strings.TrimSpace(opts.APIChannelCode) == "" {
		return checkResult{Name: name, Status: "error", Details: []string{"--require-key-usage-feedback needs --api-channel"}}
	}
	if channelID == 0 {
		return checkResult{Name: name, Status: "error", Details: []string{"cannot check key usage feedback because no API channel was selected"}}
	}
	sample, row, meta, details, errResult := findMatchingAdminUpstreamLog(ctx, client, opts, token, name)
	if errResult != nil {
		return *errResult
	}
	if stringField(sample, "selected_source_type") != "api_channel" {
		return checkResult{Name: name, Status: "error", Details: append(details, "selected audit route sample is not an API Channel route")}
	}
	if stringField(row, "provider") != "api_channel" {
		return checkResult{Name: name, Status: "error", Details: append(details, "matched upstream log provider is not api_channel")}
	}
	if source := stringField(meta, "api_channel_credential_source"); source != "key_pool" {
		return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("credential source is %q, want key_pool", source))}
	}
	keyID := uint64Field(meta, "api_channel_key_id")
	if keyID == 0 {
		return checkResult{Name: name, Status: "error", Details: append(details, "upstream log meta missing api_channel_key_id")}
	}

	keys, keyDetails, keyErr := fetchAdminAPIChannelKeys(ctx, client, opts, token, channelID, name)
	if keyErr != nil {
		return *keyErr
	}
	details = append(details, keyDetails...)
	for _, key := range keys {
		if uint64Field(key, "id") != keyID {
			continue
		}
		if uint64Field(key, "channel_id") != channelID {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("matched key channel_id mismatch: got %d want %d", uint64Field(key, "channel_id"), channelID))}
		}
		lastUsedAt, ok := intField(key, "last_used_at")
		if !ok || lastUsedAt <= 0 {
			return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("matched key %d has no last_used_at usage feedback", keyID))}
		}
		details = append(details,
			fmt.Sprintf("matched_key_id=%d", keyID),
			fmt.Sprintf("key_last_used_at=%d", lastUsedAt),
		)
		if keyName := stringField(key, "name"); keyName != "" {
			details = append(details, "key_name="+keyName)
		}
		return checkResult{Name: name, Status: "ok", Details: details}
	}
	return checkResult{Name: name, Status: "error", Details: append(details, fmt.Sprintf("api_channel_key_id=%d not found in selected channel key pool", keyID))}
}

func checkAdminDryRun(ctx context.Context, client *http.Client, opts options, token string) checkResult {
	body := map[string]string{"model_code": opts.ModelCode}
	if strings.TrimSpace(opts.EntryKind) != "" {
		body["entry_kind"] = opts.EntryKind
	}
	raw, status, err := requestJSON(ctx, client, http.MethodPost, opts.AdminBase+"/model-gateway/dry-run", token, body)
	if err != nil {
		return failed("admin_dry_run", "POST /admin/api/v1/model-gateway/dry-run failed", status, err)
	}
	var envelope apiBody
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return failed("admin_dry_run", "invalid response envelope", status, err)
	}
	if envelope.Code != 0 {
		return failed("admin_dry_run", fmt.Sprintf("response code=%d msg=%s", envelope.Code, envelope.Msg), status, nil)
	}
	var dataMap map[string]any
	if err := json.Unmarshal(envelope.Data, &dataMap); err != nil {
		return failed("admin_dry_run", "invalid dry-run data object", status, err)
	}
	if forbidden := forbiddenSecretPaths(dataMap, "dry_run"); len(forbidden) > 0 {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{fmt.Sprintf("dry-run response leaks sensitive fields: %s", strings.Join(forbidden, ","))}}
	}
	for _, field := range []string{"model_code", "entry_kind", "matched_model", "selected_index", "candidate_count", "available_count", "candidates"} {
		if _, ok := dataMap[field]; !ok {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{fmt.Sprintf("dry-run missing required field %s", field)}}
		}
	}
	if stringField(dataMap, "model_code") == "" {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{"dry-run field model_code must be a non-empty string"}}
	}
	if stringField(dataMap, "entry_kind") == "" {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{"dry-run field entry_kind must be a non-empty string"}}
	}
	if _, ok := boolField(dataMap, "matched_model"); !ok {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{"dry-run field matched_model must be boolean"}}
	}
	for _, field := range []string{"selected_index", "candidate_count", "available_count"} {
		if _, ok := intField(dataMap, field); !ok {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{fmt.Sprintf("dry-run field %s must be numeric", field)}}
		}
	}
	if _, ok := dataMap["candidates"].([]any); !ok {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: []string{"dry-run field candidates must be an array"}}
	}
	var data struct {
		ModelCode      string           `json:"model_code"`
		EntryKind      string           `json:"entry_kind"`
		MatchedModel   bool             `json:"matched_model"`
		SelectedIndex  int              `json:"selected_index"`
		CandidateCount int              `json:"candidate_count"`
		AvailableCount int              `json:"available_count"`
		Candidates     []map[string]any `json:"candidates"`
		Warning        string           `json:"warning"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return failed("admin_dry_run", "invalid dry-run data", status, err)
	}
	details := []string{
		fmt.Sprintf("model=%s", data.ModelCode),
		fmt.Sprintf("entry_kind=%s", data.EntryKind),
		fmt.Sprintf("candidate_count=%d", data.CandidateCount),
		fmt.Sprintf("available_count=%d", data.AvailableCount),
		fmt.Sprintf("selected_index=%d", data.SelectedIndex),
	}
	if data.Warning != "" {
		details = append(details, "warning="+data.Warning)
	}
	if data.ModelCode != opts.ModelCode || !data.MatchedModel {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "dry-run did not match expected model")}
	}
	if opts.EntryKind != "" && strings.TrimSpace(data.EntryKind) != opts.EntryKind {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("dry-run entry_kind mismatch: got %s want %s", data.EntryKind, opts.EntryKind))}
	}
	if data.SelectedIndex < 0 {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "selected_index must be zero or positive")}
	}
	if data.CandidateCount != len(data.Candidates) {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "candidate_count does not match candidates length")}
	}
	var selected map[string]any
	availableAccountPools := 0
	computedAvailable := 0
	selectedMatches := 0
	seenIndexes := map[int]bool{}
	for i, c := range data.Candidates {
		if stringField(c, "source_type") == "" || stringField(c, "source_code") == "" {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] missing source_type or source_code", i))}
		}
		idx, hasIndex := intField(c, "index")
		if !hasIndex || idx <= 0 {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] missing positive index", i))}
		}
		if seenIndexes[idx] {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("duplicate candidate index %d", idx))}
		}
		if idx != i+1 {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] index %d does not match expected %d", i, idx, i+1))}
		}
		seenIndexes[idx] = true
		available, hasAvailable := boolField(c, "available")
		if !hasAvailable {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] missing available", i))}
		}
		skipReason := strings.TrimSpace(stringField(c, "skip_reason"))
		if available {
			computedAvailable++
			if skipReason != "" {
				return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] is available but has skip_reason", i))}
			}
		} else if skipReason == "" {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("candidate[%d] unavailable but missing skip_reason", i))}
		}
		if idx == data.SelectedIndex {
			selected = c
			selectedMatches++
		}
		if stringField(c, "source_type") == "account_pool" {
			if available {
				availableAccountPools++
			}
		}
	}
	if data.AvailableCount != computedAvailable {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("available_count mismatch: got %d actual %d", data.AvailableCount, computedAvailable))}
	}
	if computedAvailable > 0 && data.SelectedIndex <= 0 {
		return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "dry-run has available candidates but no selected_index")}
	}
	if data.SelectedIndex > 0 {
		if selectedMatches == 0 {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("selected_index %d does not match any candidate", data.SelectedIndex))}
		}
		if selectedMatches > 1 {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("selected_index %d matches multiple candidates", data.SelectedIndex))}
		}
		if available, _ := boolField(selected, "available"); !available {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("selected_index %d points to unavailable candidate", data.SelectedIndex))}
		}
	}
	if selected != nil {
		details = append(details, fmt.Sprintf("selected_source=%s/%s", stringField(selected, "source_type"), stringField(selected, "source_code")))
	}
	if opts.RequireRouteChannel {
		if strings.TrimSpace(opts.APIChannelCode) == "" {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "--require-route-channel needs --api-channel")}
		}
		if data.SelectedIndex <= 0 || selected == nil {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "dry-run has no selected candidate")}
		}
		if stringField(selected, "source_type") != "api_channel" || stringField(selected, "source_code") != opts.APIChannelCode {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("selected candidate is not required API Channel %s", opts.APIChannelCode))}
		}
		if available, _ := boolField(selected, "available"); !available {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, fmt.Sprintf("selected API Channel %s is not available", opts.APIChannelCode))}
		}
		details = append(details, fmt.Sprintf("required_route_channel=%s", opts.APIChannelCode))
	}
	if opts.ForbidAccountPoolRoute {
		details = append(details, fmt.Sprintf("available_account_pool_candidates=%d", availableAccountPools))
		if availableAccountPools > 0 {
			return checkResult{Name: "admin_dry_run", Status: "error", Details: append(details, "dry-run still has available account_pool candidates")}
		}
	}
	return checkResult{Name: "admin_dry_run", Status: "ok", Details: details}
}

func requestJSON(ctx context.Context, client *http.Client, method, target, token string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return raw, res.StatusCode, fmt.Errorf("http %d: %s", res.StatusCode, trimForError(raw))
	}
	return raw, res.StatusCode, nil
}

func (r *report) add(c checkResult) {
	r.Checks = append(r.Checks, c)
}

func failed(name, message string, status int, err error) checkResult {
	details := []string{message}
	if status > 0 {
		details = append(details, fmt.Sprintf("http_status=%d", status))
	}
	if err != nil {
		details = append(details, err.Error())
	}
	return checkResult{Name: name, Status: "error", Details: details}
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func intField(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func intFieldOrZero(m map[string]any, key string) int {
	v, _ := intField(m, key)
	return v
}

func uint64Field(m map[string]any, key string) uint64 {
	switch v := m[key].(type) {
	case float64:
		if v <= 0 {
			return 0
		}
		return uint64(v)
	case int:
		if v <= 0 {
			return 0
		}
		return uint64(v)
	case int64:
		if v <= 0 {
			return 0
		}
		return uint64(v)
	case uint64:
		return v
	case json.Number:
		n, err := v.Int64()
		if err != nil || n <= 0 {
			return 0
		}
		return uint64(n)
	default:
		return 0
	}
}

func boolField(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func catalogItemHasEffectivePricing(item map[string]any, entryKind string) bool {
	unit := intFieldOrZero(item, "unit_points")
	input := intFieldOrZero(item, "input_unit_points")
	output := intFieldOrZero(item, "output_unit_points")
	switch strings.ToLower(strings.TrimSpace(entryKind)) {
	case "text", "chat":
		return input > 0 || output > 0 || unit > 0
	case "image", "video":
		return unit > 0 || arrayFieldLen(item, "price_rules") > 0
	default:
		return unit > 0 || input > 0 || output > 0 || arrayFieldLen(item, "price_rules") > 0
	}
}

func catalogItemHasParameterSchema(item map[string]any) bool {
	value, ok := item["parameters_schema"]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		if controls, ok := v["controls"].([]any); ok {
			return len(controls) > 0
		}
		return len(v) > 0
	case []any:
		return len(v) > 0
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			return false
		}
		return catalogItemHasParameterSchema(map[string]any{"parameters_schema": decoded})
	default:
		return false
	}
}

func publicModelItemHasParameterSchema(item map[string]any) bool {
	return catalogItemHasParameterSchema(item)
}

func normalizePricingMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func openAIModelItemPricingMode(item map[string]any) string {
	if mode := normalizePricingMode(stringField(item, "pricing_mode")); mode != "" {
		return mode
	}
	meta, ok := mapField(item, "meta")
	if !ok {
		return ""
	}
	return normalizePricingMode(stringField(meta, "pricing_mode"))
}

func openAIModelItemHasParameterSchema(item map[string]any) bool {
	if catalogItemHasParameterSchema(item) {
		return true
	}
	meta, ok := mapField(item, "meta")
	if !ok {
		return false
	}
	return catalogItemHasParameterSchema(meta)
}

func isTextLikeKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "text", "chat":
		return true
	default:
		return false
	}
}

func arrayFieldLen(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case []any:
		return len(v)
	case string:
		var rows []any
		if err := json.Unmarshal([]byte(v), &rows); err == nil {
			return len(rows)
		}
	}
	return 0
}

func mapField(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key].(map[string]any)
	return v, ok
}

func sliceField(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func forbiddenSecretFields(m map[string]any) []string {
	forbidden := []string{}
	for _, key := range []string{"api_key", "credential", "credential_enc", "token", "secret", "access_token", "refresh_token"} {
		if _, ok := m[key]; ok {
			forbidden = append(forbidden, key)
		}
	}
	return forbidden
}

func forbiddenSecretPaths(v any, prefix string) []string {
	out := []string{}
	switch typed := v.(type) {
	case map[string]any:
		for _, key := range forbiddenSecretFields(typed) {
			out = append(out, prefix+"."+key)
		}
		for key, child := range typed {
			out = append(out, forbiddenSecretPaths(child, prefix+"."+key)...)
		}
	case []any:
		for i, child := range typed {
			out = append(out, forbiddenSecretPaths(child, fmt.Sprintf("%s[%d]", prefix, i))...)
		}
	}
	return out
}

func forbiddenMetaSecretPaths(m map[string]any, prefix string) []string {
	meta := stringField(m, "meta")
	if meta == "" {
		return nil
	}
	parsed := parseJSONMapString(meta)
	if len(parsed) == 0 {
		return nil
	}
	return forbiddenSecretPaths(parsed, prefix)
}

func parseJSONMapString(value string) map[string]any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil
	}
	return out
}

func compactCounts(values map[string]int) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, values[k]))
	}
	return strings.Join(parts, ",")
}

func trimForError(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
