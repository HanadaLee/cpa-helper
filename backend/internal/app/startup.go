package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cpa-helper/backend/internal/platform/database"
	backendMigrations "cpa-helper/backend/migrations"
)

var (
	ErrDatabaseNotInitialized = errors.New("database is not initialized")
	ErrDatabaseNeedsMigration = errors.New("database needs migration")
	ErrDatabaseTooNew         = errors.New("database migration version is newer than this application")
	ErrAppSettingsMissing     = errors.New("app settings row is missing")
)

type NewOptions struct {
	Migrate         bool
	RequireReady    bool
	StartBackground bool
}

type RuntimePaths struct {
	RepoRoot string
	DataDir  string
	DBDir    string
	DBPath   string
}

type MigrationReport struct {
	DBPath          string
	PreviousVersion int64
	CurrentVersion  int64
	TargetVersion   int64
}

type StartupCheck struct {
	DBPath         string
	CurrentVersion int64
	TargetVersion  int64
}

func Migrate(ctx context.Context) (MigrationReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, err := resolveRuntimePaths()
	if err != nil {
		return MigrationReport{}, err
	}
	db, err := openRuntimeDB(paths, false)
	if err != nil {
		return MigrationReport{}, err
	}
	defer db.Close()
	if db.Postgres {
		paths.DBPath = "postgresql (CPA_HELPER_DATABASE_URL)"
	}

	before, err := currentMigrationVersion(ctx, db)
	if err != nil && !errors.Is(err, ErrDatabaseNotInitialized) {
		return MigrationReport{DBPath: paths.DBPath, TargetVersion: backendMigrations.LatestVersion}, err
	}
	app := &App{db: db}
	if err := app.runMigrations(ctx); err != nil {
		return MigrationReport{DBPath: paths.DBPath, PreviousVersion: before, TargetVersion: backendMigrations.LatestVersion}, err
	}
	after, err := currentMigrationVersion(ctx, db)
	if err != nil {
		return MigrationReport{DBPath: paths.DBPath, PreviousVersion: before, TargetVersion: backendMigrations.LatestVersion}, err
	}
	return MigrationReport{
		DBPath:          paths.DBPath,
		PreviousVersion: before,
		CurrentVersion:  after,
		TargetVersion:   backendMigrations.LatestVersion,
	}, nil
}

func CheckStartup(ctx context.Context) (StartupCheck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, err := resolveRuntimePaths()
	if err != nil {
		return StartupCheck{}, err
	}
	return checkStartupPaths(ctx, paths)
}

func (a *App) Readiness(ctx context.Context) (StartupCheck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return checkDatabaseReady(ctx, a.db, filepath.Join(a.dataDir, "db", "cpa_helper.sqlite3"))
}

func resolveRuntimePaths() (RuntimePaths, error) {
	repoRoot, err := detectRepoRoot()
	if err != nil {
		return RuntimePaths{}, err
	}
	dataDir := os.Getenv("CPA_HELPER_DATA_DIR")
	if strings.TrimSpace(dataDir) == "" {
		dataDir = filepath.Join(repoRoot, "data")
	}
	dbDir := filepath.Join(dataDir, "db")
	return RuntimePaths{
		RepoRoot: repoRoot,
		DataDir:  dataDir,
		DBDir:    dbDir,
		DBPath:   filepath.Join(dbDir, "cpa_helper.sqlite3"),
	}, nil
}

func openRuntimeDB(paths RuntimePaths, readOnly bool) (*database.DB, error) {
	if dsn := strings.TrimSpace(os.Getenv("CPA_HELPER_DATABASE_URL")); dsn != "" {
		maxConnections := 16
		if value := strings.TrimSpace(os.Getenv("CPA_HELPER_DB_MAX_CONNS")); value != "" {
			var err error
			maxConnections, err = strconv.Atoi(value)
			if err != nil || maxConnections < 2 || maxConnections > 100 {
				return nil, fmt.Errorf("CPA_HELPER_DB_MAX_CONNS must be between 2 and 100")
			}
		}
		return database.OpenPostgres(dsn, maxConnections)
	}
	if readOnly {
		if _, err := os.Stat(paths.DBPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: SQLite database file is missing; run `cpa-helper migrate`", ErrDatabaseNotInitialized)
			}
			return nil, err
		}
	} else if err := os.MkdirAll(paths.DBDir, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteDSN(paths.DBPath, readOnly))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return database.SQLite(db), nil
}

func sqliteDSN(dbPath string, readOnly bool) string {
	if readOnly {
		return "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	return dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

func checkStartupPaths(ctx context.Context, paths RuntimePaths) (StartupCheck, error) {
	db, err := openRuntimeDB(paths, true)
	if err != nil {
		return StartupCheck{DBPath: paths.DBPath, TargetVersion: backendMigrations.LatestVersion}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return StartupCheck{DBPath: paths.DBPath, TargetVersion: backendMigrations.LatestVersion}, err
	}
	return checkDatabaseReady(ctx, db, paths.DBPath)
}

func checkDatabaseReady(ctx context.Context, db *database.DB, dbPath string) (StartupCheck, error) {
	if db.Postgres {
		dbPath = "postgresql (CPA_HELPER_DATABASE_URL)"
	}
	current, err := currentMigrationVersion(ctx, db)
	report := StartupCheck{
		DBPath:         dbPath,
		CurrentVersion: current,
		TargetVersion:  backendMigrations.LatestVersion,
	}
	if err != nil {
		return report, err
	}
	if current < backendMigrations.LatestVersion {
		return report, fmt.Errorf("%w: current version %d, target version %d; run `cpa-helper migrate`", ErrDatabaseNeedsMigration, current, backendMigrations.LatestVersion)
	}
	if current > backendMigrations.LatestVersion {
		return report, fmt.Errorf("%w: current version %d, target version %d", ErrDatabaseTooNew, current, backendMigrations.LatestVersion)
	}
	if err := requireSchemaShape(ctx, db); err != nil {
		return report, err
	}
	if err := requireAppSettingsRow(ctx, db); err != nil {
		return report, err
	}
	return report, nil
}

func currentMigrationVersion(ctx context.Context, db *database.DB) (int64, error) {
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		if strings.Contains(err.Error(), "no such table: goose_db_version") || strings.Contains(err.Error(), "SQLSTATE 42P01") {
			return 0, fmt.Errorf("%w: goose_db_version table is missing", ErrDatabaseNotInitialized)
		}
		return 0, err
	}
	if !version.Valid || version.Int64 <= 0 {
		return 0, ErrDatabaseNotInitialized
	}
	return version.Int64, nil
}

func requireSchemaShape(ctx context.Context, db *database.DB) error {
	required := []struct {
		table  string
		column string
	}{
		{"tutorials", "markdown"},
		{"tutorials", "title"},
		{"tutorials", "published"},
		{"app_settings", "session_secret"},
		{"app_settings", "cas_enabled"},
		{"app_settings", "cas_default_login"},
		{"app_settings", "oidc_settings"},
		{"app_settings", "sso_default_login"},
		{"oidc_identities", "subject"},
		{"oidc_flows", "state_hash"},
		{"oidc_sessions", "id_token"},
		{"app_settings", "new_user_quota_unlimited"},
		{"app_settings", "new_user_quota_daily_usd"},
		{"app_settings", "new_user_quota_weekly_usd"},
		{"app_settings", "new_user_quota_monthly_usd"},
		{"app_settings", "new_user_quota_lifetime_usd"},
		{"users", "username"},
		{"users", "cas_bound"},
		{"users", "cas_email"},
		{"users", "cas_avatar"},
		{"usage_records", "dedupe_key"},
		{"usage_records", "ttft_ms"},
		{"usage_records", "source_key"},
		{"usage_records", "cost_usd"},
		{"usage_records", "unpriced"},
		{"usage_ingest_dedup", "dedupe_key"},
		{"usage_hourly_rollups", "cost_usd"},
		{"usage_hourly_rollups", "cache_creation_tokens"},
		{"usage_hourly_rollups", "zero_token_records"},
		{"usage_rollup_state", "last_rolled_usage_id"},
		{"app_settings", "usage_detail_retention_days"},
		{"model_prices", "request_usd"},
		{"model_prices", "fast_multiplier"},
		{"model_prices", "long_context_enabled"},
		{"model_prices", "long_context_threshold_tokens"},
		{"model_prices", "long_context_input_usd_per_million"},
		{"model_prices", "long_context_output_usd_per_million"},
		{"model_prices", "long_context_cache_read_usd_per_million"},
		{"model_prices", "long_context_cache_creation_usd_per_million"},
		{"model_prices", "long_context_fast_unsupported"},
		{"usage_records", "request_service_tier"},
		{"users", "quota_daily_usd"},
		{"users", "quota_weekly_usd"},
		{"user_quota_charges", "daily_deducted_usd"},
		{"user_quota_charges", "weekly_deducted_usd"},
		{"user_quota_charges", "lifetime_deducted_usd"},
		{"user_quota_charges", "usage_dedupe_key"},
		{"user_quota_charges", "cards_deducted_usd"},
		{"user_quota_charges", "limit_deducted_usd"},
		{"quota_cards", "expires_at"},
		{"quota_card_deductions", "charge_id"},
		{"quota_resets", "card_id"},
		{"user_card_shop_favorites", "shop_key"},
		{"user_card_shop_tags", "tag"},
	}
	for _, item := range required {
		ok, err := columnExists(ctx, db, item.table, item.column)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: missing schema column %s.%s; run `cpa-helper migrate`", ErrDatabaseNeedsMigration, item.table, item.column)
		}
	}
	return nil
}

func columnExists(ctx context.Context, db *database.DB, table, column string) (bool, error) {
	if db.Postgres {
		var exists bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?)`, table, column).Scan(&exists)
		return exists, err
	}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func requireAppSettingsRow(ctx context.Context, db *database.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE id = 1`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: app_settings id=1 is missing; run `cpa-helper migrate`", ErrAppSettingsMissing)
	}
	return nil
}
