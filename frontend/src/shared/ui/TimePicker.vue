<script setup lang="ts">
import { computed } from 'vue'
import { parseTime } from '@internationalized/date'
import { Clock } from '@lucide/vue'
import { TimeFieldInput, TimeFieldRoot, type TimeValue } from 'reka-ui'
import { useI18n } from '@/shared/i18n'

const props = defineProps<{
  modelValue?: string
  id?: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { currentLanguage } = useI18n()
const value = computed(() => {
  if (!props.modelValue) return null
  try {
    return parseTime(props.modelValue)
  } catch {
    return null
  }
})

function update(value: TimeValue | null | undefined) {
  emit('update:modelValue', value ? `${String(value.hour).padStart(2, '0')}:${String(value.minute).padStart(2, '0')}` : '')
}
</script>

<template>
  <TimeFieldRoot
    :id="id"
    v-slot="{ segments }"
    data-slot="time-picker"
    :model-value="value"
    :disabled="disabled"
    :hour-cycle="24"
    granularity="minute"
    :locale="currentLanguage === 'zh' ? 'zh-CN' : 'en-GB'"
    class="flex h-8 w-full min-w-0 items-center gap-1 rounded-lg border border-input bg-transparent px-2.5 text-sm tabular-nums outline-none transition-[color,box-shadow,border-color] focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/35 data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
    @update:model-value="update"
  >
    <Clock class="mr-1 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
    <TimeFieldInput
      v-for="segment in segments"
      :key="segment.part"
      :part="segment.part"
      class="rounded px-0.5 outline-none focus:bg-accent focus:text-accent-foreground data-[placeholder]:text-muted-foreground"
    >
      {{ segment.value }}
    </TimeFieldInput>
  </TimeFieldRoot>
</template>
