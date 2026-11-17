<script setup lang="ts">
import type { Component } from 'vue'
import { computed, onMounted, ref } from 'vue'
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
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
  AlertTriangle,
  CircleDollarSign,
  KeyRound,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  UserRound,
  WalletCards,
  RotateCcw,
  Ban,
} from '@lucide/vue'

import {
  createUser,
  disableUser,
  enableUser,
  listUsers,
  updateUser,
  updateUserQuota,
} from '@/features/users/api/usersApi'
import QuotaCardsPanel from '@/features/quota/components/QuotaCardsPanel.vue'
import QuotaProgress from '@/features/quota/components/QuotaProgress.vue'
import { getSettings } from '@/features/settings/api/settingsApi'
import { resetQuotas, revokeQuotaCards } from '@/features/quota/api/quotaApi'
import { useConfirmDialog } from '@/shared/ui/confirm-dialog'
import { useI18n } from '@/shared/i18n'
import type { UserSummary } from '@/shared/types/api'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'
import { formatCompact, formatDateTime, formatInteger, formatUsd } from '@/shared/utils/format'

const message = toast
const confirm = useConfirmDialog()
const selectedUsers = ref<number[]>([])
const cardsVisible = ref(false)
const cardsUserId = ref<number | undefined>()
const quotaBusy = ref(false)
const allUsersSelected = computed(() => pagedUsers.value.length > 0 && pagedUsers.value.every(user => selectedUsers.value.includes(user.id)))
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSavingUser = ref(false)
const users = ref<UserSummary[]>([])
const editorVisible = ref(false)
const editingUserId = ref<number | null>(null)
const userAccount = ref('')
const userPassword = ref('')
const isUserAdmin = ref(false)
const userEnabled = ref(true)
const originalUserEnabled = ref(true)
const userNickname = ref('')
const quotaDailyEnabled = ref(true)
const quotaWeeklyEnabled = ref(true)
const quotaWeeklyUsd = ref(0)
const quotaDailyUsd = ref(0)
const page = ref(1)
const pageSize = ref(10)
const pageSizeOptions = [10, 20, 50, 100]
const isEditingFirstUser = computed(() => editingUserId.value === 1)

interface UserMetricCard {
  key: string
  label: string
  value: string
  footnote: string
  icon: Component
}

const userMetrics = computed<UserMetricCard[]>(() => {
  const activeUsers = users.value.filter((user) => user.disabled_at === null).length
  const adminUsers = users.value.filter((user) => user.is_admin).length
  const boundKeys = users.value.reduce((total, user) => total + user.key_count, 0)
  const todayCost = users.value.reduce((total, user) => total + user.today_estimated_cost_usd, 0)
  return [
    {
      key: 'active',
      label: t('启用用户', 'Active users'),
      value: formatInteger(activeUsers),
      footnote: t(`共 ${formatInteger(users.value.length)} 个账号`, `${formatInteger(users.value.length)} accounts total`),
      icon: UserRound,
    },
    {
      key: 'admins',
      label: t('管理员', 'Admins'),
      value: formatInteger(adminUsers),
      footnote: t('拥有管理权限', 'Have admin access'),
      icon: ShieldCheck,
    },
    {
      key: 'keys',
      label: t('绑定 Key', 'Bound keys'),
      value: formatInteger(boundKeys),
      footnote: t('当前用户集合', 'Current users'),
      icon: KeyRound,
    },
    {
      key: 'cost',
      label: t('今日费用', 'Today cost'),
      value: formatUsd(todayCost),
      footnote: t('按现价估算', 'Estimated at current prices'),
      icon: CircleDollarSign,
    },
  ]
})

function userLabel(row: UserSummary): string {
  return row.nickname.trim() || row.username.trim() || t('未知用户', 'Unknown user')
}

function quotaDetail(row: UserSummary): string | null {
  if (row.quota.paused) {
    return t('Key 已因余额暂停', 'Keys paused due to balance')
  }
  if (row.quota.sync_error) {
    return t('同步异常', 'Sync error')
  }
  return null
}

function todayRequestDetail(row: UserSummary): string {
  if (!row.today_records) {
    return t(`累计 ${formatInteger(row.records)}`, `${formatInteger(row.records)} total`)
  }
  const failed = row.today_failed_records
    ? t(`失败 ${formatInteger(row.today_failed_records)}`, `${formatInteger(row.today_failed_records)} failed`)
    : t('无失败', 'No failures')
  const rate = Math.round((row.today_success_records / row.today_records) * 100)
  return `${rate}% · ${failed}`
}

function todayCostDetail(row: UserSummary): string {
  if (!row.today_records) {
    return t('今日无请求', 'No requests today')
  }
  if (row.today_unpriced_records > 0) {
    return t(`未计价 ${formatInteger(row.today_unpriced_records)}`, `${formatInteger(row.today_unpriced_records)} unpriced`)
  }
  return t('已计价', 'Priced')
}

function lastModelLabel(row: UserSummary): string {
  return row.last_model ?? '-'
}

function lastProviderLabel(row: UserSummary): string {
  return row.last_provider ?? t('未知服务商', 'Unknown provider')
}

function normalizeQuotaInput(value: string | number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
}

function todayTokenDetail(row: UserSummary): string {
  return t(
    `入 ${formatCompact(row.today_input_tokens)} · 出 ${formatCompact(row.today_output_tokens)} · 缓 ${formatCompact(row.today_cached_tokens)}`,
    `In ${formatCompact(row.today_input_tokens)} · Out ${formatCompact(row.today_output_tokens)} · Cache ${formatCompact(row.today_cached_tokens)}`,
  )
}

function setQuotaWeeklyUsd(value: string | number) {
  quotaWeeklyUsd.value = normalizeQuotaInput(value)
}

function setQuotaDailyUsd(value: string | number) {
  quotaDailyUsd.value = normalizeQuotaInput(value)
}

const pagedUsers = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return users.value.slice(start, start + pageSize.value)
})

function handlePageChange(value: number) {
  page.value = value
}

function handlePageSizeChange(value: number) {
  pageSize.value = value
  page.value = 1
}

function resetEditor() {
  editingUserId.value = null
  userAccount.value = ''
  userPassword.value = ''
  isUserAdmin.value = false
  userEnabled.value = true
  originalUserEnabled.value = true
  userNickname.value = ''
  quotaDailyEnabled.value = true
  quotaWeeklyEnabled.value = true
  quotaWeeklyUsd.value = 0
  quotaDailyUsd.value = 0
}

async function openCreateUser() {
  resetEditor()
  userPassword.value = 'password'
  try {
    const settings = await getSettings()
    quotaDailyEnabled.value = !settings.new_user_quota_unlimited
    quotaWeeklyEnabled.value = !settings.new_user_quota_unlimited
    quotaDailyUsd.value = settings.new_user_quota_daily_usd
    quotaWeeklyUsd.value = settings.new_user_quota_weekly_usd
  } catch (error) {
    message.error(errorText(error, '加载默认配额失败', 'Failed to load default quotas'))
    return
  }
  editorVisible.value = true
}

function editUser(row: UserSummary) {
  editingUserId.value = row.id
  userAccount.value = row.username
  userPassword.value = ''
  isUserAdmin.value = row.id === 1 ? true : row.is_admin
  userEnabled.value = !isUserDisabled(row)
  originalUserEnabled.value = userEnabled.value
  userNickname.value = row.nickname
  quotaDailyEnabled.value = row.quota.daily_quota_usd !== null
  quotaWeeklyEnabled.value = row.quota.weekly_quota_usd !== null
  quotaWeeklyUsd.value = row.quota.weekly_quota_usd ?? 0
  quotaDailyUsd.value = row.quota.daily_quota_usd ?? 0
  editorVisible.value = true
}

async function refresh() {
  isLoading.value = true
  try {
    users.value = await listUsers()
    const lastPage = Math.max(1, Math.ceil(users.value.length / pageSize.value))
    page.value = Math.min(page.value, lastPage)
  } catch (error) {
    message.error(errorText(error, '加载用户列表失败', 'Failed to load users'))
  } finally {
    isLoading.value = false
  }
}

function isUserDisabled(row: UserSummary): boolean {
  return row.disabled_at !== null
}

async function saveUser() {
  const nickname = userNickname.value.trim()
  if (!nickname) {
    message.error(t('用户昵称不能为空', 'User nickname is required'))
    return
  }
  const username = userAccount.value.trim()
  if (!username) {
    message.error(t('账号不能为空', 'Account is required'))
    return
  }
  const isEditing = editingUserId.value !== null
  const password = userPassword.value.trim()
  if (!isEditing && !password) {
    message.error(t('密码不能为空', 'Password is required'))
    return
  }
  isSavingUser.value = true
  try {
    const payload = {
      username,
      password: password || undefined,
      is_admin: isEditingFirstUser.value ? true : isUserAdmin.value,
      nickname,
    }
    const saved =
      editingUserId.value !== null
        ? await updateUser(editingUserId.value, payload)
        : await createUser(payload)
    await updateUserQuota(saved.id, {
      weekly_quota_usd: quotaWeeklyEnabled.value ? quotaWeeklyUsd.value : null,
      daily_quota_usd: quotaDailyEnabled.value ? quotaDailyUsd.value : null,
    })
    if (isEditing && !isEditingFirstUser.value && userEnabled.value !== originalUserEnabled.value) {
      if (userEnabled.value) await enableUser(saved.id)
      else await disableUser(saved.id)
    }
    message.success(isEditing ? t('用户已保存', 'User saved') : t('用户已创建', 'User created'))
    editorVisible.value = false
    resetEditor()
    await refresh()
  } catch (error) {
    message.error(errorText(error, '保存用户失败', 'Failed to save user'))
  } finally {
    isSavingUser.value = false
  }
}

function selectUser(id: number, checked: boolean | 'indeterminate') {
  selectedUsers.value = checked === true ? [...new Set([...selectedUsers.value, id])] : selectedUsers.value.filter(value => value !== id)
}

function selectPage(checked: boolean | 'indeterminate') {
  const ids = pagedUsers.value.map(user => user.id)
  selectedUsers.value = checked === true ? [...new Set([...selectedUsers.value, ...ids])] : selectedUsers.value.filter(id => !ids.includes(id))
}

function manageCards(userId?: number) { cardsUserId.value = userId; cardsVisible.value = true }

function resetQuota(allUsers = false, userId?: number) {
  const ids = userId ? [userId] : selectedUsers.value
  if (!allUsers && !ids.length) return
  confirm.warning({
    title: allUsers ? t('全站重置额度', 'Reset all quotas') : t('重置用户额度', 'Reset user quotas'),
    content: t('清零所选用户的日配额和周配额已用量，原周期和额度卡保持不变。', 'Clear daily and weekly quota usage for the selected users, preserving period boundaries and cards.'),
    positiveText: t('重置', 'Reset'),
    onPositiveClick: async () => {
      quotaBusy.value = true
      try { await resetQuotas(allUsers ? { all_users: true } : { user_ids: ids }); message.success(t('额度已重置', 'Quotas reset')); await refresh() }
      catch (error) { message.error(errorText(error, '重置额度失败', 'Failed to reset quotas')) }
      finally { quotaBusy.value = false }
    },
  })
}

function revokeUsersCards() {
  confirm.warning({
    title: t('吊销所选用户的额度卡', 'Revoke credit cards for selected users'),
    content: t('这些用户的所有额度卡将立即失效，已扣款记录会保留。', 'All credit cards for these users will be revoked immediately. Past deductions are retained.'),
    positiveText: t('吊销', 'Revoke'),
    onPositiveClick: async () => {
      quotaBusy.value = true
      try { await revokeQuotaCards({ user_ids: selectedUsers.value, kind: 'credit' }); message.success(t('额度卡已吊销', 'Credit cards revoked')); await refresh() }
      catch (error) { message.error(errorText(error, '吊销失败', 'Failed to revoke cards')) }
      finally { quotaBusy.value = false }
    },
  })
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('用户管理', 'User Management') }}</h1>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <Button variant="outline" :disabled="isLoading" @click="refresh">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCw v-else data-icon="inline-start" />
          {{ t('刷新', 'Refresh') }}
        </Button>
        <Button variant="outline" :disabled="quotaBusy" @click="resetQuota(true)"><RotateCcw data-icon="inline-start" />{{ t('全站重置额度', 'Reset all quotas') }}</Button>
        <Button variant="outline" @click="manageCards()"><WalletCards data-icon="inline-start" />{{ t('配额卡管理', 'Quota cards') }}</Button>
        <Button @click="openCreateUser">
          <Plus data-icon="inline-start" />
          {{ t('增加用户', 'Add user') }}
        </Button>
      </div>
    </div>

    <div class="metric-grid user-metrics">
      <Card v-for="metric in userMetrics" :key="metric.key" class="user-metric-card">
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

    <section class="panel table-panel user-table-panel">
      <div class="flex flex-wrap items-center gap-2 p-4">
        <span class="mr-auto text-sm text-muted-foreground">{{ t(`已选择 ${selectedUsers.length} 位用户`, `${selectedUsers.length} users selected`) }}</span>
        <Button variant="outline" :disabled="!selectedUsers.length || quotaBusy" @click="manageCards()"><WalletCards data-icon="inline-start" />{{ t('发放卡片', 'Issue cards') }}</Button>
        <Button variant="outline" :disabled="!selectedUsers.length || quotaBusy" @click="revokeUsersCards"><Ban data-icon="inline-start" />{{ t('吊销额度卡', 'Revoke credit cards') }}</Button>
        <Button variant="outline" :disabled="!selectedUsers.length || quotaBusy" @click="resetQuota()"><RotateCcw data-icon="inline-start" />{{ t('重置额度', 'Reset quotas') }}</Button>
      </div>
      <div class="user-table">
        <Table class="min-w-[1000px] table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead class="w-[4%]"><Checkbox :model-value="allUsersSelected" :aria-label="t('选择本页用户', 'Select users on this page')" @update:model-value="selectPage" /></TableHead>
              <TableHead class="w-[17%]">{{ t('用户', 'User') }}</TableHead>
              <TableHead class="w-[10%]">{{ t('角色 / 状态', 'Role / status') }}</TableHead>
              <TableHead class="w-[14%]">{{ t('配额 / 额度卡', 'Quotas / Cards') }}</TableHead>
              <TableHead class="w-[6%]">{{ t('密钥', 'Keys') }}</TableHead>
              <TableHead class="w-[9%]">{{ t('今日请求', 'Today requests') }}</TableHead>
              <TableHead class="w-[13%]">{{ t('今日 Token', 'Today tokens') }}</TableHead>
              <TableHead class="w-[9%]">{{ t('今日费用', 'Today cost') }}</TableHead>
              <TableHead class="w-[10%]">{{ t('最近使用', 'Last used') }}</TableHead>
              <TableHead class="w-[8%]">
                <span class="sr-only">{{ t('操作', 'Actions') }}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <template v-if="isLoading && users.length === 0">
              <TableRow v-for="rowIndex in 8" :key="`user-skeleton-${rowIndex}`">
                <TableCell v-for="columnIndex in 10" :key="columnIndex">
                  <Skeleton class="h-4 w-full" />
                </TableCell>
              </TableRow>
            </template>

            <TableEmpty v-else-if="users.length === 0" :colspan="10">
              {{ t('暂无用户', 'No users') }}
            </TableEmpty>

            <TableRow v-for="row in pagedUsers" v-else :key="row.id">
              <TableCell><Checkbox :model-value="selectedUsers.includes(row.id)" :aria-label="t(`选择 ${userLabel(row)}`, `Select ${userLabel(row)}`)" @update:model-value="selectUser(row.id, $event)" /></TableCell>
              <TableCell>
                <div class="metric-stack overflow-hidden">
                  <span class="metric-primary truncate" :title="userLabel(row)">{{ userLabel(row) }}</span>
                  <span class="metric-muted" :title="row.username">{{ row.username }}</span>
                </div>
              </TableCell>
              <TableCell>
                <div class="metric-stack is-compact">
                  <Badge :variant="row.is_admin ? 'default' : 'secondary'">
                    {{ row.is_admin ? t('管理员', 'Admin') : t('普通用户', 'Standard user') }}
                  </Badge>
                  <Badge :variant="isUserDisabled(row) ? 'destructive' : 'outline'">
                    {{ isUserDisabled(row) ? t('已禁用', 'Disabled') : t('启用中', 'Enabled') }}
                  </Badge>
                </div>
              </TableCell>
              <TableCell>
                <div class="flex min-w-0 flex-col gap-2">
                  <QuotaProgress compact :label="t('日配额', 'Daily quota')" :remaining="row.quota.daily_remaining_usd" :total="row.quota.daily_quota_usd" :unlimited="row.quota.daily_quota_usd === null" />
                  <QuotaProgress compact :label="t('周配额', 'Weekly quota')" :remaining="row.quota.weekly_remaining_usd" :total="row.quota.weekly_quota_usd" :unlimited="row.quota.weekly_quota_usd === null" />
                  <QuotaProgress compact :label="t('额度卡', 'Cards')" :remaining="row.quota.cards_remaining_usd" :total="row.quota.cards_total_usd" />
                  <span
                    v-if="quotaDetail(row)"
                    class="metric-muted quota-balance-detail"
                    :class="{ 'is-error': row.quota.sync_error || row.quota.paused }"
                  >
                    {{ quotaDetail(row) }}
                  </span>
                </div>
              </TableCell>
              <TableCell>{{ t(`${formatInteger(row.key_count)} 个`, `${formatInteger(row.key_count)} keys`) }}</TableCell>
              <TableCell>
                <div class="metric-stack">
                  <span class="metric-primary">{{ formatInteger(row.today_records) }}</span>
                  <span class="metric-muted">{{ todayRequestDetail(row) }}</span>
                </div>
              </TableCell>
              <TableCell>
                <div class="metric-stack">
                  <span class="metric-primary">{{ formatCompact(row.today_total_tokens) }}</span>
                  <span class="metric-muted" :title="todayTokenDetail(row)">{{ todayTokenDetail(row) }}</span>
                </div>
              </TableCell>
              <TableCell>
                <div class="metric-stack">
                  <span class="metric-primary">{{ formatUsd(row.today_estimated_cost_usd) }}</span>
                  <span class="metric-muted" :class="{ 'is-error': row.today_unpriced_records > 0 }">
                    {{ todayCostDetail(row) }}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <div class="metric-stack">
                  <span class="model-value" :title="lastModelLabel(row)">{{ lastModelLabel(row) }}</span>
                  <span class="metric-muted">{{ lastProviderLabel(row) }}</span>
                  <span class="metric-muted">{{ formatDateTime(row.last_seen_at) }}</span>
                </div>
              </TableCell>
              <TableCell class="text-right">
                <Button variant="ghost" size="icon-sm" :aria-label="t(`管理 ${userLabel(row)} 的额度卡`, `Manage cards for ${userLabel(row)}`)" @click="manageCards(row.id)"><WalletCards /></Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="row-actions-trigger"
                  :aria-label="t(`编辑 ${userLabel(row)}`, `Edit ${userLabel(row)}`)"
                  :title="t('编辑', 'Edit')"
                  @click="editUser(row)"
                >
                  <Pencil />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>

      <TablePaginationFooter
        :page="page"
        :page-size="pageSize"
        :page-size-options="pageSizeOptions"
        :total="users.length"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      />
    </section>

    <Dialog v-model:open="editorVisible">
      <DialogContent
        :show-close-button="false"
        class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl"
      >
        <form class="flex flex-col gap-5" @submit.prevent="saveUser">
          <DialogHeader>
            <DialogTitle>
              {{ editingUserId ? t('编辑用户', 'Edit user') : t('增加用户', 'Add user') }}
            </DialogTitle>
            <DialogDescription>
              {{ t('配置登录身份、管理权限和分层余额。', 'Configure sign-in identity, administrator access, and balance buckets.') }}
            </DialogDescription>
          </DialogHeader>

          <Alert v-if="editingUserId === null" class="user-editor-warning">
            <AlertTriangle />
            <AlertDescription>
              {{ t('账号一旦创建，不允许删除，只允许禁用，请谨慎操作。', 'Accounts cannot be deleted after creation. They can only be disabled, so proceed carefully.') }}
            </AlertDescription>
          </Alert>

          <FieldGroup class="user-editor-form">
            <FieldGroup class="identity-fields">
              <Field>
                <FieldLabel for="user-nickname">{{ t('用户昵称', 'User nickname') }}</FieldLabel>
                <Input
                  id="user-nickname"
                  v-model="userNickname"
                  required
                  :placeholder="t('例如：研发用户', 'Example: Engineering user')"
                />
              </Field>
              <Field :data-disabled="editingUserId !== null || undefined">
                <FieldLabel for="user-account">{{ t('账号', 'Account') }}</FieldLabel>
                <Input
                  id="user-account"
                  v-model="userAccount"
                  required
                  autocomplete="username"
                  :disabled="editingUserId !== null"
                  :placeholder="t('例如：user001', 'Example: user001')"
                />
              </Field>
              <Field class="password-field">
                <FieldLabel for="user-password">{{ t('密码', 'Password') }}</FieldLabel>
                <Input
                  id="user-password"
                  v-model="userPassword"
                  type="password"
                  :required="editingUserId === null"
                  autocomplete="new-password"
                  :placeholder="editingUserId ? t('留空不修改密码', 'Leave blank to keep the current password') : t('请输入登录密码', 'Enter a sign-in password')"
                />
                <FieldDescription v-if="editingUserId !== null">
                  {{ t('留空时保留当前密码。', 'Leave blank to keep the current password.') }}
                </FieldDescription>
              </Field>
            </FieldGroup>

            <Field orientation="horizontal" class="switch-setting" :data-disabled="isEditingFirstUser || undefined">
              <FieldContent>
                <FieldLabel for="user-admin">{{ t('管理员权限', 'Administrator access') }}</FieldLabel>
                <FieldDescription>
                  {{ t('管理员可以管理用户、价格、上游和系统设置。', 'Administrators can manage users, prices, upstreams, and system settings.') }}
                </FieldDescription>
              </FieldContent>
              <Switch id="user-admin" v-model="isUserAdmin" :disabled="isEditingFirstUser" />
            </Field>

            <Field v-if="editingUserId !== null && !isEditingFirstUser" orientation="horizontal" class="switch-setting">
              <FieldContent>
                <FieldLabel for="user-enabled">{{ t('启用账号', 'Account enabled') }}</FieldLabel>
                <FieldDescription>
                  {{ t('关闭并保存后将禁用账号，并从 CPA 移除其 API KEY。', 'Turning this off disables the account and removes its API keys from CPA when saved.') }}
                </FieldDescription>
              </FieldContent>
              <Switch id="user-enabled" v-model="userEnabled" />
            </Field>

            <FieldSet class="quota-fieldset">
              <FieldLegend>{{ t('配额设置', 'Quota settings') }}</FieldLegend>
              <FieldDescription>
                {{ t('关闭开关表示不限制该周期；开启且设为 0 表示该周期没有基础配额，仍可使用额度卡。', 'Turn a period off to remove its limit. An enabled zero limit has no base quota; credit cards remain usable.') }}
              </FieldDescription>
              <FieldGroup>
                <FieldGroup class="quota-editor-grid">
                  <FieldGroup>
                    <Field orientation="horizontal" class="switch-setting">
                      <FieldLabel for="quota-daily-enabled">{{ t('启用日配额', 'Enable daily quota') }}</FieldLabel>
                      <Switch id="quota-daily-enabled" v-model="quotaDailyEnabled" />
                    </Field>
                    <Field :data-disabled="!quotaDailyEnabled || undefined">
                      <FieldLabel for="quota-daily">{{ t('日配额 USD', 'Daily quota USD') }}</FieldLabel>
                      <Input
                        id="quota-daily"
                        type="number"
                        min="0"
                        step="0.00000001"
                        :model-value="quotaDailyUsd"
                        :disabled="!quotaDailyEnabled"
                        @update:model-value="setQuotaDailyUsd"
                      />
                    </Field>
                  </FieldGroup>
                  <FieldGroup>
                    <Field orientation="horizontal" class="switch-setting">
                      <FieldLabel for="quota-weekly-enabled">{{ t('启用周配额', 'Enable weekly quota') }}</FieldLabel>
                      <Switch id="quota-weekly-enabled" v-model="quotaWeeklyEnabled" />
                    </Field>
                    <Field :data-disabled="!quotaWeeklyEnabled || undefined">
                      <FieldLabel for="quota-weekly">{{ t('周配额 USD', 'Weekly quota USD') }}</FieldLabel>
                      <Input
                        id="quota-weekly"
                        type="number"
                        min="0"
                        step="0.00000001"
                        :model-value="quotaWeeklyUsd"
                        :disabled="!quotaWeeklyEnabled"
                        @update:model-value="setQuotaWeeklyUsd"
                      />
                    </Field>
                  </FieldGroup>
                </FieldGroup>
                <Alert v-if="!quotaDailyEnabled && !quotaWeeklyEnabled">
                  <AlertDescription>{{ t('日配额与周配额均已关闭，此用户不限制用量且不消耗额度卡。', 'Both quota limits are off. Usage is unlimited and does not consume credit cards.') }}</AlertDescription>
                </Alert>
              </FieldGroup>
            </FieldSet>
          </FieldGroup>

          <div v-if="editingUserId" class="flex flex-wrap gap-2">
            <Button type="button" variant="outline" :disabled="quotaBusy" @click="resetQuota(false, editingUserId)"><RotateCcw data-icon="inline-start" />{{ t('重置额度', 'Reset quotas') }}</Button>
            <Button type="button" variant="outline" @click="editorVisible = false; manageCards(editingUserId)"><WalletCards data-icon="inline-start" />{{ t('管理额度卡', 'Manage credit cards') }}</Button>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" :disabled="isSavingUser" @click="editorVisible = false">
              {{ t('取消', 'Cancel') }}
            </Button>
            <Button type="submit" :disabled="isSavingUser">
              <Spinner v-if="isSavingUser" data-icon="inline-start" />
              {{ editingUserId ? t('保存', 'Save') : t('创建', 'Create') }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <Dialog v-model:open="cardsVisible">
      <DialogContent class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-6xl">
        <DialogHeader><DialogTitle>{{ t('配额卡管理', 'Quota cards') }}</DialogTitle><DialogDescription>{{ cardsUserId ? userLabel(users.find(user => user.id === cardsUserId)!) : t('支持单人和批量发放、编辑及吊销额度卡。', 'Issue, edit and revoke cards for individual users or in bulk.') }}</DialogDescription></DialogHeader>
        <QuotaCardsPanel v-if="cardsVisible" admin :user-id="cardsUserId" :target-user-ids="selectedUsers" :users="users" @changed="refresh" />
      </DialogContent>
    </Dialog>
  </section>
</template>

<style scoped>
.user-metrics {
  grid-template-columns: repeat(4, minmax(150px, 1fr));
}

.user-metric-card {
  min-width: 0;
}

.user-metric-card :deep([data-slot="card-header"]) {
  padding-bottom: 10px;
}

.user-metric-card :deep([data-slot="card-content"]) {
  padding-top: 0;
}

.user-table-panel {
  min-width: 0;
  overflow: hidden;
}

.user-table {
  min-width: 0;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius) var(--radius) 0 0;
  background: var(--card);
}

.identity-fields,
.quota-editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.password-field {
  grid-column: 1 / -1;
}

.switch-setting {
  gap: 20px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: color-mix(in oklch, var(--muted) 45%, transparent);
}

.quota-fieldset {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}

.row-actions-trigger {
  margin-left: auto;
  color: var(--muted-foreground);
}

.metric-stack {
  display: grid;
  gap: 3px;
  min-width: 0;
  line-height: 1.28;
}

.metric-stack.is-compact {
  align-items: start;
}

.metric-primary {
  font-weight: 600;
}

.metric-muted {
  min-width: 0;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.metric-muted.is-error {
  color: var(--destructive);
}

.model-value {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 900px) {
  .user-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .quota-editor-grid,
  .identity-fields {
    grid-template-columns: 1fr;
  }

  .password-field {
    grid-column: auto;
  }

}
</style>
