<script setup>
import { computed, onUnmounted, watch } from 'vue'
import { Eye, Xmark } from '@iconoir/vue'
import { useI18n } from 'vue-i18n'
import BaseButton from '@/components/ui/BaseButton.vue'
import RegionFlag from '@/components/ui/RegionFlag.vue'
import { formatBytes } from '@/utils/utils'
import { billingOf, cnyValue, formatMoney } from '@/utils/billing.js'
import { normalizeDueTimestamp } from '@/utils/dueDate.js'

const props = defineProps({
  open: { type: Boolean, default: false },
  items: { type: Array, default: () => [] },
  exchange: { type: Object, default: null }
})
const emit = defineEmits(['close', 'remove', 'inspect'])
const { t, locale } = useI18n()

const cycleText = (item) => {
  const cycle = billingOf(item).cycle
  return cycle ? t(`billing-${cycle.replace('_', '-')}`) : ''
}

const priceText = (item) => {
  const billing = billingOf(item)
  if (!billing.structured) return item?.price === null || item?.price === undefined || item?.price === '' ? t('market-compare-missing') : String(item.price)
  const cycle = cycleText(item)
  const original = `${formatMoney(billing.amount, billing.currency, locale.value)}${cycle ? `/${cycle}` : ''}`
  if (billing.currency === 'CNY') return original
  const cny = cnyValue(billing.amount, billing.currency, props.exchange)
  return cny === null ? original : `${original} · ≈ ${formatMoney(cny, 'CNY', locale.value)}${cycle ? `/${cycle}` : ''}`
}

const speedText = (item) => {
  const hasInbound = item?.net_in_speed !== null && item?.net_in_speed !== undefined && item?.net_in_speed !== ''
  const hasOutbound = item?.net_out_speed !== null && item?.net_out_speed !== undefined && item?.net_out_speed !== ''
  if (!hasInbound && !hasOutbound) return t('market-compare-missing')
  const inbound = Number(item?.net_in_speed || 0)
  const outbound = Number(item?.net_out_speed || 0)
  if (!Number.isFinite(inbound) || !Number.isFinite(outbound)) return t('market-compare-missing')
  return `↓ ${formatBytes(inbound)}/s · ↑ ${formatBytes(outbound)}/s`
}

const trafficText = (item) => {
  const hasInbound = item?.net_in_transfer !== null && item?.net_in_transfer !== undefined && item?.net_in_transfer !== ''
  const hasOutbound = item?.net_out_transfer !== null && item?.net_out_transfer !== undefined && item?.net_out_transfer !== ''
  if (!hasInbound && !hasOutbound) return t('market-compare-missing')
  const inbound = Number(item?.net_in_transfer || 0)
  const outbound = Number(item?.net_out_transfer || 0)
  if (!Number.isFinite(inbound) || !Number.isFinite(outbound)) return t('market-compare-missing')
  return `↓ ${formatBytes(inbound)} · ↑ ${formatBytes(outbound)}`
}

const dueText = (item) => {
  const value = normalizeDueTimestamp(item?.due_time)
  if (!value) return t('market-compare-missing')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('market-compare-missing')
  return new Intl.DateTimeFormat(locale.value, {
    timeZone: props.exchange?.time_zone || 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(date)
}

const regionText = (item) => item?.region || item?.region_code || t('market-compare-missing')
const statusText = (item) => item?.online ? t('online') : t('offline')

const rows = computed(() => [
  { key: 'status', label: t('market-compare-status'), value: statusText },
  { key: 'price', label: t('market-compare-price'), value: priceText },
  { key: 'network', label: t('market-compare-network'), value: speedText },
  { key: 'traffic', label: t('market-compare-traffic'), value: trafficText },
  { key: 'due', label: t('market-compare-due'), value: dueText },
  { key: 'region', label: t('market-compare-region'), value: regionText }
])

const close = () => emit('close')
const onKeydown = (event) => { if (event.key === 'Escape' && props.open) close() }

watch(() => props.open, (open) => {
  document.body.classList.toggle('market-compare-open', open)
  if (open) document.addEventListener('keydown', onKeydown)
  else document.removeEventListener('keydown', onKeydown)
}, { immediate: true })

onUnmounted(() => {
  document.body.classList.remove('market-compare-open')
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="market-compare-dialog">
      <button class="market-compare-dialog__overlay" type="button" :aria-label="t('common-close')" @click="close"></button>
      <section class="market-compare-dialog__panel glow-card" role="dialog" aria-modal="true" :aria-label="t('market-compare-title')">
        <header class="market-compare-dialog__head">
          <div><h2>{{ t('market-compare-title') }}</h2><p>{{ t('market-compare-hint') }}</p></div>
          <button type="button" :aria-label="t('common-close')" @click="close"><Xmark aria-hidden="true" /></button>
        </header>

        <div class="market-compare-table-wrap">
          <div class="market-compare-table" :style="{ '--compare-count': Math.max(items.length, 1) }">
            <div class="market-compare-table__corner">{{ t('market-compare-field') }}</div>
            <div v-for="item in items" :key="`head-${item.node_id}`" class="market-compare-server">
              <div class="market-compare-server__title"><RegionFlag :region="item.region_code || item.region" /><strong>{{ item.display_name || item.node_id }}</strong></div>
              <span>{{ item.node_id }}</span>
              <button type="button" :aria-label="t('market-compare-remove-name', { name: item.display_name || item.node_id })" @click="emit('remove', item.node_id)"><Xmark aria-hidden="true" /></button>
            </div>

            <template v-for="row in rows" :key="row.key">
              <div class="market-compare-row-label">{{ row.label }}</div>
              <div v-for="item in items" :key="`${row.key}-${item.node_id}`" class="market-compare-value" :class="{ 'is-missing': row.value(item) === t('market-compare-missing'), 'is-online': row.key === 'status' && item.online, 'is-offline': row.key === 'status' && !item.online }">{{ row.value(item) }}</div>
            </template>

            <div class="market-compare-row-label">{{ t('market-compare-action') }}</div>
            <div v-for="item in items" :key="`action-${item.node_id}`" class="market-compare-value is-action">
              <BaseButton size="sm" :disabled="!Number(item.last_seen || 0)" @click="emit('inspect', item)"><Eye aria-hidden="true" />{{ t('market-view-monitor') }}</BaseButton>
            </div>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.market-compare-dialog{position:fixed;inset:0;z-index:260;display:grid;padding:20px;place-items:center}.market-compare-dialog__overlay{position:absolute;inset:0;border:0;background:rgba(0,0,0,.72);cursor:default}.market-compare-dialog__panel{position:relative;display:flex;width:min(980px,100%);max-height:min(760px,88vh);flex-direction:column;overflow:hidden;border:1px solid var(--color-border-2,#e5e6eb);border-radius:10px;background:var(--color-bg-2,#fff);box-shadow:0 24px 72px rgba(15,23,42,.28)}.market-compare-dialog__panel.glow-card::after{inset:0}.market-compare-dialog__head{display:flex;padding:18px 20px;align-items:flex-start;justify-content:space-between;gap:16px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.market-compare-dialog__head h2{margin:0;color:var(--color-text-1,#1d2129);font-size:18px}.market-compare-dialog__head p{margin:4px 0 0;color:var(--color-text-3,#86909c);font-size:12px}.market-compare-dialog__head>button,.market-compare-server>button{display:grid;width:30px;height:30px;flex:none;padding:0;place-items:center;border:0;border-radius:6px;background:transparent;color:var(--color-text-2,#4e5969);cursor:pointer}.market-compare-dialog__head>button:hover,.market-compare-server>button:hover{background:var(--color-fill-2,#f2f3f5)}.market-compare-dialog__head svg,.market-compare-server svg{width:18px;height:18px}.market-compare-table-wrap{min-height:0;overflow:auto;overscroll-behavior:contain}.market-compare-table{display:grid;grid-template-columns:120px repeat(var(--compare-count),minmax(220px,1fr));min-width:max-content}.market-compare-table__corner,.market-compare-row-label{position:sticky;left:0;z-index:3;padding:13px 14px;border-right:1px solid var(--color-border-2,#e5e6eb);border-bottom:1px solid var(--color-border-2,#e5e6eb);background:var(--color-fill-1,#f7f8fa);color:var(--color-text-3,#86909c);font-size:12px;font-weight:700}.market-compare-table__corner{top:0;z-index:5}.market-compare-server{position:sticky;top:0;z-index:4;display:grid;grid-template-columns:minmax(0,1fr) auto;padding:12px 14px;border-right:1px solid var(--color-border-2,#e5e6eb);border-bottom:1px solid var(--color-border-2,#e5e6eb);background:var(--color-bg-2,#fff)}.market-compare-server__title{display:flex;min-width:0;align-items:center;gap:7px}.market-compare-server__title strong{overflow:hidden;color:var(--color-text-1,#1d2129);font-size:14px;text-overflow:ellipsis;white-space:nowrap}.market-compare-server>span{grid-column:1;overflow:hidden;color:var(--color-text-3,#86909c);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.market-compare-server>button{grid-column:2;grid-row:1/3}.market-compare-value{display:flex;min-height:48px;padding:12px 14px;align-items:center;border-right:1px solid var(--color-border-2,#e5e6eb);border-bottom:1px solid var(--color-border-2,#e5e6eb);color:var(--color-text-2,#4e5969);font-size:12px;line-height:1.5}.market-compare-value.is-missing{color:var(--color-text-4,#c9cdd4);font-style:italic}.market-compare-value.is-online{color:#00a870;font-weight:700}.market-compare-value.is-offline{color:#d03050;font-weight:700}.market-compare-value.is-action{justify-content:flex-start}.market-compare-value.is-action svg{width:15px;height:15px}:global(body[arco-theme='dark'] .market-compare-dialog__panel),:global(body[arco-theme='dark'] .market-compare-server){background:#1f1f20}:global(body[arco-theme='dark'] .market-compare-table__corner),:global(body[arco-theme='dark'] .market-compare-row-label){background:#171718}:global(body[arco-theme='dark'] .market-compare-dialog__head>button:hover),:global(body[arco-theme='dark'] .market-compare-server>button:hover){background:#303033}
@media(max-width:640px){.market-compare-dialog{padding:8px}.market-compare-dialog__panel{max-height:calc(100dvh - 16px);border-radius:8px}.market-compare-dialog__head{padding:14px}.market-compare-dialog__head p{max-width:260px}.market-compare-table{grid-template-columns:92px repeat(var(--compare-count),minmax(178px,1fr))}.market-compare-table__corner,.market-compare-row-label{padding:11px 9px}.market-compare-server,.market-compare-value{padding:10px}.market-compare-value{min-height:46px}}
</style>

<style>
body.market-compare-open{overflow:hidden!important}
</style>
