<script setup lang="ts">
import type { DateValue } from '@internationalized/date'
import { parseDate, today } from '@internationalized/date'
import { CalendarDays, ChevronDown } from '@lucide/vue'
import { computed, ref, shallowRef, useId } from 'vue'
import Calendar from '@/components/ui/calendar/Calendar.vue'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Separator } from '@/components/ui/separator'
import { useI18n } from '@/shared/i18n'
import TimePicker from './TimePicker.vue'

// A wall-clock value in Beijing time, not the browser's local timezone.
const props = defineProps<{ modelValue: string, id?: string, disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { currentLanguage, t } = useI18n()
const timeId = useId()
const open = ref(false)
const date = shallowRef<DateValue>()
const time = ref('00:00')
const label = computed(() => props.modelValue.replace('T', ' '))

function changeOpen(value: boolean) {
  if (value) {
    date.value = props.modelValue ? parseDate(props.modelValue.slice(0, 10)) : undefined
    time.value = props.modelValue.slice(11, 16) || '00:00'
  }
  open.value = value
}

function apply() {
  if (!date.value || !time.value) return
  emit('update:modelValue', `${date.value.toString()}T${time.value}`)
  open.value = false
}
</script>

<template>
  <Popover :open="open" @update:open="changeOpen">
    <PopoverTrigger as-child>
      <Button :id="id" type="button" variant="outline" :disabled="disabled" data-slot="date-time-trigger" class="w-full justify-start font-normal">
        <CalendarDays data-icon="inline-start" />
        <span class="min-w-0 flex-1 truncate text-left tabular-nums" :class="{ 'text-muted-foreground': !modelValue }">{{ label || t('选择日期与时间', 'Select date and time') }}</span>
        <ChevronDown data-icon="inline-end" class="text-muted-foreground" />
      </Button>
    </PopoverTrigger>
    <PopoverContent align="start" :collision-padding="12" data-slot="date-time-content" class="max-h-[var(--reka-popover-content-available-height)] w-auto overflow-y-auto p-0">
      <Calendar
        v-model="date"
        :default-placeholder="date ?? today('Asia/Shanghai')"
        :week-starts-on="1"
        :locale="currentLanguage === 'zh' ? 'zh-CN' : 'en-US'"
        initial-focus
      />
      <Separator />
      <Field class="px-3 py-1">
        <FieldLabel :id="timeId">{{ t('时间（北京时间）', 'Time (Beijing time)') }}</FieldLabel>
        <TimePicker v-model="time" :aria-labelledby="timeId" />
      </Field>
      <div class="flex justify-end gap-2 px-3 pb-3">
        <Button type="button" variant="outline" size="sm" @click="open = false">{{ t('取消', 'Cancel') }}</Button>
        <Button type="button" size="sm" :disabled="!date || !time" @click="apply">{{ t('应用', 'Apply') }}</Button>
      </div>
    </PopoverContent>
  </Popover>
</template>
