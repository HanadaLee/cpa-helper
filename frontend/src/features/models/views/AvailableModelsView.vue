<script setup lang="ts">
import { ChevronLeftIcon, ChevronRightIcon, Cpu, KeyRound, RefreshCw, TriangleAlertIcon } from '@lucide/vue'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { listAvailableModels } from '@/features/models/api/availableModelsApi'
import { useI18n } from '@/shared/i18n'
import type { AvailableModel, AvailableModelPrice, AvailableModelsResponse } from '@/shared/types/api'

type BillingUnit = 'token' | 'request'
type RateField = 'input' | 'output' | 'cache_read' | 'cache_creation' | 'request'
type RatePrefix = '' | 'peak_' | 'long_context_' | 'long_context_peak_'
type RateTier = { key: string; label: string; prefix: RatePrefix; multiplier: number }
const rateFields: RateField[] = ['request', 'input', 'output', 'cache_read', 'cache_creation']

const router = useRouter()
const { currentLanguage, errorText, serverText, t } = useI18n()
const isLoading = ref(false)
const errorMessage = ref<string | null>(null)
const response = ref<AvailableModelsResponse | null>(null)
const page = ref(1)
const pageSize = 20

const modelCount = computed(() => response.value?.models.length ?? 0)
const pagedModels = computed(() => {
  const start = (page.value - 1) * pageSize
  return response.value?.models.slice(start, start + pageSize) ?? []
})
function displayText(value: string | null | undefined): string {
  return value?.trim() || '-'
}

function formatUsdPerMtok(value: number): string {
  return value.toLocaleString(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    maximumFractionDigits: 6,
  })
}

function billingUnitForModel(model: string): BillingUnit {
  return model.trim().toLowerCase().includes('image') ? 'request' : 'token'
}

function modelBillingUnit(row: AvailableModel): BillingUnit {
  if (row.price?.billing_unit === 'request') {
    return 'request'
  }
  if (row.price?.billing_unit === 'token') {
    return 'token'
  }
  return billingUnitForModel(row.price?.model || row.id)
}

function billingLabel(row: AvailableModel): string {
  const unit = modelBillingUnit(row)
  return unit === 'request' ? t('按次', 'Per request') : t('按 Token', 'Per token')
}

function rateTiers(row: AvailableModel): RateTier[] {
  const price = row.price
  if (!price) return [{ key: 'base', label: '-', prefix: '', multiplier: 1 }]
  const tiers: RateTier[] = []
  const addTier = (key: string, label: string, prefix: RatePrefix, supportsFast = true, supportsUltraFast = true) => {
    tiers.push({ key, label, prefix, multiplier: 1 })
    if (price.fast_enabled && supportsFast) tiers.push({ key: `${key}-fast`, label: `${label} · FAST`, prefix, multiplier: price.fast_multiplier })
    if (price.ultrafast_enabled && supportsUltraFast) tiers.push({ key: `${key}-ultrafast`, label: `${label} · ULTRAFAST`, prefix, multiplier: price.ultrafast_multiplier })
  }
  addTier('base', price.off_peak_enabled ? t('常规 · 空闲', 'Standard · off-peak') : t('常规', 'Standard'), '')
  if (price.off_peak_enabled) addTier('peak', t('常规 · 高峰', 'Standard · peak'), 'peak_')
  if (price.long_context_enabled) {
    const noFast = price.fast_enabled && price.long_context_fast_unsupported ? t(' · 无 FAST', ' · no FAST') : ''
    const suffix = noFast + (price.ultrafast_enabled && price.long_context_ultrafast_unsupported ? t(' · 无 ULTRAFAST', ' · no ULTRAFAST') : '')
    addTier('long', (price.off_peak_enabled ? t('长上下文 · 空闲', 'Long context · off-peak') : t('长上下文', 'Long context')) + suffix, 'long_context_', !price.long_context_fast_unsupported, !price.long_context_ultrafast_unsupported)
    if (price.off_peak_enabled) addTier('long-peak', t('长上下文 · 高峰', 'Long context · peak') + suffix, 'long_context_peak_', !price.long_context_fast_unsupported, !price.long_context_ultrafast_unsupported)
  }
  return tiers
}

function rateValue(row: AvailableModel, tier: RateTier, field: RateField): string {
  const price = row.price
  if (!price) return '-'
  if (field === 'request') {
    if (modelBillingUnit(row) !== 'request') return '-'
    const value = tier.prefix === 'peak_' ? price.peak_request_usd ?? price.request_usd : price.request_usd
    return value === null ? t('未定价', 'Unpriced') : formatUsdPerMtok(Number((value * tier.multiplier).toPrecision(12)))
  }
  if (modelBillingUnit(row) === 'request') return '-'
  const key = `${tier.prefix}${field}_usd_per_million` as keyof AvailableModelPrice
  const value = price[key]
  return typeof value === 'number' ? formatUsdPerMtok(Number((value * tier.multiplier).toPrecision(12))) : '-'
}

function goToApiKeys() {
  void router.push('/account/keys')
}

async function refresh() {
  isLoading.value = true
  errorMessage.value = null
  try {
    response.value = await listAvailableModels()
    page.value = 1
  } catch (error) {
    response.value = null
    errorMessage.value = errorText(error, '加载可用模型失败', 'Failed to load available models')
  } finally {
    isLoading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page models-page" :aria-busy="isLoading">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('可用模型', 'Available Models') }}</h1>
      <Button variant="outline" :disabled="isLoading" @click="refresh">
        <Spinner v-if="isLoading" data-icon="inline-start" />
        <RefreshCw v-else data-icon="inline-start" />
        {{ t('刷新', 'Refresh') }}
      </Button>
    </div>

    <section class="panel model-table-panel">
      <div class="panel-inner model-panel">
        <Alert v-if="errorMessage" variant="destructive">
          <AlertTitle>{{ t('加载可用模型失败', 'Failed to load available models') }}</AlertTitle>
          <AlertDescription>{{ errorMessage }}</AlertDescription>
          <AlertAction>
            <Button size="sm" variant="outline" :disabled="isLoading" @click="refresh">
              <Spinner v-if="isLoading" data-icon="inline-start" />
              <RefreshCw v-else data-icon="inline-start" />
              {{ t('重试', 'Retry') }}
            </Button>
          </AlertAction>
        </Alert>

        <template v-else>
          <Alert v-if="response?.errors.length">
            <TriangleAlertIcon />
            <AlertTitle>{{ t('部分 API Key 查询失败', 'Some API key queries failed') }}</AlertTitle>
            <AlertDescription>
              <div class="key-errors">
                <div v-for="error in response.errors" :key="error.api_key_hash">
                  {{
                    t(
                      `${error.description}（${error.api_key_preview}）：${serverText(error.message, '查询失败', 'Query failed')}`,
                      `${error.description} (${error.api_key_preview}): ${serverText(error.message, '查询失败', 'Query failed')}`,
                    )
                  }}
                </div>
              </div>
            </AlertDescription>
          </Alert>

          <div v-if="isLoading && !response" class="loading-state">
            <Spinner class="size-5" />
            <span>{{ t('正在查询模型', 'Querying models') }}</span>
          </div>

          <Empty v-else-if="response && !response.has_api_keys" class="min-h-[220px]">
            <EmptyHeader>
              <EmptyMedia variant="icon"><KeyRound /></EmptyMedia>
              <EmptyTitle>{{ t('暂无可查询的 API 密钥', 'No queryable API keys') }}</EmptyTitle>
              <EmptyDescription>{{ t('还没有可用于查询模型的 API 密钥', 'No API keys are available for model queries yet') }}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button @click="goToApiKeys">{{ t('去创建 API 密钥', 'Create API key') }}</Button>
            </EmptyContent>
          </Empty>

          <Empty
            v-else-if="response && response.has_api_keys && response.queryable_api_key_count === 0"
            class="min-h-[220px]"
          >
            <EmptyHeader>
              <EmptyMedia variant="icon"><KeyRound /></EmptyMedia>
              <EmptyTitle>{{ t('API 密钥不可查询', 'API keys unavailable') }}</EmptyTitle>
              <EmptyDescription>
                {{ response.quota_paused
                  ? t('账户额度已用尽，API 密钥暂时不可用', 'Account quota is exhausted; API keys are temporarily unavailable')
                  : t('密钥已禁用或缺少完整密钥，无法查询模型', 'Keys are disabled or missing their full value and cannot query models') }}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button @click="goToApiKeys">{{ t('去 API 密钥页检查', 'Check API keys') }}</Button>
            </EmptyContent>
          </Empty>

          <Empty v-else-if="response && response.models.length === 0" class="min-h-[220px]">
            <EmptyHeader>
              <EmptyMedia variant="icon"><Cpu /></EmptyMedia>
              <EmptyTitle>{{ t('暂无可用模型', 'No available models') }}</EmptyTitle>
              <EmptyDescription>{{ t('当前没有返回可用模型', 'No available models were returned') }}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button variant="outline" :disabled="isLoading" @click="refresh">
                <Spinner v-if="isLoading" data-icon="inline-start" />
                <RefreshCw v-else data-icon="inline-start" />
                {{ t('重新查询', 'Query again') }}
              </Button>
            </EmptyContent>
          </Empty>

          <div v-else-if="response" class="available-models-table">
            <Table class="table-fixed">
              <TableHeader class="sticky top-0 bg-card">
                <TableRow>
                  <TableHead class="w-[170px]">{{ t('模型 ID', 'Model ID') }}</TableHead>
                  <TableHead class="model-secondary-column w-[115px]">{{ t('名称', 'Name') }}</TableHead>
                  <TableHead class="w-[80px] text-center">{{ t('计费方式', 'Billing') }}</TableHead>
                  <TableHead class="w-[175px]">{{ t('费率档位', 'Rate tier') }}</TableHead>
                  <TableHead class="w-[75px] text-right whitespace-normal leading-tight">{{ t('每次 ($)', 'Per request ($)') }}</TableHead>
                  <TableHead class="w-[80px] text-right whitespace-normal leading-tight">{{ t('输入 $/MTok', 'Input $/MTok') }}</TableHead>
                  <TableHead class="w-[80px] text-right whitespace-normal leading-tight">{{ t('输出 $/MTok', 'Output $/MTok') }}</TableHead>
                  <TableHead class="model-secondary-column w-[80px] text-right">{{ t('缓存读 $/MTok', 'Cache read $/MTok') }}</TableHead>
                  <TableHead class="model-secondary-column w-[80px] text-right">{{ t('缓存写 $/MTok', 'Cache write $/MTok') }}</TableHead>
                </TableRow>
              </TableHeader>

              <TableBody>
                <template v-if="isLoading && pagedModels.length === 0">
                  <TableRow v-for="rowIndex in 5" :key="`model-skeleton-${rowIndex}`">
                    <TableCell v-for="columnIndex in 9" :key="columnIndex" :class="{ 'model-secondary-column': [2, 8, 9].includes(columnIndex) }">
                      <Skeleton class="h-4 w-full" />
                    </TableCell>
                  </TableRow>
                </template>

                <TableRow v-for="model in pagedModels" v-else :key="model.id">
                  <TableCell class="font-mono text-xs">
                    <span class="block truncate" :title="model.id">{{ model.id }}</span>
                  </TableCell>
                  <TableCell class="model-secondary-column"><span class="block truncate" :title="displayText(model.name)">{{ displayText(model.name) }}</span></TableCell>
                  <TableCell class="text-center">
                    <Badge :variant="modelBillingUnit(model) === 'request' ? 'secondary' : 'outline'">
                      {{ billingLabel(model) }}
                    </Badge>
                  </TableCell>
                  <TableCell><div class="rate-tier-stack"><div v-for="tier in rateTiers(model)" :key="tier.key" class="rate-tier-line" :title="tier.label">{{ tier.label }}</div></div></TableCell>
                  <TableCell v-for="field in rateFields" :key="field" class="text-right tabular-nums" :class="{ 'model-secondary-column': field === 'cache_read' || field === 'cache_creation' }"><div class="rate-tier-stack"><div v-for="tier in rateTiers(model)" :key="tier.key" class="rate-tier-line">{{ rateValue(model, tier, field) }}</div></div></TableCell>
                </TableRow>
              </TableBody>
            </Table>

            <div v-if="isLoading && pagedModels.length > 0" class="table-loading-overlay">
              <Spinner class="size-5" />
            </div>
          </div>

          <Pagination
            v-if="modelCount > pageSize"
            v-model:page="page"
            class="justify-end"
            :items-per-page="pageSize"
            :total="modelCount"
            :sibling-count="1"
            show-edges
          >
            <PaginationContent v-slot="{ items }">
              <PaginationPrevious size="icon" :aria-label="t('上一页', 'Previous page')">
                <ChevronLeftIcon data-icon="inline-start" />
              </PaginationPrevious>
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
              <PaginationNext size="icon" :aria-label="t('下一页', 'Next page')">
                <ChevronRightIcon data-icon="inline-end" />
              </PaginationNext>
            </PaginationContent>
          </Pagination>
        </template>
      </div>
    </section>
  </section>
</template>

<style scoped>
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
  .model-secondary-column {
    display: none;
  }
}

.model-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
  min-height: 0;
}

.models-page,
.model-table-panel,
.available-models-table {
  min-width: 0;
}

.model-table-panel {
  overflow: hidden;
}

.available-models-table {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--card);
  box-shadow: var(--cpa-shadow-card);
}

.available-models-table :deep([data-slot="table-container"]) {
  max-height: max(240px, calc(100dvh - 240px));
  overflow: auto;
}

.table-loading-overlay {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: color-mix(in oklch, var(--background) 62%, transparent);
}

.key-errors {
  display: grid;
  gap: 4px;
}

.loading-state {
  display: grid;
  min-height: 220px;
  place-items: center;
  color: var(--cpa-text-muted);
}

.loading-state {
  grid-auto-flow: column;
  justify-content: center;
  gap: 8px;
}

@media (min-width: 861px) {
  .models-page {
    grid-template-rows: auto minmax(0, 1fr);
    min-height: 0;
  }
}

</style>
