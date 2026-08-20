<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps({ apiBase: { type: String, default: '' } })
const { t, locale } = useI18n()
const loading = ref(true)
const unavailable = ref(false)
const saving = ref(false)
const monitors = ref([])
const formOpen = ref(false)
const expandedID = ref('')
const form = reactive({ id: '', name: '', type: 'https', target: '', port: '', interval_seconds: 60, timeout_seconds: 5, probe_points: 'probe_local', failure_count: 3, failure_duration_seconds: 0, ssl_days: 14, expected_status: 200, expected_keyword: '', enabled: true })

const api = (path, options = {}) => axios({ url: `${props.apiBase || ''}${path}`, withCredentials: true, ...options })
const listOf = (payload) => Array.isArray(payload) ? payload : (Array.isArray(payload?.monitors) ? payload.monitors : [])
const typeOptions = ['http', 'https', 'ping', 'tcp', 'ssl']
const splitRegions = (value) => String(value || '').split(/[,，\n]/).map((item) => item.trim()).filter(Boolean)
const timestamp = (value) => {
  const raw = Number(value || 0)
  if (!raw) return '—'
  return new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(raw < 1e12 ? raw * 1000 : raw))
}
const statusOf = (item) => item.enabled === false ? 'disabled' : ({ unknown: 'pending' }[item.state?.status] || item.state?.status || item.status || 'pending')
const trendOf = (item) => Array.isArray(item.trend) ? item.trend.slice(-24) : (Array.isArray(item.history) ? item.history.slice(-24) : [])
const quotaOf = (item) => item.traffic_quota || item.traffic || null
const quotaPercent = (item) => {
  const quota = quotaOf(item)
  if (!quota) return 0
  if (Number.isFinite(Number(quota.percent))) return Math.max(0, Math.min(100, Number(quota.percent)))
  return Number(quota.total) > 0 ? Math.max(0, Math.min(100, Number(quota.used) / Number(quota.total) * 100)) : 0
}
const overview = computed(() => ({ total: monitors.value.length, up: monitors.value.filter((item) => statusOf(item) === 'up').length, alert: monitors.value.filter((item) => ['down', 'alert'].includes(statusOf(item))).length }))

const withResults = async (item) => {
  try {
    const response = await api(`/api/account/service-monitors/results?monitor_id=${encodeURIComponent(item.id)}&limit=48`)
    const results = Array.isArray(response.data) ? response.data : []
    const successes = results.filter((result) => result.success).length
    const latestByProbe = new Map()
    results.forEach((result) => { if (!latestByProbe.has(result.probe_point_id || 'probe_local')) latestByProbe.set(result.probe_point_id || 'probe_local', result) })
    return {
      ...item,
      trend: [...results].reverse().map((result) => ({ timestamp: result.checked_at, latency_ms: result.latency_ms, up: result.success })),
      availability_24h: results.length ? successes / results.length * 100 : null,
      regions: [...latestByProbe.entries()].map(([region, result]) => ({ region, status: result.success ? 'up' : 'down', latency_ms: result.latency_ms }))
    }
  } catch { return item }
}

const load = async () => {
  loading.value = true
  try {
    const response = await api('/api/account/service-monitors')
    monitors.value = await Promise.all(listOf(response.data).map(withResults))
    unavailable.value = false
  } catch (error) {
    monitors.value = []
    unavailable.value = error?.response?.status === 404 || error?.response?.status === 501
    if (!unavailable.value) Message.error(t('service-monitor-load-fail'))
  } finally { loading.value = false }
}
const resetForm = () => {
  Object.assign(form, { id: '', name: '', type: 'https', target: '', port: '', interval_seconds: 60, timeout_seconds: 5, probe_points: 'probe_local', failure_count: 3, failure_duration_seconds: 0, ssl_days: 14, expected_status: 200, expected_keyword: '', enabled: true })
  formOpen.value = false
}
const edit = (item = null) => {
  Object.assign(form, item ? {
    id: item.id || '', name: item.name || '', type: item.kind || item.type || 'https', target: item.target || '', port: item.port || '', interval_seconds: item.interval_seconds || 60,
    timeout_seconds: item.timeout_seconds || 5, probe_points: (item.probe_point_ids || ['probe_local']).join(', '), failure_count: item.failure_threshold || 3,
    failure_duration_seconds: item.failure_duration_seconds || 0, ssl_days: item.ssl_warn_days || item.tls_warning_days || 14,
    expected_status: item.expected_status || 200, expected_keyword: item.expected_keyword || '', enabled: item.enabled !== false
  } : { id: '', name: '', type: 'https', target: '', port: '', interval_seconds: 60, timeout_seconds: 5, probe_points: 'probe_local', failure_count: 3, failure_duration_seconds: 0, ssl_days: 14, expected_status: 200, expected_keyword: '', enabled: true })
  formOpen.value = true
}
const save = async () => {
  if (!form.name.trim() || !form.target.trim()) return Message.warning(t('service-monitor-required'))
  saving.value = true
  try {
    await api('/api/account/service-monitors', { method: 'POST', data: {
      id: form.id, name: form.name.trim(), kind: form.type, target: form.target.trim(), port: ['tcp', 'ssl'].includes(form.type) ? Number(form.port) : 0,
      enabled: !!form.enabled, interval_seconds: Number(form.interval_seconds), timeout_seconds: Number(form.timeout_seconds),
      failure_threshold: Number(form.failure_count), failure_duration_seconds: Number(form.failure_duration_seconds), ssl_warn_days: Number(form.ssl_days),
      expected_status: ['http', 'https'].includes(form.type) ? Number(form.expected_status) : 0, expected_keyword: ['http', 'https'].includes(form.type) ? form.expected_keyword.trim() : '',
      probe_point_ids: splitRegions(form.probe_points)
    } })
    Message.success(t('service-monitor-saved'))
    resetForm()
    await load()
  } catch { Message.error(t('service-monitor-save-fail')) } finally { saving.value = false }
}
const action = async (item, actionName) => {
  if (actionName === 'delete' && !confirm(t('service-monitor-delete-confirm', { name: item.name }))) return
  try {
    if (actionName === 'delete') {
      await api(`/api/account/service-monitors?id=${encodeURIComponent(item.id)}`, { method: 'DELETE' })
    } else if (actionName === 'toggle') {
      const payload = { ...item, enabled: item.enabled === false }
      delete payload.state
      delete payload.trend
      delete payload.regions
      delete payload.availability_24h
      await api('/api/account/service-monitors', { method: 'POST', data: payload })
    }
    Message.success(t('service-monitor-action-success'))
    await load()
  } catch { Message.error(t('service-monitor-action-fail')) }
}

onMounted(load)
</script>

<template>
  <section class="service-monitor-panel">
    <div class="service-panel-head"><div><h2>{{ t('service-monitor-title') }}</h2><p>{{ t('service-monitor-subtitle') }}</p></div><div><BaseButton size="sm" @click="load">{{ t('common-retry') }}</BaseButton><BaseButton v-if="!unavailable" size="sm" variant="primary" @click="edit()">{{ t('service-monitor-add') }}</BaseButton></div></div>
    <EmptyState v-if="loading" loading />
    <EmptyState v-else-if="unavailable" :text="t('service-monitor-unavailable')" />
    <template v-else>
      <div class="service-overview"><div><span>{{ t('service-monitor-total') }}</span><strong>{{ overview.total }}</strong></div><div><span>{{ t('service-monitor-up') }}</span><strong class="is-up">{{ overview.up }}</strong></div><div><span>{{ t('service-monitor-alerting') }}</span><strong class="is-alert">{{ overview.alert }}</strong></div></div>
      <form v-if="formOpen" class="service-form" @submit.prevent="save">
        <div class="service-form__head"><strong>{{ t(form.id ? 'service-monitor-edit' : 'service-monitor-create') }}</strong><BaseButton size="sm" variant="text" @click="resetForm">{{ t('common-close') }}</BaseButton></div>
        <div class="service-form__grid"><label>{{ t('service-monitor-name') }}<BaseInput v-model="form.name" maxlength="64" required /></label><label>{{ t('service-monitor-type') }}<BaseInput as="select" v-model="form.type"><option v-for="type in typeOptions" :key="type" :value="type">{{ type.toUpperCase() }}</option></BaseInput></label><label class="is-wide">{{ t('service-monitor-target') }}<BaseInput v-model="form.target" :placeholder="['tcp','ssl'].includes(form.type) ? 'example.com' : t(`service-monitor-target-${form.type}`)" maxlength="500" required /></label><label v-if="['tcp','ssl'].includes(form.type)">{{ t('service-monitor-port') }}<BaseInput v-model="form.port" type="number" min="1" max="65535" required /></label><label>{{ t('service-monitor-interval') }}<BaseInput v-model="form.interval_seconds" type="number" min="30" max="86400" /></label><label>{{ t('service-monitor-timeout') }}<BaseInput v-model="form.timeout_seconds" type="number" min="1" max="30" /></label><label>{{ t('service-monitor-failure-count') }}<BaseInput v-model="form.failure_count" type="number" min="1" max="20" /></label><label>{{ t('service-monitor-failure-duration') }}<BaseInput v-model="form.failure_duration_seconds" type="number" min="0" max="86400" /></label><label v-if="['https','ssl'].includes(form.type)">{{ t('service-monitor-ssl-days') }}<BaseInput v-model="form.ssl_days" type="number" min="1" max="365" /></label><label v-if="['http','https'].includes(form.type)">{{ t('service-monitor-expected-status') }}<BaseInput v-model="form.expected_status" type="number" min="100" max="599" /></label><label v-if="['http','https'].includes(form.type)" class="is-wide">{{ t('service-monitor-expected-keyword') }}<BaseInput v-model="form.expected_keyword" maxlength="200" /></label><label class="is-wide">{{ t('service-monitor-probe-points') }}<BaseInput v-model="form.probe_points" :placeholder="t('service-monitor-probe-points-placeholder')" /></label><label class="service-check"><input v-model="form.enabled" type="checkbox" /> {{ t('notification-enabled') }}</label></div>
        <div><BaseButton type="submit" size="sm" variant="primary" :disabled="saving">{{ saving ? t('common-saving') : t('common-save') }}</BaseButton></div>
      </form>
      <EmptyState v-if="!monitors.length" :text="t('service-monitor-empty')"><BaseButton variant="primary" @click="edit()">{{ t('service-monitor-add-first') }}</BaseButton></EmptyState>
      <div v-else class="service-monitor-list"><article v-for="item in monitors" :key="item.id" class="service-monitor-card glow-card"><header><div><span class="service-dot" :class="`is-${statusOf(item)}`"></span><div><strong>{{ item.name }}</strong><small>{{ (item.kind || item.type)?.toUpperCase() }} · {{ item.target }}<template v-if="item.port">:{{ item.port }}</template></small></div></div><span class="service-status" :class="`is-${statusOf(item)}`">{{ t(`service-monitor-status-${statusOf(item)}`) }}</span></header><div class="service-monitor-metrics"><div><span>{{ t('service-monitor-latency') }}</span><strong>{{ Number.isFinite(Number(item.state?.last_latency_ms)) ? `${item.state.last_latency_ms} ms` : '—' }}</strong></div><div><span>{{ t('service-monitor-availability') }}</span><strong>{{ Number.isFinite(Number(item.availability_24h)) ? `${Number(item.availability_24h).toFixed(2)}%` : '—' }}</strong></div><div><span>{{ t('service-monitor-last-check') }}</span><strong>{{ timestamp(item.state?.last_check_at) }}</strong></div><div><span>{{ t('service-monitor-failures') }}</span><strong>{{ item.state?.consecutive_failures || 0 }}</strong></div></div><div v-if="trendOf(item).length" class="service-trend" :title="t('service-monitor-trend-title')"><span v-for="(point,index) in trendOf(item)" :key="point.timestamp || index" :class="point.up === false ? 'is-down' : ''" :style="{ height: `${Math.max(10, Math.min(100, Number(point.latency_ms || point.latency || 0) / 5))}%` }"></span></div><div v-if="item.regions?.length" class="service-regions"><span v-for="region in item.regions" :key="region.region || region"><b :class="`is-${region.status || 'pending'}`"></b>{{ region.region || region }}<small v-if="region.latency_ms">{{ region.latency_ms }}ms</small></span></div><div v-if="item.state?.certificate_expires_at || item.state?.last_ip || quotaOf(item)" class="service-facts"><div v-if="item.state?.certificate_expires_at"><span>{{ t('service-monitor-ssl-expiry') }}</span><strong>{{ timestamp(item.state.certificate_expires_at) }}</strong></div><div v-if="item.state?.last_ip"><span>{{ t('service-monitor-current-ip') }}</span><strong>{{ item.state.last_ip }}</strong><small v-if="item.state.last_certificate_change">{{ timestamp(item.state.last_certificate_change) }}</small></div><div v-if="quotaOf(item)"><span>{{ t('service-monitor-traffic-quota') }}</span><strong>{{ quotaPercent(item).toFixed(1) }}%</strong><div class="quota-track"><i :style="{ width: `${quotaPercent(item)}%` }"></i></div></div></div><p v-if="item.state?.last_error" class="service-last-error">{{ item.state.last_error }}</p><footer><BaseButton size="sm" @click="expandedID = expandedID === item.id ? '' : item.id">{{ t(expandedID === item.id ? 'common-collapse' : 'common-expand-more') }}</BaseButton><BaseButton size="sm" @click="edit(item)">{{ t('owner-edit') }}</BaseButton><BaseButton size="sm" @click="action(item, 'toggle')">{{ t(item.enabled === false ? 'service-monitor-enable' : 'service-monitor-disable') }}</BaseButton><BaseButton size="sm" variant="danger" @click="action(item, 'delete')">{{ t('owner-delete') }}</BaseButton></footer><div v-if="expandedID === item.id" class="service-detail"><p>{{ t('service-monitor-alert-policy-actual', { count: item.failure_threshold || 3, duration: item.failure_duration_seconds || 0, days: item.ssl_warn_days || 14 }) }}</p><p>{{ t('service-monitor-ip-policy') }}</p><p>{{ t('service-monitor-recovery-policy') }}</p></div></article></div>
    </template>
  </section>
</template>

<style scoped>
.service-panel-head{display:flex;margin-bottom:16px;align-items:flex-start;justify-content:space-between;gap:14px}.service-panel-head h2{margin:0;font-size:17px}.service-panel-head p{margin:5px 0 0;color:var(--color-text-3,#86909c);font-size:12px}.service-panel-head>div:last-child{display:flex;gap:6px}.service-overview{display:grid;grid-template-columns:repeat(3,1fr);margin-bottom:18px;border-block:1px solid var(--color-border-2,#e5e6eb)}.service-overview>div{display:grid;padding:12px;gap:4px;border-right:1px solid var(--color-border-2,#e5e6eb)}.service-overview>div:last-child{border:0}.service-overview span{color:var(--color-text-3,#86909c);font-size:10px}.service-overview strong{font-size:18px}.service-overview .is-up{color:#008f24}.service-overview .is-alert{color:#d03050}.service-form{display:grid;margin-bottom:18px;padding:14px;gap:12px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-fill-1,#f7f8fa)}.service-form__head{display:flex;align-items:center;justify-content:space-between}.service-form__grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.service-form__grid label{display:grid;gap:5px;color:var(--color-text-2,#4e5969);font-size:11px}.service-form__grid label.is-wide{grid-column:1/-1}.service-check{display:flex!important;align-items:center}.service-monitor-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.service-monitor-card{display:grid;padding:14px;gap:11px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.service-monitor-card>header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.service-monitor-card>header>div{display:flex;min-width:0;align-items:center;gap:8px}.service-monitor-card header div div{display:grid;min-width:0;gap:3px}.service-monitor-card header strong,.service-monitor-card header small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.service-monitor-card header strong{font-size:13px}.service-monitor-card header small{color:var(--color-text-3,#86909c);font-size:9px}.service-dot{width:9px;height:9px;flex:none;border-radius:50%;background:#86909c}.service-dot.is-up{background:#00b42a}.service-dot.is-down,.service-dot.is-alert{background:#f53f3f}.service-status{padding:2px 6px;flex:none;border-radius:4px;background:var(--color-fill-2,#f2f3f5);color:var(--color-text-3,#86909c);font-size:9px}.service-status.is-up{background:rgba(0,180,42,.08);color:#008f24}.service-status.is-down,.service-status.is-alert{background:rgba(245,63,63,.08);color:#d03050}.service-monitor-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:5px}.service-monitor-metrics>div{display:grid;padding:7px;gap:3px;border-radius:6px;background:var(--color-fill-1,#f7f8fa)}.service-monitor-metrics span,.service-facts span{color:var(--color-text-3,#86909c);font-size:8px}.service-monitor-metrics strong,.service-facts strong{font-size:10px}.service-trend{display:flex;height:34px;align-items:flex-end;gap:2px}.service-trend span{min-width:2px;flex:1;border-radius:1px 1px 0 0;background:#00b42a}.service-trend span.is-down{background:#f53f3f}.service-regions{display:flex;flex-wrap:wrap;gap:5px}.service-regions>span{display:flex;padding:3px 6px;align-items:center;gap:4px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:4px;font-size:9px}.service-regions b{width:5px;height:5px;border-radius:50%;background:#86909c}.service-regions b.is-up{background:#00b42a}.service-regions b.is-down{background:#f53f3f}.service-regions small{color:var(--color-text-3,#86909c)}.service-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:6px}.service-facts>div{display:grid;min-width:0;padding:7px;gap:3px;border-left:2px solid #165dff;background:rgba(22,93,255,.04)}.service-facts strong,.service-facts small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.service-facts small{color:#d03050;font-size:8px}.quota-track{height:3px;overflow:hidden;border-radius:2px;background:var(--color-fill-3,#e5e6eb)}.quota-track i{display:block;height:100%;background:#ff7d00}.service-last-error{margin:0;padding:7px;border-left:2px solid #f53f3f;background:rgba(245,63,63,.05);color:#d03050;font-size:9px}.service-monitor-card footer{display:flex;flex-wrap:wrap;gap:5px}.service-detail{padding-top:8px;border-top:1px dashed var(--color-border-2,#e5e6eb)}.service-detail p{margin:3px 0;color:var(--color-text-3,#86909c);font-size:9px}
body[arco-theme='dark'] .service-form{background:#202021}body[arco-theme='dark'] .service-monitor-card{background:#232324;border-color:rgba(255,255,255,.1)}
@media(max-width:720px){.service-panel-head{align-items:flex-start}.service-panel-head>div:last-child .base-btn:first-child{display:none}.service-monitor-list,.service-form__grid{grid-template-columns:1fr}.service-form__grid label.is-wide{grid-column:auto}.service-monitor-metrics{grid-template-columns:repeat(2,1fr)}.service-facts{grid-template-columns:1fr}.service-monitor-card{padding:12px}.service-overview>div{padding:9px}}
</style>
