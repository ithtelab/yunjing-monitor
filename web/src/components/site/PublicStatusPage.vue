<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import axios from 'axios'
import { useI18n } from 'vue-i18n'
import HeaderLocale from '@/components/HeaderLocale.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import BaseButton from '@/components/ui/BaseButton.vue'

const props = defineProps({
  slug: { type: String, required: true },
  apiBase: { type: String, default: '' },
  siteName: { type: String, default: '云镜监控' },
  dark: { type: Boolean, default: false }
})
const emit = defineEmits(['toggle-theme'])
const { t, locale } = useI18n()

const loading = ref(true)
const refreshing = ref(false)
const loadError = ref('')
const snapshot = ref(null)
let refreshTimer = 0

const overall = computed(() => snapshot.value?.overall || 'operational')
const nodes = computed(() => Array.isArray(snapshot.value?.nodes) ? snapshot.value.nodes : [])
const services = computed(() => Array.isArray(snapshot.value?.services) ? snapshot.value.services : [])
const statusItems = computed(() => [
  ...nodes.value.map((node) => ({ ...node, item_type: 'node' })),
  ...services.value.map((service) => ({
    ...service,
    item_type: 'service',
    display_name: service.name || service.id,
    availability_24h: service.availability_24h ?? service.uptime_percent,
    last_latency_ms: service.last_latency_ms ?? service.latency_ms
  }))
])
const incidents = computed(() => Array.isArray(snapshot.value?.incidents) ? snapshot.value.incidents : [])
const probes = computed(() => Array.isArray(snapshot.value?.probes) ? snapshot.value.probes : (Array.isArray(snapshot.value?.regional_probes) ? snapshot.value.regional_probes : []))
const page = computed(() => snapshot.value?.page || {})
const serviceOnline = (node) => node?.online === true || ['up', 'operational'].includes(String(node?.status || '').toLowerCase())
const onlineCount = computed(() => statusItems.value.filter(serviceOnline).length)
const availability24h = computed(() => {
  if (Number.isFinite(Number(snapshot.value?.availability_24h))) return Number(snapshot.value.availability_24h)
  const values = statusItems.value.map((item) => Number(item.availability_24h)).filter(Number.isFinite)
  return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : null
})
const averageLatency = computed(() => {
  const values = [...statusItems.value, ...probes.value].map((item) => Number(item.latency_ms ?? item.last_latency_ms)).filter((value) => Number.isFinite(value) && value >= 0)
  return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : null
})
const quotaOf = (item) => item?.traffic_quota || item?.traffic || null
const quotaPercent = (item) => {
  const quota = quotaOf(item)
  if (!quota) return null
  if (Number.isFinite(Number(quota.percent))) return Math.max(0, Math.min(100, Number(quota.percent)))
  return Number(quota.total) > 0 ? Math.max(0, Math.min(100, Number(quota.used) / Number(quota.total) * 100)) : null
}
const regionsOf = (item) => Array.isArray(item?.regions) ? item.regions : []
const trendOf = (item) => Array.isArray(item?.trend) ? item.trend.slice(-30) : (Array.isArray(item?.history) ? item.history.slice(-30) : [])

const formatDate = (value) => {
  const raw = Number(value || 0)
  if (!raw) return t('status-time-unknown')
  const timestamp = raw < 1000000000000 ? raw * 1000 : raw
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  }).format(new Date(timestamp))
}

const formatPercent = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? `${Math.max(0, Math.min(100, number)).toFixed(1)}%` : '—'
}

const load = async ({ initial = false } = {}) => {
  if (refreshing.value) return
  refreshing.value = true
  if (initial) loading.value = true
  try {
    const response = await axios.get(`${props.apiBase || ''}/api/status/${encodeURIComponent(props.slug)}`)
    snapshot.value = response.data || null
    loadError.value = ''
    if (page.value?.name) document.title = `${page.value.name} · ${props.siteName}`
  } catch (error) {
    loadError.value = error?.response?.status === 404 ? 'not-found' : 'load-failed'
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

const refreshWhenVisible = () => {
  if (document.visibilityState === 'visible') load()
}

onMounted(() => {
  load({ initial: true })
  document.addEventListener('visibilitychange', refreshWhenVisible)
  refreshTimer = window.setInterval(refreshWhenVisible, 30000)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', refreshWhenVisible)
  window.clearInterval(refreshTimer)
  document.title = props.siteName
})
</script>

<template>
  <main class="status-page">
    <header class="status-header">
      <a class="status-brand" href="/" :aria-label="siteName">
        <img src="/logo.svg" alt="" />
        <span>{{ siteName }}</span>
      </a>
      <div class="status-header__actions">
        <HeaderLocale :dark="dark" />
        <a-button class="status-theme" shape="round" :aria-label="t('status-toggle-theme')" @click="emit('toggle-theme')">
          <template #icon><icon-moon-fill v-if="!dark" /><icon-sun-fill v-else /></template>
        </a-button>
      </div>
    </header>

    <section v-if="loading" class="status-shell"><EmptyState loading /></section>
    <section v-else-if="loadError" class="status-shell status-error">
      <div class="status-error__mark" aria-hidden="true">!</div>
      <h1>{{ t(loadError === 'not-found' ? 'status-not-found-title' : 'status-load-failed-title') }}</h1>
      <p>{{ t(loadError === 'not-found' ? 'status-not-found-text' : 'status-load-failed-text') }}</p>
      <BaseButton @click="load({ initial: true })">{{ t('common-retry') }}</BaseButton>
    </section>

    <template v-else>
      <section class="status-intro">
        <div>
          <p class="status-intro__eyebrow">{{ t('status-public-label') }}</p>
          <h1>{{ page.name }}</h1>
          <p v-if="page.description">{{ page.description }}</p>
        </div>
        <BaseButton size="sm" :disabled="refreshing" @click="load()">
          {{ refreshing ? t('status-refreshing') : t('status-refresh') }}
        </BaseButton>
      </section>

      <section class="overall-banner" :class="`is-${overall}`" role="status">
        <span class="overall-banner__dot" aria-hidden="true"></span>
        <div>
          <strong>{{ t(`status-overall-${overall}`) }}</strong>
          <small>{{ t('status-overall-summary', { online: onlineCount, total: statusItems.length }) }}</small>
        </div>
        <time>{{ t('status-updated-at', { time: formatDate(snapshot.updated_at) }) }}</time>
      </section>

      <section class="status-insights" :aria-label="t('status-insights-title')">
        <div><span>{{ t('status-availability-24h') }}</span><strong>{{ availability24h === null ? '—' : `${availability24h.toFixed(2)}%` }}</strong></div>
        <div><span>{{ t('status-average-latency') }}</span><strong>{{ averageLatency === null ? '—' : `${averageLatency.toFixed(0)} ms` }}</strong></div>
        <div><span>{{ t('status-probe-regions') }}</span><strong>{{ probes.length }}</strong></div>
        <div><span>{{ t('status-traffic-cycle') }}</span><strong>{{ quotaPercent(snapshot) === null ? '—' : `${quotaPercent(snapshot).toFixed(1)}%` }}</strong></div>
      </section>

      <section class="status-section">
        <div class="status-section__head">
          <h2>{{ t('status-services-title') }}</h2>
          <span>{{ onlineCount }}/{{ statusItems.length }}</span>
        </div>
        <EmptyState v-if="!statusItems.length" :text="t('status-services-empty')" />
        <div v-else class="service-list">
          <article v-for="node in statusItems" :key="node.item_type === 'service' ? `service-${node.id}` : `node-${node.node_id}`" class="service-row">
            <span class="service-state" :class="serviceOnline(node) ? 'is-online' : 'is-offline'" aria-hidden="true"></span>
            <div class="service-main">
              <strong>{{ node.display_name || node.name || node.node_id || node.id }}</strong>
              <small>{{ node.item_type === 'service' ? String(node.kind || '').toUpperCase() : (node.region || t('market-no-region')) }}</small>
              <div v-if="regionsOf(node).length" class="service-regions"><span v-for="region in regionsOf(node)" :key="region.region || region"><i :class="`is-${region.status || 'pending'}`"></i>{{ region.region || region }}<b v-if="region.latency_ms != null">{{ region.latency_ms }}ms</b></span></div>
            </div>
            <div v-if="node.item_type !== 'service' && page.show_metrics" class="service-metrics">
              <span><small>CPU</small>{{ formatPercent(node.cpu) }}</span>
              <span><small>{{ t('chart-memory') }}</small>{{ formatPercent(node.memory) }}</span>
            </div>
            <div v-else class="service-metrics is-probe">
              <span><small>{{ t('status-latency') }}</small>{{ Number.isFinite(Number(node.latency_ms ?? node.last_latency_ms)) ? `${node.latency_ms ?? node.last_latency_ms} ms` : '—' }}</span>
              <span><small>{{ t('status-availability') }}</small>{{ Number.isFinite(Number(node.availability_24h)) ? `${Number(node.availability_24h).toFixed(2)}%` : '—' }}</span>
            </div>
            <span class="service-label" :class="serviceOnline(node) ? 'is-online' : 'is-offline'">
              {{ t(serviceOnline(node) ? 'status-service-operational' : 'status-service-outage') }}
            </span>
            <div v-if="trendOf(node).length || quotaOf(node)" class="service-detail-row">
              <div v-if="trendOf(node).length" class="public-trend"><span v-for="(point,index) in trendOf(node)" :key="point.timestamp || index" :class="point.up === false ? 'is-down' : ''" :style="{ height: `${Math.max(10, Math.min(100, Number(point.latency_ms || point.latency || 0) / 5))}%` }"></span></div>
              <div v-if="quotaOf(node)" class="public-quota"><span>{{ t('status-traffic-quota') }}</span><strong>{{ quotaPercent(node) === null ? '—' : `${quotaPercent(node).toFixed(1)}%` }}</strong><div><i :style="{ width: `${quotaPercent(node) || 0}%` }"></i></div><small v-if="quotaOf(node).cycle_end">{{ t('status-cycle-ends', { time: formatDate(quotaOf(node).cycle_end) }) }}</small></div>
            </div>
          </article>
        </div>
      </section>

      <section v-if="probes.length" class="status-section">
        <div class="status-section__head"><h2>{{ t('status-regional-probes-title') }}</h2><span>{{ probes.length }}</span></div>
        <div class="probe-grid"><article v-for="probe in probes" :key="probe.id || probe.region"><header><strong>{{ probe.region || probe.name }}</strong><span :class="probe.online === false || probe.status === 'down' ? 'is-down' : 'is-up'">{{ t(probe.online === false || probe.status === 'down' ? 'status-probe-down' : 'status-probe-up') }}</span></header><div><span>{{ t('status-latency') }}</span><strong>{{ Number.isFinite(Number(probe.latency_ms)) ? `${probe.latency_ms} ms` : '—' }}</strong></div><div><span>{{ t('status-availability-24h') }}</span><strong>{{ Number.isFinite(Number(probe.availability_24h)) ? `${Number(probe.availability_24h).toFixed(2)}%` : '—' }}</strong></div><small>{{ formatDate(probe.checked_at || probe.last_checked_at) }}</small></article></div>
      </section>

      <section class="status-section">
        <div class="status-section__head"><h2>{{ t('status-incidents-title') }}</h2></div>
        <div v-if="!incidents.length" class="incident-empty">
          <span aria-hidden="true">✓</span>
          <div><strong>{{ t('status-incidents-empty-title') }}</strong><small>{{ t('status-incidents-empty-text') }}</small></div>
        </div>
        <div v-else class="incident-list">
          <article v-for="incident in incidents" :key="incident.id" class="incident-item">
            <div class="incident-item__line"><span></span></div>
            <div class="incident-item__content">
              <header>
                <h3>{{ incident.title }}</h3>
                <span :class="`is-${incident.status || 'investigating'}`">{{ t(`status-incident-${incident.status || 'investigating'}`) }}</span>
              </header>
              <p v-if="incident.message">{{ incident.message }}</p>
              <time>{{ formatDate(incident.started_at) }}</time>
            </div>
          </article>
        </div>
      </section>
    </template>

    <footer class="status-footer">{{ t('status-powered-by', { name: siteName }) }}</footer>
  </main>
</template>

<style scoped>
.status-page{width:min(920px,calc(100% - 32px));min-height:100vh;margin:0 auto;color:var(--color-text-1,#1d2129)}
.status-header{display:flex;height:68px;align-items:center;justify-content:space-between;border-bottom:1px solid var(--color-border-2,#e5e6eb)}
.status-brand{display:inline-flex;min-width:0;align-items:center;gap:9px;color:inherit;font-size:15px;font-weight:750;text-decoration:none}.status-brand img{width:30px;height:30px}.status-brand span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.status-header__actions{display:flex;align-items:center;gap:8px}.status-theme{border-color:var(--color-border-2,#eee)!important;background:var(--color-bg-2,#fff)!important;color:var(--color-text-1,#333)!important}
.status-shell{min-height:440px;padding-top:80px}.status-error{display:flex;align-items:center;flex-direction:column;text-align:center}.status-error__mark{display:grid;width:44px;height:44px;place-items:center;border-radius:50%;background:rgba(245,63,63,.1);color:#d03050;font-size:22px;font-weight:800}.status-error h1{margin:14px 0 6px;font-size:22px}.status-error p{margin:0 0 18px;color:var(--color-text-3,#86909c)}
.status-intro{display:flex;padding:52px 0 26px;align-items:flex-start;justify-content:space-between;gap:24px}.status-intro__eyebrow{margin:0 0 8px!important;color:#165dff!important;font-size:11px!important;font-weight:800;letter-spacing:0;text-transform:uppercase}.status-intro h1{margin:0;font-size:30px;line-height:1.2}.status-intro p{max-width:620px;margin:10px 0 0;color:var(--color-text-3,#86909c);font-size:14px;line-height:1.7}
.overall-banner{display:grid;grid-template-columns:auto minmax(0,1fr) auto;min-height:72px;padding:0 20px;align-items:center;gap:12px;border:1px solid rgba(0,180,42,.22);border-radius:8px;background:rgba(0,180,42,.06)}.overall-banner__dot{width:11px;height:11px;border-radius:50%;background:#00b42a;box-shadow:0 0 0 5px rgba(0,180,42,.1)}.overall-banner div{display:grid;gap:3px}.overall-banner strong{font-size:15px}.overall-banner small,.overall-banner time{color:var(--color-text-3,#86909c);font-size:11px}.overall-banner.is-degraded{border-color:rgba(255,125,0,.26);background:rgba(255,125,0,.07)}.overall-banner.is-degraded .overall-banner__dot{background:#ff7d00;box-shadow:0 0 0 5px rgba(255,125,0,.1)}.overall-banner.is-incident{border-color:rgba(245,63,63,.24);background:rgba(245,63,63,.06)}.overall-banner.is-incident .overall-banner__dot{background:#f53f3f;box-shadow:0 0 0 5px rgba(245,63,63,.1)}
.status-insights{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));margin-top:14px;border-block:1px solid var(--color-border-2,#e5e6eb)}.status-insights>div{display:grid;padding:12px;gap:4px;border-right:1px solid var(--color-border-2,#e5e6eb)}.status-insights>div:last-child{border:0}.status-insights span{color:var(--color-text-3,#86909c);font-size:9px}.status-insights strong{font-size:15px;font-variant-numeric:tabular-nums}
.status-section{margin-top:30px}.status-section__head{display:flex;margin-bottom:10px;align-items:center;justify-content:space-between}.status-section__head h2{margin:0;font-size:16px}.status-section__head span{color:var(--color-text-3,#86909c);font-size:12px;font-variant-numeric:tabular-nums}.service-list{border-block:1px solid var(--color-border-2,#e5e6eb)}.service-row{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;min-height:68px;padding:0 12px;align-items:center;gap:12px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.service-row:last-child{border-bottom:0}.service-state{width:8px;height:8px;border-radius:50%}.service-state.is-online{background:#00b42a}.service-state.is-offline{background:#f53f3f}.service-main{display:grid;min-width:0;gap:3px}.service-main strong{overflow:hidden;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.service-main small{color:var(--color-text-3,#86909c);font-size:11px}.service-metrics{display:flex;gap:18px}.service-metrics>span{display:grid;min-width:54px;gap:2px;color:var(--color-text-2,#4e5969);font-size:12px;font-variant-numeric:tabular-nums}.service-metrics small{color:var(--color-text-3,#86909c);font-size:9px;text-transform:uppercase}.service-label{min-width:78px;text-align:right;font-size:11px;font-weight:700}.service-label.is-online{color:#008f24}.service-label.is-offline{color:#d03050}
.service-regions{display:flex;flex-wrap:wrap;gap:4px}.service-regions span{display:flex;align-items:center;gap:3px;color:var(--color-text-3,#86909c);font-size:8px}.service-regions i{width:5px;height:5px;border-radius:50%;background:#86909c}.service-regions i.is-up{background:#00b42a}.service-regions i.is-down{background:#f53f3f}.service-regions b{font-weight:500}.service-detail-row{display:grid;grid-column:1/-1;grid-template-columns:minmax(0,1fr) 180px;padding:0 0 10px 20px;align-items:end;gap:14px}.public-trend{display:flex;height:24px;align-items:flex-end;gap:2px}.public-trend span{min-width:2px;flex:1;border-radius:1px 1px 0 0;background:#00b42a}.public-trend span.is-down{background:#f53f3f}.public-quota{display:grid;grid-template-columns:1fr auto;gap:2px 8px;font-size:9px}.public-quota>span,.public-quota>small{color:var(--color-text-3,#86909c)}.public-quota>div{grid-column:1/-1;height:3px;overflow:hidden;border-radius:2px;background:var(--color-fill-3,#e5e6eb)}.public-quota>div i{display:block;height:100%;background:#ff7d00}.public-quota>small{grid-column:1/-1}.probe-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr));gap:10px}.probe-grid article{display:grid;padding:12px;gap:8px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px}.probe-grid header{display:flex;align-items:center;justify-content:space-between;gap:8px}.probe-grid header strong{font-size:12px}.probe-grid header span{font-size:9px;font-weight:700}.probe-grid header .is-up{color:#008f24}.probe-grid header .is-down{color:#d03050}.probe-grid article>div{display:flex;align-items:center;justify-content:space-between;color:var(--color-text-2,#4e5969);font-size:10px}.probe-grid article>small{color:var(--color-text-3,#86909c);font-size:8px}
.incident-empty{display:flex;min-height:70px;padding:0 14px;align-items:center;gap:12px;border-block:1px solid var(--color-border-2,#e5e6eb)}.incident-empty>span{display:grid;width:28px;height:28px;place-items:center;border-radius:50%;background:rgba(0,180,42,.1);color:#008f24;font-weight:800}.incident-empty div{display:grid;gap:3px}.incident-empty strong{font-size:13px}.incident-empty small{color:var(--color-text-3,#86909c);font-size:11px}.incident-list{border-top:1px solid var(--color-border-2,#e5e6eb)}.incident-item{display:grid;grid-template-columns:20px 1fr;gap:8px}.incident-item__line{position:relative;display:flex;justify-content:center}.incident-item__line::before{position:absolute;top:0;bottom:0;width:1px;background:var(--color-border-2,#e5e6eb);content:''}.incident-item__line span{position:relative;width:7px;height:7px;margin-top:22px;border:3px solid var(--color-bg-1,#f7f8fa);border-radius:50%;background:#ff7d00}.incident-item__content{padding:16px 0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.incident-item__content header{display:flex;align-items:center;justify-content:space-between;gap:12px}.incident-item h3{margin:0;font-size:13px}.incident-item header span{padding:2px 6px;border-radius:4px;background:rgba(255,125,0,.1);color:#b35400;font-size:10px;font-weight:700}.incident-item header span.is-resolved{background:rgba(0,180,42,.1);color:#008f24}.incident-item p{margin:8px 0;color:var(--color-text-2,#4e5969);font-size:12px;line-height:1.65}.incident-item time{color:var(--color-text-3,#86909c);font-size:10px}.status-footer{padding:44px 0 24px;color:var(--color-text-3,#86909c);font-size:11px;text-align:center}
body[arco-theme='dark'] .status-theme{border-color:rgba(255,255,255,.12)!important;background:#18181b!important;color:#fafafa!important}
@media(max-width:640px){.status-page{width:calc(100% - 24px)}.status-header{height:60px}.status-intro{padding:34px 0 20px;align-items:flex-end}.status-intro h1{font-size:24px}.status-intro p{font-size:12px}.overall-banner{grid-template-columns:auto 1fr;min-height:76px;padding:10px 14px}.overall-banner time{grid-column:2}.status-insights{grid-template-columns:repeat(2,1fr)}.status-insights>div:nth-child(2){border-right:0}.status-insights>div:nth-child(-n+2){border-bottom:1px solid var(--color-border-2,#e5e6eb)}.service-row{grid-template-columns:auto minmax(0,1fr) auto;min-height:64px;padding:8px 6px;gap:9px}.service-metrics{display:none}.service-label{min-width:64px;font-size:10px}.service-detail-row{grid-template-columns:1fr;padding-left:15px}.status-section{margin-top:24px}.probe-grid{grid-template-columns:1fr 1fr}}
@media(max-width:420px){.probe-grid{grid-template-columns:1fr}}
</style>
