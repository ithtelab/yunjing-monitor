<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import { StarSolid, Xmark } from '@iconoir/vue'
import ListingCard from './ListingCard.vue'
import AdvertisementCard from './AdvertisementCard.vue'
import MarketCompareDialog from './MarketCompareDialog.vue'
import { buildMarketFeed } from '@/utils/marketAds.js'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import AppDialog from '@/components/ui/AppDialog.vue'
import { formatMoney, monthlyCNY, normalizedPrice, billingOf } from '@/utils/billing.js'

const { t, locale } = useI18n()
import MarketGuide from './MarketGuide.vue'

const props = defineProps({
  apiBase: { type: String, default: '' },
  dark: { type: Boolean, default: false }
})
const emit = defineEmits(['navigate', 'inspect'])

const loading = ref(true)
const listings = ref([])
const categories = ref([])
const advertisements = ref([])
const adSettings = ref({})
const mobile = ref(false)
const region = ref('all')
const keyword = ref('')
const showGuide = ref(false)
const exchange = ref(null)
const overviewFilter = ref('all')
const sortMode = ref(localStorage.getItem('market-sort') || 'default')
const favoritesOnly = ref(false)
const showCompare = ref(false)
const reportItem = ref(null)
const reportSaving = ref(false)
const reportForm = reactive({ category: 'inaccurate', message: '' })
const orderItem = ref(null)
const orderSaving = ref(false)
const orderForm = reactive({ buyer_contact: '', message: '' })
const FAVORITES_KEY = 'monitor-market-favorites'
const readFavoriteIDs = () => {
  try {
    const values = JSON.parse(localStorage.getItem(FAVORITES_KEY) || '[]')
    return Array.isArray(values) ? [...new Set(values.map((value) => String(value || '').trim()).filter(Boolean))] : []
  } catch {
    return []
  }
}
const favoriteIDs = ref(readFavoriteIDs())
const compareIDs = ref([])
const listingError = ref(false)
const categoryError = ref(false)
const adError = ref(false)
const exchangeError = ref(false)
const hasLoadError = computed(() => listingError.value || categoryError.value || adError.value || exchangeError.value)
const primaryLoadFailed = computed(() => listingError.value && !listings.value.length)

const filtered = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  const rows = listings.value.filter((item) => {
    if (favoritesOnly.value && !favoriteIDs.value.includes(String(item.node_id || ''))) return false
    if (region.value !== 'all') {
      const code = String(item.region_code || '').toLowerCase()
      const name = String(item.region || '').toLowerCase()
      if (code !== region.value.toLowerCase() && name !== region.value.toLowerCase()) return false
    }
	if (overviewFilter.value === 'online' && !item.online) return false
	if (overviewFilter.value === 'rent' && item.listing_type !== 'rent') return false
	if (overviewFilter.value === 'sale' && item.listing_type === 'rent') return false
    if (!q) return true
    const hay = [item.display_name, item.specs, item.description, item.price, item.contact, item.listing_type]
      .map((v) => String(v || '').toLowerCase())
      .join(' ')
    return hay.includes(q)
  })
	const sorted = [...rows]
	const numeric = (value) => Number(value || 0)
	const dueValue = (item) => numeric(item.due_time) || Number.POSITIVE_INFINITY
	const name = (item) => String(item.display_name || item.node_id || '')
	const priceCompare = (a, b, direction) => {
	  const left = normalizedPrice(a, exchange.value)
	  const right = normalizedPrice(b, exchange.value)
	  const leftMissing = !Number.isFinite(left)
	  const rightMissing = !Number.isFinite(right)
	  if (leftMissing && rightMissing) return name(a).localeCompare(name(b), locale.value)
	  if (leftMissing) return 1
	  if (rightMissing) return -1
	  return direction * (left - right)
	}
	switch (sortMode.value) {
	  case 'latest': sorted.sort((a, b) => numeric(b.updated_at) - numeric(a.updated_at)); break
	  case 'name': sorted.sort((a, b) => name(a).localeCompare(name(b), locale.value)); break
	  case 'network': sorted.sort((a, b) => numeric(b.net_in_speed) + numeric(b.net_out_speed) - numeric(a.net_in_speed) - numeric(a.net_out_speed)); break
	  case 'traffic': sorted.sort((a, b) => numeric(b.net_in_transfer) + numeric(b.net_out_transfer) - numeric(a.net_in_transfer) - numeric(a.net_out_transfer)); break
	  case 'price-asc': sorted.sort((a, b) => priceCompare(a, b, 1)); break
	  case 'price-desc': sorted.sort((a, b) => priceCompare(a, b, -1)); break
	  case 'due': sorted.sort((a, b) => dueValue(a) - dueValue(b)); break
	}
	return sorted
})
const feed = computed(() => buildMarketFeed(filtered.value, advertisements.value, adSettings.value, mobile.value))
const overview = computed(() => ({
	total: listings.value.length,
	online: listings.value.filter((item) => item.online).length,
	rent: listings.value.filter((item) => item.listing_type === 'rent').length,
	sale: listings.value.filter((item) => item.listing_type !== 'rent').length
}))
const marketMedian = computed(() => {
	const values = listings.value
		.map((item) => monthlyCNY(billingOf(item), exchange.value))
		.filter((value) => Number.isFinite(value) && value > 0)
		.sort((a, b) => a - b)
	if (!values.length) return 0
	const middle = Math.floor(values.length / 2)
	return values.length % 2 ? values[middle] : (values[middle - 1] + values[middle]) / 2
})
const marketMedianText = computed(() => marketMedian.value > 0 ? formatMoney(marketMedian.value, 'CNY', locale.value) : '')
const favoriteCount = computed(() => listings.value.filter((item) => favoriteIDs.value.includes(String(item.node_id || ''))).length)
const comparedListings = computed(() => compareIDs.value.map((id) => listings.value.find((item) => String(item.node_id || '') === id)).filter(Boolean))

const persistFavorites = () => localStorage.setItem(FAVORITES_KEY, JSON.stringify(favoriteIDs.value))
const toggleFavorite = (item) => {
	const id = String(item?.node_id || '')
	if (!id) return
	favoriteIDs.value = favoriteIDs.value.includes(id) ? favoriteIDs.value.filter((value) => value !== id) : [...favoriteIDs.value, id]
	persistFavorites()
}
const toggleCompare = (item) => {
	const id = String(item?.node_id || '')
	if (!id) return
	if (compareIDs.value.includes(id)) {
		compareIDs.value = compareIDs.value.filter((value) => value !== id)
	} else if (compareIDs.value.length < 3) {
		compareIDs.value = [...compareIDs.value, id]
	}
	if (!compareIDs.value.length) showCompare.value = false
}
const clearCompare = () => {
	compareIDs.value = []
	showCompare.value = false
}
const inspectCompared = (item) => {
	showCompare.value = false
	emit('inspect', item)
}
const openReport = (item) => {
	reportItem.value = item
	reportForm.category = 'inaccurate'
	reportForm.message = ''
}
const closeReport = () => {
	if (!reportSaving.value) reportItem.value = null
}
const submitReport = async () => {
	if (!reportItem.value || reportForm.message.trim().length < 10) {
		Message.warning(t('market-report-min'))
		return
	}
	reportSaving.value = true
	try {
		await axios.post(`${props.apiBase || ''}/api/market/reports`, {
			listing_node_id: reportItem.value.node_id,
			category: reportForm.category,
			message: reportForm.message.trim()
		})
		Message.success(t('market-report-success'))
		reportItem.value = null
	} catch (error) {
		Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('market-report-fail'))
	} finally {
		reportSaving.value = false
	}
}

const openOrder = (item) => {
	orderItem.value = item
	orderForm.buyer_contact = ''
	orderForm.message = ''
}
const closeOrder = () => {
	if (!orderSaving.value) orderItem.value = null
}
const submitOrder = async () => {
	if (!orderItem.value || orderForm.buyer_contact.trim().length < 3) {
		Message.warning(t('market-order-contact-required'))
		return
	}
	orderSaving.value = true
	try {
		await axios.post(`${props.apiBase || ''}/api/account/orders`, {
			listing_node_id: orderItem.value.node_id,
			buyer_contact: orderForm.buyer_contact.trim(),
			message: orderForm.message.trim()
		}, { withCredentials: true })
		Message.success(t('market-order-success'))
		orderItem.value = null
	} catch (error) {
		if (error?.response?.status === 401) {
			Message.warning(t('market-order-login-required'))
			emit('navigate', 'owner')
			orderItem.value = null
		} else {
			Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('market-order-fail'))
		}
	} finally {
		orderSaving.value = false
	}
}

const setSort = (value) => {
	sortMode.value = value
	localStorage.setItem('market-sort', value)
}

let mediaQuery
let refreshTimer
let refreshing = false
const syncMobile = () => { mobile.value = Boolean(mediaQuery?.matches) }

const load = async ({ initial = false } = {}) => {
  if (refreshing) return
  refreshing = true
  if (initial) loading.value = true
  const base = props.apiBase || ''
  await Promise.all([
    axios.get(`${base}/api/market/listings`).then((response) => {
      listings.value = Array.isArray(response.data) ? response.data : []
      const available = new Set(listings.value.map((item) => String(item.node_id || '')))
      compareIDs.value = compareIDs.value.filter((id) => available.has(id))
      listingError.value = false
    }).catch(() => { listingError.value = true }),
    axios.get(`${base}/api/market/categories`).then((response) => {
      categories.value = Array.isArray(response.data) ? response.data : []
      categoryError.value = false
    }).catch(() => { categoryError.value = true }),
    axios.get(`${base}/api/market/ads`).then((response) => {
      advertisements.value = Array.isArray(response.data?.ads) ? response.data.ads : []
      adSettings.value = response.data?.settings || {}
      adError.value = false
    }).catch(() => { adError.value = true }),
    axios.get(`${base}/api/site/exchange-rate`).then((response) => {
      exchange.value = response.data || null
      exchangeError.value = false
    }).catch(() => { exchangeError.value = true })
  ])
  refreshing = false
  loading.value = false
}

const refreshWhenVisible = () => {
  if (document.visibilityState === 'visible') load()
}

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 640px)')
  syncMobile()
  mediaQuery.addEventListener?.('change', syncMobile)
  document.addEventListener('visibilitychange', refreshWhenVisible)
  refreshTimer = window.setInterval(refreshWhenVisible, 30000)
  load({ initial: true })
})
onBeforeUnmount(() => {
  mediaQuery?.removeEventListener?.('change', syncMobile)
  document.removeEventListener('visibilitychange', refreshWhenVisible)
  window.clearInterval(refreshTimer)
})
</script>

<template>
  <div class="market-page" :class="{ 'has-compare': comparedListings.length }">
    <header class="market-page__hero">
      <div>
        <h1>{{ t('market-title') }}</h1>
        <p>{{ t('market-subtitle') }}</p>
      </div>
      <div class="market-page__actions">
        <BaseButton @click="showGuide = true"><IconQuestionCircle /> {{ t('market-guide-link') }}</BaseButton>
        <BaseButton variant="primary" @click="emit('navigate', 'submit')">{{ t('market-submit') }}</BaseButton>
        <BaseButton @click="emit('navigate', 'owner')">{{ t('market-my-listings') }}</BaseButton>
      </div>
    </header>

    <MarketGuide :open="showGuide" @close="showGuide = false" @go-submit="emit('navigate', 'submit')" />

	<div class="market-overview" role="group" :aria-label="t('market-overview')">
	  <button :class="{ active: overviewFilter === 'all' }" @click="overviewFilter = 'all'"><span>{{ t('market-overview-total') }}</span><strong>{{ overview.total }}</strong></button>
	  <button :class="{ active: overviewFilter === 'online' }" @click="overviewFilter = 'online'"><span>{{ t('market-overview-online') }}</span><strong class="is-online">{{ overview.online }}</strong></button>
	  <button :class="{ active: overviewFilter === 'rent' }" @click="overviewFilter = 'rent'"><span>{{ t('market-overview-rent') }}</span><strong>{{ overview.rent }}</strong></button>
	  <button :class="{ active: overviewFilter === 'sale' }" @click="overviewFilter = 'sale'"><span>{{ t('market-overview-sale') }}</span><strong>{{ overview.sale }}</strong></button>
	</div>

    <div class="market-page__toolbar">
      <div class="market-page__chips">
        <button
          type="button"
          class="chip"
          :class="{ active: region === 'all' }"
          @click="region = 'all'"
        >{{ t('market-all') }}</button>
        <button
          v-for="cat in categories"
          :key="cat.id"
          type="button"
          class="chip"
          :class="{ active: region === cat.id }"
          @click="region = cat.id"
        >{{ cat.name }} ({{ cat.node_count }})</button>
      </div>
	  <div class="market-page__tools">
		<BaseInput v-model="keyword" type="search" :placeholder="t('market-search-placeholder')" />
		<BaseInput class="market-sort" as="select" :model-value="sortMode" :aria-label="t('market-sort-label')" @update:model-value="setSort">
		  <option value="default">{{ t('market-sort-default') }}</option>
		  <option value="latest">{{ t('market-sort-latest') }}</option>
		  <option value="name">{{ t('market-sort-name') }}</option>
		  <option value="network">{{ t('market-sort-network') }}</option>
		  <option value="traffic">{{ t('market-sort-traffic') }}</option>
		  <option value="price-asc">{{ t('market-sort-price-asc') }}</option>
		  <option value="price-desc">{{ t('market-sort-price-desc') }}</option>
		  <option value="due">{{ t('market-sort-due') }}</option>
		</BaseInput>
	  </div>
	  <div class="market-page__toolbar-meta">
	    <div class="market-result-meta"><small class="market-result-count">{{ t('market-result-count', { count: filtered.length }) }}</small><small v-if="marketMedianText" class="market-median">{{ t('market-price-median', { price: marketMedianText }) }}</small></div>
	    <button type="button" class="market-favorites-toggle" :class="{ 'is-active': favoritesOnly }" :aria-pressed="favoritesOnly" @click="favoritesOnly = !favoritesOnly"><StarSolid aria-hidden="true" />{{ t('market-favorites-count', { count: favoriteCount }) }}</button>
	  </div>
    </div>

    <EmptyState v-if="loading" loading />
    <div v-else-if="hasLoadError" class="market-load-status" role="status">
      <span>{{ t(primaryLoadFailed ? 'market-load-fail' : 'market-partial-load') }}</span>
      <BaseButton size="sm" @click="load()">{{ t('common-retry') }}</BaseButton>
    </div>
    <EmptyState v-else-if="!feed.length" :text="t(favoritesOnly ? 'market-favorites-empty' : 'market-empty')" />
    <div v-if="!loading && feed.length" class="market-page__grid">
      <template v-for="entry in feed" :key="entry.key">
		<ListingCard v-if="entry.kind === 'listing'" :item="entry.item" :exchange="exchange" :market-median="marketMedian" :favorite="favoriteIDs.includes(String(entry.item.node_id || ''))" :compared="compareIDs.includes(String(entry.item.node_id || ''))" :compare-disabled="compareIDs.length >= 3 && !compareIDs.includes(String(entry.item.node_id || ''))" @inspect="emit('inspect', entry.item)" @toggle-favorite="toggleFavorite(entry.item)" @toggle-compare="toggleCompare(entry.item)" @report="openReport(entry.item)" @order="openOrder(entry.item)" />
        <AdvertisementCard v-else :item="entry.item" :api-base="apiBase" />
      </template>
    </div>

    <div v-if="comparedListings.length" class="market-compare-bar" role="status">
      <div class="market-compare-bar__summary"><strong>{{ t('market-compare-selected', { count: comparedListings.length }) }}</strong><span>{{ comparedListings.length < 2 ? t('market-compare-needs-two') : t('market-compare-limit') }}</span></div>
      <div class="market-compare-bar__items">
        <span v-for="item in comparedListings" :key="item.node_id">{{ item.display_name || item.node_id }}<button type="button" :aria-label="t('market-compare-remove-name', { name: item.display_name || item.node_id })" @click="toggleCompare(item)"><Xmark aria-hidden="true" /></button></span>
      </div>
      <div class="market-compare-bar__actions"><BaseButton size="sm" @click="clearCompare">{{ t('market-compare-clear') }}</BaseButton><BaseButton variant="primary" size="sm" :disabled="comparedListings.length < 2" @click="showCompare = true">{{ t('market-compare-open') }}</BaseButton></div>
    </div>

    <MarketCompareDialog :open="showCompare" :items="comparedListings" :exchange="exchange" @close="showCompare = false" @remove="(id) => toggleCompare({ node_id: id })" @inspect="inspectCompared" />
    <AppDialog :open="Boolean(reportItem)" :title="t('market-report-title', { name: reportItem?.display_name || reportItem?.node_id || '' })" @close="closeReport">
      <form class="market-report-form" @submit.prevent="submitReport">
        <label>{{ t('market-report-category') }}</label>
        <BaseInput v-model="reportForm.category" as="select">
          <option value="fraud">{{ t('market-report-fraud') }}</option>
          <option value="inaccurate">{{ t('market-report-inaccurate') }}</option>
          <option value="unreachable">{{ t('market-report-unreachable') }}</option>
          <option value="prohibited">{{ t('market-report-prohibited') }}</option>
          <option value="other">{{ t('market-report-other') }}</option>
        </BaseInput>
        <label>{{ t('market-report-details') }}</label>
        <BaseInput v-model="reportForm.message" as="textarea" maxlength="1000" rows="4" :placeholder="t('market-report-placeholder')" />
      </form>
      <template #footer>
        <BaseButton :disabled="reportSaving" @click="closeReport">{{ t('common-cancel') }}</BaseButton>
        <BaseButton variant="primary" :disabled="reportSaving" @click="submitReport">{{ reportSaving ? t('common-saving') : t('market-report-submit') }}</BaseButton>
      </template>
    </AppDialog>
    <AppDialog :open="Boolean(orderItem)" :title="t('market-order-title', { name: orderItem?.display_name || orderItem?.node_id || '' })" @close="closeOrder">
      <form class="market-report-form" @submit.prevent="submitOrder">
        <p class="market-order-note">{{ t('market-order-note') }}</p>
        <label>{{ t('market-order-contact') }}</label>
        <BaseInput v-model="orderForm.buyer_contact" maxlength="120" :placeholder="t('market-order-contact-placeholder')" />
        <label>{{ t('market-order-message') }}</label>
        <BaseInput v-model="orderForm.message" as="textarea" maxlength="500" rows="4" :placeholder="t('market-order-message-placeholder')" />
      </form>
      <template #footer>
        <BaseButton :disabled="orderSaving" @click="closeOrder">{{ t('common-cancel') }}</BaseButton>
        <BaseButton variant="primary" :disabled="orderSaving" @click="submitOrder">{{ orderSaving ? t('common-saving') : t('market-order-submit') }}</BaseButton>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.market-page {
  max-width: 1100px;
  margin: 0 auto;
  padding: 24px 16px 0;
}
.market-page.has-compare { padding-bottom: 96px; }
.market-report-form{display:grid;gap:8px}.market-report-form label{margin-top:4px;color:var(--color-text-2,#4e5969);font-size:12px;font-weight:650}
.market-order-note{margin:0 0 4px;padding:10px 12px;border-left:3px solid #165dff;background:rgba(22,93,255,.06);color:var(--color-text-2,#4e5969);font-size:12px;line-height:1.6}
.market-page__hero {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 20px;
}
.market-page__hero h1 {
  margin: 0 0 6px;
  font-size: 24px;
}
.market-page__hero p {
  margin: 0;
  color: var(--color-text-3, #86909c);
  font-size: 14px;
}
.market-page__actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
/* 按钮已收口到公共组件 BaseButton，旧的 .btn / .btn-primary 移除 */
.market-page__toolbar {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 18px;
}
.market-overview {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  margin-bottom: 16px;
  border-block: 1px solid var(--color-border-2, #e5e6eb);
}
.market-overview button {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 58px;
  border: 0;
  border-right: 1px solid var(--color-border-2, #e5e6eb);
  background: transparent;
  color: var(--color-text-2, #4e5969);
  cursor: pointer;
}
.market-overview button:last-child { border-right: 0; }
.market-overview button.active { background: var(--color-fill-1, rgba(23, 33, 47, .04)); color: var(--color-text-1, #1d2129); }
.market-overview span { font-size: 12px; }
.market-overview strong { font-size: 20px; font-variant-numeric: tabular-nums; }
.market-overview strong.is-online { color: #00b42a; }
.market-page__tools { display: grid; grid-template-columns: minmax(0, 1fr) 180px; gap: 10px; }
.market-page__toolbar-meta{display:flex;min-width:0;align-items:center;justify-content:space-between;gap:10px}.market-result-meta{display:flex;min-width:0;align-items:center;gap:8px}.market-result-count{color:var(--color-text-3,#86909c)}.market-median{padding-left:8px;border-left:1px solid var(--color-border-2,#e5e6eb);color:#008f24;font-weight:650}.market-favorites-toggle{display:inline-flex;height:30px;padding:0 10px;align-items:center;gap:5px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:7px;background:transparent;color:var(--color-text-2,#4e5969);cursor:pointer;font:inherit;font-size:12px;font-weight:650}.market-favorites-toggle svg{width:15px;height:15px;color:#d97706}.market-favorites-toggle.is-active{border-color:#f7ba1e;background:#fff7e8;color:#b45309}
.market-load-status { display: flex; min-height: 42px; margin-bottom: 14px; padding: 8px 10px; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid rgba(255,125,0,.24); border-radius: 8px; background: rgba(255,125,0,.06); color: var(--color-text-2, #4e5969); font-size: 12px; }
.market-page__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.chip {
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: transparent;
  color: var(--color-text-2, #4e5969);
  cursor: pointer;
  font-size: 13px;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}
.chip:hover {
  border-color: var(--color-border-3, #c9cdd4);
  background: var(--color-fill-1, rgba(23, 33, 47, 0.04));
}
.chip.active {
  background: rgba(22, 93, 255, 0.1);
  border-color: #165dff;
  color: #165dff;
}
body[arco-theme='dark'] .chip {
  border-color: rgba(255, 255, 255, 0.14);
  background: #18181b;
  color: #d4d4d8;
}
body[arco-theme='dark'] .chip:hover {
  border-color: rgba(255, 255, 255, 0.28);
  background: #27272a;
  color: #fafafa;
}
body[arco-theme='dark'] .chip.active {
  border-color: #3b82f6;
  background: rgba(59, 130, 246, 0.16);
  color: #60a5fa;
}
/* 搜索框已收口到公共组件 BaseInput，旧的 .search 样式移除 */
.market-page__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}
.market-compare-bar{position:fixed;right:auto;bottom:58px;left:50%;z-index:150;display:grid;grid-template-columns:auto minmax(160px,1fr) auto;width:min(760px,calc(100% - 32px));min-height:58px;padding:8px 10px;align-items:center;gap:10px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:color-mix(in srgb,var(--color-bg-2,#fff) 94%,transparent);box-shadow:0 12px 36px rgba(15,23,42,.18);backdrop-filter:blur(12px);transform:translateX(-50%)}.market-compare-bar__summary{display:grid;gap:2px}.market-compare-bar__summary strong{color:var(--color-text-1,#1d2129);font-size:12px;white-space:nowrap}.market-compare-bar__summary span{color:var(--color-text-3,#86909c);font-size:10px;white-space:nowrap}.market-compare-bar__items{display:flex;min-width:0;gap:5px;overflow-x:auto;scrollbar-width:none}.market-compare-bar__items::-webkit-scrollbar{display:none}.market-compare-bar__items>span{display:inline-flex;max-width:180px;height:28px;padding:0 3px 0 8px;flex:none;align-items:center;gap:3px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:999px;background:var(--color-fill-1,#f7f8fa);color:var(--color-text-2,#4e5969);font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.market-compare-bar__items button{display:grid;width:22px;height:22px;padding:0;flex:none;place-items:center;border:0;border-radius:50%;background:transparent;color:var(--color-text-3,#86909c);cursor:pointer}.market-compare-bar__items button:hover{background:var(--color-fill-3,#e5e6eb)}.market-compare-bar__items svg{width:13px;height:13px}.market-compare-bar__actions{display:flex;flex:none;gap:6px}
body[arco-theme='dark'] .market-favorites-toggle.is-active{border-color:#a16207;background:#3d2e12;color:#fbbf24}body[arco-theme='dark'] .market-compare-bar{background:rgba(24,24,25,.96);box-shadow:0 12px 36px rgba(0,0,0,.42)}
/* 空态/加载态已收口到公共组件 EmptyState，旧的 .market-page__empty 移除 */
@media (max-width: 640px) {
  .market-page.has-compare { padding-bottom: 150px; }
  .market-page__hero {
    flex-direction: column;
  }
	.market-overview { grid-template-columns: repeat(2, 1fr); }
	.market-overview button { min-height: 48px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); }
	.market-overview button:nth-child(2) { border-right: 0; }
	.market-overview button:nth-child(n+3) { border-bottom: 0; }
	.market-page__tools { grid-template-columns: 1fr 132px; }
	.market-page__chips { flex-wrap: nowrap; overflow-x: auto; padding-bottom: 2px; }
	.chip { flex: none; }
	.market-page__toolbar-meta{align-items:center}.market-result-meta{display:grid;gap:2px}.market-median{padding-left:0;border-left:0}.market-compare-bar{bottom:calc(76px + env(safe-area-inset-bottom,0px));grid-template-columns:minmax(0,1fr) auto;width:calc(100% - 16px);padding:7px 8px;gap:6px}.market-compare-bar__summary{min-width:0}.market-compare-bar__summary span{overflow:hidden;text-overflow:ellipsis}.market-compare-bar__items{grid-column:1/-1;grid-row:2}.market-compare-bar__items>span{max-width:150px}.market-compare-bar__actions{grid-column:2;grid-row:1}.market-compare-bar__actions .base-btn:first-child{display:none}
}
</style>
