<script setup lang="ts">
import type { DateRange } from 'reka-ui'
import { CalendarDate } from '@internationalized/date'
import { CalendarDays, ChevronDown } from '@lucide/vue'
import { computed, ref, shallowRef, useId, watch } from 'vue'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { RangeCalendar } from '@/components/ui/range-calendar'
import { Separator } from '@/components/ui/separator'
import { useI18n } from '@/shared/i18n'
import { formatDateTime } from '@/shared/utils/format'
import TimePicker from './TimePicker.vue'

const props = withDefaults(defineProps<{
  modelValue?: [number, number] | null
  clearable?: boolean
  disabled?: boolean
  size?: 'sm' | 'default'
}>(), {
  modelValue: null,
  clearable: false,
  disabled: false,
  size: 'default',
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: [number, number] | null): void
  (event: 'clear'): void
}>()

const { currentLanguage, t } = useI18n()
const fieldId = useId()
const open = ref(false)
const draftStart = ref('')
const draftEnd = ref('')
const calendarRange = shallowRef<DateRange | null>(null)

const locale = computed(() => currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US')

function formatDateTimeLocal(timestamp: number): string {
  return formatDateTime(timestamp).replace(' ', 'T').slice(0, 16)
}

function parseDateTimeLocal(value: string): number | null {
  const timestamp = new Date(`${value}+08:00`).getTime()
  return Number.isFinite(timestamp) ? timestamp : null
}

function timestampToCalendarDate(timestamp: number): CalendarDate {
  const beijing = new Date(timestamp + 8 * 60 * 60 * 1000)
  return new CalendarDate(
    beijing.getUTCFullYear(),
    beijing.getUTCMonth() + 1,
    beijing.getUTCDate(),
  )
}

function dateValueToInputDate(value: { year: number, month: number, day: number }): string {
  return `${String(value.year).padStart(4, '0')}-${String(value.month).padStart(2, '0')}-${String(value.day).padStart(2, '0')}`
}

function formatRangePart(timestamp: number): string {
  return formatDateTime(timestamp).slice(0, 16)
}

const startText = computed(() => props.modelValue ? formatRangePart(props.modelValue[0]) : '')
const endText = computed(() => props.modelValue ? formatRangePart(props.modelValue[1]) : '')
const canApply = computed(() => {
  const start = parseDateTimeLocal(draftStart.value)
  const end = parseDateTimeLocal(draftEnd.value)
  return start !== null && end !== null && start <= end
})

function syncDraft(value: [number, number] | null | undefined) {
  draftStart.value = value ? formatDateTimeLocal(value[0]) : ''
  draftEnd.value = value ? formatDateTimeLocal(value[1]) : ''
  calendarRange.value = value
    ? { start: timestampToCalendarDate(value[0]), end: timestampToCalendarDate(value[1]) }
    : null
}

watch(
  () => props.modelValue,
  syncDraft,
  { immediate: true, deep: true },
)

function handleOpenChange(value: boolean) {
  if (value) {
    syncDraft(props.modelValue)
  }
  open.value = value
}

function updateCalendarRange(value: DateRange) {
  calendarRange.value = value
  if (value.start) {
    const time = draftStart.value.slice(11, 16) || '00:00'
    draftStart.value = `${dateValueToInputDate(value.start)}T${time}`
  }
  if (value.end) {
    const time = draftEnd.value.slice(11, 16) || '23:59'
    draftEnd.value = `${dateValueToInputDate(value.end)}T${time}`
  }
}

function updateTime(target: 'start' | 'end', value: string | number) {
  const time = String(value)
  const current = target === 'start' ? draftStart.value : draftEnd.value
  const calendarValue = target === 'start' ? calendarRange.value?.start : calendarRange.value?.end
  const date = current.slice(0, 10) || (calendarValue ? dateValueToInputDate(calendarValue) : '')
  if (!date) {
    return
  }
  if (target === 'start') {
    draftStart.value = `${date}T${time}`
  } else {
    draftEnd.value = `${date}T${time}`
  }
}

function apply() {
  const start = parseDateTimeLocal(draftStart.value)
  const end = parseDateTimeLocal(draftEnd.value)
  if (start === null || end === null || start > end) {
    return
  }
  emit('update:modelValue', [start, end])
  open.value = false
}

function clear() {
  syncDraft(null)
  emit('update:modelValue', null)
  emit('clear')
  open.value = false
}

function cancel() {
  syncDraft(props.modelValue)
  open.value = false
}
</script>

<template>
  <div data-slot="date-time-range-picker" class="flex min-w-0 items-center">
    <Popover :open="open" @update:open="handleOpenChange">
      <PopoverTrigger as-child>
        <Button
          type="button"
          variant="outline"
          :size="size"
          :disabled="disabled"
          data-slot="date-time-range-trigger"
          class="min-w-0 flex-1 justify-start text-left font-normal"
        >
          <CalendarDays data-icon="inline-start" />
          <span
            v-if="modelValue"
            data-slot="date-time-range-value"
            class="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 tabular-nums"
          >
            <span data-slot="date-time-range-start" class="truncate text-left" :title="startText">
              {{ startText }}
            </span>
            <span class="text-center text-muted-foreground">—</span>
            <span data-slot="date-time-range-end" class="truncate text-right" :title="endText">
              {{ endText }}
            </span>
          </span>
          <span v-else class="min-w-0 flex-1 truncate text-left text-muted-foreground">
            {{ t('选择日期与时间', 'Select date and time') }}
          </span>
          <ChevronDown data-icon="inline-end" class="ml-auto text-muted-foreground" />
        </Button>
      </PopoverTrigger>

      <PopoverContent
        align="start"
        class="max-h-[calc(100dvh-2rem)] w-auto max-w-[calc(100vw-2rem)] overflow-y-auto p-0"
      >
        <RangeCalendar
          :model-value="calendarRange"
          :number-of-months="2"
          :week-starts-on="1"
          :locale="locale"
          initial-focus
          @update:model-value="updateCalendarRange"
        />
        <Separator />
        <div class="grid gap-3 p-3 sm:grid-cols-2">
          <Field>
            <FieldLabel :id="`${fieldId}-start-label`">{{ t('开始时间', 'Start time') }}</FieldLabel>
            <TimePicker
              :id="`${fieldId}-start`"
              :aria-labelledby="`${fieldId}-start-label`"
              :model-value="draftStart.slice(11, 16)"
              :disabled="!calendarRange?.start"
              @update:model-value="updateTime('start', $event)"
            />
          </Field>
          <Field>
            <FieldLabel :id="`${fieldId}-end-label`">{{ t('结束时间', 'End time') }}</FieldLabel>
            <TimePicker
              :id="`${fieldId}-end`"
              :aria-labelledby="`${fieldId}-end-label`"
              :model-value="draftEnd.slice(11, 16)"
              :disabled="!calendarRange?.end"
              @update:model-value="updateTime('end', $event)"
            />
          </Field>
        </div>
        <div class="flex items-center justify-between gap-2 px-3 pb-3">
          <Button
            v-if="clearable"
            type="button"
            variant="ghost"
            size="sm"
            :disabled="!modelValue"
            @click="clear"
          >
            {{ t('清除', 'Clear') }}
          </Button>
          <span v-else />
          <div class="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" @click="cancel">
              {{ t('取消', 'Cancel') }}
            </Button>
            <Button type="button" size="sm" :disabled="!canApply" @click="apply">
              {{ t('应用', 'Apply') }}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  </div>
</template>
