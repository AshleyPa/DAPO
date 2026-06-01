// Command modelgw-config idempotently seeds official API channels into the
// Model Gateway without writing plaintext credentials to source-controlled
// files.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kleinai/backend/internal/bootstrap"
	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/config"
	"github.com/kleinai/backend/pkg/database"
	"github.com/kleinai/backend/pkg/logger"
	"github.com/kleinai/backend/pkg/version"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const serviceName = "modelgw-config"

type providerSeed struct {
	Label            string
	APIKeyEnv        string
	APIKeyValue      string
	APIKeySource     string
	ChannelCode      string
	ChannelName      string
	ProviderName     string
	BaseURL          string
	KeyName          string
	PublicModel      string
	UpstreamModel    string
	DisplayName      string
	EntryKind        string
	Capabilities     []string
	ParametersSchema any
	Tags             []string
	PricingMode      string
	UnitPoints       int64
	InputUnitPoints  int64
	OutputUnitPoints int64
	PricingExplicit  bool
	Priority         int
	TimeoutSeconds   int
	Remark           string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type commandOptions struct {
	PlanOnly      bool
	AuditOnly     bool
	ProviderProbe bool
	SchemaCheck   bool
	MigrationInv  bool
	DBTargetCheck bool
	TargetEnv     string
	ConfirmWrite  bool
	KeyStdin      bool
	ProviderScope string
}

type apiChannelKeyService interface {
	ListKeys(ctx context.Context, channelID uint64) ([]*dto.APIChannelKeyResp, error)
	CreateKey(ctx context.Context, channelID uint64, req *dto.APIChannelKeyCreateReq) (*model.APIChannelKey, error)
	UpdateKey(ctx context.Context, channelID, keyID uint64, req *dto.APIChannelKeyUpdateReq) error
}

func run() error {
	opts := parseOptions()
	if opts.MigrationInv {
		return printMigrationInventory(os.Stdout, "")
	}
	if opts.DBTargetCheck {
		return printDBTargetCheck(os.Stdout, opts.TargetEnv, os.Getenv("KLEIN_DB_DSN"))
	}
	planSeeds, err := selectedProviderSeeds(opts.ProviderScope)
	if err != nil {
		return err
	}
	if err := validateKeyStdinOption(opts, planSeeds); err != nil {
		return err
	}
	if opts.PlanOnly {
		return printPlan(os.Stdout, planSeeds)
	}
	if opts.ProviderProbe {
		seeds, err := enabledProviderSeedsForScopeWithInput(opts.ProviderScope, opts.KeyStdin, os.Stdin)
		if err != nil {
			return err
		}
		if len(seeds) == 0 {
			return noProviderKeyError(opts.ProviderScope, planSeeds)
		}
		return runProviderProbe(context.Background(), os.Stdout, seeds, nil)
	}
	if err := ensureWriteConfirmedBeforeBootstrap(opts); err != nil {
		return err
	}
	if opts.SchemaCheck || opts.AuditOnly {
		return runReadOnlyDBCommand(context.Background(), opts)
	}

	deps, err := bootstrap.Init(serviceName)
	if err != nil {
		return err
	}
	defer logger.Sync()

	if deps.DB == nil {
		return errors.New("database unavailable: start MySQL and set KLEIN_DB_DSN before running modelgw-config")
	}
	if !opts.AuditOnly && !opts.SchemaCheck && deps.AES == nil {
		return errors.New("KLEIN_AES_KEY is required so API keys can be encrypted before storage")
	}
	if sqlDB, err := deps.DB.DB(); err == nil {
		defer sqlDB.Close()
	}

	apiRepo := repo.NewAPIChannelRepo(deps.DB)
	modelRepo := repo.NewModelCatalogRepo(deps.DB)
	sourceRepo := repo.NewModelSourceRepo(deps.DB)
	apiSvc := service.NewAPIChannelAdminService(apiRepo, deps.AES)
	modelSvc := service.NewModelGatewayAdminService(modelRepo, sourceRepo, apiRepo, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	seeds, err := enabledProviderSeedsForScopeWithInput(opts.ProviderScope, opts.KeyStdin, os.Stdin)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		return noProviderKeyError(opts.ProviderScope, planSeeds)
	}

	for _, seed := range seeds {
		if err := configureProvider(ctx, apiRepo, modelRepo, sourceRepo, apiSvc, modelSvc, seed); err != nil {
			return fmt.Errorf("%s: %w", seed.Label, err)
		}
		fmt.Printf("configured %s: channel=%s public_model=%s upstream_model=%s source=api_channel key_pool=%s\n",
			seed.Label,
			seed.ChannelCode,
			seed.PublicModel,
			effectiveUpstreamModel(seed),
			seed.KeyName,
		)
	}
	scope, err := auditScopeForSeeds(opts.ProviderScope, seeds)
	if err != nil {
		return err
	}
	if err := printConfigurationAudit(ctx, os.Stdout, apiRepo, modelRepo, sourceRepo, scope, false); err != nil {
		return err
	}
	return printPostConfigureNextSteps(os.Stdout, seeds)
}

func parseOptions() commandOptions {
	var opts commandOptions
	flag.BoolVar(&opts.PlanOnly, "plan", false, "print a sanitized configuration plan without connecting to the database")
	flag.BoolVar(&opts.AuditOnly, "audit-only", false, "read the database and report conflicting or duplicate source mappings without writing")
	flag.BoolVar(&opts.ProviderProbe, "provider-probe", false, "probe official API providers from env keys without connecting to the database or printing secrets")
	flag.BoolVar(&opts.SchemaCheck, "schema-check", false, "read the database schema and fail when Model Gateway required tables or columns are missing")
	flag.BoolVar(&opts.MigrationInv, "migration-inventory", false, "scan local backend/migrations and print the release migration inventory without connecting to the database")
	flag.BoolVar(&opts.DBTargetCheck, "db-target-check", false, "inspect KLEIN_DB_DSN without connecting and fail when the target is unsafe for migration dry-run")
	flag.StringVar(&opts.TargetEnv, "target-env", envOrDefault("DAPO_MODELGW_TARGET_ENV", "staging"), "target environment label for --db-target-check: staging, dryrun, production, etc.")
	flag.BoolVar(&opts.ConfirmWrite, "confirm-write", false, "allow modelgw-config to write API Channels, Key Pool rows, Model Catalog and Source Mapping after preflight gates pass")
	flag.BoolVar(&opts.KeyStdin, "key-stdin", false, "read one provider API key from stdin for --provider-probe or --confirm-write; requires a single --provider scope")
	flag.StringVar(&opts.ProviderScope, "provider", envOrDefault("DAPO_MODELGW_PROVIDER", "all"), "official API provider scope for plan/probe/write: all, mimo, deepseek, or a configured channel code")
	flag.Parse()
	return opts
}

var modelGatewayRequiredMigrationFiles = []string{
	"20260530161000_api_channels.sql",
	"20260530173000_model_gateway_catalog.sql",
	"20260530212000_api_channel_keys.sql",
	"20260531040500_model_catalog_parameters_schema.sql",
	"20260531064000_api_channel_optional_legacy_credential.sql",
}

type migrationInventory struct {
	OK              bool                      `json:"ok"`
	MigrationsDir   string                    `json:"migrations_dir"`
	Total           int                       `json:"total"`
	Required        []string                  `json:"required_model_gateway_migrations"`
	RequiredPresent []string                  `json:"required_present"`
	RequiredMissing []string                  `json:"required_missing"`
	Migrations      []migrationInventoryEntry `json:"migrations"`
}

type migrationInventoryEntry struct {
	File                 string `json:"file"`
	SizeBytes            int64  `json:"size_bytes"`
	SHA256               string `json:"sha256"`
	ModelGatewayRequired bool   `json:"model_gateway_required"`
}

func printMigrationInventory(w io.Writer, migrationsDir string) error {
	inventory, err := buildMigrationInventory(migrationsDir)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	if !inventory.OK {
		return fmt.Errorf("missing required Model Gateway migrations: %s", strings.Join(inventory.RequiredMissing, ", "))
	}
	return nil
}

func buildMigrationInventory(migrationsDir string) (migrationInventory, error) {
	dir := strings.TrimSpace(migrationsDir)
	if dir == "" {
		found, err := defaultMigrationDir()
		if err != nil {
			return migrationInventory{}, err
		}
		dir = found
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return migrationInventory{}, fmt.Errorf("read migrations dir: %w", err)
	}
	required := make(map[string]bool, len(modelGatewayRequiredMigrationFiles))
	for _, name := range modelGatewayRequiredMigrationFiles {
		required[name] = true
	}
	inv := migrationInventory{
		MigrationsDir:   filepath.Clean(dir),
		Required:        append([]string(nil), modelGatewayRequiredMigrationFiles...),
		RequiredPresent: []string{},
		RequiredMissing: []string{},
		Migrations:      []migrationInventoryEntry{},
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return migrationInventory{}, fmt.Errorf("stat migration %s: %w", entry.Name(), err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return migrationInventory{}, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(raw)
		item := migrationInventoryEntry{
			File:                 entry.Name(),
			SizeBytes:            info.Size(),
			SHA256:               fmt.Sprintf("%x", sum[:]),
			ModelGatewayRequired: required[entry.Name()],
		}
		inv.Migrations = append(inv.Migrations, item)
		if item.ModelGatewayRequired {
			inv.RequiredPresent = append(inv.RequiredPresent, item.File)
		}
	}
	sort.Slice(inv.Migrations, func(i, j int) bool { return inv.Migrations[i].File < inv.Migrations[j].File })
	sort.Strings(inv.RequiredPresent)
	present := make(map[string]bool, len(inv.RequiredPresent))
	for _, name := range inv.RequiredPresent {
		present[name] = true
	}
	for _, name := range inv.Required {
		if !present[name] {
			inv.RequiredMissing = append(inv.RequiredMissing, name)
		}
	}
	inv.Total = len(inv.Migrations)
	inv.OK = len(inv.RequiredMissing) == 0
	return inv, nil
}

func defaultMigrationDir() (string, error) {
	candidates := []string{
		"migrations",
		filepath.Join("backend", "migrations"),
		filepath.Join("source", "backend", "migrations"),
		filepath.Join("..", "migrations"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("backend migrations directory not found; run from source/backend or pass through the normal project workspace")
}

func ensureWriteConfirmed(opts commandOptions) error {
	if opts.ConfirmWrite {
		return nil
	}
	return errors.New("refusing to write Model Gateway configuration without --confirm-write; run --provider-probe, --plan, --audit-only and --schema-check first, then rerun with --confirm-write after the target environment and backup window are confirmed")
}

func validateKeyStdinOption(opts commandOptions, selectedSeeds []providerSeed) error {
	if !opts.KeyStdin {
		return nil
	}
	if opts.PlanOnly || opts.AuditOnly || opts.SchemaCheck {
		return errors.New("--key-stdin is only valid with --provider-probe or an authorized --confirm-write run")
	}
	if !opts.ProviderProbe && !opts.ConfirmWrite {
		return errors.New("--key-stdin requires --provider-probe or --confirm-write")
	}
	if len(selectedSeeds) != 1 {
		return fmt.Errorf("--key-stdin requires a single --provider scope; got %d provider seeds for %q", len(selectedSeeds), fallbackString(strings.TrimSpace(opts.ProviderScope), "all"))
	}
	return nil
}

func ensureWriteConfirmedBeforeBootstrap(opts commandOptions) error {
	if opts.AuditOnly || opts.SchemaCheck {
		return nil
	}
	return ensureWriteConfirmed(opts)
}

type dbTargetCheckReport struct {
	OK                     bool     `json:"ok"`
	TargetEnv              string   `json:"target_env"`
	DSNPresent             bool     `json:"dsn_present"`
	Host                   string   `json:"host"`
	Port                   string   `json:"port,omitempty"`
	Address                string   `json:"address"`
	Database               string   `json:"database"`
	User                   string   `json:"user,omitempty"`
	SanitizedDSN           string   `json:"sanitized_dsn"`
	MigrationDryRunAllowed bool     `json:"migration_dry_run_allowed"`
	RiskLevel              string   `json:"risk_level"`
	Reasons                []string `json:"reasons"`
	RequiredNameMarkers    []string `json:"required_name_markers"`
	ForbiddenNameMarkers   []string `json:"forbidden_name_markers"`
}

func printDBTargetCheck(w io.Writer, targetEnv, rawDSN string) error {
	report := checkDBTargetSafety(targetEnv, rawDSN)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("database target is not safe for migration dry-run: %s", strings.Join(report.Reasons, "; "))
	}
	return nil
}

func checkDBTargetSafety(targetEnv, rawDSN string) dbTargetCheckReport {
	env := strings.ToLower(strings.TrimSpace(targetEnv))
	if env == "" {
		env = "staging"
	}
	safeMarkers := []string{"dryrun", "dry_run", "staging", "stage", "clone", "shadow", "sandbox", "test", "tmp", "temp", "disposable"}
	forbiddenMarkers := []string{"prod", "production", "live", "online", "master", "primary"}
	report := dbTargetCheckReport{
		TargetEnv:            env,
		RiskLevel:            "unknown",
		Reasons:              []string{},
		RequiredNameMarkers:  append([]string(nil), safeMarkers...),
		ForbiddenNameMarkers: append([]string(nil), forbiddenMarkers...),
	}
	dsn := strings.TrimSpace(rawDSN)
	if dsn == "" {
		report.Reasons = append(report.Reasons, "KLEIN_DB_DSN is empty")
		report.RiskLevel = "high"
		return report
	}
	report.DSNPresent = true
	parsed, err := parseMySQLDSNSummary(dsn)
	if err != nil {
		report.Reasons = append(report.Reasons, err.Error())
		report.RiskLevel = "high"
		return report
	}
	report.Host = parsed.Host
	report.Port = parsed.Port
	report.Address = parsed.Host
	if parsed.Port != "" {
		report.Address += ":" + parsed.Port
	}
	report.Database = parsed.Database
	report.User = parsed.User
	report.SanitizedDSN = parsed.Sanitized

	blockers := []string{}
	highRisk := false
	if containsMarker(env, []string{"prod", "production", "live", "online"}) {
		blockers = append(blockers, "target-env production/live/online is not allowed for migration dry-run")
		highRisk = true
	}
	if report.Database == "" {
		blockers = append(blockers, "database name is empty")
		highRisk = true
	}
	dbLower := strings.ToLower(report.Database)
	hostLower := strings.ToLower(report.Host)
	for _, marker := range forbiddenMarkers {
		if strings.Contains(dbLower, marker) || strings.Contains(hostLower, marker) {
			blockers = append(blockers, "target contains production-like marker "+marker)
			highRisk = true
		}
	}
	matchedSafeMarker := ""
	for _, marker := range safeMarkers {
		if strings.Contains(dbLower, marker) {
			matchedSafeMarker = marker
			break
		}
	}
	if matchedSafeMarker == "" {
		blockers = append(blockers, "database name must include dryrun, dry_run, staging, stage, clone, shadow, sandbox, test, tmp, temp or disposable for migration dry-run")
	}
	if len(blockers) > 0 {
		report.Reasons = blockers
		if highRisk {
			report.RiskLevel = "high"
		} else {
			report.RiskLevel = "medium"
		}
		return report
	}
	report.OK = true
	report.MigrationDryRunAllowed = true
	report.RiskLevel = "low"
	report.Reasons = append(report.Reasons, "database name contains migration-dry-run marker "+matchedSafeMarker)
	return report
}

func containsMarker(value string, markers []string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

type mysqlDSNSummary struct {
	User      string
	Host      string
	Port      string
	Database  string
	Sanitized string
}

func parseMySQLDSNSummary(raw string) (mysqlDSNSummary, error) {
	dsn := strings.TrimSpace(raw)
	if dsn == "" {
		return mysqlDSNSummary{}, errors.New("mysql dsn empty")
	}
	var out mysqlDSNSummary
	at := strings.LastIndex(dsn, "@")
	rest := dsn
	userPart := ""
	if at >= 0 {
		userPart = dsn[:at]
		rest = dsn[at+1:]
		if colon := strings.Index(userPart, ":"); colon >= 0 {
			out.User = userPart[:colon]
		} else {
			out.User = userPart
		}
	}
	if strings.HasPrefix(rest, "tcp(") {
		closeIdx := strings.Index(rest, ")/")
		if closeIdx < 0 {
			return mysqlDSNSummary{}, errors.New("mysql tcp dsn missing database path")
		}
		hostPort := rest[len("tcp("):closeIdx]
		out.Host, out.Port = splitHostPortLoose(hostPort)
		remainder := rest[closeIdx+2:]
		out.Database = strings.SplitN(remainder, "?", 2)[0]
	} else {
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return mysqlDSNSummary{}, errors.New("mysql dsn missing database path")
		}
		network := rest[:slash]
		remainder := rest[slash+1:]
		out.Host, out.Port = splitHostPortLoose(strings.TrimPrefix(strings.TrimSuffix(network, ")"), "("))
		out.Database = strings.SplitN(remainder, "?", 2)[0]
	}
	out.User = strings.TrimSpace(out.User)
	out.Host = strings.TrimSpace(out.Host)
	out.Port = strings.TrimSpace(out.Port)
	out.Database = strings.TrimSpace(out.Database)
	out.Sanitized = sanitizeMySQLDSNForOutput(dsn, out)
	return out, nil
}

func splitHostPortLoose(hostPort string) (string, string) {
	value := strings.TrimSpace(hostPort)
	if value == "" {
		return "", ""
	}
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		return host, port
	}
	if idx := strings.LastIndex(value, ":"); idx > 0 && idx < len(value)-1 && !strings.Contains(value[idx+1:], ":") {
		return value[:idx], value[idx+1:]
	}
	return value, ""
}

func sanitizeMySQLDSNForOutput(raw string, summary mysqlDSNSummary) string {
	suffix := ""
	if strings.Contains(raw, "?") {
		suffix = "?[params]"
	}
	db := fallbackString(summary.Database, "[database]")
	host := fallbackString(summary.Host, "[host]")
	if summary.Port != "" {
		host += ":" + summary.Port
	}
	user := fallbackString(summary.User, "[user]")
	return fmt.Sprintf("%s:[redacted]@tcp(%s)/%s%s", user, host, db, suffix)
}

func runReadOnlyDBCommand(ctx context.Context, opts commandOptions) error {
	db, cleanup, err := initReadOnlyDB(serviceName)
	if err != nil {
		return err
	}
	defer cleanup()

	if opts.SchemaCheck {
		return printSchemaCheck(os.Stdout, db.Migrator())
	}
	apiRepo := repo.NewAPIChannelRepo(db)
	modelRepo := repo.NewModelCatalogRepo(db)
	sourceRepo := repo.NewModelSourceRepo(db)
	scope, err := auditScopeForProvider(opts.ProviderScope)
	if err != nil {
		return err
	}
	return printConfigurationAudit(ctx, os.Stdout, apiRepo, modelRepo, sourceRepo, scope, true)
}

func initReadOnlyDB(serviceName string) (*gorm.DB, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	if err := logger.Init(cfg); err != nil {
		return nil, nil, fmt.Errorf("init logger: %w", err)
	}
	logger.L().Info("kleinai starting read-only modelgw-config",
		zap.String("service", serviceName),
		zap.String("env", cfg.App.Env),
		zap.String("version", version.Info()),
	)
	db, err := database.NewMySQL(&cfg.MySQL)
	if err != nil {
		logger.Sync()
		return nil, nil, fmt.Errorf("init read-only mysql: %w", err)
	}
	cleanup := func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		logger.Sync()
	}
	return db, cleanup, nil
}

func defaultProviderSeeds() []providerSeed {
	modelCode := envOrDefault("DAPO_MIMO_PUBLIC_MODEL", envOrDefault("DAPO_MIMO_MODEL", "mimo-v2.5-pro"))
	upstreamModel := envOrDefault("DAPO_MIMO_UPSTREAM_MODEL", envOrDefault("DAPO_MIMO_MODEL", modelCode))
	deepSeekModel := envOrDefault("DAPO_DEEPSEEK_PUBLIC_MODEL", envOrDefault("DAPO_DEEPSEEK_MODEL", "deepseek-chat"))
	deepSeekUpstream := envOrDefault("DAPO_DEEPSEEK_UPSTREAM_MODEL", envOrDefault("DAPO_DEEPSEEK_MODEL", deepSeekModel))
	mimoPricing := defaultOfficialTextPricing("DAPO_MIMO")
	deepSeekPricing := defaultOfficialTextPricing("DAPO_DEEPSEEK")
	return []providerSeed{
		{
			Label:            "MiMo",
			APIKeyEnv:        "DAPO_MIMO_API_KEY",
			ChannelCode:      envOrDefault("DAPO_MIMO_CHANNEL_CODE", "mimo-official"),
			ChannelName:      envOrDefault("DAPO_MIMO_CHANNEL_NAME", "MiMo 官方 API"),
			ProviderName:     "mimo",
			BaseURL:          envOrDefault("DAPO_MIMO_BASE_URL", "https://token-plan-cn.xiaomimimo.com/v1"),
			KeyName:          envOrDefault("DAPO_MIMO_KEY_NAME", "primary"),
			PublicModel:      modelCode,
			UpstreamModel:    upstreamModel,
			DisplayName:      envOrDefault("DAPO_MIMO_DISPLAY_NAME", "MiMo v2.5 Pro"),
			EntryKind:        model.ModelCatalogKindText,
			Capabilities:     []string{"chat"},
			ParametersSchema: defaultOfficialTextParametersSchema(),
			Tags:             []string{"official_api", "mimo"},
			PricingMode:      mimoPricing.Mode,
			UnitPoints:       mimoPricing.UnitPoints,
			InputUnitPoints:  mimoPricing.InputUnitPoints,
			OutputUnitPoints: mimoPricing.OutputUnitPoints,
			PricingExplicit:  mimoPricing.Explicit,
			Priority:         20,
			Remark:           "Created by local modelgw-config; API key is encrypted at rest.",
		},
		{
			Label:            "DeepSeek",
			APIKeyEnv:        "DAPO_DEEPSEEK_API_KEY",
			ChannelCode:      envOrDefault("DAPO_DEEPSEEK_CHANNEL_CODE", "deepseek-official"),
			ChannelName:      envOrDefault("DAPO_DEEPSEEK_CHANNEL_NAME", "DeepSeek 官方 API"),
			ProviderName:     "deepseek",
			BaseURL:          envOrDefault("DAPO_DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
			KeyName:          envOrDefault("DAPO_DEEPSEEK_KEY_NAME", "primary"),
			PublicModel:      deepSeekModel,
			UpstreamModel:    deepSeekUpstream,
			DisplayName:      envOrDefault("DAPO_DEEPSEEK_DISPLAY_NAME", "DeepSeek Chat"),
			EntryKind:        model.ModelCatalogKindText,
			Capabilities:     []string{"chat"},
			ParametersSchema: defaultOfficialTextParametersSchema(),
			Tags:             []string{"official_api", "deepseek"},
			PricingMode:      deepSeekPricing.Mode,
			UnitPoints:       deepSeekPricing.UnitPoints,
			InputUnitPoints:  deepSeekPricing.InputUnitPoints,
			OutputUnitPoints: deepSeekPricing.OutputUnitPoints,
			PricingExplicit:  deepSeekPricing.Explicit,
			Priority:         30,
			Remark:           "Created by local modelgw-config; API key is encrypted at rest.",
		},
	}
}

func enabledProviderSeeds() []providerSeed {
	defaults := defaultProviderSeeds()
	seeds := make([]providerSeed, 0, len(defaults))
	for _, seed := range defaults {
		if strings.TrimSpace(os.Getenv(seed.APIKeyEnv)) != "" {
			seeds = append(seeds, seed)
		}
	}
	return seeds
}

func selectedProviderSeeds(scope string) ([]providerSeed, error) {
	return filterProviderSeeds(defaultProviderSeeds(), scope)
}

func enabledProviderSeedsForScope(scope string) ([]providerSeed, error) {
	selected, err := selectedProviderSeeds(scope)
	if err != nil {
		return nil, err
	}
	seeds := make([]providerSeed, 0, len(selected))
	for _, seed := range selected {
		if providerAPIKey(seed) != "" {
			seeds = append(seeds, seed)
		}
	}
	return seeds, nil
}

func enabledProviderSeedsForScopeWithInput(scope string, keyStdin bool, stdin io.Reader) ([]providerSeed, error) {
	selected, err := selectedProviderSeeds(scope)
	if err != nil {
		return nil, err
	}
	if !keyStdin {
		return enabledProviderSeedsForScope(scope)
	}
	if len(selected) != 1 {
		return nil, fmt.Errorf("--key-stdin requires a single --provider scope; got %d provider seeds for %q", len(selected), fallbackString(strings.TrimSpace(scope), "all"))
	}
	seed := selected[0]
	key, err := readProviderKeyFromStdin(stdin)
	if err != nil {
		return nil, fmt.Errorf("read provider key from stdin for --provider %s: %w", fallbackString(strings.TrimSpace(scope), seed.ProviderName), err)
	}
	seed.APIKeyValue = key
	seed.APIKeySource = "stdin"
	return []providerSeed{seed}, nil
}

func readProviderKeyFromStdin(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("stdin unavailable")
	}
	const maxKeyBytes = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(r, maxKeyBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxKeyBytes {
		return "", fmt.Errorf("provider key is too large")
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", errors.New("empty key")
	}
	if strings.ContainsAny(key, "\r\n") {
		return "", errors.New("expected a single-line key")
	}
	return key, nil
}

func providerAPIKey(seed providerSeed) string {
	if value := strings.TrimSpace(seed.APIKeyValue); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(seed.APIKeyEnv))
}

func providerAPIKeySource(seed providerSeed) string {
	if source := strings.TrimSpace(seed.APIKeySource); source != "" {
		return source
	}
	if providerAPIKey(seed) != "" {
		return "env"
	}
	return ""
}

func filterProviderSeeds(seeds []providerSeed, scope string) ([]providerSeed, error) {
	normalized := strings.ToLower(strings.TrimSpace(scope))
	if normalized == "" || normalized == "all" || normalized == "*" {
		return append([]providerSeed(nil), seeds...), nil
	}
	out := make([]providerSeed, 0, 1)
	for _, seed := range seeds {
		if providerSeedMatchesScope(seed, normalized) {
			out = append(out, seed)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("unknown --provider %q; use all, mimo, deepseek, or a configured channel code", scope)
	}
	return out, nil
}

func providerSeedMatchesScope(seed providerSeed, normalizedScope string) bool {
	for _, candidate := range []string{seed.ProviderName, seed.Label, seed.ChannelCode} {
		if strings.ToLower(strings.TrimSpace(candidate)) == normalizedScope {
			return true
		}
	}
	return false
}

func noProviderKeyError(scope string, seeds []providerSeed) error {
	var envs []string
	for _, seed := range seeds {
		if seed.APIKeyEnv != "" {
			envs = append(envs, seed.APIKeyEnv)
		}
	}
	if len(envs) == 0 {
		return fmt.Errorf("no official API key env found for --provider %s", strings.TrimSpace(scope))
	}
	if strings.EqualFold(strings.TrimSpace(scope), "all") || strings.TrimSpace(scope) == "" {
		return fmt.Errorf("no official API key env found: set %s", strings.Join(envs, " or "))
	}
	return fmt.Errorf("no official API key env found for --provider %s: set %s", strings.TrimSpace(scope), strings.Join(envs, " or "))
}

func auditScopeForProvider(providerScope string) (configurationAuditScope, error) {
	seeds, err := selectedProviderSeeds(providerScope)
	if err != nil {
		return configurationAuditScope{}, err
	}
	return auditScopeForSeeds(providerScope, seeds)
}

func auditScopeForSeeds(providerScope string, seeds []providerSeed) (configurationAuditScope, error) {
	normalized := strings.ToLower(strings.TrimSpace(providerScope))
	global := normalized == "" || normalized == "all" || normalized == "*"
	scope := configurationAuditScope{
		ProviderScope:   fallbackString(strings.TrimSpace(providerScope), "all"),
		Global:          global,
		ModelCodes:      map[string]bool{},
		APIChannelCodes: map[string]bool{},
		ProviderNames:   map[string]bool{},
	}
	if global {
		return scope, nil
	}
	for _, seed := range seeds {
		addAuditScopeValue(scope.ModelCodes, seed.PublicModel)
		addAuditScopeValue(scope.ModelCodes, seed.UpstreamModel)
		addAuditScopeValue(scope.ModelCodes, effectiveUpstreamModel(seed))
		addAuditScopeValue(scope.APIChannelCodes, seed.ChannelCode)
		addAuditScopeValue(scope.ProviderNames, seed.ProviderName)
		addAuditScopeValue(scope.ProviderNames, seed.Label)
	}
	if len(scope.ModelCodes) == 0 && len(scope.APIChannelCodes) == 0 && len(scope.ProviderNames) == 0 {
		return configurationAuditScope{}, fmt.Errorf("empty audit scope for --provider %s", providerScope)
	}
	return scope, nil
}

func addAuditScopeValue(dst map[string]bool, value string) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized != "" {
		dst[normalized] = true
	}
}

func auditScopeSortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type planEntry struct {
	Label            string   `json:"label"`
	APIKeyEnv        string   `json:"api_key_env"`
	APIKeyPresent    bool     `json:"api_key_present"`
	ChannelCode      string   `json:"channel_code"`
	ChannelName      string   `json:"channel_name"`
	ProviderName     string   `json:"provider_name"`
	BaseURL          string   `json:"base_url"`
	KeyName          string   `json:"key_name"`
	PublicModel      string   `json:"public_model"`
	UpstreamModel    string   `json:"upstream_model"`
	EntryKind        string   `json:"entry_kind"`
	Adapter          string   `json:"adapter"`
	SourceType       string   `json:"source_type"`
	AuthType         string   `json:"auth_type"`
	Capabilities     []string `json:"capabilities"`
	ParametersSchema any      `json:"parameters_schema,omitempty"`
	PricingMode      string   `json:"pricing_mode"`
	UnitPoints       int64    `json:"unit_points"`
	InputUnitPoints  int64    `json:"input_unit_points"`
	OutputUnitPoints int64    `json:"output_unit_points"`
	Priority         int      `json:"priority"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
}

func printPlan(w io.Writer, seeds []providerSeed) error {
	out := make([]planEntry, 0, len(seeds))
	for _, seed := range seeds {
		timeout := seed.TimeoutSeconds
		if timeout == 0 {
			timeout = 300
		}
		out = append(out, planEntry{
			Label:            seed.Label,
			APIKeyEnv:        seed.APIKeyEnv,
			APIKeyPresent:    providerAPIKey(seed) != "",
			ChannelCode:      seed.ChannelCode,
			ChannelName:      seed.ChannelName,
			ProviderName:     seed.ProviderName,
			BaseURL:          seed.BaseURL,
			KeyName:          seed.KeyName,
			PublicModel:      seed.PublicModel,
			UpstreamModel:    effectiveUpstreamModel(seed),
			EntryKind:        seed.EntryKind,
			Adapter:          model.APIChannelAdapterOpenAIChat,
			SourceType:       model.ModelSourceTypeAPIChannel,
			AuthType:         model.AuthTypeAPIKey,
			Capabilities:     seed.Capabilities,
			ParametersSchema: seed.ParametersSchema,
			PricingMode:      seed.PricingMode,
			UnitPoints:       seed.UnitPoints,
			InputUnitPoints:  seed.InputUnitPoints,
			OutputUnitPoints: seed.OutputUnitPoints,
			Priority:         seed.Priority,
			TimeoutSeconds:   timeout,
		})
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(raw))
	return err
}

type conflictEntry struct {
	ID            uint64 `json:"id"`
	ModelCode     string `json:"model_code"`
	SourceCode    string `json:"source_code"`
	UpstreamModel string `json:"upstream_model"`
	Status        int8   `json:"status"`
	Reason        string `json:"reason"`
}

type legacyCredentialEntry struct {
	ID              uint64 `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	ProviderName    string `json:"provider_name"`
	Adapter         string `json:"adapter"`
	Status          int8   `json:"status"`
	KeyCount        int64  `json:"key_count"`
	EnabledKeyCount int64  `json:"enabled_key_count"`
	Reason          string `json:"reason"`
}

type sourceDuplicateEntry struct {
	FirstID       uint64 `json:"first_id"`
	DuplicateID   uint64 `json:"duplicate_id"`
	ModelCode     string `json:"model_code"`
	SourceType    string `json:"source_type"`
	SourceCode    string `json:"source_code"`
	UpstreamModel string `json:"upstream_model"`
	Adapter       string `json:"adapter,omitempty"`
	AuthType      string `json:"auth_type,omitempty"`
	ImageAPIMode  string `json:"image_api_mode,omitempty"`
	Reason        string `json:"reason"`
}

type configurationAuditReport struct {
	ProviderScope               string                  `json:"provider_scope"`
	Scoped                      bool                    `json:"scoped"`
	ModelCodes                  []string                `json:"model_codes,omitempty"`
	APIChannelCodes             []string                `json:"api_channel_codes,omitempty"`
	AccountPoolConflicts        []conflictEntry         `json:"account_pool_conflicts"`
	APIChannelLegacyCredentials []legacyCredentialEntry `json:"api_channel_legacy_credentials"`
	SourceDuplicates            []sourceDuplicateEntry  `json:"source_duplicates"`
}

type configurationAuditScope struct {
	ProviderScope   string
	Global          bool
	ModelCodes      map[string]bool
	APIChannelCodes map[string]bool
	ProviderNames   map[string]bool
}

type schemaInspector interface {
	HasTable(dst any) bool
	HasColumn(dst any, field string) bool
}

type schemaTableRequirement struct {
	Table   string
	Columns []string
}

type schemaTableCheck struct {
	Table          string   `json:"table"`
	OK             bool     `json:"ok"`
	MissingColumns []string `json:"missing_columns,omitempty"`
}

type schemaCheckReport struct {
	OK        bool               `json:"ok"`
	CheckedAt int64              `json:"checked_at"`
	Tables    []schemaTableCheck `json:"tables"`
}

func printSchemaCheck(w io.Writer, inspector schemaInspector) error {
	report := checkRequiredSchema(inspector, requiredSchemaTables())
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	if !report.OK {
		return errors.New("model gateway schema check failed")
	}
	return nil
}

func checkRequiredSchema(inspector schemaInspector, requirements []schemaTableRequirement) schemaCheckReport {
	report := schemaCheckReport{OK: true, CheckedAt: time.Now().Unix(), Tables: make([]schemaTableCheck, 0, len(requirements))}
	for _, req := range requirements {
		check := schemaTableCheck{Table: req.Table, OK: true}
		if !inspector.HasTable(req.Table) {
			check.OK = false
			check.MissingColumns = append(check.MissingColumns, "*")
		} else {
			for _, column := range req.Columns {
				if !inspector.HasColumn(req.Table, column) {
					check.OK = false
					check.MissingColumns = append(check.MissingColumns, column)
				}
			}
		}
		if !check.OK {
			report.OK = false
		}
		report.Tables = append(report.Tables, check)
	}
	return report
}

func requiredSchemaTables() []schemaTableRequirement {
	return []schemaTableRequirement{
		{
			Table: "api_channel",
			Columns: []string{
				"code", "name", "provider_name", "adapter", "base_url", "credential_enc",
				"models", "capabilities", "proxy_id", "priority", "weight", "rpm_limit", "tpm_limit",
				"timeout_seconds", "status", "last_test_at", "last_test_status", "last_test_error",
				"remark", "created_by", "created_at", "updated_at", "deleted_at",
			},
		},
		{
			Table: "api_channel_key",
			Columns: []string{
				"channel_id", "name", "credential_enc", "priority", "weight", "rpm_limit",
				"tpm_limit", "status", "last_used_at", "last_error", "created_at", "updated_at", "deleted_at",
			},
		},
		{
			Table: "model_catalog",
			Columns: []string{
				"model_code", "display_name", "entry_kind", "provider_hint", "upstream_default_model",
				"capabilities", "parameters_schema", "pricing_mode", "unit_points",
				"input_unit_points", "output_unit_points", "price_rules", "min_plan", "tags",
				"description", "sort_order", "visible", "status", "created_by", "created_at",
				"updated_at", "deleted_at",
			},
		},
		{
			Table: "model_source_mapping",
			Columns: []string{
				"model_code", "source_type", "source_code", "upstream_model", "adapter",
				"auth_type", "image_api_mode", "strategy", "priority", "weight", "status",
				"remark", "created_at", "updated_at", "deleted_at",
			},
		},
		{
			Table: "generation_task",
			Columns: []string{
				"task_id", "user_id", "kind", "mode", "model_code", "params", "cost_points", "status",
			},
		},
		{
			Table: "generation_upstream_log",
			Columns: []string{
				"task_id", "provider", "stage", "status_code", "duration_ms", "request_excerpt",
				"response_excerpt", "error", "meta",
			},
		},
		{
			Table: "consume_record",
			Columns: []string{
				"task_id", "user_id", "kind", "model_code", "count", "unit_points",
				"total_points", "status", "account_id", "created_at", "updated_at",
			},
		},
		{
			Table: "wallet_log",
			Columns: []string{
				"user_id", "direction", "biz_type", "biz_id", "points", "points_before",
				"points_after", "remark", "created_at",
			},
		},
		{
			Table:   "refund_record",
			Columns: []string{"task_id", "user_id", "points", "reason", "operator", "created_at"},
		},
	}
}

func printConfigurationAudit(ctx context.Context, w io.Writer, apiRepo *repo.APIChannelRepo, modelRepo *repo.ModelCatalogRepo, sourceRepo *repo.ModelSourceRepo, scope configurationAuditScope, failOnFindings bool) error {
	conflicts, err := auditConflictingAccountPoolSources(ctx, modelRepo, sourceRepo, scope)
	if err != nil {
		return err
	}
	legacyCredentials, err := auditLegacyAPIChannelCredentials(ctx, apiRepo, scope)
	if err != nil {
		return err
	}
	sourceDuplicates, err := auditDuplicateModelSources(ctx, modelRepo, sourceRepo, scope)
	if err != nil {
		return err
	}
	report := configurationAuditReport{
		ProviderScope:               scope.ProviderScope,
		Scoped:                      !scope.Global,
		ModelCodes:                  auditScopeSortedKeys(scope.ModelCodes),
		APIChannelCodes:             auditScopeSortedKeys(scope.APIChannelCodes),
		AccountPoolConflicts:        conflicts,
		APIChannelLegacyCredentials: legacyCredentials,
		SourceDuplicates:            sourceDuplicates,
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	if failOnFindings && configurationAuditHasFindings(report) {
		return fmt.Errorf("configuration audit found blocking findings: %s", configurationAuditFindingSummary(report))
	}
	return nil
}

func configurationAuditHasFindings(report configurationAuditReport) bool {
	return len(report.AccountPoolConflicts) > 0 || len(report.APIChannelLegacyCredentials) > 0 || len(report.SourceDuplicates) > 0
}

func configurationAuditFindingSummary(report configurationAuditReport) string {
	return fmt.Sprintf("account_pool_conflicts=%d api_channel_legacy_credentials=%d source_duplicates=%d",
		len(report.AccountPoolConflicts),
		len(report.APIChannelLegacyCredentials),
		len(report.SourceDuplicates),
	)
}

func auditLegacyAPIChannelCredentials(ctx context.Context, apiRepo *repo.APIChannelRepo, scope configurationAuditScope) ([]legacyCredentialEntry, error) {
	entries := []legacyCredentialEntry{}
	for page := 1; ; page++ {
		items, total, err := apiRepo.List(ctx, repo.APIChannelListFilter{Page: page, PageSize: 200})
		if err != nil {
			return nil, fmt.Errorf("list api channels: %w", err)
		}
		for _, ch := range items {
			if ch == nil || len(ch.CredentialEnc) == 0 {
				continue
			}
			if !auditScopeMatchesAPIChannel(scope, ch) {
				continue
			}
			totalKeys, enabledKeys, err := apiChannelKeyCounts(ctx, apiRepo, ch.ID)
			if err != nil {
				return nil, err
			}
			entries = append(entries, legacyAPIChannelCredentialEntry(ch, totalKeys, enabledKeys))
		}
		if int64(page*200) >= total || len(items) == 0 {
			break
		}
	}
	return entries, nil
}

func apiChannelKeyCounts(ctx context.Context, apiRepo *repo.APIChannelRepo, channelID uint64) (int64, int64, error) {
	total, err := apiRepo.CountKeys(ctx, channelID, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("count api channel keys: %w", err)
	}
	enabled := int8(model.APIChannelKeyStatusEnabled)
	enabledCount, err := apiRepo.CountKeys(ctx, channelID, &enabled)
	if err != nil {
		return 0, 0, fmt.Errorf("count enabled api channel keys: %w", err)
	}
	return total, enabledCount, nil
}

func legacyAPIChannelCredentialEntry(ch *model.APIChannel, keyCount, enabledKeyCount int64) legacyCredentialEntry {
	if ch == nil {
		return legacyCredentialEntry{}
	}
	return legacyCredentialEntry{
		ID:              ch.ID,
		Code:            ch.Code,
		Name:            ch.Name,
		ProviderName:    ch.ProviderName,
		Adapter:         ch.Adapter,
		Status:          ch.Status,
		KeyCount:        keyCount,
		EnabledKeyCount: enabledKeyCount,
		Reason:          "API Channel still has legacy channel-level credential; clear it after Key Pool is verified",
	}
}

func auditScopeMatchesAPIChannel(scope configurationAuditScope, ch *model.APIChannel) bool {
	if scope.Global || ch == nil {
		return true
	}
	if scope.APIChannelCodes[auditSignatureToken(ch.Code)] {
		return true
	}
	if scope.ProviderNames[auditSignatureToken(ch.ProviderName)] {
		return true
	}
	return false
}

func printPostConfigureNextSteps(w io.Writer, seeds []providerSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "recommended pre-launch evidence template:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  replace '<target_env>' and '<task_id>' before attaching the deployment evidence pack"); err != nil {
		return err
	}
	for _, seed := range seeds {
		if _, err := fmt.Fprintf(w, "  %s\n", preLaunchEvidenceTemplateCommand(seed)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "recommended pre-generation smoke:"); err != nil {
		return err
	}
	for _, seed := range seeds {
		if _, err := fmt.Fprintf(w, "  %s\n", preGenerationSmokeCommand(seed)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "recommended post-generation smoke:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  after a controlled generation succeeds, replace '<task_id>' with the exact generated task id"); err != nil {
		return err
	}
	for _, seed := range seeds {
		if _, err := fmt.Fprintf(w, "  %s\n", postGenerationProofCommand(seed)); err != nil {
			return err
		}
	}
	return nil
}

type providerProbeResult struct {
	Label            string `json:"label"`
	ChannelCode      string `json:"channel_code"`
	PublicModel      string `json:"public_model"`
	UpstreamModel    string `json:"upstream_model"`
	BaseURL          string `json:"base_url"`
	APIKeyEnv        string `json:"api_key_env"`
	APIKeySource     string `json:"api_key_source,omitempty"`
	APIKeyPresent    bool   `json:"api_key_present"`
	ModelsStatus     int    `json:"models_status,omitempty"`
	ModelsAttempts   int    `json:"models_attempts,omitempty"`
	ModelVisibility  string `json:"model_visibility,omitempty"`
	ProtocolStatus   int    `json:"protocol_status,omitempty"`
	ProtocolAttempts int    `json:"protocol_attempts,omitempty"`
	OK               bool   `json:"ok"`
	Proof            string `json:"proof,omitempty"`
	ErrorSummary     string `json:"error_summary,omitempty"`
	LatencyMs        int64  `json:"latency_ms"`
}

func runProviderProbe(ctx context.Context, w io.Writer, seeds []providerSeed, client *http.Client) error {
	if len(seeds) == 0 {
		return errors.New("no official API key env found: set DAPO_MIMO_API_KEY or DAPO_DEEPSEEK_API_KEY")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	results := make([]providerProbeResult, 0, len(seeds))
	for _, seed := range seeds {
		results = append(results, probeProviderSeed(ctx, client, seed))
	}
	raw, err := json.MarshalIndent(map[string]any{"provider_probes": results}, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, string(raw)); err != nil {
		return err
	}
	for _, result := range results {
		if !result.OK {
			return fmt.Errorf("provider probe failed for %s (%s): %s", result.Label, result.ChannelCode, fallbackString(result.ErrorSummary, "not ok"))
		}
	}
	return nil
}

func probeProviderSeed(ctx context.Context, client *http.Client, seed providerSeed) providerProbeResult {
	started := time.Now()
	result := providerProbeResult{
		Label:         seed.Label,
		ChannelCode:   seed.ChannelCode,
		PublicModel:   seed.PublicModel,
		UpstreamModel: effectiveUpstreamModel(seed),
		BaseURL:       sanitizeProbeBaseURLForOutput(seed.BaseURL),
		APIKeyEnv:     seed.APIKeyEnv,
		APIKeySource:  providerAPIKeySource(seed),
	}
	key := providerAPIKey(seed)
	result.APIKeyPresent = key != ""
	defer func() {
		result.LatencyMs = time.Since(started).Milliseconds()
	}()
	if key == "" {
		result.ErrorSummary = seed.APIKeyEnv + " is empty"
		return result
	}
	baseURL, err := normalizeProviderProbeBaseURL(seed.BaseURL)
	if err != nil {
		result.ErrorSummary = err.Error()
		return result
	}
	result.BaseURL = baseURL
	modelsStatus, modelsBody, modelsAttempts, err := providerProbeHTTPWithRetry(ctx, client, http.MethodGet, openAIModelsEndpoint(result.BaseURL), key, nil)
	result.ModelsStatus = modelsStatus
	result.ModelsAttempts = modelsAttempts
	if err != nil {
		result.ErrorSummary = probeAttemptErrorSummary("models probe", modelsAttempts, err, key)
		return result
	}
	if modelsStatus/100 == 2 {
		ids := parseProviderProbeModelIDs(modelsBody)
		if len(ids) > 0 {
			if matched, visibility := providerProbeModelListed(ids, result.UpstreamModel, result.PublicModel); matched {
				result.OK = true
				result.Proof = "models_endpoint_2xx_model_listed"
				result.ModelVisibility = visibility
				return result
			}
			result.ModelVisibility = "target_model_not_listed"
			result.ErrorSummary = fmt.Sprintf("target model %s not listed by /models", fallbackString(result.UpstreamModel, result.PublicModel))
			return result
		}
		result.ModelVisibility = "models_endpoint_no_parseable_model_ids"
		probeProviderChatProtocol(ctx, client, &result, key, "models_endpoint_no_parseable_model_ids_protocol_probe")
		return result
	}
	if shouldProbeProtocolAfterModels(modelsStatus) {
		probeProviderChatProtocol(ctx, client, &result, key, "protocol_probe_only")
		return result
	}
	result.ErrorSummary = fmt.Sprintf("models HTTP %d: %s", modelsStatus, redactProbeTextWithSecrets(string(modelsBody), key))
	return result
}

func probeProviderChatProtocol(ctx context.Context, client *http.Client, result *providerProbeResult, key string, visibility string) {
	payload := providerProbeChatValidationPayload(result.UpstreamModel, result.PublicModel)
	protocolStatus, protocolBody, protocolAttempts, err := providerProbeHTTPWithRetry(ctx, client, http.MethodPost, openAIChatEndpoint(result.BaseURL), key, payload)
	result.ProtocolStatus = protocolStatus
	result.ProtocolAttempts = protocolAttempts
	if err != nil {
		result.ErrorSummary = probeAttemptErrorSummary("protocol probe", protocolAttempts, err, key)
		return
	}
	if protocolStatus/100 == 2 {
		result.OK = true
		result.Proof = "chat_protocol_2xx"
		result.ModelVisibility = visibility
		return
	}
	summary := redactProbeTextWithSecrets(string(protocolBody), key)
	if protocolStatus == http.StatusBadRequest || protocolStatus == http.StatusUnprocessableEntity {
		if providerProbeLooksAuthFailure(summary) {
			result.ErrorSummary = fmt.Sprintf("protocol HTTP %d: %s", protocolStatus, summary)
			return
		}
		if providerProbeLooksModelFailure(summary) {
			result.ErrorSummary = fmt.Sprintf("target model rejected by protocol validation: %s", summary)
			return
		}
		result.OK = true
		result.Proof = fmt.Sprintf("chat_protocol_validation_%d", protocolStatus)
		result.ModelVisibility = visibility
		result.ErrorSummary = summary
		return
	}
	result.ErrorSummary = fmt.Sprintf("protocol HTTP %d: %s", protocolStatus, summary)
}

func providerProbeChatValidationPayload(upstreamModel, publicModel string) []byte {
	modelCode := fallbackString(upstreamModel, publicModel)
	payload := map[string]any{
		"model":    modelCode,
		"messages": []any{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func normalizeProviderProbeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errors.New("invalid base_url: empty")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return "", errors.New("invalid base_url")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errors.New("invalid base_url: must use https, except loopback http for local smoke")
	}
	if parsed.Host == "" {
		return "", errors.New("invalid base_url: missing host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid base_url: must not include userinfo, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func sanitizeProbeBaseURLForOutput(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return redactProbeText(trimmed)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/")
}

func isLoopbackHost(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "localhost") {
		return true
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.IsLoopback()
}

func providerProbeHTTP(ctx context.Context, client *http.Client, method, endpoint, apiKey string, payload []byte) (int, []byte, error) {
	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<15))
	return resp.StatusCode, raw, nil
}

const providerProbeMaxAttempts = 3

var providerProbeSleep = func(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func providerProbeHTTPWithRetry(ctx context.Context, client *http.Client, method, endpoint, apiKey string, payload []byte) (int, []byte, int, error) {
	var lastStatus int
	var lastBody []byte
	var lastErr error
	for attempt := 1; attempt <= providerProbeMaxAttempts; attempt++ {
		status, body, err := providerProbeHTTP(ctx, client, method, endpoint, apiKey, payload)
		lastStatus, lastBody, lastErr = status, body, err
		if !providerProbeRetryable(status, err) || attempt == providerProbeMaxAttempts {
			return lastStatus, lastBody, attempt, lastErr
		}
		if err := providerProbeSleep(ctx, providerProbeRetryDelay(attempt)); err != nil {
			return lastStatus, lastBody, attempt, err
		}
	}
	return lastStatus, lastBody, providerProbeMaxAttempts, lastErr
}

func providerProbeRetryable(status int, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled)
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return status >= 520 && status <= 524
	}
}

func providerProbeRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(attempt) * 300 * time.Millisecond
}

func probeAttemptErrorSummary(prefix string, attempts int, err error, key string) string {
	summary := redactProbeTextWithSecrets(err.Error(), key)
	if attempts > 1 {
		return fmt.Sprintf("%s failed after %d attempts: %s", prefix, attempts, summary)
	}
	return summary
}

func parseProviderProbeModelIDs(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				addProviderProbeModelID(&out, seen, id)
			}
			if modelCode, ok := v["model"].(string); ok {
				addProviderProbeModelID(&out, seen, modelCode)
			}
			if data, ok := v["data"]; ok {
				walk(data)
			}
			if models, ok := v["models"]; ok {
				walk(models)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case []string:
			for _, item := range v {
				addProviderProbeModelID(&out, seen, item)
			}
		case string:
			addProviderProbeModelID(&out, seen, v)
		}
	}
	walk(payload)
	return out
}

func addProviderProbeModelID(out *[]string, seen map[string]bool, value string) {
	item := strings.TrimSpace(value)
	key := strings.ToLower(item)
	if item == "" || seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, item)
}

func providerProbeModelListed(ids []string, upstreamModel, publicModel string) (bool, string) {
	targets := []struct {
		value      string
		visibility string
	}{
		{upstreamModel, "matched_upstream_model"},
		{publicModel, "matched_public_model"},
	}
	for _, target := range targets {
		want := strings.ToLower(strings.TrimSpace(target.value))
		if want == "" {
			continue
		}
		for _, id := range ids {
			if strings.ToLower(strings.TrimSpace(id)) == want {
				return true, target.visibility
			}
		}
	}
	return false, ""
}

func openAIModelsEndpoint(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/models"
	}
	return base + "/v1/models"
}

func openAIChatEndpoint(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func shouldProbeProtocolAfterModels(status int) bool {
	return status == http.StatusBadRequest ||
		status == http.StatusNotFound ||
		status == http.StatusMethodNotAllowed ||
		status == http.StatusUnprocessableEntity
}

func providerProbeLooksAuthFailure(summary string) bool {
	text := strings.ToLower(summary)
	for _, marker := range []string{"unauthorized", "invalid api key", "invalid_api_key", "authentication", "auth", "forbidden", "permission denied", "access denied"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func providerProbeLooksModelFailure(summary string) bool {
	text := strings.ToLower(summary)
	for _, marker := range []string{
		"model not found",
		"model_not_found",
		"unknown model",
		"invalid model",
		"invalid_model",
		"model does not exist",
		"model is not available",
		"model unavailable",
		"unsupported model",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func redactProbeText(value string) string {
	return redactProbeTextWithSecrets(value)
}

func redactProbeTextWithSecrets(value string, secrets ...string) string {
	text := strings.TrimSpace(value)
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) >= 4 {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	text = probeBearerPattern.ReplaceAllString(text, "${1}[redacted]")
	text = probeSecretPattern.ReplaceAllString(text, "$1$2[redacted]")
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}

var probeSecretPattern = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|credential|token)(["'\s:=\-]+)([^"',}\]\s&]+)`)
var probeBearerPattern = regexp.MustCompile(`(?i)\b((?:authorization["'\s:=\-]+)?bearer\s+)([^"',}\]\s&]+)`)

func preGenerationSmokeCommand(seed providerSeed) string {
	parts := []string{
		"go run ./cmd/modelgw-smoke",
		"--model " + shellQuote(seed.PublicModel),
		"--entry-kind " + shellQuote(seed.EntryKind),
		"--api-channel " + shellQuote(seed.ChannelCode),
		"--require-openai-auth",
		"--require-admin",
		"--require-model",
		"--require-catalog-model",
		"--require-parameter-schema",
		"--require-pricing-mode",
	}
	if pricingMode := strings.TrimSpace(seed.PricingMode); pricingMode != "" {
		parts = append(parts, "--pricing-mode "+shellQuote(pricingMode))
	}
	parts = append(parts,
		"--require-api-channel",
		"--require-api-channel-health",
		"--require-key-pool",
		"--forbid-legacy-channel-key",
		"--require-source-mapping",
		"--require-no-source-conflicts",
		"--require-route-channel",
		"--forbid-account-pool-route",
	)
	return strings.Join(parts, " ")
}

func postGenerationProofCommand(seed providerSeed) string {
	parts := []string{
		"go run ./cmd/modelgw-smoke",
		"--model " + shellQuote(seed.PublicModel),
		"--entry-kind " + shellQuote(seed.EntryKind),
		"--api-channel " + shellQuote(seed.ChannelCode),
		"--task-id " + shellQuote("<task_id>"),
		"--require-openai-auth",
		"--require-post-generation-proof",
	}
	if pricingMode := strings.TrimSpace(seed.PricingMode); pricingMode != "" {
		parts = append(parts, "--pricing-mode "+shellQuote(pricingMode))
	}
	return strings.Join(parts, " ")
}

func preLaunchEvidenceTemplateCommand(seed providerSeed) string {
	parts := []string{
		"go run ./cmd/modelgw-smoke",
		"--evidence-template",
		"--target-env " + shellQuote("<target_env>"),
		"--model " + shellQuote(seed.PublicModel),
		"--entry-kind " + shellQuote(seed.EntryKind),
		"--api-channel " + shellQuote(seed.ChannelCode),
		"--task-id " + shellQuote("<task_id>"),
	}
	if pricingMode := strings.TrimSpace(seed.PricingMode); pricingMode != "" {
		parts = append(parts, "--pricing-mode "+shellQuote(pricingMode))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func auditConflictingAccountPoolSources(ctx context.Context, modelRepo *repo.ModelCatalogRepo, sourceRepo *repo.ModelSourceRepo, scope configurationAuditScope) ([]conflictEntry, error) {
	conflicts := []conflictEntry{}
	for page := 1; ; page++ {
		items, total, err := sourceRepo.List(ctx, repo.ModelSourceListFilter{
			SourceType: model.ModelSourceTypeAccountPool,
			Page:       page,
			PageSize:   200,
		})
		if err != nil {
			return nil, fmt.Errorf("list account-pool sources: %w", err)
		}
		for _, source := range items {
			if source == nil {
				continue
			}
			item, err := modelRepo.GetByCode(ctx, source.ModelCode)
			if err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					if !auditScopeMatchesModelSource(scope, nil, source) {
						continue
					}
					conflicts = append(conflicts, conflictEntry{
						ID:            source.ID,
						ModelCode:     source.ModelCode,
						SourceCode:    source.SourceCode,
						UpstreamModel: source.UpstreamModel,
						Status:        source.Status,
						Reason:        "模型库中不存在该模型编码",
					})
					continue
				}
				return nil, fmt.Errorf("read model catalog %s: %w", source.ModelCode, err)
			}
			if !auditScopeMatchesModelSource(scope, item, source) {
				continue
			}
			upstreamModel := source.UpstreamModel
			if strings.TrimSpace(upstreamModel) == "" {
				upstreamModel = item.UpstreamDefaultModel
			}
			if strings.TrimSpace(upstreamModel) == "" {
				upstreamModel = item.ModelCode
			}
			if reason := accountPoolConflictReason(item, source.SourceCode, upstreamModel); reason != "" {
				conflicts = append(conflicts, conflictEntry{
					ID:            source.ID,
					ModelCode:     source.ModelCode,
					SourceCode:    source.SourceCode,
					UpstreamModel: upstreamModel,
					Status:        source.Status,
					Reason:        reason,
				})
			}
		}
		if int64(page*200) >= total || len(items) == 0 {
			break
		}
	}
	return conflicts, nil
}

func accountPoolConflictReason(item *model.ModelCatalog, sourceCode, upstreamModel string) string {
	if item == nil {
		return ""
	}
	hint := strings.ToLower(strings.TrimSpace(item.ProviderHint))
	switch strings.ToLower(strings.TrimSpace(sourceCode)) {
	case model.ProviderGPT:
		if hint != "" && hint != "gpt" && hint != "openai" && hint != "chatgpt" {
			return "模型 Provider 提示与 GPT 账号池不匹配"
		}
	case model.ProviderGROK:
		if hint != "" && hint != "grok" && hint != "xai" {
			return "模型 Provider 提示与 Grok 账号池不匹配"
		}
	}
	for _, candidate := range []string{item.ProviderHint, item.ModelCode, item.UpstreamDefaultModel, upstreamModel} {
		value := strings.ToLower(strings.TrimSpace(candidate))
		if strings.HasPrefix(value, "mimo") || strings.HasPrefix(value, "deepseek") {
			return "MiMo/DeepSeek 等官方接口模型应挂到 API 渠道"
		}
	}
	return ""
}

func auditScopeMatchesModelSource(scope configurationAuditScope, item *model.ModelCatalog, source *model.ModelSourceMapping) bool {
	if scope.Global || source == nil {
		return true
	}
	for _, candidate := range []string{
		source.ModelCode,
		source.UpstreamModel,
		source.SourceCode,
		auditEffectiveSourceUpstream(item, source),
	} {
		token := auditSignatureToken(candidate)
		if token == "" {
			continue
		}
		if scope.ModelCodes[token] || scope.APIChannelCodes[token] {
			return true
		}
	}
	if item != nil {
		for _, candidate := range []string{item.ModelCode, item.ProviderHint, item.UpstreamDefaultModel} {
			token := auditSignatureToken(candidate)
			if token == "" {
				continue
			}
			if scope.ModelCodes[token] || scope.ProviderNames[token] {
				return true
			}
		}
	}
	return false
}

func auditScopeMatchesDuplicateEntry(scope configurationAuditScope, entry sourceDuplicateEntry) bool {
	if scope.Global {
		return true
	}
	for _, candidate := range []string{entry.ModelCode, entry.UpstreamModel} {
		if scope.ModelCodes[auditSignatureToken(candidate)] {
			return true
		}
	}
	return scope.APIChannelCodes[auditSignatureToken(entry.SourceCode)]
}

type auditSourceRouteSignature struct {
	ModelCode     string
	SourceType    string
	SourceCode    string
	UpstreamModel string
	Adapter       string
	AuthType      string
	ImageAPIMode  string
}

func auditDuplicateModelSources(ctx context.Context, modelRepo *repo.ModelCatalogRepo, sourceRepo *repo.ModelSourceRepo, scope configurationAuditScope) ([]sourceDuplicateEntry, error) {
	sources := []*model.ModelSourceMapping{}
	for page := 1; ; page++ {
		items, total, err := sourceRepo.List(ctx, repo.ModelSourceListFilter{Page: page, PageSize: 500})
		if err != nil {
			return nil, fmt.Errorf("list model sources: %w", err)
		}
		sources = append(sources, items...)
		if int64(page*500) >= total || len(items) == 0 {
			break
		}
	}
	modelsByCode := map[string]*model.ModelCatalog{}
	missingModels := map[string]bool{}
	for _, source := range sources {
		if source == nil {
			continue
		}
		modelCode := strings.TrimSpace(source.ModelCode)
		if modelCode == "" || modelsByCode[modelCode] != nil || missingModels[modelCode] {
			continue
		}
		item, err := modelRepo.GetByCode(ctx, modelCode)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				missingModels[modelCode] = true
				continue
			}
			return nil, fmt.Errorf("read model catalog %s: %w", modelCode, err)
		}
		modelsByCode[modelCode] = item
	}
	duplicates := duplicateModelSourceEntries(modelsByCode, sources)
	if scope.Global {
		return duplicates, nil
	}
	out := make([]sourceDuplicateEntry, 0, len(duplicates))
	for _, entry := range duplicates {
		if auditScopeMatchesDuplicateEntry(scope, entry) {
			out = append(out, entry)
		}
	}
	return out, nil
}

func duplicateModelSourceEntries(modelsByCode map[string]*model.ModelCatalog, sources []*model.ModelSourceMapping) []sourceDuplicateEntry {
	seen := map[auditSourceRouteSignature]*model.ModelSourceMapping{}
	out := []sourceDuplicateEntry{}
	for _, source := range sources {
		if source == nil {
			continue
		}
		item := modelsByCode[strings.TrimSpace(source.ModelCode)]
		sig := auditModelSourceSignature(item, source)
		if previous := seen[sig]; previous != nil {
			out = append(out, sourceDuplicateEntry{
				FirstID:       previous.ID,
				DuplicateID:   source.ID,
				ModelCode:     strings.TrimSpace(source.ModelCode),
				SourceType:    strings.TrimSpace(source.SourceType),
				SourceCode:    strings.TrimSpace(source.SourceCode),
				UpstreamModel: auditEffectiveSourceUpstream(item, source),
				Adapter:       strings.TrimSpace(source.Adapter),
				AuthType:      strings.TrimSpace(source.AuthType),
				ImageAPIMode:  strings.TrimSpace(source.ImageAPIMode),
				Reason:        "duplicate model source mapping route signature",
			})
			continue
		}
		seen[sig] = source
	}
	return out
}

func auditModelSourceSignature(item *model.ModelCatalog, source *model.ModelSourceMapping) auditSourceRouteSignature {
	if source == nil {
		return auditSourceRouteSignature{}
	}
	return auditSourceRouteSignature{
		ModelCode:     auditSignatureToken(source.ModelCode),
		SourceType:    auditSignatureToken(source.SourceType),
		SourceCode:    auditSignatureToken(source.SourceCode),
		UpstreamModel: auditSignatureToken(auditEffectiveSourceUpstream(item, source)),
		Adapter:       auditSignatureToken(source.Adapter),
		AuthType:      auditSignatureToken(source.AuthType),
		ImageAPIMode:  auditSignatureToken(source.ImageAPIMode),
	}
}

func auditEffectiveSourceUpstream(item *model.ModelCatalog, source *model.ModelSourceMapping) string {
	upstream := ""
	if source != nil {
		upstream = strings.TrimSpace(source.UpstreamModel)
	}
	if upstream != "" {
		return upstream
	}
	if item != nil {
		if upstream = strings.TrimSpace(item.UpstreamDefaultModel); upstream != "" {
			return upstream
		}
		return strings.TrimSpace(item.ModelCode)
	}
	if source != nil {
		return strings.TrimSpace(source.ModelCode)
	}
	return ""
}

func auditSignatureToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func configureProvider(
	ctx context.Context,
	apiRepo *repo.APIChannelRepo,
	modelRepo *repo.ModelCatalogRepo,
	sourceRepo *repo.ModelSourceRepo,
	apiSvc *service.APIChannelAdminService,
	modelSvc *service.ModelGatewayAdminService,
	seed providerSeed,
) error {
	if err := validateSeed(seed); err != nil {
		return err
	}
	ch, err := upsertAPIChannel(ctx, apiRepo, apiSvc, seed)
	if err != nil {
		return err
	}
	if err := upsertAPIChannelKey(ctx, apiSvc, seed, ch.ID); err != nil {
		return err
	}
	if err := upsertModelCatalog(ctx, modelRepo, modelSvc, seed); err != nil {
		return err
	}
	return upsertModelSource(ctx, sourceRepo, modelSvc, seed)
}

func validateSeed(seed providerSeed) error {
	if providerAPIKey(seed) == "" {
		return fmt.Errorf("%s is empty", seed.APIKeyEnv)
	}
	required := map[string]string{
		"channel code": seed.ChannelCode,
		"channel name": seed.ChannelName,
		"base url":     seed.BaseURL,
		"key name":     seed.KeyName,
		"public model": seed.PublicModel,
	}
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
	}
	return nil
}

func upsertAPIChannel(ctx context.Context, apiRepo *repo.APIChannelRepo, apiSvc *service.APIChannelAdminService, seed providerSeed) (*model.APIChannel, error) {
	current, err := apiRepo.GetByCode(ctx, seed.ChannelCode)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, fmt.Errorf("read api channel: %w", err)
	}
	if current == nil {
		ch, err := apiSvc.Create(ctx, 0, apiChannelCreateReq(seed))
		if err != nil {
			return nil, fmt.Errorf("create api channel: %w", err)
		}
		return ch, nil
	}
	err = apiSvc.Update(ctx, current.ID, apiChannelUpdateReq(seed))
	if err != nil {
		return nil, fmt.Errorf("update api channel: %w", err)
	}
	return current, nil
}

func apiChannelCreateReq(seed providerSeed) *dto.APIChannelCreateReq {
	status := int8(model.APIChannelStatusEnabled)
	priority, weight, timeout := apiChannelSeedRoutingDefaults(seed)
	return &dto.APIChannelCreateReq{
		Code:           seed.ChannelCode,
		Name:           seed.ChannelName,
		ProviderName:   seed.ProviderName,
		Adapter:        model.APIChannelAdapterOpenAIChat,
		BaseURL:        seed.BaseURL,
		Models:         uniqueStrings(seed.PublicModel, seed.UpstreamModel),
		Capabilities:   seed.Capabilities,
		Priority:       priority,
		Weight:         weight,
		TimeoutSeconds: timeout,
		Status:         &status,
		Remark:         seed.Remark,
	}
}

func apiChannelUpdateReq(seed providerSeed) *dto.APIChannelUpdateReq {
	status := int8(model.APIChannelStatusEnabled)
	priority, weight, timeout := apiChannelSeedRoutingDefaults(seed)
	return &dto.APIChannelUpdateReq{
		Code:           &seed.ChannelCode,
		Name:           &seed.ChannelName,
		ProviderName:   &seed.ProviderName,
		Adapter:        ptr(model.APIChannelAdapterOpenAIChat),
		BaseURL:        &seed.BaseURL,
		Models:         uniqueStrings(seed.PublicModel, seed.UpstreamModel),
		Capabilities:   seed.Capabilities,
		Priority:       &priority,
		Weight:         &weight,
		TimeoutSeconds: &timeout,
		Status:         &status,
		Remark:         &seed.Remark,
	}
}

func apiChannelSeedRoutingDefaults(seed providerSeed) (priority, weight, timeout int) {
	priority = seed.Priority
	if priority == 0 {
		priority = 100
	}
	weight = 100
	timeout = seed.TimeoutSeconds
	if timeout == 0 {
		timeout = 300
	}
	return priority, weight, timeout
}

func upsertAPIChannelKey(ctx context.Context, apiSvc apiChannelKeyService, seed providerSeed, channelID uint64) error {
	apiKey := providerAPIKey(seed)
	if apiKey == "" {
		return fmt.Errorf("%s is empty", seed.APIKeyEnv)
	}
	keyName := strings.TrimSpace(seed.KeyName)
	if keyName == "" {
		keyName = "primary"
	}
	status := int8(model.APIChannelKeyStatusEnabled)
	priority := 1
	weight := 100
	items, err := apiSvc.ListKeys(ctx, channelID)
	if err != nil {
		return fmt.Errorf("list api channel keys: %w", err)
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), keyName) {
			return apiSvc.UpdateKey(ctx, channelID, item.ID, &dto.APIChannelKeyUpdateReq{
				Name:     &keyName,
				APIKey:   &apiKey,
				Priority: &priority,
				Weight:   &weight,
				Status:   &status,
			})
		}
	}
	_, err = apiSvc.CreateKey(ctx, channelID, &dto.APIChannelKeyCreateReq{
		Name:     keyName,
		APIKey:   apiKey,
		Priority: priority,
		Weight:   weight,
		Status:   &status,
	})
	if err != nil {
		return fmt.Errorf("create api channel key: %w", err)
	}
	return nil
}

func upsertModelCatalog(ctx context.Context, modelRepo *repo.ModelCatalogRepo, modelSvc *service.ModelGatewayAdminService, seed providerSeed) error {
	visible := int8(1)
	status := int8(model.ModelCatalogStatusEnabled)
	minPlan := "free"
	sortOrder := seed.Priority
	desc := fmt.Sprintf("%s routed through an official API channel. Pricing is owned by Model Catalog and can be adjusted in the admin model gateway.", seed.DisplayName)
	upstreamModel := effectiveUpstreamModel(seed)

	current, err := modelRepo.GetByCode(ctx, seed.PublicModel)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return fmt.Errorf("read model catalog: %w", err)
	}
	pricing := pricingForUpsert(current, seed)
	parametersSchema := parametersSchemaForUpsert(current, seed)
	if current == nil {
		_, err := modelSvc.CreateModel(ctx, 0, &dto.ModelCatalogCreateReq{
			ModelCode:            seed.PublicModel,
			DisplayName:          seed.DisplayName,
			EntryKind:            seed.EntryKind,
			ProviderHint:         seed.ProviderName,
			UpstreamDefaultModel: upstreamModel,
			Capabilities:         seed.Capabilities,
			ParametersSchema:     parametersSchema,
			PricingMode:          pricing.Mode,
			UnitPoints:           pricing.UnitPoints,
			InputUnitPoints:      pricing.InputUnitPoints,
			OutputUnitPoints:     pricing.OutputUnitPoints,
			MinPlan:              minPlan,
			Tags:                 seed.Tags,
			Description:          desc,
			SortOrder:            sortOrder,
			Visible:              &visible,
			Status:               &status,
		})
		if err != nil {
			return fmt.Errorf("create model catalog: %w", err)
		}
		return nil
	}
	updateReq := &dto.ModelCatalogUpdateReq{
		DisplayName:          &seed.DisplayName,
		EntryKind:            &seed.EntryKind,
		ProviderHint:         &seed.ProviderName,
		UpstreamDefaultModel: &upstreamModel,
		Capabilities:         seed.Capabilities,
		PricingMode:          &pricing.Mode,
		UnitPoints:           &pricing.UnitPoints,
		InputUnitPoints:      &pricing.InputUnitPoints,
		OutputUnitPoints:     &pricing.OutputUnitPoints,
		MinPlan:              &minPlan,
		Tags:                 seed.Tags,
		Description:          &desc,
		SortOrder:            &sortOrder,
		Visible:              &visible,
		Status:               &status,
	}
	if parametersSchema != nil {
		updateReq.ParametersSchema = parametersSchema
	}
	err = modelSvc.UpdateModel(ctx, current.ID, updateReq)
	if err != nil {
		return fmt.Errorf("update model catalog: %w", err)
	}
	return nil
}

func upsertModelSource(ctx context.Context, sourceRepo *repo.ModelSourceRepo, modelSvc *service.ModelGatewayAdminService, seed providerSeed) error {
	status := int8(model.ModelSourceStatusEnabled)
	sourceType := model.ModelSourceTypeAPIChannel
	upstreamModel := effectiveUpstreamModel(seed)
	adapter := model.APIChannelAdapterOpenAIChat
	authType := model.AuthTypeAPIKey
	strategy := "round_robin"
	priority := seed.Priority
	if priority == 0 {
		priority = 100
	}
	weight := 100
	remark := "Official API channel mapping; do not move this model into GPT/GROK account pools."

	sources, _, err := sourceRepo.List(ctx, repo.ModelSourceListFilter{
		ModelCode:  seed.PublicModel,
		SourceType: sourceType,
		Page:       1,
		PageSize:   500,
	})
	if err != nil {
		return fmt.Errorf("list model sources: %w", err)
	}
	for _, item := range sources {
		if strings.EqualFold(item.SourceCode, seed.ChannelCode) {
			err := modelSvc.UpdateSource(ctx, item.ID, &dto.ModelSourceUpdateReq{
				ModelCode:     &seed.PublicModel,
				SourceType:    &sourceType,
				SourceCode:    &seed.ChannelCode,
				UpstreamModel: &upstreamModel,
				Adapter:       &adapter,
				AuthType:      &authType,
				Strategy:      &strategy,
				Priority:      &priority,
				Weight:        &weight,
				Status:        &status,
				Remark:        &remark,
			})
			if err != nil {
				return fmt.Errorf("update model source: %w", err)
			}
			return nil
		}
	}
	_, err = modelSvc.CreateSource(ctx, &dto.ModelSourceCreateReq{
		ModelCode:     seed.PublicModel,
		SourceType:    sourceType,
		SourceCode:    seed.ChannelCode,
		UpstreamModel: upstreamModel,
		Adapter:       adapter,
		AuthType:      authType,
		Strategy:      strategy,
		Priority:      priority,
		Weight:        weight,
		Status:        &status,
		Remark:        remark,
	})
	if err != nil {
		return fmt.Errorf("create model source: %w", err)
	}
	return nil
}

func effectiveUpstreamModel(seed providerSeed) string {
	if value := strings.TrimSpace(seed.UpstreamModel); value != "" {
		return value
	}
	return seed.PublicModel
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

type textPricingSeed struct {
	Mode             string
	UnitPoints       int64
	InputUnitPoints  int64
	OutputUnitPoints int64
	Explicit         bool
}

func defaultOfficialTextPricing(prefix string) textPricingSeed {
	return textPricingSeed{
		Mode:             envOrDefault(prefix+"_PRICING_MODE", model.ModelCatalogPricingToken),
		UnitPoints:       envStoredPoints(prefix+"_UNIT_POINTS", prefix+"_POINTS", 0),
		InputUnitPoints:  envStoredPoints(prefix+"_INPUT_UNIT_POINTS", prefix+"_INPUT_POINTS", 100),
		OutputUnitPoints: envStoredPoints(prefix+"_OUTPUT_UNIT_POINTS", prefix+"_OUTPUT_POINTS", 300),
		Explicit: envAnyPresent(
			prefix+"_PRICING_MODE",
			prefix+"_UNIT_POINTS",
			prefix+"_POINTS",
			prefix+"_INPUT_UNIT_POINTS",
			prefix+"_INPUT_POINTS",
			prefix+"_OUTPUT_UNIT_POINTS",
			prefix+"_OUTPUT_POINTS",
		),
	}
}

func defaultOfficialTextParametersSchema() map[string]any {
	return map[string]any{
		"controls": []map[string]any{
			{
				"key":     "temperature",
				"label":   "温度",
				"type":    "number",
				"min":     0,
				"max":     2,
				"step":    0.1,
				"default": 0.7,
			},
			{
				"key":     "max_tokens",
				"label":   "最大输出 Token",
				"type":    "number",
				"min":     1,
				"max":     8192,
				"step":    1,
				"default": 1200,
			},
		},
	}
}

func pricingForUpsert(current *model.ModelCatalog, seed providerSeed) textPricingSeed {
	seedPricing := textPricingSeed{
		Mode:             strings.TrimSpace(seed.PricingMode),
		UnitPoints:       seed.UnitPoints,
		InputUnitPoints:  seed.InputUnitPoints,
		OutputUnitPoints: seed.OutputUnitPoints,
		Explicit:         seed.PricingExplicit,
	}
	if seedPricing.Mode == "" {
		seedPricing.Mode = model.ModelCatalogPricingToken
	}
	if current != nil && modelCatalogHasEffectivePricing(current) && !seed.PricingExplicit {
		return textPricingSeed{
			Mode:             current.PricingMode,
			UnitPoints:       current.UnitPoints,
			InputUnitPoints:  current.InputUnitPoints,
			OutputUnitPoints: current.OutputUnitPoints,
			Explicit:         false,
		}
	}
	return seedPricing
}

func parametersSchemaForUpsert(current *model.ModelCatalog, seed providerSeed) any {
	if current != nil && current.ParametersSchema != nil && strings.TrimSpace(*current.ParametersSchema) != "" {
		return nil
	}
	return seed.ParametersSchema
}

func modelCatalogHasEffectivePricing(item *model.ModelCatalog) bool {
	if item == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.EntryKind)) {
	case model.ModelCatalogKindText, model.ModelCatalogKindChat:
		return item.InputUnitPoints > 0 || item.OutputUnitPoints > 0 || item.UnitPoints > 0
	case model.ModelCatalogKindImage, model.ModelCatalogKindVideo:
		if item.UnitPoints > 0 {
			return true
		}
		return item.PriceRules != nil && strings.TrimSpace(*item.PriceRules) != ""
	default:
		return item.UnitPoints > 0 || item.InputUnitPoints > 0 || item.OutputUnitPoints > 0
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envStoredPoints(storedKey, humanPointsKey string, fallback int64) int64 {
	if raw := strings.TrimSpace(os.Getenv(storedKey)); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value >= 0 {
			return value
		}
	}
	if raw := strings.TrimSpace(os.Getenv(humanPointsKey)); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value >= 0 {
			return int64(math.Round(value * 100))
		}
	}
	return fallback
}

func envAnyPresent(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func uniqueStrings(values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func ptr[T any](value T) *T {
	return &value
}
