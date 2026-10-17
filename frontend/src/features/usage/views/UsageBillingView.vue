<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { CircleCheck, Cpu, Download, RefreshCw, Route, Server, UserRound } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldGroup } from '@/components/ui/field'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { exportUsageBilling, getUsageBilling, getUsageOptions } from '@/features/usage/api/usageApi'
import { useI18n } from '@/shared/i18n'
import type { UsageBillingItem, UsageBillingResponse, UsageFilters, UsageOptionsResponse } from '@/shared/types/api'
import DateTimeRangePicker from '@/shared/ui/DateTimeRangePicker.vue'
import FilterCombobox from '@/shared/ui/FilterCombobox.vue'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'
import { beijingDayRange, formatCompact, formatDateTime, formatInteger, formatLocalDateTimeParam, formatUsd } from '@/shared/utils/format'

type QuickRange = 'today' | 'yesterday' | 'last24h' | 'last3d' | 'last7d' | 'last30d'
const route = useRoute()
const router = useRouter()
const { t, errorText, language } = useI18n()
const response = ref<UsageBillingResponse | null>(null)
const options = ref<UsageOptionsResponse | null>(null)
const error = ref('')
const optionsError = ref('')
const loading = ref(false)
const exporting = ref(false)
const page = ref(1)
const pageSize = ref(50)
const dateRange = ref<[number, number]>(beijingDayRange())
const loadedFilters = ref<UsageFilters>({})
const refreshedAt = ref<number | null>(null)
let sequence = 0
let timer: number | undefined

const quickRanges = computed(() => [
  { key: 'today', label: t('今日', 'Today') },
  { key: 'yesterday', label: t('昨日', 'Yesterday') },
  { key: 'last24h', label: t('近 24 小时', 'Last 24 hours') },
  { key: 'last3d', label: t('近 3 日', 'Last 3 days') },
  { key: 'last7d', label: t('近 7 日', 'Last 7 days') },
  { key: 'last30d', label: t('近 30 日', 'Last 30 days') },
])

function queryValue(key: string): string | null {
  const value = route.query[key]
  return typeof value === 'string' && value ? value : null
}

const activeQuickRange = computed<QuickRange | null>(() => {
  const value = queryValue('quick_range')
  if (quickRanges.value.some(item => item.key === value)) return value as QuickRange
  return queryValue('start') && queryValue('end') ? null : 'today'
})

function buildRange(): [number, number] {
  const quick = activeQuickRange.value
  if (quick === 'today') return beijingDayRange()
  if (quick === 'yesterday') return beijingDayRange(-1)
  if (quick) {
    const days = { last24h: 1, last3d: 3, last7d: 7, last30d: 30 }[quick]
    const end = Date.now()
    return [end - days * 86_400_000, end]
  }
  // Query parameters without an offset follow the API's Beijing-time convention.
  const parse = (value: string) => new Date(/(?:Z|[+-]\d\d:\d\d)$/i.test(value) ? value : `${value}+08:00`).getTime()
  return [parse(queryValue('start')!), parse(queryValue('end')!)]
}

async function setFilter(key: string, value: string | number | null) {
  await router.replace({ query: { ...route.query, [key]: value === null ? undefined : String(value) } })
}

function setQuickRange(value: unknown) {
  if (typeof value !== 'string' || !quickRanges.value.some(item => item.key === value)) return
  void router.replace({ query: { ...route.query, quick_range: value, start: undefined, end: undefined } })
}

function setCustomRange(value: [number, number] | null) {
  if (!value) return
  void router.replace({ query: {
    ...route.query, quick_range: undefined,
    start: formatLocalDateTimeParam(value[0]), end: formatLocalDateTimeParam(value[1]),
  } })
}

const fields = computed(() => [
  { key: 'user_id', label: t('用户昵称', 'User nickname'), icon: UserRound,
    value: queryValue('user_id') === null ? null : Number(queryValue('user_id')),
    options: (response.value?.users ?? []).filter(user => user.user_id !== null).map(user => ({
      value: user.user_id!, label: user.label === user.username ? user.label : `${user.label} (${user.username})`,
    })) },
  { key: 'provider', label: t('服务商', 'Provider'), icon: Server, value: queryValue('provider'),
    options: (options.value?.providers ?? []).map(value => ({ value, label: value })) },
  { key: 'model', label: t('模型', 'Model'), icon: Cpu, value: queryValue('model'),
    options: (options.value?.models ?? []).map(value => ({ value, label: value })) },
  { key: 'endpoint', label: t('接口', 'Endpoint'), icon: Route, value: queryValue('endpoint'),
    options: (options.value?.endpoints ?? []).map(value => ({ value, label: value })) },
  { key: 'failed', label: t('结果', 'Result'), icon: CircleCheck, value: queryValue('failed'),
    options: [{ value: 'false', label: t('成功', 'Success') }, { value: 'true', label: t('失败', 'Failed') }] },
])

async function refresh(silent = false) {
  if (silent && loading.value) return
  const request = ++sequence
  loading.value = true
  error.value = ''
  try {
    const range = buildRange()
    if (!range.every(Number.isFinite) || range[0] >= range[1]) {
      throw new Error(t('请选择有效的起止时间', 'Choose a valid start and end time'))
    }
    dateRange.value = range
    const filters: UsageFilters = {
      scope: 'admin', start: formatLocalDateTimeParam(range[0]), end: formatLocalDateTimeParam(range[1]),
      user_id: queryValue('user_id') === null ? undefined : Number(queryValue('user_id')),
      provider: queryValue('provider') ?? undefined, model: queryValue('model') ?? undefined,
      endpoint: queryValue('endpoint') ?? undefined,
      failed: queryValue('failed') === null ? undefined : queryValue('failed') === 'true',
    }
    const [bill, nextOptions] = await Promise.all([
      getUsageBilling(filters),
      silent ? Promise.resolve(null) : getUsageOptions(filters).then(value => ({ value, error: '' })).catch(cause => ({
        value: null, error: errorText(cause, '筛选选项加载失败', 'Failed to load filter options'),
      })),
    ])
    if (request !== sequence) return
    response.value = bill
    loadedFilters.value = filters
    refreshedAt.value = Date.now()
    page.value = Math.min(page.value, Math.max(1, Math.ceil(bill.items.length / pageSize.value)))
    if (nextOptions) {
      if (nextOptions.value) options.value = nextOptions.value
      optionsError.value = nextOptions.error
    }
  } catch (cause) {
    if (request !== sequence) return
    error.value = errorText(cause, '费用核算加载失败', 'Failed to load cost accounting')
    response.value = null
  } finally {
    if (request === sequence) loading.value = false
  }
}

const rows = computed(() => response.value?.items.slice((page.value - 1) * pageSize.value, page.value * pageSize.value) ?? [])
const metrics = computed(() => [
  { label: t('费用合计', 'Total cost'), value: formatUsd(response.value?.summary.cost_usd ?? 0) },
  { label: t('用户数', 'Users'), value: formatInteger(response.value?.items.length ?? 0) },
  { label: t('请求数', 'Requests'), value: formatInteger(response.value?.summary.records ?? 0) },
  { label: t('总 Token', 'Total tokens'), value: formatCompact(response.value?.summary.total_tokens ?? 0) },
])
function costShare(item: UsageBillingItem) {
  const total = response.value?.summary.cost_usd ?? 0
  return total > 0 ? Math.min(100, item.cost_usd / total * 100) : 0
}
function openRecords(item: UsageBillingItem) {
  if (item.user_id === null) return
  const query: Record<string, string> = { user_id: String(item.user_id) }
  for (const [key, value] of Object.entries(loadedFilters.value)) {
    if (key !== 'scope' && key !== 'user_id' && value !== undefined) query[key] = String(value)
  }
  void router.push({ path: '/admin/records', query })
}
function changePageSize(value: number) { pageSize.value = value; page.value = 1 }
async function exportWorkbook() {
  if (!response.value || loading.value || exporting.value) return
  const filters = { ...loadedFilters.value }
  const filenameTime = (value: string) => formatDateTime(value).replace(/:/g, '-').replace(/ /g, '_')
  const filename = `${t('费用核算', 'Cost-Accounting')}_${filenameTime(response.value.start)}_${filenameTime(response.value.end)}.xlsx`
  exporting.value = true
  try {
    const blob = await exportUsageBilling(filters, language.value)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.append(link)
    link.click()
    link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    toast.success(t('费用核算表格已导出', 'Cost accounting spreadsheet exported'))
  } catch (cause) {
    toast.error(errorText(cause, '费用核算导出失败', 'Failed to export cost accounting'))
  } finally {
    exporting.value = false
  }
}
watch(() => route.query, () => { page.value = 1; void refresh() }, { immediate: true })
onMounted(() => {
  timer = window.setInterval(() => { if (!document.hidden) void refresh(true) }, 10_000)
})
onBeforeUnmount(() => { sequence++; window.clearInterval(timer) })
</script>

<template>
  <section class="page billing-page" :aria-busy="loading">
    <div class="page-toolbar">
      <h1 class="page-title" data-page-title>{{ t('费用核算', 'Cost Accounting') }}</h1>
      <div class="flex flex-wrap items-center gap-3">
        <span v-if="refreshedAt" class="text-sm text-muted-foreground">{{ t('最近更新', 'Updated') }} {{ formatDateTime(refreshedAt) }}</span>
        <Button variant="outline" :disabled="loading" @click="refresh()">
          <Spinner v-if="loading" data-icon="inline-start" /><RefreshCw v-else data-icon="inline-start" />{{ t('刷新', 'Refresh') }}
        </Button>
        <Button variant="outline" :disabled="!response || loading || exporting" @click="exportWorkbook">
          <Spinner v-if="exporting" data-icon="inline-start" /><Download v-else data-icon="inline-start" />{{ t('导出 Excel', 'Export Excel') }}
        </Button>
      </div>
    </div>

    <Card size="sm">
      <CardContent class="flex flex-col gap-3">
        <div class="billing-time-row">
          <Tabs :model-value="activeQuickRange ?? undefined" @update:model-value="setQuickRange">
            <TabsList class="grid h-auto w-full grid-cols-3 lg:grid-cols-6" :aria-label="t('快捷时间范围', 'Quick time ranges')">
              <TabsTrigger v-for="item in quickRanges" :key="item.key" :value="item.key">{{ item.label }}</TabsTrigger>
            </TabsList>
          </Tabs>
          <DateTimeRangePicker :model-value="dateRange" @update:model-value="setCustomRange" />
        </div>
        <FieldGroup class="billing-filter-row">
          <Field v-for="field in fields" :key="field.key">
            <FilterCombobox
              :model-value="field.value" :options="field.options" :placeholder="field.label"
              :icon="field.icon" @update:model-value="setFilter(field.key, $event)"
            />
          </Field>
        </FieldGroup>
      </CardContent>
    </Card>

    <Alert v-if="error" variant="destructive" class="border-border"><AlertTitle>{{ t('加载失败', 'Loading failed') }}</AlertTitle><AlertDescription>{{ error }}</AlertDescription></Alert>
    <Alert v-if="optionsError" class="border-border"><AlertTitle>{{ t('筛选选项加载失败', 'Failed to load filter options') }}</AlertTitle><AlertDescription>{{ optionsError }}</AlertDescription></Alert>
    <div class="grid grid-cols-2 gap-4 xl:grid-cols-4">
      <Card v-for="metric in metrics" :key="metric.label" size="sm">
        <CardHeader><CardTitle>{{ metric.label }}</CardTitle></CardHeader>
        <CardContent><Skeleton v-if="!response && loading" class="h-8 w-28" /><div v-else class="text-2xl font-semibold tabular-nums">{{ response ? metric.value : '—' }}</div></CardContent>
      </Card>
    </div>
    <Alert v-if="response && response.summary.unpriced_records > 0" class="border-border">
      <AlertTitle>{{ t('存在未定价用量', 'Unpriced usage exists') }}</AlertTitle>
      <AlertDescription>{{ t(`有 ${response.summary.unpriced_records} 条请求未定价，费用合计不包含这些请求。`, `${response.summary.unpriced_records} requests are unpriced and excluded from the cost total.`) }}</AlertDescription>
    </Alert>

    <Card class="min-w-0">
      <CardHeader class="flex flex-row flex-wrap items-center justify-between gap-3">
        <CardTitle>{{ t('用户费用', 'Costs by user') }}</CardTitle>
        <span v-if="response" class="text-sm text-muted-foreground">{{ t(`有用量用户 ${response.active_users} 位`, `${response.active_users} users with usage`) }}</span>
      </CardHeader>
      <CardContent class="min-w-0">
        <div class="billing-table overflow-hidden rounded-lg border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t('用户', 'User') }}</TableHead>
                <TableHead class="text-right">{{ t('请求数', 'Requests') }}</TableHead>
                <TableHead class="text-right">{{ t('总 Token', 'Total tokens') }}</TableHead>
                <TableHead class="text-right">{{ t('费用 USD', 'Cost USD') }}</TableHead>
                <TableHead class="w-44">{{ t('费用占比', 'Cost share') }}</TableHead>
                <TableHead v-if="response && response.summary.unpriced_records > 0" class="text-right">{{ t('未定价', 'Unpriced') }}</TableHead>
                <TableHead class="w-16"><span class="sr-only">{{ t('操作', 'Actions') }}</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template v-if="!response && loading">
                <TableRow v-for="index in 6" :key="index"><TableCell v-for="cell in 6" :key="cell"><Skeleton class="h-6 w-full" /></TableCell></TableRow>
              </template>
              <TableRow v-for="item in rows" :key="item.username" data-billing-row>
                <TableCell>
                  <div class="flex flex-col gap-1">
                    <span>{{ item.label || t('未绑定用户', 'Unattributed user') }}</span>
                    <span v-if="item.label && item.label !== item.username" class="text-xs text-muted-foreground">{{ item.username }}</span>
                    <Badge v-if="item.user_id === null" variant="outline">{{ t('未绑定', 'Unlinked') }}</Badge>
                  </div>
                </TableCell>
                <TableCell class="text-right tabular-nums">{{ formatInteger(item.records) }}</TableCell>
                <TableCell class="text-right tabular-nums" :title="formatInteger(item.total_tokens)">{{ formatCompact(item.total_tokens) }}</TableCell>
                <TableCell class="text-right tabular-nums">{{ formatUsd(item.cost_usd) }}</TableCell>
                <TableCell><div class="flex min-w-28 items-center gap-2"><Progress :model-value="costShare(item)" :aria-label="t('费用占比', 'Cost share')" /><span class="text-xs tabular-nums">{{ costShare(item).toFixed(1) }}%</span></div></TableCell>
                <TableCell v-if="response && response.summary.unpriced_records > 0" class="text-right tabular-nums">{{ formatInteger(item.unpriced_records) }}</TableCell>
                <TableCell class="text-right"><Button v-if="item.user_id !== null" variant="ghost" size="sm" @click="openRecords(item)">{{ t('明细', 'Records') }}</Button></TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <Empty v-if="!loading && response && !rows.length"><EmptyHeader><EmptyTitle>{{ t('没有匹配的用户', 'No matching users') }}</EmptyTitle></EmptyHeader></Empty>
          <TablePaginationFooter v-if="response" v-model:page="page" :page-size="pageSize" :total="response.items.length" @update:page-size="changePageSize" />
        </div>
      </CardContent>
    </Card>
  </section>
</template>

<style scoped>
.billing-page { gap: 16px; }
.billing-time-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(460px, 520px); gap: 10px; align-items: center; }
.billing-filter-row { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 8px; }
.billing-time-row > *, .billing-filter-row > * { min-width: 0; }
.billing-table :deep([data-slot="table-container"]) { max-height: 52vh; overflow: auto; }
.billing-table :deep([data-slot="table-header"]) { position: sticky; top: 0; z-index: 1; background: var(--background); }
@media (max-width: 1200px) { .billing-time-row { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 760px) { .billing-filter-row { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
