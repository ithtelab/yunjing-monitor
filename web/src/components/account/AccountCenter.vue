<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseDatePicker from '@/components/ui/BaseDatePicker.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import BillingFields from '@/components/ui/BillingFields.vue'
import CopyCommandBox from '@/components/ui/CopyCommandBox.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import BackButton from '@/components/ui/BackButton.vue'
import CaptchaInput from '@/components/market/CaptchaInput.vue'
import ServiceMonitorPanel from '@/components/account/ServiceMonitorPanel.vue'
import { billingOf } from '@/utils/billing.js'
import { formatBytes } from '@/utils/utils.js'

const props = defineProps({
  apiBase: { type: String, default: '' },
  marketEnabled: { type: Boolean, default: true },
  registrationEnabled: { type: Boolean, default: true },
  selfServiceNodeEnabled: { type: Boolean, default: true },
  userNodeLimit: { type: Number, default: 0 }
})
const emit = defineEmits(['navigate'])
const { t, locale } = useI18n()

const api = (path, options = {}) => axios({
  url: `${props.apiBase || ''}${path}`,
  withCredentials: true,
  ...options
})

const loading = ref(true)
const authenticated = ref(false)
const me = ref(null)
const authMode = ref('login')
const authLoading = ref(false)
const authError = ref('')
const captchaRef = ref(null)
const captchaId = ref('')
const captchaCode = ref('')
const activeTab = ref('nodes')
const nodes = ref([])
const nodesLoading = ref(false)
const installBox = ref(null)
const nodeFormOpen = ref(false)
const nodeSaving = ref(false)
const editingNodeID = ref('')
const listingNodeID = ref('')
const listingSaving = ref(false)
const subscriptions = ref([])
const subscriptionsLoading = ref(false)
const subscriptionFormOpen = ref(false)
const subscriptionSaving = ref(false)
const reports = ref([])
const appeals = ref([])
const reportsLoading = ref(false)
const appealReportID = ref('')
const appealMessage = ref('')
const appealSaving = ref(false)
const notificationsLoading = ref(false)
const notificationSaving = ref(false)
const notificationBinding = ref(null)
const notificationDeliveries = ref([])
const bindSession = ref(null)
const bindStarting = ref(false)
const notificationTesting = ref(false)
const ordersLoading = ref(false)
const orders = ref([])
const orderActingID = ref('')
let nodesRequest = null
let nodesRefreshTimer = 0
let bindPollTimer = 0
let bindPollBusy = false

const authForm = reactive({ email: '', password: '', password_confirm: '', remember_me: true })
const nodeForm = reactive({ display_name: '', region: '', due_date: '', visibility: 'private' })
const listingForm = reactive({
  display_name: '', region: '', specs: '', listing_type: 'rent', contact: '', description: '',
  price_amount: '', price_currency: 'USD', billing_cycle: 'monthly', due_date: ''
})
const subscriptionForm = reactive({ id: '', name: '', regions: '', tags: '', max_price: '', currency: 'CNY', min_memory_gb: '', enabled: true })
const notificationPreference = reactive({
  enabled: true,
  events: ['alert.firing', 'alert.recovered', 'market.approved', 'market.subscription.match', 'market.order.created'],
  node_ids: [], quiet_start: '', quiet_end: '', time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai', delivery_mode: 'immediate'
})

const notificationEvents = [
  { key: 'alert.firing', label: 'notification-event-alert-firing' },
  { key: 'alert.recovered', label: 'notification-event-alert-recovered' },
  { key: 'market.approved', label: 'notification-event-market-approved' },
  { key: 'market.rejected', label: 'notification-event-market-rejected' },
  { key: 'market.subscription.match', label: 'notification-event-subscription-match' },
  { key: 'market.report.created', label: 'notification-event-report-created' },
  { key: 'market.report.resolved', label: 'notification-event-report-resolved' },
  { key: 'market.appeal.resolved', label: 'notification-event-appeal-resolved' },
  { key: 'market.order.created', label: 'notification-event-order-created' },
  { key: 'market.order.accepted', label: 'notification-event-order-accepted' },
  { key: 'market.order.completed', label: 'notification-event-order-completed' },
  { key: 'market.order.cancelled', label: 'notification-event-order-cancelled' },
  { key: 'market.order.expired', label: 'notification-event-order-expired' },
  { key: 'agent.upgrade.result', label: 'notification-event-agent-upgrade' },
  { key: 'account.login', label: 'notification-event-account-login' },
  { key: 'node.token.rotated', label: 'notification-event-token-rotated' },
  { key: 'node.deleted', label: 'notification-event-node-deleted' }
]

const tabs = computed(() => [
  { key: 'nodes', label: t('account-tab-nodes') },
  { key: 'services', label: t('account-tab-services') },
  { key: 'market', label: t('account-tab-market') },
  { key: 'subscriptions', label: t('account-tab-subscriptions') },
  { key: 'reports', label: t('account-tab-reports') },
  { key: 'notifications', label: t('account-tab-notifications') },
  { key: 'orders', label: t('account-tab-orders') },
  { key: 'security', label: t('account-tab-security') }
])
const marketAvailable = computed(() => props.marketEnabled && me.value?.market_enabled !== false)
const registrationAvailable = computed(() => props.registrationEnabled)
const nodeCreationAvailable = computed(() => props.selfServiceNodeEnabled && me.value?.self_service_node_enabled !== false)
const effectiveNodeLimit = computed(() => Math.max(0, Number(me.value?.user_node_limit ?? props.userNodeLimit) || 0))
const canCreateNode = computed(() => nodeCreationAvailable.value && (!effectiveNodeLimit.value || nodes.value.length < effectiveNodeLimit.value))

const splitValues = (value) => String(value || '').split(/[,，\n]/).map((item) => item.trim()).filter(Boolean)
const nodeID = (node) => String(node?.node_id || node?.id || '')
const nodeName = (node) => node?.display_name || node?.name || nodeID(node)
const nodeOnline = (node) => node?.online === true || node?.status === true || node?.status === 'online'
const nodePublic = (node) => node?.private === false || node?.visibility === 'public' || node?.public === true || node?.is_public === true
const nodeListed = (node) => node?.has_listing === true || node?.for_sale === true || node?.market_listed === true || node?.listing?.for_sale === true
const nodeNumber = (node, ...keys) => {
  for (const key of keys) {
    const value = key.split('.').reduce((current, part) => current?.[part], node)
    if (Number.isFinite(Number(value))) return Number(value)
  }
  return 0
}
const percentText = (value) => Number.isFinite(Number(value)) ? `${Math.max(0, Math.min(100, Number(value))).toFixed(1)}%` : '—'
const nodeCPU = (node) => nodeNumber(node, 'cpu_percent', 'cpu', 'state.cpu', 'host.State.CPU')
const nodeMemory = (node) => {
  const direct = nodeNumber(node, 'memory_percent', 'mem_percent', 'state.memory')
  if (direct > 0) return direct
  const used = nodeNumber(node, 'host.State.MemUsed')
  const total = nodeNumber(node, 'host.Host.MemTotal')
  return total > 0 ? used / total * 100 : 0
}
const nodeNetwork = (node) => nodeNumber(node, 'net_in_speed', 'state.net_in_speed', 'host.State.NetInSpeed') + nodeNumber(node, 'net_out_speed', 'state.net_out_speed', 'host.State.NetOutSpeed')
const nodeTraffic = (node) => nodeNumber(node, 'net_in_transfer', 'state.net_in_transfer', 'host.State.NetInTransfer') + nodeNumber(node, 'net_out_transfer', 'state.net_out_transfer', 'host.State.NetOutTransfer')
const nodeDue = (node) => {
  const raw = Number(node?.due_time || node?.info?.due_time || node?.listing?.due_time || node?.expires_at || 0)
  if (!raw) return t('account-node-no-due')
  const timestamp = raw < 1000000000000 ? raw * 1000 : raw
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(timestamp))
}
const dueInput = (node) => {
  const raw = Number(node?.due_time || node?.info?.due_time || node?.listing?.due_time || node?.expires_at || 0)
  if (!raw) return ''
  const timestamp = raw < 1000000000000 ? raw * 1000 : raw
  return new Date(timestamp).toISOString().slice(0, 10)
}
const accountTimestamp = (value, withTime = true) => {
  const raw = Number(value || 0)
  if (!raw) return '—'
  const timestamp = raw < 1000000000000 ? raw * 1000 : raw
  return new Intl.DateTimeFormat(locale.value, withTime
    ? { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
    : { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(timestamp))
}
const responseList = (payload, key) => Array.isArray(payload) ? payload : (Array.isArray(payload?.[key]) ? payload[key] : [])

const refreshMe = async () => {
  const response = await api('/api/account/me')
  const payload = response.data || {}
  authenticated.value = payload.authenticated === true || Boolean((payload.account || payload.user || payload)?.email)
  me.value = payload.account || payload.user || payload
  return authenticated.value
}

const loadNodes = ({ silent = false } = {}) => {
  if (nodesRequest) return nodesRequest
  if (!silent) nodesLoading.value = true
  nodesRequest = (async () => {
    try {
      const response = await api('/api/account/nodes')
      nodes.value = Array.isArray(response.data) ? response.data : (Array.isArray(response.data?.nodes) ? response.data.nodes : [])
      if (!nodes.value.length && localStorage.getItem('monitor-onboarding-intent') === 'create-node' && nodeCreationAvailable.value) {
        window.setTimeout(() => openNodeForm(), 0)
      }
    } catch (error) {
      if (error?.response?.status === 401) {
        authenticated.value = false
        me.value = null
        nodes.value = []
      } else if (!silent) {
        nodes.value = []
      }
    }
  })().finally(() => {
    nodesLoading.value = false
    nodesRequest = null
  })
  return nodesRequest
}

const loadSubscriptions = async () => {
  subscriptionsLoading.value = true
  try {
    const response = await api('/api/account/subscriptions')
    subscriptions.value = Array.isArray(response.data) ? response.data : []
  } catch {
    subscriptions.value = []
  } finally {
    subscriptionsLoading.value = false
  }
}

const loadReports = async () => {
  reportsLoading.value = true
  try {
    const response = await api('/api/account/market-appeals')
    reports.value = Array.isArray(response.data?.reports) ? response.data.reports : []
    appeals.value = Array.isArray(response.data?.appeals) ? response.data.appeals : []
  } catch {
    reports.value = []
    appeals.value = []
  } finally {
    reportsLoading.value = false
  }
}

const loadNotifications = async () => {
  notificationsLoading.value = true
  try {
    const [summaryResponse, deliveriesResponse] = await Promise.all([
      api('/api/account/notifications'),
      api('/api/account/notifications/deliveries?limit=50')
    ])
    const payload = summaryResponse.data || {}
    notificationBinding.value = { enabled: payload.enabled !== false, ...(payload.binding || {}) }
    const preference = payload.preference || {}
    Object.assign(notificationPreference, {
      enabled: preference.enabled !== false,
      events: Array.isArray(preference.events) ? preference.events : notificationPreference.events,
      node_ids: Array.isArray(preference.node_ids) ? preference.node_ids.map(String) : [],
      quiet_start: preference.quiet_start || '', quiet_end: preference.quiet_end || '',
      time_zone: preference.time_zone || notificationPreference.time_zone,
      delivery_mode: preference.delivery_mode === 'digest' ? 'digest' : 'immediate'
    })
    notificationDeliveries.value = responseList(deliveriesResponse.data, 'deliveries')
  } catch (error) {
    if (error?.response?.status !== 404) Message.error(t('notification-load-fail'))
  } finally {
    notificationsLoading.value = false
  }
}

const stopBindPolling = () => {
  window.clearInterval(bindPollTimer)
  bindPollTimer = 0
}
const pollBindStatus = async () => {
  if (!bindSession.value?.session_id || bindPollBusy) return
  bindPollBusy = true
  try {
    const response = await api(`/api/account/notifications/bind/status?session_id=${encodeURIComponent(bindSession.value.session_id)}`)
    const payload = response.data || {}
    if (payload.status === 'bound') {
      stopBindPolling()
      bindSession.value = null
      Message.success(t('notification-bind-success'))
      await loadNotifications()
    } else if (payload.status === 'expired') {
      stopBindPolling()
      bindSession.value = { ...bindSession.value, status: 'expired' }
    }
  } catch (error) {
    if (error?.response?.status === 404 || error?.response?.status === 410) {
      stopBindPolling()
      bindSession.value = { ...bindSession.value, status: 'expired' }
    }
  } finally {
    bindPollBusy = false
  }
}
const startNotificationBinding = async () => {
  bindStarting.value = true
  stopBindPolling()
  try {
    const response = await api('/api/account/notifications/bind/start', { method: 'POST', data: {} })
    bindSession.value = { ...(response.data || {}), status: 'pending' }
    await pollBindStatus()
    if (bindSession.value?.status === 'pending') bindPollTimer = window.setInterval(pollBindStatus, 2000)
  } catch { Message.error(t('notification-bind-fail')) } finally { bindStarting.value = false }
}
const saveNotificationPreference = async () => {
  notificationSaving.value = true
  try {
    await api('/api/account/notifications/preferences', { method: 'POST', data: {
      enabled: !!notificationPreference.enabled,
      events: notificationPreference.events,
      node_ids: notificationPreference.node_ids,
      quiet_start: notificationPreference.quiet_start,
      quiet_end: notificationPreference.quiet_end,
      time_zone: notificationPreference.time_zone,
      delivery_mode: notificationPreference.delivery_mode
    } })
    Message.success(t('notification-preferences-saved'))
  } catch { Message.error(t('notification-preferences-fail')) } finally { notificationSaving.value = false }
}
const testNotification = async () => {
  notificationTesting.value = true
  try {
    await api('/api/account/notifications/test', { method: 'POST', data: {} })
    Message.success(t('notification-test-success'))
    await loadNotifications()
  } catch { Message.error(t('notification-test-fail')) } finally { notificationTesting.value = false }
}
const unbindNotification = async () => {
  if (!confirm(t('notification-unbind-confirm'))) return
  try {
    await api('/api/account/notifications/unbind', { method: 'POST', data: {} })
    stopBindPolling()
    bindSession.value = null
    Message.success(t('notification-unbind-success'))
    await loadNotifications()
  } catch { Message.error(t('notification-unbind-fail')) }
}

const loadOrders = async () => {
  ordersLoading.value = true
  try {
    const response = await api('/api/account/orders')
    const payload = response.data
    if (Array.isArray(payload)) orders.value = payload
    else if (Array.isArray(payload?.orders)) orders.value = payload.orders
    else orders.value = [
      ...responseList(payload, 'selling').map((item) => ({ ...item, relationship: item.relationship || 'seller' })),
      ...responseList(payload, 'buying').map((item) => ({ ...item, relationship: item.relationship || 'buyer' })),
      ...responseList(payload, 'received').map((item) => ({ ...item, relationship: item.relationship || 'seller' })),
      ...responseList(payload, 'sent').map((item) => ({ ...item, relationship: item.relationship || 'buyer' }))
    ]
  } catch (error) {
    orders.value = []
    if (error?.response?.status !== 404) Message.error(t('order-load-fail'))
  } finally { ordersLoading.value = false }
}
const orderRole = (item) => item.relationship || item.role || (item.seller ? 'seller' : 'buyer')
const orderName = (item) => item.listing_name || item.display_name || item.node_name || item.listing_node_id || item.node_id || '—'
const orderAction = async (item, action) => {
  if ((action === 'completed' || action === 'cancelled') && !confirm(t(`order-action-${action}-confirm`, { name: orderName(item) }))) return
  orderActingID.value = item.id
  try {
    const backendAction = { accepted: 'accept', completed: 'complete', cancelled: 'cancel' }[action] || action
    await api('/api/account/orders/action', { method: 'POST', data: { order_id: item.id, action: backendAction } })
    Message.success(t('order-action-success'))
    await loadOrders()
  } catch { Message.error(t('order-action-fail')) } finally { orderActingID.value = '' }
}
const orderCan = (item, action) => {
  const status = item.status || 'pending'
  const role = orderRole(item)
  if (action === 'accepted') return role === 'seller' && status === 'pending'
  if (action === 'completed') return role === 'seller' && status === 'accepted'
  if (action === 'cancelled') return role === 'buyer' && (status === 'pending' || status === 'accepted')
  return false
}

const loadAccountData = () => Promise.all([loadNodes(), loadSubscriptions(), loadReports()])

const bootstrap = async () => {
  loading.value = true
  try {
    if (await refreshMe()) await loadAccountData()
  } catch {
    authenticated.value = false
    me.value = null
  } finally {
    loading.value = false
  }
}

const submitAuth = async () => {
  authError.value = ''
  if (!authForm.email.trim() || !authForm.password) {
    authError.value = t('account-auth-required')
    return
  }
  if (authMode.value === 'register') {
    if (authForm.password.length < 8) {
      authError.value = t('account-password-min')
      return
    }
    if (authForm.password !== authForm.password_confirm) {
      authError.value = t('account-password-mismatch')
      return
    }
    if (!captchaCode.value) {
      authError.value = t('submit-warn-captcha')
      return
    }
  }
  authLoading.value = true
  try {
    const payload = {
      email: authForm.email.trim(), password: authForm.password,
      remember_me: !!authForm.remember_me
    }
    if (authMode.value === 'register') {
      Object.assign(payload, { password_confirm: authForm.password_confirm, captcha_id: captchaId.value, captcha_code: captchaCode.value })
    }
    await api(`/api/account/${authMode.value}`, { method: 'POST', data: payload })
    Message.success(t(authMode.value === 'register' ? 'account-register-success' : 'owner-login-success'))
    await bootstrap()
  } catch (error) {
    authError.value = typeof error?.response?.data === 'string' ? error.response.data : t(authMode.value === 'register' ? 'account-register-fail' : 'owner-login-fail')
    captchaRef.value?.refresh?.()
  } finally {
    authLoading.value = false
  }
}

const logout = async () => {
  try { await api('/api/account/logout', { method: 'POST', data: {} }) } catch {}
  authenticated.value = false
  me.value = null
  nodes.value = []
  installBox.value = null
  stopBindPolling()
  notificationBinding.value = null
  notificationDeliveries.value = []
  orders.value = []
  activeTab.value = 'nodes'
}

const resetNodeForm = () => {
  Object.assign(nodeForm, { display_name: '', region: '', due_date: '', visibility: 'private' })
  editingNodeID.value = ''
  nodeFormOpen.value = false
}

const openNodeForm = (node = null) => {
  if (!node && !canCreateNode.value) return
  editingNodeID.value = node ? nodeID(node) : ''
  Object.assign(nodeForm, node ? {
    display_name: nodeName(node), region: node.region || '', due_date: dueInput(node), visibility: nodePublic(node) ? 'public' : 'private'
  } : { display_name: '', region: '', due_date: '', visibility: 'private' })
  nodeFormOpen.value = true
}

const saveNode = async () => {
  if (!nodeForm.display_name.trim()) return Message.warning(t('account-node-name-required'))
  nodeSaving.value = true
  try {
    const endpoint = editingNodeID.value ? '/api/account/nodes/update' : '/api/account/nodes'
    const response = await api(endpoint, {
      method: 'POST',
      data: { node_id: editingNodeID.value, display_name: nodeForm.display_name.trim(), region: nodeForm.region.trim(), due_date: nodeForm.due_date }
    })
    const savedNodeID = editingNodeID.value || response.data?.node_id || ''
    if (savedNodeID && (editingNodeID.value || nodeForm.visibility === 'public')) {
      await api('/api/account/nodes/privacy', { method: 'POST', data: { node_id: savedNodeID, private: nodeForm.visibility !== 'public' } })
    }
    if (!editingNodeID.value) installBox.value = response.data || null
    if (!editingNodeID.value) localStorage.removeItem('monitor-onboarding-intent')
    Message.success(t(editingNodeID.value ? 'account-node-updated' : 'account-node-created'))
    resetNodeForm()
    await loadNodes()
  } catch (error) {
    Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('account-node-save-fail'))
  } finally {
    nodeSaving.value = false
  }
}

const deleteNode = async (node) => {
  if (!confirm(t('account-node-delete-confirm', { name: nodeName(node) }))) return
  try {
    await api('/api/account/nodes/delete', { method: 'POST', data: { node_id: nodeID(node) } })
    Message.success(t('owner-deleted'))
    await loadNodes()
  } catch { Message.error(t('owner-delete-fail')) }
}

const showInstall = async (node, reset = false) => {
  if (reset && !confirm(t('account-reset-token-confirm', { name: nodeName(node) }))) return
  try {
    const response = await api('/api/account/nodes/reset-token', { method: 'POST', data: { node_id: nodeID(node), reset } })
    installBox.value = response.data || null
    Message.success(t(reset ? 'owner-token-reset-ok' : 'owner-install-fetched'))
  } catch { Message.error(t('owner-fetch-fail')) }
}

const openListing = (node) => {
  listingNodeID.value = nodeID(node)
  const source = node.listing || node
  const billing = billingOf(source)
  Object.assign(listingForm, {
    display_name: source.display_name || nodeName(node), region: source.region || node.region || '', specs: source.specs || '',
    listing_type: source.listing_type || 'rent', contact: source.contact || '', description: source.description || '',
    price_amount: billing.structured ? billing.amount : '', price_currency: billing.currency || 'USD', billing_cycle: billing.cycle || 'monthly', due_date: dueInput(source)
  })
  activeTab.value = 'market'
}

const closeListing = () => { listingNodeID.value = '' }
const saveListing = async () => {
  if (!listingNodeID.value || !listingForm.display_name.trim() || !(Number(listingForm.price_amount) > 0) || !listingForm.contact.trim()) {
    return Message.warning(t('owner-required-tip'))
  }
  listingSaving.value = true
  try {
    const target = nodes.value.find((node) => nodeID(node) === listingNodeID.value)
    const payload = {
      node_id: listingNodeID.value, listing_type: listingForm.listing_type, contact: listingForm.contact.trim(),
      description: listingForm.description, specs: listingForm.specs, price_amount: Number(listingForm.price_amount),
      price_currency: listingForm.price_currency, billing_cycle: listingForm.billing_cycle,
      price: `${listingForm.price_currency} ${listingForm.price_amount}`
    }
    await api('/api/account/nodes/update', { method: 'POST', data: {
      node_id: listingNodeID.value, display_name: listingForm.display_name.trim(), region: listingForm.region.trim(), due_date: listingForm.due_date
    } })
    if (nodeListed(target)) {
      await api('/api/account/nodes/update', { method: 'POST', data: { ...payload, for_sale: true } })
    } else {
      await api('/api/account/nodes/listing', { method: 'POST', data: payload })
    }
    Message.success(t('account-listing-saved'))
    closeListing()
    await loadNodes()
  } catch (error) {
    Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('owner-save-fail'))
  } finally { listingSaving.value = false }
}

const unlistNode = async (node) => {
  try {
    await api('/api/account/nodes/toggle', { method: 'POST', data: { node_id: nodeID(node), for_sale: false } })
    Message.success(t('owner-unlisted'))
    await loadNodes()
  } catch { Message.error(t('owner-op-fail')) }
}

const resetSubscription = () => {
  Object.assign(subscriptionForm, { id: '', name: '', regions: '', tags: '', max_price: '', currency: 'CNY', min_memory_gb: '', enabled: true })
  subscriptionFormOpen.value = false
}
const openSubscription = (item = null) => {
  Object.assign(subscriptionForm, item ? {
    id: item.id || '', name: item.name || '', regions: (item.regions || []).join(', '), tags: (item.tags || []).join(', '),
    max_price: Number(item.max_price) > 0 ? item.max_price : '', currency: item.currency || 'CNY',
    min_memory_gb: Number(item.min_memory) > 0 ? Number(item.min_memory) / (1024 ** 3) : '', enabled: item.enabled !== false
  } : { id: '', name: '', regions: '', tags: '', max_price: '', currency: 'CNY', min_memory_gb: '', enabled: true })
  subscriptionFormOpen.value = true
}
const saveSubscription = async () => {
  if (!subscriptionForm.name.trim()) return Message.warning(t('owner-subscription-name-required'))
  subscriptionSaving.value = true
  try {
    await api('/api/account/subscriptions', {
      method: 'POST', data: { action: 'save', subscription: {
        id: subscriptionForm.id, name: subscriptionForm.name.trim(), regions: splitValues(subscriptionForm.regions), tags: splitValues(subscriptionForm.tags),
        max_price: Math.max(0, Number(subscriptionForm.max_price) || 0), currency: subscriptionForm.currency,
        min_memory: Math.max(0, Math.round((Number(subscriptionForm.min_memory_gb) || 0) * (1024 ** 3))), enabled: !!subscriptionForm.enabled
      } }
    })
    Message.success(t('owner-subscription-saved'))
    resetSubscription()
    await loadSubscriptions()
  } catch { Message.error(t('owner-subscription-save-fail')) } finally { subscriptionSaving.value = false }
}
const deleteSubscription = async (item) => {
  if (!confirm(t('owner-subscription-delete-confirm', { name: item.name }))) return
  try {
    await api('/api/account/subscriptions', { method: 'POST', data: { action: 'delete', id: item.id } })
    await loadSubscriptions()
  } catch { Message.error(t('owner-subscription-save-fail')) }
}

const appealFor = (reportID) => appeals.value.find((item) => item.report_id === reportID)
const submitAppeal = async () => {
  if (appealMessage.value.trim().length < 10) return Message.warning(t('owner-appeal-min'))
  appealSaving.value = true
  try {
    await api('/api/account/market-appeals', {
      method: 'POST', data: { report_id: appealReportID.value, message: appealMessage.value.trim() }
    })
    Message.success(t('owner-appeal-success'))
    appealReportID.value = ''
    appealMessage.value = ''
    await loadReports()
  } catch { Message.error(t('owner-appeal-fail')) } finally { appealSaving.value = false }
}

watch(registrationAvailable, (enabled) => {
  if (!enabled) authMode.value = 'login'
})
watch(nodeCreationAvailable, (enabled) => {
  if (!enabled && !editingNodeID.value) resetNodeForm()
})
watch(activeTab, (tab) => {
  if (!authenticated.value) return
  if (tab === 'notifications') loadNotifications()
  if (tab === 'orders') loadOrders()
})
onMounted(() => {
  bootstrap()
  nodesRefreshTimer = window.setInterval(() => {
    if (authenticated.value && document.visibilityState === 'visible') loadNodes({ silent: true })
  }, 15_000)
})
onUnmounted(() => {
  window.clearInterval(nodesRefreshTimer)
  stopBindPolling()
})
</script>

<template>
  <section class="account-center">
    <EmptyState v-if="loading" loading />

    <div v-else-if="!authenticated" class="account-auth">
      <BackButton @click="emit('navigate', 'overview')">{{ t('common-back') }}</BackButton>
      <div class="account-auth__intro">
        <img src="/logo.svg" alt="" />
        <h1>{{ t('account-title') }}</h1>
        <p>{{ t('account-auth-subtitle') }}</p>
      </div>
      <div class="account-auth__modes" :class="{ single: !registrationAvailable }" role="tablist" :aria-label="t('account-auth-mode')">
        <button type="button" role="tab" :aria-selected="authMode === 'login'" :class="{ active: authMode === 'login' }" @click="authMode = 'login'; authError = ''">{{ t('auth-login') }}</button>
        <button v-if="registrationAvailable" type="button" role="tab" :aria-selected="authMode === 'register'" :class="{ active: authMode === 'register' }" @click="authMode = 'register'; authError = ''">{{ t('account-register') }}</button>
      </div>
      <form class="account-auth__form" @submit.prevent="submitAuth">
        <p v-if="!registrationAvailable" class="account-auth__notice">{{ t('account-registration-disabled') }}</p>
        <p v-if="authError" class="account-auth__error" role="alert">{{ authError }}</p>
        <label>{{ t('auth-email-label') }}</label>
        <BaseInput v-model="authForm.email" type="email" autocomplete="email" required />
        <label>{{ t('auth-password-label') }}</label>
        <BaseInput v-model="authForm.password" type="password" :autocomplete="authMode === 'register' ? 'new-password' : 'current-password'" minlength="8" required />
        <template v-if="authMode === 'register'">
          <label>{{ t('submit-password-confirm-label') }}</label>
          <BaseInput v-model="authForm.password_confirm" type="password" autocomplete="new-password" minlength="8" required />
          <label>{{ t('submit-captcha-label') }}</label>
          <CaptchaInput ref="captchaRef" v-model="captchaCode" endpoint="/api/account/captcha" :api-base="apiBase" @update:captcha-id="captchaId = $event" />
        </template>
        <label v-else class="account-check"><input v-model="authForm.remember_me" type="checkbox" /> {{ t('auth-remember') }}</label>
        <BaseButton type="submit" variant="primary" size="lg" block :disabled="authLoading">{{ authLoading ? t('common-loading') : t(authMode === 'register' ? 'account-register-submit' : 'auth-login') }}</BaseButton>
      </form>
    </div>

    <template v-else>
      <header class="account-head">
        <div><p>{{ t('account-eyebrow') }}</p><h1>{{ t('account-title') }}</h1><span>{{ me?.email }}</span></div>
        <div><BaseButton size="sm" @click="emit('navigate', 'overview')">{{ t('nav-overview') }}</BaseButton><BaseButton size="sm" @click="logout">{{ t('owner-logout') }}</BaseButton></div>
      </header>

      <nav class="account-tabs" :aria-label="t('account-tabs-label')">
        <button v-for="tab in tabs" :key="tab.key" type="button" :class="{ active: activeTab === tab.key }" @click="activeTab = tab.key">{{ tab.label }}</button>
      </nav>

      <div v-if="activeTab === 'nodes'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('account-nodes-title') }}</h2><p>{{ t(nodeCreationAvailable ? 'account-nodes-subtitle' : 'account-nodes-disabled') }}<span v-if="effectiveNodeLimit"> · {{ t('account-node-limit', { used: nodes.length, limit: effectiveNodeLimit }) }}</span></p></div><BaseButton v-if="canCreateNode" size="sm" variant="primary" @click="openNodeForm()">{{ t('account-node-add') }}</BaseButton></div>
        <form v-if="nodeFormOpen" class="account-tool" @submit.prevent="saveNode">
          <div class="account-tool__head"><strong>{{ t(editingNodeID ? 'account-node-edit' : 'account-node-create') }}</strong><BaseButton variant="text" size="sm" @click="resetNodeForm">{{ t('common-close') }}</BaseButton></div>
          <div class="account-form-grid">
            <label>{{ t('account-node-name') }}</label><BaseInput v-model="nodeForm.display_name" maxlength="64" required />
            <label>{{ t('account-node-region') }}</label><BaseInput v-model="nodeForm.region" maxlength="32" :placeholder="t('owner-region-placeholder')" />
            <label>{{ t('account-node-due') }}</label><BaseDatePicker v-model="nodeForm.due_date" />
            <label>{{ t('account-node-visibility') }}</label><BaseInput as="select" v-model="nodeForm.visibility"><option value="private">{{ t('account-node-private') }}</option><option value="public">{{ t('account-node-public') }}</option></BaseInput>
          </div>
          <div class="account-tool__actions"><BaseButton size="sm" variant="primary" type="submit" :disabled="nodeSaving">{{ nodeSaving ? t('common-saving') : t('common-save') }}</BaseButton><BaseButton size="sm" @click="resetNodeForm">{{ t('common-cancel') }}</BaseButton></div>
        </form>
        <div v-if="installBox" class="account-install onboarding-install">
          <div class="account-tool__head"><div><span>{{ t('onboarding-created-badge') }}</span><strong>{{ t('account-install-title') }}</strong></div><BaseButton variant="text" size="sm" @click="installBox = null">{{ t('common-close') }}</BaseButton></div>
          <div class="onboarding-progress"><span class="is-done"><b>1</b>{{ t('onboarding-progress-created') }}</span><i></i><span class="is-active"><b>2</b>{{ t('onboarding-progress-install') }}</span><i></i><span><b>3</b>{{ t('onboarding-progress-wait') }}</span></div>
          <p>{{ t('onboarding-install-hint') }}</p>
          <label>Linux</label><CopyCommandBox :command="installBox.linux || installBox.linux_install" />
          <label>Windows</label><CopyCommandBox :command="installBox.windows || installBox.windows_install" />
          <div class="onboarding-wait"><span></span><div><strong>{{ t('onboarding-wait-title') }}</strong><small>{{ t('onboarding-wait-desc') }}</small></div></div>
        </div>
        <EmptyState v-if="nodesLoading" loading />
        <section v-else-if="!nodes.length && !nodeFormOpen" class="account-first-node"><span>{{ t('onboarding-eyebrow') }}</span><h3>{{ t('account-first-node-title') }}</h3><p>{{ t('account-first-node-subtitle') }}</p><div><small>1 · {{ t('onboarding-progress-created') }}</small><small>2 · {{ t('onboarding-progress-install') }}</small><small>3 · {{ t('onboarding-progress-wait') }}</small></div><BaseButton v-if="canCreateNode" variant="primary" @click="openNodeForm()">{{ t('account-node-add-first') }}</BaseButton></section>
        <div v-else class="account-node-list">
          <article v-for="node in nodes" :key="nodeID(node)" class="account-node glow-card">
            <header><div><span class="account-node__status" :class="nodeOnline(node) ? 'online' : 'offline'"></span><strong>{{ nodeName(node) }}</strong></div><div class="account-node__badges"><span>{{ t(nodePublic(node) ? 'account-node-public' : 'account-node-private') }}</span><span v-if="nodeListed(node)" class="listed">{{ t('account-node-listed') }}</span></div></header>
            <div class="account-node__meta"><span>{{ node.region || t('market-no-region') }}</span><span>{{ t('account-node-due-value', { date: nodeDue(node) }) }}</span><span>{{ nodeOnline(node) ? t('online') : t('offline') }}</span></div>
            <div class="account-node__metrics">
              <span><small>CPU</small><strong>{{ percentText(nodeCPU(node)) }}</strong></span>
              <span><small>{{ t('chart-memory') }}</small><strong>{{ percentText(nodeMemory(node)) }}</strong></span>
              <span><small>{{ t('chart-network') }}</small><strong>{{ formatBytes(nodeNetwork(node)) }}/s</strong></span>
              <span><small>{{ t('account-node-traffic') }}</small><strong>{{ formatBytes(nodeTraffic(node)) }}</strong></span>
            </div>
            <footer><BaseButton size="sm" @click="openNodeForm(node)">{{ t('owner-edit') }}</BaseButton><BaseButton v-if="node.has_token && node.can_view_token !== false" size="sm" @click="showInstall(node, false)">{{ t('owner-install-cmd') }}</BaseButton><BaseButton size="sm" @click="showInstall(node, true)">{{ t('owner-reset-token') }}</BaseButton><BaseButton v-if="marketAvailable && !nodeListed(node)" size="sm" variant="primary" @click="openListing(node)">{{ t('account-list-market') }}</BaseButton><BaseButton size="sm" variant="danger" @click="deleteNode(node)">{{ t('owner-delete') }}</BaseButton></footer>
          </article>
        </div>
      </div>

      <ServiceMonitorPanel v-else-if="activeTab === 'services'" :api-base="apiBase" />

      <div v-else-if="activeTab === 'market'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('account-market-title') }}</h2><p>{{ t('account-market-subtitle') }}</p></div><BaseButton v-if="marketAvailable" size="sm" @click="emit('navigate', 'market')">{{ t('nav-market') }}</BaseButton></div>
        <p v-if="!marketAvailable" class="account-market-disabled">{{ t('account-market-disabled') }}</p>
        <form v-if="listingNodeID" class="account-tool" @submit.prevent="saveListing">
          <div class="account-tool__head"><strong>{{ t('account-listing-edit') }}</strong><BaseButton variant="text" size="sm" @click="closeListing">{{ t('common-close') }}</BaseButton></div>
          <div class="account-form-grid">
            <label>{{ t('owner-field-name') }}</label><BaseInput v-model="listingForm.display_name" maxlength="64" required />
            <label>{{ t('owner-field-region') }}</label><BaseInput v-model="listingForm.region" maxlength="32" />
            <label>{{ t('owner-field-specs') }}</label><BaseInput v-model="listingForm.specs" maxlength="200" />
            <label>{{ t('owner-field-price') }}</label><BillingFields v-model:amount="listingForm.price_amount" v-model:currency="listingForm.price_currency" v-model:cycle="listingForm.billing_cycle" required />
            <label>{{ t('owner-field-type') }}</label><BaseInput as="select" v-model="listingForm.listing_type"><option value="rent">{{ t('market-type-rent') }}</option><option value="sale">{{ t('market-type-sale') }}</option><option value="transfer">{{ t('market-type-transfer') }}</option></BaseInput>
            <label>{{ t('owner-field-contact') }}</label><BaseInput v-model="listingForm.contact" maxlength="120" required />
            <label>{{ t('owner-field-desc') }}</label><BaseInput as="textarea" v-model="listingForm.description" autogrow :max-rows="4" maxlength="500" />
            <label>{{ t('owner-field-due') }}</label><BaseDatePicker v-model="listingForm.due_date" />
          </div>
          <div class="account-tool__actions"><BaseButton size="sm" variant="primary" type="submit" :disabled="listingSaving">{{ listingSaving ? t('common-saving') : t('common-save') }}</BaseButton><BaseButton size="sm" @click="closeListing">{{ t('common-cancel') }}</BaseButton></div>
        </form>
        <EmptyState v-if="!nodes.length" :text="t('account-market-no-nodes')" />
        <div v-else class="account-market-list">
          <article v-for="node in nodes" :key="nodeID(node)"><div><strong>{{ nodeName(node) }}</strong><span>{{ t(nodeListed(node) ? 'account-node-listed' : 'account-node-unlisted') }}</span><small>{{ node.region || t('market-no-region') }}</small></div><div v-if="marketAvailable"><BaseButton size="sm" :variant="nodeListed(node) ? 'default' : 'primary'" @click="openListing(node)">{{ t(nodeListed(node) ? 'account-listing-edit' : 'account-list-market') }}</BaseButton><BaseButton v-if="nodeListed(node)" size="sm" @click="unlistNode(node)">{{ t('owner-unlist') }}</BaseButton></div></article>
        </div>
      </div>

      <div v-else-if="activeTab === 'subscriptions'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('owner-subscriptions-title') }}</h2><p>{{ t('owner-subscriptions-subtitle') }}</p></div><BaseButton v-if="marketAvailable" size="sm" variant="primary" @click="openSubscription()">{{ t('owner-subscription-add') }}</BaseButton></div>
        <p v-if="!marketAvailable" class="account-market-disabled">{{ t('account-market-features-paused') }}</p>
        <form v-if="subscriptionFormOpen && marketAvailable" class="account-tool" @submit.prevent="saveSubscription">
          <div class="account-tool__head"><strong>{{ t(subscriptionForm.id ? 'owner-subscription-edit' : 'owner-subscription-create') }}</strong><BaseButton variant="text" size="sm" @click="resetSubscription">{{ t('common-close') }}</BaseButton></div>
          <div class="account-form-grid">
            <label>{{ t('owner-subscription-name') }}</label><BaseInput v-model="subscriptionForm.name" maxlength="64" required />
            <label>{{ t('owner-subscription-regions') }}</label><BaseInput v-model="subscriptionForm.regions" :placeholder="t('owner-subscription-regions-placeholder')" />
            <label>{{ t('owner-subscription-tags') }}</label><BaseInput v-model="subscriptionForm.tags" :placeholder="t('owner-subscription-tags-placeholder')" />
            <label>{{ t('owner-subscription-max-price') }}</label><div class="account-split"><BaseInput v-model="subscriptionForm.max_price" type="number" min="0" step="0.01" /><BaseInput as="select" v-model="subscriptionForm.currency"><option>CNY</option><option>USD</option><option>HKD</option><option>EUR</option><option>JPY</option></BaseInput></div>
            <label>{{ t('owner-subscription-min-memory') }}</label><BaseInput v-model="subscriptionForm.min_memory_gb" type="number" min="0" step="0.5" />
            <label class="account-check"><input v-model="subscriptionForm.enabled" type="checkbox" /> {{ t('owner-subscription-enabled') }}</label>
          </div>
          <div class="account-tool__actions"><BaseButton type="submit" size="sm" variant="primary" :disabled="subscriptionSaving">{{ subscriptionSaving ? t('common-saving') : t('common-save') }}</BaseButton><BaseButton size="sm" @click="resetSubscription">{{ t('common-cancel') }}</BaseButton></div>
        </form>
        <EmptyState v-if="subscriptionsLoading" loading />
        <EmptyState v-else-if="!subscriptions.length" :text="t('owner-subscriptions-empty')" />
        <div v-else class="account-simple-list"><article v-for="item in subscriptions" :key="item.id"><div><strong>{{ item.name }}</strong><small>{{ item.regions?.join(t('common-list-sep')) || t('owner-subscription-any-condition') }}</small></div><div v-if="marketAvailable"><BaseButton size="sm" @click="openSubscription(item)">{{ t('owner-edit') }}</BaseButton><BaseButton size="sm" variant="danger" @click="deleteSubscription(item)">{{ t('owner-subscription-delete') }}</BaseButton></div></article></div>
      </div>

      <div v-else-if="activeTab === 'reports'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('account-reports-title') }}</h2><p>{{ t('account-reports-subtitle') }}</p></div></div>
        <p v-if="!marketAvailable" class="account-market-disabled">{{ t('account-market-features-paused') }}</p>
        <EmptyState v-if="reportsLoading" loading />
        <EmptyState v-else-if="!reports.length" :text="t('account-reports-empty')" />
        <div v-else class="account-report-list"><article v-for="report in reports" :key="report.id"><header><strong>{{ report.category || t('account-report-default-title') }}</strong><span>{{ t(`owner-report-status-${report.status || 'pending'}`) }}</span></header><p v-if="report.message">{{ report.message }}</p><small v-if="report.resolution">{{ report.resolution }}</small><div v-if="appealFor(report.id)" class="account-appeal-note">{{ t(`owner-appeal-status-${appealFor(report.id).status || 'pending'}`) }}</div><BaseButton v-else-if="marketAvailable && report.status === 'resolved'" size="sm" @click="appealReportID = report.id">{{ t('owner-appeal-action') }}</BaseButton></article></div>
        <form v-if="appealReportID && marketAvailable" class="account-tool" @submit.prevent="submitAppeal"><div class="account-tool__head"><strong>{{ t('owner-appeal-title') }}</strong><BaseButton variant="text" size="sm" @click="appealReportID = ''">{{ t('common-close') }}</BaseButton></div><BaseInput as="textarea" v-model="appealMessage" autogrow :max-rows="5" maxlength="800" :placeholder="t('owner-appeal-placeholder')" /><BaseButton type="submit" size="sm" variant="primary" :disabled="appealSaving">{{ appealSaving ? t('common-saving') : t('owner-appeal-submit') }}</BaseButton></form>
      </div>

      <div v-else-if="activeTab === 'notifications'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('notification-title') }}</h2><p>{{ t('notification-subtitle') }}</p></div><BaseButton size="sm" :disabled="notificationsLoading" @click="loadNotifications">{{ t('common-retry') }}</BaseButton></div>
        <EmptyState v-if="notificationsLoading && !notificationBinding" loading />
        <template v-else>
          <p v-if="notificationBinding?.enabled === false" class="account-market-disabled">{{ t('notification-disabled') }}</p>
          <section v-else-if="bindSession || !notificationBinding?.bound" class="notification-bind glow-card">
            <div class="notification-bind__copy"><span class="notification-provider">ShowDoc Push</span><h3>{{ t('notification-bind-title') }}</h3><p>{{ t('notification-bind-subtitle') }}</p><ol><li>{{ t('notification-bind-follow') }}</li><li>{{ t('notification-bind-existing') }}</li></ol></div>
            <div class="notification-bind__qr">
              <template v-if="bindSession?.qr_code_url && bindSession.status !== 'expired'"><img :src="bindSession.qr_code_url" :alt="t('notification-qr-alt')" /><strong>{{ t('notification-waiting-scan') }}</strong><small>{{ t('notification-qr-expires', { time: accountTimestamp(bindSession.expires_at) }) }}</small></template>
              <template v-else><div class="notification-qr-placeholder">QR</div><strong v-if="bindSession?.status === 'expired'">{{ t('notification-qr-expired') }}</strong><BaseButton size="sm" variant="primary" :disabled="bindStarting" @click="startNotificationBinding">{{ bindStarting ? t('common-loading') : t(bindSession ? 'notification-qr-refresh' : 'notification-bind-start') }}</BaseButton></template>
            </div>
          </section>
          <template v-else>
            <section class="notification-status">
              <div><span class="notification-status__dot"></span><div><strong>{{ t('notification-bound') }}</strong><small>{{ t('notification-token-suffix', { suffix: notificationBinding.token_suffix || '----' }) }} · {{ t('notification-bound-at', { time: accountTimestamp(notificationBinding.bound_at) }) }}</small></div></div>
              <div><BaseButton size="sm" :disabled="notificationTesting" @click="testNotification">{{ notificationTesting ? t('common-loading') : t('notification-test') }}</BaseButton><BaseButton size="sm" @click="startNotificationBinding">{{ t('notification-rebind') }}</BaseButton><BaseButton size="sm" variant="danger" @click="unbindNotification">{{ t('notification-unbind') }}</BaseButton></div>
            </section>
            <p v-if="notificationBinding.last_error" class="notification-error">{{ t('notification-last-error', { error: notificationBinding.last_error }) }}</p>

            <form class="notification-preferences" @submit.prevent="saveNotificationPreference">
              <div class="notification-section-head"><div><h3>{{ t('notification-preferences-title') }}</h3><p>{{ t('notification-preferences-subtitle') }}</p></div><label class="notification-switch"><input v-model="notificationPreference.enabled" type="checkbox" /><span>{{ t(notificationPreference.enabled ? 'notification-enabled' : 'notification-paused') }}</span></label></div>
              <fieldset :disabled="!notificationPreference.enabled"><legend>{{ t('notification-events-title') }}</legend><div class="notification-event-grid"><label v-for="event in notificationEvents" :key="event.key"><input v-model="notificationPreference.events" type="checkbox" :value="event.key" /><span>{{ t(event.label) }}</span></label></div></fieldset>
              <fieldset :disabled="!notificationPreference.enabled"><legend>{{ t('notification-nodes-title') }}</legend><p>{{ t('notification-nodes-hint') }}</p><div v-if="nodes.length" class="notification-node-grid"><label v-for="node in nodes" :key="nodeID(node)"><input v-model="notificationPreference.node_ids" type="checkbox" :value="nodeID(node)" /><span>{{ nodeName(node) }}</span></label></div><small v-else>{{ t('account-nodes-empty') }}</small></fieldset>
              <fieldset :disabled="!notificationPreference.enabled"><legend>{{ t('notification-delivery-title') }}</legend><div class="notification-delivery-grid"><label>{{ t('notification-mode') }}<BaseInput as="select" v-model="notificationPreference.delivery_mode"><option value="immediate">{{ t('notification-mode-immediate') }}</option><option value="digest">{{ t('notification-mode-digest') }}</option></BaseInput></label><label>{{ t('notification-time-zone') }}<BaseInput v-model="notificationPreference.time_zone" maxlength="64" /></label><label>{{ t('notification-quiet-start') }}<BaseInput v-model="notificationPreference.quiet_start" type="time" /></label><label>{{ t('notification-quiet-end') }}<BaseInput v-model="notificationPreference.quiet_end" type="time" /></label></div><p>{{ t('notification-quiet-hint') }}</p></fieldset>
              <BaseButton type="submit" size="sm" variant="primary" :disabled="notificationSaving">{{ notificationSaving ? t('common-saving') : t('notification-save') }}</BaseButton>
            </form>

            <section class="notification-history"><div class="notification-section-head"><div><h3>{{ t('notification-history-title') }}</h3><p>{{ t('notification-history-subtitle') }}</p></div></div><EmptyState v-if="!notificationDeliveries.length" :text="t('notification-history-empty')" /><div v-else class="notification-history__list"><article v-for="item in notificationDeliveries" :key="item.id"><div><strong>{{ item.title || t('notification-default-title') }}</strong><span :class="`is-${item.status || 'pending'}`">{{ t(`notification-status-${item.status || 'pending'}`) }}</span></div><p>{{ item.content }}</p><small>{{ accountTimestamp(item.created_at) }}<template v-if="item.attempts"> · {{ t('notification-attempts', { count: item.attempts }) }}</template></small><small v-if="item.error" class="is-error">{{ item.error }}</small></article></div></section>
          </template>
        </template>
      </div>

      <div v-else-if="activeTab === 'orders'" class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('orders-title') }}</h2><p>{{ t('orders-subtitle') }}</p></div><BaseButton size="sm" :disabled="ordersLoading" @click="loadOrders">{{ t('common-retry') }}</BaseButton></div>
        <p v-if="!marketAvailable" class="account-market-disabled">{{ t('orders-market-disabled') }}</p>
        <EmptyState v-if="ordersLoading" loading />
        <EmptyState v-else-if="!orders.length" :text="t('orders-empty')" />
        <div v-else class="order-list"><article v-for="item in orders" :key="item.id" class="order-card glow-card"><header><div><span>{{ t(orderRole(item) === 'seller' ? 'order-received' : 'order-sent') }}</span><strong>{{ orderName(item) }}</strong></div><em :class="`is-${item.status || 'pending'}`">{{ t(`order-status-${item.status || 'pending'}`) }}</em></header><dl><div><dt>{{ t('order-contact') }}</dt><dd>{{ item.buyer_contact || item.contact || '—' }}</dd></div><div><dt>{{ t('order-created-at') }}</dt><dd>{{ accountTimestamp(item.created_at) }}</dd></div><div v-if="item.expires_at"><dt>{{ t('order-expires-at') }}</dt><dd>{{ accountTimestamp(item.expires_at) }}</dd></div></dl><p v-if="item.message">{{ item.message }}</p><footer><BaseButton v-if="orderCan(item, 'accepted')" size="sm" variant="primary" :disabled="orderActingID === item.id" @click="orderAction(item, 'accepted')">{{ t('order-action-accept') }}</BaseButton><BaseButton v-if="orderCan(item, 'completed')" size="sm" variant="primary" :disabled="orderActingID === item.id" @click="orderAction(item, 'completed')">{{ t('order-action-complete') }}</BaseButton><BaseButton v-if="orderCan(item, 'cancelled')" size="sm" variant="danger" :disabled="orderActingID === item.id" @click="orderAction(item, 'cancelled')">{{ t('order-action-cancel') }}</BaseButton></footer></article></div>
      </div>

      <div v-else class="account-panel">
        <div class="account-panel__head"><div><h2>{{ t('account-security-title') }}</h2><p>{{ t('account-security-subtitle') }}</p></div></div>
        <div class="account-security-summary"><span>{{ t('auth-email-label') }}</span><strong>{{ me?.email }}</strong><small>{{ t('account-security-session') }}</small></div>
        <div class="account-security-facts">
          <div><span>{{ t('account-security-created') }}</span><strong>{{ me?.created_at ? new Intl.DateTimeFormat(locale, { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(Number(me.created_at) < 1000000000000 ? Number(me.created_at) * 1000 : Number(me.created_at))) : '—' }}</strong></div>
          <div><span>{{ t('account-security-tokens') }}</span><strong>{{ nodes.filter((node) => node.has_token).length }}</strong></div>
          <div><span>{{ t('account-security-visible-tokens') }}</span><strong>{{ nodes.filter((node) => node.can_view_token).length }}</strong></div>
        </div>
        <div class="account-danger"><div><strong>{{ t('account-logout-title') }}</strong><p>{{ t('account-logout-text') }}</p></div><BaseButton size="sm" @click="logout">{{ t('owner-logout') }}</BaseButton></div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.account-center{width:min(1040px,calc(100% - 32px));min-height:520px;margin:0 auto;padding:26px 0 48px;color:var(--color-text-1,#1d2129)}
.account-auth{width:min(430px,100%);margin:20px auto 0}.account-auth__intro{text-align:center}.account-auth__intro img{width:48px;height:48px}.account-auth__intro h1{margin:10px 0 5px;font-size:24px}.account-auth__intro p{margin:0;color:var(--color-text-3,#86909c);font-size:13px}.account-auth__modes{display:grid;grid-template-columns:repeat(2,1fr);margin:22px 0 14px;padding:3px;border-radius:8px;background:var(--color-fill-2,#f2f3f5)}.account-auth__modes.single{grid-template-columns:1fr}.account-auth__modes button{height:34px;border:0;border-radius:6px;background:transparent;color:var(--color-text-3,#86909c);cursor:pointer;font:inherit;font-size:13px}.account-auth__modes button.active{background:var(--color-bg-2,#fff);color:var(--color-text-1,#1d2129);box-shadow:0 1px 4px rgba(15,23,42,.1)}.account-auth__form{display:grid;gap:8px;padding:18px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px}.account-auth__form>label,.account-form-grid>label,.account-install>label{color:var(--color-text-2,#4e5969);font-size:12px}.account-auth__error,.account-auth__notice{margin:0;padding:8px 10px;font-size:12px}.account-auth__error{border-left:2px solid #f53f3f;background:rgba(245,63,63,.06);color:#d03050}.account-auth__notice{border-left:2px solid #ff7d00;background:rgba(255,125,0,.06);color:var(--color-text-2,#4e5969)}
.account-check{display:flex!important;grid-column:1/-1;align-items:center;gap:7px}.account-head{display:flex;align-items:flex-end;justify-content:space-between;gap:16px}.account-head p{margin:0 0 5px;color:#165dff;font-size:10px;font-weight:800}.account-head h1{margin:0;font-size:25px}.account-head span{display:block;margin-top:5px;color:var(--color-text-3,#86909c);font-size:12px}.account-head>div:last-child{display:flex;gap:7px}.account-tabs{display:flex;margin:22px 0 24px;overflow-x:auto;border-block:1px solid var(--color-border-2,#e5e6eb);scrollbar-width:none}.account-tabs::-webkit-scrollbar{display:none}.account-tabs button{position:relative;height:48px;padding:0 18px;flex:none;border:0;background:transparent;color:var(--color-text-3,#86909c);cursor:pointer;font:inherit;font-size:12px;font-weight:650}.account-tabs button.active{color:var(--color-text-1,#1d2129)}.account-tabs button.active::after{position:absolute;right:18px;bottom:-1px;left:18px;height:2px;background:#165dff;content:''}
.account-panel__head{display:flex;margin-bottom:16px;align-items:flex-start;justify-content:space-between;gap:16px}.account-panel__head h2{margin:0;font-size:17px}.account-panel__head p{margin:5px 0 0;color:var(--color-text-3,#86909c);font-size:12px}.account-tool,.account-install{display:grid;margin:0 0 18px;padding:14px;gap:10px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-fill-1,#f7f8fa)}.account-tool__head{display:flex;align-items:center;justify-content:space-between;font-size:13px}.account-form-grid{display:grid;grid-template-columns:120px minmax(0,1fr);align-items:center;gap:8px 10px}.account-tool__actions{display:flex;gap:8px}.account-split{display:grid;grid-template-columns:1fr 100px;gap:8px}
.account-node-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.account-node{display:grid;padding:14px;gap:11px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.account-node>header{display:flex;min-width:0;align-items:center;justify-content:space-between;gap:10px}.account-node>header>div:first-child{display:flex;min-width:0;align-items:center;gap:7px}.account-node>header strong{overflow:hidden;font-size:14px;text-overflow:ellipsis;white-space:nowrap}.account-node__status{width:8px;height:8px;flex:none;border-radius:50%}.account-node__status.online{background:#00b42a}.account-node__status.offline{background:#f53f3f}.account-node__badges{display:flex;flex:none;gap:4px}.account-node__badges span{padding:2px 5px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);color:var(--color-text-3,#86909c);font-size:9px}.account-node__badges span.listed{background:rgba(22,93,255,.08);color:#165dff}.account-node__meta{display:flex;min-width:0;gap:7px;color:var(--color-text-3,#86909c);font-size:10px}.account-node__meta span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.account-node__metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:6px}.account-node__metrics span{display:grid;min-width:0;padding:8px;border-radius:6px;background:var(--color-fill-1,#f7f8fa);gap:3px}.account-node__metrics small{color:var(--color-text-3,#86909c);font-size:9px}.account-node__metrics strong{overflow:hidden;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.account-node>footer{display:flex;flex-wrap:wrap;gap:6px;padding-top:9px;border-top:1px dashed var(--color-border-2,#e5e6eb)}
.account-market-list,.account-simple-list{border-top:1px solid var(--color-border-2,#e5e6eb)}.account-market-list article,.account-simple-list article{display:flex;min-height:68px;padding:9px 4px;align-items:center;justify-content:space-between;gap:12px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.account-market-list article>div:first-child,.account-simple-list article>div:first-child{display:grid;min-width:0;gap:3px}.account-market-list strong,.account-simple-list strong{font-size:13px}.account-market-list span{color:#165dff;font-size:10px}.account-market-list small,.account-simple-list small{overflow:hidden;color:var(--color-text-3,#86909c);font-size:11px;text-overflow:ellipsis;white-space:nowrap}.account-market-list article>div:last-child,.account-simple-list article>div:last-child{display:flex;flex:none;gap:6px}
.account-market-disabled{margin:0 0 14px;padding:9px 11px;border-left:2px solid #ff7d00;background:rgba(255,125,0,.06);color:var(--color-text-2,#4e5969);font-size:12px}
.account-first-node{display:grid;justify-items:start;padding:24px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.account-first-node>span{color:#165dff;font-size:9px;font-weight:800}.account-first-node h3{margin:6px 0 4px;font-size:17px}.account-first-node p{margin:0;color:var(--color-text-3,#86909c);font-size:11px}.account-first-node>div{display:flex;margin:16px 0;gap:12px}.account-first-node small{padding:5px 8px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);color:var(--color-text-2,#4e5969);font-size:9px}.onboarding-install .account-tool__head>div{display:grid;gap:3px}.onboarding-install .account-tool__head span{color:#008f24;font-size:9px;font-weight:800}.onboarding-install>p{margin:0;color:var(--color-text-3,#86909c);font-size:10px}.onboarding-progress{display:flex;align-items:center;gap:7px}.onboarding-progress>span{display:flex;align-items:center;gap:4px;color:var(--color-text-3,#86909c);font-size:9px}.onboarding-progress b{display:grid;width:18px;height:18px;place-items:center;border-radius:50%;background:var(--color-fill-3,#e5e6eb);font-size:8px}.onboarding-progress .is-done{color:#008f24}.onboarding-progress .is-done b{background:rgba(0,180,42,.1)}.onboarding-progress .is-active{color:#165dff}.onboarding-progress .is-active b{background:rgba(22,93,255,.1)}.onboarding-progress i{width:24px;height:1px;background:var(--color-border-2,#e5e6eb)}.onboarding-wait{display:flex;padding:9px;align-items:center;gap:8px;border-left:2px solid #165dff;background:rgba(22,93,255,.05)}.onboarding-wait>span{width:8px;height:8px;border:2px solid rgba(22,93,255,.25);border-top-color:#165dff;border-radius:50%;animation:onboarding-spin 1s linear infinite}.onboarding-wait>div{display:grid;gap:2px}.onboarding-wait strong{font-size:10px}.onboarding-wait small{color:var(--color-text-3,#86909c);font-size:9px}@keyframes onboarding-spin{to{transform:rotate(360deg)}}
.account-report-list{display:grid;gap:10px}.account-report-list article{display:grid;padding:13px 4px;gap:7px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.account-report-list header{display:flex;align-items:center;justify-content:space-between;gap:10px}.account-report-list header strong{font-size:13px}.account-report-list header span{padding:2px 5px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);font-size:10px}.account-report-list p{margin:0;color:var(--color-text-2,#4e5969);font-size:12px}.account-report-list small,.account-appeal-note{color:var(--color-text-3,#86909c);font-size:11px}.account-report-list .base-btn{justify-self:start}.account-security-summary{display:grid;margin-bottom:18px;padding:14px 0;grid-template-columns:120px 1fr;gap:5px 10px;border-block:1px solid var(--color-border-2,#e5e6eb)}.account-security-summary span,.account-security-summary small{color:var(--color-text-3,#86909c);font-size:11px}.account-security-summary small{grid-column:2}.account-danger{display:flex;padding:16px 0;align-items:center;justify-content:space-between;gap:14px;border-top:1px solid var(--color-border-2,#e5e6eb)}.account-danger strong{font-size:13px}.account-danger p{margin:4px 0 0;color:var(--color-text-3,#86909c);font-size:11px}
.account-security-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));margin-bottom:18px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.account-security-facts>div{display:grid;padding:12px 10px;gap:4px;border-right:1px solid var(--color-border-2,#e5e6eb)}.account-security-facts>div:last-child{border-right:0}.account-security-facts span{color:var(--color-text-3,#86909c);font-size:10px}.account-security-facts strong{font-size:14px;font-variant-numeric:tabular-nums}
.notification-bind{display:grid;grid-template-columns:minmax(0,1fr) 230px;padding:22px;align-items:center;gap:24px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.notification-bind__copy h3,.notification-section-head h3{margin:7px 0 5px;font-size:15px}.notification-bind__copy p,.notification-section-head p,.notification-preferences fieldset>p{margin:0;color:var(--color-text-3,#86909c);font-size:11px;line-height:1.6}.notification-bind__copy ol{margin:14px 0 0;padding-left:18px;color:var(--color-text-2,#4e5969);font-size:12px;line-height:1.8}.notification-provider{display:inline-flex;padding:3px 7px;border-radius:4px;background:rgba(0,180,42,.08);color:#008f24;font-size:10px;font-weight:800}.notification-bind__qr{display:grid;justify-items:center;gap:8px;text-align:center}.notification-bind__qr img,.notification-qr-placeholder{width:168px;height:168px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:#fff}.notification-bind__qr img{display:block;object-fit:contain}.notification-qr-placeholder{display:grid;place-items:center;color:var(--color-text-3,#86909c);font-size:28px;font-weight:800}.notification-bind__qr strong{font-size:12px}.notification-bind__qr small{color:var(--color-text-3,#86909c);font-size:10px}.notification-status{display:flex;padding:14px 0;align-items:center;justify-content:space-between;gap:14px;border-block:1px solid var(--color-border-2,#e5e6eb)}.notification-status>div{display:flex;align-items:center;gap:9px}.notification-status>div:last-child{flex-wrap:wrap;justify-content:flex-end;gap:6px}.notification-status__dot{width:9px;height:9px;flex:none;border-radius:50%;background:#00b42a;box-shadow:0 0 0 4px rgba(0,180,42,.1)}.notification-status strong,.notification-status small{display:block}.notification-status strong{font-size:13px}.notification-status small{margin-top:3px;color:var(--color-text-3,#86909c);font-size:10px}.notification-error{margin:10px 0 0;padding:8px 10px;border-left:2px solid #f53f3f;background:rgba(245,63,63,.06);color:#d03050;font-size:11px}.notification-preferences,.notification-history{display:grid;margin-top:22px;gap:14px}.notification-section-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.notification-switch{display:flex;flex:none;align-items:center;gap:6px;color:var(--color-text-2,#4e5969);font-size:11px}.notification-preferences fieldset{min-width:0;margin:0;padding:13px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px}.notification-preferences fieldset:disabled{opacity:.55}.notification-preferences legend{padding:0 5px;font-size:12px;font-weight:700}.notification-event-grid,.notification-node-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:7px 10px}.notification-event-grid label,.notification-node-grid label{display:flex;min-width:0;align-items:center;gap:6px;color:var(--color-text-2,#4e5969);font-size:11px}.notification-event-grid span,.notification-node-grid span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.notification-node-grid{margin-top:9px}.notification-delivery-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.notification-delivery-grid label{display:grid;gap:5px;color:var(--color-text-2,#4e5969);font-size:11px}.notification-preferences>.base-btn{justify-self:start}.notification-history{padding-top:4px;border-top:1px solid var(--color-border-2,#e5e6eb)}.notification-history__list{display:grid}.notification-history__list article{display:grid;padding:11px 3px;gap:5px;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.notification-history__list article>div{display:flex;align-items:center;justify-content:space-between;gap:10px}.notification-history__list strong{font-size:12px}.notification-history__list span,.order-card em{padding:2px 6px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);color:var(--color-text-3,#86909c);font-size:9px;font-style:normal}.notification-history__list span.is-delivered,.order-card em.is-completed{background:rgba(0,180,42,.08);color:#008f24}.notification-history__list span.is-failed,.notification-history__list small.is-error,.order-card em.is-cancelled,.order-card em.is-expired{color:#d03050}.notification-history__list span.is-pending,.notification-history__list span.is-digest,.order-card em.is-pending,.order-card em.is-accepted{background:rgba(22,93,255,.08);color:#165dff}.notification-history__list p{margin:0;color:var(--color-text-2,#4e5969);font-size:11px;line-height:1.5}.notification-history__list small{color:var(--color-text-3,#86909c);font-size:9px}.order-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.order-card{display:grid;padding:14px;gap:11px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.order-card header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.order-card header>div{display:grid;min-width:0;gap:3px}.order-card header span{color:var(--color-text-3,#86909c);font-size:9px;font-weight:700}.order-card header strong{overflow:hidden;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.order-card dl{display:grid;margin:0;gap:6px}.order-card dl>div{display:grid;grid-template-columns:76px minmax(0,1fr);gap:8px}.order-card dt{color:var(--color-text-3,#86909c);font-size:10px}.order-card dd{min-width:0;margin:0;overflow:hidden;color:var(--color-text-2,#4e5969);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.order-card>p{margin:0;padding:9px;border-radius:6px;background:var(--color-fill-1,#f7f8fa);color:var(--color-text-2,#4e5969);font-size:11px;line-height:1.5}.order-card footer{display:flex;flex-wrap:wrap;gap:6px}
body[arco-theme='dark'] .account-node{background:#232324;border-color:rgba(255,255,255,.09)}body[arco-theme='dark'] .account-auth__modes button.active{background:#2a2a2b}body[arco-theme='dark'] .account-tool,body[arco-theme='dark'] .account-install{background:#202021}
body[arco-theme='dark'] .notification-bind,body[arco-theme='dark'] .order-card{background:#232324;border-color:rgba(255,255,255,.09)}
body[arco-theme='dark'] .account-first-node{background:#232324;border-color:rgba(255,255,255,.09)}
@media(max-width:720px){.account-center{width:calc(100% - 24px);padding-top:16px}.account-head{align-items:flex-start}.account-head h1{font-size:21px}.account-head>div:last-child .base-btn:first-child{display:none}.account-tabs{margin:16px 0 20px}.account-tabs button{height:44px;padding:0 13px}.account-tabs button.active::after{right:13px;left:13px}.account-node-list{grid-template-columns:1fr}.account-form-grid{grid-template-columns:1fr}.account-check{grid-column:auto}.account-node__metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.account-panel__head{align-items:center}.account-market-list article,.account-simple-list article{align-items:flex-start;flex-direction:column}.account-security-summary{grid-template-columns:1fr}.account-security-summary small{grid-column:auto}.account-auth__form{padding:14px}}
@media(max-width:720px){.account-security-facts{grid-template-columns:1fr}.account-security-facts>div{border-right:0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.account-security-facts>div:last-child{border-bottom:0}.notification-bind{grid-template-columns:1fr;padding:15px}.notification-bind__copy{text-align:center}.notification-bind__copy ol{text-align:left}.notification-status{align-items:flex-start;flex-direction:column}.notification-status>div:last-child{justify-content:flex-start}.notification-event-grid,.notification-node-grid,.notification-delivery-grid,.order-list{grid-template-columns:1fr}.notification-section-head{align-items:flex-start}.notification-bind__qr img,.notification-qr-placeholder{width:156px;height:156px}.account-first-node{padding:16px}.account-first-node>div{display:grid;gap:5px}.onboarding-progress{overflow-x:auto}.onboarding-progress>span{flex:none}.onboarding-progress i{width:12px;flex:none}}
</style>
