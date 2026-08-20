<!-- 公共日期选择器：统一上架、Owner 编辑的日期交互，并随视口滚动保持在安全区域。 -->
<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import IconCalendar from '@arco-design/web-vue/es/icon/icon-calendar'
import IconLeft from '@arco-design/web-vue/es/icon/icon-left'
import IconRight from '@arco-design/web-vue/es/icon/icon-right'
import IconClose from '@arco-design/web-vue/es/icon/icon-close'

const props = defineProps({
  modelValue: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  clearable: { type: Boolean, default: true },
  id: { type: String, default: undefined }
})
const emit = defineEmits(['update:modelValue', 'change'])
const { t, locale } = useI18n()

const root = ref(null)
const panel = ref(null)
const open = ref(false)
const panelStyle = ref({})
const cursor = ref(new Date())
const focusedValue = ref('')
let placementFrame = 0
const safeTop = 76
const safeBottom = 80

const pad = (value) => String(value).padStart(2, '0')
const dateValue = (date) => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
const parseDate = (value) => {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value || '')) return null
  const [year, month, day] = value.split('-').map(Number)
  const date = new Date(year, month - 1, day)
  return date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day ? date : null
}
const todayValue = dateValue(new Date())

const localeTag = computed(() => ({ zh: 'zh-CN', en: 'en-US', ja: 'ja-JP', ko: 'ko-KR', de: 'de-DE' })[locale.value] || locale.value)
const displayValue = computed(() => {
  const date = parseDate(props.modelValue)
  return date ? new Intl.DateTimeFormat(localeTag.value, { year: 'numeric', month: 'long', day: 'numeric' }).format(date) : ''
})
const monthLabel = computed(() => new Intl.DateTimeFormat(localeTag.value, { year: 'numeric', month: 'long' }).format(cursor.value))
const yearOptions = computed(() => {
  const currentYear = new Date().getFullYear()
  const selectedYear = cursor.value.getFullYear()
  const start = Math.min(currentYear - 5, selectedYear)
  const end = Math.max(currentYear + 50, selectedYear)
  return Array.from({ length: end - start + 1 }, (_, index) => start + index)
})
const monthOptions = computed(() => Array.from({ length: 12 }, (_, month) => ({
  value: month,
  label: new Intl.DateTimeFormat(localeTag.value, { month: 'short' }).format(new Date(2024, month, 1))
})))
const weekdays = computed(() => {
  const monday = new Date(2024, 0, 1)
  return Array.from({ length: 7 }, (_, index) => new Intl.DateTimeFormat(localeTag.value, { weekday: 'short' }).format(new Date(2024, 0, monday.getDate() + index)))
})
const days = computed(() => {
  const year = cursor.value.getFullYear()
  const month = cursor.value.getMonth()
  const first = new Date(year, month, 1)
  const offset = (first.getDay() + 6) % 7
  const start = new Date(year, month, 1 - offset)
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start.getFullYear(), start.getMonth(), start.getDate() + index)
    const value = dateValue(date)
    return { date, value, label: date.getDate(), outside: date.getMonth() !== month }
  })
})

const focusDay = async (value) => {
  focusedValue.value = value
  await nextTick()
  root.value?.querySelector(`[data-date="${value}"]`)?.focus()
}
const updatePanelPosition = () => {
  if (!open.value || !root.value || !panel.value) return
  window.cancelAnimationFrame(placementFrame)
  placementFrame = window.requestAnimationFrame(() => {
    if (!open.value || !root.value || !panel.value) return
    const bounds = root.value.getBoundingClientRect()
    const viewportBottom = window.innerHeight - safeBottom
    if (bounds.bottom <= safeTop || bounds.top >= viewportBottom) {
      close()
      return
    }

    const gap = 7
    const availableAbove = Math.max(0, bounds.top - safeTop - gap)
    const availableBelow = Math.max(0, viewportBottom - bounds.bottom - gap)
    const naturalHeight = panel.value.scrollHeight + 2
    let placeBelow = availableBelow >= naturalHeight
    if (!placeBelow && availableAbove < naturalHeight) {
      placeBelow = availableBelow >= availableAbove
    }
    const availableHeight = placeBelow ? availableBelow : availableAbove
    const panelHeight = Math.min(naturalHeight, availableHeight)
    const panelWidth = panel.value.getBoundingClientRect().width
    const left = Math.min(
      Math.max(16, bounds.left),
      Math.max(16, window.innerWidth - panelWidth - 16)
    )
    const top = placeBelow ? bounds.bottom + gap : bounds.top - panelHeight - gap
    panelStyle.value = {
      left: `${Math.round(left)}px`,
      top: `${Math.round(Math.max(safeTop, top))}px`,
      maxHeight: `${Math.max(120, Math.floor(availableHeight))}px`
    }
  })
}
const show = async () => {
  if (props.disabled) return
  const bounds = root.value?.getBoundingClientRect()
  const viewportBottom = window.innerHeight - safeBottom
  if (bounds && (bounds.top < safeTop || bounds.bottom > viewportBottom)) {
    root.value.scrollIntoView({ block: 'center', inline: 'nearest' })
    await new Promise((resolve) => window.requestAnimationFrame(resolve))
  }
  const selected = parseDate(props.modelValue) || new Date()
  cursor.value = new Date(selected.getFullYear(), selected.getMonth(), 1)
  open.value = true
  await nextTick()
  updatePanelPosition()
  await focusDay(dateValue(selected))
}
const close = () => {
  open.value = false
  panelStyle.value = {}
  window.cancelAnimationFrame(placementFrame)
}
const select = (day) => {
  emit('update:modelValue', day.value)
  emit('change', day.value)
  close()
}
const clear = () => {
  emit('update:modelValue', '')
  emit('change', '')
  close()
}
const moveMonth = (delta) => {
  cursor.value = new Date(cursor.value.getFullYear(), cursor.value.getMonth() + delta, 1)
}
const setYear = (event) => {
  cursor.value = new Date(Number(event.target.value), cursor.value.getMonth(), 1)
}
const setMonth = (event) => {
  cursor.value = new Date(cursor.value.getFullYear(), Number(event.target.value), 1)
}
const chooseToday = () => {
  const today = new Date()
  select({ value: dateValue(today) })
}
const handleCalendarKey = (event) => {
  const current = parseDate(focusedValue.value) || parseDate(props.modelValue) || new Date()
  let next = null
  if (event.key === 'ArrowLeft') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() - 1)
  if (event.key === 'ArrowRight') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() + 1)
  if (event.key === 'ArrowUp') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() - 7)
  if (event.key === 'ArrowDown') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() + 7)
  if (event.key === 'Home') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() - ((current.getDay() + 6) % 7))
  if (event.key === 'End') next = new Date(current.getFullYear(), current.getMonth(), current.getDate() + (6 - ((current.getDay() + 6) % 7)))
  if (event.key === 'PageUp') next = new Date(current.getFullYear(), current.getMonth() - 1, current.getDate())
  if (event.key === 'PageDown') next = new Date(current.getFullYear(), current.getMonth() + 1, current.getDate())
  if (!next) return
  event.preventDefault()
  cursor.value = new Date(next.getFullYear(), next.getMonth(), 1)
  focusDay(dateValue(next))
}
const outside = (event) => {
  if (!root.value?.contains(event.target)) close()
}
const escape = (event) => {
  if (event.key === 'Escape') close()
}

watch(() => props.modelValue, (value) => {
  const parsed = parseDate(value)
  if (parsed && open.value) cursor.value = new Date(parsed.getFullYear(), parsed.getMonth(), 1)
})
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', escape)
  window.addEventListener('scroll', updatePanelPosition, true)
  window.addEventListener('resize', updatePanelPosition)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', escape)
  window.removeEventListener('scroll', updatePanelPosition, true)
  window.removeEventListener('resize', updatePanelPosition)
  window.cancelAnimationFrame(placementFrame)
})
</script>

<template>
  <div ref="root" class="date-picker" :class="{ 'is-open': open, 'is-disabled': disabled }">
    <button
      :id="id"
      class="date-picker__trigger"
      type="button"
      :disabled="disabled"
      :aria-label="t('date-picker-label')"
      :aria-expanded="open"
      aria-haspopup="dialog"
      @click="open ? close() : show()"
    >
      <IconCalendar aria-hidden="true" />
      <span :class="{ 'is-placeholder': !displayValue }">{{ displayValue || t('date-picker-placeholder') }}</span>
      <IconClose
        v-if="clearable && modelValue"
        class="date-picker__clear"
        :aria-label="t('date-picker-clear')"
        role="button"
        tabindex="0"
        @click.stop="clear"
        @keydown.enter.stop="clear"
        @keydown.space.prevent.stop="clear"
      />
    </button>

    <div v-if="open" ref="panel" class="date-picker__panel" role="dialog" :aria-label="t('date-picker-label')" :style="panelStyle">
      <header class="date-picker__header">
        <button type="button" :title="t('date-picker-prev')" :aria-label="t('date-picker-prev')" @click="moveMonth(-1)"><IconLeft /></button>
        <div class="date-picker__selectors" :aria-label="monthLabel">
          <select :value="cursor.getFullYear()" :aria-label="t('date-picker-year')" @change="setYear">
            <option v-for="year in yearOptions" :key="year" :value="year">{{ year }}</option>
          </select>
          <select :value="cursor.getMonth()" :aria-label="t('date-picker-month')" @change="setMonth">
            <option v-for="month in monthOptions" :key="month.value" :value="month.value">{{ month.label }}</option>
          </select>
        </div>
        <button type="button" :title="t('date-picker-next')" :aria-label="t('date-picker-next')" @click="moveMonth(1)"><IconRight /></button>
      </header>
      <div class="date-picker__week" aria-hidden="true">
        <span v-for="weekday in weekdays" :key="weekday">{{ weekday }}</span>
      </div>
      <div class="date-picker__grid" role="grid" @keydown="handleCalendarKey">
        <button
          v-for="day in days"
          :key="day.value"
          type="button"
          role="gridcell"
          :data-date="day.value"
          :tabindex="day.value === focusedValue ? 0 : -1"
          :class="{ 'is-outside': day.outside, 'is-today': day.value === todayValue, 'is-selected': day.value === modelValue }"
          :aria-selected="day.value === modelValue"
          @focus="focusedValue = day.value"
          @click="select(day)"
        >{{ day.label }}</button>
      </div>
      <footer class="date-picker__footer">
        <button v-if="clearable" type="button" @click="clear">{{ t('date-picker-clear') }}</button>
        <button type="button" @click="chooseToday">{{ t('date-picker-today') }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.date-picker { position: relative; width: 100%; }
.date-picker__trigger {
  width: 100%; min-height: 38px; display: flex; align-items: center; gap: 9px;
  padding: 7px 11px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 6px;
  background: var(--color-bg-2, #fff); color: var(--color-text-1, #1d2129); text-align: left; cursor: pointer;
}
.date-picker__trigger:hover, .date-picker.is-open .date-picker__trigger { border-color: #4080ff; }
.date-picker__trigger:focus-visible { outline: 2px solid rgba(22, 93, 255, .28); outline-offset: 2px; }
.date-picker__trigger > svg { width: 16px; height: 16px; flex: none; color: #165dff; }
.date-picker__trigger span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.date-picker__trigger .is-placeholder { color: var(--color-text-3, #86909c); }
.date-picker__trigger .date-picker__clear { color: var(--color-text-3, #86909c); }
.date-picker__panel {
  position: fixed; z-index: 260; width: min(310px, calc(100vw - 32px)); overflow-y: auto; overscroll-behavior: contain;
  padding: 12px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 8px;
  background: var(--color-bg-2, #fff); color: var(--color-text-1, #1d2129);
  box-shadow: 0 14px 34px rgba(15, 23, 42, .16);
}
.date-picker__header { display: grid; grid-template-columns: 32px 1fr 32px; align-items: center; gap: 6px; margin-bottom: 8px; }
.date-picker__selectors { display: flex; min-width: 0; align-items: center; justify-content: center; gap: 5px; }
.date-picker__selectors select { min-width: 0; height: 30px; padding: 0 22px 0 8px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 5px; background: var(--color-bg-2, #fff); color: inherit; font-size: 12px; font-weight: 650; cursor: pointer; }
.date-picker__selectors select:first-child { width: 82px; }
.date-picker__selectors select:last-child { width: 72px; }
.date-picker__selectors select:focus-visible { outline: 2px solid rgba(22, 93, 255, .3); outline-offset: 1px; }
.date-picker__header button, .date-picker__grid button { border: 0; background: transparent; color: inherit; cursor: pointer; }
.date-picker__header button { width: 32px; height: 32px; border-radius: 6px; display: grid; place-items: center; }
.date-picker__header button:hover, .date-picker__grid button:hover { background: var(--color-fill-2, #f2f3f5); }
.date-picker__week, .date-picker__grid { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); }
.date-picker__week span { padding: 6px 0; text-align: center; font-size: 11px; color: var(--color-text-3, #86909c); }
.date-picker__grid button { aspect-ratio: 1; min-width: 0; border-radius: 6px; font-size: 12px; }
.date-picker__grid button.is-outside { color: var(--color-text-4, #c9cdd4); }
.date-picker__grid button.is-today { color: #165dff; font-weight: 700; box-shadow: inset 0 0 0 1px #94bfff; }
.date-picker__grid button.is-selected { background: #165dff; color: #fff; box-shadow: none; font-weight: 700; }
.date-picker__grid button:focus-visible { outline: 2px solid #4080ff; outline-offset: -2px; }
.date-picker__footer { display: flex; justify-content: space-between; gap: 8px; margin-top: 8px; padding-top: 9px; border-top: 1px solid var(--color-border-2, #e5e6eb); }
.date-picker__footer button { border: 0; padding: 5px 8px; border-radius: 5px; background: transparent; color: #165dff; cursor: pointer; font-size: 12px; }
.date-picker__footer button:hover { background: var(--color-fill-2, #f2f3f5); }
.is-disabled { opacity: .6; }
body[arco-theme='dark'] .date-picker__trigger, body[arco-theme='dark'] .date-picker__panel { background: #232324; border-color: rgba(255,255,255,.12); color: #f2f3f5; }
body[arco-theme='dark'] .date-picker__selectors select { background: #2b2b2c; border-color: rgba(255,255,255,.14); }
body[arco-theme='dark'] .date-picker__panel { box-shadow: 0 16px 38px rgba(0,0,0,.48); }
</style>
