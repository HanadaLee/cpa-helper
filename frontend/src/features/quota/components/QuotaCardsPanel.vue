<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import { Pencil, Plus, RefreshCw, RotateCcw, Ban } from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldContent,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableCell,
  TableRow,
  TableEmpty,
} from '@/components/ui/table'
import FilterCombobox from '@/shared/ui/FilterCombobox.vue'
import DateTimePicker from '@/shared/ui/DateTimePicker.vue'
import TablePaginationFooter from '@/shared/ui/TablePaginationFooter.vue'
import QuotaProgress from './QuotaProgress.vue'
import { useConfirmDialog } from '@/shared/ui/confirm-dialog'
import { useI18n } from '@/shared/i18n'
import { formatDateTime } from '@/shared/utils/format'
import type { QuotaCard, QuotaTargets, UserSummary } from '@/shared/types/api'
import {
  editQuotaCard,
  issueQuotaCards,
  listQuotaCards,
  revokeQuotaCards,
  useResetCard,
} from '../api/quotaApi'

const props = withDefaults(
  defineProps<{
    admin?: boolean
    userId?: number
    targetUserIds?: number[]
    users?: UserSummary[]
  }>(),
  { admin: false, userId: undefined, targetUserIds: () => [], users: () => [] },
)
const emit = defineEmits<{ changed: [] }>()
const { t, errorText } = useI18n()
const confirm = useConfirmDialog()
const cards = ref<QuotaCard[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(true)
const busy = ref(false)
const kind = ref('credit')
const showActions = computed(() => props.admin || kind.value === 'reset')
const tableColumnCount = computed(() => 3 + Number(props.admin) + Number(props.admin && !props.userId)
  + Number(kind.value === 'credit') + Number(showActions.value))
const statusFilter = ref<string | number | null>(null)
const statusOptions = computed(() => [
  kind.value === 'credit'
    ? { value: 'active', label: t('生效中', 'Active') }
    : { value: 'unused', label: t('未使用', 'Unused') },
  ...(kind.value === 'credit'
    ? [{ value: 'exhausted', label: t('已用完', 'Depleted') }]
    : [{ value: 'used', label: t('已使用', 'Used') }]),
  { value: 'expired', label: t('已过期', 'Expired') },
  { value: 'revoked', label: t('已失效', 'Invalidated') },
])
const userFilter = ref<string | number | null>(null)
const selected = ref<number[]>([])
const editorOpen = ref(false)
const editingId = ref<number | null>(null)
const cardKind = ref<'credit' | 'reset'>('credit')
const name = ref('')
const amount = ref<string | number>(10)
const count = ref<string | number>(1)
const permanent = ref(true)
const expires = ref('')
const allUsers = ref(false)
const targetIDs = computed(() =>
  props.userId
    ? [props.userId]
    : props.targetUserIds.length
      ? props.targetUserIds
      : userFilter.value
        ? [Number(userFilter.value)]
        : [],
)
const userOptions = computed(() =>
  props.users.map((user) => ({ value: user.id, label: user.nickname || user.username })),
)
const selectable = computed(() =>
  cards.value
    .filter((card) => card.status !== 'revoked' && card.status !== 'used')
    .map((card) => card.id),
)
const allSelected = computed(
  () => selectable.value.length > 0 && selectable.value.every((id) => selected.value.includes(id)),
)
const targetText = computed(() =>
  allUsers.value
    ? t('全站所有用户', 'All users')
    : t(`已选择 ${targetIDs.value.length} 位用户`, `${targetIDs.value.length} users selected`),
)
let requestId = 0

async function load() {
  const id = ++requestId
  loading.value = true
  try {
    const result = await listQuotaCards(
      props.admin,
      page.value,
      props.userId ?? (userFilter.value ? Number(userFilter.value) : undefined),
      kind.value,
      statusFilter.value ? String(statusFilter.value) : undefined,
    )
    if (id !== requestId) return
    cards.value = result.items
    total.value = result.total
    selected.value = []
  } catch (error) {
    if (id === requestId) toast.error(errorText(error, '加载卡片失败', 'Failed to load cards'))
  } finally {
    if (id === requestId) loading.value = false
  }
}

watch(kind, () => {
  statusFilter.value = null
})
watch([kind, userFilter, statusFilter, () => props.userId], () => {
  if (page.value !== 1) page.value = 1
  else void load()
})
watch(page, load, { immediate: true })

function selectCard(id: number, checked: boolean | 'indeterminate') {
  selected.value =
    checked === true
      ? [...new Set([...selected.value, id])]
      : selected.value.filter((value) => value !== id)
}

function statusLabel(status: QuotaCard['status']) {
  return {
    active: t('生效中', 'Active'),
    unused: t('未使用', 'Unused'),
    expired: t('已过期', 'Expired'),
    exhausted: t('已用完', 'Depleted'),
    used: t('已使用', 'Used'),
    revoked: t('已失效', 'Invalidated'),
  }[status]
}

function openEditor(card?: QuotaCard) {
  editingId.value = card?.id ?? null
  cardKind.value = card?.kind ?? (kind.value === 'reset' ? 'reset' : 'credit')
  name.value = card?.name ?? ''
  amount.value = card?.amount_usd ?? 10
  count.value = 1
  permanent.value = !card?.expires_at
  expires.value = card?.expires_at
    ? formatDateTime(card.expires_at).replace(' ', 'T').slice(0, 16)
    : ''
  allUsers.value = false
  editorOpen.value = true
}

async function save() {
  if (busy.value) return
  if (!editingId.value && !allUsers.value && !targetIDs.value.length) {
    toast.error(
      t('请选择用户，或开启向全站用户发放', 'Select users or enable issuance to all users'),
    )
    return
  }
  const amountValue = Number(amount.value)
  const countValue = Number(count.value)
  if (
    !Number.isFinite(amountValue) ||
    (cardKind.value === 'credit' && amountValue <= 0) ||
    !Number.isInteger(countValue) ||
    countValue < 1 ||
    countValue > 100
  ) {
    toast.error(t('请填写有效金额和数量', 'Enter a valid amount and quantity'))
    return
  }
  const expiry = permanent.value ? null : new Date(`${expires.value}:00+08:00`)
  if (expiry && (!Number.isFinite(expiry.getTime()) || expiry.getTime() <= Date.now())) {
    toast.error(t('请选择未来的到期时间', 'Choose a future expiration time'))
    return
  }
  const targets: QuotaTargets = allUsers.value ? { all_users: true } : { user_ids: targetIDs.value }
  const payload = {
    ...targets,
    kind: cardKind.value,
    name: name.value,
    amount_usd: amountValue,
    expires_at: expiry?.toISOString() ?? null,
    count: countValue,
  }
  busy.value = true
  try {
    if (editingId.value) {
      await editQuotaCard(editingId.value, payload)
      toast.success(t('卡片已保存', 'Card saved'))
    } else {
      const result = await issueQuotaCards(payload)
      toast.success(t(`已发放 ${result.issued} 张卡片`, `${result.issued} cards issued`))
    }
    editorOpen.value = false
    await load()
    emit('changed')
  } catch (error) {
    toast.error(errorText(error, '保存卡片失败', 'Failed to save cards'))
  } finally {
    busy.value = false
  }
}

function revoke(ids = selected.value) {
  if (!ids.length) return
  confirm.warning({
    title: t('吊销卡片', 'Revoke cards'),
    content: t(
      `确定吊销这 ${ids.length} 张卡片？剩余额度立即失效，历史扣款记录仍会保留。`,
      `Revoke these ${ids.length} cards? Remaining credit expires immediately; deduction history is retained.`,
    ),
    positiveText: t('吊销', 'Revoke'),
    onPositiveClick: async () => {
      busy.value = true
      try {
        await revokeQuotaCards({ card_ids: ids })
        toast.success(t('额度卡已吊销', 'Cards revoked'))
        await load()
        emit('changed')
      } catch (error) {
        toast.error(errorText(error, '吊销失败', 'Failed to revoke cards'))
      } finally {
        busy.value = false
      }
    },
  })
}

function useCard(card: QuotaCard) {
  confirm.warning({
    title: t('使用重置卡', 'Use reset card'),
    content: t(
      '将清零日配额和周配额已用量，原重置时间保持不变。此卡只能使用一次。',
      'Clear daily and weekly usage without changing their reset times. This card can only be used once.',
    ),
    positiveText: t('使用', 'Use'),
    onPositiveClick: async () => {
      busy.value = true
      try {
        await useResetCard(card.id)
        toast.success(t('日配额与周配额已重置', 'Daily and weekly quotas reset'))
        await load()
        emit('changed')
      } catch (error) {
        toast.error(errorText(error, '使用重置卡失败', 'Failed to use reset card'))
      } finally {
        busy.value = false
      }
    },
  })
}

defineExpose({ refresh: load })
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>
        {{
          admin ? t('配额卡管理', 'Quota cards') : t('额度管理', 'Credit management')
        }}
      </CardTitle>
    </CardHeader>
    <CardContent class="flex min-w-0 flex-col gap-4">
      <div class="flex flex-wrap items-center gap-2">
        <Tabs v-model="kind">
          <TabsList>
            <TabsTrigger value="credit">{{ t('额度卡', 'Credit cards') }}</TabsTrigger><TabsTrigger value="reset">{{ t('重置卡', 'Reset cards') }}</TabsTrigger>
          </TabsList>
        </Tabs>
        <FilterCombobox
          v-if="admin && !userId"
          v-model="userFilter"
          class="w-56"
          :options="userOptions"
          :placeholder="t('用户', 'User')"
        />
        <FilterCombobox
          v-model="statusFilter"
          class="w-36"
          :options="statusOptions"
          :placeholder="t('状态', 'Status')"
        />
        <div class="ml-auto flex flex-wrap gap-2">
          <Button variant="outline" :disabled="loading || busy" @click="load">
            <RefreshCw data-icon="inline-start" />{{ t('刷新', 'Refresh') }}
          </Button>
          <Button
            v-if="admin"
            variant="outline"
            :disabled="!selected.length || busy"
            @click="revoke()"
          >
            <Ban data-icon="inline-start" />{{ t('吊销', 'Revoke') }}
          </Button>
          <Button v-if="admin" :disabled="busy" @click="openEditor()">
            <Plus data-icon="inline-start" />{{ t('发放卡片', 'Issue cards') }}
          </Button>
        </div>
      </div>
      <div class="overflow-hidden rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead v-if="admin" class="w-10">
                <Checkbox
                  :model-value="allSelected"
                  :aria-label="t('选择本页卡片', 'Select cards on this page')"
                  @update:model-value="selected = $event === true ? [...selectable] : []"
                />
              </TableHead>
              <TableHead>{{ t('卡片', 'Card') }}</TableHead>
              <TableHead v-if="admin && !userId">{{ t('用户', 'User') }}</TableHead>
              <TableHead>{{ t('状态', 'Status') }}</TableHead>
              <TableHead v-if="kind === 'credit'">
                {{
                  t('剩余 / 总额度', 'Remaining / total')
                }}
              </TableHead>
              <TableHead>{{ t('有效期至', 'Expires at') }}</TableHead>
              <TableHead v-if="showActions" class="text-right">{{ t('操作', 'Actions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <template v-if="loading && !cards.length">
              <TableRow v-for="i in 4" :key="i">
                <TableCell :colspan="tableColumnCount"><Skeleton class="h-8 w-full" /></TableCell>
              </TableRow>
            </template>
            <TableEmpty v-else-if="!cards.length" :colspan="tableColumnCount">
              {{
                kind === 'credit' ? t('暂无额度卡', 'No credit cards') : t('暂无重置卡', 'No reset cards')
              }}
            </TableEmpty>
            <TableRow v-for="card in cards" :key="card.id">
              <TableCell v-if="admin">
                <Checkbox
                  :model-value="selected.includes(card.id)"
                  :disabled="card.status === 'revoked' || card.status === 'used'"
                  :aria-label="t(`选择卡片 ${card.id}`, `Select card ${card.id}`)"
                  @update:model-value="selectCard(card.id, $event)"
                />
              </TableCell>
              <TableCell>
                <div class="flex flex-col gap-1">
                  <span>{{
                    card.name ||
                      (card.kind === 'credit'
                        ? t('额度卡', 'Credit card')
                        : t('重置卡', 'Reset card'))
                  }}
                    #{{ card.id }}</span><span class="text-xs text-muted-foreground">{{
                    formatDateTime(card.created_at)
                  }}</span>
                </div>
              </TableCell>
              <TableCell v-if="admin && !userId">{{ card.username }}</TableCell>
              <TableCell>
                <Badge :variant="card.status === 'active' ? 'default' : 'secondary'">
                  {{
                    statusLabel(card.status)
                  }}
                </Badge>
              </TableCell>
              <TableCell v-if="kind === 'credit'" class="tabular-nums">
                <QuotaProgress :label="t('额度卡', 'Credit card')" :remaining="card.remaining_usd" :total="card.amount_usd" :inactive="card.status === 'expired' || card.status === 'revoked'" class="min-w-40" />
              </TableCell>
              <TableCell>
                {{
                  card.expires_at ? formatDateTime(card.expires_at) : t('永久有效', 'Never expires')
                }}
              </TableCell>
              <TableCell v-if="showActions" class="text-right">
                <div class="flex justify-end gap-1">
                  <template v-if="admin && card.status !== 'revoked' && card.status !== 'used'">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      :disabled="busy"
                      :aria-label="t(`编辑卡片 ${card.id}`, `Edit card ${card.id}`)"
                      @click="openEditor(card)"
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      :disabled="busy"
                      :aria-label="t(`吊销卡片 ${card.id}`, `Revoke card ${card.id}`)"
                      @click="revoke([card.id])"
                    >
                      <Ban />
                    </Button>
                  </template>
                  <Button
                    v-else-if="!admin && card.kind === 'reset' && card.status === 'unused'"
                    variant="outline"
                    size="sm"
                    :disabled="busy"
                    @click="useCard(card)"
                  >
                    <RotateCcw data-icon="inline-start" />{{ t('使用', 'Use') }}
                  </Button>
                  <span v-else class="text-muted-foreground">{{
                    card.used_at ? formatDateTime(card.used_at) : '—'
                  }}</span>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <TablePaginationFooter
          v-model:page="page"
          :page-size="20"
          :page-size-options="[20]"
          :total="total"
        />
      </div>
    </CardContent>
  </Card>

  <Dialog v-model:open="editorOpen">
    <DialogContent class="sm:max-w-lg">
      <form class="flex flex-col gap-5" @submit.prevent="save">
        <DialogHeader>
          <DialogTitle>
            {{
              editingId ? t('编辑卡片', 'Edit card') : t('发放卡片', 'Issue cards')
            }}
          </DialogTitle><DialogDescription>
            {{
              editingId
                ? t('已扣额度不会因编辑而退回。', 'Editing does not refund previous deductions.')
                : targetText
            }}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field v-if="!editingId && !userId" orientation="horizontal">
            <FieldContent>
              <FieldLabel for="quota-all-users">
                {{
                  t('向全站所有用户发放', 'Issue to all users')
                }}
              </FieldLabel>
            </FieldContent><Switch id="quota-all-users" v-model="allUsers" />
          </Field>
          <Field>
            <FieldLabel for="card-name">{{ t('卡片名称', 'Card name') }}</FieldLabel><Input id="card-name" v-model="name" :maxlength="120" />
          </Field>
          <Field v-if="cardKind === 'credit'">
            <FieldLabel for="card-amount">{{ t('总额度 USD', 'Total credit USD') }}</FieldLabel><Input
              id="card-amount"
              v-model="amount"
              required
              type="number"
              min="0.00000001"
              step="0.00000001"
            />
          </Field>
          <Field v-if="!editingId">
            <FieldLabel for="card-count">{{ t('每位用户发放数量', 'Cards per user') }}</FieldLabel><Input
              id="card-count"
              v-model="count"
              required
              type="number"
              min="1"
              max="100"
              step="1"
            />
          </Field>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldLabel for="card-permanent">
                {{
                  t('永久有效', 'Never expires')
                }}
              </FieldLabel>
            </FieldContent><Switch id="card-permanent" v-model="permanent" />
          </Field>
          <Field v-if="!permanent">
            <FieldLabel for="card-expires">
              {{
                t('到期时间（北京时间）', 'Expiration (Beijing time)')
              }}
            </FieldLabel><DateTimePicker
              id="card-expires"
              v-model="expires"
              :disabled="busy"
            /><FieldDescription>
              {{
                t('到期后剩余额度失效。', 'Unused credit expires at this time.')
              }}
            </FieldDescription>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button type="button" variant="outline" :disabled="busy" @click="editorOpen = false">
            {{
              t('取消', 'Cancel')
            }}
          </Button><Button type="submit" :disabled="busy">
            <Spinner v-if="busy" data-icon="inline-start" />{{
              editingId ? t('保存', 'Save') : t('发放', 'Issue')
            }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
