<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
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
import { Checkbox } from '@/components/ui/checkbox'
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
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
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
import { Textarea } from '@/components/ui/textarea'
import {
  Activity,
  ArrowLeft,
  Check,
  Copy,
  Eye,
  ExternalLink,
  Gauge,
  KeyRound,
  LogIn,
  MoreHorizontal,
  PauseCircle,
  Pencil,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  Upload,
  Users,
} from '@lucide/vue'

import {
  bulkDeleteCodexKeeperAccounts,
  coreCredentialAction,
  bindCredentialSIWC,
  cancelCodexKeeperOAuth,
  consumeCodexKeeperResetCredit,
  deleteCodexKeeperAccount,
  disableCodexKeeperAccount,
  enableCodexKeeperAccount,
  getCodexKeeperAuthFile,
  getCodexKeeperAuthFileModels,
  getCodexKeeperOAuthStatus,
  getCodexKeeperStatus,
  listCodexKeeperAccounts,
  queryCodexKeeperResetCredits,
  refreshCodexKeeperAccounts,
  runCodexKeeperOnce,
  startCodexKeeperOAuth,
  submitCodexKeeperOAuthCallback,
  syncCodexKeeperAccountList,
  updateCodexKeeperAuthFile,
  updateCodexKeeperPriority,
  uploadCodexKeeperAuthFiles,
} from '@/features/codex-keeper/api/codexKeeperApi'
import CodexKeeperLogsPanel from '@/features/codex-keeper/components/CodexKeeperLogsPanel.vue'
import { useCurrentUser } from '@/features/auth/state/currentUser'
import type {
  CodexKeeperAccount,
  CodexKeeperAuthFileFields,
  CodexKeeperAuthFileModel,
  CodexKeeperPriorityRule,
  CodexKeeperQuotaWindowUsage,
  CodexKeeperResetCredits,
  CodexKeeperStatus,
  CredentialOAuthProvider,
} from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import FilterCombobox from '@/shared/ui/FilterCombobox.vue'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'
import { copyToClipboard } from '@/shared/utils/clipboard'
import {
  formatCompact,
  formatDateTime,
  formatInteger,
  formatRelativeTime,
  formatUsd,
} from '@/shared/utils/format'

type FixedPriorityFilter = 'all' | 'high' | 'minusOne' | 'low'
type PriorityTypeFilter = `type:${string}`
type PriorityFilter = FixedPriorityFilter | PriorityTypeFilter
type AccountStatusFilter = 'all' | 'enabled' | 'disabled' | 'unauthorized' | 'quotaExhausted'
type AccountDisplaySize = 50 | 100 | 150 | 200
type AccountSortKey = 'quotaDay' | 'quotaWeek' | 'provider' | 'status' | 'priority' | 'lastCheckedAt'
type SortDirection = 'asc' | 'desc'
type PriorityMode = 'low' | 'high' | 'default' | 'custom'
type AccountAction = 'toggle' | 'priority' | 'delete' | 'refresh' | 'reset-query' | 'reset-consume'
type AccountConfirmType = 'default' | 'warning' | 'error' | 'primary'
type OAuthDialogStatus = 'idle' | 'waiting' | 'success' | 'error'
type KeeperPollMode = 'once' | 'accounts'
type ResetCreditFeedback = {
  variant: 'success' | 'warning' | 'error'
  text: string
}
type QuotaWindowItem = {
  label: string
  usedPercent: number
  remainingPercent: number
  resetAt: string | null
  usage: CodexKeeperQuotaWindowUsage | null
}
type QuotaUsageTag = { label: string; value: string; tone?: 'stale' }
type CredentialQuotaObservation = {
  label: string
  observedAt: string | null
  signals: Array<{ name: string; value: string }>
}
type AccountStatusPreferences = {
  displaySize?: unknown
  sort?: {
    key?: unknown
    direction?: unknown
  }
}
type AuthFileEditorState = {
  fileName: string
  fileInfoText: string
  loading: boolean
  saving: boolean
  error: string | null
  rawText: string
  invalidContentPreview: string
  json: Record<string, unknown> | null
  prefix: string
  proxyUrl: string
  weight: number | null
  weightTouched: boolean
  requestRetry: number | null
  requestRetryTouched: boolean
  websockets: boolean
  websocketsTouched: boolean
  usingApi: boolean
  usingApiTouched: boolean
  note: string
  noteTouched: boolean
  headersText: string
  headersTouched: boolean
  headersError: string | null
}

const ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY = 'cpa-helper-codex-keeper-status-preferences'
const AUTH_FILE_MAX_SIZE = 10 * 1024 * 1024
const CODEX_FIVE_HOUR_WINDOW_SECONDS = 5 * 60 * 60
const CODEX_WEEK_WINDOW_SECONDS = 7 * 24 * 60 * 60
const CODEX_MONTH_WINDOW_SECONDS = 30 * 24 * 60 * 60
const accountManageTableScrollX = 1562
const accountReadOnlyTableScrollX = 1534
const KEEPER_STATUS_POLL_INTERVAL_MS = 3000
const REFRESH_STATUS_POLL_INTERVAL_MS = 1500
const OAUTH_STATUS_POLL_INTERVAL_MS = 3000
const message = toast
const { credentialServerText, errorText, keeperStatusText, t } = useI18n()
const { currentUser } = useCurrentUser()
const canManageAccounts = computed(() => currentUser.value?.is_admin === true)
const accountPageTitle = computed(() =>
  canManageAccounts.value ? t('凭证管理', 'Credential Management') : t('凭证状态', 'Credential Status'),
)
const accountTableScrollX = computed(() =>
  canManageAccounts.value ? accountManageTableScrollX : accountReadOnlyTableScrollX,
)
const isLoading = ref(false)
const isBulkDeleting = ref(false)
const isBulkRefreshing = ref(false)
const bulkToggleAction = ref<'enable' | 'disable' | null>(null)
const isBulkOperationRunning = computed(
  () => isBulkDeleting.value || isBulkRefreshing.value || bulkToggleAction.value !== null,
)
const actingActions = ref<Set<string>>(new Set())
const accounts = ref<CodexKeeperAccount[]>([])
const accountListDegraded = ref(false)
const resetCreditFeedback = ref<Record<string, ResetCreditFeedback>>({})
const priorityRules = ref<CodexKeeperPriorityRule[]>([])
const keeperStatus = ref<CodexKeeperStatus | null>(null)
const selectedAccount = ref<CodexKeeperAccount | null>(null)
const selectedAccountModels = ref<CodexKeeperAuthFileModel[] | null>(null)
const selectedAccountModelsLoading = ref(false)
const selectedAccountModelsError = ref<string | null>(null)
const selectedAccountNote = ref<string | null>(null)
const isSelectedAccountNoteLoading = ref(false)
let selectedAccountNoteRequestID = 0
const selectedAccountKeys = ref<string[]>([])
const detailOpen = ref(false)
const authFileInput = ref<HTMLInputElement | null>(null)
const isUploadingAuthFiles = ref(false)
const isStartingAccountInspection = ref(false)
const authFileEditor = ref<AuthFileEditorState | null>(null)
const oauthDialogOpen = ref(false)
const oauthProvider = ref<CredentialOAuthProvider>('codex')
const oauthTarget = ref<CodexKeeperAccount | null>(null)
const oauthFlow = ref<'login' | 'siwc'>('login')
const oauthState = ref('')
const oauthProjectId = ref('')
const oauthDialogStatus = ref<OAuthDialogStatus>('idle')
const oauthAuthURL = ref('')
const oauthCallbackURL = ref('')
const oauthError = ref('')
const isStartingOAuth = ref(false)
const isSubmittingOAuthCallback = ref(false)
const accountDisplaySize = ref<AccountDisplaySize>(50)
const accountListPage = ref(1)
const relativeTimeNow = ref(Date.now())
const filters = reactive({
  keyword: '',
  provider: null as string | null,
  priority: 'all' as PriorityFilter,
  status: 'all' as AccountStatusFilter,
})
const accountSort = reactive({
  key: null as AccountSortKey | null,
  direction: 'asc' as SortDirection,
})
const bulkDeleteDialog = reactive({
  show: false,
})
const accountConfirmDialog = reactive({
  show: false,
  title: '',
  content: '',
  positiveText: '',
  type: 'warning' as AccountConfirmType,
  action: null as (() => Promise<void>) | null,
})
const isAccountConfirmSubmitting = ref(false)
const priorityDialog = reactive({
  show: false,
  mode: 'low' as PriorityMode,
  account: null as CodexKeeperAccount | null,
  value: null as number | null,
})
const keeperPollTokens: Record<KeeperPollMode, number> = { once: 0, accounts: 0 }
let keeperStatusTimer: number | undefined
let oauthPollToken = 0

const oauthStatusType = computed(() => {
  switch (oauthDialogStatus.value) {
    case 'waiting':
      return 'secondary'
    case 'success':
      return 'default'
    case 'error':
      return 'destructive'
    default:
      return 'outline'
  }
})
const oauthStatusText = computed(() => {
  switch (oauthDialogStatus.value) {
    case 'waiting':
      return t('等待认证中', 'Waiting for authentication')
    case 'success':
      return t('认证成功', 'Authentication successful')
    case 'error':
      return t('认证失败', 'Authentication failed')
    default:
      return t('尚未开始', 'Not started')
  }
})
const oauthProviderOptions = computed<Array<{ label: string; value: CredentialOAuthProvider }>>(() => [
  { label: 'Codex', value: 'codex' },
  { label: 'Claude', value: 'anthropic' },
  { label: 'Antigravity', value: 'antigravity' },
  { label: 'Gemini CLI', value: 'gemini-cli' },
  { label: 'Kimi', value: 'kimi' },
  { label: 'xAI', value: 'xai' },
  { label: 'Meta', value: 'meta' },
])
const oauthProviderLabel = computed(
  () => oauthProviderOptions.value.find((option) => option.value === oauthProvider.value)?.label ?? oauthProvider.value,
)
const oauthCallbackSupported = computed(() => oauthProvider.value !== 'kimi')

const priorityRuleMap = computed(() =>
  Object.fromEntries(priorityRules.value.map((rule) => [rule.account_type, rule.priority])),
)
const priorityFilterOptions = computed<Array<{ label: string; value: PriorityFilter }>>(() => [
  { label: t('手动优先 >20', 'Manual Priority >20'), value: 'high' },
  ...[...priorityRules.value]
    .filter((rule) => rule.priority >= 0 && rule.priority <= 20)
    .sort((left, right) => {
      const priorityDiff = right.priority - left.priority
      return priorityDiff === 0
        ? left.account_type.localeCompare(right.account_type)
        : priorityDiff
    })
    .map((rule) => ({
      label: `${formatInteger(rule.priority)} (${rule.account_type})`,
      value: priorityTypeFilter(rule.account_type),
    })),
  { label: t('临时降级', 'Temporary Downgrade'), value: 'minusOne' },
  { label: t('手动低优先 <-1', 'Manual Low Priority <-1'), value: 'low' },
])
const quotaSortOptions = computed(() => [
  { label: t('天', 'Day'), key: 'quotaDay' },
  { label: t('月/周', 'Month/Week'), key: 'quotaWeek' },
])

const providerOptions = computed(() =>
  [...new Set(accounts.value.map((item) => item.provider).filter(Boolean))]
    .sort((a, b) => String(a).localeCompare(String(b)))
    .map((value) => ({ label: credentialProviderLabel(String(value)), value: String(value) })),
)
const filteredAccounts = computed(() =>
  accounts.value.filter((account) => {
    const keyword = filters.keyword.trim().toLowerCase()
    if (
      keyword &&
      ![account.name, account.email ?? '', account.label ?? '', account.account ?? '', account.project_id ?? '']
        .some((value) => value.toLowerCase().includes(keyword))
    ) {
      return false
    }
    if (filters.provider && account.provider !== filters.provider) {
      return false
    }
    return matchesPriorityFilter(account, filters.priority) && matchesStatusFilter(account, filters.status)
  }),
)
const filteredDisabledAccounts = computed(() =>
  sortAccountsForDisplay(filteredAccounts.value.filter((account) => account.disabled)),
)
const filteredNormalAccounts = computed(() =>
  sortAccountsForDisplay(
    filteredAccounts.value.filter((account) => !account.disabled),
    compareNormalAccounts,
  ),
)
const sortedListAccounts = computed(() =>
  accountSort.key === null
    ? [...filteredDisabledAccounts.value, ...filteredNormalAccounts.value]
    : sortAccountsForDisplay(filteredAccounts.value),
)
const enabledAccountCount = computed(() => accounts.value.filter((account) => !account.disabled).length)
const disabledAccountCount = computed(() => accounts.value.filter((account) => account.disabled).length)
const hasDisabledAccounts = computed(() => disabledAccountCount.value > 0)
const showListLoadingState = computed(() => isLoading.value && accounts.value.length === 0)
const isKeeperRunning = computed(() => keeperStatus.value?.running === true)
const isKeeperDaemonRunning = computed(() => keeperStatus.value?.daemon_running === true)
const keeperRunningModes = computed(() => new Set(keeperStatus.value?.running_modes ?? []))
const isAccountInspectionRunning = computed(() => keeperRunningModes.value.has('once'))
const isAccountInspectionBlocked = computed(
  () => isAccountInspectionRunning.value || keeperRunningModes.value.has('daemon'),
)
const keeperStateType = computed(() => {
  if (isKeeperRunning.value || isKeeperDaemonRunning.value) {
    return 'default'
  }
  if (keeperStatus.value?.state === 'error' || keeperStatus.value?.state === 'failed') {
    return 'destructive'
  }
  if (keeperStatus.value?.state === 'stopping') {
    return 'secondary'
  }
  return 'outline'
})
const keeperStatusDetailText = computed(() => {
  const detail = keeperStatus.value?.detail
  if (isKeeperDaemonRunning.value && !isKeeperRunning.value) {
    return t('自动巡检已开启', 'Automatic inspection is enabled')
  }
  if (!detail) {
    return t('未运行', 'Not running')
  }
  return keeperStatusText(detail)
})
const keeperStatusFootnoteText = computed(() =>
  isKeeperDaemonRunning.value ? t('等待 Cron 调度', 'Waiting for Cron schedule') : t('后台自动巡检', 'Background automatic inspection'),
)
const unauthorizedErrorAccountCount = computed(
  () => accounts.value.filter((account) => isCodexCredential(account) && account.last_status_code === 401).length,
)
const quotaExhaustedAccountCount = computed(
  () => accounts.value.filter(isQuotaExhaustedAccount).length,
)
const activeFilterCount = computed(
  () =>
    Number(filters.keyword.trim() !== '') +
    Number(filters.provider !== null) +
    Number(filters.priority !== 'all') +
    Number(filters.status !== 'all'),
)
const accountListPageCount = computed(() => accountPageCount(sortedListAccounts.value.length))
const visibleListAccounts = computed(() =>
  pagedAccounts(sortedListAccounts.value, accountListPage.value),
)
const selectableVisibleAccounts = computed(() =>
  visibleListAccounts.value.filter((account) => isCredentialMutable(account) && !isRowActing(account) && !isBulkOperationRunning.value),
)
const visibleSelectedCount = computed(() => {
  const selected = new Set(selectedAccountKeys.value)
  return selectableVisibleAccounts.value.filter((account) => selected.has(account.name)).length
})
const visibleSelectionState = computed<boolean | 'indeterminate'>(() => {
  if (selectableVisibleAccounts.value.length === 0 || visibleSelectedCount.value === 0) {
    return false
  }
  return visibleSelectedCount.value === selectableVisibleAccounts.value.length ? true : 'indeterminate'
})
const activeQuotaSortLabel = computed(() => {
  if (accountSort.key === 'quotaDay') {
    return t('天', 'Day')
  }
  if (accountSort.key === 'quotaWeek') {
    return t('月/周', 'Month/Week')
  }
  return ''
})
const sortDirectionMark = computed(() => (accountSort.direction === 'asc' ? '↑' : '↓'))

function accountPageCount(rowCount: number): number {
  return Math.max(1, Math.ceil(rowCount / accountDisplaySize.value))
}

function pagedAccounts(source: CodexKeeperAccount[], page: number): CodexKeeperAccount[] {
  const safePage = clampPage(page, accountPageCount(source.length))
  const start = (safePage - 1) * accountDisplaySize.value
  return source.slice(start, start + accountDisplaySize.value)
}

function clampPage(page: number, pageCount: number): number {
  return Math.min(Math.max(1, page), pageCount)
}

function resetAccountPages() {
  accountListPage.value = 1
}

function clampAccountPages() {
  accountListPage.value = clampPage(accountListPage.value, accountListPageCount.value)
}

function isAccountDisplaySize(value: unknown): value is AccountDisplaySize {
  return value === 50 || value === 100 || value === 150 || value === 200
}

function isAccountSortKey(value: unknown): value is AccountSortKey {
  return (
    value === 'quotaDay' ||
    value === 'quotaWeek' ||
    value === 'provider' ||
    value === 'status' ||
    value === 'priority' ||
    value === 'lastCheckedAt'
  )
}

function isSortDirection(value: unknown): value is SortDirection {
  return value === 'asc' || value === 'desc'
}

function readAccountStatusPreferences(): AccountStatusPreferences | null {
  if (typeof localStorage === 'undefined') {
    return null
  }
  const raw = localStorage.getItem(ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY)
  if (!raw) {
    return null
  }
  try {
    const value: unknown = JSON.parse(raw)
    return value && typeof value === 'object' ? (value as AccountStatusPreferences) : null
  } catch {
    return null
  }
}

function restoreAccountStatusPreferences() {
  const preferences = readAccountStatusPreferences()
  if (!preferences) {
    return
  }
  if (isAccountDisplaySize(preferences.displaySize)) {
    accountDisplaySize.value = preferences.displaySize
  }
  const sort = preferences.sort
  if (!sort || typeof sort !== 'object') {
    return
  }
  if (sort.key === null) {
    accountSort.key = null
    accountSort.direction = 'asc'
    return
  }
  if (isAccountSortKey(sort.key) && isSortDirection(sort.direction)) {
    accountSort.key = sort.key
    accountSort.direction = sort.direction
  }
}

function saveAccountStatusPreferences() {
  if (typeof localStorage === 'undefined') {
    return
  }
  try {
    localStorage.setItem(
      ACCOUNT_STATUS_PREFERENCE_STORAGE_KEY,
      JSON.stringify({
        displaySize: accountDisplaySize.value,
        sort: {
          key: accountSort.key,
          direction: accountSort.direction,
        },
      }),
    )
  } catch {
    // Keep the page usable when local storage is unavailable.
  }
}
const selectedAccountNames = computed(() =>
  selectedAccountKeys.value.map((key) => String(key)),
)
const selectedAccounts = computed(() => {
  const selectedNames = new Set(selectedAccountNames.value)
  return accounts.value.filter((account) => selectedNames.has(account.name))
})
const selectedDisabledAccounts = computed(() =>
  selectedAccounts.value.filter((account) => account.disabled),
)
const selectedEnabledAccounts = computed(() =>
  selectedAccounts.value.filter((account) => !account.disabled),
)
const selectedAccountCount = computed(() => selectedAccountNames.value.length)
const canBulkDelete = computed(() =>
  selectedAccountCount.value > 0 && !isBulkOperationRunning.value,
)
const canRefreshSelected = computed(
  () => selectedAccounts.value.some(isCodexCredential) && !isBulkOperationRunning.value && !isLoading.value,
)
const canBulkEnable = computed(
  () => selectedDisabledAccounts.value.length > 0 && !isBulkOperationRunning.value,
)
const canBulkDisable = computed(
  () => selectedEnabledAccounts.value.length > 0 && !isBulkOperationRunning.value,
)
const bulkDeletePreviewNames = computed(() => selectedAccountNames.value.slice(0, 5))
const bulkDeletePreviewOverflow = computed(() =>
  Math.max(0, selectedAccountCount.value - bulkDeletePreviewNames.value.length),
)
const bulkDeleteDialogTitle = computed(() => t('批量删除凭证', 'Bulk Delete Credentials'))
const bulkDeleteWarningText = computed(() =>
  t(
    `将删除已选 ${selectedAccountCount.value} 份凭证，并从 CPA 删除认证文件。此操作不可恢复。`,
    `This will delete ${selectedAccountCount.value} selected credentials and remove their auth files from CPA. This cannot be undone.`,
  ),
)
const canSubmitPriority = computed(() => {
  if (priorityDialog.mode === 'custom') {
    return priorityDialog.account !== null && priorityDialog.value !== null && Number.isInteger(priorityDialog.value)
  }
  if (priorityDialog.mode === 'default') {
    return priorityDialog.account !== null && defaultPriority(priorityDialog.account) !== null
  }
  const value = priorityDialog.value
  if (value === null || !Number.isInteger(value)) {
    return false
  }
  return priorityDialog.mode === 'low' ? value < -1 : value > 20
})
const priorityDialogTitle = computed(() => t('修改优先级', 'Change Priority'))
const priorityDialogHint = computed(() => {
  if (priorityDialog.mode === 'custom') {
    return t('为此凭证设置 CPA 调度优先级。', 'Set the CPA scheduling priority for this credential.')
  }
  if (priorityDialog.mode === 'low') {
    return t('手动低优先级必须小于 -1，巡检永远不会自动调整。', 'Manual low priority must be less than -1. Inspection will never adjust it automatically.')
  }
  if (priorityDialog.mode === 'high') {
    return t('手动优先必须大于 20，额度耗尽时会临时降为 -1，恢复后回到该值。', 'Manual priority must be greater than 20. When quota is exhausted it is temporarily lowered to -1 and restored to this value after recovery.')
  }
  const account = priorityDialog.account
  const value = account ? defaultPriority(account) : null
  return value === null
    ? t('该凭证类型没有配置默认优先级，不能使用类型默认值。', 'This credential type has no default priority, so the type default cannot be used.')
    : t(`将优先级设置为当前凭证类型默认值 ${value}。`, `Set the priority to the current credential type default: ${value}.`)
})
const priorityModeOptions = computed(() => {
  if (!isCodexCredential(priorityDialog.account)) {
    return [{ label: t('自定义优先级', 'Custom Priority'), value: 'custom', disabled: false }]
  }
  const defaultValue = priorityDialog.account ? defaultPriority(priorityDialog.account) : null
  return [
    { label: t('手动低优先 (< -1)', 'Manual Low Priority (< -1)'), value: 'low' },
    { label: t('手动优先 (> 20)', 'Manual Priority (> 20)'), value: 'high' },
    {
      label: defaultValue === null
        ? t('类型默认优先级（不可用）', 'Type Default Priority (unavailable)')
        : t(`类型默认优先级 (${defaultValue})`, `Type Default Priority (${defaultValue})`),
      value: 'default',
      disabled: defaultValue === null,
    },
  ]
})

function matchesPriorityFilter(account: CodexKeeperAccount, value: PriorityFilter): boolean {
  const priority = accountPriority(account)
  if (value === 'high') {
    return priority > 20
  }
  if (value === 'minusOne') {
    return priority === -1
  }
  if (value === 'low') {
    return priority < -1
  }
  const accountType = priorityTypeFromFilter(value)
  if (accountType !== null) {
    return (
      credentialPlanType(account) === accountType &&
      priority >= 0 &&
      priority <= 20
    )
  }
  return true
}

function matchesStatusFilter(account: CodexKeeperAccount, value: AccountStatusFilter): boolean {
  if (value === 'enabled') {
    return !account.disabled
  }
  if (value === 'disabled') {
    return account.disabled
  }
  if (value === 'unauthorized') {
    return account.last_status_code === 401
  }
  if (value === 'quotaExhausted') {
    return isQuotaExhaustedAccount(account)
  }
  return true
}

function toggleStatusFilter(value: Exclude<AccountStatusFilter, 'all'>) {
  filters.status = filters.status === value ? 'all' : value
}

function isStatusFilterActive(value: Exclude<AccountStatusFilter, 'all'>): boolean {
  return filters.status === value
}

function setProviderFilter(value: unknown) {
  filters.provider = typeof value === 'string' ? value : null
}

function setPriorityFilter(value: unknown) {
  if (typeof value === 'string' && priorityFilterOptions.value.some((option) => option.value === value)) {
    filters.priority = value as PriorityFilter
    return
  }
  filters.priority = 'all'
}

function setAccountDisplaySize(value: unknown) {
  const parsed = typeof value === 'number' ? value : Number(value)
  if (isAccountDisplaySize(parsed)) {
    accountDisplaySize.value = parsed
  }
}

function handleAccountPageChange(value: number) {
  accountListPage.value = value
}

function defaultSortDirection(key: AccountSortKey): SortDirection {
  return key === 'priority' || key === 'lastCheckedAt' ? 'desc' : 'asc'
}

function handleQuotaSortSelect(key: string | number) {
  if (key === 'quotaDay' || key === 'quotaWeek') {
    toggleAccountSort(key)
  }
}

function toggleAccountSort(key: AccountSortKey) {
  if (accountSort.key === key) {
    accountSort.direction = accountSort.direction === 'asc' ? 'desc' : 'asc'
    return
  }
  accountSort.key = key
  accountSort.direction = defaultSortDirection(key)
}

function isAccountSortActive(key: AccountSortKey): boolean {
  return accountSort.key === key
}

function accountSortMark(key: AccountSortKey): string {
  return isAccountSortActive(key) ? sortDirectionMark.value : ''
}

function accountPriority(account: CodexKeeperAccount): number {
  return account.priority ?? 0
}

function isQuotaExhaustedAccount(account: CodexKeeperAccount): boolean {
  return isCodexCredential(account) && !account.disabled && accountPriority(account) === -1
}

function priorityTypeFilter(accountType: string): PriorityTypeFilter {
  return `type:${accountType}`
}

function priorityTypeFromFilter(value: PriorityFilter): string | null {
  return value.startsWith('type:') ? value.slice('type:'.length) : null
}

function normalAccountTypePriority(account: CodexKeeperAccount): number {
  const planType = credentialPlanType(account)
  if (!planType) {
    return Number.NEGATIVE_INFINITY
  }
  return priorityRuleMap.value[planType] ?? Number.NEGATIVE_INFINITY
}

function compareNormalAccounts(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  const priorityDiff = normalAccountTypePriority(right) - normalAccountTypePriority(left)
  if (priorityDiff !== 0) {
    return priorityDiff
  }
  return compareAccountFileName(left, right)
}

function sortAccountsForDisplay(
  source: CodexKeeperAccount[],
  defaultCompare?: (left: CodexKeeperAccount, right: CodexKeeperAccount) => number,
): CodexKeeperAccount[] {
  const rows = [...source]
  if (accountSort.key === null) {
    return defaultCompare ? rows.sort(defaultCompare) : rows
  }
  return rows.sort(compareAccountsByActiveSort)
}

function compareAccountsByActiveSort(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  const direction = accountSort.direction
  let result = 0
  switch (accountSort.key) {
    case 'quotaDay':
    case 'quotaWeek':
      result = compareNullableNumber(
        quotaSortRemainingPercent(left, accountSort.key),
        quotaSortRemainingPercent(right, accountSort.key),
        direction,
      )
      break
    case 'provider':
      result = compareNullableString(left.provider, right.provider, direction)
      break
    case 'status':
      result = compareNullableNumber(left.disabled ? 1 : 0, right.disabled ? 1 : 0, direction)
      break
    case 'priority':
      result = compareNullableNumber(accountPriority(left), accountPriority(right), direction)
      break
    case 'lastCheckedAt':
      result = compareNullableNumber(
        timestampValue(credentialActivityAt(left)),
        timestampValue(credentialActivityAt(right)),
        direction,
      )
      break
    default:
      result = 0
  }
  return result === 0 ? compareAccountFileName(left, right) : result
}

function compareNullableNumber(
  left: number | null,
  right: number | null,
  direction: SortDirection,
): number {
  if (left === null && right === null) {
    return 0
  }
  if (left === null) {
    return 1
  }
  if (right === null) {
    return -1
  }
  const result = left - right
  return direction === 'asc' ? result : -result
}

function compareNullableString(
  left: string | null,
  right: string | null,
  direction: SortDirection,
): number {
  if (left === null && right === null) {
    return 0
  }
  if (left === null) {
    return 1
  }
  if (right === null) {
    return -1
  }
  const result = left.localeCompare(right)
  return direction === 'asc' ? result : -result
}

function timestampValue(value: string | null): number | null {
  if (!value) {
    return null
  }
  const timestamp = new Date(value).getTime()
  return Number.isNaN(timestamp) ? null : timestamp
}

function compareAccountFileName(left: CodexKeeperAccount, right: CodexKeeperAccount): number {
  return left.name.localeCompare(right.name)
}

function defaultPriority(account: CodexKeeperAccount): number | null {
  const planType = credentialPlanType(account)
  if (!isCodexCredential(account) || !planType) {
    return null
  }
  return priorityRuleMap.value[planType] ?? null
}

function credentialActivityAt(account: CodexKeeperAccount): string | null {
  return isCodexCredential(account)
    ? account.last_checked_at
    : account.last_refresh_at ?? account.modified_at
}

function isCodexCredential(account: CodexKeeperAccount | null | undefined): boolean {
  return account?.provider?.trim().toLowerCase() === 'codex'
}

function isCredentialMutable(account: CodexKeeperAccount | null | undefined): boolean {
  return account?.runtime_only !== true
}

function credentialProviderLabel(provider: string | null | undefined): string {
  const normalized = provider?.trim().toLowerCase()
  const labels: Record<string, string> = {
    anthropic: 'Claude',
    claude: 'Claude',
    codex: 'Codex',
    'gemini-cli': 'Gemini CLI',
    gemini: 'Gemini',
    aistudio: 'AI Studio',
    antigravity: 'Antigravity',
    iflow: 'iFlow',
    kimi: 'Kimi',
    qwen: 'Qwen',
    vertex: 'Vertex AI',
    xai: 'xAI',
  }
  if (!normalized || normalized === 'unknown') {
    return t('未知', 'Unknown')
  }
  return labels[normalized] ?? provider ?? normalized
}

function credentialObject(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

function credentialQuotaObservation(
  label: string,
  value: unknown,
): CredentialQuotaObservation | null {
  const observation = credentialObject(value)
  if (!observation) {
    return null
  }
  const rawObservedAt = observation.observed_at ?? observation.observedAt
  const observedAt = typeof rawObservedAt === 'string' ? rawObservedAt : null
  const signalObject = credentialObject(observation.signals)
  const signals = signalObject
    ? Object.entries(signalObject)
        .filter(([, signalValue]) => signalValue !== null && signalValue !== undefined)
        .map(([name, signalValue]) => ({ name, value: String(signalValue) }))
    : []
  return observedAt || signals.length > 0 ? { label, observedAt, signals } : null
}

function credentialQuotaObservations(account: CodexKeeperAccount): CredentialQuotaObservation[] {
  const observations: CredentialQuotaObservation[] = []
  const overall = credentialQuotaObservation(t('凭证', 'Credential'), account.quota)
  if (overall) {
    observations.push(overall)
  }
  Object.entries(account.model_quotas ?? {}).forEach(([model, value]) => {
    const observation = credentialQuotaObservation(model, value)
    if (observation) {
      observations.push(observation)
    }
  })
  return observations
}

const selectedAccountQuotaObservations = computed(() =>
  selectedAccount.value ? credentialQuotaObservations(selectedAccount.value) : [],
)

function accountTypeLabel(accountType: string | null): string {
  const normalized = accountType?.trim().toLowerCase()
  if (!normalized || normalized === 'unknown') {
    return t('未知', 'Unknown')
  }
  if (normalized === 'k12') {
    return 'K12'
  }
  if (normalized === 'oauth') {
    return 'OAuth'
  }
  return accountType ?? normalized
}

function credentialPlanType(account: CodexKeeperAccount): string | null {
  return account.plan_type ?? (isCodexCredential(account) ? account.account_type : null)
}

function isPaidQuotaWindowAccount(accountType: string | null): boolean {
  const normalized = accountType?.trim().toLowerCase()
  return normalized === 'plus' || normalized === 'team' || normalized === 'k12' || normalized?.startsWith('pro') === true
}

function isFreeQuotaWindowAccount(accountType: string | null): boolean {
  return accountType?.trim().toLowerCase() === 'free'
}

function quotaWindowSecondsFor(account: CodexKeeperAccount, window: 'primary' | 'secondary'): number | null {
  if (window === 'primary') {
    return account.primary_window_seconds ?? account.primary_window_usage?.window_seconds ?? null
  }
  return account.secondary_window_seconds ?? account.secondary_window_usage?.window_seconds ?? null
}

function hasFiveHourQuotaWindow(account: CodexKeeperAccount): boolean {
  return (
    quotaWindowSecondsFor(account, 'primary') === CODEX_FIVE_HOUR_WINDOW_SECONDS ||
    quotaWindowSecondsFor(account, 'secondary') === CODEX_FIVE_HOUR_WINDOW_SECONDS
  )
}

function isPaidQuotaWindow(account: CodexKeeperAccount): boolean {
  const planType = credentialPlanType(account)
  return (
    isPaidQuotaWindowAccount(planType) ||
    (quotaWindowSecondsFor(account, 'primary') === CODEX_FIVE_HOUR_WINDOW_SECONDS &&
      quotaWindowSecondsFor(account, 'secondary') === CODEX_WEEK_WINDOW_SECONDS)
  )
}

function isFreeQuotaWindow(account: CodexKeeperAccount): boolean {
  const planType = credentialPlanType(account)
  return (
    isFreeQuotaWindowAccount(planType) ||
    (quotaWindowSecondsFor(account, 'primary') === CODEX_MONTH_WINDOW_SECONDS &&
      quotaWindowSecondsFor(account, 'secondary') === null)
  )
}

function hasSingleProWeeklyQuotaWindow(account: CodexKeeperAccount): boolean {
  return (
    credentialPlanType(account)?.trim().toLowerCase().startsWith('pro') === true &&
    account.secondary_used_percent === null &&
    quotaWindowSecondsFor(account, 'secondary') === null
  )
}

function quotaWindowLabels(account: CodexKeeperAccount): { primary: string; secondary: string } {
  if (isFreeQuotaWindow(account)) {
    return { primary: t('月配额', 'Monthly Quota'), secondary: t('次配额', 'Secondary Quota') }
  }
  if (isPaidQuotaWindow(account)) {
    // The upstream usage payload decides the real window length; the plan
    // type alone does not. Pro accounts, for example, can report a weekly
    // primary window, so label by actual seconds and only fall back to the
    // conventional 5-hour/weekly pair when the seconds are unknown.
    const primaryLabel = quotaWindowLabelForSeconds(quotaWindowSecondsFor(account, 'primary'))
    const secondaryLabel = quotaWindowLabelForSeconds(quotaWindowSecondsFor(account, 'secondary'))
    return {
      primary: primaryLabel ?? (hasSingleProWeeklyQuotaWindow(account)
        ? t('周配额', 'Weekly Quota')
        : t('5小时配额', '5-Hour Quota')),
      secondary: secondaryLabel ?? t('周配额', 'Weekly Quota'),
    }
  }
  return { primary: t('主', 'Primary'), secondary: t('次', 'Secondary') }
}

function quotaWindowLabelForSeconds(seconds: number | null): string | null {
  if (seconds === CODEX_FIVE_HOUR_WINDOW_SECONDS) {
    return t('5小时配额', '5-Hour Quota')
  }
  if (seconds === CODEX_WEEK_WINDOW_SECONDS) {
    return t('周配额', 'Weekly Quota')
  }
  if (seconds === CODEX_MONTH_WINDOW_SECONDS) {
    return t('月配额', 'Monthly Quota')
  }
  return null
}

function shouldShowQuotaWindow(account: CodexKeeperAccount): boolean {
  return isCodexCredential(account) && !account.disabled
}

function quotaWindowItems(account: CodexKeeperAccount): QuotaWindowItem[] {
  if (!shouldShowQuotaWindow(account) || account.primary_used_percent === null) {
    return []
  }
  const labels = quotaWindowLabels(account)
  const items = [
    {
      label: labels.primary,
      usedPercent: Math.max(0, Math.min(100, account.primary_used_percent)),
      remainingPercent: remainingQuotaPercent(account.primary_used_percent),
      resetAt: account.primary_reset_at,
      usage: account.primary_window_usage,
    },
  ]
  if (account.secondary_used_percent !== null && !isFreeQuotaWindow(account)) {
    items.push({
      label: labels.secondary,
      usedPercent: Math.max(0, Math.min(100, account.secondary_used_percent)),
      remainingPercent: remainingQuotaPercent(account.secondary_used_percent),
      resetAt: account.secondary_reset_at,
      usage: account.secondary_window_usage,
    })
  }
  return items
}

function quotaSortRemainingPercent(account: CodexKeeperAccount, key: AccountSortKey): number | null {
  if (!shouldShowQuotaWindow(account)) {
    return null
  }
  if (key === 'quotaDay') {
    if (isFreeQuotaWindow(account) || hasSingleProWeeklyQuotaWindow(account)) {
      return null
    }
    return nullableRemainingQuotaPercent(account.primary_used_percent)
  }
  if (key === 'quotaWeek') {
    if (isFreeQuotaWindow(account) || hasSingleProWeeklyQuotaWindow(account)) {
      return nullableRemainingQuotaPercent(account.primary_used_percent)
    }
    if (isPaidQuotaWindow(account)) {
      return nullableRemainingQuotaPercent(account.secondary_used_percent)
    }
  }
  return null
}

function nullableRemainingQuotaPercent(usedPercent: number | null): number | null {
  return usedPercent === null ? null : remainingQuotaPercent(usedPercent)
}

function remainingQuotaPercent(usedPercent: number): number {
  return Math.max(0, Math.min(100, 100 - usedPercent))
}

function quotaBarTone(percent: number): string {
  if (percent < 30) {
    return 'is-danger'
  }
  if (percent < 70) {
    return 'is-warning'
  }
  return 'is-healthy'
}

function formatQuotaResetTime(value: string | null): string | null {
  const formatted = formatDateTime(value)
  return formatted === '-' ? null : formatted
}

function quotaWindowUsageTags(item: QuotaWindowItem): QuotaUsageTag[] {
  if (!item.resetAt || item.usage?.stale === true) {
    return [{ label: t('状态', 'Status'), value: t('需刷新', 'Needs refresh'), tone: 'stale' }]
  }
  const usage = item.usage
  return [
    { label: t('请求', 'Requests'), value: formatInteger(usage?.records ?? 0) },
    { label: 'Tokens', value: formatCompact(usage?.total_tokens ?? 0) },
    { label: t('费用', 'Cost'), value: formatUsd(usage?.estimated_cost_usd ?? 0) },
  ]
}

function quotaWindowUsageTitle(item: QuotaWindowItem): string {
  const usage = item.usage
  if (!item.resetAt || usage?.stale === true) {
    return t(`${item.label} 额度数据需刷新`, `${item.label} quota data needs refresh`)
  }
  if (!usage) {
    return t(`${item.label} 本窗口暂无用量`, `${item.label} has no usage in this window`)
  }
  const unpriced =
    usage.unpriced_records > 0
      ? t(`，未计价 ${formatInteger(usage.unpriced_records)} 条`, `, ${formatInteger(usage.unpriced_records)} unpriced records`)
      : ''
  return t(
    `${item.label} 当前窗口：${formatInteger(usage.records)} 次请求，${formatCompact(usage.total_tokens)} Tokens，${formatUsd(usage.estimated_cost_usd)}${unpriced}`,
    `${item.label} current window: ${formatInteger(usage.records)} requests, ${formatCompact(usage.total_tokens)} Tokens, ${formatUsd(usage.estimated_cost_usd)}${unpriced}`,
  )
}

function quotaWindowUsageTone(item: QuotaWindowItem): string {
  return !item.resetAt || item.usage?.stale === true ? 'is-stale' : ''
}

function quotaWindowProjectedCost(item: QuotaWindowItem): number | null {
  if (!item.resetAt || item.usage?.stale === true || item.usedPercent <= 0) {
    return null
  }
  const cost = item.usage?.estimated_cost_usd ?? 0
  if (!Number.isFinite(cost) || cost < 0) {
    return null
  }
  return cost / (item.usedPercent / 100)
}

function quotaWindowPredictionTitle(item: QuotaWindowItem): string {
  if (!item.resetAt || item.usage?.stale === true) {
    return t(`${item.label} 窗口预测需刷新额度数据`, `${item.label} projection needs refreshed quota data`)
  }
  const projectedCost = quotaWindowProjectedCost(item)
  if (projectedCost === null) {
    return t(`${item.label} 已用配额为 0%，暂无窗口预测`, `${item.label} usage is 0%; no projection yet`)
  }
  const currentCost = item.usage?.estimated_cost_usd ?? 0
  return t(
    `${item.label} 窗口预测：${formatUsd(currentCost)} ÷ ${item.usedPercent}% = ${formatUsd(projectedCost)}`,
    `${item.label} projection: ${formatUsd(currentCost)} ÷ ${item.usedPercent}% = ${formatUsd(projectedCost)}`,
  )
}

function latestActionText(account: CodexKeeperAccount): string {
  const text = account.last_error?.trim() || account.latest_action?.trim() || account.status_message?.trim()
  return text ? credentialServerText(text, '凭证状态', 'Credential status') : '-'
}

function isTransientCredentialWarning(account: CodexKeeperAccount): boolean {
  const text = [account.status_message, account.last_error, account.latest_action]
    .filter((value): value is string => Boolean(value?.trim()))
    .join('\n')
    .toLowerCase()
  return text.includes('service_unavailable_error') || text.includes('server_is_overloaded')
}

function accountStatusTags(account: CodexKeeperAccount) {
  const remoteStatus = account.status?.trim().toLowerCase()
  const statusMessage = account.status_message?.trim()
  const healthyStatuses = new Set(['active', 'ok', 'ready', 'healthy', 'success', 'available'])
  const unavailableStatuses = new Set(['error', 'failed', 'unavailable'])
  const issueTag = account.requires_sign_in
    ? { label: t('需要重新登录', 'Sign-in required'), tone: 'is-danger' }
    : isTransientCredentialWarning(account)
    ? { label: t('状态警告', 'Status Warning'), tone: 'is-warning' }
    : account.last_status_code === 401
    ? { label: t('401报错', '401 Error'), tone: 'is-danger' }
    : isQuotaExhaustedAccount(account)
      ? { label: t('额度耗尽', 'Quota Exhausted'), tone: 'is-purple' }
      : account.unavailable || (remoteStatus !== undefined && unavailableStatuses.has(remoteStatus))
        ? { label: t('不可用', 'Unavailable'), tone: 'is-danger' }
        : !account.disabled && remoteStatus && !healthyStatuses.has(remoteStatus)
          ? { label: account.status ?? remoteStatus, tone: 'is-warning' }
          : !account.disabled && statusMessage && !healthyStatuses.has(statusMessage.toLowerCase())
            ? { label: t('状态警告', 'Status Warning'), tone: 'is-warning' }
            : null

  return [
    {
      label: account.disabled ? t('已禁用', 'Disabled') : t('启用中', 'Enabled'),
      tone: account.disabled ? 'is-warning' : 'is-success',
    },
    account.runtime_only
      ? { label: t('运行时', 'Runtime'), tone: 'is-type' }
      : null,
    issueTag,
    account.v8_supported && account.recovery_stage
      ? { label: coreRecoveryStageLabel(account.recovery_stage), tone: 'is-type' }
      : null,
    account.use_siwc ? { label: `SIWC ${account.siwc_status ?? '-'}`, tone: account.siwc_status === 'valid' ? 'is-type' : 'is-warning' } : null,
  ].filter((item): item is { label: string; tone: string } => item !== null)
}

function accountIdentityTitle(account: CodexKeeperAccount): string {
  const primary = credentialDisplayName(account)
  const statusTags = accountStatusTags(account)
  const statusLabel = statusTags.map((item) => item.label).join(' / ')
  return `${primary}\n${account.name}\n${t('提供商', 'Provider')} ${credentialProviderLabel(account.provider)}\n${t('状态', 'Status')} ${statusLabel}`
}

function credentialDisplayName(account: CodexKeeperAccount): string {
  return account.email ?? account.account ?? account.label ?? account.project_id ?? account.user_id ?? account.name
}

function quotaWindowTooltip(item: QuotaWindowItem): string {
  const resetTime = formatQuotaResetTime(item.resetAt)
  return resetTime
    ? t(
        `${item.label}剩余 ${item.remainingPercent}%，刷新 ${resetTime}；${quotaWindowUsageTitle(item)}`,
        `${item.label} ${item.remainingPercent}% remaining, refreshes ${resetTime}; ${quotaWindowUsageTitle(item)}`,
      )
    : t(
        `${item.label}剩余 ${item.remainingPercent}%；${quotaWindowUsageTitle(item)}`,
        `${item.label} ${item.remainingPercent}% remaining; ${quotaWindowUsageTitle(item)}`,
      )
}

async function loadAccounts() {
  isLoading.value = true
  try {
    const [accountsResponse, nextStatus] = await Promise.all([
      listCodexKeeperAccounts(),
      getCodexKeeperStatus(),
    ])
    accounts.value = accountsResponse.items
    accountListDegraded.value = accountsResponse.degraded === true
    if (selectedAccount.value) {
      selectedAccount.value = accountsResponse.items.find((account) => account.name === selectedAccount.value?.name) ?? null
      if (!selectedAccount.value) {
        detailOpen.value = false
      }
    }
    priorityRules.value = accountsResponse.priority_rules
    keeperStatus.value = nextStatus
  } catch (error) {
    message.error(errorText(error, '加载凭证状态失败', 'Failed to load credential status'))
  } finally {
    isLoading.value = false
  }
}

async function loadKeeperStatus() {
  try {
    keeperStatus.value = await getCodexKeeperStatus()
  } catch {
    return
  }
}

function isAccountSelected(account: CodexKeeperAccount): boolean {
  return selectedAccountKeys.value.includes(account.name)
}

function setAccountSelected(account: CodexKeeperAccount, selected: boolean | 'indeterminate') {
  const names = new Set(selectedAccountKeys.value)
  if (selected === true) {
    names.add(account.name)
  } else {
    names.delete(account.name)
  }
  selectedAccountKeys.value = [...names]
}

function setAllVisibleAccounts(selected: boolean | 'indeterminate') {
  const names = new Set(selectedAccountKeys.value)
  for (const account of selectableVisibleAccounts.value) {
    if (selected === true) {
      names.add(account.name)
    } else {
      names.delete(account.name)
    }
  }
  selectedAccountKeys.value = [...names]
}

function pruneSelectedAccountKeys() {
  const availableNames = new Set(
    visibleListAccounts.value
      .filter(isCredentialMutable)
      .map((account) => account.name),
  )
  selectedAccountKeys.value = selectedAccountKeys.value.filter((key) =>
    availableNames.has(String(key)),
  )
}

function authFileStringField(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function authFileBooleanField(value: unknown): boolean {
  if (typeof value === 'boolean') {
    return value
  }
  if (typeof value === 'number') {
    return value !== 0
  }
  if (typeof value === 'string') {
    return ['1', 'true', 'yes', 'on'].includes(value.trim().toLowerCase())
  }
  return false
}

function authFileIntegerField(value: unknown): number | null {
  if (value === null || value === undefined || String(value).trim() === '') {
    return null
  }
  const parsed = typeof value === 'number' ? value : Number(String(value).trim())
  return Number.isSafeInteger(parsed) ? parsed : null
}

function authFileHeadersField(value: unknown): Record<string, string> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return Object.fromEntries(
    Object.entries(value).filter(([, headerValue]) => typeof headerValue === 'string'),
  )
}

function parseAuthFileHeaders(value: string): Record<string, string> {
  if (!value.trim()) {
    return {}
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    throw new Error(t('自定义请求头必须是有效的 JSON。', 'Custom headers must be valid JSON.'))
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(t('自定义请求头必须是 JSON 对象。', 'Custom headers must be a JSON object.'))
  }
  const entries = Object.entries(parsed)
  if (entries.some(([, headerValue]) => typeof headerValue !== 'string')) {
    throw new Error(t('每个自定义请求头的值都必须是字符串。', 'Every custom header value must be a string.'))
  }
  return Object.fromEntries(entries as Array<[string, string]>)
}

function buildAuthFileEditorFields(editor: AuthFileEditorState): CodexKeeperAuthFileFields {
  if (!editor.json) {
    return {}
  }
  const original = editor.json
  const fields: CodexKeeperAuthFileFields = {}
  if (editor.prefix.trim() !== authFileStringField(original.prefix).trim()) {
    fields.prefix = editor.prefix.trim()
  }
  if (editor.proxyUrl.trim() !== authFileStringField(original.proxy_url).trim()) {
    fields.proxy_url = editor.proxyUrl.trim()
  }
  if (editor.weightTouched && editor.weight !== authFileIntegerField(original.weight)) {
    fields.weight = editor.weight
  }
  if (editor.requestRetryTouched && editor.requestRetry !== authFileIntegerField(original.request_retry)) {
    fields.request_retry = editor.requestRetry
  }
  if (editor.websocketsTouched && editor.websockets !== authFileBooleanField(original.websockets ?? original.websocket)) {
    fields.websockets = editor.websockets
  }
  if (editor.usingApiTouched && editor.usingApi !== authFileBooleanField(original.using_api)) {
    fields.using_api = editor.usingApi
  }
  if (editor.noteTouched && editor.note.trim() !== authFileStringField(original.note).trim()) {
    fields.note = editor.note.trim()
  }
  if (editor.headersTouched) {
    const nextHeaders = parseAuthFileHeaders(editor.headersText)
    const originalHeaders = authFileHeadersField(original.headers)
    const headerPatch: Record<string, string> = {}
    Object.entries(nextHeaders).forEach(([name, value]) => {
      if (originalHeaders[name] !== value) {
        headerPatch[name] = value
      }
    })
    Object.keys(originalHeaders).forEach((name) => {
      if (!(name in nextHeaders)) {
        headerPatch[name] = ''
      }
    })
    if (Object.keys(headerPatch).length > 0) {
      fields.headers = headerPatch
    }
  }
  return fields
}

function buildAuthFileUpdatedText(editor: AuthFileEditorState): string {
  if (!editor.json) {
    return editor.rawText
  }
  const fields = buildAuthFileEditorFields(editor)
  const updated: Record<string, unknown> = { ...editor.json }
  if (fields.prefix !== undefined) {
    if (fields.prefix) {
      updated.prefix = fields.prefix
    } else {
      delete updated.prefix
    }
  }
  if (fields.proxy_url !== undefined) {
    if (fields.proxy_url) {
      updated.proxy_url = fields.proxy_url
    } else {
      delete updated.proxy_url
    }
  }
  if (fields.weight !== undefined) {
    if (fields.weight === null) delete updated.weight
    else updated.weight = fields.weight
  }
  if (fields.request_retry !== undefined) {
    if (fields.request_retry === null) delete updated.request_retry
    else updated.request_retry = fields.request_retry
  }
  if (fields.websockets !== undefined) {
    delete updated.websocket
    updated.websockets = fields.websockets
  }
  if (fields.using_api !== undefined) {
    updated.using_api = fields.using_api
  }
  if (fields.note !== undefined) {
    if (fields.note) {
      updated.note = fields.note
    } else {
      delete updated.note
    }
  }
  if (fields.headers !== undefined) {
    const headers = authFileHeadersField(updated.headers)
    Object.entries(fields.headers).forEach(([name, value]) => {
      if (value) {
        headers[name] = value
      } else {
        delete headers[name]
      }
    })
    if (Object.keys(headers).length > 0) {
      updated.headers = headers
    } else {
      delete updated.headers
    }
  }
  return JSON.stringify(updated, null, 2)
}

const authFileEditorUpdatedText = computed(() => {
  const editor = authFileEditor.value
  if (!editor || editor.headersError) {
    return ''
  }
  try {
    return buildAuthFileUpdatedText(editor)
  } catch {
    return ''
  }
})

const authFileEditorDirty = computed(() => {
  const editor = authFileEditor.value
  if (!editor?.json) {
    return false
  }
  try {
    return Object.keys(buildAuthFileEditorFields(editor)).length > 0
  } catch {
    return true
  }
})

const authFileEditorProvider = computed(() => {
  const editor = authFileEditor.value
  const rawProvider = authFileStringField(editor?.json?.type || editor?.json?.provider)
  return rawProvider.trim().toLowerCase()
})

const authFileEditorSupportsWebsockets = computed(
  () => authFileEditorProvider.value === 'codex' || authFileEditorProvider.value === 'xai',
)

const authFileEditorSupportsUsingApi = computed(() => authFileEditorProvider.value === 'xai')

function handleAuthFileHeadersChange(value: string) {
  const editor = authFileEditor.value
  if (!editor) {
    return
  }
  editor.headersText = value
  editor.headersTouched = true
  try {
    parseAuthFileHeaders(value)
    editor.headersError = null
  } catch (error) {
    editor.headersError = error instanceof Error ? error.message : String(error)
  }
}

async function reloadAccounts() {
  isLoading.value = true
  if (canManageAccounts.value) {
    try {
      await syncCodexKeeperAccountList()
    } catch (error) {
      message.error(errorText(error, '同步 CPA 凭证列表失败', 'Failed to sync the CPA credential list'))
    }
  }
  try {
    const [accountsResponse, nextStatus] = await Promise.all([
      listCodexKeeperAccounts(),
      getCodexKeeperStatus(),
    ])
    accounts.value = accountsResponse.items
    accountListDegraded.value = accountsResponse.degraded === true
    priorityRules.value = accountsResponse.priority_rules
    keeperStatus.value = nextStatus
  } catch (error) {
    message.error(errorText(error, '加载凭证状态失败', 'Failed to load credential status'))
  } finally {
    isLoading.value = false
  }
}

async function openAuthFileEditor(account: CodexKeeperAccount) {
  if (!canManageAccounts.value) {
    return
  }
  const fileName = account.name
  authFileEditor.value = {
    fileName,
    fileInfoText: JSON.stringify(account, null, 2),
    loading: true,
    saving: false,
    error: null,
    rawText: '',
    invalidContentPreview: '',
    json: null,
    prefix: '',
    proxyUrl: '',
    weight: null,
    weightTouched: false,
    requestRetry: null,
    requestRetryTouched: false,
    websockets: false,
    websocketsTouched: false,
    usingApi: false,
    usingApiTouched: false,
    note: '',
    noteTouched: false,
    headersText: '',
    headersTouched: false,
    headersError: null,
  }
  try {
    const detail = await getCodexKeeperAuthFile(fileName)
    const current = authFileEditor.value
    if (!current || current.fileName !== fileName) {
      return
    }
    current.loading = false
    current.json = detail.json
    if (!detail.json) {
      current.rawText = detail.raw_text ?? ''
      const preview = current.rawText.trim()
      current.invalidContentPreview = preview.length > 1000 ? `${preview.slice(0, 1000)}\n...` : preview
      current.error = detail.invalid_reason === 'html_challenge'
        ? t(
            '下载到的是 HTML 验证页面，不是认证 JSON 对象。请重新认证或替换该认证文件后再编辑字段。',
            'Downloaded content is an HTML challenge page, not an auth JSON object. Re-authenticate or replace the auth file before editing fields.',
          )
        : t(
            '该认证文件不是 JSON 对象，无法编辑字段。',
            'This auth file is not a JSON object, so its fields cannot be edited.',
          )
      return
    }
    const headers = authFileHeadersField(detail.json.headers)
    current.prefix = authFileStringField(detail.json.prefix)
    current.proxyUrl = authFileStringField(detail.json.proxy_url)
    current.weight = authFileIntegerField(detail.json.weight)
    current.requestRetry = authFileIntegerField(detail.json.request_retry)
    current.websockets = authFileBooleanField(detail.json.websockets ?? detail.json.websocket)
    current.usingApi = authFileBooleanField(detail.json.using_api)
    current.note = authFileStringField(detail.json.note)
    current.headersText = Object.keys(headers).length > 0 ? JSON.stringify(headers, null, 2) : ''
  } catch (error) {
    const current = authFileEditor.value
    if (!current || current.fileName !== fileName) {
      return
    }
    current.loading = false
    current.error = errorText(error, '加载认证文件失败', 'Failed to load auth file')
  }
}

function closeAuthFileEditor() {
  if (!authFileEditor.value?.saving) {
    authFileEditor.value = null
  }
}

function handleAuthFileEditorOpen(open: boolean) {
  if (!open) {
    closeAuthFileEditor()
  }
}

function setAuthFileWebsockets(value: boolean) {
  const editor = authFileEditor.value
  if (!editor) {
    return
  }
  editor.websockets = value
  editor.websocketsTouched = true
}

function setAuthFileUsingApi(value: boolean) {
  const editor = authFileEditor.value
  if (!editor) return
  editor.usingApi = value
  editor.usingApiTouched = true
}

function setAuthFileWeight(value: string | number) {
  const editor = authFileEditor.value
  if (!editor) return
  editor.weight = authFileIntegerField(value)
  editor.weightTouched = true
}

function setAuthFileRequestRetry(value: string | number) {
  const editor = authFileEditor.value
  if (!editor) return
  editor.requestRetry = authFileIntegerField(value)
  editor.requestRetryTouched = true
}

async function saveAuthFileEditor() {
  const editor = authFileEditor.value
  if (!editor?.json || editor.loading || editor.saving || editor.headersError) {
    return
  }
  let fields: CodexKeeperAuthFileFields
  try {
    fields = buildAuthFileEditorFields(editor)
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
    return
  }
  if (Object.keys(fields).length === 0) {
    closeAuthFileEditor()
    return
  }
  editor.saving = true
  try {
    await updateCodexKeeperAuthFile(editor.fileName, fields)
    message.success(t(`已更新认证文件“${editor.fileName}”`, `Auth file “${editor.fileName}” updated`))
    await loadAccounts()
    if (selectedAccount.value?.name === editor.fileName) {
      selectedAccountNoteRequestID += 1
      isSelectedAccountNoteLoading.value = true
      await loadSelectedAccountNote(editor.fileName, selectedAccountNoteRequestID)
    }
    const editedAccount = accounts.value.find((account) => account.name === editor.fileName)
    authFileEditor.value = null
    if (isCodexCredential(editedAccount)) {
      await refreshAccounts([editor.fileName], {
        successMessage: t(
          `已开始巡检“${editor.fileName}”以同步凭证信息`,
          `Started inspecting “${editor.fileName}” to sync credential information`,
        ),
      })
    }
  } catch (error) {
    message.error(errorText(error, '更新认证文件失败', 'Failed to update auth file'))
  } finally {
    if (authFileEditor.value?.fileName === editor.fileName) {
      editor.saving = false
    }
  }
}

async function copyAuthFileEditorText() {
  const text = authFileEditorUpdatedText.value
  if (!text) {
    return
  }
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('认证文件内容已复制', 'Auth file content copied'))
  } catch (error) {
    message.error(errorText(error, '复制认证文件内容失败', 'Failed to copy auth file content'))
  }
}

function resetCodexOAuthDialog() {
  if (oauthState.value && oauthTarget.value && oauthDialogStatus.value === 'waiting') {
    void cancelCodexKeeperOAuth(oauthState.value).catch(() => undefined)
  }
  oauthState.value = ''
  oauthPollToken += 1
  oauthDialogStatus.value = 'idle'
  oauthAuthURL.value = ''
  oauthCallbackURL.value = ''
  oauthError.value = ''
  isStartingOAuth.value = false
  isSubmittingOAuthCallback.value = false
}

function handleOAuthProviderChange(value: unknown) {
  if (!oauthProviderOptions.value.some((option) => option.value === value)) {
    return
  }
  resetCodexOAuthDialog()
  oauthProvider.value = value as CredentialOAuthProvider
}

function openCodexOAuthDialog() {
  if (!canManageAccounts.value) {
    return
  }
  resetCodexOAuthDialog()
  oauthTarget.value = null
  oauthFlow.value = 'login'
  oauthProvider.value = 'codex'
  oauthProjectId.value = ''
  oauthDialogOpen.value = true
}

function closeCodexOAuthDialog() {
  resetCodexOAuthDialog()
  oauthProjectId.value = ''
  oauthDialogOpen.value = false
}

function handleCodexOAuthDialogOpen(open: boolean) {
  if (!open && !isStartingOAuth.value && !isSubmittingOAuthCallback.value) {
    closeCodexOAuthDialog()
  }
}

async function reloadAccountsAfterOAuth() {
  await loadAccounts()
}

async function pollCodexOAuthStatus(state: string, token: number) {
  for (;;) {
    await sleep(OAUTH_STATUS_POLL_INTERVAL_MS)
    if (token !== oauthPollToken || !oauthDialogOpen.value) {
      return
    }
    try {
      const response = await getCodexKeeperOAuthStatus(state)
      if (token !== oauthPollToken || !oauthDialogOpen.value) {
        return
      }
      const status = response.status.trim().toLowerCase()
      if (status === 'ok') {
        oauthDialogStatus.value = 'success'
        oauthError.value = ''
        oauthPollToken += 1
        message.success(t(`${oauthProviderLabel.value} OAuth 认证成功`, `${oauthProviderLabel.value} OAuth authentication successful`))
        void reloadAccountsAfterOAuth()
        return
      }
      if (status === 'error') {
        oauthDialogStatus.value = 'error'
        oauthError.value = response.error?.trim() || t(`${oauthProviderLabel.value} OAuth 认证失败`, `${oauthProviderLabel.value} OAuth authentication failed`)
        oauthPollToken += 1
        return
      }
      oauthError.value = ''
    } catch (error) {
      if (token !== oauthPollToken || !oauthDialogOpen.value) {
        return
      }
      oauthError.value = errorText(
        error,
        '暂时无法查询认证状态，将继续重试',
        'Unable to check authentication status; retrying',
      )
    }
  }
}

async function startCodexOAuth() {
  if (isStartingOAuth.value) {
    return
  }
  const token = ++oauthPollToken
  oauthDialogStatus.value = 'idle'
  oauthAuthURL.value = ''
  oauthCallbackURL.value = ''
  oauthError.value = ''
  isStartingOAuth.value = true
  try {
    const response = oauthFlow.value === 'siwc' && oauthTarget.value
      ? await bindCredentialSIWC(oauthTarget.value.name)
      : await startCodexKeeperOAuth(oauthProvider.value, oauthProjectId.value, oauthTarget.value ? { name: oauthTarget.value.name, auth_index: oauthTarget.value.auth_index } : undefined)
    if (token !== oauthPollToken || !oauthDialogOpen.value) {
      return
    }
    oauthAuthURL.value = response.url
    oauthState.value = response.state
    oauthDialogStatus.value = 'waiting'
    void pollCodexOAuthStatus(response.state, token)
  } catch (error) {
    if (token !== oauthPollToken || !oauthDialogOpen.value) {
      return
    }
    oauthDialogStatus.value = 'error'
    oauthError.value = errorText(
      error,
      `启动 ${oauthProviderLabel.value} OAuth 失败`,
      `Failed to start ${oauthProviderLabel.value} OAuth`,
    )
  } finally {
    if (token === oauthPollToken) {
      isStartingOAuth.value = false
    }
  }
}

function openCodexOAuthURL() {
  if (oauthAuthURL.value) {
    window.open(oauthAuthURL.value, '_blank', 'noopener,noreferrer')
  }
}

async function copyCodexOAuthURL() {
  try {
    await copyToClipboard(oauthAuthURL.value)
    message.success(t('授权链接已复制', 'Authorization link copied'))
  } catch (error) {
    message.error(errorText(error, '复制授权链接失败', 'Failed to copy authorization link'))
  }
}

async function submitCodexOAuthCallbackURL() {
  const redirectURL = oauthCallbackURL.value.trim()
  if (!redirectURL || oauthDialogStatus.value !== 'waiting' || isSubmittingOAuthCallback.value) {
    return
  }
  isSubmittingOAuthCallback.value = true
  try {
    await submitCodexKeeperOAuthCallback(oauthProvider.value, redirectURL)
    message.success(t('回调 URL 已提交，正在等待认证结果', 'Callback URL submitted; waiting for authentication'))
  } catch (error) {
    message.error(errorText(error, '提交回调 URL 失败', 'Failed to submit callback URL'))
  } finally {
    isSubmittingOAuthCallback.value = false
  }
}

function triggerAuthFileUpload() {
  if (canManageAccounts.value && !isUploadingAuthFiles.value) {
    authFileInput.value?.click()
  }
}

async function startAccountInspection(): Promise<boolean> {
  if (!canManageAccounts.value || isStartingAccountInspection.value) {
    return false
  }
  isStartingAccountInspection.value = true
  try {
    await runCodexKeeperOnce()
    message.success(t('已开始凭证巡检', 'Credential inspection started'))
    await loadKeeperStatus()
    void pollKeeperModeUntilIdle('once')
    return true
  } catch (error) {
    message.error(errorText(error, '启动凭证巡检失败', 'Failed to start credential inspection'))
    return false
  } finally {
    isStartingAccountInspection.value = false
  }
}

async function handleAuthFileUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length === 0 || isUploadingAuthFiles.value) {
    return
  }
  const validFiles: File[] = []
  const invalidNames: string[] = []
  files.forEach((file) => {
    if (!file.name.toLowerCase().endsWith('.json')) {
      invalidNames.push(`${file.name}: ${t('只能上传 JSON 文件', 'Only JSON files can be uploaded')}`)
      return
    }
    if (file.size > AUTH_FILE_MAX_SIZE) {
      invalidNames.push(`${file.name}: ${t('文件大小不能超过 10 MB', 'File size must not exceed 10 MB')}`)
      return
    }
    validFiles.push(file)
  })
  if (invalidNames.length > 0) {
    message.error(invalidNames.join('; '))
  }
  if (validFiles.length === 0) {
    return
  }
  isUploadingAuthFiles.value = true
  try {
    const result = await uploadCodexKeeperAuthFiles(validFiles)
    if (result.uploaded > 0) {
      message.success(t(`上传成功 ${result.uploaded} 个认证文件`, `Uploaded ${result.uploaded} auth file(s)`))
      await loadAccounts()
      const uploadedNames = new Set(result.files)
      const uploadedCodexNames = accounts.value
        .filter((account) => uploadedNames.has(account.name) && isCodexCredential(account))
        .map((account) => account.name)
      if (uploadedCodexNames.length > 0) {
        await refreshAccounts(uploadedCodexNames, {
          successMessage: t(
            `已开始巡检 ${uploadedCodexNames.length} 份新 Codex 凭证`,
            `Started inspecting ${uploadedCodexNames.length} new Codex credential(s)`,
          ),
        })
      }
    }
    if (result.failed.length > 0) {
      message.error(result.failed.map((item) => `${item.name}: ${item.error}`).join('; '))
    }
  } catch (error) {
    message.error(errorText(error, '上传认证文件失败', 'Failed to upload auth files'))
  } finally {
    isUploadingAuthFiles.value = false
  }
}

function openBulkDeleteDialog() {
  if (!canBulkDelete.value) {
    return
  }
  bulkDeleteDialog.show = true
}

async function submitBulkDelete() {
  const authNames = selectedAccountNames.value
  if (authNames.length === 0) {
    return
  }
  isBulkDeleting.value = true
  try {
    const result = await bulkDeleteCodexKeeperAccounts({ auth_names: authNames })
    const deletedNames = new Set(result.deleted)
    selectedAccountKeys.value = selectedAccountKeys.value.filter(
      (key) => !deletedNames.has(String(key)),
    )
    if (result.failed.length > 0 && result.deleted.length > 0) {
      message.warning(t(`批量删除完成：成功 ${result.deleted.length} 个，失败 ${result.failed.length} 个`, `Bulk delete complete: ${result.deleted.length} succeeded, ${result.failed.length} failed`))
    } else if (result.failed.length > 0) {
      message.error(t(`批量删除失败：失败 ${result.failed.length} 个`, `Bulk delete failed: ${result.failed.length} failed`))
    } else {
      message.success(t(`已删除 ${result.deleted.length} 份凭证`, `Deleted ${result.deleted.length} credentials`))
    }
    bulkDeleteDialog.show = false
    await loadAccounts()
  } catch (error) {
    message.error(errorText(error, '批量删除失败', 'Bulk delete failed'))
  } finally {
    isBulkDeleting.value = false
  }
}

async function toggleSelectedAccounts(action: 'enable' | 'disable') {
  const targets = action === 'enable' ? selectedDisabledAccounts.value : selectedEnabledAccounts.value
  if (targets.length === 0 || isBulkOperationRunning.value) {
    return
  }
  bulkToggleAction.value = action
  try {
    const results = await Promise.allSettled(
      targets.map((account) =>
        action === 'enable'
          ? enableCodexKeeperAccount(account.name)
          : disableCodexKeeperAccount(account.name),
      ),
    )
    const succeededNames = new Set(
      targets
        .filter((_, index) => results[index]?.status === 'fulfilled')
        .map((account) => account.name),
    )
    const failedCount = results.length - succeededNames.size
    selectedAccountKeys.value = selectedAccountKeys.value.filter(
      (key) => !succeededNames.has(String(key)),
    )
    if (succeededNames.size > 0) {
      await loadAccounts()
    }
    const zhAction = action === 'enable' ? '启用' : '禁用'
    const enAction = action === 'enable' ? 'enable' : 'disable'
    if (failedCount > 0 && succeededNames.size > 0) {
      message.warning(t(
        `批量${zhAction}完成：成功 ${succeededNames.size} 个，失败 ${failedCount} 个`,
        `Bulk ${enAction} complete: ${succeededNames.size} succeeded, ${failedCount} failed`,
      ))
    } else if (failedCount > 0) {
      message.error(t(
        `批量${zhAction}失败：失败 ${failedCount} 个`,
        `Bulk ${enAction} failed: ${failedCount} failed`,
      ))
    } else {
      message.success(t(
        `已${zhAction} ${succeededNames.size} 份凭证`,
        `${succeededNames.size} credentials ${action === 'enable' ? 'enabled' : 'disabled'}`,
      ))
    }
  } finally {
    bulkToggleAction.value = null
  }
}

function confirmToggleSelectedAccounts(action: 'enable' | 'disable') {
  const count = action === 'enable'
    ? selectedDisabledAccounts.value.length
    : selectedEnabledAccounts.value.length
  if (count === 0) {
    return
  }
  if (action === 'enable') {
    openAccountConfirm(
      t('批量启用凭证', 'Bulk Enable Credentials'),
      t(`确认启用已选的 ${count} 份已禁用凭证？`, `Enable the ${count} selected disabled credentials?`),
      t('确认启用', 'Confirm Enable'),
      'primary',
      () => toggleSelectedAccounts('enable'),
    )
    return
  }
  openAccountConfirm(
    t('批量禁用凭证', 'Bulk Disable Credentials'),
    t(`确认禁用已选的 ${count} 份正常凭证？`, `Disable the ${count} selected enabled credentials?`),
    t('确认禁用', 'Confirm Disable'),
    'warning',
    () => toggleSelectedAccounts('disable'),
  )
}

async function loadSelectedAccountNote(accountName: string, requestID: number) {
  try {
    const detail = await getCodexKeeperAuthFile(accountName)
    if (requestID !== selectedAccountNoteRequestID || selectedAccount.value?.name !== accountName) {
      return
    }
    const note = authFileStringField(detail.json?.note).trim()
    selectedAccountNote.value = note || null
  } catch {
    if (requestID === selectedAccountNoteRequestID && selectedAccount.value?.name === accountName) {
      selectedAccountNote.value = null
    }
  } finally {
    if (requestID === selectedAccountNoteRequestID && selectedAccount.value?.name === accountName) {
      isSelectedAccountNoteLoading.value = false
    }
  }
}

function openDetail(account: CodexKeeperAccount) {
  selectedAccount.value = account
  selectedAccountModels.value = null
  selectedAccountModelsError.value = null
  selectedAccountModelsLoading.value = false
  selectedAccountNote.value = null
  isSelectedAccountNoteLoading.value = false
  selectedAccountNoteRequestID += 1
  if (canManageAccounts.value) {
    isSelectedAccountNoteLoading.value = true
    void loadSelectedAccountNote(account.name, selectedAccountNoteRequestID)
  }
  detailOpen.value = true
}

async function loadSelectedAccountModels() {
  const account = selectedAccount.value
  if (!account || selectedAccountModelsLoading.value) {
    return
  }
  selectedAccountModelsLoading.value = true
  selectedAccountModelsError.value = null
  try {
    const response = await getCodexKeeperAuthFileModels(account.name)
    if (selectedAccount.value?.name !== account.name) {
      return
    }
    selectedAccountModels.value = response.models
  } catch (error) {
    if (selectedAccount.value?.name === account.name) {
      selectedAccountModelsError.value = errorText(
        error,
        '读取凭证支持模型失败',
        'Failed to load supported models',
      )
    }
  } finally {
    if (selectedAccount.value?.name === account.name) {
      selectedAccountModelsLoading.value = false
    }
  }
}

function setAccountResetCredits(accountName: string, resetCredits: CodexKeeperResetCredits) {
  accounts.value = accounts.value.map((account) =>
    account.name === accountName ? { ...account, reset_credits: resetCredits } : account,
  )
  if (selectedAccount.value?.name === accountName) {
    selectedAccount.value = { ...selectedAccount.value, reset_credits: resetCredits }
  }
}

function setResetCreditFeedback(accountName: string, feedback: ResetCreditFeedback | null) {
  const next = { ...resetCreditFeedback.value }
  if (feedback) {
    next[accountName] = feedback
  } else {
    delete next[accountName]
  }
  resetCreditFeedback.value = next
}

function getResetCreditFeedback(accountName: string): ResetCreditFeedback | null {
  return resetCreditFeedback.value[accountName] ?? null
}

async function runResetCreditAction(
  account: CodexKeeperAccount,
  action: 'reset-query' | 'reset-consume',
) {
  const key = accountActionKey(account, action)
  if (actingActions.value.has(key)) {
    return
  }
  actingActions.value = new Set(actingActions.value).add(key)
  setResetCreditFeedback(account.name, null)
  try {
    const result = action === 'reset-query'
      ? await queryCodexKeeperResetCredits(account.name)
      : await consumeCodexKeeperResetCredit(account.name)
    setAccountResetCredits(account.name, result)
    if (action === 'reset-query') {
      const successText = t('重置次数已更新', 'Reset credits updated')
      setResetCreditFeedback(account.name, { variant: 'success', text: successText })
      message.success(successText)
    } else {
      const successText = t('已使用一次重置次数，并开始巡检该凭证', 'One reset credit was used and this credential inspection has started')
      setResetCreditFeedback(account.name, { variant: 'success', text: successText })
      message.success(successText)
      void pollKeeperModeUntilIdle('accounts')
    }
  } catch (error) {
    const failureText = errorText(
      error,
      action === 'reset-query' ? '查询重置次数失败' : '使用重置次数失败',
      action === 'reset-query' ? 'Failed to query reset credits' : 'Failed to use reset credit',
    )
    setResetCreditFeedback(account.name, { variant: 'error', text: failureText })
    message.error(failureText)
  } finally {
    const nextActions = new Set(actingActions.value)
    nextActions.delete(key)
    actingActions.value = nextActions
  }
}

function confirmConsumeResetCredit(account: CodexKeeperAccount) {
  if ((account.reset_credits?.available_count ?? 0) <= 0) {
    return
  }
  openAccountConfirm(
    t('使用重置次数', 'Use Reset Credit'),
    t(
      `确认使用 ${account.email ?? account.name} 的一次重置次数？这会立即重置当前额度窗口，且无法撤销。`,
      `Use one reset credit for ${account.email ?? account.name}? This immediately resets the current quota window and cannot be undone.`,
    ),
    t('确认使用', 'Confirm Use'),
    'error',
    () => runResetCreditAction(account, 'reset-consume'),
  )
}

function openPriorityDialog(account: CodexKeeperAccount) {
  priorityDialog.account = account
  const priority = accountPriority(account)
  if (!isCodexCredential(account)) {
    setPriorityDialogMode('custom')
    priorityDialog.show = true
    return
  }
  const mode =
    priority < -1
      ? 'low'
      : priority > 20
        ? 'high'
        : 'default'
  setPriorityDialogMode(defaultPriority(account) === null && mode === 'default' ? 'low' : mode)
  priorityDialog.show = true
}

function setPriorityDialogMode(mode: PriorityMode) {
  priorityDialog.mode = mode
  const account = priorityDialog.account
  if (!account) {
    priorityDialog.value = null
    return
  }
  if (mode === 'custom') {
    priorityDialog.value = account.priority ?? 0
    return
  }
  if (mode === 'low') {
    const priority = accountPriority(account)
    priorityDialog.value = priority < -1 ? priority : -2
    return
  }
  if (mode === 'high') {
    const priority = accountPriority(account)
    priorityDialog.value = priority > 20 ? priority : 21
    return
  }
  priorityDialog.value = defaultPriority(account)
}

function handlePriorityDialogMode(value: unknown) {
  if (value === 'low' || value === 'high' || value === 'default' || value === 'custom') {
    setPriorityDialogMode(value)
  }
}

function setPriorityDialogValue(value: string | number) {
  const parsed = Number(value)
  priorityDialog.value = Number.isFinite(parsed) ? parsed : null
}

function accountConfirmButtonVariant(): 'default' | 'secondary' | 'destructive' {
  if (accountConfirmDialog.type === 'error') {
    return 'destructive'
  }
  if (accountConfirmDialog.type === 'warning') {
    return 'secondary'
  }
  return 'default'
}

async function submitPriorityDialog() {
  if (!priorityDialog.account || !canSubmitPriority.value) {
    return
  }
  const value =
    priorityDialog.mode === 'default'
      ? defaultPriority(priorityDialog.account)
      : priorityDialog.value
  if (value === null) {
    return
  }
  await runAccountAction(
    priorityDialog.account,
    'priority',
    () => updateCodexKeeperPriority(priorityDialog.account!.name, value),
    t('优先级已更新', 'Priority updated'),
  )
  priorityDialog.show = false
}

function openAccountConfirm(
  title: string,
  content: string,
  positiveText: string,
  type: AccountConfirmType,
  action: () => Promise<void>,
) {
  accountConfirmDialog.title = title
  accountConfirmDialog.content = content
  accountConfirmDialog.positiveText = positiveText
  accountConfirmDialog.type = type
  accountConfirmDialog.action = action
  accountConfirmDialog.show = true
}

async function submitAccountConfirm() {
  if (!accountConfirmDialog.action || isAccountConfirmSubmitting.value) {
    return
  }
  isAccountConfirmSubmitting.value = true
  try {
    await accountConfirmDialog.action()
    accountConfirmDialog.show = false
  } finally {
    isAccountConfirmSubmitting.value = false
  }
}

function confirmEnableAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    t('启用凭证', 'Enable Credential'),
    t(`启用 ${account.name}？`, `Enable ${account.name}?`),
    t('确认启用', 'Confirm Enable'),
    'primary',
    () => enableAccount(account),
  )
}

function confirmDisableAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    t('禁用凭证', 'Disable Credential'),
    t(`禁用 ${account.name}？`, `Disable ${account.name}?`),
    t('确认禁用', 'Confirm Disable'),
    'warning',
    () => disableAccount(account),
  )
}

function confirmDeleteAccount(account: CodexKeeperAccount) {
  openAccountConfirm(
    t('删除凭证', 'Delete Credential'),
    t(`删除 ${account.name}？此操作会从 CPA 删除认证文件。`, `Delete ${account.name}? This will remove the auth file from CPA.`),
    t('确认删除', 'Confirm Delete'),
    'error',
    () => deleteAccount(account),
  )
}

function enableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => enableCodexKeeperAccount(account.name),
    t('凭证已启用', 'Credential enabled'),
  )
}

function disableAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'toggle',
    () => disableCodexKeeperAccount(account.name),
    t('凭证已禁用', 'Credential disabled'),
  )
}

function deleteAccount(account: CodexKeeperAccount) {
  return runAccountAction(
    account,
    'delete',
    () => deleteCodexKeeperAccount(account.name),
    t('凭证已删除', 'Credential deleted'),
  )
}

function refreshAccount(account: CodexKeeperAccount, options: { closeDetail?: boolean } = {}) {
  return refreshAccounts([account.name], options)
}

async function refreshSelectedAccounts() {
  await refreshAccounts(
    selectedAccounts.value.filter(isCodexCredential).map((account) => account.name),
    { clearSelection: true },
  )
}

function uniqueAccountNames(raw: string[]): string[] {
  return [...new Set(raw.map((name) => name.trim()).filter(Boolean))]
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

async function pollKeeperModeUntilIdle(mode: KeeperPollMode) {
  const token = ++keeperPollTokens[mode]
  for (;;) {
    await sleep(REFRESH_STATUS_POLL_INTERVAL_MS)
    if (token !== keeperPollTokens[mode]) {
      return
    }
    try {
      const status = await getCodexKeeperStatus()
      keeperStatus.value = status
      const runningModes = status.running_modes ?? []
      const modeRunning = runningModes.length > 0 ? runningModes.includes(mode) : status.running
      if (modeRunning) {
        continue
      }
      await loadAccounts()
      return
    } catch {
      continue
    }
  }
}

async function refreshAccounts(
  rawNames: string[],
  options: { closeDetail?: boolean; clearSelection?: boolean; successMessage?: string } = {},
) {
  const authNames = uniqueAccountNames(rawNames)
  if (authNames.length === 0) {
    return
  }
  const refreshKeys = authNames
    .map((name) => accounts.value.find((account) => account.name === name))
    .filter((account): account is CodexKeeperAccount => account !== undefined)
    .map((account) => accountActionKey(account, 'refresh'))
  if (refreshKeys.some((key) => actingActions.value.has(key)) || isBulkOperationRunning.value) {
    return
  }
  const nextActions = new Set(actingActions.value)
  refreshKeys.forEach((key) => nextActions.add(key))
  actingActions.value = nextActions
  if (authNames.length > 1 || options.clearSelection) {
    isBulkRefreshing.value = true
  }
  try {
    await refreshCodexKeeperAccounts({ auth_names: authNames })
    message.success(options.successMessage ?? (authNames.length === 1
      ? t('已开始刷新凭证', 'Started refreshing credential')
      : t(`已开始刷新 ${authNames.length} 份凭证`, `Started refreshing ${authNames.length} credentials`)))
    if (options.closeDetail) {
      detailOpen.value = false
    }
    if (options.clearSelection) {
      selectedAccountKeys.value = []
    }
    void pollKeeperModeUntilIdle('accounts')
  } catch (error) {
    message.error(errorText(error, '刷新凭证失败', 'Failed to refresh credentials'))
  } finally {
    const restActions = new Set(actingActions.value)
    refreshKeys.forEach((key) => restActions.delete(key))
    actingActions.value = restActions
    isBulkRefreshing.value = false
  }
}

function accountActionKey(account: CodexKeeperAccount, action: AccountAction): string {
  return `${action}\u0000${account.name}`
}

function coreRecoveryStageLabel(stage: string): string {
  const labels: Record<string, string> = {
    sign_in_required: t('需要重新登录', 'Sign-in required'),
    quota_blocked: t('配额受限', 'Quota blocked'),
    loaded: t('已加载，待验证', 'Loaded, not verified'),
    inference_verified: t('推理已验证', 'Inference verified'),
    renewal_verified: t('续期已验证', 'Renewal verified'),
    expired_sign_in_required: t('已过期，需重新登录', 'Expired, sign-in required'),
    retry_scheduled: t('等待重试续期', 'Renewal retry scheduled'),
    awaiting_first_renewal: t('等待首次续期', 'Awaiting first renewal'),
  }
  return labels[stage] ?? stage
}

function openTargetOAuth(account: CodexKeeperAccount, siwc = false) {
  openCodexOAuthDialog()
  oauthTarget.value = account
  oauthFlow.value = siwc ? 'siwc' : 'login'
  const provider = account.provider === 'claude' ? 'anthropic' : account.provider === 'gemini' ? 'gemini-cli' : account.provider
  oauthProvider.value = provider as CredentialOAuthProvider
}

function runCoreCredentialAction(account: CodexKeeperAccount, action: 'core-refresh' | 'core-verify') {
  const run = () => runAccountAction(account, 'refresh', () => coreCredentialAction(account.name, action), t('Core 凭证操作完成', 'Core credential operation completed'))
  if (action === 'core-verify') {
    openAccountConfirm(t('推理验证', 'Verify inference'), t('将使用此凭证发起一次最小真实请求，可能产生少量用量。', 'This sends a minimal real request using this credential and may incur a small usage charge.'), t('验证', 'Verify'), 'warning', run)
  } else void run()
}

function isActionLoading(account: CodexKeeperAccount, action: AccountAction): boolean {
  return actingActions.value.has(accountActionKey(account, action))
}

function isRowActing(account: CodexKeeperAccount): boolean {
  return (['toggle', 'priority', 'delete', 'refresh', 'reset-query', 'reset-consume'] as const).some((action) =>
    isActionLoading(account, action),
  )
}

async function runAccountAction(
  account: CodexKeeperAccount,
  actionType: AccountAction,
  action: () => Promise<void>,
  successText: string,
) {
  const key = accountActionKey(account, actionType)
  if (actingActions.value.has(key)) {
    return
  }
  actingActions.value = new Set(actingActions.value).add(key)
  try {
    await action()
    message.success(successText)
    await loadAccounts()
    if (selectedAccount.value?.name === account.name) {
      const freshAccount = accounts.value.find((item) => item.name === account.name) ?? null
      selectedAccount.value = freshAccount
      detailOpen.value = freshAccount !== null
    }
  } catch (error) {
    message.error(errorText(error, '凭证操作失败', 'Credential operation failed'))
  } finally {
    const nextActions = new Set(actingActions.value)
    nextActions.delete(key)
    actingActions.value = nextActions
  }
}

restoreAccountStatusPreferences()

watch(
  [accountDisplaySize, () => accountSort.key, () => accountSort.direction],
  saveAccountStatusPreferences,
)
watch(
  [
    accountDisplaySize,
    () => accountSort.key,
    () => accountSort.direction,
    () => filters.keyword,
    () => filters.provider,
    () => filters.priority,
    () => filters.status,
  ],
  resetAccountPages,
)
watch(accountListPageCount, clampAccountPages)
watch(visibleListAccounts, pruneSelectedAccountKeys)

onMounted(() => {
  void loadAccounts()
  keeperStatusTimer = window.setInterval(() => {
    relativeTimeNow.value = Date.now()
    void loadKeeperStatus()
  }, KEEPER_STATUS_POLL_INTERVAL_MS)
})

onBeforeUnmount(() => {
  keeperPollTokens.once += 1
  keeperPollTokens.accounts += 1
  oauthPollToken += 1
  if (keeperStatusTimer !== undefined) {
    window.clearInterval(keeperStatusTimer)
  }
})
</script>

<template>
  <section class="page account-status-page">
    <div class="page-toolbar account-page-header">
      <h1 data-page-title class="page-title">{{ accountPageTitle }}</h1>
      <div class="header-actions">
        <Button v-if="canManageAccounts" variant="outline" @click="openCodexOAuthDialog">
          <LogIn data-icon="inline-start" />
          {{ t('OAuth 登录', 'OAuth Login') }}
        </Button>
        <Button
          v-if="canManageAccounts"
          :disabled="isUploadingAuthFiles"
          @click="triggerAuthFileUpload"
        >
          <Spinner v-if="isUploadingAuthFiles" data-icon="inline-start" />
          <Upload v-else data-icon="inline-start" />
          {{ t('上传文件', 'Upload Files') }}
        </Button>
        <Button
          v-if="canManageAccounts"
          variant="outline"
          :disabled="isAccountInspectionBlocked || isStartingAccountInspection"
          @click="startAccountInspection"
        >
          <Spinner v-if="isStartingAccountInspection || isAccountInspectionRunning" data-icon="inline-start" />
          <ShieldCheck v-else data-icon="inline-start" />
          {{ t('Codex 巡检', 'Inspect Codex') }}
        </Button>
        <Button variant="outline" :disabled="isLoading" @click="reloadAccounts">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCw v-else data-icon="inline-start" />
          {{ t('重新加载', 'Reload') }}
        </Button>
        <input
          ref="authFileInput"
          class="auth-file-input"
          type="file"
          accept=".json,application/json"
          multiple
          @change="handleAuthFileUpload"
        >
      </div>
    </div>

    <Alert v-if="accountListDegraded" variant="destructive">
      <ShieldAlert />
      <AlertDescription>
        {{ t('无法读取 CPA 实时凭证列表，当前仅展示本地 Codex 巡检缓存。', 'The live CPA credential list is unavailable. Only the local Codex inspection cache is shown.') }}
      </AlertDescription>
    </Alert>

    <div class="account-metrics">
      <Card size="sm" class="account-metric-card inspection-status-card">
        <CardHeader class="account-metric-header">
          <div class="min-w-0">
            <CardDescription>{{ t('Codex 巡检', 'Codex Inspection') }}</CardDescription>
            <CardTitle class="inspection-status-value" :title="keeperStatusDetailText">
              <Badge class="inspection-status-tag" :variant="keeperStateType">
                {{ keeperStatusDetailText }}
              </Badge>
            </CardTitle>
          </div>
          <div class="account-metric-icon"><Activity /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">{{ keeperStatusFootnoteText }}</CardContent>
      </Card>
      <Card size="sm" class="account-metric-card">
        <CardHeader class="account-metric-header">
          <div class="min-w-0">
            <CardDescription>{{ t('凭证总数', 'Total Credentials') }}</CardDescription>
            <CardTitle class="text-2xl tabular-nums">{{ formatInteger(accounts.length) }}</CardTitle>
          </div>
          <div class="account-metric-icon"><Users /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">{{ t('全部认证文件', 'All auth files') }}</CardContent>
      </Card>
      <Card
        size="sm"
        class="account-metric-card metric-action is-green"
        :class="{ 'is-active': isStatusFilterActive('enabled') }"
        role="button"
        tabindex="0"
        :aria-pressed="isStatusFilterActive('enabled')"
        @click="toggleStatusFilter('enabled')"
        @keydown.enter="toggleStatusFilter('enabled')"
        @keydown.space.prevent="toggleStatusFilter('enabled')"
      >
        <CardHeader class="account-metric-header">
          <div><CardDescription>{{ t('启用中', 'Enabled') }}</CardDescription><CardTitle class="text-2xl tabular-nums">{{ formatInteger(enabledAccountCount) }}</CardTitle></div>
          <div class="account-metric-icon"><ShieldCheck /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">{{ t('可参与调度', 'Available for scheduling') }}</CardContent>
      </Card>
      <Card
        size="sm"
        class="account-metric-card metric-action is-warning"
        :class="{ 'is-active': isStatusFilterActive('disabled') }"
        role="button"
        tabindex="0"
        :aria-pressed="isStatusFilterActive('disabled')"
        @click="toggleStatusFilter('disabled')"
        @keydown.enter="toggleStatusFilter('disabled')"
        @keydown.space.prevent="toggleStatusFilter('disabled')"
      >
        <CardHeader class="account-metric-header">
          <div><CardDescription>{{ t('已禁用', 'Disabled') }}</CardDescription><CardTitle class="text-2xl tabular-nums">{{ formatInteger(disabledAccountCount) }}</CardTitle></div>
          <div class="account-metric-icon"><PauseCircle /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">{{ t('停用凭证', 'Inactive credentials') }}</CardContent>
      </Card>
      <Card
        size="sm"
        class="account-metric-card metric-action is-danger"
        :class="{ 'is-active': isStatusFilterActive('unauthorized') }"
        role="button"
        tabindex="0"
        :aria-pressed="isStatusFilterActive('unauthorized')"
        @click="toggleStatusFilter('unauthorized')"
        @keydown.enter="toggleStatusFilter('unauthorized')"
        @keydown.space.prevent="toggleStatusFilter('unauthorized')"
      >
        <CardHeader class="account-metric-header">
          <div><CardDescription>{{ t('401报错', '401 Errors') }}</CardDescription><CardTitle class="text-2xl tabular-nums">{{ formatInteger(unauthorizedErrorAccountCount) }}</CardTitle></div>
          <div class="account-metric-icon"><ShieldAlert /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">HTTP 401</CardContent>
      </Card>
      <Card
        size="sm"
        class="account-metric-card metric-action is-purple"
        :class="{ 'is-active': isStatusFilterActive('quotaExhausted') }"
        role="button"
        tabindex="0"
        :aria-pressed="isStatusFilterActive('quotaExhausted')"
        @click="toggleStatusFilter('quotaExhausted')"
        @keydown.enter="toggleStatusFilter('quotaExhausted')"
        @keydown.space.prevent="toggleStatusFilter('quotaExhausted')"
      >
        <CardHeader class="account-metric-header">
          <div><CardDescription>{{ t('额度耗尽', 'Quota Exhausted') }}</CardDescription><CardTitle class="text-2xl tabular-nums">{{ formatInteger(quotaExhaustedAccountCount) }}</CardTitle></div>
          <div class="account-metric-icon"><Gauge /></div>
        </CardHeader>
        <CardContent class="text-xs text-muted-foreground">{{ t('临时降级', 'Temporary downgrade') }}</CardContent>
      </Card>
    </div>

    <Card class="account-list-panel">
      <CardHeader class="status-toolbar">
        <div class="toolbar-heading">
          <div>
            <CardTitle>{{ t('凭证列表', 'Credential List') }}</CardTitle>
            <CardDescription class="toolbar-subtitle">
              {{ t(`正常 ${filteredNormalAccounts.length} / ${enabledAccountCount} 份凭证`, `Normal ${filteredNormalAccounts.length} / ${enabledAccountCount} credentials`) }}
              <template v-if="hasDisabledAccounts">
                {{ t(`，已禁用 ${filteredDisabledAccounts.length} / ${disabledAccountCount} 份凭证`, `, disabled ${filteredDisabledAccounts.length} / ${disabledAccountCount} credentials`) }}
              </template>
            </CardDescription>
          </div>
          <Badge v-if="activeFilterCount > 0" variant="secondary">
            {{ t(`已筛选 ${activeFilterCount} 项`, `${activeFilterCount} filters active`) }}
          </Badge>
        </div>
        <div class="filter-grid">
          <InputGroup>
            <InputGroupAddon><Search /></InputGroupAddon>
            <InputGroupInput v-model="filters.keyword" :placeholder="t('搜索凭证或邮箱', 'Search credential or email')" />
          </InputGroup>
          <FilterCombobox
            :model-value="filters.provider"
            :options="providerOptions"
            :placeholder="t('提供商', 'Provider')"
            :search-placeholder="t('搜索提供商', 'Search providers')"
            :empty-text="t('没有匹配提供商', 'No matching providers')"
            :icon="Users"
            @update:model-value="setProviderFilter"
          />
          <FilterCombobox
            :model-value="filters.priority === 'all' ? null : filters.priority"
            :options="priorityFilterOptions"
            :placeholder="t('优先级', 'Priority')"
            :search-placeholder="t('搜索优先级', 'Search priorities')"
            :empty-text="t('没有匹配优先级', 'No matching priorities')"
            :icon="Gauge"
            @update:model-value="setPriorityFilter"
          />
        </div>
      </CardHeader>

      <CardContent class="account-list-content">
        <section class="account-section">
          <div class="account-section-actions-row">
            <div class="sort-control-row" :aria-label="t('凭证排序', 'Credential Sorting')">
              <span class="sort-control-label">{{ t('排序', 'Sort') }}</span>
              <DropdownMenu>
                <DropdownMenuTrigger as-child>
                  <Button size="sm" :variant="accountSort.key === 'quotaDay' || accountSort.key === 'quotaWeek' ? 'secondary' : 'outline'">
                    {{ activeQuotaSortLabel ? t(`额度窗口：${activeQuotaSortLabel} ${sortDirectionMark}`, `Quota Window: ${activeQuotaSortLabel} ${sortDirectionMark}`) : t('额度窗口', 'Quota Window') }}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  <DropdownMenuItem v-for="option in quotaSortOptions" :key="option.key" @select="handleQuotaSortSelect(option.key)">
                    {{ option.label }} {{ accountSortMark(option.key as AccountSortKey) }}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button
                size="sm"
                :variant="isAccountSortActive('provider') ? 'secondary' : 'outline'"
                @click="toggleAccountSort('provider')"
              >
                {{ t('提供商', 'Provider') }} {{ accountSortMark('provider') }}
              </Button>
              <Button
                size="sm"
                :variant="isAccountSortActive('status') ? 'secondary' : 'outline'"
                @click="toggleAccountSort('status')"
              >
                {{ t('状态', 'Status') }} {{ accountSortMark('status') }}
              </Button>
              <Button
                size="sm"
                :variant="isAccountSortActive('priority') ? 'secondary' : 'outline'"
                @click="toggleAccountSort('priority')"
              >
                {{ t('优先级', 'Priority') }} {{ accountSortMark('priority') }}
              </Button>
              <Button
                size="sm"
                :variant="isAccountSortActive('lastCheckedAt') ? 'secondary' : 'outline'"
                @click="toggleAccountSort('lastCheckedAt')"
              >
                {{ t('最近活动', 'Last Activity') }} {{ accountSortMark('lastCheckedAt') }}
              </Button>
            </div>
            <div v-if="canManageAccounts" class="account-section-actions">
              <Button
                variant="outline"
                size="sm"
                :disabled="!canBulkEnable"
                @click="confirmToggleSelectedAccounts('enable')"
              >
                <Spinner v-if="bulkToggleAction === 'enable'" data-icon="inline-start" />
                <ShieldCheck v-else data-icon="inline-start" />
                {{ t('启用', 'Enable') }}
              </Button>
              <Button
                variant="outline"
                size="sm"
                :disabled="!canBulkDisable"
                @click="confirmToggleSelectedAccounts('disable')"
              >
                <Spinner v-if="bulkToggleAction === 'disable'" data-icon="inline-start" />
                <PauseCircle v-else data-icon="inline-start" />
                {{ t('禁用', 'Disable') }}
              </Button>
              <Button
                variant="outline"
                size="sm"
                :disabled="!canRefreshSelected"
                @click="refreshSelectedAccounts"
              >
                <Spinner v-if="isBulkRefreshing" data-icon="inline-start" />
                <RefreshCw v-else data-icon="inline-start" />
                {{ t('刷新', 'Refresh') }}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                :disabled="!canBulkDelete"
                @click="openBulkDeleteDialog"
              >
                <Spinner v-if="isBulkDeleting" data-icon="inline-start" />
                <Trash2 v-else data-icon="inline-start" />
                {{ t('删除', 'Delete') }}
              </Button>
            </div>
          </div>

          <div class="account-table-shell">
            <Table class="account-table table-fixed" :style="{ minWidth: `${accountTableScrollX}px` }">
              <TableHeader>
                <TableRow>
                  <TableHead v-if="canManageAccounts" class="w-11">
                    <Checkbox
                      :model-value="visibleSelectionState"
                      :disabled="selectableVisibleAccounts.length === 0"
                      :aria-label="t('选择当前页凭证', 'Select credentials on this page')"
                      @update:model-value="setAllVisibleAccounts"
                    />
                  </TableHead>
                  <TableHead class="w-[220px]">{{ t('凭证', 'Credential') }}</TableHead>
                  <TableHead class="w-[112px]">{{ t('提供商', 'Provider') }}</TableHead>
                  <TableHead class="w-[88px]">{{ t('优先级', 'Priority') }}</TableHead>
                  <TableHead class="w-[168px]">{{ t('额度窗口', 'Quota Window') }}</TableHead>
                  <TableHead class="w-[152px]">{{ t('重置时间', 'Reset Time') }}</TableHead>
                  <TableHead class="w-[266px]">{{ t('窗口用量', 'Window Usage') }}</TableHead>
                  <TableHead class="w-[116px]">{{ t('窗口预测', 'Window Projection') }}</TableHead>
                  <TableHead class="w-[100px]">{{ t('最近活动', 'Last Activity') }}</TableHead>
                  <TableHead class="w-[152px]">{{ t('最近操作', 'Latest Action') }}</TableHead>
                  <TableHead class="w-[52px]"><span class="sr-only">{{ t('操作', 'Actions') }}</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <template v-if="showListLoadingState">
                  <TableRow v-for="rowIndex in 6" :key="`account-skeleton-${rowIndex}`">
                    <TableCell v-for="columnIndex in (canManageAccounts ? 11 : 10)" :key="columnIndex">
                      <Skeleton class="h-4 w-full" />
                    </TableCell>
                  </TableRow>
                </template>
                <TableEmpty v-else-if="visibleListAccounts.length === 0" :colspan="canManageAccounts ? 11 : 10">
                  {{ t('当前筛选下暂无凭证', 'No credentials match the current filter') }}
                </TableEmpty>
                <TableRow v-for="row in visibleListAccounts" v-else :key="row.name">
                  <TableCell v-if="canManageAccounts">
                    <Checkbox
                      :model-value="isAccountSelected(row)"
                      :disabled="!isCredentialMutable(row) || isRowActing(row) || isBulkOperationRunning"
                      :aria-label="t(`选择 ${row.email ?? row.name}`, `Select ${row.email ?? row.name}`)"
                      @update:model-value="setAccountSelected(row, $event)"
                    />
                  </TableCell>
                  <TableCell>
                    <div class="account-table-identity" :title="accountIdentityTitle(row)">
                      <span class="account-table-email">{{ credentialDisplayName(row) }}</span>
                      <span class="account-table-name">{{ row.name }}</span>
                      <span class="account-table-meta">
                        <span v-for="tag in accountStatusTags(row)" :key="tag.label" class="account-table-chip" :class="tag.tone">
                          {{ tag.label }}
                        </span>
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="credential-provider-cell">
                      <span class="account-table-chip is-type" :title="credentialProviderLabel(row.provider)">{{ credentialProviderLabel(row.provider) }}</span>
                      <small v-if="row.account_type" class="truncate text-xs text-muted-foreground" :title="accountTypeLabel(row.account_type)">{{ accountTypeLabel(row.account_type) }}</small>
                    </div>
                  </TableCell>
                  <TableCell><span class="account-table-chip is-priority" :title="t(`优先级 ${formatInteger(accountPriority(row))}`, `Priority ${formatInteger(accountPriority(row))}`)">{{ formatInteger(accountPriority(row)) }}</span></TableCell>
                  <TableCell>
                    <div v-if="quotaWindowItems(row).length" class="quota-window-cell">
                      <div v-for="item in quotaWindowItems(row)" :key="item.label" class="quota-window-item" :title="quotaWindowTooltip(item)">
                        <div class="quota-window-head">
                          <span class="quota-window-label">{{ item.label }}</span>
                          <span class="quota-window-percent">{{ item.remainingPercent }}%</span>
                        </div>
                        <div class="quota-window-track">
                          <div class="quota-window-fill" :class="quotaBarTone(item.remainingPercent)" :style="{ width: `${item.remainingPercent}%` }" />
                        </div>
                      </div>
                    </div>
                    <span v-else>-</span>
                  </TableCell>
                  <TableCell>
                    <div v-if="quotaWindowItems(row).length" class="quota-reset-cell">
                      <span v-for="item in quotaWindowItems(row)" :key="item.label" class="quota-reset-item" :title="quotaWindowTooltip(item)">
                        {{ formatQuotaResetTime(item.resetAt) ?? '-' }}
                      </span>
                    </div>
                    <span v-else>-</span>
                  </TableCell>
                  <TableCell>
                    <div v-if="quotaWindowItems(row).length" class="quota-usage-cell">
                      <div v-for="item in quotaWindowItems(row)" :key="item.label" class="quota-usage-item" :class="quotaWindowUsageTone(item)" :title="quotaWindowUsageTitle(item)">
                        <span v-for="tag in quotaWindowUsageTags(item)" :key="tag.label" class="quota-usage-chip" :class="tag.tone ? `is-${tag.tone}` : undefined">
                          <span class="quota-usage-chip-label">{{ tag.label }}</span>
                          <strong class="quota-usage-chip-value">{{ tag.value }}</strong>
                        </span>
                      </div>
                    </div>
                    <span v-else>-</span>
                  </TableCell>
                  <TableCell>
                    <div v-if="quotaWindowItems(row).length" class="quota-usage-cell">
                      <div v-for="item in quotaWindowItems(row)" :key="item.label" class="quota-usage-item is-projection" :title="quotaWindowPredictionTitle(item)">
                        <span class="quota-usage-chip is-projection" :class="{ 'is-stale': !item.resetAt || item.usage?.stale === true }">
                          <span class="quota-usage-chip-label">{{ t('额度', 'Quota') }}</span>
                          <strong class="quota-usage-chip-value">
                            {{ !item.resetAt || item.usage?.stale === true
                              ? t('需刷新', 'Refresh needed')
                              : quotaWindowProjectedCost(item) === null
                                ? '-'
                                : formatUsd(quotaWindowProjectedCost(item) ?? 0) }}
                          </strong>
                        </span>
                      </div>
                    </div>
                    <span v-else>-</span>
                  </TableCell>
                  <TableCell>
                    <span
                      class="account-table-value-pill is-time"
                      :class="{ 'is-empty': formatRelativeTime(credentialActivityAt(row), relativeTimeNow) === '-' }"
                      :title="formatDateTime(credentialActivityAt(row))"
                    >
                      {{ formatRelativeTime(credentialActivityAt(row), relativeTimeNow) }}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span class="account-table-value-pill is-action" :class="{ 'is-empty': latestActionText(row) === '-' }" :title="latestActionText(row) === '-' ? undefined : latestActionText(row)">
                      {{ latestActionText(row) }}
                    </span>
                  </TableCell>
                  <TableCell class="text-right">
                    <DropdownMenu v-if="canManageAccounts">
                      <DropdownMenuTrigger as-child>
                        <Button variant="ghost" size="icon-sm" class="account-actions-trigger" :aria-label="t(`打开 ${row.email ?? row.name} 的操作菜单`, `Open actions for ${row.email ?? row.name}`)">
                          <MoreHorizontal />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" :side-offset="4" class="w-40">
                        <DropdownMenuGroup>
                          <DropdownMenuItem @select="openDetail(row)"><Eye /><span>{{ t('详情', 'Details') }}</span></DropdownMenuItem>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator v-if="isCredentialMutable(row)" />
                        <DropdownMenuGroup v-if="isCredentialMutable(row)">
                          <DropdownMenuItem :disabled="isRowActing(row) || isBulkOperationRunning" @select="row.disabled ? confirmEnableAccount(row) : confirmDisableAccount(row)">
                            <ShieldCheck v-if="row.disabled" /><PauseCircle v-else />
                            <span>{{ row.disabled ? t('启用', 'Enable') : t('禁用', 'Disable') }}</span>
                          </DropdownMenuItem>
                          <DropdownMenuItem v-if="isCodexCredential(row)" :disabled="isRowActing(row) || isBulkOperationRunning" @select="refreshAccount(row)"><RefreshCw /><span>{{ t('巡检', 'Inspect') }}</span></DropdownMenuItem>
                          <DropdownMenuItem v-if="row.v8_supported" :disabled="isRowActing(row)" @select="runCoreCredentialAction(row, 'core-refresh')"><RefreshCw /><span>{{ t('Core 刷新令牌', 'Refresh token in Core') }}</span></DropdownMenuItem>
                          <DropdownMenuItem v-if="row.v8_supported" :disabled="isRowActing(row) || row.disabled" @select="runCoreCredentialAction(row, 'core-verify')"><ShieldCheck /><span>{{ t('推理验证', 'Verify inference') }}</span></DropdownMenuItem>
                          <DropdownMenuItem v-if="row.v8_supported && oauthProviderOptions.some(option => option.value === (row.provider === 'claude' ? 'anthropic' : row.provider === 'gemini' ? 'gemini-cli' : row.provider))" :disabled="isRowActing(row)" @select="openTargetOAuth(row)"><KeyRound /><span>{{ t('重新授权此凭证', 'Reauthorize this credential') }}</span></DropdownMenuItem>
                          <DropdownMenuItem v-if="row.siwc_bind_supported" :disabled="isRowActing(row)" @select="openTargetOAuth(row, true)"><KeyRound /><span>{{ t('绑定 SIWC', 'Bind SIWC') }}</span></DropdownMenuItem>
                        </DropdownMenuGroup>
                        <DropdownMenuSeparator v-if="isCredentialMutable(row)" />
                        <DropdownMenuItem v-if="isCredentialMutable(row)" variant="destructive" :disabled="isRowActing(row) || isBulkOperationRunning" @select="confirmDeleteAccount(row)"><Trash2 /><span>{{ t('删除', 'Delete') }}</span></DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                    <Button v-else size="sm" variant="ghost" @click="openDetail(row)">{{ t('详情', 'Details') }}</Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
        </section>
        <TablePaginationFooter
          class="account-table-pagination"
          :page="accountListPage"
          :page-size="accountDisplaySize"
          :page-size-options="[50, 100, 150, 200]"
          :total="sortedListAccounts.length"
          @update:page="handleAccountPageChange"
          @update:page-size="setAccountDisplaySize"
        />
      </CardContent>
    </Card>

    <CodexKeeperLogsPanel
      v-if="canManageAccounts"
      :logs="keeperStatus?.logs ?? []"
      @refresh="loadKeeperStatus"
    />

    <Sheet v-model:open="detailOpen">
      <SheetContent
        side="right"
        :show-close-button="false"
        class="overflow-hidden data-[side=right]:w-full data-[side=right]:sm:max-w-[520px]"
      >
        <SheetHeader>
          <div class="detail-drawer-header">
            <Button variant="ghost" size="sm" class="detail-back-button" @click="detailOpen = false">
              <ArrowLeft data-icon="inline-start" />
              {{ t('返回', 'Back') }}
            </Button>
            <SheetTitle class="detail-drawer-title">{{ t('凭证详情', 'Credential Details') }}</SheetTitle>
          </div>
          <SheetDescription>{{ selectedAccount?.email ?? selectedAccount?.name ?? '-' }}</SheetDescription>
        </SheetHeader>
        <div class="detail-drawer-body">
          <dl v-if="selectedAccount" class="detail-list">
            <div class="detail-row"><dt>{{ t('凭证', 'Credential') }}</dt><dd>{{ selectedAccount.name }}</dd></div>
            <div class="detail-row"><dt>{{ t('邮箱', 'Email') }}</dt><dd>{{ selectedAccount.email ?? '-' }}</dd></div>
            <div v-if="canManageAccounts" class="detail-row"><dt>{{ t('备注', 'Note') }}</dt><dd>{{ isSelectedAccountNoteLoading ? t('加载中...', 'Loading...') : (selectedAccountNote ?? '-') }}</dd></div>
            <div class="detail-row"><dt>{{ t('提供商', 'Provider') }}</dt><dd>{{ credentialProviderLabel(selectedAccount.provider) }}</dd></div>
            <div v-if="selectedAccount.account_type" class="detail-row"><dt>{{ t('账户类型', 'Account Type') }}</dt><dd>{{ accountTypeLabel(selectedAccount.account_type) }}</dd></div>
            <div v-if="selectedAccount.account" class="detail-row"><dt>{{ t('账户', 'Account') }}</dt><dd>{{ selectedAccount.account }}</dd></div>
            <div v-if="selectedAccount.project_id" class="detail-row"><dt>{{ t('项目 ID', 'Project ID') }}</dt><dd>{{ selectedAccount.project_id }}</dd></div>
            <div v-if="selectedAccount.auth_index" class="detail-row"><dt>{{ t('认证索引', 'Auth Index') }}</dt><dd>{{ selectedAccount.auth_index }}</dd></div>
            <div class="detail-row"><dt>{{ t('启用状态', 'Enabled Status') }}</dt><dd>{{ selectedAccount.disabled ? t('已禁用', 'Disabled') : t('启用中', 'Enabled') }}</dd></div>
            <div class="detail-row"><dt>{{ t('运行状态', 'Runtime Status') }}</dt><dd>{{ selectedAccount.status ?? '-' }}</dd></div>
            <template v-if="selectedAccount.v8_supported">
              <div class="detail-row"><dt>{{ t('恢复阶段', 'Recovery stage') }}</dt><dd>{{ coreRecoveryStageLabel(selectedAccount.recovery_stage ?? '-') }}</dd></div>
              <div class="detail-row"><dt>{{ t('续期状态', 'Renewal state') }}</dt><dd>{{ coreRecoveryStageLabel(selectedAccount.renewal_state ?? '-') }}</dd></div>
              <div class="detail-row"><dt>{{ t('访问令牌到期', 'Access token expires') }}</dt><dd>{{ formatDateTime(selectedAccount.access_token_expires_at ?? null) }}</dd></div>
              <div class="detail-row"><dt>{{ t('下次刷新检查', 'Next refresh check') }}</dt><dd>{{ formatDateTime(selectedAccount.next_refresh_after ?? null) }}</dd></div>
              <div v-if="selectedAccount.use_siwc" class="detail-row"><dt>SIWC</dt><dd>{{ selectedAccount.siwc_status ?? '-' }} · {{ formatDateTime(selectedAccount.siwc_expires_at ?? null) }}</dd></div>
              <div v-if="selectedAccount.refresh_error?.message" class="detail-row"><dt>{{ t('续期错误', 'Renewal error') }}</dt><dd>{{ selectedAccount.refresh_error.message }}</dd></div>
            </template>
            <div v-if="selectedAccount.success !== null" class="detail-row"><dt>{{ t('成功请求', 'Successful Requests') }}</dt><dd>{{ formatInteger(selectedAccount.success) }}</dd></div>
            <div v-if="selectedAccount.failed !== null" class="detail-row"><dt>{{ t('失败请求', 'Failed Requests') }}</dt><dd>{{ formatInteger(selectedAccount.failed) }}</dd></div>
            <div v-if="selectedAccount.weight !== null" class="detail-row"><dt>{{ t('调度权重', 'Routing Weight') }}</dt><dd>{{ selectedAccount.weight }}</dd></div>
            <div v-if="selectedAccount.request_retry !== null" class="detail-row"><dt>{{ t('请求重试次数', 'Request Retry Count') }}</dt><dd>{{ selectedAccount.request_retry }}</dd></div>
            <div class="detail-row">
              <dt>{{ t('当前优先级', 'Current Priority') }}</dt>
              <dd class="detail-priority-control">
                <Button
                  v-if="canManageAccounts && isCredentialMutable(selectedAccount)"
                  size="xs"
                  variant="outline"
                  :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
                  @click="openPriorityDialog(selectedAccount)"
                >
                  <Spinner v-if="isActionLoading(selectedAccount, 'priority')" data-icon="inline-start" />
                  {{ t('修改优先级', 'Change Priority') }}
                </Button>
                <span>{{ accountPriority(selectedAccount) }}</span>
              </dd>
            </div>
            <div v-if="isCodexCredential(selectedAccount)" class="detail-row"><dt>{{ t('类型默认优先级', 'Type Default Priority') }}</dt><dd>{{ defaultPriority(selectedAccount) ?? '-' }}</dd></div>
            <div v-if="isCodexCredential(selectedAccount)" class="detail-row"><dt>{{ t('状态码', 'Status Code') }}</dt><dd>{{ selectedAccount.last_status_code ?? '-' }}</dd></div>
            <div v-if="isCodexCredential(selectedAccount)" class="detail-row"><dt>{{ t('最近健康', 'Last Healthy') }}</dt><dd>{{ formatDateTime(selectedAccount.last_healthy_at) }}</dd></div>
            <div class="detail-row"><dt>{{ isCodexCredential(selectedAccount) ? t('最近巡检', 'Last Inspection') : t('最近刷新', 'Last Refresh') }}</dt><dd>{{ formatDateTime(credentialActivityAt(selectedAccount)) }}</dd></div>
            <div class="detail-row"><dt>{{ t('最近操作', 'Latest Action') }}</dt><dd>{{ latestActionText(selectedAccount) }}</dd></div>
          </dl>
          <section v-if="selectedAccount && canManageAccounts" class="detail-section">
            <div class="detail-section-heading">
              <h3>{{ t('支持模型', 'Supported Models') }}</h3>
              <Button size="sm" variant="outline" :disabled="selectedAccountModelsLoading" @click="loadSelectedAccountModels">
                <Spinner v-if="selectedAccountModelsLoading" data-icon="inline-start" />
                <Search v-else data-icon="inline-start" />
                {{ selectedAccountModels === null ? t('查询模型', 'Load Models') : t('重新查询', 'Load Again') }}
              </Button>
            </div>
            <Alert v-if="selectedAccountModelsError" variant="destructive"><AlertDescription>{{ selectedAccountModelsError }}</AlertDescription></Alert>
            <div v-else-if="selectedAccountModels !== null" class="credential-model-list">
              <Badge v-for="model in selectedAccountModels" :key="model.id" variant="secondary" :title="model.display_name || model.id">{{ model.id }}</Badge>
              <span v-if="selectedAccountModels.length === 0" class="text-sm text-muted-foreground">{{ t('该凭证当前没有已注册模型', 'No models are currently registered for this credential') }}</span>
            </div>
          </section>
          <section v-if="selectedAccount && selectedAccountQuotaObservations.length > 0" class="detail-section">
            <div class="detail-section-heading">
              <h3>{{ t('CPA 额度状态', 'CPA Quota Status') }}</h3>
              <span>{{ t('最近一次上游观测', 'Latest upstream observation') }}</span>
            </div>
            <div class="credential-observation-list">
              <div
                v-for="observation in selectedAccountQuotaObservations"
                :key="observation.label"
                class="credential-observation-item"
              >
                <div class="credential-observation-heading">
                  <strong>{{ observation.label }}</strong>
                  <span>{{ formatDateTime(observation.observedAt) }}</span>
                </div>
                <div v-if="observation.signals.length > 0" class="credential-observation-signals">
                  <Badge v-for="signal in observation.signals" :key="signal.name" variant="secondary">
                    {{ signal.name }}: {{ signal.value }}
                  </Badge>
                </div>
              </div>
            </div>
          </section>
          <section v-if="selectedAccount && shouldShowQuotaWindow(selectedAccount)" class="detail-section">
            <div class="detail-section-heading">
              <h3>{{ t('额度窗口', 'Quota Windows') }}</h3>
              <span>{{ t('用量、重置时间与预测', 'Usage, reset time, and projection') }}</span>
            </div>
            <div
              class="detail-quota-list"
              :class="{ 'is-expanded': !hasFiveHourQuotaWindow(selectedAccount) }"
            >
              <Card v-for="item in quotaWindowItems(selectedAccount)" :key="item.label" class="detail-quota-card">
                <CardHeader class="detail-quota-card-header">
                  <CardTitle>{{ item.label }}</CardTitle>
                  <Badge variant="secondary">{{ t(`剩余 ${item.remainingPercent}%`, `${item.remainingPercent}% remaining`) }}</Badge>
                </CardHeader>
                <CardContent class="detail-quota-card-content">
                  <div class="quota-window-track">
                    <div class="quota-window-fill" :class="quotaBarTone(item.remainingPercent)" :style="{ width: `${item.remainingPercent}%` }" />
                  </div>
                  <div class="detail-quota-metrics">
                    <div>
                      <span>{{ t('重置时间', 'Reset Time') }}</span>
                      <strong>{{ formatQuotaResetTime(item.resetAt) ?? '-' }}</strong>
                    </div>
                    <div>
                      <span>{{ t('窗口预测', 'Window Projection') }}</span>
                      <strong>{{ !item.resetAt || item.usage?.stale === true
                        ? t('需刷新', 'Refresh needed')
                        : quotaWindowProjectedCost(item) === null
                          ? '-'
                          : formatUsd(quotaWindowProjectedCost(item) ?? 0) }}</strong>
                    </div>
                  </div>
                  <div class="detail-quota-usage" :title="quotaWindowUsageTitle(item)">
                    <span v-for="tag in quotaWindowUsageTags(item)" :key="tag.label">
                      <small>{{ tag.label }}</small>
                      <strong>{{ tag.value }}</strong>
                    </span>
                  </div>
                </CardContent>
              </Card>
            </div>
          </section>
          <section v-if="selectedAccount && canManageAccounts && isCodexCredential(selectedAccount)" class="detail-section">
            <div class="detail-section-heading">
              <h3>{{ t('主动重置', 'Manual Reset') }}</h3>
              <span>{{ t('查询结果会保留到下次查询或使用', 'The result stays cached until the next query or use') }}</span>
            </div>
            <Card class="detail-reset-card">
              <CardContent class="detail-reset-content">
                <template v-if="selectedAccount.reset_credits">
                  <div class="detail-reset-summary">
                    <div>
                      <span>{{ t('可用次数', 'Available') }}</span>
                      <strong>{{ formatInteger(selectedAccount.reset_credits.available_count) }}</strong>
                    </div>
                    <div>
                      <span>{{ t('最近过期', 'Earliest Expiry') }}</span>
                      <strong>{{ formatDateTime(selectedAccount.reset_credits.earliest_expires_at) }}</strong>
                    </div>
                  </div>
                  <p class="detail-reset-cached-at">
                    {{ t('查询于', 'Queried at') }} {{ formatDateTime(selectedAccount.reset_credits.cached_at) }}
                  </p>
                </template>
                <p v-else class="detail-reset-empty">
                  {{ t('尚未查询该凭证的可用重置次数。', 'Reset credits have not been queried for this credential.') }}
                </p>
                <div class="detail-reset-actions">
                  <Button
                    size="sm"
                    variant="outline"
                    :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
                    @click="runResetCreditAction(selectedAccount, 'reset-query')"
                  >
                    <Spinner v-if="isActionLoading(selectedAccount, 'reset-query')" data-icon="inline-start" />
                    <Search v-else data-icon="inline-start" />
                    {{ selectedAccount.reset_credits ? t('重新查询', 'Query Again') : t('查询次数', 'Query Credits') }}
                  </Button>
                  <Button
                    v-if="selectedAccount.reset_credits"
                    size="sm"
                    variant="destructive"
                    :disabled="selectedAccount.reset_credits.available_count <= 0 || isRowActing(selectedAccount) || isBulkOperationRunning"
                    @click="confirmConsumeResetCredit(selectedAccount)"
                  >
                    <Spinner v-if="isActionLoading(selectedAccount, 'reset-consume')" data-icon="inline-start" />
                    <RefreshCw v-else data-icon="inline-start" />
                    {{ t('使用一次', 'Use Once') }}
                  </Button>
                </div>
                <Alert
                  v-if="getResetCreditFeedback(selectedAccount.name)"
                  :variant="getResetCreditFeedback(selectedAccount.name)?.variant === 'error' ? 'destructive' : 'default'"
                  :class="[
                    'detail-reset-feedback',
                    getResetCreditFeedback(selectedAccount.name)?.variant === 'error' ? 'is-error' : '',
                    getResetCreditFeedback(selectedAccount.name)?.variant === 'warning' ? 'is-warning' : '',
                  ]"
                >
                  <Check v-if="getResetCreditFeedback(selectedAccount.name)?.variant === 'success'" />
                  <ShieldAlert v-else />
                  <AlertDescription>{{ getResetCreditFeedback(selectedAccount.name)?.text }}</AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </section>
          <Separator v-if="selectedAccount && canManageAccounts && isCredentialMutable(selectedAccount)" class="detail-action-separator" />
          <div v-if="selectedAccount && canManageAccounts && isCredentialMutable(selectedAccount)" class="detail-action-row">
            <Button size="sm" variant="outline" @click="openAuthFileEditor(selectedAccount)">
              <Pencil data-icon="inline-start" />
              {{ t('认证文件管理', 'Auth File Management') }}
            </Button>
            <Button
              v-if="isCodexCredential(selectedAccount)"
              size="sm"
              variant="outline"
              :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
              @click="refreshAccount(selectedAccount, { closeDetail: true })"
            >
              <Spinner v-if="isActionLoading(selectedAccount, 'refresh')" data-icon="inline-start" />
              <RefreshCw v-else data-icon="inline-start" />
              {{ t('刷新', 'Refresh') }}
            </Button>
            <Button
              v-if="selectedAccount.disabled"
              size="sm"
              variant="outline"
              :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
              @click="confirmEnableAccount(selectedAccount)"
            >
              <Spinner v-if="isActionLoading(selectedAccount, 'toggle')" data-icon="inline-start" />
              {{ t('启用', 'Enable') }}
            </Button>
            <Button
              v-else
              size="sm"
              variant="outline"
              :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
              @click="confirmDisableAccount(selectedAccount)"
            >
              <Spinner v-if="isActionLoading(selectedAccount, 'toggle')" data-icon="inline-start" />
              {{ t('禁用', 'Disable') }}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              :disabled="isRowActing(selectedAccount) || isBulkOperationRunning"
              @click="confirmDeleteAccount(selectedAccount)"
            >
              <Spinner v-if="isActionLoading(selectedAccount, 'delete')" data-icon="inline-start" />
              {{ t('删除', 'Delete') }}
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>

    <Dialog v-if="canManageAccounts" :open="oauthDialogOpen" @update:open="handleCodexOAuthDialogOpen">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-[640px]">
        <DialogHeader>
          <DialogTitle>{{ t('OAuth 登录', 'OAuth Login') }}</DialogTitle>
          <DialogDescription>
            {{ t('选择提供商完成 OAuth 登录，生成的认证文件会自动加入凭证管理。', 'Choose a provider to sign in with OAuth. The generated auth file will be added to credential management automatically.') }}
          </DialogDescription>
        </DialogHeader>
        <div class="oauth-dialog">
          <Alert v-if="oauthTarget">
            <AlertDescription>{{ oauthFlow === 'siwc' ? t('为指定凭证绑定 SIWC', 'Bind SIWC to the selected credential') : t('仅重新授权指定凭证；其他账户不会被替换', 'Reauthorize only this credential; other accounts will not be replaced') }} · {{ oauthTarget.name }}</AlertDescription>
          </Alert>
          <FieldGroup>
            <Field>
              <FieldLabel>{{ t('提供商', 'Provider') }}</FieldLabel>
              <Select
                :model-value="oauthProvider"
                :disabled="Boolean(oauthTarget) || oauthDialogStatus === 'waiting' || isStartingOAuth"
                @update:model-value="handleOAuthProviderChange"
              >
                <SelectTrigger class="w-full">
                  <SelectValue :placeholder="t('选择 OAuth 提供商', 'Select an OAuth provider')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem
                      v-for="option in oauthProviderOptions"
                      :key="option.value"
                      :value="option.value"
                    >
                      {{ option.label }}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field v-if="oauthProvider === 'gemini-cli'">
              <FieldLabel>{{ t('项目 ID（可选）', 'Project ID (optional)') }}</FieldLabel>
              <Input
                v-model="oauthProjectId"
                :disabled="oauthDialogStatus === 'waiting' || isStartingOAuth"
                :placeholder="t('Google Cloud 项目 ID', 'Google Cloud project ID')"
              />
            </Field>
          </FieldGroup>
          <div class="oauth-status-row">
            <span>{{ t('认证状态', 'Authentication status') }}</span>
            <Badge :variant="oauthStatusType">
              {{ oauthStatusText }}
            </Badge>
          </div>
          <Alert v-if="oauthError" :variant="oauthDialogStatus === 'error' ? 'destructive' : 'default'">
            <AlertDescription>{{ oauthError }}</AlertDescription>
          </Alert>
          <Button
            v-if="oauthDialogStatus === 'idle'"
            :disabled="isStartingOAuth"
            @click="startCodexOAuth"
          >
            <Spinner v-if="isStartingOAuth" data-icon="inline-start" />
            <LogIn v-else data-icon="inline-start" />
            {{ oauthFlow === 'siwc' ? t('开始 SIWC 绑定', 'Start SIWC Binding') : t(`开始 ${oauthProviderLabel} 登录`, `Start ${oauthProviderLabel} Login`) }}
          </Button>
          <div v-if="oauthAuthURL" class="oauth-dialog-section">
            <label>{{ t('授权链接', 'Authorization link') }}</label>
            <Textarea
              readonly
              :model-value="oauthAuthURL"
              rows="3"
            />
            <div class="flex flex-wrap gap-2">
              <Button variant="outline" @click="openCodexOAuthURL">
                <ExternalLink data-icon="inline-start" />
                {{ t('打开链接', 'Open Link') }}
              </Button>
              <Button variant="outline" @click="copyCodexOAuthURL">
                <Copy data-icon="inline-start" />
                {{ t('复制链接', 'Copy Link') }}
              </Button>
            </div>
          </div>
          <div v-if="oauthDialogStatus === 'waiting' && oauthCallbackSupported" class="oauth-dialog-section">
            <label>{{ t('回调 URL', 'Callback URL') }}</label>
            <p class="oauth-dialog-hint">
              {{ t('如果当前浏览器无法访问 localhost 回调地址，请复制浏览器最终跳转后的完整 URL 并粘贴到这里。', 'If this browser cannot reach the localhost callback, paste the complete URL from the browser after its final redirect here.') }}
            </p>
            <Textarea
              v-model="oauthCallbackURL"
              rows="4"
              :placeholder="t('粘贴完整回调 URL', 'Paste the complete callback URL')"
            />
            <Button
              variant="outline"
              :disabled="!oauthCallbackURL.trim() || isSubmittingOAuthCallback"
              @click="submitCodexOAuthCallbackURL"
            >
              <Spinner v-if="isSubmittingOAuthCallback" data-icon="inline-start" />
              {{ t('提交回调 URL', 'Submit Callback URL') }}
            </Button>
          </div>
          <Button
            v-if="oauthDialogStatus === 'success' || oauthDialogStatus === 'error'"
            variant="outline"
            :disabled="isStartingOAuth"
            @click="startCodexOAuth"
          >
            <Spinner v-if="isStartingOAuth" data-icon="inline-start" />
            {{ t(`重新发起 ${oauthProviderLabel} 登录`, `Restart ${oauthProviderLabel} Login`) }}
          </Button>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            :disabled="isStartingOAuth || isSubmittingOAuthCallback"
            @click="closeCodexOAuthDialog"
          >
            {{ t('关闭', 'Close') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog
      v-if="canManageAccounts"
      :open="authFileEditor !== null"
      @update:open="handleAuthFileEditorOpen"
    >
      <DialogContent
        class="max-h-[calc(100vh-2rem)] overflow-hidden sm:max-w-[720px]"
        :show-close-button="authFileEditor?.saving !== true"
        @escape-key-down="authFileEditor?.saving && $event.preventDefault()"
        @pointer-down-outside="authFileEditor?.saving && $event.preventDefault()"
      >
        <DialogHeader>
          <DialogTitle>
            {{ authFileEditor
              ? t(`认证文件管理 - ${authFileEditor.fileName}`, `Auth File Management - ${authFileEditor.fileName}`)
              : t('认证文件管理', 'Auth File Management') }}
          </DialogTitle>
          <DialogDescription>
            {{ t('查看并管理 CPA 返回的认证文件配置。', 'Review and manage the auth file returned by CPA.') }}
          </DialogDescription>
        </DialogHeader>
        <div v-if="authFileEditor" class="auth-file-editor">
          <div v-if="authFileEditor.loading" class="auth-file-editor-loading">
            <Spinner />
            {{ t('正在加载认证文件...', 'Loading auth file...') }}
          </div>
          <template v-else>
            <Alert v-if="authFileEditor.error" variant="destructive">
              <AlertDescription>{{ authFileEditor.error }}</AlertDescription>
            </Alert>
            <FieldGroup>
              <Field class="auth-file-editor-json-block">
                <FieldLabel>{{ t('认证文件信息（info）', 'Auth file info (info)') }}</FieldLabel>
                <Textarea
                  readonly
                  :model-value="authFileEditor.fileInfoText"
                  rows="6"
                />
              </Field>
              <Field class="auth-file-editor-json-block">
                <FieldLabel>
                  {{ authFileEditor.json
                    ? t('认证文件 JSON（预览）', 'Auth file JSON (preview)')
                    : t('下载内容（已截断）', 'Downloaded content (truncated)') }}
                </FieldLabel>
                <Textarea
                  v-if="authFileEditor.json"
                  readonly
                  :model-value="authFileEditorUpdatedText"
                  rows="8"
                />
                <pre v-else class="auth-file-editor-invalid-preview">{{ authFileEditor.invalidContentPreview }}</pre>
              </Field>
              <div v-if="authFileEditor.json" class="auth-file-editor-fields">
                <Field>
                  <FieldLabel>{{ t('前缀（prefix）', 'Prefix (prefix)') }}</FieldLabel>
                  <Input v-model="authFileEditor.prefix" />
                </Field>
                <Field>
                  <FieldLabel>{{ t('代理 URL（proxy_url）', 'Proxy URL (proxy_url)') }}</FieldLabel>
                  <Input
                    v-model="authFileEditor.proxyUrl"
                    :placeholder="t('socks5://username:password@proxy_ip:port/', 'socks5://username:password@proxy_ip:port/')"
                  />
                </Field>
                <Field>
                  <FieldLabel>{{ t('调度权重（weight）', 'Routing Weight (weight)') }}</FieldLabel>
                  <Input
                    type="number"
                    step="1"
                    max="1000000"
                    :model-value="authFileEditor.weight ?? ''"
                    :placeholder="t('留空使用默认权重 1', 'Leave empty to use the default weight of 1')"
                    @update:model-value="setAuthFileWeight"
                  />
                  <FieldDescription>{{ t('设置为 0 可退出加权调度，最大值为 1000000。', 'Set to 0 to exclude this credential from weighted routing; maximum 1000000.') }}</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>{{ t('请求重试次数（request_retry）', 'Request Retry Count (request_retry)') }}</FieldLabel>
                  <Input
                    type="number"
                    step="1"
                    min="0"
                    :model-value="authFileEditor.requestRetry ?? ''"
                    :placeholder="t('留空使用全局配置', 'Leave empty to use the global setting')"
                    @update:model-value="setAuthFileRequestRetry"
                  />
                  <FieldDescription>{{ t('覆盖此凭证的请求级重试次数。', 'Override the request retry count for this credential.') }}</FieldDescription>
                </Field>
                <Field v-if="authFileEditorSupportsWebsockets" orientation="horizontal" class="auth-file-editor-switch-field">
                  <div>
                    <FieldLabel>{{ t('WebSockets（websockets）', 'WebSockets (websockets)') }}</FieldLabel>
                    <FieldDescription>{{ t('为此认证文件启用 WebSocket 支持。', 'Enable WebSocket support for this auth file.') }}</FieldDescription>
                  </div>
                  <Switch
                    :model-value="authFileEditor.websockets"
                    @update:model-value="setAuthFileWebsockets"
                  />
                </Field>
                <Field v-if="authFileEditorSupportsUsingApi" orientation="horizontal" class="auth-file-editor-switch-field">
                  <div>
                    <FieldLabel>{{ t('API 模式（using_api）', 'API Mode (using_api)') }}</FieldLabel>
                    <FieldDescription>{{ t('让 xAI 凭证使用 API 模式。', 'Use API mode for this xAI credential.') }}</FieldDescription>
                  </div>
                  <Switch
                    :model-value="authFileEditor.usingApi"
                    @update:model-value="setAuthFileUsingApi"
                  />
                </Field>
                <Field class="auth-file-editor-wide-field">
                  <FieldLabel>{{ t('自定义请求头（headers）', 'Custom headers (headers)') }}</FieldLabel>
                  <Textarea
                    :model-value="authFileEditor.headersText"
                    :placeholder="'{ &quot;Header-Name&quot;: &quot;value&quot; }'"
                    rows="4"
                    @update:model-value="handleAuthFileHeadersChange(String($event))"
                  />
                  <FieldDescription v-if="!authFileEditor.headersError">
                    {{ t('以 JSON 对象格式输入，每个值都必须是字符串。', 'Enter a JSON object; every value must be a string.') }}
                  </FieldDescription>
                  <p v-else class="auth-file-editor-error">{{ authFileEditor.headersError }}</p>
                </Field>
                <Field class="auth-file-editor-wide-field">
                  <FieldLabel>{{ t('备注（note）', 'Note (note)') }}</FieldLabel>
                  <Textarea
                    v-model="authFileEditor.note"
                    rows="3"
                    :placeholder="t('输入备注信息，例如：张三的凭证', 'Enter a note, for example: credential owner')"
                    @update:model-value="authFileEditor.noteTouched = true"
                  />
                </Field>
              </div>
            </FieldGroup>
          </template>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            :disabled="authFileEditor?.saving === true"
            @click="closeAuthFileEditor"
          >
            {{ authFileEditorDirty ? t('取消', 'Cancel') : t('关闭', 'Close') }}
          </Button>
          <Button
            variant="secondary"
            :disabled="authFileEditor?.saving === true || !authFileEditorUpdatedText"
            @click="copyAuthFileEditorText"
          >
            <Copy data-icon="inline-start" />
            {{ t('复制', 'Copy') }}
          </Button>
          <Button
            :disabled="!authFileEditorDirty || !!authFileEditor?.headersError || authFileEditor?.loading === true || authFileEditor?.saving === true"
            @click="saveAuthFileEditor"
          >
            <Spinner v-if="authFileEditor?.saving === true" data-icon="inline-start" />
            {{ t('保存', 'Save') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-if="canManageAccounts" v-model:open="accountConfirmDialog.show">
      <DialogContent class="sm:max-w-[420px]">
        <DialogHeader>
          <DialogTitle>{{ accountConfirmDialog.title }}</DialogTitle>
          <DialogDescription>{{ accountConfirmDialog.content }}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            :disabled="isAccountConfirmSubmitting"
            @click="accountConfirmDialog.show = false"
          >
            {{ t('取消', 'Cancel') }}
          </Button>
          <Button
            :variant="accountConfirmButtonVariant()"
            :disabled="isAccountConfirmSubmitting"
            @click="submitAccountConfirm"
          >
            <Spinner v-if="isAccountConfirmSubmitting" data-icon="inline-start" />
            {{ accountConfirmDialog.positiveText }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-if="canManageAccounts" v-model:open="bulkDeleteDialog.show">
      <DialogContent class="sm:max-w-[460px]">
        <DialogHeader>
          <DialogTitle>{{ bulkDeleteDialogTitle }}</DialogTitle>
          <DialogDescription>{{ bulkDeleteWarningText }}</DialogDescription>
        </DialogHeader>
        <div v-if="bulkDeletePreviewNames.length > 0" class="bulk-delete-preview">
          <Badge v-for="name in bulkDeletePreviewNames" :key="name" variant="secondary">
            {{ name }}
          </Badge>
          <Badge v-if="bulkDeletePreviewOverflow > 0" variant="outline">
            {{ t(`另 ${bulkDeletePreviewOverflow} 个...`, `${bulkDeletePreviewOverflow} more...`) }}
          </Badge>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            :disabled="isBulkDeleting"
            @click="bulkDeleteDialog.show = false"
          >
            {{ t('取消', 'Cancel') }}
          </Button>
          <Button
            variant="destructive"
            :disabled="selectedAccountCount === 0 || isBulkDeleting"
            @click="submitBulkDelete"
          >
            <Spinner v-if="isBulkDeleting" data-icon="inline-start" />
            {{ t('确认删除', 'Confirm Delete') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-if="canManageAccounts" v-model:open="priorityDialog.show">
      <DialogContent class="sm:max-w-[460px]">
        <DialogHeader>
          <DialogTitle>{{ priorityDialogTitle }}</DialogTitle>
          <DialogDescription>{{ priorityDialogHint }}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel>{{ t('优先级模式', 'Priority Mode') }}</FieldLabel>
            <Select :model-value="priorityDialog.mode" @update:model-value="handlePriorityDialogMode">
              <SelectTrigger class="w-full">
                <SelectValue :placeholder="t('选择优先级模式', 'Select priority mode')" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem
                    v-for="option in priorityModeOptions"
                    :key="option.value"
                    :value="option.value"
                    :disabled="option.disabled"
                  >
                    {{ option.label }}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field v-if="priorityDialog.mode !== 'default'">
            <FieldLabel>{{ t('优先级数值', 'Priority Value') }}</FieldLabel>
            <Input
              type="number"
              step="1"
              :min="priorityDialog.mode === 'high' ? 21 : undefined"
              :max="priorityDialog.mode === 'low' ? -2 : undefined"
              :model-value="priorityDialog.value ?? ''"
              @update:model-value="setPriorityDialogValue"
            />
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" @click="priorityDialog.show = false">
            {{ t('取消', 'Cancel') }}
          </Button>
          <Button
            :disabled="!canSubmitPriority || (priorityDialog.account ? isActionLoading(priorityDialog.account, 'priority') : false)"
            @click="submitPriorityDialog"
          >
            <Spinner
              v-if="priorityDialog.account && isActionLoading(priorityDialog.account, 'priority')"
              data-icon="inline-start"
            />
            {{ t('确认', 'Confirm') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>

<style scoped>
.account-status-page,
.account-list-panel,
.account-section,
.account-table {
  min-width: 0;
}

.account-header-copy {
  flex: 1;
  min-width: 0;
}

.header-actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  flex-shrink: 0;
  justify-content: flex-end;
}

.auth-file-input {
  display: none;
}

.account-metrics {
  display: grid;
  grid-template-columns: repeat(6, minmax(112px, 1fr));
  gap: 12px;
}

.account-metric-card {
  --metric-color: var(--primary);
  min-height: 116px;
  overflow: hidden;
}

.account-metric-card.is-green {
  --metric-color: var(--cpa-success);
}

.account-metric-card.is-warning {
  --metric-color: var(--cpa-warning);
}

.account-metric-card.is-danger {
  --metric-color: var(--destructive);
}

.account-metric-card.is-purple {
  --metric-color: var(--cpa-accent-purple);
}

.account-metric-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.account-metric-icon {
  display: grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  border-radius: calc(var(--radius) - 2px);
  background: color-mix(in oklch, var(--metric-color) 12%, var(--muted));
  color: var(--metric-color);
}

.account-metric-icon > svg {
  width: 18px;
  height: 18px;
}

.account-metrics .metric-action {
  width: 100%;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  appearance: none;
}

.account-metrics .metric-action:hover {
  border-color: color-mix(in oklch, var(--metric-color) 40%, var(--border));
  transform: translateY(-1px);
}

.account-metrics .metric-action:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 3px;
}

.account-metrics .metric-action.is-active {
  border-color: color-mix(in oklch, var(--metric-color) 58%, var(--border));
  box-shadow: 0 0 0 3px color-mix(in oklch, var(--metric-color) 14%, transparent);
}

.inspection-status-card {
  min-width: 0;
}

.inspection-status-value {
  display: flex;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}

.inspection-status-tag {
  max-width: 100%;
  min-width: 0;
}

.inspection-status-tag {
  display: block;
  max-width: 100%;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-toolbar {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
  border-bottom: 1px solid var(--border);
}

.toolbar-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.toolbar-title {
  margin: 0;
  color: var(--foreground);
  font-size: 15px;
  font-weight: 700;
  line-height: 1.25;
}

.toolbar-subtitle {
  margin: 3px 0 0;
  color: var(--muted-foreground);
  font-size: 13px;
}

.filter-grid {
  display: grid;
  grid-template-columns: minmax(220px, 1.35fr) minmax(150px, 0.8fr) minmax(170px, 0.9fr);
  gap: 8px;
  min-width: 0;
}

.sort-control-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  min-width: 0;
}

.sort-control-label {
  color: var(--muted-foreground);
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.account-list-content {
  padding: 0;
}

.account-section {
  display: grid;
  gap: 12px;
  padding: 0 16px;
}

.account-section-actions-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px 14px;
  min-width: 0;
}

.account-section-actions {
  display: flex;
  flex-wrap: wrap;
  flex-shrink: 0;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}

.account-table-pagination {
  margin-inline: 16px;
}

.account-table-shell {
  min-width: 0;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius) var(--radius) 0 0;
}

.account-table-shell table {
  border-collapse: separate;
  border-spacing: 0;
}

.account-table-shell thead th {
  white-space: nowrap;
  background: var(--muted);
}

.account-table-shell tbody td {
  vertical-align: middle;
}

.account-table-shell tbody tr:hover > td {
  background: color-mix(in oklch, var(--muted) 70%, transparent);
}


.detail-action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-bottom: 4px;
}

.detail-action-separator {
  margin: 16px 0;
}

.detail-reset-feedback {
  align-items: center;
  min-height: 0;
  border: 0;
  border-inline-start: 3px solid var(--cpa-success);
  border-radius: var(--radius-sm);
  padding: 8px 10px;
  background: var(--cpa-success-weak);
  box-shadow: none;
}

.detail-reset-feedback :deep([data-slot="alert-description"]) {
  color: var(--foreground);
  font-size: 12px;
  line-height: 1.4;
}

.detail-reset-feedback.is-warning {
  border-inline-start-color: var(--cpa-warning);
  background: var(--cpa-warning-weak);
}

.detail-reset-feedback.is-error {
  border-inline-start-color: var(--cpa-danger);
  background: var(--cpa-danger-weak);
}

.detail-reset-feedback :deep(svg) {
  color: var(--cpa-success);
}

.detail-reset-feedback.is-warning :deep(svg) {
  color: var(--cpa-warning);
}

.detail-reset-feedback.is-error :deep(svg) {
  color: var(--cpa-danger);
}

.detail-drawer-header {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.detail-back-button {
  flex-shrink: 0;
}

.detail-drawer-title {
  min-width: 0;
  overflow: hidden;
  color: var(--foreground);
  font-size: 16px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-drawer-body {
  min-height: 0;
  overflow-y: auto;
  padding: 0 16px 16px;
}

.detail-list {
  display: grid;
  gap: 0;
  margin: 0;
}

.detail-row {
  display: grid;
  grid-template-columns: minmax(104px, 0.35fr) minmax(0, 1fr);
  gap: 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}

.detail-row dt {
  color: var(--muted-foreground);
  font-size: 13px;
}

.detail-row dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--foreground);
  font-size: 13px;
  text-align: right;
}

.detail-list > .detail-row:last-child {
  border-bottom: 0;
}

.detail-priority-control {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.detail-section {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.detail-section-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 12px;
}

.detail-section-heading h3,
.detail-section-heading span {
  margin: 0;
}

.detail-section-heading h3 {
  color: var(--foreground);
  font-size: 14px;
  font-weight: 650;
}

.credential-model-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

.credential-observation-list {
  display: grid;
  gap: 8px;
}

.credential-observation-item {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--muted);
}

.credential-observation-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
}

.credential-observation-heading span {
  color: var(--muted-foreground);
  font-size: 11px;
}

.credential-observation-signals {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.detail-section-heading span {
  color: var(--muted-foreground);
  font-size: 11px;
  text-align: right;
}

.detail-quota-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.detail-quota-list.is-expanded {
  grid-template-columns: minmax(0, 1fr);
}

.detail-quota-card,
.detail-reset-card {
  min-width: 0;
  box-shadow: none;
}

.detail-quota-card-header {
  display: flex;
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 12px 8px;
}

.detail-quota-card-header :deep([data-slot='card-title']) {
  overflow: hidden;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-quota-card-content {
  display: grid;
  gap: 10px;
  padding: 0 12px 12px;
}

.detail-quota-metrics {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(0, 0.65fr);
  gap: 8px;
}

.detail-quota-metrics > div,
.detail-reset-summary > div {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.detail-quota-metrics span,
.detail-reset-summary span {
  color: var(--muted-foreground);
  font-size: 10px;
}

.detail-quota-metrics strong,
.detail-reset-summary strong {
  overflow: hidden;
  color: var(--foreground);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-quota-usage {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}

.detail-quota-usage > span {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 6px;
  background: var(--muted);
  border-radius: calc(var(--radius) - 3px);
}

.detail-quota-usage small,
.detail-quota-usage strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-quota-usage small {
  color: var(--muted-foreground);
  font-size: 9px;
}

.detail-quota-usage strong {
  color: var(--foreground);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.detail-reset-content {
  display: grid;
  gap: 10px;
  padding: 12px;
}

.detail-reset-summary {
  display: grid;
  grid-template-columns: minmax(80px, 0.35fr) minmax(0, 1fr);
  gap: 12px;
}

.detail-reset-summary > div:first-child strong {
  color: var(--primary);
  font-size: 22px;
  line-height: 1;
}

.detail-reset-cached-at,
.detail-reset-empty {
  margin: 0;
  color: var(--muted-foreground);
  font-size: 11px;
}

.detail-reset-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.oauth-dialog {
  display: grid;
  gap: 14px;
  min-width: 0;
}

.oauth-dialog-description,
.oauth-dialog-message,
.oauth-dialog-hint {
  margin: 0;
  font-size: 13px;
  line-height: 1.55;
}

.oauth-dialog-description,
.oauth-dialog-hint {
  color: var(--muted-foreground);
}

.oauth-dialog-message.is-error {
  color: var(--destructive);
}

.oauth-dialog-message.is-warning {
  color: var(--cpa-warning);
}

.oauth-status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  color: var(--foreground);
  font-size: 13px;
  background: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}

.oauth-dialog-section {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.oauth-dialog-section > label {
  color: var(--muted-foreground);
  font-size: 12px;
}

.oauth-dialog-section textarea {
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
}

.auth-file-editor {
  display: grid;
  gap: 14px;
  min-width: 0;
  max-height: min(72vh, 720px);
  overflow-y: auto;
  padding-right: 2px;
}

.auth-file-editor-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 0;
  color: var(--muted-foreground);
}

.auth-file-editor-error {
  color: var(--destructive);
  font-size: 12px;
  line-height: 1.45;
}

.auth-file-editor-json-block textarea,
.auth-file-editor-wide-field textarea {
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
}

.auth-file-editor-invalid-preview {
  min-height: 120px;
  max-height: 260px;
  margin: 0;
  overflow: auto;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--muted);
  color: var(--foreground);
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.auth-file-editor-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  min-width: 0;
}

.auth-file-editor-wide-field {
  grid-column: 1 / -1;
}

.auth-file-editor-switch-field {
  justify-content: space-between;
}

:global(.quota-window-cell) {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 4px 0;
}

:global(.quota-reset-cell) {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 4px 0;
}

:global(.quota-reset-item) {
  display: flex;
  align-items: center;
  min-width: 0;
  min-height: 38px;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-window-item) {
  display: grid;
  align-content: center;
  gap: 4px;
  min-width: 0;
  min-height: 38px;
}

:global(.quota-window-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  line-height: 1.2;
}

:global(.quota-window-label) {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-window-percent) {
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 700;
}

:global(.quota-window-usage) {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 11px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-window-usage.is-stale) {
  color: var(--cpa-warning);
}

:global(.quota-usage-cell) {
  display: grid;
  gap: 8px;
  min-width: 0;
  padding: 4px 0;
}

:global(.quota-usage-item) {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
  min-width: 0;
  min-height: 38px;
}

:global(.quota-usage-item.is-projection) {
  grid-template-columns: minmax(0, 1fr);
}

:global(.quota-usage-chip) {
  --usage-accent: var(--cpa-primary);
  display: grid;
  align-content: center;
  gap: 2px;
  min-width: 0;
  min-height: 38px;
  padding: 6px 7px;
  overflow: hidden;
  background:
    linear-gradient(
      180deg,
      color-mix(in srgb, var(--usage-accent) 5%, var(--cpa-surface-raised)),
      color-mix(in srgb, var(--cpa-surface-muted) 90%, var(--cpa-surface-raised))
    );
  border: 1px solid color-mix(in srgb, var(--usage-accent) 18%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  box-shadow: inset 0 1px 0 color-mix(in srgb, var(--cpa-surface-raised) 70%, transparent);
}

:global(.quota-usage-chip:nth-child(2)) {
  --usage-accent: var(--cpa-accent-blue);
}

:global(.quota-usage-chip:nth-child(3)) {
  --usage-accent: var(--cpa-accent-orange);
}

:global(.quota-usage-chip.is-projection) {
  --usage-accent: var(--cpa-accent-purple);
}

:global(.quota-usage-chip-label),
:global(.quota-usage-chip-value) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.quota-usage-chip-label) {
  color: color-mix(in srgb, var(--usage-accent) 62%, var(--cpa-text-muted));
  font-size: 10px;
  font-weight: 700;
  line-height: 1.1;
}

:global(.quota-usage-chip-value) {
  color: var(--cpa-text-strong);
  font-size: 12px;
  font-weight: 800;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}

:global(.quota-usage-chip.is-stale) {
  --usage-accent: var(--cpa-warning);
  grid-column: 1 / -1;
  background: color-mix(in srgb, var(--cpa-warning) 9%, var(--cpa-surface-raised));
  border-color: color-mix(in srgb, var(--cpa-warning) 20%, var(--cpa-border));
}

:global(.quota-usage-chip.is-stale .quota-usage-chip-value) {
  color: var(--cpa-warning);
}

:global(.quota-window-track) {
  height: 5px;
  overflow: hidden;
  background: var(--cpa-surface-muted);
  border-radius: 999px;
}

:global(.quota-window-fill) {
  height: 100%;
  min-width: 3px;
  border-radius: inherit;
}

:global(.quota-window-fill.is-healthy) {
  background: var(--cpa-success);
}

:global(.quota-window-fill.is-warning) {
  background: var(--cpa-warning);
}

:global(.quota-window-fill.is-danger) {
  background: var(--cpa-danger);
}

:global(.account-table-identity) {
  display: grid;
  gap: 3px;
  min-width: 0;
  line-height: 1.25;
}

:global(.account-table-email),
:global(.account-table-name) {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.account-table-email) {
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 650;
}

:global(.account-table-name) {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 500;
}

:global(.account-table-meta) {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
  padding-top: 1px;
}

:global(.account-table-chip) {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  min-width: 0;
  padding: 1px 6px;
  overflow: hidden;
  font-size: 11px;
  font-weight: 700;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: var(--cpa-radius-sm);
  font-variant-numeric: tabular-nums;
}

:global(.account-table-chip.is-type) {
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 750;
  background: var(--cpa-surface-muted);
  border-color: color-mix(in srgb, var(--cpa-border) 72%, transparent);
}

:global(.account-table-chip.is-success) {
  color: var(--cpa-success);
  background: var(--cpa-success-weak);
  border-color: color-mix(in srgb, var(--cpa-success) 26%, transparent);
}

:global(.account-table-chip.is-warning) {
  color: var(--cpa-warning);
  background: var(--cpa-warning-weak);
  border-color: color-mix(in srgb, var(--cpa-warning) 26%, transparent);
}

:global(.credential-provider-cell) {
  display: grid;
  gap: 4px;
  min-width: 0;
}

:global(.account-table-chip.is-danger) {
  color: var(--cpa-danger);
  background: var(--cpa-danger-weak);
  border-color: color-mix(in srgb, var(--cpa-danger) 26%, transparent);
}

:global(.account-table-chip.is-purple) {
  color: var(--cpa-accent-purple);
  background: var(--cpa-accent-purple-weak);
  border-color: color-mix(in srgb, var(--cpa-accent-purple) 26%, transparent);
}

:global(.account-table-chip.is-priority) {
  color: var(--cpa-primary);
  background: color-mix(in srgb, var(--cpa-primary) 9%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-primary) 24%, transparent);
}

:global(.account-table-value-pill) {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  min-width: 0;
  padding: 3px 8px;
  overflow: hidden;
  color: var(--cpa-text);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  font-variant-numeric: tabular-nums;
}

:global(.account-table-value-pill.is-time) {
  color: var(--cpa-primary);
  background: color-mix(in srgb, var(--cpa-primary) 8%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-primary) 22%, transparent);
}

:global(.account-table-value-pill.is-action) {
  display: -webkit-box;
  line-height: 1.5;
  white-space: normal;
  overflow-wrap: anywhere;
  background: color-mix(in srgb, var(--cpa-text-muted) 8%, var(--cpa-surface-muted));
  border-color: color-mix(in srgb, var(--cpa-border) 78%, transparent);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  word-break: break-word;
}

:global(.account-table-value-pill.is-action.is-empty) {
  display: inline-flex;
  white-space: nowrap;
}

:global(.account-table-value-pill.is-empty) {
  color: var(--cpa-text-muted);
  font-weight: 700;
}

:global(.account-actions-trigger) {
  margin-left: auto;
  color: var(--cpa-text-muted);
}

.empty-state {
  padding: 42px 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
  text-align: center;
}

.bulk-delete-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 116px;
  padding: 8px;
  overflow: auto;
  color: var(--foreground);
  font-size: 12px;
  background: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}

.bulk-delete-preview span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1280px) {
  .account-metrics {
    grid-template-columns: repeat(3, minmax(112px, 1fr));
  }
}

@media (max-width: 980px) {
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .sort-control-row {
    justify-content: flex-start;
  }
}

@media (max-width: 560px) {
  .detail-quota-list {
    grid-template-columns: minmax(0, 1fr);
  }

  .account-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .header-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
  }

  .header-actions > button {
    width: 100%;
  }

  .auth-file-editor-fields {
    grid-template-columns: minmax(0, 1fr);
  }

  .toolbar-heading,
  .account-section-actions-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .account-section-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .sort-control-row {
    width: 100%;
  }

  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-grid > :first-child {
    grid-column: 1 / -1;
  }

}
</style>
