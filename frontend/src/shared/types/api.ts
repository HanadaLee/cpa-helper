export type ThemePreference = 'system' | 'light' | 'dark'
export type SSODefaultLogin = '' | 'cas' | 'oidc'

export interface AuthUser {
  id: number
  username: string
  nickname: string
  email: string
  avatar: string
  created_at: string
  is_admin: boolean
  cas_bound: boolean
  oidc_bound: boolean
  can_change_password: boolean
  must_change_password: boolean
  can_view_account_status: boolean
  can_view_usage_history: boolean
}

export interface LoginPayload {
  username: string
  password: string
}

export interface ChangeCredentialsPayload {
  password: string
  current_password?: string | undefined
}

export interface SetupState {
  setup_required: boolean
  cas_enabled: boolean
  cas_default_login: boolean
  oidc_enabled: boolean
  sso_default_login: SSODefaultLogin
}

export interface FirstAdminSetupPayload {
  username: string
  password: string
  nickname: string
}

export interface ModelRequestExtraEndpoint {
  url: string
  description: string
}

export interface BrandingResponse {
  brand_name_zh: string
  brand_name_en: string
  brand_subtitle_zh: string
  brand_subtitle_en: string
}

export interface SettingsResponse extends BrandingResponse {
  cliaproxy_url: string
  model_request_url: string
  model_request_extra_endpoints: ModelRequestExtraEndpoint[]
  cpamc_url: string
  management_key: string
  management_key_set: boolean
  collector_enabled: boolean
  queue_name: string
  batch_size: number
  poll_interval_seconds: number
  retry_interval_seconds: number
  allow_user_account_status: boolean
  allow_user_usage_history: boolean
  usage_detail_retention_days: number
  cas_enabled: boolean
  cas_default_login: boolean
  cas_base_url: string
  cas_validation_url: string
  cas_validation_host: string
  cas_public_url: string
  cas_auto_create_users: boolean
  sso_default_login: SSODefaultLogin
  oidc_enabled: boolean
  oidc_issuer: string
  oidc_client_id: string
  oidc_client_secret_set: boolean
  oidc_public_url: string
  oidc_logout_url: string
  oidc_username_claim: string
  oidc_auto_create_users: boolean
  new_user_quota_unlimited: boolean
  new_user_quota_daily_usd: number
  new_user_quota_weekly_usd: number
}

export interface SettingsUpdatePayload {
  cliaproxy_url?: string
  model_request_url?: string
  model_request_extra_endpoints?: ModelRequestExtraEndpoint[]
  cpamc_url?: string
  brand_name_zh?: string
  brand_name_en?: string
  brand_subtitle_zh?: string
  brand_subtitle_en?: string
  management_key?: string
  collector_enabled?: boolean
  queue_name?: string
  batch_size?: number
  poll_interval_seconds?: number
  retry_interval_seconds?: number
  allow_user_account_status?: boolean
  allow_user_usage_history?: boolean
  usage_detail_retention_days?: number
  cas_enabled?: boolean
  cas_default_login?: boolean
  cas_base_url?: string
  cas_validation_url?: string
  cas_validation_host?: string
  cas_public_url?: string
  cas_auto_create_users?: boolean
  sso_default_login?: SSODefaultLogin
  oidc_enabled?: boolean
  oidc_issuer?: string
  oidc_client_id?: string
  oidc_client_secret?: string
  oidc_public_url?: string
  oidc_logout_url?: string
  oidc_username_claim?: string
  oidc_auto_create_users?: boolean
  new_user_quota_unlimited?: boolean
  new_user_quota_daily_usd?: number
  new_user_quota_weekly_usd?: number
}

export interface ModelRequestGuide {
  model_request_url: string
  openai_base_url: string
  chat_completions_url: string
  extra_endpoints: ModelRequestExtraEndpoint[]
}

export type ModelRequestEndpoint = 'chat_completions' | 'responses' | 'claude_messages'

export interface ModelRequestTestPayload {
  api_key_hash: string
  endpoint: ModelRequestEndpoint
  base_url: string
  model: string
  message: string
}

export interface ModelRequestTestResponse {
  endpoint: ModelRequestEndpoint
  model: string
  reply: string
  status_code: number
  duration_ms: number
  usage?: Record<string, unknown>
}

export interface CollectorStatus {
  enabled: boolean
  running: boolean
  queue_name: string
  batch_size: number
  poll_interval_seconds: number
  retry_interval_seconds: number
  last_poll_at: string | null
  last_success_at: string | null
  last_error: string | null
  remote_enabled: boolean | null
  records_collected: number
}

export interface CardShopProductItem {
  name?: string | null
  price?: number | null
  stockCount?: number | null
  salesCount?: number | null
  itemUrl?: string | null
  category?: string | null
  group?: string | null
}

export interface CardShopProductSummary {
  totalCount?: number | null
}

export interface CardShop {
  id?: string | null
  shopName?: string | null
  shopUrl?: string | null
  telegram?: string | null
  shopSellCount?: number | null
  productsInStock?: string[] | null
  productItems?: CardShopProductItem[] | null
  productSummary?: CardShopProductSummary | null
  notes?: string | null
  updatedAt?: string | null
}

export interface CardShopsResponse {
  shops: CardShop[]
  fetched_at: string
}

export interface CardShopFavoritesResponse {
  shop_keys: string[]
}

export interface CardShopFavoriteUpdatePayload {
  shop_key: string
  favorite: boolean
}

export interface CardShopTagsResponse {
  tags: string[]
}

export interface CardShopTagsUpdatePayload {
  tags: string[]
}

export interface CodexKeeperPriorityRule {
  account_type: string
  priority: number
}

export interface CodexKeeperSettings {
  cliaproxy_url: string
  management_key_set: boolean
  schedule_cron: string
  next_run_times: string[]
  quota_threshold: number
  usage_timeout_seconds: number
  cpa_timeout_seconds: number
  max_retries: number
  worker_threads: number
  conditional_refresh_interval_seconds: number
  account_refresh_cache_minutes: number
  dry_run: boolean
  enable_credential_websockets: boolean
  auto_start_daemon: boolean
  priority_rules: CodexKeeperPriorityRule[]
}

export interface CodexKeeperSettingsUpdatePayload {
  schedule_cron?: string
  quota_threshold?: number
  usage_timeout_seconds?: number
  cpa_timeout_seconds?: number
  max_retries?: number
  worker_threads?: number
  conditional_refresh_interval_seconds?: number
  account_refresh_cache_minutes?: number
  dry_run?: boolean
  enable_credential_websockets?: boolean
  auto_start_daemon?: boolean
  priority_rules?: CodexKeeperPriorityRule[]
}

export interface CodexKeeperCronPreviewPayload {
  schedule_cron: string
}

export interface CodexKeeperCronPreviewResponse {
  schedule_cron: string
  next_run_times: string[]
}

export interface CodexKeeperStats {
  total: number
  healthy: number
  status_disabled: number
  status_enabled: number
  priority_degraded: number
  priority_restored: number
  skipped: number
  network_error: number
}

export interface CodexKeeperStatus {
  running: boolean
  running_modes: string[]
  daemon_running: boolean
  state: string
  detail: string
  mode: string | null
  last_started_at: string | null
  last_finished_at: string | null
  stats: CodexKeeperStats
  logs: string[]
}

export interface CodexKeeperQuotaWindowUsage {
  window_start: string
  window_end: string
  reset_at: string
  window_seconds: number
  records: number
  success_records: number
  failed_records: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  unpriced_records: number
  stale: boolean
  window_source: string
}

export interface CodexKeeperResetCredits {
  available_count: number
  total_earned_count: number | null
  earliest_expires_at: string | null
  cached_at: string
}

export interface CodexKeeperAccount {
  name: string
  provider: string
  email: string | null
  label: string | null
  account: string | null
  project_id: string | null
  user_id: string | null
  auth_index: string | null
  account_type: string | null
  plan_type: string | null
  weight: number | null
  request_retry: number | null
  disabled: boolean
  unavailable: boolean
  runtime_only: boolean
  status: string | null
  status_message: string | null
  success: number | null
  failed: number | null
  quota: Record<string, unknown> | null
  model_quotas: Record<string, unknown> | null
  last_refresh_at: string | null
  modified_at: string | null
  priority: number | null
  primary_used_percent: number | null
  secondary_used_percent: number | null
  primary_reset_at: string | null
  secondary_reset_at: string | null
  primary_window_seconds: number | null
  secondary_window_seconds: number | null
  primary_window_usage: CodexKeeperQuotaWindowUsage | null
  secondary_window_usage: CodexKeeperQuotaWindowUsage | null
  quota_threshold: number | null
  last_status_code: number | null
  last_error: string | null
  latest_action: string | null
  last_checked_at: string | null
  last_healthy_at: string | null
  reset_credits: CodexKeeperResetCredits | null
}

export interface CodexKeeperAuthFileFields {
  prefix?: string
  proxy_url?: string
  priority?: number
  weight?: number | null
  request_retry?: number | null
  websockets?: boolean
  using_api?: boolean
  note?: string
  headers?: Record<string, string>
}

export interface CodexKeeperAuthFileModel {
  id: string
  display_name?: string
  type?: string
  owned_by?: string
}

export interface CodexKeeperAuthFileModelsResponse {
  models: CodexKeeperAuthFileModel[]
}

export interface CodexKeeperAuthFileDetail {
  json: Record<string, unknown> | null
  raw_text?: string
  invalid_reason?: 'invalid_json' | 'html_challenge'
}

export interface CodexKeeperAuthFileUploadFailure {
  name: string
  error: string
}

export interface CodexKeeperAuthFileUploadResponse {
  status: string
  uploaded: number
  files: string[]
  failed: CodexKeeperAuthFileUploadFailure[]
}

export interface CodexKeeperOAuthStartResponse {
  url: string
  state: string
}

export type CredentialOAuthProvider = 'codex' | 'anthropic' | 'antigravity' | 'gemini-cli' | 'kimi' | 'xai'

export interface CodexKeeperOAuthStatusResponse {
  status: string
  error?: string
}

export interface CodexKeeperAccountsResponse {
  items: CodexKeeperAccount[]
  priority_rules: CodexKeeperPriorityRule[]
  degraded?: boolean
}

export interface CodexKeeperBulkDeletePayload {
  auth_names: string[]
}

export interface CodexKeeperRefreshPayload {
  auth_names: string[]
}

export interface CodexKeeperBulkDeleteFailure {
  name: string
  message: string
}

export interface CodexKeeperBulkDeleteResponse {
  status: string
  deleted: string[]
  failed: CodexKeeperBulkDeleteFailure[]
}

export interface UsageFilters {
  scope?: 'admin' | 'account' | 'shared' | undefined
  start?: string | undefined
  end?: string | undefined
  user_id?: number | undefined
  api_key_description?: string | undefined
  provider?: string | undefined
  model?: string | undefined
  source_key?: string | undefined
  endpoint?: string | undefined
  failed?: boolean | undefined
  request_id?: string | undefined
}

export interface UsageSummary {
  start: string
  end: string
  total_records: number
  failed_records: number
  success_records: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  cache_hit_tokens: number
  reasoning_tokens: number
  total_tokens: number
  zero_token_records: number
  average_ttft_ms: number | null
  estimated_cost_usd: number
  unpriced_records: number
}

export interface UsageBillingTotals {
  cost_usd: number
  records: number
  failed_records: number
  total_tokens: number
  unpriced_records: number
}

export interface UsageBillingUser {
  user_id: number | null
  username: string
  label: string
}

export interface UsageBillingItem extends UsageBillingUser, UsageBillingTotals {}

export interface UsageBillingResponse {
  start: string
  end: string
  summary: UsageBillingTotals
  active_users: number
  items: UsageBillingItem[]
  users: UsageBillingUser[]
}

export interface TrendPoint {
  bucket: string
  records: number
  failed_records: number
  total_tokens: number
  estimated_cost_usd: number
}

export interface RankingItem {
  key: string
  label: string
  records: number
  failed_records: number
  total_tokens: number
  estimated_cost_usd: number
  user_id: number | null
  api_key_description: string | null
}

export interface UsageRankingsResponse {
  group_by: 'api_key_description' | 'model' | 'user'
  items: RankingItem[]
}

export interface DistributionItem {
  key: string
  label: string
  records: number
  total_tokens: number
  estimated_cost_usd: number
}

export interface UsageDistributionsResponse {
  providers: DistributionItem[]
  models: DistributionItem[]
  endpoints: DistributionItem[]
}

export interface UsageOptionsResponse {
  users: RankingItem[]
  api_key_descriptions: RankingItem[]
  providers: string[]
  models: string[]
  sources: UsageSourceOption[]
  endpoints: string[]
}

export interface UsageSourceOption {
  key: string
  label: string
}

export interface UsageOverviewResponse {
  summary: UsageSummary
  trends: TrendPoint[]
  user_ranking: UsageRankingsResponse
  api_key_description_ranking?: UsageRankingsResponse
  api_key_ranking?: UsageRankingsResponse
  model_ranking: UsageRankingsResponse
  distributions: UsageDistributionsResponse
  options?: UsageOptionsResponse
  today_trends: TrendPoint[]
  failed_summary: UsageSummary
  failed_trends: TrendPoint[]
  failed_endpoint_distribution: DistributionItem[]
  realtime_summary: UsageSummary
}

export interface UsageRecordListItem {
  id: number
  timestamp: string
  api_key_description: string | null
  user_id: number | null
  user_label: string
  provider: string | null
  model: string | null
  reasoning_effort: string | null
  request_service_tier: string | null
  endpoint: string | null
  source: string | null
  request_id: string | null
  auth_index: string | null
  auth: string | null
  latency_ms: number | null
  ttft_ms: number | null
  failed: boolean
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  reasoning_tokens: number
  total_tokens: number
  estimated_cost_usd: number
  unpriced: boolean
}

export interface UsageRecordsResponse {
  items: UsageRecordListItem[]
  total?: number
  page: number
  page_size: number
  start: string
  end: string
}

export interface UsageRecordDetail extends UsageRecordListItem {
  raw_json: Record<string, unknown> | unknown[] | string
}

export interface ModelPrice {
  id: number
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
  fast_enabled: boolean
  fast_multiplier: number
  long_context_enabled: boolean
  long_context_threshold_tokens: number
  long_context_input_usd_per_million: number
  long_context_output_usd_per_million: number
  long_context_cache_read_usd_per_million: number
  long_context_cache_creation_usd_per_million: number
  long_context_fast_unsupported: boolean
  off_peak_enabled: boolean
  peak_input_usd_per_million: number
  peak_output_usd_per_million: number
  peak_cache_read_usd_per_million: number
  peak_cache_creation_usd_per_million: number
  peak_request_usd: number | null
  long_context_peak_input_usd_per_million: number
  long_context_peak_output_usd_per_million: number
  long_context_peak_cache_read_usd_per_million: number
  long_context_peak_cache_creation_usd_per_million: number
  billing_unit: 'token' | 'request' | string
  source: 'manual' | 'litellm' | string
  source_model: string | null
  auto_synced: boolean
  last_synced_at: string | null
  updated_at: string
}

export interface ModelPricePayload {
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
  fast_enabled: boolean
  fast_multiplier: number
  long_context_enabled: boolean
  long_context_threshold_tokens: number
  long_context_input_usd_per_million: number
  long_context_output_usd_per_million: number
  long_context_cache_read_usd_per_million: number
  long_context_cache_creation_usd_per_million: number
  long_context_fast_unsupported: boolean
  off_peak_enabled: boolean
  peak_input_usd_per_million: number
  peak_output_usd_per_million: number
  peak_cache_read_usd_per_million: number
  peak_cache_creation_usd_per_million: number
  peak_request_usd: number | null
  long_context_peak_input_usd_per_million: number
  long_context_peak_output_usd_per_million: number
  long_context_peak_cache_read_usd_per_million: number
  long_context_peak_cache_creation_usd_per_million: number
}

export interface PricingHolidayCalendar {
  year: number
  configured: boolean
  dates: string[]
  days: Array<{ date: string; name: string; is_off_day: boolean }>
  source_url: string
  synced_at: string | null
  peak_on_makeup_days: boolean
  peak_periods: Array<{ start: string; end: string }>
}

export interface ModelPriceSyncResponse {
  source_url: string
  total_entries: number
  imported: number
  created: number
  updated: number
  unchanged: number
  skipped_manual: number
  skipped_invalid: number
}

export interface ModelPriceCatalogItem {
  id: string
  name: string
  object: string | null
  owner: string | null
  created: number | null
  metadata: Record<string, string | number | boolean | null>
  suggested_provider: string
  price: ModelPrice | null
  sources: AvailableModelSource[]
}

export interface ModelPriceCatalogResponse {
  has_api_keys: boolean
  api_key_count: number
  queryable_api_key_count: number
  models: ModelPriceCatalogItem[]
  errors: AvailableModelKeyError[]
  priced_models: number
  unpriced_models: number
}

export interface LiteLLMProxySettings {
  enabled: boolean
  proxy_url: string
}

export interface LiteLLMProxySettingsPayload {
  enabled: boolean
  proxy_url: string
}

export interface UserApiKeySummary {
  api_key_hash: string
  api_key: string | null
  description: string
  disabled: boolean
  user_id: number | null
  user_name: string | null
  created_at: string | null
  updated_at: string | null
  records: number
  success_records: number
  failed_records: number
  total_tokens: number
  today_records: number
  today_success_records: number
  today_failed_records: number
  today_input_tokens: number
  today_output_tokens: number
  today_cached_tokens: number
  today_reasoning_tokens: number
  today_total_tokens: number
  today_estimated_cost_usd: number
  today_unpriced_records: number
  first_seen_at: string | null
  last_seen_at: string | null
  last_provider: string | null
  last_model: string | null
  providers: string[]
  models: string[]
}

export interface UserQuotaStatus {
  unlimited: boolean
  cards_remaining_usd: number
  cards_total_usd: number
  limits_remaining_usd: number
  available_usd: number
  daily_resets_at: string
  weekly_resets_at: string
  weekly_quota_usd: number | null
  weekly_used_usd: number
  weekly_remaining_usd: number | null
  quota_week: string
  daily_quota_usd: number | null
  daily_used_usd: number
  daily_remaining_usd: number | null
  quota_day: string
  paused: boolean
  paused_at: string | null
  pause_reason: string | null
  sync_error: string | null
  unpriced_records: number
  can_create_keys: boolean
  started_at: string | null
}

export interface AvailableModelSource {
  api_key_hash: string
  api_key_preview: string
  description: string
  user_id?: number
  user_label?: string
}

export interface AvailableModelPrice {
  provider: string
  model: string
  input_usd_per_million: number
  output_usd_per_million: number
  cache_read_usd_per_million: number
  cache_creation_usd_per_million: number
  request_usd: number | null
  fast_enabled: boolean
  fast_multiplier: number
  long_context_enabled: boolean
  long_context_threshold_tokens: number
  long_context_input_usd_per_million: number
  long_context_output_usd_per_million: number
  long_context_cache_read_usd_per_million: number
  long_context_cache_creation_usd_per_million: number
  long_context_fast_unsupported: boolean
  off_peak_enabled: boolean
  peak_input_usd_per_million: number
  peak_output_usd_per_million: number
  peak_cache_read_usd_per_million: number
  peak_cache_creation_usd_per_million: number
  peak_request_usd: number | null
  long_context_peak_input_usd_per_million: number
  long_context_peak_output_usd_per_million: number
  long_context_peak_cache_read_usd_per_million: number
  long_context_peak_cache_creation_usd_per_million: number
  billing_unit: 'token' | 'request' | string
}

export interface AvailableModel {
  id: string
  name: string
  object: string | null
  owner: string | null
  created: number | null
  metadata: Record<string, string | number | boolean | null>
  price: AvailableModelPrice | null
  sources: AvailableModelSource[]
}

export interface AvailableModelKeyError {
  api_key_hash: string
  api_key_preview: string
  description: string
  message: string
}

export interface AvailableModelsResponse {
  has_api_keys: boolean
  api_key_count: number
  queryable_api_key_count: number
  quota_paused: boolean
  models: AvailableModel[]
  errors: AvailableModelKeyError[]
}

export interface UserSummary {
  id: number
  username: string
  is_admin: boolean
  nickname: string
  disabled_at: string | null
  password_set: boolean
  created_at: string
  updated_at: string
  api_keys: UserApiKeySummary[]
  key_count: number
  records: number
  success_records: number
  failed_records: number
  total_tokens: number
  today_records: number
  today_success_records: number
  today_failed_records: number
  today_input_tokens: number
  today_output_tokens: number
  today_cached_tokens: number
  today_reasoning_tokens: number
  today_total_tokens: number
  today_estimated_cost_usd: number
  today_unpriced_records: number
  first_seen_at: string | null
  last_seen_at: string | null
  last_provider: string | null
  last_model: string | null
  providers: string[]
  models: string[]
  quota: UserQuotaStatus
}

export interface UserPayload {
  username: string
  password?: string | undefined
  is_admin: boolean
  nickname: string
}

export interface UserQuotaPayload {
  weekly_quota_usd: number | null
  daily_quota_usd: number | null
}

export interface QuotaCard {
  id: number
  user_id: number
  username: string
  kind: 'credit' | 'reset'
  name: string
  amount_usd: number
  used_usd: number
  remaining_usd: number
  status: 'active' | 'unused' | 'expired' | 'exhausted' | 'used' | 'revoked'
  expires_at: string | null
  activated_at: string | null
  used_at: string | null
  revoked_at: string | null
  created_at: string
  batch_id: string
}

export interface QuotaPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface QuotaTargets { user_ids?: number[]; all_users?: boolean }

export interface QuotaCardPayload extends QuotaTargets {
  kind: 'credit' | 'reset'
  name: string
  amount_usd: number
  expires_at: string | null
  count?: number
}

export interface QuotaCharge {
  id: number
  amount_usd: number
  daily_usd: number
  weekly_usd: number
  limit_usd: number
  cards_usd: number
  legacy_usd: number
  uncovered_usd: number
  unpriced: boolean
  timestamp: string
  created_at: string
  cards: { card_id: number; name: string; amount_usd: number }[]
}

export interface QuotaReset {
  id: number
  card_id: number | null
  daily_used_usd: number
  weekly_used_usd: number
  created_at: string
}

export interface UserApiKeyBindPayload {
  api_key?: string
  api_key_hash?: string
  description: string
}

export interface ApiKeyCreatePayload {
  description: string
}

export interface ApiKeyUpdatePayload {
  description: string
}
