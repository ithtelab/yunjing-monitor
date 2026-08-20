<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import axios from 'axios'
import { useI18n } from 'vue-i18n'
import IconClockCircle from '@arco-design/web-vue/es/icon/icon-clock-circle'
import IconEye from '@arco-design/web-vue/es/icon/icon-eye'
import IconCheckCircle from '@arco-design/web-vue/es/icon/icon-check-circle'
import RegionFlag from '@/components/ui/RegionFlag.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import { hostArea, hostDisplayName } from '@/utils/monitor'
import { billingOf, cnyValue, formatMoney, monthlyCNY, renewalCNY } from '@/utils/billing.js'
import { calendarDaysUntil, normalizeDueTimestamp } from '@/utils/dueDate.js'

const { t, locale } = useI18n()
const props = defineProps({
  hosts: { type: Array, default: () => [] },
  hostInfo: { type: Object, default: () => ({}) },
  apiBase: { type: String, default: '' }
})
const emit = defineEmits(['locate'])

const WARN = 80
const DANGER = 90
const expirySection = ref(null)
const alertSection = ref(null)
const exchange = ref(null)
const now = ref(Date.now())
const filter = ref(localStorage.getItem('alert-filter') || 'all')
const sortMode = ref(localStorage.getItem('alert-sort') || 'due-asc')
const showUnset = ref(false)
let clockTimer
const timeZone = computed(() => exchange.value?.time_zone || 'Asia/Shanghai')

const infoOf = (name) => {
  const key = String(name || '')
  return props.hostInfo[key] || props.hostInfo[key.trim()] || {}
}
const hasReport = (item) => Number(item?.TimeStamp) > 0

const allAssets = computed(() => (props.hosts || []).map((item) => {
  const key = String(item?.Host?.Name || '')
  const info = infoOf(key)
  const due = normalizeDueTimestamp(info.due_time)
  return {
    key,
    item,
    info,
    name: hostDisplayName(item),
    region: hostArea(item),
    due,
    days: due ? calendarDaysUntil(due, now.value, timeZone.value) : null,
    date: due ? formatDate(due) : '',
    billing: billingOf(info),
    seller: info.seller || ''
  }
}).filter((row) => row.key))

const expiryRows = computed(() => allAssets.value.filter((row) => row.due))
const unsetRows = computed(() => allAssets.value.filter((row) => !row.due))
const expiredCount = computed(() => expiryRows.value.filter((row) => row.days < 0).length)
const weekCount = computed(() => expiryRows.value.filter((row) => row.days >= 0 && row.days <= 7).length)
const monthCount = computed(() => expiryRows.value.filter((row) => row.days > 7 && row.days <= 30).length)

const filteredExpiry = computed(() => {
  const rows = expiryRows.value.filter((row) => {
    if (filter.value === 'expired') return row.days < 0
    if (filter.value === 'week') return row.days >= 0 && row.days <= 7
    if (filter.value === 'month') return row.days > 7 && row.days <= 30
    if (filter.value === 'priced') return row.billing.structured
    if (filter.value === 'unpriced') return !row.billing.structured
    return true
  })
  const output = [...rows]
  const price = (row) => row.billing.structured
    ? cnyValue(row.billing.amount, row.billing.currency, exchange.value)
    : null
  const priceCompare = (a, b, direction) => {
    const left = price(a)
    const right = price(b)
    if (left === null && right === null) return a.name.localeCompare(b.name, locale.value)
    if (left === null) return 1
    if (right === null) return -1
    return direction * (left - right)
  }
  switch (sortMode.value) {
    case 'due-desc': output.sort((a, b) => b.days - a.days); break
    case 'price-asc': output.sort((a, b) => priceCompare(a, b, 1)); break
    case 'price-desc': output.sort((a, b) => priceCompare(a, b, -1)); break
    case 'name': output.sort((a, b) => a.name.localeCompare(b.name, locale.value)); break
    default: output.sort((a, b) => a.days - b.days)
  }
  return output
})

const assetMetrics = computed(() => {
  let priced = 0
  let monthly = 0
  let renewal30 = 0
  allAssets.value.forEach((row) => {
    if (row.billing.structured) priced += 1
    const monthlyValue = monthlyCNY(row.billing, exchange.value)
    if (monthlyValue !== null) monthly += monthlyValue
    if (row.days !== null && row.days >= 0 && row.days <= 30) {
      const renewal = renewalCNY(row.billing, exchange.value)
      if (renewal !== null) renewal30 += renewal
    }
  })
  return { total: allAssets.value.length, priced, monthly, annual: monthly * 12, renewal30 }
})

const expiryLevel = (days) => days < 0 ? 'is-expired' : days <= 7 ? 'is-danger' : days <= 30 ? 'is-warning' : 'is-ok'
const expiryBadge = (days) => days < 0
  ? t('alert-expired-days', { days: Math.abs(days) })
  : days === 0 ? t('alert-today') : t('alert-days-left', { days })

const cycleLabel = (cycle) => cycle ? t(`billing-${cycle.replace('_', '-')}`) : ''
const originalPrice = (row) => {
  if (!row.billing.structured) return row.info.price || t('billing-unpriced')
  const price = formatMoney(row.billing.amount, row.billing.currency, locale.value)
  const cycle = cycleLabel(row.billing.cycle)
  return cycle ? `${price}/${cycle}` : price
}
const convertedPrice = (row) => {
  if (!row.billing.structured || row.billing.currency === 'CNY') return ''
  const value = cnyValue(row.billing.amount, row.billing.currency, exchange.value)
  if (value === null) return ''
  const cycle = cycleLabel(row.billing.cycle)
  return `≈ ${formatMoney(value, 'CNY', locale.value)}${cycle ? `/${cycle}` : ''}`
}

const levelOf = (value) => value >= DANGER ? 'is-danger' : 'is-warning'
const alertRows = computed(() => {
  const rows = []
  ;(props.hosts || []).forEach((item) => {
    if (!hasReport(item) || !item.status) return
    const metrics = []
    const push = (label, value) => {
      const numeric = Number(value)
      if (!Number.isFinite(numeric) || numeric < WARN) return
      metrics.push({ label, value: numeric, level: levelOf(numeric) })
    }
    const memTotal = Number(item.Host?.MemTotal) || 0
    const swapTotal = Number(item.Host?.SwapTotal) || 0
    const diskTotal = Number(item.State?.DiskTotal) || 0
    push('CPU', item.State?.CPU)
    if (memTotal > 0) push(t('chart-memory'), ((item.State?.MemUsed || 0) / memTotal) * 100)
    if (swapTotal > 0) push('Swap', ((item.State?.SwapUsed || 0) / swapTotal) * 100)
    if (diskTotal > 0) push(t('chart-disk'), ((item.State?.DiskUsed || 0) / diskTotal) * 100)
    ;(item.State?.Disks || []).forEach((disk) => push(t('alert-partition', { mount: disk.mount }), disk.used_percent))
    if (!metrics.length) return
    metrics.sort((a, b) => b.value - a.value)
    rows.push({ key: String(item.Host?.Name || ''), name: hostDisplayName(item), region: hostArea(item), metrics, max: metrics[0] })
  })
  return rows.sort((a, b) => b.max.value - a.max.value)
})

const clockDate = computed(() => new Intl.DateTimeFormat(locale.value, {
  timeZone: timeZone.value, year: 'numeric', month: '2-digit', day: '2-digit', weekday: 'short'
}).format(now.value))
const clockTime = computed(() => new Intl.DateTimeFormat(locale.value, {
  timeZone: timeZone.value, hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
}).format(now.value))
const exchangeText = computed(() => {
  if (!(Number(exchange.value?.rate) > 0)) return t('exchange-unavailable')
  const updated = new Intl.DateTimeFormat(locale.value, { timeZone: timeZone.value, hour: '2-digit', minute: '2-digit' }).format(Number(exchange.value.updated_at) * 1000)
  return t(exchange.value.stale ? 'exchange-cached' : 'exchange-live', { rate: Number(exchange.value.rate).toFixed(4), time: updated })
})

function formatDate(value) {
  return new Intl.DateTimeFormat(locale.value, { timeZone: timeZone.value, year: 'numeric', month: '2-digit', day: '2-digit' }).format(value)
}
const setFilter = (value) => {
  filter.value = value
  localStorage.setItem('alert-filter', value)
  expirySection.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
const setSort = (value) => {
  sortMode.value = value
  localStorage.setItem('alert-sort', value)
}
const scrollTo = (target) => target?.scrollIntoView({ behavior: 'smooth', block: 'start' })

onMounted(async () => {
  clockTimer = window.setInterval(() => { now.value = Date.now() }, 1000)
  try {
    const response = await axios.get(`${props.apiBase || ''}/api/site/exchange-rate`)
    exchange.value = response.data
  } catch {}
})
onBeforeUnmount(() => window.clearInterval(clockTimer))
</script>

<template>
  <div class="site-alerts">
    <header class="sa-heading">
      <h1>{{ t('nav-stats') }}</h1>
      <div class="sa-clock"><IconClockCircle /><span>{{ clockDate }}</span><strong>{{ clockTime }}</strong><small>{{ timeZone }}</small></div>
    </header>

    <section class="sa-summary-wrap" :aria-label="t('alert-summary')">
      <button class="sa-summary-item is-expired" @click="setFilter('expired')"><span>{{ t('alert-expired') }}</span><strong>{{ expiredCount }}</strong><small>{{ t('alert-servers') }}</small></button>
      <button class="sa-summary-item is-danger" @click="setFilter('week')"><span>{{ t('alert-week') }}</span><strong>{{ weekCount }}</strong><small>{{ t('alert-servers') }}</small></button>
      <button class="sa-summary-item is-warning" @click="setFilter('month')"><span>{{ t('alert-month') }}</span><strong>{{ monthCount }}</strong><small>{{ t('alert-servers') }}</small></button>
      <button class="sa-summary-item" :class="alertRows.length ? 'is-danger' : 'is-ok'" @click="scrollTo(alertSection)"><span>{{ t('alert-resource') }}</span><strong>{{ alertRows.length }}</strong><small>{{ t('alert-servers') }}</small></button>
    </section>

    <section class="sa-assets-section">
      <div class="sa-section-heading"><div><h2>{{ t('asset-overview') }}</h2><small>{{ exchangeText }}</small></div></div>
      <div class="sa-assets">
        <div><span>{{ t('asset-total') }}</span><strong>{{ assetMetrics.total }}</strong></div>
        <button @click="setFilter('priced')"><span>{{ t('asset-priced') }}</span><strong>{{ assetMetrics.priced }}/{{ assetMetrics.total }}</strong></button>
        <div><span>{{ t('asset-monthly') }}</span><strong>{{ formatMoney(assetMetrics.monthly, 'CNY', locale) }}</strong></div>
        <div><span>{{ t('asset-annual') }}</span><strong>{{ formatMoney(assetMetrics.annual, 'CNY', locale) }}</strong></div>
        <div><span>{{ t('asset-renewal-30') }}</span><strong>{{ formatMoney(assetMetrics.renewal30, 'CNY', locale) }}</strong></div>
      </div>
    </section>

    <section ref="expirySection" class="sa-section">
      <div class="sa-section-heading sa-plan-heading">
        <div><h2>{{ t('alert-upcoming') }}</h2><small>{{ t('alert-upcoming-sub', { count: expiryRows.length }) }}</small></div>
        <BaseInput class="sa-sort" as="select" :model-value="sortMode" :aria-label="t('alert-sort')" @update:model-value="setSort">
          <option value="due-asc">{{ t('alert-sort-due-asc') }}</option>
          <option value="due-desc">{{ t('alert-sort-due-desc') }}</option>
          <option value="price-asc">{{ t('alert-sort-price-asc') }}</option>
          <option value="price-desc">{{ t('alert-sort-price-desc') }}</option>
          <option value="name">{{ t('alert-sort-name') }}</option>
        </BaseInput>
      </div>
      <div class="sa-filters">
        <button v-for="item in [
          ['all', 'alert-filter-all'], ['expired', 'alert-expired'], ['week', 'alert-week'],
          ['month', 'alert-month'], ['priced', 'alert-filter-priced'], ['unpriced', 'billing-unpriced']
        ]" :key="item[0]" :class="{ active: filter === item[0] }" @click="setFilter(item[0])">{{ t(item[1]) }}</button>
      </div>

      <div v-if="filteredExpiry.length" class="sa-list">
        <article v-for="row in filteredExpiry" :key="row.key" class="sa-row sa-expiry-row glow-card">
          <div class="sa-node"><RegionFlag :region="row.region" /><div><strong>{{ row.name }}</strong><small v-if="row.seller">{{ row.seller }}</small></div></div>
          <span class="sa-badge" :class="expiryLevel(row.days)">{{ expiryBadge(row.days) }}</span>
          <div class="sa-date"><span>{{ t('alert-due-date') }}</span><strong>{{ row.date }}</strong></div>
          <div class="sa-price" :class="{ 'is-unpriced': !row.billing.structured }"><strong>{{ originalPrice(row) }}</strong><small v-if="convertedPrice(row)">{{ convertedPrice(row) }}</small></div>
          <button class="sa-monitor" :disabled="!hasReport(row.item)" @click="emit('locate', row.key)"><IconEye /><span>{{ t('alert-monitor') }}</span></button>
        </article>
      </div>
      <div v-else class="sa-empty">{{ t('alert-filter-empty') }}</div>

      <div v-if="unsetRows.length" class="sa-unset">
        <button @click="showUnset = !showUnset">{{ t('alert-unset-count', { count: unsetRows.length }) }} <span>{{ showUnset ? '−' : '+' }}</span></button>
        <p v-if="showUnset">{{ unsetRows.map((row) => row.name).join(t('common-list-sep')) }}</p>
      </div>
    </section>

    <section ref="alertSection" class="sa-section">
      <div class="sa-section-heading"><div><h2>{{ t('alert-resource') }}</h2><small>{{ t('alert-resource-sub', { warn: WARN, danger: DANGER }) }}</small></div></div>
      <div v-if="alertRows.length" class="sa-list">
        <article v-for="row in alertRows" :key="row.key" class="sa-row sa-alert-row glow-card">
          <div class="sa-node"><RegionFlag :region="row.region" /><div><strong>{{ row.name }}</strong><small :class="row.max.level">{{ t('alert-max', { label: row.max.label, value: row.max.value.toFixed(0) }) }}</small></div></div>
          <div class="sa-metrics"><span v-for="metric in row.metrics" :key="metric.label" :class="metric.level">{{ metric.label }} {{ metric.value.toFixed(0) }}%</span></div>
          <div class="sa-bar"><span :class="row.max.level" :style="{ width: `${Math.min(100, row.max.value)}%` }"></span></div>
          <button class="sa-monitor" @click="emit('locate', row.key)"><IconEye /><span>{{ t('alert-monitor') }}</span></button>
        </article>
      </div>
      <div v-else class="sa-ok"><IconCheckCircle /><span>{{ t('alert-all-ok') }}</span></div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.site-alerts { margin: 16px 14px 0; }
.sa-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 8px 0 16px; }
.sa-heading h1 { margin: 0; font-size: 18px; font-weight: 800; }
.sa-clock { display: flex; align-items: center; gap: 7px; color: #4e5969; font-size: 12px; font-variant-numeric: tabular-nums; }
.sa-clock svg { width: 16px; height: 16px; color: #165dff; }
.sa-clock strong { font-size: 15px; color: #1d2129; }
.sa-clock small { color: #8a94a3; }

.sa-summary-wrap { display: grid; grid-template-columns: repeat(4, 1fr); margin-bottom: 20px; border-block: 1px solid rgba(23,33,47,.1); }
.sa-summary-item { display: grid; grid-template-columns: 1fr auto; grid-template-rows: auto auto; align-items: center; min-height: 72px; padding: 10px 18px; border: 0; border-right: 1px solid rgba(23,33,47,.1); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.sa-summary-item:last-child { border-right: 0; }
.sa-summary-item:hover { background: rgba(22,93,255,.04); }
.sa-summary-item span { font-size: 12px; font-weight: 700; color: #6b7684; }
.sa-summary-item strong { grid-row: 1 / 3; grid-column: 2; font-size: 25px; font-variant-numeric: tabular-nums; }
.sa-summary-item small { font-size: 11px; color: #8a94a3; }
.sa-summary-item.is-expired strong { color: #7f1d1d; }
.sa-summary-item.is-danger strong { color: #f53f3f; }
.sa-summary-item.is-warning strong { color: #ff7d00; }
.sa-summary-item.is-ok strong { color: #00b42a; }

.sa-assets-section, .sa-section { margin-bottom: 24px; scroll-margin-top: 102px; }
.sa-section-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.sa-section-heading h2 { margin: 0 0 3px; font-size: 15px; }
.sa-section-heading small { color: #8a94a3; font-size: 11px; }
.sa-assets { display: grid; grid-template-columns: repeat(5, 1fr); border: 1px solid rgba(23,33,47,.08); border-radius: 8px; overflow: hidden; background: #fff; }
.sa-assets > div, .sa-assets > button { display: flex; flex-direction: column; justify-content: center; gap: 4px; min-height: 72px; padding: 10px 14px; border: 0; border-right: 1px solid rgba(23,33,47,.08); background: transparent; color: inherit; text-align: left; }
.sa-assets > *:last-child { border-right: 0; }
.sa-assets button { cursor: pointer; }
.sa-assets button:hover { background: rgba(22,93,255,.04); }
.sa-assets span { color: #8a94a3; font-size: 11px; }
.sa-assets strong { font-size: 17px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }

.sa-plan-heading { align-items: center; }
.sa-sort { width: 180px; flex: none; }
.sa-filters { display: flex; gap: 6px; margin-bottom: 10px; overflow-x: auto; }
.sa-filters button { flex: none; height: 28px; padding: 0 10px; border: 1px solid rgba(23,33,47,.1); border-radius: 999px; background: transparent; color: #4e5969; font-size: 12px; cursor: pointer; }
.sa-filters button.active { border-color: #165dff; background: rgba(22,93,255,.08); color: #165dff; }
.sa-list { display: flex; flex-direction: column; gap: 8px; }
.sa-row { border: 1px solid rgba(23,33,47,.08); border-radius: 8px; background: #fff; }
.sa-expiry-row { display: grid; grid-template-columns: minmax(210px, 1.3fr) 96px 135px minmax(150px, .9fr) 106px; align-items: center; gap: 12px; min-height: 62px; padding: 10px 14px; }
.sa-node { display: flex; align-items: center; gap: 8px; min-width: 0; }
.sa-node > div { min-width: 0; display: flex; flex-direction: column; }
.sa-node strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.sa-node small { color: #8a94a3; font-size: 11px; }
.sa-node small.is-danger { color: #f53f3f; }
.sa-node small.is-warning { color: #ff7d00; }
.sa-badge { justify-self: start; padding: 3px 9px; border-radius: 999px; font-size: 11px; font-weight: 700; white-space: nowrap; }
.sa-badge.is-expired { background: #fef2f2; color: #7f1d1d; }
.sa-badge.is-danger { background: #ffece8; color: #f53f3f; }
.sa-badge.is-warning { background: #fff7e8; color: #ff7d00; }
.sa-badge.is-ok { background: #e8ffea; color: #00b42a; }
.sa-date, .sa-price { display: flex; flex-direction: column; min-width: 0; font-variant-numeric: tabular-nums; }
.sa-date span { color: #8a94a3; font-size: 10px; }
.sa-date strong, .sa-price strong { font-size: 12px; font-weight: 650; }
.sa-price small { color: #00a870; font-size: 10px; }
.sa-price.is-unpriced strong { color: #a0a7b2; font-weight: 500; }
.sa-monitor { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 34px; padding: 0 12px; border: 1px solid #111827; border-radius: 7px; background: #111827; color: #fff; font-size: 12px; font-weight: 650; cursor: pointer; }
.sa-monitor:hover { background: #165dff; border-color: #165dff; }
.sa-monitor:disabled { opacity: .4; cursor: not-allowed; }
.sa-monitor svg { width: 15px; height: 15px; }
.sa-empty { min-height: 46px; display: grid; place-items: center; border: 1px dashed rgba(23,33,47,.14); border-radius: 8px; color: #8a94a3; font-size: 12px; }
.sa-unset { margin-top: 8px; font-size: 11px; color: #8a94a3; }
.sa-unset button { padding: 0; border: 0; background: transparent; color: inherit; cursor: pointer; }
.sa-unset p { margin: 6px 0 0; line-height: 1.6; }

.sa-alert-row { display: grid; grid-template-columns: minmax(210px, 1fr) minmax(240px, 1.4fr) 120px 106px; align-items: center; gap: 12px; padding: 10px 14px; }
.sa-metrics { display: flex; flex-wrap: wrap; gap: 5px; }
.sa-metrics span { padding: 2px 7px; border-radius: 5px; font-size: 11px; font-weight: 650; }
.sa-metrics .is-danger { background: #ffece8; color: #f53f3f; }
.sa-metrics .is-warning { background: #fff7e8; color: #ff7d00; }
.sa-bar { height: 5px; border-radius: 999px; background: #f2f3f5; overflow: hidden; }
.sa-bar span { display: block; height: 100%; border-radius: inherit; }
.sa-bar .is-danger { background: #f53f3f; }
.sa-bar .is-warning { background: #ff7d00; }
.sa-ok { display: flex; align-items: center; justify-content: center; gap: 8px; min-height: 48px; border: 1px solid rgba(0,180,42,.25); border-radius: 8px; background: rgba(0,180,42,.04); color: #00a63c; font-size: 12px; font-weight: 650; }
.sa-ok svg { width: 17px; height: 17px; }

body[arco-theme='dark'] {
  .sa-clock { color: #a1a1aa; }
  .sa-clock strong { color: #fafafa; }
  .sa-summary-wrap { border-color: rgba(255,255,255,.1); }
  .sa-summary-item { border-color: rgba(255,255,255,.1); }
  .sa-assets, .sa-row { background: #1f1f20; border-color: rgba(255,255,255,.1); color: #f5f5f5; }
  .sa-assets > div, .sa-assets > button { border-color: rgba(255,255,255,.1); }
  .sa-filters button { border-color: rgba(255,255,255,.14); color: #d4d4d8; }
  .sa-filters button.active { color: #60a5fa; border-color: #3b82f6; background: rgba(59,130,246,.16); }
  .sa-monitor { background: #f5f5f5; border-color: #f5f5f5; color: #111; }
  .sa-badge.is-expired { background: rgba(127,29,29,.4); color: #fca5a5; }
  .sa-badge.is-danger, .sa-metrics .is-danger { background: rgba(245,63,63,.18); color: #ff8f8f; }
  .sa-badge.is-warning, .sa-metrics .is-warning { background: rgba(255,125,0,.18); color: #ffb65c; }
  .sa-badge.is-ok { background: rgba(0,180,42,.18); color: #4be38a; }
  .sa-bar { background: rgba(255,255,255,.1); }
}

@media (max-width: 760px) {
  .site-alerts { margin-inline: 8px; }
  .sa-heading { align-items: flex-start; }
  .sa-clock { display: grid; grid-template-columns: auto auto; gap: 1px 5px; }
  .sa-clock svg, .sa-clock small { display: none; }
  .sa-clock strong { grid-column: 2; }
  .sa-summary-wrap { grid-template-columns: repeat(2, 1fr); }
  .sa-summary-item { min-height: 56px; padding: 8px 12px; border-bottom: 1px solid rgba(23,33,47,.1); }
  .sa-summary-item:nth-child(2) { border-right: 0; }
  .sa-summary-item:nth-child(n+3) { border-bottom: 0; }
  .sa-summary-item strong { font-size: 21px; }
  .sa-assets { grid-template-columns: repeat(2, 1fr); }
  .sa-assets > div, .sa-assets > button { min-height: 58px; border-bottom: 1px solid rgba(23,33,47,.08); }
  .sa-assets > *:nth-child(2n) { border-right: 0; }
  .sa-assets > *:last-child { grid-column: 1 / -1; border-bottom: 0; }
  .sa-section-heading { align-items: flex-start; }
  .sa-plan-heading { flex-direction: column; }
  .sa-sort { width: 100%; }
  .sa-expiry-row { grid-template-columns: minmax(0, 1fr) auto; grid-template-rows: auto auto auto; gap: 7px 10px; padding: 11px 12px; }
	.sa-expiry-row, .sa-alert-row { scroll-margin-bottom: 112px; }
  .sa-node { grid-column: 1; grid-row: 1; }
  .sa-badge { grid-column: 2; grid-row: 1; }
  .sa-date { grid-column: 1; grid-row: 2; }
  .sa-price { grid-column: 1; grid-row: 3; }
  .sa-monitor { grid-column: 2; grid-row: 2 / 4; align-self: end; min-width: 76px; height: 40px; }
  .sa-alert-row { grid-template-columns: 1fr auto; }
  .sa-alert-row .sa-node { grid-column: 1 / -1; }
  .sa-metrics { grid-column: 1 / -1; }
  .sa-alert-row .sa-bar { grid-column: 1; }
  .sa-alert-row .sa-monitor { grid-column: 2; grid-row: 3 / 5; }
}
</style>
