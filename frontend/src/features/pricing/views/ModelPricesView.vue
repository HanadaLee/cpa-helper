<script setup lang="ts">
import type { Component } from 'vue'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import { useConfirmDialog } from '@/shared/ui/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import TimePicker from '@/shared/ui/TimePicker.vue'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  CircleAlert,
  CalendarDays,
  Database,
  Layers3,
  ListFilter,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
  Settings2,
  Trash2,
} from '@lucide/vue'

import {
  createModelPrice,
  deleteModelPrice,
  getLiteLLMProxySettings,
  getPricingHolidayCalendar,
  listModelPriceCatalog,
  listModelPrices,
  syncLitellmModelPrices,
  updateLiteLLMProxySettings,
  updateModelPrice,
  syncPricingHolidayCalendar,
  updatePricingCalendarSettings,
} from '@/features/pricing/api/pricingApi'
import type {
  LiteLLMProxySettingsPayload,
  ModelPrice,
  ModelPriceCatalogResponse,
  ModelPricePayload,
  PricingHolidayCalendar,
} from '@/shared/types/api'
import { formatDateTime, formatInteger } from '@/shared/utils/format'
import { useI18n } from '@/shared/i18n'
import FilterCombobox from '@/shared/ui/FilterCombobox.vue'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'

type PriceRowStatus = 'missing' | 'litellm' | 'manual'
type PriceStatusFilter = 'cpa' | 'missing' | 'litellm' | 'manual' | 'library'
type BillingUnit = 'token' | 'request'
type PriceFieldName = keyof Pick<
  ModelPrice,
  | 'input_usd_per_million'
  | 'output_usd_per_million'
  | 'cache_read_usd_per_million'
  | 'cache_creation_usd_per_million'
  | 'long_context_input_usd_per_million'
  | 'long_context_output_usd_per_million'
  | 'long_context_cache_read_usd_per_million'
  | 'long_context_cache_creation_usd_per_million'
  | 'peak_input_usd_per_million'
  | 'peak_output_usd_per_million'
  | 'peak_cache_read_usd_per_million'
  | 'peak_cache_creation_usd_per_million'
  | 'long_context_peak_input_usd_per_million'
  | 'long_context_peak_output_usd_per_million'
  | 'long_context_peak_cache_read_usd_per_million'
  | 'long_context_peak_cache_creation_usd_per_million'
>

type PriceTier = {
  key: string
  label: string
  prefix: '' | 'long_context_' | 'peak_' | 'long_context_peak_'
  multiplier: number
}

const peakRateFields: Array<{ key: PriceFieldName; zh: string; en: string }> = [
  { key: 'peak_input_usd_per_million', zh: '高峰输入 ($/MTok)', en: 'Peak input ($/MTok)' },
  { key: 'peak_output_usd_per_million', zh: '高峰输出 ($/MTok)', en: 'Peak output ($/MTok)' },
  { key: 'peak_cache_read_usd_per_million', zh: '高峰缓存读 ($/MTok)', en: 'Peak cache read ($/MTok)' },
  { key: 'peak_cache_creation_usd_per_million', zh: '高峰缓存写 ($/MTok)', en: 'Peak cache write ($/MTok)' },
]
const longPeakRateFields: Array<{ key: PriceFieldName; zh: string; en: string }> = [
  { key: 'long_context_peak_input_usd_per_million', zh: '长上下文高峰输入 ($/MTok)', en: 'Long peak input ($/MTok)' },
  { key: 'long_context_peak_output_usd_per_million', zh: '长上下文高峰输出 ($/MTok)', en: 'Long peak output ($/MTok)' },
  { key: 'long_context_peak_cache_read_usd_per_million', zh: '长上下文高峰缓存读 ($/MTok)', en: 'Long peak cache read ($/MTok)' },
  { key: 'long_context_peak_cache_creation_usd_per_million', zh: '长上下文高峰缓存写 ($/MTok)', en: 'Long peak cache write ($/MTok)' },
]

interface PriceDisplayRow {
  key: string
  in_cpa: boolean
  id: string
  name: string
  owner: string | null
  suggested_provider: string
  price: ModelPrice | null
  provider: string
  model: string
  billing_unit: BillingUnit
  status: PriceRowStatus
}

const message = toast
const dialog = useConfirmDialog()
const { errorText, serverText, t } = useI18n()
const isLoading = ref(false)
const isSyncing = ref(false)
const isPriceSaving = ref(false)
const modalOpen = ref(false)
const proxyModalOpen = ref(false)
const isProxyLoading = ref(false)
const isProxySaving = ref(false)
const calendarModalOpen = ref(false)
const calendarYear = ref(Number(new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric' }).format(new Date())))
const calendarDays = ref<PricingHolidayCalendar['days']>([])
const calendarSourceURL = ref('')
const calendarConfigured = ref(false)
const calendarSyncedAt = ref<string | null>(null)
const peakOnMakeupDays = ref(false)
const peakPeriods = ref<PricingHolidayCalendar['peak_periods']>([])
const isCalendarLoading = ref(false)
const isCalendarSaving = ref(false)
const isCalendarSyncing = ref(false)
const editingId = ref<number | null>(null)
const prices = ref<ModelPrice[]>([])
const catalog = ref<ModelPriceCatalogResponse | null>(null)
const selectedProvider = ref<string | null>(null)
const selectedStatus = ref<PriceStatusFilter | null>(null)
const searchQuery = ref('')
const page = ref(1)
const pageSize = ref(20)
const form = reactive<ModelPricePayload>({
  provider: '',
  model: '',
  input_usd_per_million: 0,
  output_usd_per_million: 0,
  cache_read_usd_per_million: 0,
  cache_creation_usd_per_million: 0,
  request_usd: null,
  fast_enabled: false,
  fast_multiplier: 1,
  long_context_enabled: false,
  long_context_threshold_tokens: 0,
  long_context_input_usd_per_million: 0,
  long_context_output_usd_per_million: 0,
  long_context_cache_read_usd_per_million: 0,
  long_context_cache_creation_usd_per_million: 0,
  long_context_fast_unsupported: false,
  off_peak_enabled: false,
  peak_input_usd_per_million: 0,
  peak_output_usd_per_million: 0,
  peak_cache_read_usd_per_million: 0,
  peak_cache_creation_usd_per_million: 0,
  peak_request_usd: null,
  long_context_peak_input_usd_per_million: 0,
  long_context_peak_output_usd_per_million: 0,
  long_context_peak_cache_read_usd_per_million: 0,
  long_context_peak_cache_creation_usd_per_million: 0,
})
const proxyForm = reactive<LiteLLMProxySettingsPayload>({
  enabled: false,
  proxy_url: '',
})

const priceRows = computed<PriceDisplayRow[]>(() => {
  const rows: PriceDisplayRow[] = []
  const catalogPriceIds = new Set<number>()
  for (const model of catalog.value?.models ?? []) {
    if (model.price) {
      catalogPriceIds.add(model.price.id)
    }
    const provider = model.price?.provider || model.suggested_provider || model.owner || providerFromModelId(model.id)
    const billingUnit = billingUnitForPrice(model.price, model.id)
    rows.push({
      key: `catalog:${model.id}`,
      in_cpa: true,
      id: model.id,
      name: model.name || model.id,
      owner: model.owner,
      suggested_provider: model.suggested_provider,
      price: model.price,
      provider,
      model: model.price?.model || model.id,
      billing_unit: billingUnit,
      status: model.price ? priceStatus(model.price, model.id) : 'missing',
    })
  }
  for (const price of prices.value) {
    if (catalogPriceIds.has(price.id)) {
      continue
    }
    rows.push({
      key: `price:${price.id}`,
      in_cpa: false,
      id: price.model,
      name: price.model,
      owner: null,
      suggested_provider: '',
      price,
      provider: price.provider,
      model: price.model,
      billing_unit: billingUnitForPrice(price, price.model),
      status: priceStatus(price, price.model),
    })
  }
  return rows
})

const providerOptions = computed(() =>
  [...new Set(priceRows.value.map((row) => row.provider).filter(Boolean))]
    .sort((a, b) => a.localeCompare(b))
    .map((provider) => ({ label: provider, value: provider })),
)

const liteLLMProxyHint = computed(() =>
  t(
    'LiteLLM 价格数据从 GitHub 下载；如果当前网络无法访问 GitHub，可以启用代理后再同步。',
    'LiteLLM price data is downloaded from GitHub. If GitHub is not reachable from this network, enable a proxy and sync again.',
  ),
)

const statusOptions = computed<Array<{ label: string; value: PriceStatusFilter }>>(() => [
  { label: t('CPA 可用模型', 'CPA available models'), value: 'cpa' },
  { label: t('未定价', 'Unpriced'), value: 'missing' },
  { label: 'LiteLLM', value: 'litellm' },
  { label: t('手动', 'Manual'), value: 'manual' },
  { label: t('仅有价格', 'Prices only'), value: 'library' },
])

const filteredPrices = computed(() => {
  return priceRows.value.filter((row) => {
    if (selectedProvider.value && row.provider !== selectedProvider.value) {
      return false
    }
    if (selectedStatus.value && !rowMatchesStatus(row, selectedStatus.value)) {
      return false
    }
    return priceMatchesSearch(row)
  })
})

const pagedPrices = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredPrices.value.slice(start, start + pageSize.value)
})

watch([selectedProvider, selectedStatus, searchQuery], () => {
  page.value = 1
})

function updatePricePage(value: number) {
  page.value = value
}

function updatePricePageSize(value: number) {
  pageSize.value = value
  page.value = 1
}

function handleProviderFilterChange(value: unknown) {
  selectedProvider.value = typeof value === 'string' ? value : null
}

function handleStatusFilterChange(value: unknown) {
  selectedStatus.value = statusOptions.value.some((option) => option.value === value)
    ? value as PriceStatusFilter
    : null
}

function normalizePriceSearch(value: string) {
  return value.trim().toLowerCase()
}

function providerFromModelId(modelId: string) {
  const separator = modelId.indexOf('/')
  return separator > 0 ? modelId.slice(0, separator) : ''
}

function billingUnitForModel(model: string): BillingUnit {
  return model.trim().toLowerCase().includes('image') ? 'request' : 'token'
}

function billingUnitForPrice(price: ModelPrice | null, fallbackModel: string): BillingUnit {
  if (price?.billing_unit === 'request') {
    return 'request'
  }
  if (price?.billing_unit === 'token') {
    return 'token'
  }
  return billingUnitForModel(price?.model || fallbackModel)
}

function priceReadyForBilling(price: ModelPrice, fallbackModel: string): boolean {
  return billingUnitForPrice(price, fallbackModel) === 'request' ? typeof price.request_usd === 'number' : true
}

function priceStatus(price: ModelPrice, fallbackModel: string): PriceRowStatus {
  if (!priceReadyForBilling(price, fallbackModel)) {
    return 'missing'
  }
  return price.auto_synced ? 'litellm' : 'manual'
}

function rowMatchesStatus(row: PriceDisplayRow, status: PriceStatusFilter) {
  switch (status) {
    case 'cpa':
      return row.in_cpa
    case 'library':
      return !row.in_cpa
    default:
      return row.status === status
  }
}

const normalizedSearchQuery = computed(() => normalizePriceSearch(searchQuery.value))

const filteredPriceCount = computed(() => filteredPrices.value.length)

const totalPriceCount = computed(() => priceRows.value.length)
const cpaModelCount = computed(() => catalog.value?.models.length ?? 0)
const unpricedModelCount = computed(
  () => catalog.value?.unpriced_models ?? priceRows.value.filter((row) => row.in_cpa && row.status === 'missing').length,
)
const syncedPriceCount = computed(() => prices.value.filter((price) => price.auto_synced).length)
const manualPriceCount = computed(() => prices.value.filter((price) => !price.auto_synced).length)
const catalogNotice = computed(() => {
  const current = catalog.value
  if (!current) {
    return ''
  }
  if (!current.has_api_keys) {
    return t('还没有本地绑定的 API Key，当前只显示已有价格库条目。', 'No local API keys are bound yet. Only existing price library entries are shown.')
  }
  if (current.queryable_api_key_count === 0) {
    return t(
      '当前没有可查询的 API Key；已禁用、额度暂停或缺少完整密钥的 Key 不参与模型查询。只显示已有价格库条目。',
      'No API keys can be queried. Disabled, quota-paused, or incomplete keys are excluded; only existing price entries are shown.',
    )
  }
  if (current.errors.length > 0) {
    const details = current.errors
      .slice(0, 3)
      .map((item) =>
        t(
          `${item.description}：${serverText(item.message, '查询失败', 'Query failed')}`,
          `${item.description}: ${serverText(item.message, '查询失败', 'Query failed')}`,
        ),
      )
      .join(t('；', '; '))
    return t(`部分 Key 查询 CPA 模型失败：${details}`, `Some keys failed to query CPA models: ${details}`)
  }
  return ''
})
const isRequestPriceForm = computed(() => billingUnitForModel(form.model) === 'request')
const showLongContextFastUnsupported = computed(
  () => form.long_context_enabled && form.fast_enabled,
)
const priceSaveHint = computed(() =>
  isRequestPriceForm.value
    ? t(
        'image 模型按每次成功调用固定金额计费；仅修改 FAST 或峰谷设置不会取消 LiteLLM 同步。',
        'Image models are charged per successful call. Changing only FAST or peak/off-peak settings keeps LiteLLM sync enabled.',
      )
    : t(
        '基础价格修改后会转为手动价格；仅修改 FAST、长上下文或峰谷设置不会取消 LiteLLM 同步。',
        'Changing base prices makes them manual. Changing only FAST, long-context or peak/off-peak settings keeps LiteLLM sync enabled.',
      ),
)

interface PriceMetricCard {
  key: string
  label: string
  value: string
  footnote: string
  icon: Component
}

const priceMetrics = computed<PriceMetricCard[]>(() => [
  {
    key: 'models',
    label: t('CPA 模型', 'CPA models'),
    value: formatInteger(cpaModelCount.value),
    footnote: catalog.value
      ? t(
          `可查询 Key ${formatInteger(catalog.value.queryable_api_key_count)} / ${formatInteger(catalog.value.api_key_count)}`,
          `Queryable keys ${formatInteger(catalog.value.queryable_api_key_count)} / ${formatInteger(catalog.value.api_key_count)}`,
        )
      : t('等待刷新', 'Waiting for refresh'),
    icon: Layers3,
  },
  {
    key: 'unpriced',
    label: t('未定价', 'Unpriced'),
    value: formatInteger(unpricedModelCount.value),
    footnote: t(
      `筛选后 ${formatInteger(filteredPriceCount.value)} / ${formatInteger(totalPriceCount.value)}`,
      `Filtered ${formatInteger(filteredPriceCount.value)} / ${formatInteger(totalPriceCount.value)}`,
    ),
    icon: Server,
  },
  {
    key: 'synced',
    label: t('LiteLLM 同步', 'LiteLLM sync'),
    value: formatInteger(syncedPriceCount.value),
    footnote: t('自动维护', 'Auto maintained'),
    icon: RefreshCw,
  },
  {
    key: 'manual',
    label: t('手动价格', 'Manual prices'),
    value: formatInteger(manualPriceCount.value),
    footnote: t('优先保留', 'Preserved first'),
    icon: Database,
  },
])

function priceMatchesSearch(row: PriceDisplayRow) {
  if (!normalizedSearchQuery.value) {
    return true
  }
  return (
    row.provider.toLowerCase().includes(normalizedSearchQuery.value) ||
    row.model.toLowerCase().includes(normalizedSearchQuery.value) ||
    row.id.toLowerCase().includes(normalizedSearchQuery.value) ||
    row.name.toLowerCase().includes(normalizedSearchQuery.value) ||
    (row.owner ?? '').toLowerCase().includes(normalizedSearchQuery.value) ||
    row.suggested_provider.toLowerCase().includes(normalizedSearchQuery.value)
  )
}

function resetForm() {
  editingId.value = null
  form.provider = ''
  form.model = ''
  form.input_usd_per_million = 0
  form.output_usd_per_million = 0
  form.cache_read_usd_per_million = 0
  form.cache_creation_usd_per_million = 0
  form.request_usd = null
  form.fast_enabled = false
  form.fast_multiplier = 1
  form.long_context_enabled = false
  form.long_context_threshold_tokens = 0
  form.long_context_input_usd_per_million = 0
  form.long_context_output_usd_per_million = 0
  form.long_context_cache_read_usd_per_million = 0
  form.long_context_cache_creation_usd_per_million = 0
  form.long_context_fast_unsupported = false
  form.off_peak_enabled = false
  form.peak_input_usd_per_million = 0
  form.peak_output_usd_per_million = 0
  form.peak_cache_read_usd_per_million = 0
  form.peak_cache_creation_usd_per_million = 0
  form.peak_request_usd = null
  form.long_context_peak_input_usd_per_million = 0
  form.long_context_peak_output_usd_per_million = 0
  form.long_context_peak_cache_read_usd_per_million = 0
  form.long_context_peak_cache_creation_usd_per_million = 0
}

async function refresh() {
  isLoading.value = true
  try {
    const [nextPrices, nextCatalog] = await Promise.all([listModelPrices(), listModelPriceCatalog()])
    prices.value = nextPrices
    catalog.value = nextCatalog
    const lastPage = Math.max(1, Math.ceil(filteredPrices.value.length / pageSize.value))
    page.value = Math.min(page.value, lastPage)
  } catch (error) {
    message.error(errorText(error, '加载模型价格失败', 'Failed to load model prices'))
  } finally {
    isLoading.value = false
  }
}

function openCreate(prefill: Partial<ModelPricePayload> = {}) {
  resetForm()
  form.provider = prefill.provider ?? ''
  form.model = prefill.model ?? ''
  form.input_usd_per_million = prefill.input_usd_per_million ?? 0
  form.output_usd_per_million = prefill.output_usd_per_million ?? 0
  form.cache_read_usd_per_million = prefill.cache_read_usd_per_million ?? 0
  form.cache_creation_usd_per_million = prefill.cache_creation_usd_per_million ?? 0
  form.request_usd = prefill.request_usd ?? null
  form.fast_enabled = prefill.fast_enabled ?? false
  form.fast_multiplier = prefill.fast_multiplier ?? 1
  form.long_context_enabled = prefill.long_context_enabled ?? false
  form.long_context_threshold_tokens = prefill.long_context_threshold_tokens ?? 0
  form.long_context_input_usd_per_million = prefill.long_context_input_usd_per_million ?? 0
  form.long_context_output_usd_per_million = prefill.long_context_output_usd_per_million ?? 0
  form.long_context_cache_read_usd_per_million = prefill.long_context_cache_read_usd_per_million ?? 0
  form.long_context_cache_creation_usd_per_million = prefill.long_context_cache_creation_usd_per_million ?? 0
  form.long_context_fast_unsupported = prefill.long_context_fast_unsupported ?? false
  fillOffPeakForm(prefill)
  modalOpen.value = true
}

function openCreateForRow(row: PriceDisplayRow) {
  openCreate({
    provider: row.provider || row.suggested_provider || row.owner || '',
    model: row.id,
  })
}

function openEdit(row: ModelPrice) {
  editingId.value = row.id
  form.provider = row.provider
  form.model = row.model
  form.input_usd_per_million = row.input_usd_per_million
  form.output_usd_per_million = row.output_usd_per_million
  form.cache_read_usd_per_million = row.cache_read_usd_per_million
  form.cache_creation_usd_per_million = row.cache_creation_usd_per_million
  form.request_usd = row.request_usd
  form.fast_enabled = row.fast_enabled
  form.fast_multiplier = row.fast_multiplier
  form.long_context_enabled = row.long_context_enabled
  form.long_context_threshold_tokens = row.long_context_threshold_tokens
  form.long_context_input_usd_per_million = row.long_context_input_usd_per_million
  form.long_context_output_usd_per_million = row.long_context_output_usd_per_million
  form.long_context_cache_read_usd_per_million = row.long_context_cache_read_usd_per_million
  form.long_context_cache_creation_usd_per_million = row.long_context_cache_creation_usd_per_million
  form.long_context_fast_unsupported = row.long_context_fast_unsupported
  fillOffPeakForm(row)
  modalOpen.value = true
}

function fillOffPeakForm(price: Partial<ModelPricePayload>) {
  form.off_peak_enabled = price.off_peak_enabled ?? false
  form.peak_input_usd_per_million = price.peak_input_usd_per_million ?? 0
  form.peak_output_usd_per_million = price.peak_output_usd_per_million ?? 0
  form.peak_cache_read_usd_per_million = price.peak_cache_read_usd_per_million ?? 0
  form.peak_cache_creation_usd_per_million = price.peak_cache_creation_usd_per_million ?? 0
  form.peak_request_usd = price.peak_request_usd ?? null
  form.long_context_peak_input_usd_per_million = price.long_context_peak_input_usd_per_million ?? 0
  form.long_context_peak_output_usd_per_million = price.long_context_peak_output_usd_per_million ?? 0
  form.long_context_peak_cache_read_usd_per_million = price.long_context_peak_cache_read_usd_per_million ?? 0
  form.long_context_peak_cache_creation_usd_per_million = price.long_context_peak_cache_creation_usd_per_million ?? 0
}

function normalizeNumberInput(value: string | number, fallback = 0): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function setPriceNumber(
  field: PriceFieldName | 'fast_multiplier',
  value: string | number,
) {
  form[field] = normalizeNumberInput(value)
}

function setRequestPrice(value: string | number) {
  form.request_usd = value === '' ? null : normalizeNumberInput(value)
}

function setPeakRequestPrice(value: string | number) {
  form.peak_request_usd = value === '' ? null : normalizeNumberInput(value)
}

function toggleOffPeak(enabled: boolean) {
  form.off_peak_enabled = enabled
  if (!enabled) return
  if (form.peak_request_usd === null) form.peak_request_usd = form.request_usd
  const fields = ['input', 'output', 'cache_read', 'cache_creation'] as const
  for (const field of fields) {
    const shortKey = `peak_${field}_usd_per_million` as PriceFieldName
    const longKey = `long_context_peak_${field}_usd_per_million` as PriceFieldName
    const baseKey = `${field}_usd_per_million` as PriceFieldName
    const longBaseKey = `long_context_${field}_usd_per_million` as PriceFieldName
    if (form[shortKey] === 0) form[shortKey] = form[baseKey]
    if (form[longKey] === 0) form[longKey] = form[longBaseKey]
  }
}

function setLongContextThreshold(value: string | number) {
  form.long_context_threshold_tokens = normalizeNumberInput(value)
}

function toggleLongContext(enabled: boolean) {
  form.long_context_enabled = enabled
  if (!enabled) {
    return
  }
  const longContextRates = [
    form.long_context_input_usd_per_million,
    form.long_context_output_usd_per_million,
    form.long_context_cache_read_usd_per_million,
    form.long_context_cache_creation_usd_per_million,
  ]
  if (longContextRates.every((value) => value === 0)) {
    form.long_context_input_usd_per_million = form.input_usd_per_million
    form.long_context_output_usd_per_million = form.output_usd_per_million
    form.long_context_cache_read_usd_per_million = form.cache_read_usd_per_million
    form.long_context_cache_creation_usd_per_million = form.cache_creation_usd_per_million
  }
}

async function savePrice() {
  const requestPriceMode = isRequestPriceForm.value
  const requestUSD = requestPriceMode && typeof form.request_usd === 'number' ? form.request_usd : null
  const payload: ModelPricePayload = {
    provider: form.provider.trim(),
    model: form.model.trim(),
    input_usd_per_million: form.input_usd_per_million,
    output_usd_per_million: form.output_usd_per_million,
    cache_read_usd_per_million: form.cache_read_usd_per_million,
    cache_creation_usd_per_million: form.cache_creation_usd_per_million,
    request_usd: requestUSD,
    fast_enabled: form.fast_enabled,
    fast_multiplier: form.fast_enabled ? form.fast_multiplier : 1,
    long_context_enabled: !requestPriceMode && form.long_context_enabled,
    long_context_threshold_tokens: requestPriceMode ? 0 : form.long_context_threshold_tokens,
    long_context_input_usd_per_million: requestPriceMode ? 0 : form.long_context_input_usd_per_million,
    long_context_output_usd_per_million: requestPriceMode ? 0 : form.long_context_output_usd_per_million,
    long_context_cache_read_usd_per_million: requestPriceMode ? 0 : form.long_context_cache_read_usd_per_million,
    long_context_cache_creation_usd_per_million: requestPriceMode ? 0 : form.long_context_cache_creation_usd_per_million,
    long_context_fast_unsupported: !requestPriceMode && form.fast_enabled && form.long_context_fast_unsupported,
    off_peak_enabled: form.off_peak_enabled,
    peak_input_usd_per_million: form.peak_input_usd_per_million,
    peak_output_usd_per_million: form.peak_output_usd_per_million,
    peak_cache_read_usd_per_million: form.peak_cache_read_usd_per_million,
    peak_cache_creation_usd_per_million: form.peak_cache_creation_usd_per_million,
    peak_request_usd: requestPriceMode ? form.peak_request_usd : null,
    long_context_peak_input_usd_per_million: form.long_context_peak_input_usd_per_million,
    long_context_peak_output_usd_per_million: form.long_context_peak_output_usd_per_million,
    long_context_peak_cache_read_usd_per_million: form.long_context_peak_cache_read_usd_per_million,
    long_context_peak_cache_creation_usd_per_million: form.long_context_peak_cache_creation_usd_per_million,
  }
  if (!payload.provider || !payload.model) {
    message.error(t('服务商和模型不能为空', 'Provider and model are required'))
    return
  }
  if (!Number.isFinite(payload.fast_multiplier) || payload.fast_multiplier <= 0) {
    message.error(t('FAST 倍率必须大于 0', 'FAST multiplier must be greater than 0'))
    return
  }
  if (requestPriceMode && requestUSD === null) {
    message.error(t('image 模型需要填写每次调用价格', 'Image models require a per-call price'))
    return
  }
  if (requestPriceMode && payload.off_peak_enabled && payload.peak_request_usd === null) {
    message.error(t('请填写高峰时段每次调用价格', 'Enter the peak per-call price'))
    return
  }
  if (payload.long_context_enabled && (!Number.isInteger(payload.long_context_threshold_tokens) || payload.long_context_threshold_tokens <= 0)) {
    message.error(t('长上下文 Token 阈值必须是大于 0 的整数', 'Long-context token threshold must be a positive integer'))
    return
  }
  isPriceSaving.value = true
  try {
    if (editingId.value === null) {
      await createModelPrice(payload)
      message.success(t('模型价格已创建', 'Model price created'))
    } else {
      await updateModelPrice(editingId.value, payload)
      message.success(t('模型价格已更新', 'Model price updated'))
    }
    modalOpen.value = false
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存模型价格失败', 'Failed to save model price'))
  } finally {
    isPriceSaving.value = false
  }
}

async function syncPrices() {
  isSyncing.value = true
  try {
    const result = await syncLitellmModelPrices()
    message.success(
      t(
        `同步完成：LiteLLM 价格 ${result.imported} 条，手动价格保留 ${result.skipped_manual} 条`,
        `Sync complete: ${result.imported} LiteLLM prices imported, ${result.skipped_manual} manual prices preserved`,
      ),
    )
    await refresh()
  } catch (error) {
    const detail = errorText(error, '同步模型价格失败', 'Failed to sync model prices')
    message.error(t(`${detail}。${liteLLMProxyHint.value}`, `${detail}. ${liteLLMProxyHint.value}`))
  } finally {
    isSyncing.value = false
  }
}

async function openProxySettings() {
  proxyModalOpen.value = true
  isProxyLoading.value = true
  try {
    const settings = await getLiteLLMProxySettings()
    proxyForm.enabled = settings.enabled
    proxyForm.proxy_url = settings.proxy_url
  } catch (error) {
    message.error(errorText(error, '加载代理配置失败', 'Failed to load proxy settings'))
  } finally {
    isProxyLoading.value = false
  }
}

async function saveProxySettings() {
  const payload: LiteLLMProxySettingsPayload = {
    enabled: proxyForm.enabled,
    proxy_url: proxyForm.proxy_url.trim(),
  }
  if (payload.enabled && !payload.proxy_url) {
    message.error(t('启用代理时必须填写代理地址', 'Proxy URL is required when proxy is enabled'))
    return
  }
  isProxySaving.value = true
  try {
    const saved = await updateLiteLLMProxySettings(payload)
    proxyForm.enabled = saved.enabled
    proxyForm.proxy_url = saved.proxy_url
    proxyModalOpen.value = false
    message.success(t('代理配置已保存', 'Proxy settings saved'))
  } catch (error) {
    message.error(errorText(error, '保存代理配置失败', 'Failed to save proxy settings'))
  } finally {
    isProxySaving.value = false
  }
}

function confirmDelete(row: ModelPrice) {
  dialog.warning({
    title: t('删除价格', 'Delete price'),
    content: `${row.provider} / ${row.model}`,
    positiveText: t('删除', 'Delete'),
    negativeText: t('取消', 'Cancel'),
    onPositiveClick: async () => {
      await deleteModelPrice(row.id)
      message.success(t('模型价格已删除', 'Model price deleted'))
      await refresh()
    },
  })
}

function formatPriceValue(value: number | null | undefined): string {
  return typeof value === 'number' ? String(value) : '-'
}

function priceTiers(row: PriceDisplayRow): PriceTier[] {
  const price = row.price
  if (!price) return [{ key: 'base', label: '-', prefix: '', multiplier: 1 }]
  const tiers: PriceTier[] = []
  const addTier = (key: string, label: string, prefix: PriceTier['prefix'], supportsFast = true) => {
    tiers.push({ key, label, prefix, multiplier: 1 })
    if (supportsFast && price.fast_enabled) {
      tiers.push({ key: `${key}-fast`, label: `${label} · FAST`, prefix, multiplier: price.fast_multiplier })
    }
  }
  addTier('base', price.off_peak_enabled ? t('常规 · 空闲', 'Standard · off-peak') : t('常规', 'Standard'), '')
  if (price.off_peak_enabled) addTier('peak', t('常规 · 高峰', 'Standard · peak'), 'peak_')
  if (price.long_context_enabled) {
    const fastSuffix = price.fast_enabled && price.long_context_fast_unsupported ? t(' · 无 FAST', ' · no FAST') : ''
    addTier('long', (price.off_peak_enabled ? t('长上下文 · 空闲', 'Long context · off-peak') : t('长上下文', 'Long context')) + fastSuffix, 'long_context_', !price.long_context_fast_unsupported)
    if (price.off_peak_enabled) addTier('long-peak', t('长上下文 · 高峰', 'Long context · peak') + fastSuffix, 'long_context_peak_', !price.long_context_fast_unsupported)
  }
  return tiers
}

function multipliedPriceValue(value: number | null | undefined, multiplier: number): string {
  if (typeof value !== 'number') return '-'
  return formatPriceValue(Number((value * multiplier).toPrecision(12)))
}

function tierPriceValue(row: PriceDisplayRow, tier: PriceTier, field: 'input' | 'output' | 'cache_read' | 'cache_creation' | 'request'): string {
  if (!row.price) return '-'
  if (field === 'request') {
    if (row.billing_unit !== 'request') return '-'
    const requestPrice = tier.prefix === 'peak_' ? row.price.peak_request_usd ?? row.price.request_usd : row.price.request_usd
    return multipliedPriceValue(requestPrice, tier.multiplier)
  }
  if (row.billing_unit === 'request') return '-'
  const key = `${tier.prefix}${field}_usd_per_million` as PriceFieldName
  return multipliedPriceValue(row.price[key], tier.multiplier)
}

async function loadCalendarYear() {
  isCalendarLoading.value = true
  try {
    applyCalendar(await getPricingHolidayCalendar(calendarYear.value))
  } catch (error) {
    message.error(errorText(error, '加载节假日失败', 'Failed to load holidays'))
  } finally {
    isCalendarLoading.value = false
  }
}

function applyCalendar(calendar: PricingHolidayCalendar) {
  calendarDays.value = calendar.days ?? (calendar.dates ?? []).map((date) => ({ date, name: '', is_off_day: true }))
  calendarSourceURL.value = calendar.source_url || `https://raw.githubusercontent.com/NateScarlet/holiday-cn/master/${calendar.year}.json`
  calendarConfigured.value = calendar.configured
  calendarSyncedAt.value = calendar.synced_at ?? null
  peakOnMakeupDays.value = calendar.peak_on_makeup_days ?? false
  peakPeriods.value = (calendar.peak_periods ?? []).map((period) => ({ ...period }))
}

async function openCalendar() {
  calendarModalOpen.value = true
  await loadCalendarYear()
}

async function syncCalendar() {
  isCalendarSyncing.value = true
  try {
    const pendingPeakOnMakeupDays = peakOnMakeupDays.value
    const pendingPeakPeriods = peakPeriods.value.map((period) => ({ ...period }))
    applyCalendar(await syncPricingHolidayCalendar(calendarYear.value))
    peakOnMakeupDays.value = pendingPeakOnMakeupDays
    peakPeriods.value = pendingPeakPeriods
    message.success(t('节假日已从 holiday-cn 同步', 'Holidays synced from holiday-cn'))
  } catch (error) {
    message.error(errorText(error, '同步节假日失败，已保留本地数据', 'Holiday sync failed; cached data was retained'))
  } finally {
    isCalendarSyncing.value = false
  }
}

async function saveCalendarSettings() {
  isCalendarSaving.value = true
  try {
    const settings = await updatePricingCalendarSettings(peakOnMakeupDays.value, peakPeriods.value)
    peakOnMakeupDays.value = settings.peak_on_makeup_days
    peakPeriods.value = settings.peak_periods
    message.success(t('峰谷计费选项已保存', 'Peak pricing settings saved'))
    calendarModalOpen.value = false
  } catch (error) {
    message.error(errorText(error, '保存计费选项失败', 'Failed to save pricing setting'))
  } finally {
    isCalendarSaving.value = false
  }
}

function billingUnitLabel(row: PriceDisplayRow): string {
  return row.billing_unit === 'request' ? t('按次', 'Per call') : t('按 Token', 'Per token')
}

function billingUnitVariant(row: PriceDisplayRow): 'secondary' | 'outline' {
  return row.billing_unit === 'request' ? 'secondary' : 'outline'
}

function priceStatusLabel(row: PriceDisplayRow): string {
  if (row.status === 'missing') {
    return t('未定价', 'Unpriced')
  }
  return row.status === 'litellm' ? 'LiteLLM' : t('手动', 'Manual')
}

function priceStatusVariant(row: PriceDisplayRow): 'destructive' | 'secondary' | 'outline' {
  if (row.status === 'missing') {
    return 'destructive'
  }
  return row.status === 'litellm' ? 'secondary' : 'outline'
}

onMounted(() => {
  void refresh()
})

</script>

<template>
  <section class="page price-page">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('模型价格', 'Model prices') }}</h1>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <Button variant="outline" :disabled="isSyncing" @click="syncPrices">
          <Spinner v-if="isSyncing" data-icon="inline-start" />
          <RefreshCw v-else data-icon="inline-start" />
          {{ t('同步 LiteLLM', 'Sync LiteLLM') }}
        </Button>
        <Button variant="outline" :disabled="isSyncing" @click="openProxySettings">
          <Settings2 data-icon="inline-start" />
          {{ t('代理配置', 'Proxy settings') }}
        </Button>
        <Button variant="outline" @click="openCalendar">
          <CalendarDays data-icon="inline-start" />
          {{ t('节假日日历', 'Holiday calendar') }}
        </Button>
        <Button @click="openCreate()">
          <Plus data-icon="inline-start" />
          {{ t('新增价格', 'Add price') }}
        </Button>
      </div>
    </div>

    <div class="metric-grid price-metrics">
      <Card v-for="metric in priceMetrics" :key="metric.key" class="price-metric-card border border-border ring-0">
        <CardHeader class="flex flex-row items-start justify-between gap-3">
          <div class="flex min-w-0 flex-col gap-1">
            <CardDescription>{{ metric.label }}</CardDescription>
            <CardTitle class="text-2xl tabular-nums">{{ metric.value }}</CardTitle>
          </div>
          <div class="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary" aria-hidden="true">
            <component :is="metric.icon" class="size-5" />
          </div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">
          {{ metric.footnote }}
        </CardContent>
      </Card>
    </div>

    <section class="panel table-panel price-table-panel">
      <div class="price-table-top">
        <Alert v-if="catalogNotice" class="price-alert">
          <CircleAlert />
          <AlertDescription>{{ catalogNotice }}</AlertDescription>
        </Alert>

        <div class="table-toolbar">
          <div class="price-toolbar-layout">
            <div class="price-filters">
              <span class="filter-label">{{ t('筛选', 'Filters') }}</span>

              <FilterCombobox
                class="provider-filter"
                :model-value="selectedProvider"
                :options="providerOptions"
                :placeholder="t('服务商', 'Provider')"
                :search-placeholder="t('搜索服务商', 'Search providers')"
                :empty-text="t('没有匹配的服务商', 'No matching providers')"
                :icon="Server"
                @update:model-value="handleProviderFilterChange"
              />

              <FilterCombobox
                class="status-filter"
                :model-value="selectedStatus"
                :options="statusOptions"
                :placeholder="t('状态', 'Status')"
                :icon="ListFilter"
                :searchable="false"
                @update:model-value="handleStatusFilterChange"
              />

              <InputGroup class="price-search">
                <InputGroupAddon>
                  <Search />
                </InputGroupAddon>
                <InputGroupInput
                  v-model="searchQuery"
                  :placeholder="t('搜索模型或服务商', 'Search models or providers')"
                />
              </InputGroup>
            </div>

            <span class="result-count">
              {{ t(`共 ${filteredPriceCount} / ${totalPriceCount} 条`, `${filteredPriceCount} / ${totalPriceCount} items`) }}
            </span>
          </div>
        </div>
      </div>

      <div class="price-table">
        <Table class="table-fixed">
          <TableHeader class="sticky top-0 bg-card">
            <TableRow>
              <TableHead class="price-model-column w-[190px]">{{ t('模型', 'Model') }}</TableHead>
              <TableHead class="price-secondary-column w-[70px]">{{ t('服务商', 'Provider') }}</TableHead>
              <TableHead class="w-[175px]">{{ t('费率档位', 'Rate tier') }}</TableHead>
              <TableHead class="w-[65px] whitespace-normal text-right">{{ t('每次 ($)', 'Per call ($)') }}</TableHead>
              <TableHead class="w-[75px] whitespace-normal text-right">{{ t('输入 ($/MTok)', 'Input ($/MTok)') }}</TableHead>
              <TableHead class="w-[75px] whitespace-normal text-right">{{ t('输出 ($/MTok)', 'Output ($/MTok)') }}</TableHead>
              <TableHead class="price-secondary-column w-[75px] whitespace-normal text-right">{{ t('缓存读 ($/MTok)', 'Cache read ($/MTok)') }}</TableHead>
              <TableHead class="price-secondary-column w-[75px] whitespace-normal text-right">{{ t('缓存写 ($/MTok)', 'Cache write ($/MTok)') }}</TableHead>
              <TableHead class="w-[70px]">
                <span class="sr-only">{{ t('操作', 'Actions') }}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <template v-if="isLoading && filteredPrices.length === 0">
              <TableRow v-for="rowIndex in 8" :key="`price-skeleton-${rowIndex}`">
                <TableCell v-for="columnIndex in 9" :key="columnIndex" :class="{ 'price-secondary-column': [2, 7, 8].includes(columnIndex) }">
                  <Skeleton class="h-4 w-full" />
                </TableCell>
              </TableRow>
            </template>

            <TableEmpty v-else-if="filteredPrices.length === 0" :colspan="9">
              {{ t('暂无模型价格', 'No model prices') }}
            </TableEmpty>

            <TableRow v-for="row in pagedPrices" v-else :key="row.key">
              <TableCell>
                <div class="model-cell">
                  <div class="model-title-row">
                    <span class="model-name" :title="row.id">{{ row.id }}</span>
                  </div>
                  <div v-if="row.name && row.name !== row.id" class="model-sub" :title="row.name">
                    {{ row.name }}
                  </div>
                  <div class="model-price-badges">
                    <Badge :variant="priceStatusVariant(row)">{{ priceStatusLabel(row) }}</Badge>
                    <Badge :variant="billingUnitVariant(row)">{{ billingUnitLabel(row) }}</Badge>
                    <Badge v-if="row.in_cpa" variant="secondary" :title="t('CPA 可用模型', 'CPA available model')">CPA</Badge>
                  </div>
                </div>
              </TableCell>
              <TableCell class="price-secondary-column">
                <div class="provider-cell">
                  <div class="provider-main" :title="row.provider || '-'">{{ row.provider || '-' }}</div>
                  <div v-if="row.owner && row.owner !== row.provider" class="model-sub">
                    {{ t('所有者', 'Owner') }}: {{ row.owner }}
                  </div>
                </div>
              </TableCell>
              <TableCell>
                <div class="rate-tier-stack">
                  <div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line" :title="tier.label">{{ tier.label }}</div>
                </div>
              </TableCell>
              <TableCell class="text-right tabular-nums"><div class="rate-tier-stack"><div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line">{{ tierPriceValue(row, tier, 'request') }}</div></div></TableCell>
              <TableCell class="text-right tabular-nums"><div class="rate-tier-stack"><div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line">{{ tierPriceValue(row, tier, 'input') }}</div></div></TableCell>
              <TableCell class="text-right tabular-nums"><div class="rate-tier-stack"><div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line">{{ tierPriceValue(row, tier, 'output') }}</div></div></TableCell>
              <TableCell class="price-secondary-column text-right tabular-nums"><div class="rate-tier-stack"><div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line">{{ tierPriceValue(row, tier, 'cache_read') }}</div></div></TableCell>
              <TableCell class="price-secondary-column text-right tabular-nums"><div class="rate-tier-stack"><div v-for="tier in priceTiers(row)" :key="tier.key" class="rate-tier-line">{{ tierPriceValue(row, tier, 'cache_creation') }}</div></div></TableCell>
              <TableCell class="text-right">
                <div class="flex items-center justify-end">
                  <DropdownMenu v-if="row.price">
                    <DropdownMenuTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        class="price-actions-trigger"
                        :aria-label="t(`打开 ${row.id} 的操作菜单`, `Open actions for ${row.id}`)"
                      >
                        <MoreHorizontal />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" :side-offset="4" class="w-40">
                      <DropdownMenuGroup>
                        <DropdownMenuItem @select="openEdit(row.price)">
                          <Pencil />
                          <span>{{ t('编辑', 'Edit') }}</span>
                        </DropdownMenuItem>
                        <DropdownMenuItem variant="destructive" @select="confirmDelete(row.price)">
                          <Trash2 />
                          <span>{{ t('删除', 'Delete') }}</span>
                        </DropdownMenuItem>
                      </DropdownMenuGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                  <Button v-else variant="ghost" size="sm" @click="openCreateForRow(row)">{{ t('设价', 'Set') }}</Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <div v-if="isLoading && filteredPrices.length > 0" class="table-loading-overlay">
          <Spinner />
        </div>
      </div>

      <TablePaginationFooter
        :page="page"
        :page-size="pageSize"
        :total="filteredPriceCount"
        @update:page="updatePricePage"
        @update:page-size="updatePricePageSize"
      />
    </section>

    <Dialog v-model:open="modalOpen">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
        <form class="flex flex-col gap-5" @submit.prevent="savePrice">
          <DialogHeader>
            <DialogTitle>
              {{ editingId === null ? t('新增价格', 'Add price') : t('编辑价格', 'Edit price') }}
            </DialogTitle>
            <DialogDescription>{{ priceSaveHint }}</DialogDescription>
          </DialogHeader>

          <FieldGroup class="form-grid">
            <Field>
              <FieldLabel for="price-provider">{{ t('服务商', 'Provider') }}</FieldLabel>
              <Input id="price-provider" v-model="form.provider" required />
            </Field>
            <Field>
              <FieldLabel for="price-model">{{ t('模型', 'Model') }}</FieldLabel>
              <Input id="price-model" v-model="form.model" required />
            </Field>
            <Field orientation="horizontal" class="wide-form-item long-context-switch-row">
              <FieldContent>
                <FieldLabel for="price-fast-enabled">{{ t('支持 FAST 模式', 'Supports FAST mode') }}</FieldLabel>
                <FieldDescription>{{ t('仅支持 FAST 的模型才会按倍率计费并展示 FAST 档位。', 'Only supported models use the multiplier and show FAST rate tiers.') }}</FieldDescription>
              </FieldContent>
              <Switch id="price-fast-enabled" v-model="form.fast_enabled" />
            </Field>
            <Field v-if="form.fast_enabled" class="wide-form-item">
              <FieldLabel for="price-fast-multiplier">{{ t('FAST 倍率', 'FAST multiplier') }}</FieldLabel>
              <Input
                id="price-fast-multiplier"
                type="number"
                min="0.01"
                step="0.01"
                :model-value="form.fast_multiplier"
                @update:model-value="setPriceNumber('fast_multiplier', $event)"
              />
            </Field>
            <Field v-if="isRequestPriceForm" class="wide-form-item">
              <FieldLabel for="price-per-request">{{ t('每次调用价格 USD', 'Per-call price USD') }}</FieldLabel>
              <Input
                id="price-per-request"
                type="number"
                min="0"
                step="any"
                :model-value="form.request_usd ?? ''"
                :placeholder="t('例如：0.04', 'Example: 0.04')"
                @update:model-value="setRequestPrice"
              />
            </Field>
            <template v-else>
              <Field>
                <FieldLabel for="price-input">{{ t('输入价格', 'Input price') }}</FieldLabel>
                <Input
                  id="price-input"
                  type="number"
                  min="0"
                  step="any"
                  :model-value="form.input_usd_per_million"
                  @update:model-value="setPriceNumber('input_usd_per_million', $event)"
                />
              </Field>
              <Field>
                <FieldLabel for="price-output">{{ t('输出价格', 'Output price') }}</FieldLabel>
                <Input
                  id="price-output"
                  type="number"
                  min="0"
                  step="any"
                  :model-value="form.output_usd_per_million"
                  @update:model-value="setPriceNumber('output_usd_per_million', $event)"
                />
              </Field>
              <Field>
                <FieldLabel for="price-cache-read">{{ t('缓存读价格', 'Cache read price') }}</FieldLabel>
                <Input
                  id="price-cache-read"
                  type="number"
                  min="0"
                  step="any"
                  :model-value="form.cache_read_usd_per_million"
                  @update:model-value="setPriceNumber('cache_read_usd_per_million', $event)"
                />
              </Field>
              <Field>
                <FieldLabel for="price-cache-write">{{ t('缓存写价格', 'Cache write price') }}</FieldLabel>
                <Input
                  id="price-cache-write"
                  type="number"
                  min="0"
                  step="any"
                  :model-value="form.cache_creation_usd_per_million"
                  @update:model-value="setPriceNumber('cache_creation_usd_per_million', $event)"
                />
              </Field>
              <div class="wide-form-item long-context-panel">
                <Field orientation="horizontal" class="long-context-switch-row">
                  <FieldContent>
                    <FieldLabel for="price-long-context-enabled">
                      {{ t('启用长上下文费率', 'Enable long-context rates') }}
                    </FieldLabel>
                    <FieldDescription>
                      {{
                        t(
                          '输入上下文 Token 超出阈值后，整条请求使用长上下文费率；FAST 请求默认继续应用倍率，不支持时可在下方标记。',
                          'When input context exceeds the threshold, the entire request uses long-context rates. FAST requests apply the multiplier by default and can be marked unsupported below.',
                        )
                      }}
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    id="price-long-context-enabled"
                    :model-value="form.long_context_enabled"
                    @update:model-value="toggleLongContext"
                  />
                </Field>

                <FieldGroup v-if="form.long_context_enabled" class="long-context-rate-grid">
                  <Field class="wide-form-item">
                    <FieldLabel for="price-long-context-threshold">
                      {{ t('Token 阈值', 'Token threshold') }}
                    </FieldLabel>
                    <Input
                      id="price-long-context-threshold"
                      type="number"
                      min="1"
                      step="1"
                      :model-value="form.long_context_threshold_tokens"
                      :placeholder="t('例如：272000', 'Example: 272000')"
                      @update:model-value="setLongContextThreshold"
                    />
                    <FieldDescription>
                      {{ t('仅统计完整输入上下文（含缓存 Token），输出 Token 不参与阈值判断。', 'Counts the full input context including cached tokens; output tokens do not affect the threshold.') }}
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel for="price-long-context-input">
                      {{ t('长上下文输入价格 ($/MTok)', 'Long-context input ($/MTok)') }}
                    </FieldLabel>
                    <Input
                      id="price-long-context-input"
                      type="number"
                      min="0"
                      step="any"
                      :model-value="form.long_context_input_usd_per_million"
                      @update:model-value="setPriceNumber('long_context_input_usd_per_million', $event)"
                    />
                  </Field>
                  <Field>
                    <FieldLabel for="price-long-context-output">
                      {{ t('长上下文输出价格 ($/MTok)', 'Long-context output ($/MTok)') }}
                    </FieldLabel>
                    <Input
                      id="price-long-context-output"
                      type="number"
                      min="0"
                      step="any"
                      :model-value="form.long_context_output_usd_per_million"
                      @update:model-value="setPriceNumber('long_context_output_usd_per_million', $event)"
                    />
                  </Field>
                  <Field>
                    <FieldLabel for="price-long-context-cache-read">
                      {{ t('长上下文缓存读价格 ($/MTok)', 'Long-context cache read ($/MTok)') }}
                    </FieldLabel>
                    <Input
                      id="price-long-context-cache-read"
                      type="number"
                      min="0"
                      step="any"
                      :model-value="form.long_context_cache_read_usd_per_million"
                      @update:model-value="setPriceNumber('long_context_cache_read_usd_per_million', $event)"
                    />
                  </Field>
                  <Field>
                    <FieldLabel for="price-long-context-cache-write">
                      {{ t('长上下文缓存写价格 ($/MTok)', 'Long-context cache write ($/MTok)') }}
                    </FieldLabel>
                    <Input
                      id="price-long-context-cache-write"
                      type="number"
                      min="0"
                      step="any"
                      :model-value="form.long_context_cache_creation_usd_per_million"
                      @update:model-value="setPriceNumber('long_context_cache_creation_usd_per_million', $event)"
                    />
                  </Field>
                  <Field
                    v-if="showLongContextFastUnsupported"
                    orientation="horizontal"
                    class="wide-form-item long-context-fast-row"
                  >
                    <FieldContent>
                      <FieldLabel for="price-long-context-fast-unsupported">
                        {{ t('长上下文支持 FAST 模式', 'FAST mode available for long context') }}
                      </FieldLabel>
                      <FieldDescription>
                        {{
                          t(
                            '开启后，长上下文请求也可叠加 FAST 倍率；关闭时仅短上下文支持 FAST。',
                            'When enabled, long-context requests also use the FAST multiplier; otherwise only short-context requests do.',
                          )
                        }}
                      </FieldDescription>
                    </FieldContent>
                    <Switch
                      id="price-long-context-fast-unsupported"
                      :model-value="!form.long_context_fast_unsupported"
                      @update:model-value="form.long_context_fast_unsupported = !$event"
                    />
                  </Field>
                </FieldGroup>
              </div>
            </template>
            <div class="wide-form-item long-context-panel">
              <Field orientation="horizontal" class="long-context-switch-row">
                <FieldContent>
                  <FieldLabel for="price-off-peak-enabled">{{ t('启用峰谷分段计费', 'Enable peak/off-peak pricing') }}</FieldLabel>
                  <FieldDescription>
                    {{ t('常规费率为空闲价；开启后另填高峰价。北京时间高峰时段与调休日计费方式可在节假日日历中设置；法定休息日和未公布年份使用空闲价。', 'Regular rates are off-peak rates. Enter separate peak rates when enabled. Configure Beijing-time peak hours and makeup days in the holiday calendar; published off-days and unpublished years use off-peak rates.') }}
                  </FieldDescription>
                </FieldContent>
                <Switch id="price-off-peak-enabled" :model-value="form.off_peak_enabled" @update:model-value="toggleOffPeak" />
              </Field>
              <FieldGroup v-if="form.off_peak_enabled" class="long-context-rate-grid">
                <Field v-if="isRequestPriceForm" class="wide-form-item">
                  <FieldLabel for="price-peak-request">{{ t('高峰时段每次调用价格 USD', 'Peak per-call price USD') }}</FieldLabel>
                  <Input id="price-peak-request" type="number" min="0" step="any" :model-value="form.peak_request_usd ?? ''" @update:model-value="setPeakRequestPrice" />
                </Field>
                <template v-else>
                  <Field v-for="field in peakRateFields" :key="field.key">
                    <FieldLabel :for="`price-${field.key}`">{{ t(field.zh, field.en) }}</FieldLabel>
                    <Input :id="`price-${field.key}`" type="number" min="0" step="any" :model-value="form[field.key]" @update:model-value="setPriceNumber(field.key, $event)" />
                  </Field>
                  <template v-if="form.long_context_enabled">
                    <Field v-for="field in longPeakRateFields" :key="field.key">
                      <FieldLabel :for="`price-${field.key}`">{{ t(field.zh, field.en) }}</FieldLabel>
                      <Input :id="`price-${field.key}`" type="number" min="0" step="any" :model-value="form[field.key]" @update:model-value="setPriceNumber(field.key, $event)" />
                    </Field>
                  </template>
                </template>
              </FieldGroup>
            </div>
          </FieldGroup>

          <DialogFooter>
            <Button type="button" variant="outline" :disabled="isPriceSaving" @click="modalOpen = false">
              {{ t('取消', 'Cancel') }}
            </Button>
            <Button type="submit" :disabled="isPriceSaving">
              <Spinner v-if="isPriceSaving" data-icon="inline-start" />
              {{ t('保存', 'Save') }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="proxyModalOpen">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-lg">
        <form class="flex flex-col gap-5" @submit.prevent="saveProxySettings">
          <DialogHeader>
            <DialogTitle>{{ t('LiteLLM 代理配置', 'LiteLLM proxy settings') }}</DialogTitle>
            <DialogDescription>
              {{ t('为 LiteLLM 价格同步配置网络代理。', 'Configure a network proxy for LiteLLM price synchronization.') }}
            </DialogDescription>
          </DialogHeader>

          <Alert>
            <CircleAlert />
            <AlertDescription>{{ liteLLMProxyHint }}</AlertDescription>
          </Alert>

          <FieldGroup>
            <Field orientation="horizontal" class="proxy-switch-row">
              <FieldContent>
                <FieldLabel for="litellm-proxy-enabled">{{ t('使用代理', 'Use proxy') }}</FieldLabel>
                <FieldDescription>
                  {{ t('同步时通过下方代理地址访问 GitHub。', 'Use the proxy URL below when synchronizing from GitHub.') }}
                </FieldDescription>
              </FieldContent>
              <Switch
                id="litellm-proxy-enabled"
                v-model="proxyForm.enabled"
                :disabled="isProxyLoading || isProxySaving"
              />
            </Field>
            <Field :data-disabled="!proxyForm.enabled || isProxyLoading || isProxySaving || undefined">
              <FieldLabel for="litellm-proxy-url">{{ t('代理地址', 'Proxy URL') }}</FieldLabel>
              <Input
                id="litellm-proxy-url"
                v-model="proxyForm.proxy_url"
                :disabled="!proxyForm.enabled || isProxyLoading || isProxySaving"
                :placeholder="t('http://127.0.0.1:7890 或 socks5://127.0.0.1:1080', 'http://127.0.0.1:7890 or socks5://127.0.0.1:1080')"
              />
            </Field>
          </FieldGroup>

          <DialogFooter>
            <Button type="button" variant="outline" :disabled="isProxySaving" @click="proxyModalOpen = false">
              {{ t('取消', 'Cancel') }}
            </Button>
            <Button type="submit" :disabled="isProxySaving">
              <Spinner v-if="isProxySaving" data-icon="inline-start" />
              {{ t('保存', 'Save') }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="calendarModalOpen">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-xl">
        <form class="flex flex-col gap-5" @submit.prevent="saveCalendarSettings">
          <DialogHeader>
            <DialogTitle>{{ t('峰谷计费节假日日历', 'Peak pricing holiday calendar') }}</DialogTitle>
            <DialogDescription>{{ t('从 holiday-cn 获取放假与调休上班日期；后台每日自动刷新当前年份，失败时保留本地缓存。未公布年份全天使用空闲费率。', 'Fetch off-days and makeup workdays from holiday-cn. The server refreshes the current year daily and retains cached data on failure. Unpublished years use off-peak rates all day.') }}</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel for="pricing-calendar-year">{{ t('年份', 'Year') }}</FieldLabel>
              <InputGroup>
                <InputGroupInput id="pricing-calendar-year" v-model.number="calendarYear" type="number" min="2000" max="2100" step="1" :disabled="isCalendarLoading || isCalendarSaving || isCalendarSyncing" />
                <InputGroupAddon align="inline-end">
                  <Button type="button" variant="ghost" size="sm" :disabled="isCalendarLoading || isCalendarSaving || isCalendarSyncing" @click="loadCalendarYear">{{ t('查看缓存', 'View cache') }}</Button>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <Field>
              <div class="flex items-center justify-between gap-2">
                <FieldLabel>{{ t('日期数据', 'Calendar data') }}</FieldLabel>
                <Button type="button" variant="outline" size="sm" :disabled="isCalendarLoading || isCalendarSyncing || isCalendarSaving" @click="syncCalendar">
                  <Spinner v-if="isCalendarSyncing" data-icon="inline-start" />
                  <RefreshCw v-else data-icon="inline-start" />
                  {{ t('从 holiday-cn 同步', 'Sync from holiday-cn') }}
                </Button>
              </div>
              <FieldDescription>{{ calendarSyncedAt ? t(`最近同步：${formatDateTime(calendarSyncedAt)}`, `Last synced: ${formatDateTime(calendarSyncedAt)}`) : calendarConfigured ? t('正在使用本地预置数据，尚未从 holiday-cn 同步。', 'Using preloaded local data; not yet synced from holiday-cn.') : t('该年份尚无已公布的节假日数据，全天使用空闲价。', 'No published calendar for this year; off-peak rates apply all day.') }}</FieldDescription>
              <div class="max-h-64 overflow-y-auto rounded-lg border border-border">
                <Table>
                  <TableHeader class="sticky top-0 bg-card">
                    <TableRow>
                      <TableHead>{{ t('日期', 'Date') }}</TableHead>
                      <TableHead>{{ t('节日', 'Holiday') }}</TableHead>
                      <TableHead>{{ t('类型', 'Type') }}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableEmpty v-if="calendarDays.length === 0" :colspan="3">{{ t('暂无已公布日期', 'No published dates') }}</TableEmpty>
                    <TableRow v-for="day in calendarDays" :key="day.date">
                      <TableCell class="font-mono">{{ day.date }}</TableCell>
                      <TableCell>{{ day.name || '-' }}</TableCell>
                      <TableCell><Badge :variant="day.is_off_day ? 'secondary' : 'outline'">{{ day.is_off_day ? t('休息日', 'Off-day') : t('调休上班', 'Makeup workday') }}</Badge></TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </div>
            </Field>
            <Field>
              <div class="flex items-center justify-between gap-2">
                <FieldLabel>{{ t('高峰时段（北京时间）', 'Peak hours (Beijing time)') }}</FieldLabel>
                <Button type="button" variant="outline" size="sm" :disabled="peakPeriods.length >= 8 || isCalendarLoading || isCalendarSaving" @click="peakPeriods.push({ start: '', end: '' })">
                  <Plus data-icon="inline-start" />{{ t('添加时段', 'Add period') }}
                </Button>
              </div>
              <FieldDescription>{{ t('仅工作日及开启计费的调休上班日适用；按开始时间排序，时段不可重叠或跨日。', 'Applies only on workdays and opted-in makeup days. Order by start time; periods cannot overlap or cross midnight.') }}</FieldDescription>
              <div v-for="(period, index) in peakPeriods" :key="index" class="flex items-center gap-2">
                <TimePicker v-model="period.start" :aria-label="t(`第 ${index + 1} 段开始时间`, `Period ${index + 1} start`)" :disabled="isCalendarLoading || isCalendarSaving" />
                <span class="text-muted-foreground">–</span>
                <TimePicker v-model="period.end" :aria-label="t(`第 ${index + 1} 段结束时间`, `Period ${index + 1} end`)" :disabled="isCalendarLoading || isCalendarSaving" />
                <Button type="button" variant="ghost" size="icon-sm" :aria-label="t(`删除第 ${index + 1} 段`, `Remove period ${index + 1}`)" :disabled="peakPeriods.length <= 1 || isCalendarLoading || isCalendarSaving" @click="peakPeriods.splice(index, 1)"><Trash2 /></Button>
              </div>
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel for="pricing-peak-on-makeup-days">{{ t('调休日按高峰价计算', 'Charge peak rates on makeup workdays') }}</FieldLabel>
                <FieldDescription>{{ t('仅对数据源标记的调休上班日生效，并且仅在上方设置的高峰时段使用高峰价；普通周末仍为空闲价。', 'Only published makeup workdays use peak rates during the configured hours. Ordinary weekends remain off-peak.') }}</FieldDescription>
              </FieldContent>
              <Switch id="pricing-peak-on-makeup-days" v-model="peakOnMakeupDays" :disabled="isCalendarLoading || isCalendarSyncing || isCalendarSaving" />
            </Field>
            <a v-if="calendarSourceURL" :href="calendarSourceURL" target="_blank" rel="noopener noreferrer" class="text-sm text-primary underline-offset-4 hover:underline">{{ t('查看年份数据源', 'View yearly data source') }}</a>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" @click="calendarModalOpen = false">{{ t('取消', 'Cancel') }}</Button>
            <Button type="submit" :disabled="isCalendarLoading || isCalendarSyncing || isCalendarSaving">{{ t('保存选项', 'Save setting') }}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </section>
</template>
<style scoped>
.price-metrics {
  grid-template-columns: repeat(4, minmax(150px, 1fr));
}

.price-metric-card {
  min-width: 0;
}

.rate-tier-stack {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.rate-tier-line {
  min-height: 1.5rem;
  overflow: hidden;
  line-height: 1.5rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1100px) {
  .price-secondary-column {
    display: none;
  }

  .price-model-column {
    width: 170px;
  }
}

.price-metric-card :deep([data-slot="card-header"]) {
  padding-bottom: 10px;
}

.price-metric-card :deep([data-slot="card-content"]) {
  padding-top: 0;
}

.price-table-panel,
.price-table {
  max-width: 100%;
  min-width: 0;
  min-height: 0;
}

.price-table-panel {
  overflow: hidden;
}

.price-table-top {
  display: grid;
  gap: 8px;
}

.table-toolbar {
  padding: 12px 16px;
  border: 1px solid var(--border);
  border-bottom: 0;
  border-radius: var(--radius) var(--radius) 0 0;
  background: var(--card);
}

.price-toolbar-layout {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-width: 0;
}

.price-filters {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 100%;
}

.filter-label,
.result-count {
  color: var(--muted-foreground);
  font-size: 12px;
  white-space: nowrap;
}

.provider-filter {
  width: 210px;
}

.status-filter {
  width: 160px;
}

.price-search {
  width: min(300px, 100%);
}

.price-table {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--border);
  border-top: 0;
  background: var(--card);
}

.price-table :deep([data-slot="table-container"]) {
  max-height: max(240px, calc(100dvh - 420px));
  overflow: auto;
  overscroll-behavior: contain;
}

.table-loading-overlay {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: color-mix(in oklch, var(--background) 62%, transparent);
}

.price-actions-trigger {
  color: var(--muted-foreground);
}

.model-cell,
.provider-cell {
  min-width: 0;
}

.model-title-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.model-price-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}

.model-name,
.provider-main {
  max-width: 100%;
  min-width: 0;
  overflow: hidden;
  color: var(--foreground);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-sub {
  margin-top: 2px;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.wide-form-item {
  grid-column: 1 / -1;
}

.long-context-panel {
  display: grid;
  gap: 16px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--card);
}

.long-context-switch-row {
  gap: 20px;
}

.long-context-rate-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
}

.long-context-fast-row {
  gap: 20px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}

.proxy-switch-row {
  gap: 20px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: color-mix(in oklch, var(--muted) 45%, transparent);
}

@media (min-width: 861px) {
  .price-page {
    grid-template-rows: auto auto minmax(0, 1fr);
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }

  .price-table-panel {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    min-height: 0;
  }

  .price-table {
    height: 100%;
  }

  .price-table :deep([data-slot="table-container"]) {
    height: 100%;
    max-height: none;
  }
}

@media (max-width: 980px) {
  .price-toolbar-layout {
    align-items: flex-start;
    flex-direction: column;
  }

  .provider-filter {
    width: min(200px, calc(100vw - 32px));
  }

  .status-filter {
    width: min(170px, calc(100vw - 32px));
  }

  .price-search {
    width: min(260px, calc(100vw - 32px));
  }
}

@media (max-width: 620px) {
  .price-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .form-grid {
    grid-template-columns: 1fr;
  }

  .wide-form-item {
    grid-column: auto;
  }

  .long-context-rate-grid {
    grid-template-columns: 1fr;
  }

  .price-filters {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
  }

  .filter-label {
    display: none;
  }

  .provider-filter,
  .status-filter,
  .price-search {
    width: 100%;
  }

  .price-search {
    grid-column: 1 / -1;
  }

}
</style>
