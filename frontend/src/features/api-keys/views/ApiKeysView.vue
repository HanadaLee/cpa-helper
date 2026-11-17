<script setup lang="ts">
import type { Component } from 'vue'
import { computed, onMounted, ref, watch } from 'vue'
import TutorialGuide from '@/features/tutorials/components/TutorialGuide.vue'
import { toast } from 'vue-sonner'
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
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
  Combobox,
  ComboboxAnchor,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxItemIndicator,
  ComboboxList,
  ComboboxTrigger,
} from '@/components/ui/combobox'
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { useConfirmDialog } from '@/shared/ui/confirm-dialog'
import {
  Activity,
  Check,
  ChevronsUpDown,
  CircleDollarSign,
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Layers3,
  MoreHorizontal,
  Pencil,
  Power,
  PowerOff,
  Plus,
  RefreshCw,
  Send,
  Trash2,
} from '@lucide/vue'

import {
  createApiKey,
  deleteApiKey,
  disableApiKey,
  enableApiKey,
  getModelRequestGuide,
  listApiKeys,
  testModelRequest,
  updateApiKey,
} from '@/features/api-keys/api/apiKeysApi'
import { listAvailableModels } from '@/features/models/api/availableModelsApi'
import { getCurrentUserQuota } from '@/features/users/api/usersApi'
import { getUsageOverview } from '@/features/usage/api/usageApi'
import type {
  AvailableModel,
  AvailableModelsResponse,
  ModelRequestEndpoint,
  ModelRequestGuide,
  ModelRequestTestResponse,
  UsageSummary,
  UserApiKeySummary,
  UserQuotaStatus,
} from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { copyToClipboard } from '@/shared/utils/clipboard'
import { formatCompact, formatDateTime, formatInteger, formatUsd } from '@/shared/utils/format'

const message = toast
const dialog = useConfirmDialog()
const { copiedText, currentLanguage, errorText, t } = useI18n()
const isLoading = ref(false)
const isSaving = ref(false)
const apiKeys = ref<UserApiKeySummary[]>([])
const usageSummary = ref<UsageSummary | null>(null)
const quotaStatus = ref<UserQuotaStatus | null>(null)
const modelRequestGuide = ref<ModelRequestGuide | null>(null)
const availableModels = ref<AvailableModelsResponse | null>(null)
const editorVisible = ref(false)
const requestTestVisible = ref(false)
const requestTestApiKey = ref<UserApiKeySummary | null>(null)
const requestEndpoint = ref<ModelRequestEndpoint>('chat_completions')
const requestTestEndpointKey = ref('default')
type PublicRequestURLType = 'base' | ModelRequestEndpoint
const publicRequestURLType = ref<PublicRequestURLType>('base')
const requestTestModel = ref<string | null>(null)
const requestTestMessageDefaults = {
  zh: '请用一句中文回复：连接测试成功。',
  en: 'Reply in one English sentence: connection test succeeded.',
} as const
const requestTestMessage = ref(requestTestMessageDefaults[currentLanguage.value])
const requestTestResult = ref<ModelRequestTestResponse | null>(null)
const requestTestError = ref<string | null>(null)
const isAvailableModelsLoading = ref(false)
const isRequestTesting = ref(false)
const editingApiKeyHash = ref<string | null>(null)
const apiKeyDescription = ref('VSCode')
const generatedApiKey = ref<string | null>(null)
const generatedApiKeyHash = ref<string | null>(null)
const visibleApiKeyHashes = ref<Set<string>>(new Set())
const apiKeyActionHashes = ref<Set<string>>(new Set())
const page = ref(1)
const pageSize = 12

const requestLoadingText = computed(() => t('加载中', 'Loading'))

interface RequestEndpointOption {
  label: string
  value: ModelRequestEndpoint
  path: string
  urlLabel: string
}

interface PublicRequestEndpoint {
  key: string
  label: string
  baseURL: string
}

interface PublicRequestURLTypeOption {
  label: string
  ariaLabel: string
  value: PublicRequestURLType
  path: string
}

const chatCompletionsEndpointOption = computed<RequestEndpointOption>(() => ({
  label: t('聊天补全', 'Chat Completions'),
  value: 'chat_completions',
  path: '/chat/completions',
  urlLabel: t('聊天补全 URL', 'Chat Completions URL'),
}))

const requestEndpointOptions = computed<RequestEndpointOption[]>(() => [
  chatCompletionsEndpointOption.value,
  {
    label: t('Responses 响应', 'Responses'),
    value: 'responses',
    path: '/responses',
    urlLabel: t('Responses 响应 URL', 'Responses URL'),
  },
  {
    label: t('Claude 消息', 'Claude Messages'),
    value: 'claude_messages',
    path: '/messages',
    urlLabel: t('Claude 消息 URL', 'Claude Messages URL'),
  },
])

interface ApiKeyMetricCard {
  key: string
  label: string
  value: string
  footnote: string
  tone: 'primary' | 'blue' | 'purple' | 'green'
  icon: Component
}

const apiKeyMetrics = computed<ApiKeyMetricCard[]>(() => {
  const summary = usageSummary.value
  const todayRequests = summary?.total_records ?? 0
  const failedToday = summary?.failed_records ?? 0
  const todayCost = summary?.estimated_cost_usd ?? 0
  const todayTokens = summary?.total_tokens ?? 0
  const quota = quotaStatus.value
  return [
    {
      key: 'keys',
      label: t('API 密钥', 'API keys'),
      value: formatInteger(apiKeys.value.length),
      footnote: t('当前账号', 'Current account'),
      tone: 'primary',
      icon: KeyRound,
    },
    {
      key: 'requests',
      label: t('今日请求', 'Requests today'),
      value: formatInteger(todayRequests),
      footnote: t(`失败 ${formatInteger(failedToday)}`, `${formatInteger(failedToday)} failed`),
      tone: 'blue',
      icon: Activity,
    },
    {
      key: 'tokens',
      label: t('今日 Token', 'Tokens today'),
      value: formatCompact(todayTokens),
      footnote: t('当前账号用量', 'Current account usage'),
      tone: 'purple',
      icon: Layers3,
    },
    {
      key: 'cost',
      label: t('今日费用', 'Cost today'),
      value: formatUsd(todayCost),
      footnote: t('按现价估算', 'Estimated at current prices'),
      tone: 'green',
      icon: CircleDollarSign,
    },
    {
      key: 'quota',
      label: t('可用余额', 'Available balance'),
      value: quotaValueText(quota),
      footnote: quotaFootnote(quota),
      tone: quota?.paused ? 'purple' : 'green',
      icon: CircleDollarSign,
    },
  ]
})

const canCreateApiKey = computed(() => quotaStatus.value?.can_create_keys ?? true)

const publicRequestURLTypeOptions = computed<PublicRequestURLTypeOption[]>(() => [
  {
    label: t('基础', 'Base'),
    ariaLabel: t('基础 URL', 'Base URL'),
    value: 'base',
    path: '',
  },
  ...requestEndpointOptions.value.map((option) => ({
    label:
      option.value === 'chat_completions'
        ? t('聊天', 'Chat')
        : option.value === 'responses'
          ? 'Responses'
          : 'Claude',
    ariaLabel: option.label,
    value: option.value,
    path: option.path,
  })),
])
const publicRequestURLTypeMeta = computed(
  () =>
    publicRequestURLTypeOptions.value.find((option) => option.value === publicRequestURLType.value) ??
    publicRequestURLTypeOptions.value[0],
)
const publicRequestEndpoints = computed<PublicRequestEndpoint[]>(() => {
  const defaultBaseURL = modelRequestGuide.value?.openai_base_url
  const extraEndpoints = modelRequestGuide.value?.extra_endpoints ?? []
  return [
    {
      key: 'default',
      label: t('默认 Endpoint', 'Default endpoint'),
      baseURL: defaultBaseURL || requestLoadingText.value,
    },
    ...extraEndpoints.map((endpoint, index) => ({
      key: `extra-${index}`,
      label: endpoint.description,
      baseURL: modelRequestOpenAIBaseURL(endpoint.url),
    })),
  ]
})
const selectedRequestTestEndpoint = computed(() =>
  publicRequestEndpoints.value.find((endpoint) => endpoint.key === requestTestEndpointKey.value) ?? publicRequestEndpoints.value[0],
)
const requestBaseURL = computed(() => selectedRequestTestEndpoint.value?.baseURL ?? requestLoadingText.value)
const publicRequestEndpointRows = computed(() => {
  const path = publicRequestURLTypeMeta.value?.path ?? ''
  return publicRequestEndpoints.value.map((endpoint) => ({
    ...endpoint,
    url:
      endpoint.baseURL === requestLoadingText.value
        ? endpoint.baseURL
        : `${endpoint.baseURL.replace(/\/$/, '')}${path}`,
  }))
})

function updatePublicRequestURLType(value: unknown) {
  if (publicRequestURLTypeOptions.value.some((option) => option.value === value)) {
    publicRequestURLType.value = value as PublicRequestURLType
  }
}

function updateRequestEndpoint(value: unknown) {
  if (requestEndpointOptions.value.some((option) => option.value === value)) {
    requestEndpoint.value = value as ModelRequestEndpoint
  }
}

function updateRequestTestEndpointKey(value: unknown) {
  if (publicRequestEndpoints.value.some((endpoint) => endpoint.key === value)) {
    requestTestEndpointKey.value = value as string
  }
}

const requestEndpointMeta = computed(
  () =>
    requestEndpointOptions.value.find((option) => option.value === requestEndpoint.value) ??
    chatCompletionsEndpointOption.value,
)
const requestEndpointURL = computed(() => {
  const baseURL = requestBaseURL.value
  if (baseURL === requestLoadingText.value) {
    return requestLoadingText.value
  }
  return `${baseURL.replace(/\/$/, '')}${requestEndpointMeta.value.path}`
})
const requestEndpointURLLabel = computed(() => requestEndpointMeta.value.urlLabel)
const requestTestApiKeyText = computed(() => requestTestApiKey.value?.api_key || t('<你的 API KEY>', '<your API key>'))
const requestHeaderLines = computed(() => {
  if (requestEndpoint.value === 'claude_messages') {
    return [`x-api-key: ${requestTestApiKeyText.value}`, 'anthropic-version: 2023-06-01']
  }
  return [`Authorization: Bearer ${requestTestApiKeyText.value}`]
})
const requestHeadersText = computed(() => requestHeaderLines.value.join('\n'))
const sampleRequest = computed(() => {
  const targetURL =
    requestEndpointURL.value === requestLoadingText.value
      ? `<${requestEndpointURLLabel.value}>`
      : requestEndpointURL.value
  const model = requestTestModel.value || t('<模型名>', '<model name>')
  const content = requestTestMessage.value.trim() || t('你好', 'Hello')
  const body = requestBodyForEndpoint(requestEndpoint.value, model, content)
  return [
    `curl ${targetURL} \\`,
    ...requestHeaderLines.value.map((header) => `  -H "${header}" \\`),
    '  -H "Content-Type: application/json" \\',
    `  -d ${quoteForCurl(JSON.stringify(body))}`,
  ].join('\n')
})
const requestTestModelOptions = computed(() => {
  const selectedHash = requestTestApiKey.value?.api_key_hash
  const models = availableModels.value?.models ?? []
  const filtered = selectedHash
    ? models.filter((model) => model.sources.some((source) => source.api_key_hash === selectedHash))
    : models
  return filtered.map((model) => ({
    label: modelOptionLabel(model),
    value: model.id,
  }))
})
const selectedRequestTestModelOption = computed(
  () => requestTestModelOptions.value.find((option) => option.value === requestTestModel.value) ?? null,
)
const pagedApiKeys = computed(() => {
  const start = (page.value - 1) * pageSize
  return apiKeys.value.slice(start, start + pageSize)
})
const requestTestReplyText = computed(() => {
  const reply = requestTestResult.value?.reply?.trim()
  return reply || t('模型返回成功，但没有可展示文本。', 'The model returned successfully, but there is no displayable text.')
})
const requestTestUsageText = computed(() => {
  const usage = requestTestResult.value?.usage
  if (!usage) {
    return ''
  }
  const input = numberFromUsage(usage.prompt_tokens ?? usage.input_tokens)
  const output = numberFromUsage(usage.completion_tokens ?? usage.output_tokens)
  const total = numberFromUsage(usage.total_tokens)
  const parts: string[] = []
  if (input !== null) {
    parts.push(t(`输入 ${formatInteger(input)}`, `Input ${formatInteger(input)}`))
  }
  if (output !== null) {
    parts.push(t(`输出 ${formatInteger(output)}`, `Output ${formatInteger(output)}`))
  }
  if (total !== null) {
    parts.push(t(`总计 ${formatInteger(total)}`, `Total ${formatInteger(total)}`))
  }
  return parts.join(' / ')
})

watch(requestEndpoint, () => {
  requestTestResult.value = null
  requestTestError.value = null
})

watch(requestTestEndpointKey, () => {
  requestTestResult.value = null
  requestTestError.value = null
})

watch(currentLanguage, (language, previousLanguage) => {
  const previousDefault = requestTestMessageDefaults[previousLanguage]
  if (requestTestMessage.value === previousDefault) {
    requestTestMessage.value = requestTestMessageDefaults[language]
  }
})

function requestBodyForEndpoint(
  endpoint: ModelRequestEndpoint,
  model: string,
  content: string,
): Record<string, unknown> {
  if (endpoint === 'responses') {
    return {
      model,
      input: content,
      stream: false,
    }
  }
  if (endpoint === 'claude_messages') {
    return {
      model,
      max_tokens: 1024,
      messages: [{ role: 'user', content }],
    }
  }
  return {
    model,
    messages: [{ role: 'user', content }],
    stream: false,
  }
}

function quoteForCurl(value: string): string {
  return "'" + value.replace(/'/g, "'\"'\"'") + "'"
}

function modelRequestOpenAIBaseURL(value: string): string {
  const normalized = value.trim().replace(/\/+$/, '')
  if (!normalized) {
    return requestLoadingText.value
  }
  return /\/v1$/i.test(normalized) ? normalized : `${normalized}/v1`
}

function quotaValueText(quota: UserQuotaStatus | null): string {
  if (!quota) {
    return t('加载中', 'Loading')
  }
  if (quota.unlimited) {
    return t('无限制', 'Unlimited')
  }
  const total = quota.available_usd
  return formatUsd(total)
}

function quotaFootnote(quota: UserQuotaStatus | null): string {
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
    notes.push(t(`未定价 ${formatInteger(quota.unpriced_records)} 条`, `${formatInteger(quota.unpriced_records)} unpriced records`))
  }
  if (quota.paused) {
    notes.push(t('Key 已因余额暂停', 'Key paused due to balance'))
  }
  return notes.length > 0 ? `${balancesText} · ${notes.join(' · ')}` : balancesText
}

function modelOptionLabel(model: AvailableModel): string {
  return model.id
}

function numberFromUsage(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null
  }
  return value
}

function ensureRequestTestModel() {
  const options = requestTestModelOptions.value
  if (options.length === 0) {
    requestTestModel.value = null
    return
  }
  if (!requestTestModel.value || !options.some((option) => option.value === requestTestModel.value)) {
    const firstOption = options[0]
    requestTestModel.value = firstOption ? firstOption.value : null
  }
}

function displayedApiKey(row: UserApiKeySummary): string {
  if (row.api_key && isApiKeyVisible(row)) {
    return row.api_key
  }
  return maskDisplayedApiKey(row.api_key)
}

function maskDisplayedApiKey(apiKey: string | null | undefined): string {
  if (!apiKey) {
    return t('未知', 'Unknown')
  }
  if (apiKey.length <= 12) {
    return `${apiKey.slice(0, 3)}${'*'.repeat(Math.max(apiKey.length - 3, 0))}`
  }
  const visiblePrefix = apiKey.startsWith('sk-') ? 4 : 6
  const visibleSuffix = 4
  const maskedLength = Math.max(apiKey.length - visiblePrefix - visibleSuffix, 8)
  return `${apiKey.slice(0, visiblePrefix)}${'*'.repeat(maskedLength)}${apiKey.slice(-visibleSuffix)}`
}

function updateRequestTestModel(value: unknown) {
  const option = value as { value?: unknown } | null
  requestTestModel.value = typeof option?.value === 'string' ? option.value : null
}

function handlePageChange(value: number) {
  page.value = value
}

function isApiKeyVisible(row: UserApiKeySummary): boolean {
  return visibleApiKeyHashes.value.has(row.api_key_hash)
}

function toggleApiKeyVisibility(row: UserApiKeySummary) {
  if (!row.api_key) {
    message.info(t('当前没有完整密钥可显示', 'No full key is available to show'))
    return
  }
  const nextVisible = new Set(visibleApiKeyHashes.value)
  if (nextVisible.has(row.api_key_hash)) {
    nextVisible.delete(row.api_key_hash)
  } else {
    nextVisible.add(row.api_key_hash)
  }
  visibleApiKeyHashes.value = nextVisible
}

async function copyApiKey(row: UserApiKeySummary) {
  try {
    if (!row.api_key) {
      message.info(t('当前没有完整密钥可复制', 'No full key is available to copy'))
      return
    }
    await copyToClipboard(row.api_key)
    message.success(t('API 密钥已复制', 'API key copied'))
  } catch (error) {
    message.error(errorText(error, '复制失败', 'Copy failed'))
  }
}

async function copyGeneratedApiKey() {
  if (!generatedApiKey.value) {
    return
  }
  try {
    await copyToClipboard(generatedApiKey.value)
    message.success(t('API 密钥已复制', 'API key copied'))
  } catch (error) {
    message.error(errorText(error, '复制失败', 'Copy failed'))
  }
}

async function loadModelRequestGuide() {
  try {
    modelRequestGuide.value = await getModelRequestGuide()
  } catch (error) {
    message.error(errorText(error, '加载请求地址失败', 'Failed to load request endpoints'))
  }
}

let availableModelsRequest: Promise<void> | null = null
async function fetchAvailableModels(force = false): Promise<void> {
  if (availableModelsRequest) return availableModelsRequest
  if (!force && availableModels.value) return
  isAvailableModelsLoading.value = true
  availableModelsRequest = (async () => {
    try {
      availableModels.value = await listAvailableModels()
      ensureRequestTestModel()
    } catch (error) {
      availableModels.value = null
      throw error
    } finally {
      isAvailableModelsLoading.value = false
      availableModelsRequest = null
    }
  })()
  return availableModelsRequest
}

async function loadAvailableModelsForTest() {
  try {
    await fetchAvailableModels(true)
  } catch (error) {
    message.error(errorText(error, '加载可用模型失败', 'Failed to load available models'))
  }
}

function openRequestTest(row: UserApiKeySummary) {
  requestTestApiKey.value = row
  requestTestEndpointKey.value = 'default'
  requestTestModel.value = row.last_model ?? row.models[0] ?? null
  requestTestMessage.value = requestTestMessageDefaults[currentLanguage.value]
  requestTestResult.value = null
  requestTestError.value = null
  requestTestVisible.value = true
  if (!modelRequestGuide.value) {
    void loadModelRequestGuide()
  }
  if (!availableModels.value) {
    void loadAvailableModelsForTest()
  } else {
    ensureRequestTestModel()
  }
}

async function copyRequestValue(label: string, value: string) {
  if (!value || value === requestLoadingText.value) {
    return
  }
  try {
    await copyToClipboard(value)
    message.success(copiedText(label))
  } catch (error) {
    message.error(errorText(error, '复制失败', 'Copy failed'))
  }
}

async function runRequestTest() {
  if (isRequestTesting.value) {
    return
  }
  const currentKey = requestTestApiKey.value
  const model = requestTestModel.value?.trim() ?? ''
  if (!currentKey) {
    message.error(t('请选择要测试的 API KEY', 'Select an API key to test'))
    return
  }
  if (!model) {
    message.error(t('请选择测试模型', 'Select a test model'))
    return
  }
  isRequestTesting.value = true
  requestTestResult.value = null
  requestTestError.value = null
  try {
    requestTestResult.value = await testModelRequest({
      api_key_hash: currentKey.api_key_hash,
      endpoint: requestEndpoint.value,
      base_url: requestBaseURL.value,
      model,
      message: requestTestMessage.value,
    })
    message.success(t('请求测试完成', 'Request test completed'))
  } catch (error) {
    requestTestError.value = errorText(error, '请求测试失败', 'Request test failed')
  } finally {
    isRequestTesting.value = false
  }
}

function openCreateDialog() {
  if (!canCreateApiKey.value) {
    message.error(t('当前账号额度已用尽，API KEY 已暂停', 'This account has exhausted its quota, so API keys are paused'))
    return
  }
  editingApiKeyHash.value = null
  apiKeyDescription.value = 'VSCode'
  generatedApiKey.value = null
  generatedApiKeyHash.value = null
  editorVisible.value = true
}

function closeGeneratedApiKey() {
  generatedApiKey.value = null
  generatedApiKeyHash.value = null
}

function editApiKey(row: UserApiKeySummary) {
  editingApiKeyHash.value = row.api_key_hash
  apiKeyDescription.value = row.description || 'VSCode'
  generatedApiKey.value = null
  generatedApiKeyHash.value = null
  editorVisible.value = true
}

function isApiKeyActionLoading(row: UserApiKeySummary): boolean {
  return apiKeyActionHashes.value.has(row.api_key_hash)
}

function setApiKeyActionLoading(apiKeyHash: string, loading: boolean) {
  const next = new Set(apiKeyActionHashes.value)
  if (loading) {
    next.add(apiKeyHash)
  } else {
    next.delete(apiKeyHash)
  }
  apiKeyActionHashes.value = next
}

function confirmToggleApiKey(row: UserApiKeySummary) {
  const nextDisabled = row.disabled !== true
  const label = row.description || t('未命名密钥', 'Unnamed key')
  dialog.warning({
    title: nextDisabled ? t('禁用 API 密钥', 'Disable API key') : t('启用 API 密钥', 'Enable API key'),
    content: nextDisabled
      ? t(`将暂时禁用“${label}”，并从可用密钥中移除。之后可以再次启用。`, `Temporarily disable “${label}” and remove it from active keys. You can enable it again later.`)
      : t(`将重新启用“${label}”，并将其加入可用密钥。`, `Re-enable “${label}” and add it back to active keys.`),
    positiveText: nextDisabled ? t('确认禁用', 'Confirm disable') : t('确认启用', 'Confirm enable'),
    negativeText: t('取消', 'Cancel'),
    onPositiveClick: async () => {
      if (isApiKeyActionLoading(row)) {
        return
      }
      setApiKeyActionLoading(row.api_key_hash, true)
      try {
        const updated = nextDisabled
          ? await disableApiKey(row.api_key_hash)
          : await enableApiKey(row.api_key_hash)
        apiKeys.value = apiKeys.value.map((item) =>
          item.api_key_hash === updated.api_key_hash ? updated : item,
        )
        message.success(nextDisabled ? t('API 密钥已禁用', 'API key disabled') : t('API 密钥已启用', 'API key enabled'))
      } catch (error) {
        message.error(errorText(
          error,
          nextDisabled ? '禁用 API 密钥失败' : '启用 API 密钥失败',
          nextDisabled ? 'Failed to disable API key' : 'Failed to enable API key',
        ))
      } finally {
        setApiKeyActionLoading(row.api_key_hash, false)
      }
    },
  })
}

async function refresh() {
  isLoading.value = true
  try {
    const [nextApiKeys, overview, quota, guide] = await Promise.all([
      listApiKeys(),
      getUsageOverview({ scope: 'account' }, false),
      getCurrentUserQuota(),
      getModelRequestGuide(),
    ])
    apiKeys.value = nextApiKeys
    availableModels.value = null
    page.value = Math.min(page.value, Math.max(1, Math.ceil(nextApiKeys.length / pageSize)))
    usageSummary.value = overview.summary
    quotaStatus.value = quota
    modelRequestGuide.value = guide
    if (editingApiKeyHash.value) {
      const current = apiKeys.value.find((item) => item.api_key_hash === editingApiKeyHash.value)
      if (!current) {
        editorVisible.value = false
        editingApiKeyHash.value = null
      }
    }
  } catch (error) {
    message.error(errorText(error, '加载 API 密钥失败', 'Failed to load API keys'))
  } finally {
    isLoading.value = false
  }
}

async function saveApiKey() {
  if (isSaving.value) {
    return
  }
  const description = apiKeyDescription.value.trim()
  if (!description) {
    message.error(t('API KEY 描述不能为空', 'API key description is required'))
    return
  }
  isSaving.value = true
  try {
    if (editingApiKeyHash.value) {
      await updateApiKey(editingApiKeyHash.value, { description })
      message.success(t('API 密钥已更新', 'API key updated'))
    } else {
      if (!canCreateApiKey.value) {
        message.error(t('当前账号额度已用尽，API KEY 已暂停', 'This account has exhausted its quota, so API keys are paused'))
        return
      }
      const created = await createApiKey({ description })
      generatedApiKey.value = created.api_key ?? null
      generatedApiKeyHash.value = created.api_key_hash
      message.success(t('API 密钥已创建并同步', 'API key created and synced'))
    }
    editorVisible.value = false
    editingApiKeyHash.value = null
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存 API 密钥失败', 'Failed to save API key'))
  } finally {
    isSaving.value = false
  }
}

function confirmDelete(row: UserApiKeySummary) {
  dialog.warning({
    title: t('删除 API 密钥', 'Delete API key'),
    content: t(
      `将删除 ${row.description || '未命名'} 对应的密钥，并从可用密钥中移除。`,
      `This deletes the key for ${row.description || 'Unnamed'} and removes it from active keys.`,
    ),
    positiveText: t('删除', 'Delete'),
    negativeText: t('取消', 'Cancel'),
    onPositiveClick: async () => {
      await deleteApiKey(row.api_key_hash)
      message.success(t('API 密钥已删除', 'API key deleted'))
      if (editingApiKeyHash.value === row.api_key_hash) {
        editorVisible.value = false
        editingApiKeyHash.value = null
      }
      if (generatedApiKeyHash.value === row.api_key_hash) {
        generatedApiKey.value = null
        generatedApiKeyHash.value = null
      }
      await refresh()
    },
  })
}

const tutorialGuide = ref<InstanceType<typeof TutorialGuide> | null>(null)
const tutorialContext = computed(() => ({
  apiKeys: apiKeys.value,
  endpoints: modelRequestGuide.value ? publicRequestEndpoints.value : [],
  models: availableModels.value?.models ?? [],
  ensureModels: () => fetchAvailableModels(),
}))

onMounted(refresh)
</script>

<template>
  <section class="page api-keys-page">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('API 密钥', 'API keys') }}</h1>
      <div class="flex items-center gap-2">
        <Button variant="outline" :disabled="isLoading" @click="refresh(); tutorialGuide?.reload()">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCw v-else data-icon="inline-start" />
          {{ t('刷新', 'Refresh') }}
        </Button>
        <Button :disabled="!canCreateApiKey" @click="openCreateDialog">
          <Plus data-icon="inline-start" />
          {{ t('新建 API 密钥', 'New API key') }}
        </Button>
      </div>
    </div>

    <div class="api-key-metrics">
      <Card
        v-for="metric in apiKeyMetrics"
        :key="metric.key"
        size="sm"
        class="api-key-metric"
        :class="`is-${metric.tone}`"
      >
        <CardHeader class="flex flex-row items-start justify-between gap-3">
          <div class="flex min-w-0 flex-col gap-1">
            <CardDescription>{{ metric.label }}</CardDescription>
            <CardTitle class="text-2xl tabular-nums">{{ metric.value }}</CardTitle>
          </div>
          <div class="api-key-metric__icon" aria-hidden="true">
            <component :is="metric.icon" class="size-5" />
          </div>
        </CardHeader>
        <CardContent class="truncate text-xs text-muted-foreground" :title="metric.footnote">
          {{ metric.footnote }}
        </CardContent>
      </Card>
    </div>

    <div class="api-key-content-grid">
      <Card class="api-key-panel-shell">
        <CardHeader>
          <CardTitle>{{ t('密钥列表', 'Key list') }}</CardTitle>
          <CardDescription>
            {{ t('管理当前账号可用于 API 请求的密钥。', 'Manage API keys available to the current account.') }}
          </CardDescription>
        </CardHeader>
        <CardContent class="api-key-panel">
          <Alert v-if="quotaStatus?.paused" variant="destructive">
            <AlertTitle>{{ t('额度已用尽', 'Quota exhausted') }}</AlertTitle>
            <AlertDescription>
              {{ t('当前账号的 API 密钥已暂停。补充额度或进入新的日、周周期后，系统会自动恢复可用密钥。', 'API keys for this account are paused. They are restored automatically after quota is added or a new daily or weekly period begins.') }}
            </AlertDescription>
          </Alert>
          <Alert v-else-if="quotaStatus?.unpriced_records">
            <AlertTitle>{{ t('存在未定价用量', 'Unpriced usage found') }}</AlertTitle>
            <AlertDescription>
              {{ t(`当前账号存在 ${formatInteger(quotaStatus.unpriced_records)} 条未定价用量，未计入额度扣减。`, `This account has ${formatInteger(quotaStatus.unpriced_records)} unpriced usage records that are not deducted from quota.`) }}
            </AlertDescription>
          </Alert>

          <div v-if="generatedApiKey" class="generated-key-box">
            <div class="generated-key-main">
              <div class="generated-key-title">{{ t('新创建的密钥', 'Newly created key') }}</div>
              <code class="generated-key-value">{{ generatedApiKey }}</code>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <Button size="sm" variant="outline" @click="copyGeneratedApiKey">
                <Copy data-icon="inline-start" />
                {{ t('复制', 'Copy') }}
              </Button>
              <Button size="sm" variant="ghost" @click="closeGeneratedApiKey">{{ t('关闭', 'Close') }}</Button>
            </div>
          </div>

          <div class="api-key-table-shell">
            <Table class="api-key-table min-w-[680px] table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead class="w-[47%]">
                    <span class="api-key-title">
                      <EyeOff class="api-key-mask-icon size-4" />
                      {{ t('密钥（点击复制）', 'Key (click to copy)') }}
                    </span>
                  </TableHead>
                  <TableHead class="w-[22%]">{{ t('描述', 'Description') }}</TableHead>
                  <TableHead class="w-[23%]">{{ t('创建时间', 'Created at') }}</TableHead>
                  <TableHead class="w-[8%]">
                    <span class="sr-only">{{ t('操作', 'Actions') }}</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <template v-if="isLoading && apiKeys.length === 0">
                  <TableRow v-for="rowIndex in 5" :key="`api-key-skeleton-${rowIndex}`">
                    <TableCell v-for="columnIndex in 4" :key="columnIndex">
                      <Skeleton class="h-4 w-full" />
                    </TableCell>
                  </TableRow>
                </template>

                <TableEmpty v-else-if="apiKeys.length === 0" :colspan="4">
                  {{ t('暂无 API 密钥', 'No API keys') }}
                </TableEmpty>

                <TableRow v-for="row in pagedApiKeys" v-else :key="row.api_key_hash">
                  <TableCell>
                    <div class="api-key-cell">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        :disabled="!row.api_key"
                        :title="isApiKeyVisible(row) ? t('隐藏完整密钥', 'Hide full key') : t('显示完整密钥', 'Show full key')"
                        :aria-label="isApiKeyVisible(row) ? t('隐藏完整密钥', 'Hide full key') : t('显示完整密钥', 'Show full key')"
                        @click="toggleApiKeyVisibility(row)"
                      >
                        <Eye v-if="isApiKeyVisible(row)" />
                        <EyeOff v-else />
                      </Button>
                      <Button
                        class="api-key-copy-button"
                        type="button"
                        size="sm"
                        variant="ghost"
                        :title="row.api_key ? t('点击复制完整密钥', 'Click to copy full key') : t('无完整密钥可复制', 'No full key available to copy')"
                        @click="copyApiKey(row)"
                      >
                        <code class="api-key-mask-text">{{ displayedApiKey(row) }}</code>
                      </Button>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="api-key-description-cell">
                      <span class="description-cell" :title="row.description || '-'">{{ row.description || '-' }}</span>
                      <Badge v-if="row.disabled" variant="destructive">
                        {{ t('已禁用', 'Disabled') }}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell class="whitespace-nowrap text-muted-foreground">
                    {{ formatDateTime(row.created_at) }}
                  </TableCell>
                  <TableCell class="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger as-child>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          :aria-label="t(`打开 ${row.description || 'API KEY'} 的操作菜单`, `Open actions for ${row.description || 'API key'}`)"
                        >
                          <MoreHorizontal />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" class="w-40">
                        <DropdownMenuGroup>
                          <DropdownMenuItem :disabled="row.disabled" @select="openRequestTest(row)">
                            <Send />
                            <span>{{ t('请求测试', 'Request test') }}</span>
                          </DropdownMenuItem>
                          <DropdownMenuItem @select="editApiKey(row)">
                            <Pencil />
                            <span>{{ t('编辑', 'Edit') }}</span>
                          </DropdownMenuItem>
                          <DropdownMenuItem :disabled="isApiKeyActionLoading(row)" @select="confirmToggleApiKey(row)">
                            <PowerOff v-if="row.disabled" />
                            <Power v-else />
                            <span>{{ row.disabled ? t('启用', 'Enable') : t('禁用', 'Disable') }}</span>
                          </DropdownMenuItem>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem variant="destructive" @select="confirmDelete(row)">
                          <Trash2 />
                          <span>{{ t('删除', 'Delete') }}</span>
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>

          <div v-if="apiKeys.length > pageSize" class="api-key-pagination">
            <span>{{ t(`共 ${formatInteger(apiKeys.length)} 个密钥`, `${formatInteger(apiKeys.length)} keys total`) }}</span>
            <Pagination
              :page="page"
              :items-per-page="pageSize"
              :total="apiKeys.length"
              :sibling-count="1"
              @update:page="handlePageChange"
            >
              <PaginationContent v-slot="{ items }">
                <PaginationPrevious />
                <template v-for="(item, index) in items" :key="index">
                  <PaginationItem
                    v-if="item.type === 'page'"
                    :value="item.value"
                    :is-active="item.value === page"
                  >
                    {{ item.value }}
                  </PaginationItem>
                  <PaginationEllipsis v-else :index="index" />
                </template>
                <PaginationNext />
              </PaginationContent>
            </Pagination>
          </div>
        </CardContent>
      </Card>

      <Card class="api-endpoint-panel-shell">
        <CardHeader>
          <CardTitle>API Endpoint</CardTitle>
          <CardDescription>
            {{ t('选择 URL 类型，然后复制对应接入地址。', 'Choose a URL type, then copy the matching endpoint.') }}
          </CardDescription>
        </CardHeader>
        <CardContent class="api-endpoint-panel">
          <div class="api-endpoint-type-picker">
            <Tabs
              :model-value="publicRequestURLType"
              class="api-endpoint-type-tabs w-full"
              @update:model-value="updatePublicRequestURLType"
            >
              <TabsList
                class="api-endpoint-type-options grid h-10 w-full grid-cols-4 p-1"
                :aria-label="t('URL 类型', 'URL type')"
              >
                <TabsTrigger
                  v-for="option in publicRequestURLTypeOptions"
                  :key="option.value"
                  :value="option.value"
                  class="min-w-0 justify-center px-2 text-center"
                  :aria-label="option.ariaLabel"
                  :title="option.ariaLabel"
                >
                  {{ option.label }}
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          <div class="request-guide-list">
            <div v-for="endpoint in publicRequestEndpointRows" :key="endpoint.key" class="request-guide-row">
              <div class="min-w-0">
                <div class="request-guide-label">{{ endpoint.label }}</div>
                <code class="request-guide-value">{{ endpoint.url }}</code>
              </div>
              <Button size="sm" variant="outline" @click="copyRequestValue(endpoint.label, endpoint.url)">
                <Copy data-icon="inline-start" />
                {{ t('复制', 'Copy') }}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>

    <TutorialGuide ref="tutorialGuide" :context="tutorialContext" />

    <Dialog v-model:open="editorVisible">
      <DialogContent
        :show-close-button="false"
        class="sm:max-w-[520px]"
        @interact-outside.prevent
      >
        <form class="flex flex-col gap-5" @submit.prevent="saveApiKey">
          <DialogHeader>
            <DialogTitle>
              {{ editingApiKeyHash ? t('编辑 API 密钥', 'Edit API key') : t('新建 API 密钥', 'New API key') }}
            </DialogTitle>
            <DialogDescription>
              {{ t('描述用于区分不同客户端或使用场景。', 'Use the description to identify clients or usage scenarios.') }}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel for="api-key-description">{{ t('API KEY 描述', 'API key description') }}</FieldLabel>
              <Input
                id="api-key-description"
                v-model="apiKeyDescription"
                :disabled="isSaving"
                :placeholder="t('例如：VSCode', 'Example: VSCode')"
                autofocus
              />
              <FieldDescription>{{ t('建议填写客户端名称，方便日后识别。', 'Use a client name so the key is easy to identify later.') }}</FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" :disabled="isSaving" @click="editorVisible = false">
              {{ t('取消', 'Cancel') }}
            </Button>
            <Button
              type="submit"
              :disabled="isSaving || (!editingApiKeyHash && !canCreateApiKey)"
            >
              <Spinner v-if="isSaving" data-icon="inline-start" />
              {{ editingApiKeyHash ? t('保存', 'Save') : t('创建', 'Create') }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="requestTestVisible">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{{ t('请求测试', 'Request test') }}</DialogTitle>
          <DialogDescription>
            {{ t('查看当前 API KEY 的请求说明，或选择模型发起真实测试。', 'Review request instructions for this API key or run a real model test.') }}
          </DialogDescription>
        </DialogHeader>

        <div class="request-test-scroll">
          <Field>
            <FieldLabel for="request-test-endpoint">{{ t('请求 Endpoint', 'Request endpoint') }}</FieldLabel>
            <Select :model-value="requestTestEndpointKey" :disabled="!modelRequestGuide" @update:model-value="updateRequestTestEndpointKey">
              <SelectTrigger id="request-test-endpoint" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem v-for="endpoint in publicRequestEndpoints" :key="endpoint.key" :value="endpoint.key">
                    {{ endpoint.label }}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <div class="request-endpoint-switch">
            <span id="request-format-label" class="request-endpoint-label">{{ t('请求格式', 'Request format') }}</span>
            <Tabs
              :model-value="requestEndpoint"
              class="request-format-tabs"
              @update:model-value="updateRequestEndpoint"
            >
              <TabsList aria-labelledby="request-format-label">
                <TabsTrigger
                  v-for="option in requestEndpointOptions"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          <div class="request-guide-list">
            <div class="request-guide-row">
              <div class="min-w-0">
                <div class="request-guide-label">{{ t('基础 URL', 'Base URL') }}</div>
                <code class="request-guide-value">{{ requestBaseURL }}</code>
              </div>
              <Button size="sm" variant="outline" @click="copyRequestValue(t('基础 URL', 'Base URL'), requestBaseURL)">
                <Copy data-icon="inline-start" />
                {{ t('复制', 'Copy') }}
              </Button>
            </div>
            <div class="request-guide-row">
              <div class="min-w-0">
                <div class="request-guide-label">{{ requestEndpointURLLabel }}</div>
                <code class="request-guide-value">{{ requestEndpointURL }}</code>
              </div>
              <Button size="sm" variant="outline" @click="copyRequestValue(t('请求 URL', 'Request URL'), requestEndpointURL)">
                <Copy data-icon="inline-start" />
                {{ t('复制', 'Copy') }}
              </Button>
            </div>
            <div class="request-guide-row">
              <div class="min-w-0">
                <div class="request-guide-label">{{ t('请求 Header', 'Request headers') }}</div>
                <code class="request-guide-value request-guide-value-multiline">{{ requestHeadersText }}</code>
              </div>
              <Button size="sm" variant="outline" @click="copyRequestValue(t('请求 Header', 'Request headers'), requestHeadersText)">
                <Copy data-icon="inline-start" />
                {{ t('复制', 'Copy') }}
              </Button>
            </div>
          </div>

          <div class="request-example">
            <div class="request-example-head">
              <span>{{ t('curl 示例', 'curl example') }}</span>
              <Button size="sm" variant="outline" @click="copyRequestValue(t('curl 示例', 'curl example'), sampleRequest)">
                <Copy data-icon="inline-start" />
                {{ t('复制示例', 'Copy example') }}
              </Button>
            </div>
            <pre>{{ sampleRequest }}</pre>
          </div>

          <div class="request-test-section-title">{{ t('请求测试', 'Request test') }}</div>

          <FieldGroup class="request-test-form">
            <Field>
              <FieldLabel>{{ t('测试模型', 'Test model') }}</FieldLabel>
              <Combobox
                :model-value="selectedRequestTestModelOption"
                by="value"
                @update:model-value="updateRequestTestModel"
              >
                <ComboboxAnchor as-child>
                  <ComboboxTrigger as-child>
                    <Button variant="outline" class="model-combobox-trigger">
                      <Spinner v-if="isAvailableModelsLoading" data-icon="inline-start" />
                      <span class="min-w-0 flex-1 truncate text-left">
                        {{ selectedRequestTestModelOption?.label ?? t('选择当前 Key 可用的模型', 'Select a model available to this key') }}
                      </span>
                      <ChevronsUpDown data-icon="inline-end" class="text-muted-foreground" />
                    </Button>
                  </ComboboxTrigger>
                </ComboboxAnchor>
                <ComboboxList align="start" class="model-combobox-list">
                  <ComboboxInput :placeholder="t('搜索模型', 'Search models')" />
                  <ComboboxEmpty>{{ t('没有匹配模型', 'No matching models') }}</ComboboxEmpty>
                  <ComboboxGroup>
                    <ComboboxItem
                      v-for="option in requestTestModelOptions"
                      :key="option.value"
                      :value="option"
                    >
                      <span class="truncate">{{ option.label }}</span>
                      <ComboboxItemIndicator>
                        <Check />
                      </ComboboxItemIndicator>
                    </ComboboxItem>
                  </ComboboxGroup>
                </ComboboxList>
              </Combobox>
            </Field>
            <Field>
              <FieldLabel for="request-test-message">{{ t('测试消息', 'Test message') }}</FieldLabel>
              <Textarea
                id="request-test-message"
                v-model="requestTestMessage"
                rows="4"
                :placeholder="t('输入要发送给模型的测试消息', 'Enter the test message to send to the model')"
              />
            </Field>
          </FieldGroup>

          <Alert v-if="!isAvailableModelsLoading && requestTestModelOptions.length === 0">
            <AlertDescription>
              {{ t('当前 Key 暂未查询到可选模型，可以先刷新模型列表，或到「可用模型」页面检查 Key 是否可用。', 'No selectable models were found for this key. Refresh the model list, or check whether the key is available on the Available models page.') }}
            </AlertDescription>
          </Alert>

          <div class="request-test-actions">
            <Button variant="outline" :disabled="isAvailableModelsLoading" @click="loadAvailableModelsForTest">
              <Spinner v-if="isAvailableModelsLoading" data-icon="inline-start" />
              <RefreshCw v-else data-icon="inline-start" />
              {{ t('刷新模型', 'Refresh models') }}
            </Button>
            <Button
              :disabled="!requestTestModel || !modelRequestGuide || isAvailableModelsLoading || isRequestTesting"
              @click="runRequestTest"
            >
              <Spinner v-if="isRequestTesting" data-icon="inline-start" />
              <Send v-else data-icon="inline-start" />
              {{ t('发送测试', 'Send test') }}
            </Button>
          </div>

          <Alert v-if="requestTestError" variant="destructive">
            <AlertDescription>{{ requestTestError }}</AlertDescription>
          </Alert>

          <div v-if="requestTestResult" class="request-test-result">
            <div class="request-test-result-head">
              <span>{{ t('模型回复', 'Model reply') }}</span>
              <span>
                HTTP {{ requestTestResult.status_code }} · {{ requestTestResult.duration_ms }}ms
                <template v-if="requestTestUsageText"> · {{ requestTestUsageText }}</template>
              </span>
            </div>
            <pre>{{ requestTestReplyText }}</pre>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </section>
</template>

<style scoped>
.api-keys-page {
  min-width: 0;
}

.api-key-metrics {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 1rem;
}

.api-key-metric {
  min-width: 0;
}

.api-key-metric__icon {
  display: flex;
  width: 2.25rem;
  height: 2.25rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--primary) 10%, transparent);
  color: var(--primary);
}

.api-key-metric.is-blue .api-key-metric__icon {
  background: color-mix(in srgb, var(--chart-2) 12%, transparent);
  color: var(--chart-2);
}

.api-key-metric.is-purple .api-key-metric__icon {
  background: color-mix(in srgb, var(--chart-4) 12%, transparent);
  color: var(--chart-4);
}

.api-key-metric.is-green .api-key-metric__icon {
  background: color-mix(in srgb, var(--cpa-success) 12%, transparent);
  color: var(--cpa-success);
}

.api-key-content-grid {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(21rem, 2fr);
  align-items: start;
  gap: 1rem;
  min-width: 0;
}

.api-key-panel {
  display: grid;
  gap: 1rem;
  min-width: 0;
}

.api-key-panel-shell,
.api-endpoint-panel-shell {
  min-width: 0;
}

.api-endpoint-panel {
  display: grid;
  gap: .75rem;
  min-width: 0;
}

.api-endpoint-type-picker {
  display: grid;
  gap: .5rem;
  min-width: 0;
}

.api-key-table-shell {
  min-width: 0;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}

.api-key-table {
  width: 100%;
}

.generated-key-box {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-width: 0;
  padding: 1rem;
  border: 1px solid color-mix(in srgb, var(--primary) 24%, var(--border));
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--primary) 7%, transparent);
}

.generated-key-main {
  min-width: 0;
}

.generated-key-title {
  margin-bottom: .25rem;
  font-weight: 600;
}

.generated-key-value {
  display: block;
  overflow-wrap: anywhere;
  font-size: .8125rem;
}

.api-key-pagination {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1rem;
  color: var(--muted-foreground);
  font-size: .75rem;
}

.request-test-scroll {
  display: grid;
  min-height: 0;
  gap: 1rem;
  overflow-y: auto;
  padding: 0 .25rem .25rem 0;
}

.request-endpoint-switch {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: .625rem .75rem;
  min-width: 0;
  padding: .75rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--muted) 24%, transparent);
}

.request-endpoint-label {
  color: var(--muted-foreground);
  font-size: .75rem;
  font-weight: 600;
}

.request-guide-list {
  display: grid;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--card);
}

.request-guide-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: .75rem;
  min-width: 0;
  padding: .75rem .875rem;
  border-bottom: 1px solid var(--border);
}

.request-guide-row:last-child {
  border-bottom: 0;
}

.request-guide-label {
  margin-bottom: .25rem;
  color: var(--muted-foreground);
  font-size: .75rem;
  font-weight: 600;
}

.request-guide-value {
  display: block;
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--foreground);
  font-size: .8125rem;
  line-height: 1.45;
}

.request-guide-value-multiline {
  white-space: pre-wrap;
}

.request-example {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--card);
}

.request-example-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding: .625rem .875rem;
  border-bottom: 1px solid var(--border);
  font-weight: 600;
}

.request-example pre {
  overflow: auto;
  margin: 0;
  padding: .875rem;
  background: color-mix(in srgb, var(--muted) 52%, transparent);
  color: var(--foreground);
  font-size: .8125rem;
  line-height: 1.6;
  white-space: pre-wrap;
}

.request-test-section-title {
  color: var(--foreground);
  font-size: .875rem;
  font-weight: 600;
}

.request-test-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}

.request-test-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: .5rem;
}

.model-combobox-trigger {
  width: 100%;
  min-width: 0;
  justify-content: space-between;
}

.model-combobox-list {
  width: var(--reka-combobox-trigger-width);
  max-width: calc(100vw - 2rem);
}

.request-test-result {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--card);
}

.request-test-result-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding: .625rem .875rem;
  border-bottom: 1px solid var(--border);
  font-weight: 600;
}

.request-test-result-head span:last-child {
  color: var(--muted-foreground);
  font-size: .75rem;
  font-weight: 500;
}

.request-test-result pre {
  overflow: auto;
  margin: 0;
  padding: .875rem;
  background: color-mix(in srgb, var(--muted) 52%, transparent);
  color: var(--foreground);
  font-size: .8125rem;
  line-height: 1.6;
  white-space: pre-wrap;
}

.api-key-cell {
  display: flex;
  align-items: center;
  gap: .5rem;
  width: 100%;
  min-width: 0;
}

.api-key-copy-button {
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--foreground);
  font: inherit;
  line-height: 1.35;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.api-key-title {
  display: inline-flex;
  align-items: center;
  gap: .5rem;
}

.api-key-mask-icon {
  flex: 0 0 auto;
  color: var(--muted-foreground);
}

.api-key-mask-text {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: inherit;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.description-cell {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.api-key-description-cell {
  display: flex;
  align-items: center;
  gap: .5rem;
  min-width: 0;
}

.api-key-description-cell .description-cell {
  min-width: 0;
  flex: 1 1 auto;
}

.api-key-copy-button:hover,
.api-key-copy-button:focus-visible {
  color: var(--primary);
}

.api-key-copy-button:focus-visible {
  border-radius: .25rem;
  outline: 2px solid color-mix(in srgb, var(--ring) 42%, transparent);
  outline-offset: .125rem;
}

@media (max-width: 1180px) {
  .api-key-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .api-key-content-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .api-key-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .generated-key-box {
    align-items: stretch;
    flex-direction: column;
  }

  .request-guide-row {
    grid-template-columns: 1fr;
  }

  .request-example-head,
  .request-test-result-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .request-test-form {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 480px) {
  .api-key-metrics {
    grid-template-columns: 1fr;
  }

  .request-format-tabs,
  .request-format-tabs :deep([data-slot='tabs-list']) {
    width: 100%;
  }

  .request-format-tabs :deep([data-slot='tabs-list']) {
    display: grid;
    height: auto;
    grid-template-columns: 1fr;
  }
}
</style>
