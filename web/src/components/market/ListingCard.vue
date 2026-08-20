<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import IconEye from '@arco-design/web-vue/es/icon/icon-eye'
import { BadgeCheck, CheckSquare, CheckSquareSolid, ShoppingBag, Star, StarSolid, TriangleFlag } from '@iconoir/vue'
import RegionFlag from '@/components/ui/RegionFlag.vue'
import { formatBytes } from '@/utils/utils'
import { billingOf, cnyValue, formatMoney, monthlyCNY } from '@/utils/billing.js'
import { calendarDaysUntil, normalizeDueTimestamp } from '@/utils/dueDate.js'

const { t, locale } = useI18n()

const props = defineProps({
  item: { type: Object, required: true },
  exchange: { type: Object, default: null },
  favorite: { type: Boolean, default: false },
  compared: { type: Boolean, default: false },
  compareDisabled: { type: Boolean, default: false },
  marketMedian: { type: Number, default: 0 },
  marketActions: { type: Boolean, default: true }
})
const emit = defineEmits(['inspect', 'toggle-favorite', 'toggle-compare', 'report', 'order'])

const monitorAvailable = computed(() => Number(props.item?.last_seen || 0) > 0)
const billing = computed(() => billingOf(props.item))
const cycleText = computed(() => billing.value.cycle ? t(`billing-${billing.value.cycle.replace('_', '-')}`) : '')
const originalPrice = computed(() => {
	if (!billing.value.structured) return props.item?.price || t('billing-unpriced')
	return `${formatMoney(billing.value.amount, billing.value.currency, locale.value)}${cycleText.value ? `/${cycleText.value}` : ''}`
})
const cnyPrice = computed(() => {
	if (!billing.value.structured || billing.value.currency === 'CNY') return ''
	const value = cnyValue(billing.value.amount, billing.value.currency, props.exchange)
	return value === null ? '' : `≈ ${formatMoney(value, 'CNY', locale.value)}${cycleText.value ? `/${cycleText.value}` : ''}`
})
const sellerTrust = computed(() => {
  const trust = props.item?.seller_trust || props.item?.sellerTrust
  if (!trust || typeof trust !== 'object') return null
  const level = String(trust.level || 'standard').toLowerCase()
  if (!trust.verified && level === 'standard') return null
  return { ...trust, level }
})
const trustLabel = computed(() => {
  if (!sellerTrust.value) return ''
  if (sellerTrust.value.verified) return t('market-seller-verified')
  return t(`market-seller-${sellerTrust.value.level === 'trusted' ? 'trusted' : 'watch'}`)
})
const pricePosition = computed(() => {
  const median = Number(props.marketMedian || 0)
  const current = monthlyCNY(billing.value, props.exchange)
  if (!(median > 0) || !(current > 0)) return null
  const ratio = current / median
  if (ratio <= 0.85) return { tone: 'good', label: t('market-price-below', { percent: Math.round((1 - ratio) * 100) }) }
  if (ratio >= 1.25) return { tone: 'high', label: t('market-price-above', { percent: Math.round((ratio - 1) * 100) }) }
  return { tone: 'fair', label: t('market-price-near') }
})
const openMonitor = () => {
  if (monitorAvailable.value) emit('inspect')
}

const listingTypeLabel = computed(() => {
  const type = String(props.item?.listing_type || '')
  if (type === 'rent') return t('market-type-rent')
  if (type === 'sale') return t('market-type-sale')
  if (type === 'transfer') return t('market-type-transfer')
  return type || '—'
})

const statusText = computed(() => (props.item?.online ? t('online') : t('offline')))
const dueDate = computed(() => {
  const value = normalizeDueTimestamp(props.item?.due_time)
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const timeZone = props.exchange?.time_zone || 'Asia/Shanghai'
  const days = calendarDaysUntil(value, Date.now(), timeZone)
  if (days === null) return null
  return {
    text: new Intl.DateTimeFormat(locale.value, { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(date),
	expired: days < 0,
	days
  }
})
const regionText = computed(() => {
  const name = props.item?.region || props.item?.region_code || ''
  return name || t('market-no-region')
})

const specsLine = computed(() => {
  if (props.item?.specs) return props.item.specs
  const parts = []
  if (props.item?.logical_cores) parts.push(t('monitor-cores', { n: props.item.logical_cores }))
  if (props.item?.mem_total) parts.push(`${t('chart-memory')} ${formatBytes(props.item.mem_total)}`)
  if (props.item?.disk_total) parts.push(`${t('chart-disk')} ${formatBytes(props.item.disk_total)}`)
  return parts.join(' · ') || t('market-no-specs')
})
</script>

<template>
  <article
    class="listing-card glow-card"
    :class="{ 'is-pinned': item.pinned, 'is-monitorable': monitorAvailable }"
    :role="monitorAvailable ? 'button' : undefined"
    :tabindex="monitorAvailable ? 0 : undefined"
    :aria-label="monitorAvailable ? t('market-monitor-aria', { name: item.display_name || item.node_id }) : undefined"
    @click="openMonitor"
    @keydown.enter="openMonitor"
    @keydown.space.prevent="openMonitor"
  >
    <header class="listing-card__head">
      <div class="listing-card__title-row">
        <h3 class="listing-card__title">{{ item.display_name || item.node_id }}</h3>
        <span v-if="item.pinned" class="listing-card__pin">{{ t('market-pinned') }}</span>
        <span v-if="sellerTrust" class="listing-card__trust" :class="`is-${sellerTrust.level}`" :title="sellerTrust.note || trustLabel"><BadgeCheck aria-hidden="true" />{{ trustLabel }}</span>
        <div v-if="marketActions" class="listing-card__actions">
          <button type="button" :class="{ 'is-active': favorite }" :aria-pressed="favorite" :aria-label="t(favorite ? 'market-unfavorite-name' : 'market-favorite-name', { name: item.display_name || item.node_id })" :title="t(favorite ? 'market-unfavorite' : 'market-favorite')" @click.stop="emit('toggle-favorite')" @keydown.stop>
            <StarSolid v-if="favorite" aria-hidden="true" />
            <Star v-else aria-hidden="true" />
          </button>
          <button type="button" :class="{ 'is-active': compared }" :disabled="compareDisabled" :aria-pressed="compared" :aria-label="t(compared ? 'market-compare-remove-name' : 'market-compare-add-name', { name: item.display_name || item.node_id })" :title="compareDisabled ? t('market-compare-limit') : t(compared ? 'market-compare-remove' : 'market-compare-add')" @click.stop="emit('toggle-compare')" @keydown.stop>
            <CheckSquareSolid v-if="compared" aria-hidden="true" />
            <CheckSquare v-else aria-hidden="true" />
          </button>
          <button type="button" :aria-label="t('market-report-name', { name: item.display_name || item.node_id })" :title="t('market-report-action')" @click.stop="emit('report')" @keydown.stop>
            <TriangleFlag aria-hidden="true" />
          </button>
        </div>
      </div>
      <div class="listing-card__meta">
        <RegionFlag :region="item.region_code || item.region" />
        <span>{{ regionText }}</span>
        <span class="dot">·</span>
        <span :class="item.online ? 'is-online' : 'is-offline'">{{ statusText }}</span>
      </div>
    </header>

    <p class="listing-card__specs">{{ specsLine }}</p>
    <p v-if="dueDate" class="listing-card__due" :class="{ 'is-expired': dueDate.expired }">
	  {{ dueDate.expired ? t('market-expired') : t('market-due') }} · {{ dueDate.text }}
	  <span>{{ dueDate.expired ? t('alert-expired-days', { days: Math.abs(dueDate.days) }) : (dueDate.days === 0 ? t('alert-today') : t('alert-days-left', { days: dueDate.days })) }}</span>
    </p>
    <p v-if="item.description" class="listing-card__desc">{{ item.description }}</p>

    <footer class="listing-card__foot">
      <div class="listing-card__price">
		<div><strong>{{ originalPrice }}</strong><small v-if="cnyPrice">{{ cnyPrice }}</small></div>
        <span class="listing-card__type">{{ listingTypeLabel }}</span>
        <span v-if="pricePosition" class="listing-card__price-position" :class="`is-${pricePosition.tone}`">{{ pricePosition.label }}</span>
      </div>
      <div class="listing-card__foot-meta">
        <div class="listing-card__contact" :title="item.contact">
          {{ t('market-contact', { contact: item.contact || '—' }) }}
        </div>
        <span v-if="monitorAvailable" class="listing-card__monitor">
          <IconEye aria-hidden="true" />
          {{ t('market-view-monitor') }}
        </span>
      </div>
      <button class="listing-card__order" type="button" @click.stop="emit('order')" @keydown.stop>
        <ShoppingBag aria-hidden="true" />
        {{ t('market-order-action') }}
      </button>
    </footer>
  </article>
</template>

<style scoped>
.listing-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  border-radius: 14px;
  border: 1px solid var(--color-border-2, rgba(0, 0, 0, 0.08));
  background: var(--color-bg-2, #fff);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.listing-card:hover {
  border-color: var(--color-primary-light-3, #94bfff);
  box-shadow: 0 6px 18px rgba(22, 93, 255, 0.08);
}
.listing-card.is-monitorable {
  cursor: pointer;
}
.listing-card.is-monitorable:focus-visible {
  outline: 2px solid #165dff;
  outline-offset: 3px;
}
.listing-card.is-pinned {
  border-color: #f7ba1e;
}
.listing-card__head {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.listing-card__title-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.listing-card__title {
  flex: 1 1 120px;
  min-width: 0;
  overflow: hidden;
  margin: 0;
  font-size: 16px;
  font-weight: 650;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.listing-card__pin {
  font-size: 12px;
  padding: 1px 6px;
  border-radius: 999px;
  background: #fff7e8;
  color: #d25f00;
}
.listing-card__trust{display:inline-flex;height:20px;padding:0 6px;flex:none;align-items:center;gap:3px;border-radius:4px;background:rgba(0,180,42,.08);color:#008f24;font-size:10px;font-weight:700}.listing-card__trust svg{width:13px;height:13px}.listing-card__trust.is-watch{background:rgba(255,125,0,.1);color:#b35400}
.listing-card__actions{display:flex;margin-left:auto;flex:none;gap:4px}.listing-card__actions button{display:grid;width:30px;height:30px;padding:0;place-items:center;border:1px solid var(--color-border-2,#e5e6eb);border-radius:7px;background:var(--color-bg-2,#fff);color:var(--color-text-3,#86909c);cursor:pointer;transition:background .15s,border-color .15s,color .15s}.listing-card__actions button:hover:not(:disabled){border-color:#94bfff;color:#165dff}.listing-card__actions button.is-active{border-color:#165dff;background:rgba(22,93,255,.08);color:#165dff}.listing-card__actions button:first-child.is-active{border-color:#f7ba1e;background:#fff7e8;color:#d97706}.listing-card__actions button:disabled{cursor:not-allowed;opacity:.38}.listing-card__actions svg{width:17px;height:17px}
.listing-card__meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--color-text-3, #86909c);
}
.listing-card__meta .is-online { color: #00b42a; }
.listing-card__meta .is-offline { color: #f53f3f; }
.listing-card__specs,
.listing-card__due,
.listing-card__desc {
  margin: 0;
  font-size: 13px;
  color: var(--color-text-2, #4e5969);
  line-height: 1.5;
}
.listing-card__due { color: #0e7490; }
.listing-card__due.is-expired { color: #d03050; font-weight: 600; }
.listing-card__desc {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.listing-card__foot {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px dashed var(--color-border-2, rgba(0, 0, 0, 0.08));
}
.listing-card__price {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
}
.listing-card__price strong {
  font-size: 18px;
  color: #165dff;
}
.listing-card__price > div { display: flex; flex-direction: column; gap: 2px; }
.listing-card__price small { color: #00a870; font-size: 11px; font-weight: 650; }
.listing-card__due span { margin-left: 6px; font-weight: 650; }
.listing-card__type {
  font-size: 12px;
  color: var(--color-text-3, #86909c);
}
.listing-card__price-position{margin-left:auto;padding:2px 5px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);color:var(--color-text-3,#86909c);font-size:10px;font-weight:700}.listing-card__price-position.is-good{background:rgba(0,180,42,.08);color:#008f24}.listing-card__price-position.is-high{background:rgba(255,125,0,.1);color:#b35400}
.listing-card__contact {
  min-width: 0;
  font-size: 13px;
  color: var(--color-text-2, #4e5969);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.listing-card__foot-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.listing-card__order{display:inline-flex;width:100%;height:34px;align-items:center;justify-content:center;gap:6px;border:1px solid #b9c8e8;border-radius:8px;background:rgba(22,93,255,.05);color:#165dff;font-size:13px;font-weight:650;cursor:pointer;transition:background .15s,border-color .15s}.listing-card__order:hover{border-color:#165dff;background:rgba(22,93,255,.1)}.listing-card__order svg{width:16px;height:16px}
.listing-card__monitor {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 5px;
  color: #165dff;
  font-size: 12px;
  font-weight: 650;
}
.listing-card__monitor svg {
  width: 15px;
  height: 15px;
}
body[arco-theme='dark'] .listing-card {
  background: #232324;
  border-color: rgba(255, 255, 255, 0.08);
}
body[arco-theme='dark'] .listing-card__pin {
  background: #3d2e12;
  color: #f7ba1e;
}
body[arco-theme='dark'] .listing-card__actions button{border-color:rgba(255,255,255,.12);background:#1b1b1c}body[arco-theme='dark'] .listing-card__actions button.is-active{background:rgba(59,130,246,.16);color:#60a5fa}body[arco-theme='dark'] .listing-card__actions button:first-child.is-active{background:#3d2e12;color:#f7ba1e}
</style>
