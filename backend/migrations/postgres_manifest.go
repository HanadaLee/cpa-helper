package migrations

// PostgreSQLBaselineVersion is also the schema understood by the atomic SQLite importer.
// Regenerate the import baseline/manifest when advancing the current schema.
const PostgreSQLBaselineVersion int64 = 202609280001

// PostgreSQLTables is the baseline's dependency order. Keep it in sync with both
// schemas; the importer refuses missing/extra tables or columns.
var PostgreSQLTables = []string{"app_settings", "codex_keeper_auth_states", "codex_keeper_runs", "codex_keeper_run_accounts", "collector_state", "model_prices", "oidc_flows", "pricing_calendar_settings", "pricing_holiday_years", "pricing_holidays", "tutorials", "usage_hourly_rollups", "usage_records", "usage_ingest_dedup", "usage_rollup_state", "users", "oidc_identities", "oidc_sessions", "quota_cards", "quota_resets", "user_api_keys", "user_card_shop_favorites", "user_card_shop_tags", "user_quota_charges", "quota_card_deductions"}
