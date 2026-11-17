<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
  TableEmpty,
} from '@/components/ui/table'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'
import { useI18n } from '@/shared/i18n'
import { formatDateTime, formatUsd } from '@/shared/utils/format'
import type { QuotaReset, UserQuotaStatus } from '@/shared/types/api'
import { getCurrentUserQuota } from '@/features/users/api/usersApi'
import { listQuotaResets } from '../api/quotaApi'
import QuotaCardsPanel from '../components/QuotaCardsPanel.vue'
import QuotaProgress from '../components/QuotaProgress.vue'

const { t, errorText } = useI18n()
const quota = ref<UserQuotaStatus | null>(null)
const loading = ref(true)
const cardsPanel = ref<InstanceType<typeof QuotaCardsPanel> | null>(null)
const resets = ref<QuotaReset[]>([])
const resetPage = ref(1)
const resetTotal = ref(0)
const metrics = computed(() => [
  {
    label: t('可用余额', 'Available balance'),
    value: quota.value?.available_usd,
    detail: t('当前可用配额与有效额度卡之和', 'Available base quota plus active card credit'),
  },
  {
    label: t('额度卡余额', 'Card balance'),
    value: quota.value?.cards_remaining_usd,
    detail: t('按到期时间依次抵扣', 'Deducted by earliest expiration'),
    card: true,
  },
])

async function loadResets() {
  const result = await listQuotaResets(resetPage.value)
  resets.value = result.items
  resetTotal.value = result.total
}
async function load() {
  loading.value = true
  try {
    await Promise.all([
      getCurrentUserQuota().then((value) => {
        quota.value = value
      }),
      loadResets(),
    ])
  } catch (error) {
    toast.error(errorText(error, '加载配额失败', 'Failed to load quota'))
  } finally {
    loading.value = false
  }
}
async function refresh() {
  await Promise.all([load(), cardsPanel.value?.refresh()])
}
watch(resetPage, () => {
  void loadResets().catch((error) =>
    toast.error(errorText(error, '加载重置记录失败', 'Failed to load resets')),
  )
})
onMounted(load)
</script>

<template>
  <section class="page">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('我的配额', 'My Quota') }}</h1>
      <Button variant="outline" :disabled="loading" @click="refresh">
        <RefreshCw data-icon="inline-start" />{{ t('刷新', 'Refresh') }}
      </Button>
    </div>
    <Alert v-if="quota?.paused" variant="destructive">
      <AlertDescription>
        {{
          t(
            '可用额度已用尽，API 密钥已暂停。收到额度卡、使用重置卡或进入新的额度周期后会自动恢复。',
            'API keys are paused because credit is exhausted. They resume after receiving credit, using a reset card, or entering a new quota period.',
          )
        }}
      </AlertDescription>
    </Alert>
    <Alert v-if="quota?.sync_error" variant="destructive">
      <AlertDescription>
        {{ t('密钥状态同步失败，系统将自动重试：', 'Key synchronization failed and will retry: ')
        }}{{ quota.sync_error }}
      </AlertDescription>
    </Alert>
    <div class="grid gap-4 sm:grid-cols-2">
      <Card v-for="metric in metrics" :key="metric.label">
        <CardHeader>
          <CardDescription>{{ metric.label }}</CardDescription><CardTitle v-if="quota">
            {{
              quota.unlimited && !metric.card
                ? t('无限制', 'Unlimited')
                : formatUsd(metric.value ?? 0)
            }}
          </CardTitle><Skeleton v-else class="h-7 w-28" />
        </CardHeader>
        <CardContent class="flex flex-col gap-2 text-sm text-muted-foreground">
          <span>{{ metric.detail }}</span>
        </CardContent>
      </Card>
    </div>
    <Card data-testid="quota-limits">
      <CardHeader>
        <CardTitle>{{ t('配额管理', 'Quota management') }}</CardTitle>
      </CardHeader>
      <CardContent class="grid gap-6 md:grid-cols-2">
        <template v-if="quota">
          <QuotaProgress :label="t('日配额', 'Daily quota')" :remaining="quota.daily_remaining_usd" :total="quota.daily_quota_usd" :unlimited="quota.daily_quota_usd === null">
            <span>{{ t('重置', 'Resets') }} {{ formatDateTime(quota.daily_resets_at) }}</span>
          </QuotaProgress>
          <QuotaProgress :label="t('周配额', 'Weekly quota')" :remaining="quota.weekly_remaining_usd" :total="quota.weekly_quota_usd" :unlimited="quota.weekly_quota_usd === null">
            <span>{{ t('重置', 'Resets') }} {{ formatDateTime(quota.weekly_resets_at) }}</span>
          </QuotaProgress>
        </template>
        <template v-else><Skeleton v-for="i in 2" :key="i" class="h-20 w-full" /></template>
        <div class="flex min-w-0 flex-col gap-3 md:col-span-2">
          <h3 class="text-sm font-medium">{{ t('重置记录', 'Reset history') }}</h3>
          <div class="overflow-hidden rounded-lg border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t('时间', 'Time') }}</TableHead>
                  <TableHead>{{ t('来源', 'Source') }}</TableHead>
                  <TableHead>{{ t('恢复日配额', 'Daily quota restored') }}</TableHead>
                  <TableHead>{{ t('恢复周配额', 'Weekly quota restored') }}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableEmpty v-if="!resets.length" :colspan="4">
                  {{ loading ? t('加载中', 'Loading') : t('暂无重置记录', 'No resets') }}
                </TableEmpty>
                <TableRow v-for="reset in resets" :key="reset.id">
                  <TableCell>{{ formatDateTime(reset.created_at) }}</TableCell>
                  <TableCell>
                    {{ reset.card_id ? t(`重置卡 #${reset.card_id}`, `Reset card #${reset.card_id}`) : t('管理员重置', 'Administrator reset') }}
                  </TableCell>
                  <TableCell>{{ formatUsd(reset.daily_used_usd) }}</TableCell>
                  <TableCell>{{ formatUsd(reset.weekly_used_usd) }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
            <TablePaginationFooter
              v-model:page="resetPage"
              :page-size="20"
              :page-size-options="[20]"
              :total="resetTotal"
            />
          </div>
        </div>
      </CardContent>
    </Card>
    <QuotaCardsPanel ref="cardsPanel" @changed="load" />
  </section>
</template>
