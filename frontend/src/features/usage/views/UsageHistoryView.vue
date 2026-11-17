<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Toggle } from '@/components/ui/toggle'
import {
  CircleCheck,
  CircleDollarSign,
  ClipboardList,
  Cpu,
  Gauge,
  KeyRound,
  Route,
  Server,
  Timer,
  UserRound,
  Zap,
} from '@lucide/vue'

import { getUsageOptions, getUsageOverview } from '@/features/usage/api/usageApi'
import { getCurrentUserQuota } from '@/features/users/api/usersApi'
import ChartPanel from '@/features/usage/components/ChartPanel.vue'
import UsageDonutChart from '@/features/usage/components/UsageDonutChart.vue'
import UsageDashboardSkeleton from '@/features/usage/components/UsageDashboardSkeleton.vue'
import UsageTrendChart from '@/features/usage/components/UsageTrendChart.vue'
import type {
  DistributionItem,
  RankingItem,
  TrendPoint,
  UsageDistributionsResponse,
  UsageFilters,
  UsageOptionsResponse,
  UsageOverviewResponse,
  UsageRankingsResponse,
  UsageSummary,
  UserQuotaStatus,
} from '@/shared/types/api'
import {
  BEIJING_TIME_ZONE,
  beijingDayRange,
  formatCompact,
  formatDateTime,
  formatInteger,
  formatLocalDateTimeParam,
  formatUsd,
} from '@/shared/utils/format'
import { useI18n } from '@/shared/i18n'
import DateTimeRangePicker from '@/shared/ui/DateTimeRangePicker.vue'
import FilterCombobox from '@/shared/ui/FilterCombobox.vue'

type FailedFilter = 'all' | 'success' | 'failed'
type QuickRangeKey = 'today' | 'yesterday' | 'last24h' | 'last3d' | 'last7d' | 'last30d'
type UsageScope = 'admin' | 'account' | 'shared'
type RankingSort = 'tokens' | 'cost'
type TrendSeriesKey = 'requests' | 'tokens' | 'failed'

interface RefreshOptions {
  silent?: boolean
}

interface Props {
  scope: UsageScope
}

interface MetricCardConfig {
  key: string
  label: string
  value: string
  icon: Component
  tone: string
  footnote: string
}

interface DistributionLegendItem {
  key: string
  label: string
  value: number
  recordsText: string
  percentText: string
  colorIndex: number
}

interface TokenBreakdownItem {
  key: string
  label: string
  value: number
  valueText: string
  percentText: string
  colorIndex: number
}

interface HourActivityItem {
  hour: number
  label: string
  records: number
  tokens: number
  recordTitle: string
  tokenTitle: string
  recordStyle: Record<string, string>
  tokenStyle: Record<string, string>
}

const AUTO_REFRESH_INTERVAL_MS = 10_000
const RATE_WINDOW_MINUTES = 10
const HOUR_MS = 60 * 60 * 1000
const DAY_MS = 24 * HOUR_MS
const DISTRIBUTION_CHART_COLORS = [
  { token: '--cpa-chart-1', fallback: '#2563eb' },
  { token: '--cpa-chart-2', fallback: '#06b6d4' },
  { token: '--cpa-chart-3', fallback: '#8b5cf6' },
  { token: '--cpa-chart-4', fallback: '#f59e0b' },
  { token: '--cpa-chart-5', fallback: '#16a34a' },
] as const

const route = useRoute()
const router = useRouter()
const message = toast
const props = defineProps<Props>()
const { currentLanguage, errorText, t } = useI18n()
const isLoading = ref(false)
const hasLoadedDashboard = ref(false)
const isAutoRefreshing = ref(false)
const autoRefreshError = ref<string | null>(null)
const auxiliaryError = ref<string | null>(null)
const lastRefreshedAt = ref<Date | null>(null)
const filtersExpanded = ref(false)
const summary = ref<UsageSummary | null>(null)
const quotaStatus = ref<UserQuotaStatus | null>(null)
const realtimeSummary = ref<UsageSummary | null>(null)
const todayTrends = ref<TrendPoint[]>([])
const failedSummary = ref<UsageSummary | null>(null)
const failedTrends = ref<TrendPoint[]>([])
const trends = ref<TrendPoint[]>([])
const userRanking = ref<RankingItem[]>([])
const modelRanking = ref<RankingItem[]>([])
const primaryRankingSort = ref<RankingSort>('cost')
const modelRankingSort = ref<RankingSort>('cost')
const trendSeriesVisibility = reactive<Record<TrendSeriesKey, boolean>>({
  requests: true,
  tokens: true,
  failed: true,
})
const distributions = ref<UsageDistributionsResponse>({ providers: [], models: [], endpoints: [] })
const failedEndpointDistribution = ref<DistributionItem[]>([])
const options = ref<UsageOptionsResponse>({
  users: [],
  api_key_descriptions: [],
  providers: [],
  models: [],
  sources: [],
  endpoints: [],
})

function normalizeUsageOptions(nextOptions: UsageOptionsResponse): UsageOptionsResponse {
  return {
    users: nextOptions.users ?? [],
    api_key_descriptions: nextOptions.api_key_descriptions ?? [],
    providers: nextOptions.providers ?? [],
    models: nextOptions.models ?? [],
    sources: nextOptions.sources ?? [],
    endpoints: nextOptions.endpoints ?? [],
  }
}

function normalizeUsageDistributions(
  nextDistributions: Partial<UsageDistributionsResponse> | undefined,
): UsageDistributionsResponse {
  return {
    providers: nextDistributions?.providers ?? [],
    models: nextDistributions?.models ?? [],
    endpoints: nextDistributions?.endpoints ?? [],
  }
}

function emptyRanking(groupBy: UsageRankingsResponse['group_by']): UsageRankingsResponse {
  return { group_by: groupBy, items: [] }
}

function descriptionRanking(overview: UsageOverviewResponse): UsageRankingsResponse {
  return (
    overview.api_key_description_ranking ??
    overview.api_key_ranking ??
    emptyRanking('api_key_description')
  )
}

function initialRange(): [number, number] | null {
  const startQuery = typeof route.query.start === 'string' ? route.query.start : ''
  const endQuery = typeof route.query.end === 'string' ? route.query.end : ''
  if (startQuery && endQuery) {
    return [new Date(startQuery).getTime(), new Date(endQuery).getTime()]
  }
  return null
}

function todayRange(): [number, number] {
  return beijingDayRange()
}

function yesterdayRange(): [number, number] {
  return beijingDayRange(-1)
}

function rollingRange(durationMs: number): [number, number] {
  const end = Date.now()
  return [end - durationMs, end]
}

function isTodayRange(range: [number, number] | null): boolean {
  if (!range) {
    return false
  }
  const [todayStart, tomorrowStart] = todayRange()
  return range[0] === todayStart && range[1] === tomorrowStart
}

function isYesterdayRange(range: [number, number] | null): boolean {
  if (!range) {
    return false
  }
  const [yesterdayStart, todayStart] = yesterdayRange()
  return range[0] === yesterdayStart && range[1] === todayStart
}

function buildQuickRange(key: QuickRangeKey): [number, number] {
  switch (key) {
    case 'today':
      return todayRange()
    case 'yesterday':
      return yesterdayRange()
    case 'last24h':
      return rollingRange(24 * HOUR_MS)
    case 'last3d':
      return rollingRange(3 * DAY_MS)
    case 'last7d':
      return rollingRange(7 * DAY_MS)
    case 'last30d':
      return rollingRange(30 * DAY_MS)
  }
}

function isQuickRangeKey(value: unknown): value is QuickRangeKey {
  return typeof value === 'string' && quickRangeOptions.value.some((option) => option.key === value)
}

function quickRangeFromQuery(): QuickRangeKey | null {
  const value = route.query.quick_range
  return isQuickRangeKey(value) ? value : null
}

function inferQuickRangeFromRange(range: [number, number] | null): QuickRangeKey | null {
  if (!range) {
    return null
  }
  if (isTodayRange(range)) {
    return 'today'
  }
  if (isYesterdayRange(range)) {
    return 'yesterday'
  }

  const duration = range[1] - range[0]
  const endDrift = Math.abs(Date.now() - range[1])
  const durationToleranceMs = 2 * 60 * 1000
  const refreshToleranceMs = 10 * 60 * 1000
  const rollingRanges: Array<{ key: QuickRangeKey; durationMs: number }> = [
    { key: 'last24h', durationMs: 24 * HOUR_MS },
    { key: 'last3d', durationMs: 3 * DAY_MS },
    { key: 'last7d', durationMs: 7 * DAY_MS },
    { key: 'last30d', durationMs: 30 * DAY_MS },
  ]

  if (endDrift > refreshToleranceMs) {
    return null
  }

  return (
    rollingRanges.find((item) => Math.abs(duration - item.durationMs) <= durationToleranceMs)
      ?.key ?? null
  )
}

function failedFromQuery(): FailedFilter {
  if (route.query.failed === 'true') {
    return 'failed'
  }
  if (route.query.failed === 'false') {
    return 'success'
  }
  return 'all'
}

function numberFromQuery(value: unknown): number | null {
  if (typeof value !== 'string' || !value) {
    return null
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

const quickRangeOptions = computed<Array<{ key: QuickRangeKey; label: string }>>(() => [
  { key: 'today', label: t('今日', 'Today') },
  { key: 'yesterday', label: t('昨日', 'Yesterday') },
  { key: 'last24h', label: t('近 24 小时', 'Last 24 hours') },
  { key: 'last3d', label: t('近 3 日', 'Last 3 days') },
  { key: 'last7d', label: t('近 7 日', 'Last 7 days') },
  { key: 'last30d', label: t('近 30 日', 'Last 30 days') },
])

const initialQuickRange = quickRangeFromQuery()
const initialDateRange = initialRange()
const inferredQuickRange = initialQuickRange ?? inferQuickRangeFromRange(initialDateRange)
const dateRange = ref<[number, number] | null>(
  inferredQuickRange ? buildQuickRange(inferredQuickRange) : initialDateRange,
)
const activeQuickRange = ref<QuickRangeKey | null>(
  inferredQuickRange ?? (initialDateRange === null ? 'today' : null),
)
const filterForm = reactive({
  user_id: numberFromQuery(route.query.user_id),
  api_key_description:
    typeof route.query.api_key_description === 'string' ? route.query.api_key_description : null,
  provider: typeof route.query.provider === 'string' ? route.query.provider : null,
  model: typeof route.query.model === 'string' ? route.query.model : null,
  endpoint: typeof route.query.endpoint === 'string' ? route.query.endpoint : null,
  failed: failedFromQuery(),
})

const failedFilterOptions = computed(() => [
  { label: t('成功', 'Success'), value: 'success' },
  { label: t('失败', 'Failed'), value: 'failed' },
])

function apiKeyFilterLabel(item: UsageOptionsResponse['api_key_descriptions'][number]): string {
  return item.label?.trim() || item.key
}

const selectOptions = computed(() => ({
  users: options.value.users
    .filter((item) => item.user_id !== null)
    .map((item) => ({ label: item.label, value: item.user_id as number })),
  apiKeyDescriptions: options.value.api_key_descriptions.map((item) => ({
    label: apiKeyFilterLabel(item),
    value: item.key,
  })),
  providers: options.value.providers.map((item) => ({ label: item, value: item })),
  models: options.value.models.map((item) => ({ label: item, value: item })),
  endpoints: options.value.endpoints.map((item) => ({ label: item, value: item })),
}))

const isAccountScope = computed(() => props.scope === 'account')
const canOpenRecords = computed(() => props.scope !== 'shared')
const pageTitle = computed(() =>
  isAccountScope.value ? t('我的用量', 'My usage') : t('用量分析', 'Usage analytics'),
)
const rankingTitle = computed(() =>
  isAccountScope.value ? t('KEY 排行', 'Key ranking') : t('用户排行', 'User ranking'),
)

const filterComboboxes = computed<UsageFilterCombobox[]>(() => {
  const items: UsageFilterCombobox[] = [
    {
      key: 'provider',
      icon: Server,
      value: filterForm.provider,
      options: selectOptions.value.providers,
      placeholder: t('服务商', 'Provider'),
      searchPlaceholder: t('搜索服务商', 'Search providers'),
      emptyText: t('没有匹配的服务商', 'No matching providers'),
      onChange: handleProviderChange,
    },
    {
      key: 'model',
      icon: Cpu,
      value: filterForm.model,
      options: selectOptions.value.models,
      placeholder: t('模型', 'Model'),
      searchPlaceholder: t('搜索模型', 'Search models'),
      emptyText: t('没有匹配的模型', 'No matching models'),
      onChange: handleModelChange,
    },
    {
      key: 'endpoint',
      icon: Route,
      value: filterForm.endpoint,
      options: selectOptions.value.endpoints,
      placeholder: t('接口', 'Endpoint'),
      searchPlaceholder: t('搜索接口', 'Search endpoints'),
      emptyText: t('没有匹配的接口', 'No matching endpoints'),
      onChange: handleEndpointChange,
    },
  ]

  if (isAccountScope.value) {
    items.unshift({
      key: 'api-key',
      icon: KeyRound,
      value: filterForm.api_key_description,
      options: selectOptions.value.apiKeyDescriptions,
      placeholder: t('KEY 描述', 'Key description'),
      searchPlaceholder: t('搜索 KEY 描述', 'Search key descriptions'),
      emptyText: t('没有匹配的 KEY 描述', 'No matching key descriptions'),
      onChange: handleApiKeyChange,
    })
  } else {
    items.unshift({
      key: 'user',
      icon: UserRound,
      value: filterForm.user_id,
      options: selectOptions.value.users,
      placeholder: t('用户昵称', 'User nickname'),
      searchPlaceholder: t('搜索用户', 'Search users'),
      emptyText: t('没有匹配的用户', 'No matching users'),
      onChange: handleUserChange,
    })
  }

  return items
})

const rankingSortOptions = computed(() => [
  { label: t('按 Token', 'By tokens'), value: 'tokens' },
  { label: t('按费用', 'By cost'), value: 'cost' },
])

const refreshStatusText = computed(() => {
  const lastRefreshTime = lastRefreshedAt.value
  if (!lastRefreshTime) {
    return autoRefreshError.value
      ? t('自动刷新异常 · 尚无成功同步', 'Auto refresh error · no successful sync yet')
      : t('每 10 秒自动刷新 · 等待首次同步', 'Auto refresh every 10 seconds · waiting for first sync')
  }
  const lastRefreshText = formatDateTime(lastRefreshTime)
  if (autoRefreshError.value) {
    return t(`自动刷新异常 · 最近成功 ${lastRefreshText}`, `Auto refresh error · last success ${lastRefreshText}`)
  }
  if (auxiliaryError.value) {
    return t(`已同步 ${lastRefreshText} · 辅助指标降级`, `Synced ${lastRefreshText} · auxiliary metrics degraded`)
  }
  return t(`每 10 秒自动刷新 · 最近 ${lastRefreshText}`, `Auto refresh every 10 seconds · latest ${lastRefreshText}`)
})

const dashboardRangeLabel = computed(() => {
  const activeRange = quickRangeOptions.value.find((option) => option.key === activeQuickRange.value)
  if (activeRange) {
    return activeRange.label
  }
  const currentSummary = summary.value
  if (currentSummary) {
    return `${formatDateTime(currentSummary.start)} - ${formatDateTime(currentSummary.end)}`
  }
  const range = dateRange.value
  if (!range) {
    return t('当前筛选', 'Current filters')
  }
  return `${formatDateTime(range[0])} - ${formatDateTime(range[1])}`
})

const rateRangeLabel = computed(() => t('近 10 分钟', 'Last 10 minutes'))

function buildFilters(): UsageFilters {
  const failed =
    filterForm.failed === 'all' ? undefined : filterForm.failed === 'failed' ? true : false
  return {
    scope: props.scope,
    start: dateRange.value ? formatLocalDateTimeParam(dateRange.value[0]) : undefined,
    end: dateRange.value ? formatLocalDateTimeParam(dateRange.value[1]) : undefined,
    user_id: isAccountScope.value ? undefined : (filterForm.user_id ?? undefined),
    api_key_description: isAccountScope.value ? (filterForm.api_key_description ?? undefined) : undefined,
    provider: filterForm.provider ?? undefined,
    model: filterForm.model ?? undefined,
    endpoint: filterForm.endpoint ?? undefined,
    failed,
  }
}

function filtersToQuery(
  filters: UsageFilters,
  quickRangeKey: QuickRangeKey | null = null,
): Record<string, string> {
  const query: Record<string, string> = {}
  Object.entries(filters).forEach(([key, value]) => {
    if (key !== 'scope' && value !== undefined && value !== '') {
      query[key] = String(value)
    }
  })
  if (quickRangeKey) {
    query.quick_range = quickRangeKey
  }
  return query
}

function normalizeRangeValue(value: unknown): [number, number] | null {
  if (
    Array.isArray(value) &&
    value.length === 2 &&
    typeof value[0] === 'number' &&
    typeof value[1] === 'number'
  ) {
    return [value[0], value[1]]
  }
  return null
}

function normalizeSelectValue(value: unknown): string | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  return String(value)
}

function refreshAfterFilterChange() {
  void refresh()
}

function handleCustomRangeChange(value: unknown) {
  dateRange.value = normalizeRangeValue(value)
  activeQuickRange.value = null
  refreshAfterFilterChange()
}

function handleApiKeyChange(value: unknown) {
  filterForm.api_key_description = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleUserChange(value: unknown) {
  filterForm.user_id = typeof value === 'number' ? value : null
  refreshAfterFilterChange()
}

function handleProviderChange(value: unknown) {
  filterForm.provider = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleModelChange(value: unknown) {
  filterForm.model = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleEndpointChange(value: unknown) {
  filterForm.endpoint = normalizeSelectValue(value)
  refreshAfterFilterChange()
}

function handleFailedChange(value: unknown) {
  filterForm.failed = value === 'success' || value === 'failed' ? value : 'all'
  refreshAfterFilterChange()
}

async function applyQuickRange(key: QuickRangeKey) {
  activeQuickRange.value = key
  dateRange.value = buildQuickRange(key)
  await refresh()
}

function handleQuickRangeChange(value: unknown) {
  if (isQuickRangeKey(value)) {
    void applyQuickRange(value)
  }
}

let queuedRefresh: RefreshOptions | null = null

function queueRefresh(options: RefreshOptions) {
  if (options.silent) {
    return
  }
  queuedRefresh = { silent: false }
}

async function refresh({ silent = false }: RefreshOptions = {}) {
  if (isLoading.value || isAutoRefreshing.value) {
    queueRefresh({ silent })
    return
  }
  if (activeQuickRange.value) {
    dateRange.value = buildQuickRange(activeQuickRange.value)
  }
  if (silent) {
    isAutoRefreshing.value = true
  } else {
    isLoading.value = true
  }
  try {
    const filters = buildFilters()
    const usedServerDefaultRange = filters.start === undefined && filters.end === undefined
    const optionsRequest = silent ? Promise.resolve(null) : getUsageOptions(filters)
    const quotaRequest = isAccountScope.value ? getCurrentUserQuota() : Promise.resolve(null)
    const [overviewResult, optionsResult, quotaResult] = await Promise.allSettled([
      getUsageOverview(filters, false),
      optionsRequest,
      quotaRequest,
    ] as const)

    if (overviewResult.status === 'rejected') {
      throw overviewResult.reason
    }

    const overview = overviewResult.value
    summary.value = overview.summary
    if (usedServerDefaultRange) {
      dateRange.value = [
        new Date(overview.summary.start).getTime(),
        new Date(overview.summary.end).getTime(),
      ]
    }
    trends.value = overview.trends
    userRanking.value = isAccountScope.value
      ? descriptionRanking(overview).items
      : (overview.user_ranking ?? emptyRanking('user')).items
    modelRanking.value = (overview.model_ranking ?? emptyRanking('model')).items
    distributions.value = normalizeUsageDistributions(overview.distributions)
    todayTrends.value = overview.today_trends ?? []
    failedSummary.value = overview.failed_summary ?? null
    failedTrends.value = overview.failed_trends ?? []
    failedEndpointDistribution.value = overview.failed_endpoint_distribution ?? []
    realtimeSummary.value = overview.realtime_summary ?? null
    if (!silent && optionsResult.status === 'fulfilled' && optionsResult.value) {
      options.value = normalizeUsageOptions(optionsResult.value)
    }

    if (quotaResult.status === 'fulfilled') {
      quotaStatus.value = quotaResult.value
    } else if (isAccountScope.value) {
      quotaStatus.value = null
    }

    auxiliaryError.value =
      optionsResult.status === 'rejected' ||
      quotaResult.status === 'rejected'
        ? t('部分辅助指标加载失败', 'Some auxiliary metrics failed to load')
        : null

    void router.replace({
      query: filtersToQuery(
        usedServerDefaultRange
          ? { ...filters, start: overview.summary.start, end: overview.summary.end }
          : filters,
        activeQuickRange.value,
      ),
    })
    autoRefreshError.value = null
    lastRefreshedAt.value = new Date()
  } catch (error) {
    const errorMessage = errorText(error, '加载用量分析失败', 'Failed to load usage analytics')
    if (silent) {
      autoRefreshError.value = errorMessage
    } else {
      message.error(errorMessage)
    }
  } finally {
    if (silent) {
      isAutoRefreshing.value = false
    } else {
      hasLoadedDashboard.value = true
      isLoading.value = false
    }
    const nextRefresh = queuedRefresh
    queuedRefresh = null
    if (nextRefresh) {
      void refresh(nextRefresh)
    }
  }
}

function goRecords(extra: UsageFilters = {}) {
  if (!canOpenRecords.value) {
    return
  }
  const filters = { ...buildFilters(), ...extra }
  void router.push({
    name: isAccountScope.value ? 'account-records' : 'admin-records',
    query: filtersToQuery(filters),
  })
}

function rankingFilters(row: RankingItem): UsageFilters {
  if (!isAccountScope.value && row.user_id) {
    return { user_id: row.user_id }
  }
  if (row.api_key_description) {
    return { api_key_description: row.api_key_description }
  }
  return {}
}

function modelFilters(row: RankingItem): UsageFilters {
  const [provider, model] = row.key.split('::')
  const filters: UsageFilters = {}
  if (provider) {
    filters.provider = provider
  }
  if (model) {
    filters.model = model
  }
  return filters
}

function distributionMarkerStyle(index: number): Record<string, string> {
  const color =
    DISTRIBUTION_CHART_COLORS[index % DISTRIBUTION_CHART_COLORS.length] ??
    DISTRIBUTION_CHART_COLORS[0]
  return {
    '--distribution-color': `var(${color.token}, ${color.fallback})`,
  }
}

function distributionLegendItems(items: DistributionItem[]): DistributionLegendItem[] {
  const totalRecords = items.reduce((sum, item) => sum + item.records, 0)

  return items.map((item, index) => ({
    key: item.key,
    label: item.label,
    value: item.records,
    recordsText: formatCompact(item.records),
    percentText: totalRecords > 0 ? `${Math.round((item.records / totalRecords) * 100)}%` : '0%',
    colorIndex: index,
  }))
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    style: 'percent',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value)
}

function formatRate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0'
  }
  if (value >= 1000) {
    return formatCompact(value)
  }
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: value < 10 ? 1 : 0,
  }).format(value)
}

function formatLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value <= 0) {
    return '-'
  }
  return `${formatInteger(Math.round(value))} ms`
}

const successRate = computed(() => {
  const currentSummary = summary.value
  if (!currentSummary || currentSummary.total_records === 0) {
    return 0
  }
  return currentSummary.success_records / currentSummary.total_records
})

const failedRate = computed(() => {
  const currentSummary = summary.value
  if (!currentSummary || currentSummary.total_records === 0) {
    return 0
  }
  return currentSummary.failed_records / currentSummary.total_records
})

const rateSummary = computed(() => realtimeSummary.value)

// 缓存命中率 = provider-aware cache-hit tokens / aggregated input tokens,
// capped at 100% (contract pinned in the task #30 thread).
const cacheHitRate = computed(() => {
  const currentSummary = summary.value
  const inputTokens = currentSummary?.input_tokens ?? 0
  const hitTokens = currentSummary?.cache_hit_tokens ?? 0
  if (inputTokens <= 0 || hitTokens <= 0) {
    return 0
  }
  return Math.min(1, hitTokens / inputTokens)
})

const requestsPerMinute = computed(() => {
  const currentSummary = rateSummary.value
  return (currentSummary?.total_records ?? 0) / RATE_WINDOW_MINUTES
})

const tokensPerMinute = computed(() => {
  const currentSummary = rateSummary.value
  return (currentSummary?.total_tokens ?? 0) / RATE_WINDOW_MINUTES
})

function quotaValueText(quota: UserQuotaStatus | null): string {
  if (!quota) {
    return t('加载中', 'Loading')
  }
  if (quota.unlimited) {
    return t('可用余额 无限制', 'Available balance unlimited')
  }
  const total = quota.available_usd
  return t(`可用余额 ${formatUsd(total)}`, `Available balance ${formatUsd(total)}`)
}

interface UsageFilterOption {
  label: string
  value: string | number
}

interface UsageFilterCombobox {
  key: string
  icon: Component
  value: string | number | null
  options: UsageFilterOption[]
  placeholder: string
  searchPlaceholder: string
  emptyText: string
  onChange: (value: unknown) => void
}

function quotaStatusTitle(quota: UserQuotaStatus | null): string {
  if (!quota) {
    return t('额度加载中', 'Quota loading')
  }
  const daily = quota.daily_quota_usd === null ? t('不限制', 'Unlimited') : formatUsd(quota.daily_remaining_usd ?? 0)
  const weekly = quota.weekly_quota_usd === null ? t('不限制', 'Unlimited') : formatUsd(quota.weekly_remaining_usd ?? 0)
  const balancesText = t(
    `日配额 ${daily} / 周配额 ${weekly} / 额度卡 ${formatUsd(quota.cards_remaining_usd ?? 0)}`,
    `Daily quota ${daily} / Weekly quota ${weekly} / Cards ${formatUsd(quota.cards_remaining_usd ?? 0)}`,
  )
  const notes: string[] = []
  if (quota.sync_error) {
    notes.push(t('Key 同步异常', 'Key sync error'))
  }
  if (quota.unpriced_records > 0) {
    notes.push(t(`历史未定价 ${formatInteger(quota.unpriced_records)} 条`, `${formatInteger(quota.unpriced_records)} historical unpriced records`))
  }
  if (quota.paused) {
    notes.push(t('Key 已因余额暂停', 'Key paused due to balance'))
  }
  return notes.length > 0 ? `${balancesText} · ${notes.join(' · ')}` : balancesText
}

const metricCards = computed<MetricCardConfig[]>(() => {
  const currentSummary = summary.value
  return [
    {
      key: 'requests',
      label: t('请求数', 'Requests'),
      value: formatInteger(currentSummary?.total_records ?? 0),
      icon: ClipboardList,
      tone: 'blue',
      footnote: t(
        `失败数 ${formatInteger(currentSummary?.failed_records ?? 0)}  成功率 ${formatPercent(successRate.value)}`,
        `Failed ${formatInteger(currentSummary?.failed_records ?? 0)}  success rate ${formatPercent(successRate.value)}`,
      ),
    },
    {
      key: 'cache_hit_rate',
      label: t('缓存命中率', 'Cache hit rate'),
      value: formatPercent(cacheHitRate.value),
      icon: Zap,
      tone: 'purple',
      footnote: t(
        `缓存 Token ${formatCompact(currentSummary?.cache_hit_tokens ?? 0)}`,
        `Cached tokens ${formatCompact(currentSummary?.cache_hit_tokens ?? 0)}`,
      ),
    },
    {
      key: 'rpm',
      label: 'RPM',
      value: formatRate(requestsPerMinute.value),
      icon: Gauge,
      tone: 'orange',
      footnote: rateRangeLabel.value,
    },
    {
      key: 'tpm',
      label: 'TPM',
      value: formatRate(tokensPerMinute.value),
      icon: Gauge,
      tone: 'purple',
      footnote: rateRangeLabel.value,
    },
    {
      key: 'average_ttft',
      label: t('平均首字耗时', 'Avg TTFT'),
      value: formatLatency(currentSummary?.average_ttft_ms ?? null),
      icon: Timer,
      tone: 'primary',
      footnote: t(
        `零 Token ${formatInteger(currentSummary?.zero_token_records ?? 0)} 次`,
        `Zero-token ${formatInteger(currentSummary?.zero_token_records ?? 0)} requests`,
      ),
    },
    {
      key: 'cost',
      label: t('费用', 'Cost'),
      value: formatUsd(currentSummary?.estimated_cost_usd ?? 0),
      icon: CircleDollarSign,
      tone: 'green',
      footnote: t(
        `总 Token ${formatCompact(currentSummary?.total_tokens ?? 0)}`,
        `Total tokens ${formatCompact(currentSummary?.total_tokens ?? 0)}`,
      ),
    },
  ]
})

function formatTrendBucket(value: string): string {
  const formatted = formatDateTime(value)
  return formatted === '-' ? value : formatted
}

const trendLegendItems = computed<Array<{ key: TrendSeriesKey; label: string; color: string }>>(() => [
  { key: 'requests', label: t('请求数', 'Requests'), color: 'var(--chart-2)' },
  { key: 'tokens', label: 'Token', color: 'var(--chart-4)' },
  { key: 'failed', label: t('失败请求', 'Failed requests'), color: 'var(--cpa-danger)' },
])
const trendChartKey = computed(() => [
  trendSeriesVisibility.requests ? 'requests' : '',
  trendSeriesVisibility.tokens ? 'tokens' : '',
  trendSeriesVisibility.failed ? 'failed' : '',
].filter(Boolean).join('-') || 'empty')

function toggleTrendSeries(key: TrendSeriesKey) {
  trendSeriesVisibility[key] = !trendSeriesVisibility[key]
}

const tokenBreakdownItems = computed<TokenBreakdownItem[]>(() => {
  const currentSummary = summary.value
  const cacheHitTokens = currentSummary?.cache_hit_tokens ?? 0
  const uncachedInputTokens = Math.max(0, (currentSummary?.input_tokens ?? 0) - cacheHitTokens)
  const values = [
    { key: 'input', label: t('输入 Token', 'Input tokens'), value: uncachedInputTokens },
    { key: 'output', label: t('输出 Token', 'Output tokens'), value: currentSummary?.output_tokens ?? 0 },
    { key: 'cached', label: t('缓存 Token', 'Cached tokens'), value: cacheHitTokens },
    { key: 'reasoning', label: t('推理 Token', 'Reasoning tokens'), value: currentSummary?.reasoning_tokens ?? 0 },
  ]
  const total = values.reduce((sum, item) => sum + item.value, 0)
  return values.map((item, index) => ({
    ...item,
    valueText: formatCompact(item.value),
    percentText: total > 0 ? `${Math.round((item.value / total) * 100)}%` : '0%',
    colorIndex: index,
  }))
})

const tokenBreakdownTotal = computed(() =>
  tokenBreakdownItems.value.reduce((sum, item) => sum + item.value, 0),
)

function hourFromBucket(bucket: string): number | null {
  const localHourMatch = bucket.match(/\s(\d{2})(?::\d{2})?$/)
  if (localHourMatch) {
    const hour = Number(localHourMatch[1])
    return hour >= 0 && hour <= 23 ? hour : null
  }
  const parsed = new Date(bucket)
  if (Number.isNaN(parsed.getTime())) {
    return null
  }
  return Number(
    new Intl.DateTimeFormat('en-US', {
      timeZone: BEIJING_TIME_ZONE,
      hour: '2-digit',
      hour12: false,
    }).format(parsed),
  )
}

const hourActivityItems = computed<HourActivityItem[]>(() => {
  const byHour = new Map<number, { records: number; tokens: number }>()
  todayTrends.value.forEach((item) => {
    const hour = hourFromBucket(item.bucket)
    if (hour === null) {
      return
    }
    const current = byHour.get(hour) ?? { records: 0, tokens: 0 }
    current.records += item.records
    current.tokens += item.total_tokens
    byHour.set(hour, current)
  })
  const maxRecords = Math.max(1, ...[...byHour.values()].map((item) => item.records))
  const maxTokens = Math.max(1, ...[...byHour.values()].map((item) => item.tokens))
  return Array.from({ length: 24 }, (_, hour) => {
    const value = byHour.get(hour) ?? { records: 0, tokens: 0 }
    const recordIntensity = value.records === 0 ? 0 : Math.max(0.12, value.records / maxRecords)
    const tokenIntensity = value.tokens === 0 ? 0 : Math.max(0.12, value.tokens / maxTokens)
    const hourLabel = String(hour).padStart(2, '0')
    return {
      hour,
      label: hourLabel,
      records: value.records,
      tokens: value.tokens,
      recordTitle: t(
        `${hourLabel}:00 · ${formatInteger(value.records)} 次请求`,
        `${hourLabel}:00 · ${formatInteger(value.records)} requests`,
      ),
      tokenTitle: `${hourLabel}:00 · ${formatCompact(value.tokens)} Token`,
      recordStyle: { '--heat-intensity': recordIntensity.toFixed(3) },
      tokenStyle: { '--heat-intensity': tokenIntensity.toFixed(3) },
    }
  })
})

const todayRecordTotal = computed(() =>
  hourActivityItems.value.reduce((sum, item) => sum + item.records, 0),
)
const todayTokenTotal = computed(() =>
  hourActivityItems.value.reduce((sum, item) => sum + item.tokens, 0),
)

function rankingMetricValue(item: RankingItem, sort: RankingSort): number {
  return sort === 'cost' ? item.estimated_cost_usd : item.total_tokens
}

function sortedRankingRows(items: RankingItem[], sort: RankingSort): RankingItem[] {
  const secondarySort: RankingSort = sort === 'tokens' ? 'cost' : 'tokens'
  return [...items]
    .sort((left, right) => {
      const metricDifference = rankingMetricValue(right, sort) - rankingMetricValue(left, sort)
      if (metricDifference !== 0) {
        return metricDifference
      }
      const secondaryDifference =
        rankingMetricValue(right, secondarySort) - rankingMetricValue(left, secondarySort)
      if (secondaryDifference !== 0) {
        return secondaryDifference
      }
      if (left.records !== right.records) {
        return right.records - left.records
      }
      return left.label.localeCompare(right.label)
    })
    .slice(0, 5)
}

const primaryRankingRows = computed(() =>
  sortedRankingRows(userRanking.value, primaryRankingSort.value),
)
const modelRankingRows = computed(() =>
  sortedRankingRows(modelRanking.value, modelRankingSort.value),
)
const maxPrimaryRankingValue = computed(() =>
  Math.max(
    0,
    ...primaryRankingRows.value.map((item) =>
      rankingMetricValue(item, primaryRankingSort.value),
    ),
  ),
)
const maxModelRankingValue = computed(() =>
  Math.max(
    0,
    ...modelRankingRows.value.map((item) => rankingMetricValue(item, modelRankingSort.value)),
  ),
)

function rankingPrimaryText(item: RankingItem, sort: RankingSort): string {
  return sort === 'cost' ? formatUsd(item.estimated_cost_usd) : formatCompact(item.total_tokens)
}

function rankingSecondaryText(item: RankingItem, sort: RankingSort): string {
  return sort === 'cost' ? `${formatCompact(item.total_tokens)} Token` : formatUsd(item.estimated_cost_usd)
}

function normalizeRankingSort(value: unknown): RankingSort {
  return value === 'cost' ? 'cost' : 'tokens'
}

function handlePrimaryRankingSortChange(value: unknown) {
  primaryRankingSort.value = normalizeRankingSort(value)
}

function handleModelRankingSortChange(value: unknown) {
  modelRankingSort.value = normalizeRankingSort(value)
}

function rankingBarStyle(value: number, maxValue: number): Record<string, string> {
  const percent = maxValue > 0 ? Math.max(4, Math.round((value / maxValue) * 100)) : 0
  return { '--ranking-width': `${percent}%` }
}

const topFailedEndpoint = computed(() => failedEndpointDistribution.value[0] ?? null)
const recentFailedRows = computed(() =>
  failedTrends.value
    .filter((item) => item.records > 0)
    .slice(-4)
    .reverse(),
)

const anomalyStats = computed(() => [
  {
    key: 'failed_rate',
    label: t('失败率', 'Failure rate'),
    value: formatPercent(failedRate.value),
    tone: failedRate.value > 0 ? 'danger' : 'success',
  },
  {
    key: 'failed_records',
    label: t('失败请求', 'Failed requests'),
    value: formatInteger(summary.value?.failed_records ?? failedSummary.value?.total_records ?? 0),
    tone: 'danger',
  },
  {
    key: 'unpriced',
    label: t('未计价', 'Unpriced'),
    value: formatInteger(summary.value?.unpriced_records ?? 0),
    tone: (summary.value?.unpriced_records ?? 0) > 0 ? 'warning' : 'success',
  },
])

const providerDistributionLegend = computed(() =>
  distributionLegendItems(distributions.value.providers),
)
const endpointDistributionLegend = computed(() =>
  distributionLegendItems(distributions.value.endpoints),
)

let autoRefreshTimer: number | undefined

function handleVisibilityChange() {
  if (!document.hidden) {
    void refresh({ silent: true })
  }
}

onMounted(() => {
  void refresh()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  autoRefreshTimer = window.setInterval(() => {
    if (!document.hidden) {
      void refresh({ silent: true })
    }
  }, AUTO_REFRESH_INTERVAL_MS)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  if (autoRefreshTimer !== undefined) {
    window.clearInterval(autoRefreshTimer)
  }
})
</script>

<template>
  <section class="page usage-dashboard-page" :aria-busy="!hasLoadedDashboard || isLoading">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ pageTitle }}</h1>
      <div class="header-actions">
        <span
          v-if="isAccountScope"
          class="quota-status-pill"
          data-testid="usage-balance"
          :class="{
            'is-paused': quotaStatus?.paused,
            'is-warning': (quotaStatus?.unpriced_records ?? 0) > 0 || !!quotaStatus?.sync_error,
          }"
          :title="quotaStatusTitle(quotaStatus)"
          :aria-label="quotaStatusTitle(quotaStatus)"
        >
          <CircleDollarSign :size="14" :stroke-width="2.2" aria-hidden="true" />
          <strong>{{ quotaValueText(quotaStatus) }}</strong>
        </span>
        <span class="refresh-status" :class="{ 'is-error': autoRefreshError }">
          {{ refreshStatusText }}
        </span>
        <Button v-if="canOpenRecords" variant="outline" @click="goRecords()">
          {{ t('明细', 'Records') }}
        </Button>
      </div>
    </div>

    <section class="panel filter-panel" :class="{ 'is-expanded': filtersExpanded }">
      <div class="filter-summary">
        <div>
          <strong>{{ t('筛选', 'Filters') }}</strong>
          <span>{{ dashboardRangeLabel }}</span>
        </div>
        <Button class="filter-toggle" variant="outline" size="sm" @click="filtersExpanded = !filtersExpanded">
          {{ filtersExpanded ? t('收起', 'Collapse') : t('展开', 'Expand') }}
        </Button>
      </div>
      <div class="panel-inner filter-toolbar">
        <div class="time-row">
          <Tabs
            class="quick-ranges"
            :model-value="activeQuickRange ?? undefined"
            @update:model-value="handleQuickRangeChange"
          >
            <TabsList
              class="quick-range-options grid h-8 w-full grid-cols-6"
              :aria-label="t('快捷时间范围', 'Quick time ranges')"
            >
              <TabsTrigger
                v-for="option in quickRangeOptions"
                :key="option.key"
                :value="option.key"
                class="min-w-0 justify-center px-2 text-center"
                :aria-label="option.label"
              >
                {{ option.label }}
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <DateTimeRangePicker
            :model-value="dateRange"
            class="range-picker"
            clearable
            @update:model-value="handleCustomRangeChange"
          />
        </div>
        <div class="field-row" :class="{ 'is-account-scope': isAccountScope }">
          <FilterCombobox
            v-for="filter in filterComboboxes"
            :key="filter.key"
            class="filter-combobox"
            :model-value="filter.value"
            :options="filter.options"
            :placeholder="filter.placeholder"
            :search-placeholder="filter.searchPlaceholder"
            :empty-text="filter.emptyText"
            :icon="filter.icon"
            @update:model-value="filter.onChange"
          />
          <div class="status-actions">
            <FilterCombobox
              class="status-filter"
              :model-value="filterForm.failed === 'all' ? null : filterForm.failed"
              :options="failedFilterOptions"
              :placeholder="t('结果', 'Result')"
              :icon="CircleCheck"
              :searchable="false"
              @update:model-value="handleFailedChange"
            />
            <Button variant="outline" :disabled="isLoading" @click="refresh()">
              <Spinner v-if="isLoading" data-icon="inline-start" />
              {{ t('筛选', 'Filter') }}
            </Button>
          </div>
        </div>
      </div>
    </section>

    <UsageDashboardSkeleton v-if="!hasLoadedDashboard" />
    <template v-else>
      <div class="metric-grid dashboard-metric-grid">
        <div
          v-for="metric in metricCards"
          :key="metric.key"
          class="metric-card dashboard-metric-card"
          :class="`is-${metric.tone}`"
        >
          <div class="metric-icon" aria-hidden="true">
            <component :is="metric.icon" :size="20" :stroke-width="2.2" />
          </div>
          <div class="metric-label">{{ metric.label }}</div>
          <div class="metric-value">{{ metric.value }}</div>
          <div class="metric-footnote usage-metric-footnote" :title="metric.footnote">
            {{ metric.footnote }}
          </div>
        </div>
      </div>

      <div class="dashboard-layout">
        <div class="dashboard-top-grid">
          <ChartPanel
            class="usage-trend-panel area-trend"
            :title="t('用量趋势', 'Usage trend')"
            :empty="trends.length === 0"
            :loading="isLoading"
          >
            <template #chart>
              <UsageTrendChart
                :key="trendChartKey"
                :data="trends"
                :requests-visible="trendSeriesVisibility.requests"
                :tokens-visible="trendSeriesVisibility.tokens"
                :failed-visible="trendSeriesVisibility.failed"
                :requests-label="t('请求数', 'Requests')"
                :failed-label="t('失败请求', 'Failed requests')"
                :format-bucket="formatTrendBucket"
              />
            </template>
            <template #actions>
              <div class="trend-legend" :aria-label="t('用量趋势图例', 'Usage trend legend')">
                <Toggle
                  v-for="item in trendLegendItems"
                  :key="item.key"
                  class="trend-legend-button"
                  :class="[`is-${item.key}`, { 'is-hidden': !trendSeriesVisibility[item.key] }]"
                  :style="{ '--trend-color': item.color }"
                  size="sm"
                  :model-value="trendSeriesVisibility[item.key]"
                  @update:model-value="toggleTrendSeries(item.key)"
                >
                  <span class="trend-legend-marker" aria-hidden="true" />
                  <span>{{ item.label }}</span>
                </Toggle>
              </div>
            </template>
          </ChartPanel>

          <ChartPanel
            class="token-panel area-token"
            :title="t('Token 构成', 'Token breakdown')"
            :empty="tokenBreakdownTotal === 0"
            :loading="isLoading"
            :compact-footer="tokenBreakdownItems.length <= 1"
          >
            <template #chart>
              <UsageDonutChart
                :items="tokenBreakdownItems"
                :center-value="formatCompact(summary?.total_tokens ?? 0)"
                center-label="Token"
                :value-formatter="formatCompact"
              />
            </template>
            <ol class="distribution-legend token-legend" :aria-label="t('Token 构成图例', 'Token breakdown legend')">
              <li
                v-for="item in tokenBreakdownItems"
                :key="item.key"
                class="distribution-legend-item"
              >
                <span
                  class="distribution-marker"
                  :style="distributionMarkerStyle(item.colorIndex)"
                  aria-hidden="true"
                />
                <span class="distribution-label">{{ item.label }}</span>
                <span class="distribution-count">{{ item.valueText }}</span>
                <span class="distribution-percent">{{ item.percentText }}</span>
              </li>
            </ol>
          </ChartPanel>
        </div>

        <div class="dashboard-columns">
          <div class="dashboard-column dashboard-column-left">
            <section class="panel heatmap-panel area-heatmap">
              <div class="panel-heading-row dashboard-panel-heading">
                <h2 class="section-title">{{ t('小时活跃（今日）', 'Hourly activity (today)') }}</h2>
              </div>
              <div class="panel-inner compact-panel-inner">
                <div class="heatmap-groups">
                  <div class="heatmap-group is-records">
                    <div class="heatmap-group-heading">
                      <span>{{ t('请求数', 'Requests') }}</span>
                      <strong>{{ t(`${formatInteger(todayRecordTotal)} 次`, `${formatInteger(todayRecordTotal)} requests`) }}</strong>
                    </div>
                    <div class="hour-heatmap is-records" :aria-label="t('今日请求数小时活跃热力图', 'Today hourly request heatmap')">
                      <div
                        v-for="item in hourActivityItems"
                        :key="`records-${item.hour}`"
                        class="hour-cell"
                        :class="{ 'is-empty': item.records === 0 }"
                        :style="item.recordStyle"
                        :title="item.recordTitle"
                      >
                        <span>{{ item.label }}</span>
                      </div>
                    </div>
                  </div>
                  <div class="heatmap-group is-tokens">
                    <div class="heatmap-group-heading">
                      <span>Token</span>
                      <strong>{{ formatCompact(todayTokenTotal) }}</strong>
                    </div>
                    <div class="hour-heatmap is-tokens" :aria-label="t('今日 Token 小时活跃热力图', 'Today hourly token heatmap')">
                      <div
                        v-for="item in hourActivityItems"
                        :key="`tokens-${item.hour}`"
                        class="hour-cell"
                        :class="{ 'is-empty': item.tokens === 0 }"
                        :style="item.tokenStyle"
                        :title="item.tokenTitle"
                      >
                        <span>{{ item.label }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <ChartPanel
              class="distribution-panel area-provider"
              :title="t('服务商分布', 'Provider distribution')"
              :empty="distributions.providers.length === 0"
              :loading="isLoading"
              :compact-footer="providerDistributionLegend.length <= 1"
            >
              <template #chart>
                <UsageDonutChart
                  :items="providerDistributionLegend"
                  :center-value="formatCompact(distributions.providers.reduce((sum, item) => sum + item.records, 0))"
                  :center-label="t('服务商', 'Providers')"
                />
              </template>
              <ol
                class="distribution-legend"
                :class="{ 'is-single': providerDistributionLegend.length === 1 }"
                :aria-label="t('服务商分布图例', 'Provider distribution legend')"
              >
                <li
                  v-for="item in providerDistributionLegend"
                  :key="item.key"
                  class="distribution-legend-item"
                >
                  <span
                    class="distribution-marker"
                    :style="distributionMarkerStyle(item.colorIndex)"
                    aria-hidden="true"
                  />
                  <span class="distribution-label" :title="item.label">{{ item.label }}</span>
                  <span class="distribution-count">{{ item.recordsText }}</span>
                  <span class="distribution-percent">{{ item.percentText }}</span>
                </li>
              </ol>
            </ChartPanel>
          </div>

          <div class="dashboard-column dashboard-column-middle">
            <section class="panel anomaly-panel area-anomaly">
              <div class="panel-heading-row dashboard-panel-heading">
                <h2 class="section-title">{{ t('异常概览', 'Anomaly overview') }}</h2>
                <Button v-if="canOpenRecords" size="sm" variant="ghost" @click="goRecords({ failed: true })">
                  {{ t('更多', 'More') }}
                </Button>
              </div>
              <div class="panel-inner compact-panel-inner">
                <div class="anomaly-stat-grid">
                  <div
                    v-for="item in anomalyStats"
                    :key="item.key"
                    class="anomaly-stat"
                    :class="`is-${item.tone}`"
                  >
                    <span>{{ item.label }}</span>
                    <strong>{{ item.value }}</strong>
                  </div>
                </div>
                <div class="top-failed-endpoint">
                  <span>{{ t('Top 失败接口', 'Top failed endpoint') }}</span>
                  <strong :title="topFailedEndpoint?.label ?? t('暂无失败接口', 'No failed endpoints')">
                    {{ topFailedEndpoint?.label ?? t('暂无失败接口', 'No failed endpoints') }}
                  </strong>
                </div>
                <div class="recent-failed-list">
                  <div v-if="recentFailedRows.length === 0" class="empty-inline">
                    {{ t('当前范围暂无失败请求', 'No failed requests in the current range') }}
                  </div>
                  <component
                    :is="canOpenRecords ? 'button' : 'div'"
                    v-for="item in recentFailedRows"
                    v-else
                    :key="item.bucket"
                    class="recent-failed-row"
                    :class="{ 'is-static': !canOpenRecords }"
                    :type="canOpenRecords ? 'button' : undefined"
                    @click="canOpenRecords ? goRecords({ failed: true }) : undefined"
                  >
                    <span>{{ formatTrendBucket(item.bucket) }}</span>
                    <strong>{{ t(`${formatInteger(item.records)} 次`, `${formatInteger(item.records)} requests`) }}</strong>
                    <em>{{ formatCompact(item.total_tokens) }} Token</em>
                  </component>
                </div>
              </div>
            </section>

            <ChartPanel
              class="distribution-panel area-endpoint"
              :title="t('接口分布', 'Endpoint distribution')"
              :empty="distributions.endpoints.length === 0"
              :loading="isLoading"
              :compact-footer="endpointDistributionLegend.length <= 1"
            >
              <template #chart>
                <UsageDonutChart
                  :items="endpointDistributionLegend"
                  :center-value="formatCompact(distributions.endpoints.reduce((sum, item) => sum + item.records, 0))"
                  :center-label="t('接口', 'Endpoints')"
                />
              </template>
              <ol
                class="distribution-legend"
                :class="{ 'is-single': endpointDistributionLegend.length === 1 }"
                :aria-label="t('接口分布图例', 'Endpoint distribution legend')"
              >
                <li
                  v-for="item in endpointDistributionLegend"
                  :key="item.key"
                  class="distribution-legend-item"
                >
                  <span
                    class="distribution-marker"
                    :style="distributionMarkerStyle(item.colorIndex)"
                    aria-hidden="true"
                  />
                  <span class="distribution-label" :title="item.label">{{ item.label }}</span>
                  <span class="distribution-count">{{ item.recordsText }}</span>
                  <span class="distribution-percent">{{ item.percentText }}</span>
                </li>
              </ol>
            </ChartPanel>
          </div>

          <div class="dashboard-column dashboard-column-right">
            <section class="panel ranking-panel area-primary-ranking">
              <div class="panel-heading-row dashboard-panel-heading">
                <h2 class="section-title">{{ rankingTitle }}</h2>
                <Select :model-value="primaryRankingSort" @update:model-value="handlePrimaryRankingSortChange">
                  <SelectTrigger class="ranking-sort-select" size="sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    <SelectGroup>
                      <SelectItem
                        v-for="option in rankingSortOptions"
                        :key="option.value"
                        :value="option.value"
                      >
                        {{ option.label }}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
              <div class="panel-inner compact-panel-inner">
                <div class="ranking-list">
                  <div v-if="primaryRankingRows.length === 0" class="empty-inline">{{ t('暂无排行数据', 'No ranking data') }}</div>
                  <div
                    v-for="(row, index) in primaryRankingRows"
                    v-else
                    :key="row.key"
                    class="ranking-row"
                    :style="rankingBarStyle(rankingMetricValue(row, primaryRankingSort), maxPrimaryRankingValue)"
                  >
                    <span class="ranking-index">{{ index + 1 }}</span>
                    <div class="ranking-main">
                      <div class="ranking-label-line">
                        <strong :title="row.label">{{ row.label }}</strong>
                        <span>{{ rankingSecondaryText(row, primaryRankingSort) }}</span>
                      </div>
                      <div class="ranking-track" aria-hidden="true"><span /></div>
                    </div>
                    <div class="ranking-values">
                      <strong>{{ rankingPrimaryText(row, primaryRankingSort) }}</strong>
                      <span>{{ t(`${formatInteger(row.records)} 次`, `${formatInteger(row.records)} requests`) }}</span>
                    </div>
                    <Button v-if="canOpenRecords" size="xs" variant="ghost" @click="goRecords(rankingFilters(row))">
                      {{ t('明细', 'Records') }}
                    </Button>
                  </div>
                </div>
              </div>
            </section>

            <section class="panel ranking-panel area-model-ranking">
              <div class="panel-heading-row dashboard-panel-heading">
                <h2 class="section-title">{{ t('模型排行', 'Model ranking') }}</h2>
                <Select :model-value="modelRankingSort" @update:model-value="handleModelRankingSortChange">
                  <SelectTrigger class="ranking-sort-select" size="sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    <SelectGroup>
                      <SelectItem
                        v-for="option in rankingSortOptions"
                        :key="option.value"
                        :value="option.value"
                      >
                        {{ option.label }}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
              <div class="panel-inner compact-panel-inner">
                <div class="ranking-list">
                  <div v-if="modelRankingRows.length === 0" class="empty-inline">{{ t('暂无模型数据', 'No model data') }}</div>
                  <div
                    v-for="(row, index) in modelRankingRows"
                    v-else
                    :key="row.key"
                    class="ranking-row"
                    :style="rankingBarStyle(rankingMetricValue(row, modelRankingSort), maxModelRankingValue)"
                  >
                    <span class="ranking-index">{{ index + 1 }}</span>
                    <div class="ranking-main">
                      <div class="ranking-label-line">
                        <strong :title="row.label">{{ row.label }}</strong>
                        <span>{{ rankingSecondaryText(row, modelRankingSort) }}</span>
                      </div>
                      <div class="ranking-track" aria-hidden="true"><span /></div>
                    </div>
                    <div class="ranking-values">
                      <strong>{{ rankingPrimaryText(row, modelRankingSort) }}</strong>
                      <span>{{ t(`${formatInteger(row.records)} 次`, `${formatInteger(row.records)} requests`) }}</span>
                    </div>
                    <Button v-if="canOpenRecords" size="xs" variant="ghost" @click="goRecords(modelFilters(row))">
                      {{ t('明细', 'Records') }}
                    </Button>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.usage-dashboard-page {
  gap: 16px;
}

.filter-panel {
  overflow: visible;
}

.filter-summary {
  display: none;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 12px;
  border-bottom: 1px solid var(--cpa-border);
}

.filter-summary > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.filter-summary strong {
  color: var(--cpa-text-strong);
  font-size: 13px;
  line-height: 1.2;
}

.filter-summary span {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-toggle {
  flex: 0 0 auto;
}

.filter-toolbar {
  display: grid;
  gap: 12px;
  padding: 14px 16px 16px;
}

.time-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(460px, 520px);
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.field-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr)) minmax(184px, 1.2fr);
  gap: 8px;
  align-items: stretch;
  width: 100%;
  min-width: 0;
}

.range-picker {
  min-width: 0;
  width: 100%;
}

.quick-ranges {
  min-width: 0;
  width: 100%;
}

.filter-combobox {
  min-width: 0;
  width: 100%;
}

.status-actions {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  min-width: 0;
}

.status-filter {
  min-width: 96px;
}

.status-actions :deep([data-slot="button"]) {
  min-width: 78px;
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  min-width: 0;
}

.quota-status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 420px;
  min-width: 0;
  padding: 5px 9px;
  border: 1px solid color-mix(in srgb, var(--cpa-primary) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-primary-wash) 72%, var(--cpa-surface));
  color: var(--cpa-primary);
  line-height: 1.2;
  white-space: nowrap;
}

.quota-status-pill strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-weight: 760;
}

.quota-status-pill.is-warning {
  border-color: color-mix(in srgb, var(--cpa-warning) 24%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-warning-weak) 68%, var(--cpa-surface));
  color: var(--cpa-warning);
}

.quota-status-pill.is-paused {
  border-color: color-mix(in srgb, var(--cpa-danger) 24%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-danger-weak) 68%, var(--cpa-surface));
  color: var(--cpa-danger);
}

.refresh-status {
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.refresh-status.is-error {
  color: var(--cpa-danger);
}

.dashboard-metric-grid {
  /* Keep the six usage metrics on one row where space allows. */
  grid-template-columns: repeat(auto-fit, minmax(138px, 1fr));
  gap: 12px;
}

.dashboard-metric-card {
  min-height: 116px;
  padding: 16px;
}

.dashboard-metric-card .metric-value {
  font-size: 24px;
}

.usage-metric-footnote {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-layout {
  display: grid;
  gap: 16px;
  min-width: 0;
}

.dashboard-top-grid,
.dashboard-columns {
  display: grid;
  gap: 16px;
  min-width: 0;
}

.dashboard-top-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: stretch;
}

.dashboard-columns {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: start;
  --dashboard-card-height: 352px;
}

.dashboard-column {
  display: grid;
  align-content: start;
  gap: 16px;
  min-width: 0;
}

.area-trend,
.area-token,
.area-anomaly,
.area-heatmap,
.area-primary-ranking,
.area-model-ranking,
.area-provider,
.area-endpoint {
  min-width: 0;
}

.area-heatmap {
  order: 1;
}

.area-anomaly {
  order: 2;
}

.area-primary-ranking {
  order: 3;
}

.area-model-ranking {
  order: 4;
}

.area-provider {
  order: 5;
}

.area-endpoint {
  order: 6;
}

.area-anomaly,
.area-heatmap,
.area-provider,
.area-endpoint {
  height: var(--dashboard-card-height);
}

.area-primary-ranking,
.area-model-ranking {
  height: var(--dashboard-card-height);
}

.usage-trend-panel.chart-panel {
  min-height: 320px;
}

.usage-trend-panel.chart-panel :deep(.chart-body),
.usage-trend-panel.chart-panel :deep(.chart-surface),
.usage-trend-panel.chart-panel :deep(.chart-empty) {
  height: 262px;
}

.token-panel.chart-panel,
.token-panel.chart-panel.has-chart-footer,
.token-panel.chart-panel.has-chart-footer.has-compact-footer {
  min-height: 282px;
}

.token-panel.chart-panel,
.distribution-panel.chart-panel {
  overflow: hidden;
}

.token-panel.chart-panel.has-chart-footer :deep(.chart-body),
.token-panel.chart-panel.has-chart-footer :deep(.chart-surface),
.token-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
  height: 154px;
}

.distribution-panel.chart-panel,
.distribution-panel.chart-panel.has-chart-footer,
.distribution-panel.chart-panel.has-chart-footer.has-compact-footer {
  min-height: var(--dashboard-card-height);
}

.distribution-panel.chart-panel.has-chart-footer :deep(.chart-body),
.distribution-panel.chart-panel.has-chart-footer :deep(.chart-surface),
.distribution-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
  height: 146px;
}

.token-panel.chart-panel :deep(.chart-footer),
.distribution-panel.chart-panel :deep(.chart-footer) {
  padding: 0 16px 14px;
}

.heatmap-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.heatmap-panel .compact-panel-inner {
  grid-template-rows: minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}

.anomaly-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.ranking-panel {
  min-height: var(--dashboard-card-height);
  overflow: hidden;
}

.anomaly-panel .compact-panel-inner {
  grid-template-rows: auto auto minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}

.ranking-panel .compact-panel-inner {
  grid-template-rows: minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}

.compact-panel-inner {
  display: grid;
  align-content: start;
  gap: 12px;
  box-sizing: border-box;
  height: calc(100% - 56px);
  padding: 16px;
}

.panel-heading-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.area-trend {
  grid-column: span 2;
}

.dashboard-panel-heading {
  box-sizing: border-box;
  align-items: center;
  min-height: 56px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--cpa-border);
}

.panel-heading-row .section-title {
  min-width: 0;
  margin: 0;
}

.ranking-sort-select {
  flex: 0 0 104px;
  width: 104px;
}

.anomaly-stat-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 7px;
}

.anomaly-stat {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-muted);
}

.anomaly-stat span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.2;
}

.anomaly-stat strong {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--cpa-text-strong);
  font-size: 17px;
  font-weight: 760;
  line-height: 1.18;
}

.anomaly-stat.is-danger strong {
  color: var(--cpa-danger);
}

.anomaly-stat.is-warning strong {
  color: var(--cpa-warning);
}

.anomaly-stat.is-success strong {
  color: var(--cpa-success);
}

.top-failed-endpoint {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--cpa-danger) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-danger-weak) 56%, var(--cpa-surface));
}

.top-failed-endpoint span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.top-failed-endpoint strong {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-failed-list,
.ranking-list {
  display: grid;
  align-content: start;
  gap: 6px;
  min-width: 0;
}

.recent-failed-list,
.ranking-list {
  min-height: 0;
  overflow-y: auto;
  padding-right: 2px;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 34%, transparent) transparent;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
}

.recent-failed-list::-webkit-scrollbar,
.ranking-list::-webkit-scrollbar,
.distribution-legend::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

.recent-failed-list::-webkit-scrollbar-track,
.recent-failed-list::-webkit-scrollbar-corner,
.ranking-list::-webkit-scrollbar-track,
.ranking-list::-webkit-scrollbar-corner,
.distribution-legend::-webkit-scrollbar-track,
.distribution-legend::-webkit-scrollbar-corner {
  background: transparent;
}

.recent-failed-list::-webkit-scrollbar-thumb,
.ranking-list::-webkit-scrollbar-thumb,
.distribution-legend::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: color-mix(in srgb, var(--cpa-text-muted) 30%, transparent);
}

.recent-failed-list::-webkit-scrollbar-thumb:hover,
.ranking-list::-webkit-scrollbar-thumb:hover,
.distribution-legend::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--cpa-text-muted) 48%, transparent);
}

.recent-failed-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-width: 0;
  min-height: 32px;
  padding: 5px 8px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-raised);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.recent-failed-row:hover {
  border-color: color-mix(in srgb, var(--cpa-danger) 22%, var(--cpa-border));
  background: color-mix(in srgb, var(--cpa-danger-weak) 42%, var(--cpa-surface));
}

.recent-failed-row.is-static {
  cursor: default;
}

.recent-failed-row.is-static:hover {
  border-color: var(--cpa-border);
  background: var(--cpa-surface-raised);
}

.recent-failed-row span,
.recent-failed-row em {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-style: normal;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-failed-row strong {
  color: var(--cpa-danger);
  font-size: 12px;
  font-weight: 760;
  white-space: nowrap;
}

.heatmap-groups {
  display: grid;
  align-content: stretch;
  grid-template-rows: repeat(2, minmax(0, 1fr));
  gap: 10px;
  min-width: 0;
  min-height: 0;
}

.heatmap-group {
  display: grid;
  grid-template-columns: minmax(70px, 84px) minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-width: 0;
  min-height: 0;
  padding: 8px;
  border: 1px solid color-mix(in srgb, var(--cpa-border) 72%, transparent);
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, var(--cpa-surface-muted) 72%, transparent);
}

.heatmap-group.is-records {
  --heat-color-start: var(--cpa-accent-blue);
  --heat-color-end: var(--cpa-primary);
}

.heatmap-group.is-tokens {
  --heat-color-start: var(--cpa-primary);
  --heat-color-end: var(--cpa-chart-3, #8b5cf6);
}

.heatmap-group-heading {
  display: grid;
  gap: 5px;
  align-content: center;
  min-width: 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.2;
}

.heatmap-group-heading span {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}

.heatmap-group-heading span::before {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 3px;
  background: var(--heat-color-end);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--heat-color-end) 14%, transparent);
  content: "";
}

.heatmap-group-heading span,
.heatmap-group-heading strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.heatmap-group-heading strong {
  color: var(--cpa-text-strong);
  font-variant-numeric: tabular-nums;
  font-weight: 760;
}

.hour-heatmap {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 5px;
  min-width: 0;
}

.hour-heatmap.is-records {
  --heat-color-start: var(--cpa-accent-blue);
  --heat-color-end: var(--cpa-primary);
}

.hour-heatmap.is-tokens {
  --heat-color-start: var(--cpa-primary);
  --heat-color-end: var(--cpa-chart-3, #8b5cf6);
}

.hour-cell {
  display: grid;
  position: relative;
  min-width: 0;
  min-height: 28px;
  overflow: hidden;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--heat-color-end) 14%, var(--cpa-border));
  border-radius: 5px;
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-strong);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  line-height: 1;
}

.hour-cell::before {
  position: absolute;
  inset: 0;
  border-radius: inherit;
  background: linear-gradient(180deg, var(--heat-color-start), var(--heat-color-end));
  content: "";
  opacity: var(--heat-intensity);
}

.hour-cell span {
  position: relative;
  z-index: 1;
}

.hour-cell.is-empty {
  border-color: color-mix(in srgb, var(--cpa-border) 70%, transparent);
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-muted);
}

.ranking-row {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  min-width: 0;
  min-height: 43px;
  padding: 6px 8px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  background: var(--cpa-surface-raised);
}

.ranking-index {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 6px;
  background: var(--cpa-primary-wash);
  color: var(--cpa-primary);
  font-size: 12px;
  font-weight: 760;
}

.ranking-main {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.ranking-label-line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.ranking-label-line strong {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ranking-label-line span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.ranking-track {
  height: 6px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--cpa-surface-muted);
}

.ranking-track span {
  display: block;
  width: var(--ranking-width);
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--cpa-primary), var(--cpa-accent-blue));
}

.ranking-values {
  display: grid;
  gap: 1px;
  min-width: 72px;
  text-align: right;
}

.ranking-values strong {
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
}

.ranking-values span {
  color: var(--cpa-text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.trend-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 12px;
  justify-content: flex-end;
}

.trend-legend-button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  margin: 0;
  padding: 2px 0;
  border: 0;
  color: var(--cpa-text-muted);
  font: inherit;
  font-size: 12px;
  line-height: 18px;
  white-space: nowrap;
  background: transparent;
  cursor: pointer;
  transition: color 0.15s ease, opacity 0.15s ease;
}

.trend-legend-button:hover,
.trend-legend-button:focus-visible {
  color: var(--cpa-text-strong);
}

.trend-legend-button:focus-visible {
  border-radius: 3px;
  outline: 2px solid color-mix(in srgb, var(--trend-color) 52%, transparent);
  outline-offset: 2px;
}

.trend-legend-marker {
  width: 10px;
  height: 10px;
  flex: 0 0 auto;
  border: 2px solid var(--trend-color);
  border-radius: 3px;
  background: var(--trend-color);
}

.trend-legend-button.is-hidden {
  opacity: 0.48;
}

.trend-legend-button.is-hidden .trend-legend-marker {
  background: transparent;
}

.distribution-legend {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(168px, 1fr));
  gap: 5px 7px;
  max-height: 74px;
  margin: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 0;
  padding-right: 2px;
  list-style: none;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 34%, transparent) transparent;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
}

.distribution-legend.is-single {
  grid-template-columns: minmax(0, 300px);
  justify-content: center;
  max-height: none;
  overflow: visible;
}

.token-legend {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px 6px;
}

.token-legend .distribution-legend-item {
  grid-template-columns: 8px minmax(0, 1fr) auto auto;
  gap: 5px;
  padding: 3px 5px;
}

.token-legend .distribution-marker {
  width: 7px;
  height: 7px;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--distribution-color) 12%, transparent);
}

.token-legend .distribution-label,
.token-legend .distribution-count,
.token-legend .distribution-percent {
  font-size: 11px;
  line-height: 16px;
}

.token-legend .distribution-percent {
  min-width: 28px;
}

.distribution-legend-item {
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  min-width: 0;
  padding: 4px 7px;
  border: 1px solid color-mix(in srgb, var(--cpa-border) 68%, transparent);
  border-radius: 6px;
  background: color-mix(in srgb, var(--cpa-surface-muted) 72%, transparent);
}

.distribution-marker {
  width: 9px;
  height: 9px;
  border-radius: 3px;
  background: var(--distribution-color);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--distribution-color) 14%, transparent);
}

.distribution-label {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.distribution-count {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  font-weight: 750;
  line-height: 18px;
}

.distribution-percent {
  min-width: 36px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  line-height: 18px;
  text-align: right;
}

.empty-inline {
  display: grid;
  min-height: 48px;
  place-items: center;
  border: 1px dashed var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-text-muted);
  font-size: 12px;
}

@media (max-width: 1680px) {
  .dashboard-columns {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 1320px) {
  .dashboard-columns {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .field-row,
  .field-row.is-account-scope {
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  }

}

@media (max-width: 980px) {
  .dashboard-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .dashboard-top-grid,
  .dashboard-columns,
  .dashboard-column {
    display: contents;
  }

  .area-trend {
    grid-column: auto;
    order: 1;
  }

  .area-anomaly {
    order: 2;
  }

  .area-token {
    order: 3;
  }

  .area-heatmap {
    order: 4;
  }

  .area-primary-ranking {
    order: 5;
  }

  .area-model-ranking {
    order: 6;
  }

  .area-provider {
    order: 7;
  }

  .area-endpoint {
    order: 8;
  }

  .area-anomaly,
  .area-heatmap,
  .area-primary-ranking,
  .area-model-ranking,
  .area-provider,
  .area-endpoint {
    height: auto;
  }

  .usage-trend-panel.chart-panel {
    min-height: 304px;
  }

  .token-panel.chart-panel,
  .token-panel.chart-panel.has-chart-footer,
  .token-panel.chart-panel.has-chart-footer.has-compact-footer {
    min-height: 252px;
  }

  .distribution-panel.chart-panel,
  .distribution-panel.chart-panel.has-chart-footer,
  .distribution-panel.chart-panel.has-chart-footer.has-compact-footer {
    min-height: 236px;
  }

  .usage-trend-panel.chart-panel :deep(.chart-body),
  .usage-trend-panel.chart-panel :deep(.chart-surface),
  .usage-trend-panel.chart-panel :deep(.chart-empty) {
    height: 248px;
  }

  .token-panel.chart-panel.has-chart-footer :deep(.chart-body),
  .token-panel.chart-panel.has-chart-footer :deep(.chart-surface),
  .token-panel.chart-panel.has-chart-footer :deep(.chart-empty) {
    height: 136px;
  }

  .time-row {
    grid-template-columns: 1fr;
  }

  .hour-heatmap {
    grid-template-columns: repeat(12, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .usage-dashboard-page {
    gap: 12px;
  }

  .filter-summary {
    display: flex;
  }

  .filter-panel:not(.is-expanded) .filter-toolbar {
    display: none;
  }

  .filter-toolbar {
    gap: 10px;
    padding: 12px;
  }

  .status-actions {
    grid-column: 1 / -1;
    align-items: stretch;
  }

  .status-actions :deep([data-slot="select-trigger"]) {
    min-width: 0;
  }

  .header-actions {
    width: 100%;
    align-items: flex-start;
    justify-content: space-between;
  }

  .quota-status-pill {
    max-width: 100%;
  }

  .refresh-status {
    white-space: normal;
  }

  .dashboard-metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .dashboard-metric-card {
    min-height: 108px;
  }

  .compact-panel-inner {
    padding: 12px;
  }

  .panel-heading-row {
    align-items: flex-start;
  }

  .anomaly-stat-grid {
    grid-template-columns: 1fr;
  }

  .top-failed-endpoint {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .recent-failed-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .recent-failed-row em {
    grid-column: 1 / -1;
  }

  .heatmap-groups {
    grid-template-rows: none;
  }

  .heatmap-group {
    grid-template-columns: 1fr;
    gap: 7px;
  }

  .heatmap-group-heading {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }

  .hour-heatmap {
    grid-template-columns: repeat(6, minmax(0, 1fr));
  }

  .hour-cell {
    min-height: 24px;
  }

  .ranking-row {
    grid-template-columns: 22px minmax(0, 1fr) auto;
    gap: 8px;
  }

  .ranking-row :deep([data-slot="button"]) {
    grid-column: 2 / -1;
    justify-self: end;
  }

  .ranking-values {
    min-width: 58px;
  }

  .distribution-legend,
  .token-legend {
    grid-template-columns: 1fr;
    max-height: none;
    overflow: visible;
  }
}
</style>
