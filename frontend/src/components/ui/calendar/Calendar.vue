<script setup lang="ts">
import type { CalendarRootEmits, CalendarRootProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import { reactiveOmit } from '@vueuse/core'
import { ChevronLeft, ChevronRight } from '@lucide/vue'
import {
  CalendarRoot, CalendarHeader, CalendarHeading, CalendarPrev, CalendarNext,
  CalendarGrid, CalendarGridHead, CalendarGridBody, CalendarGridRow,
  CalendarHeadCell, CalendarCell, CalendarCellTrigger, useForwardPropsEmits,
} from 'reka-ui'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const props = defineProps<CalendarRootProps & { class?: HTMLAttributes['class'] }>()
const emit = defineEmits<CalendarRootEmits>()
const forwarded = useForwardPropsEmits(reactiveOmit(props, 'class'), emit)
</script>

<template>
  <CalendarRoot v-slot="{ grid, weekDays }" v-bind="forwarded" data-slot="calendar" :class="cn('p-3', props.class)">
    <CalendarHeader class="flex items-center justify-between gap-2">
      <CalendarHeading class="text-sm font-medium" />
      <div class="flex items-center gap-1">
        <CalendarPrev as-child><Button type="button" variant="outline" size="icon-sm" data-slot="calendar-prev"><ChevronLeft /></Button></CalendarPrev>
        <CalendarNext as-child><Button type="button" variant="outline" size="icon-sm" data-slot="calendar-next"><ChevronRight /></Button></CalendarNext>
      </div>
    </CalendarHeader>
    <CalendarGrid v-for="month in grid" :key="month.value.toString()" class="mt-4 w-full border-collapse">
      <CalendarGridHead>
        <CalendarGridRow class="flex">
          <CalendarHeadCell v-for="day in weekDays" :key="day" class="w-8 text-xs font-normal text-muted-foreground">{{ day }}</CalendarHeadCell>
        </CalendarGridRow>
      </CalendarGridHead>
      <CalendarGridBody>
        <CalendarGridRow v-for="(week, index) in month.rows" :key="index" class="mt-2 flex">
          <CalendarCell v-for="day in week" :key="day.toString()" :date="day" class="p-0 text-center">
            <CalendarCellTrigger
              as="button"
              type="button"
              :day="day"
              :month="month.value"
              :class="cn(buttonVariants({ variant: 'ghost' }),
                         'size-8 p-0 font-normal',
                         'data-[selected]:bg-primary data-[selected]:text-primary-foreground data-[selected]:hover:bg-primary data-[selected]:hover:text-primary-foreground',
                         '[&[data-today]:not([data-selected])]:bg-accent [&[data-today]:not([data-selected])]:text-accent-foreground',
                         'data-[outside-view]:text-muted-foreground data-[outside-view]:opacity-30 data-[disabled]:opacity-50')"
            />
          </CalendarCell>
        </CalendarGridRow>
      </CalendarGridBody>
    </CalendarGrid>
  </CalendarRoot>
</template>
