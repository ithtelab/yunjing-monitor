<script setup>
import {computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, provide, ref} from "vue";
import moment from 'moment'
import axios from "axios";
import Message from "@arco-design/web-vue/es/message";
import StatsCard from "@/components/StatsCard.vue";
import AnimeNavBar from "@/components/ui/AnimeNavBar.vue";
import RegionFlag from "@/components/ui/RegionFlag.vue";
import {formatAgo, formatBytes, formatDateStamp, formatTimeStamp, formatUptime, formatUptimeZh, calculateRemainingDays} from '@/utils/utils'
import {compactPlatformName, getHostChartSeries, hostArea, hostDisplayName, normalizeAPIURL, normalizeMonitorHosts} from '@/utils/monitor'
import HeaderLocale from "@/components/HeaderLocale.vue";
import LandingPage from "@/components/LandingPage.vue";
import SiteFooter from "@/components/site/SiteFooter.vue";
import SiteUpdateNotice from "@/components/site/SiteUpdateNotice.vue";
import EmptyState from "@/components/ui/EmptyState.vue";
import BaseButton from "@/components/ui/BaseButton.vue";
import BaseInput from "@/components/ui/BaseInput.vue";
import FirstNodeOnboarding from "@/components/site/FirstNodeOnboarding.vue";
import RegionDistributionView from "@/components/site/RegionDistributionView.vue";
import {useI18n} from "vue-i18n";
import { UserCircle } from '@iconoir/vue'

const { t } = useI18n()

const CPU = defineAsyncComponent(() => import("@/components/CPU.vue"))
const Mem = defineAsyncComponent(() => import("@/components/Mem.vue"))
const NetIn = defineAsyncComponent(() => import("@/components/NetIn.vue"))
const NetOut = defineAsyncComponent(() => import("@/components/NetOut.vue"))
const SiteChangelog = defineAsyncComponent(() => import("@/components/site/SiteChangelog.vue"))
const SiteAnnouncement = defineAsyncComponent(() => import("@/components/site/SiteAnnouncement.vue"))
const SiteAlerts = defineAsyncComponent(() => import("@/components/site/SiteAlerts.vue"))
const PublicStatusPage = defineAsyncComponent(() => import("@/components/site/PublicStatusPage.vue"))
const MarketPage = defineAsyncComponent(() => import("@/components/market/MarketPage.vue"))
const SubmitListing = defineAsyncComponent(() => import("@/components/market/SubmitListing.vue"))
const AccountCenter = defineAsyncComponent(() => import("@/components/account/AccountCenter.vue"))

const socketURL = ref('')
const apiURL = ref('')
const siteName = ref('云镜监控')
const compactSiteName = computed(() => {
  const value = String(siteName.value || 'Monitor').replace(/\s+Party$/i, '').trim() || 'Monitor'
  const chars = Array.from(value)
  return chars.length > 12 ? `${chars.slice(0, 12).join('')}…` : value
})
const offlineWait = ref(60)
const landingEnabled = ref(false)
const marketEnabled = ref(true)
const registrationEnabled = ref(true)
const selfServiceNodeEnabled = ref(true)
const userNodeLimit = ref(0)
const appReady = ref(false)
const pathName = ref(window.location.pathname || '/')
const monitorRoute = computed(() => /^\/monitor(?:\/|$)/.test(pathName.value))
const statusSlug = computed(() => {
  const match = pathName.value.match(/^\/status\/([^/]+)\/?$/)
  if (!match) return ''
  try { return decodeURIComponent(match[1]) } catch { return match[1] }
})
const statusRoute = computed(() => Boolean(statusSlug.value))
const accountRoute = computed(() => /^\/account(?:\/|$)/.test(pathName.value) || /^\/market\/owner(?:\/|$)/.test(pathName.value))
const marketRoute = computed(() => {
  const p = pathName.value
  if (p === '/market' || p === '/market/') return 'market'
  if (p === '/market/submit' || p.startsWith('/market/submit/')) return 'submit'
  if (p === '/market/owner' || p.startsWith('/market/owner')) return 'owner'
  return ''
})
const showMarket = computed(() => Boolean(marketRoute.value && marketRoute.value !== 'owner'))
const showLanding = computed(() => landingEnabled.value && !monitorRoute.value && !showMarket.value && !statusRoute.value && !accountRoute.value)

const theme = window.localStorage.getItem('theme') || 'light'
const dark = ref(theme !== 'light')
const readMonitorPreferences = () => {
  try { return JSON.parse(window.localStorage.getItem('monitor-list-preferences') || '{}') || {} } catch { return {} }
}
const monitorPreferences = readMonitorPreferences()
const savedViewMode = monitorPreferences.view_mode || window.localStorage.getItem('monitor-view-mode')
const viewMode = ref(['list', 'grid', 'distribution'].includes(savedViewMode) ? savedViewMode : 'list')
const sortMode = ref(['name', 'status', 'uptime', 'cpu', 'memory', 'disk', 'network', 'traffic'].includes(monitorPreferences.sort_mode) ? monitorPreferences.sort_mode : 'status')
const groupBy = ref(['none', 'region', 'status', 'platform'].includes(monitorPreferences.group_by) ? monitorPreferences.group_by : 'none')
const groupValue = ref(String(monitorPreferences.group_value || 'all'))

const persistMonitorPreferences = () => window.localStorage.setItem('monitor-list-preferences', JSON.stringify({
  view_mode: viewMode.value, sort_mode: sortMode.value, group_by: groupBy.value, group_value: groupValue.value
}))

const handleViewMode = (mode) => {
  viewMode.value = ['grid', 'distribution'].includes(mode) ? mode : 'list'
  window.localStorage.setItem('monitor-view-mode', viewMode.value)
  persistMonitorPreferences()
  // 切换列表/卡片模式时，关闭当前已展开的节点详情，避免详情沿用旧模式的布局呈现。
  selectHost.value = ''
  setNodeModalOpen(false)
}

const handleSortMode = (value) => {
  sortMode.value = value
  persistMonitorPreferences()
}
const handleGroupBy = (value) => {
  groupBy.value = value
  groupValue.value = 'all'
  persistMonitorPreferences()
}
const handleGroupValue = (value) => {
  groupValue.value = value
  persistMonitorPreferences()
}

const handleChangeDark = () => {
  dark.value = !dark.value

  if (dark.value) {
    window.localStorage.setItem('theme','dark')
    document.body.setAttribute('arco-theme', 'dark')
  } else {
    // 恢复亮色主题
    window.localStorage.setItem('theme','light')
    document.body.removeAttribute('arco-theme');
   }
}

const area = ref([])
const selectArea = ref('all')

const TAB_KEYS = ['all', 'online', 'offline', 'changelog', 'announcement', 'stats']
// 当前标签同步到 URL hash（如 /monitor#changelog），刷新后恢复原页面
const readHashTab = () => {
  const h = window.location.hash.replace(/^#/, '')
  return TAB_KEYS.includes(h) ? h : ''
}
const type = ref(readHashTab() || 'all')
const navActive = computed(() => {
  if (accountRoute.value) return ''
  if (marketRoute.value) return 'market'
  if (type.value === 'online' || type.value === 'offline') return 'all'
  return type.value
})
// 节点列表(概览 / 在线 / 离线）共用这一组主区；更新日志 / 站点公告 / 实时统计各自单独面板。
const showNodeList = computed(() => !showMarket.value && !statusRoute.value && !accountRoute.value && (type.value === 'all' || type.value === 'online' || type.value === 'offline'))
const navItems = computed(() => [
  { key: 'all', label: t('nav-overview'), icon: 'home' },
  { key: 'market', label: t('nav-market'), icon: 'market' },
  { key: 'changelog', label: t('nav-changelog'), icon: 'changelog' },
  { key: 'announcement', label: t('nav-announcement'), icon: 'announcement' },
  { key: 'stats', label: t('nav-stats'), icon: 'stats' },
  { key: 'admin', label: t('nav-admin'), icon: 'settings' }
].filter((item) => marketEnabled.value || item.key !== 'market'))

const setMarketPath = (route) => {
  const map = {
    market: '/market',
    submit: '/market/submit',
    owner: '/account'
  }
  const next = map[route] || '/market'
  if (window.location.pathname !== next) {
    window.history.pushState({}, '', next)
  }
  pathName.value = next
}

const setAccountPath = () => {
  if (window.location.pathname !== '/account') window.history.pushState({}, '', '/account')
  pathName.value = '/account'
}

const handleMarketNavigate = (route) => {
  if (route === 'owner') {
    setAccountPath()
    return
  }
  if (!marketEnabled.value) return
  setMarketPath(route)
}

const handleAccountNavigate = (route) => {
  if (route === 'market' && marketEnabled.value) {
    setMarketPath('market')
    return
  }
  const next = landingEnabled.value ? '/monitor' : '/'
  window.history.pushState({}, '', next)
  pathName.value = next
  type.value = 'all'
}

const handleMarketDisabledBack = () => {
  const next = landingEnabled.value ? '/monitor' : '/'
  window.history.pushState({}, '', next)
  pathName.value = next
  type.value = 'all'
}

const handlePopState = () => {
  pathName.value = window.location.pathname || '/'
  if (!marketRoute.value) {
    type.value = readHashTab() || 'all'
  }
}

const data = ref([])
const monitorDataReady = ref(false)
const monitorEmptyText = computed(() => data.value.length ? t('monitor-filter-empty') : t('monitor-empty'))

const selectHost = ref('')
const marketDetailHost = ref('')
const detailMobileTab = ref('overview')
const trendRanges = ['live', '1h', '6h', '24h', '7d']
const trendRange = ref('1h')
const trendLoading = ref(false)
const trendError = ref(false)
const trendEmpty = ref(false)
const trendHistory = ref({
  nodeID: '',
  range: '',
  series: { cpu: [], mem: [], net_in: [], net_out: [] },
  memoryTotal: 0
})
let trendController = null
let trendRequestID = 0

const cancelTrendRequest = () => {
  trendRequestID += 1
  trendController?.abort()
  trendController = null
}

const clearTrendHistory = () => {
  trendHistory.value = {
    nodeID: '',
    range: '',
    series: { cpu: [], mem: [], net_in: [], net_out: [] },
    memoryTotal: 0
  }
}

const resetTrendState = (resetRange = false) => {
  cancelTrendRequest()
  trendLoading.value = false
  trendError.value = false
  trendEmpty.value = false
  clearTrendHistory()
  if (resetRange) trendRange.value = '1h'
}

const normalizeHistoryTimestamp = (value) => {
  const timestamp = Number(value)
  if (!Number.isFinite(timestamp) || timestamp <= 0) return 0
  return timestamp < 1000000000000 ? timestamp * 1000 : timestamp
}

const loadTrendHistory = async (nodeID, range = trendRange.value) => {
  const normalizedNodeID = String(nodeID || '').trim()
  if (!normalizedNodeID || range === 'live') return

  cancelTrendRequest()
  const requestID = trendRequestID
  trendController = new AbortController()
  trendLoading.value = true
  trendError.value = false
  trendEmpty.value = false
  clearTrendHistory()

  try {
    const response = await axios.get(`${apiURL.value}/api/nodes/history`, {
      params: { node_id: normalizedNodeID, range },
      signal: trendController.signal
    })
    if (requestID !== trendRequestID) return

    const samples = (Array.isArray(response.data?.samples) ? response.data.samples : [])
      .map((sample) => ({ ...sample, timestamp: normalizeHistoryTimestamp(sample?.timestamp) }))
      .filter((sample) => sample.timestamp > 0)
      .sort((a, b) => a.timestamp - b.timestamp)

    if (!samples.length) {
      trendHistory.value = {
        nodeID: normalizedNodeID,
        range,
        series: { cpu: [], mem: [], net_in: [], net_out: [] },
        memoryTotal: 0
      }
      trendEmpty.value = true
      return
    }

    trendHistory.value = {
      nodeID: normalizedNodeID,
      range,
      series: {
        cpu: samples.map((sample) => [sample.timestamp, Number(sample.cpu_percent) || 0]),
        mem: samples.map((sample) => [sample.timestamp, Number(sample.memory_used) || 0]),
        net_in: samples.map((sample) => [sample.timestamp, Number(sample.net_in_speed) || 0]),
        net_out: samples.map((sample) => [sample.timestamp, Number(sample.net_out_speed) || 0])
      },
      memoryTotal: samples.reduce((max, sample) => Math.max(max, Number(sample.memory_total) || 0), 0)
    }
  } catch (error) {
    if (requestID !== trendRequestID || error?.code === 'ERR_CANCELED' || error?.name === 'CanceledError') return
    trendError.value = true
  } finally {
    if (requestID === trendRequestID) {
      trendLoading.value = false
      trendController = null
    }
  }
}

const handleTrendRange = (range, nodeID) => {
  if (!trendRanges.includes(range)) return
  trendRange.value = range
  if (range === 'live') {
    resetTrendState(false)
    return
  }
  loadTrendHistory(nodeID, range)
}

const trendUsesHistory = (nodeID) => (
  trendRange.value !== 'live' &&
  !trendLoading.value &&
  !trendError.value &&
  !trendEmpty.value &&
  trendHistory.value.nodeID === nodeID &&
  trendHistory.value.range === trendRange.value &&
  trendHistory.value.series.cpu.length > 0
)

const trendSeriesFor = (nodeID, key) => (
  trendUsesHistory(nodeID) ? trendHistory.value.series[key] : chartSeries(nodeID)[key]
)

const trendMemoryMax = (item) => (
  trendUsesHistory(item.Host.Name) ? (trendHistory.value.memoryTotal || item.Host.MemTotal) : item.Host.MemTotal
)

const trendSubtitle = (nodeID) => (
  trendUsesHistory(nodeID)
    ? t('monitor-trend-history', { range: trendRange.value })
    : t('monitor-last-60')
)

const handleDetailMobileTab = async (tab) => {
  detailMobileTab.value = tab
  if (tab === 'trends') {
    if (trendRange.value !== 'live' && trendHistory.value.nodeID !== selectHost.value && !trendLoading.value) {
      await loadTrendHistory(selectHost.value, trendRange.value)
    }
    await nextTick()
    window.dispatchEvent(new Event('resize'))
  }
}

const setNodeModalOpen = (open) => {
  document.body.classList.toggle('node-modal-open', open)
  document.documentElement.classList.toggle('node-modal-open', open)
}

const closeHostDetail = () => {
  resetTrendState(true)
  selectHost.value = ''
  marketDetailHost.value = ''
  detailMobileTab.value = 'overview'
  setNodeModalOpen(false)
}

const handleDetailKeydown = (event) => {
  if (event.key === 'Escape' && selectHost.value && (marketDetailHost.value || viewMode.value === 'grid' || isMobileDetail())) {
    closeHostDetail()
  }
}

const charts = ref({})

const cpuRef = ref(null)
const memRef = ref(null)
const netInRef = ref(null)
const netOutRef = ref(null)

const host = computed(() => {
  if (selectArea.value === 'all') {
    return data.value
  }

  return data.value.filter(item => hostArea(item) === selectArea.value)
})

const statusFilteredHosts = computed(() => {
  if (type.value === 'all' || type.value === 'changelog' || type.value === 'announcement' || type.value === 'stats') {
    return host.value
  } else if (type.value === 'online') {
    return host.value.filter(item => item.status)
  } else {
    return host.value.filter(item => !item.status)
  }
})

const groupOptions = computed(() => {
  if (groupBy.value === 'region') return [...new Set(statusFilteredHosts.value.map((item) => hostArea(item) || t('market-no-region')))].sort()
  if (groupBy.value === 'status') return ['online', 'offline']
  if (groupBy.value === 'platform') return [...new Set(statusFilteredHosts.value.map((item) => compactPlatformName(item.Host.Platform) || t('common-no-data')))].sort()
  return []
})

const groupKeyOf = (item) => {
  if (groupBy.value === 'region') return hostArea(item) || t('market-no-region')
  if (groupBy.value === 'status') return item.status ? 'online' : 'offline'
  if (groupBy.value === 'platform') return compactPlatformName(item.Host.Platform) || t('common-no-data')
  return 'all'
}

const hosts = computed(() => {
  if (!showNodeList.value) return statusFilteredHosts.value
  const selectedGroup = groupValue.value === 'all' || groupOptions.value.includes(groupValue.value)
    ? groupValue.value
    : 'all'
  let result = selectedGroup === 'all' || groupBy.value === 'none'
    ? [...statusFilteredHosts.value]
    : statusFilteredHosts.value.filter((item) => groupKeyOf(item) === selectedGroup)
  const number = (value) => Number(value || 0)
  const diskUsage = (item) => number(item.State.DiskTotal) > 0 ? number(item.State.DiskUsed) / number(item.State.DiskTotal) * 100 : 0
  const memoryUsage = (item) => number(item.Host.MemTotal) > 0 ? number(item.State.MemUsed) / number(item.Host.MemTotal) * 100 : 0
  const network = (item) => number(item.State.NetInSpeed) + number(item.State.NetOutSpeed)
  const traffic = (item) => number(item.State.NetInTransfer) + number(item.State.NetOutTransfer)
  switch (sortMode.value) {
    case 'name': result.sort((a, b) => hostDisplayName(a).localeCompare(hostDisplayName(b))); break
    case 'uptime': result.sort((a, b) => number(b.State.Uptime) - number(a.State.Uptime)); break
    case 'cpu': result.sort((a, b) => number(b.State.CPU) - number(a.State.CPU)); break
    case 'memory': result.sort((a, b) => memoryUsage(b) - memoryUsage(a)); break
    case 'disk': result.sort((a, b) => diskUsage(b) - diskUsage(a)); break
    case 'network': result.sort((a, b) => network(b) - network(a)); break
    case 'traffic': result.sort((a, b) => traffic(b) - traffic(a)); break
    default: result.sort((a, b) => Number(b.status) - Number(a.status) || hostDisplayName(a).localeCompare(hostDisplayName(b)))
  }
  return result
})

const handleStatsMetric = (metric) => {
  if (metric !== 'traffic') return
  handleChangeType('all')
  handleSortMode('traffic')
}

const stats = computed(() => {
  const online = host.value.filter(item => item.status)
  let bandwidth_up = 0
  let bandwidth_down = 0
  let traffic_up = 0
  let traffic_down = 0

  host.value.forEach((item) => {
    bandwidth_up += item.State.NetOutSpeed
    bandwidth_down += item.State.NetInSpeed
    traffic_up += item.State.NetOutTransfer
    traffic_down += item.State.NetInTransfer
  })

  return {
    total: host.value.length,
    online: online.length,
    offline: host.value.length - online.length,
    bandwidth_up: bandwidth_up,
    bandwidth_down: bandwidth_down,
    traffic_up: traffic_up,
    traffic_down: traffic_down
  }
})

let socket = null
let reconnectTimer = null
let pingTimer = null
let reconnectAttempts = 0
let mounted = false
let configLoaded = false

let nowtime = (Math.floor(Date.now() / 1000))

const deriveSocketURL = () => {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/ws`
}

const fetchConfig = async () => {
  if (configLoaded) {
    return true
  }
  try {
    const res = await axios.get('/config.json')
    socketURL.value = res.data.socket || deriveSocketURL()
    apiURL.value = normalizeAPIURL(res.data.apiURL)
    siteName.value = res.data.siteName || '云镜监控'
    offlineWait.value = Number(res.data.offlineWait) || 60
    landingEnabled.value = res.data.landingEnabled === true || res.data.landingEnabled === 'true'
    marketEnabled.value = res.data.marketEnabled !== false && res.data.marketEnabled !== 'false'
    registrationEnabled.value = res.data.registrationEnabled !== false && res.data.registrationEnabled !== 'false'
    selfServiceNodeEnabled.value = res.data.selfServiceNodeEnabled !== false && res.data.selfServiceNodeEnabled !== 'false'
    userNodeLimit.value = Math.max(0, Number(res.data.userNodeLimit) || 0)
    document.title = siteName.value
    configLoaded = true
    return true
  } catch (e) {
    Message.error(t('get-config-error'))
    return false
  }
}

const initSocket = async () => {
  if (!mounted || socket?.readyState === WebSocket.OPEN || socket?.readyState === WebSocket.CONNECTING) {
    return
  }

  if (!await fetchConfig()) {
    scheduleReconnect()
    return
  }

  socket = new WebSocket(socketURL.value)

  socket.onmessage = function(event) {
    try {
      const message = typeof event.data === 'string' ? event.data : ''
      // Legacy SSE-style prefix only when the whole frame starts with "data: ".
      const payload = message.startsWith('data: ') ? message.slice(6) : message
      const parsed = JSON.parse(payload) || []
      // Use wall-clock now for online status so we don't lag behind the last ping tick.
      nowtime = Math.floor(Date.now() / 1000)
      const normalized = normalizeMonitorHosts(parsed, nowtime, offlineWait.value, charts.value)
      area.value = normalized.areas
      data.value = normalized.hosts
      monitorDataReady.value = true

      schedulePing()

    } catch (error) {
      console.error(t('ws-error'), error);
    }
  };

  socket.onopen = function () {
    reconnectAttempts = 0
    sendPing()
  }

  socket.onclose = function () {
    socket = null
    scheduleReconnect()
  }

  socket.onerror = function () {
    socket?.close()
  }
}

const scheduleReconnect = () => {
  if (!mounted || reconnectTimer) {
    return
  }
  Message.warning(t('ws-error-reconnect'))
  const delay = Math.min(30000, 1000 * Math.pow(2, reconnectAttempts))
  reconnectAttempts += 1
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = null
    initSocket()
  }, delay)
}

const schedulePing = () => {
  window.clearTimeout(pingTimer)
  pingTimer = window.setTimeout(() => sendPing(), 1000)
}

const sendPing = () => {
  nowtime = (Math.floor(Date.now() / 1000))
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send('ping')
  }
}

// 光斑跟随（glow-card）已收口到 components/ui/glow-card.{css,js}，由 main.js 全局装载

onMounted(async() => {
  mounted = true
  window.addEventListener('keydown', handleDetailKeydown)
  window.addEventListener('popstate', handlePopState)
  if (dark.value) {
    document.body.setAttribute('arco-theme', 'dark')
  }

  const loaded = await fetchConfig()
  appReady.value = true
  if (!loaded) {
    scheduleReconnect()
    return
  }
  if (showLanding.value || statusRoute.value) return
  // Market pages don't need live WS; still fine to connect for nav back to monitor.
  await initSocket()
  handleFetchHostInfo()
})

onUnmounted(() => {
  mounted = false
  cancelTrendRequest()
  window.clearTimeout(reconnectTimer)
  window.clearTimeout(pingTimer)
  socket?.close()
  window.removeEventListener('keydown', handleDetailKeydown)
  window.removeEventListener('popstate', handlePopState)
  setNodeModalOpen(false)
})

const progressStatus = (value) => {
  if (value < 80) {
    return 'success';
  } else if (value < 90) {
    return 'warning';
  } else {
    return 'danger';
  }
}

const handleSelectArea = (area) => {
  selectArea.value = area
}

const handleSelectHost = (item) => {
  if (!hasReport(item)) return

  const host = item.Host.Name
  handleFetchHostInfo()
  if (selectHost.value === host) {
    closeHostDetail()
    return
  }

  selectHost.value = host
  detailMobileTab.value = 'overview'
  setNodeModalOpen(viewMode.value === 'grid' || isMobileDetail())
  loadTrendHistory(host, trendRange.value)
}

const hostInfo = ref({})

const handleFetchHostInfo = async () => {
  try {
    const res = await axios.get(`${apiURL.value}/info`)
    const nextHostInfo = {}
    ;(Array.isArray(res.data) ? res.data : []).forEach((item) => {
      if (item?.name) {
        nextHostInfo[item.name] = item
      }
    })
    hostInfo.value = nextHostInfo
  } catch (e) {
    // Message.error('删除失败，管理密钥错误')
  }
}

const handleChangeType = (value) => {
  type.value = value
  // 写入 URL hash，刷新 / 前进后退 / 分享链接都能回到同一标签
  if (!marketRoute.value) {
    const url = value === 'all' ? window.location.pathname : `${window.location.pathname}#${value}`
    window.history.pushState({}, '', url)
    pathName.value = window.location.pathname || '/'
  }
}

const handleNavSelect = (item) => {
  if (item.key === 'admin') {
    window.location.assign('/admin')
    return
  }
  if (item.key === 'market') {
    setMarketPath('market')
    return
  }
  // Leave market path when returning to monitor tabs.
  if (showMarket.value || accountRoute.value) {
    const next = landingEnabled.value ? '/monitor' : '/'
    if (window.location.pathname !== next) {
      // 用 replace 先切路径，由 handleChangeType 统一 push 带 hash 的地址，历史记录只多一条
      window.history.replaceState({}, '', next)
    }
    pathName.value = next
  }
  selectArea.value = 'all'
  closeHostDetail()
  handleChangeType(item.key)
}

const getHostInfo = (name) => {
  const key = String(name || '')
  return hostInfo.value[key] || hostInfo.value[key.trim()] || {}
}

const hasReport = (item) => Number(item?.TimeStamp) > 0

const isMobileDetail = () => window.matchMedia('(max-width: 768px)').matches
const isDetailModal = (item) => marketDetailHost.value === item?.Host?.Name || viewMode.value === 'grid' || isMobileDetail()
const marketDetailItem = computed(() => {
  if (!marketDetailHost.value) return null
  return data.value.find((item) => item?.Host?.Name === marketDetailHost.value && hasReport(item)) || null
})

const handleMarketInspect = (listing) => {
  const nodeID = String(listing?.node_id || '')
  const item = data.value.find((host) => host?.Host?.Name === nodeID)
  if (!item || !hasReport(item)) {
    Message.warning(t('market-monitor-unavailable'))
    return
  }
  marketDetailHost.value = nodeID
  selectHost.value = nodeID
  detailMobileTab.value = 'overview'
  handleFetchHostInfo()
  setNodeModalOpen(true)
  loadTrendHistory(nodeID, trendRange.value)
}

// 告警页「定位」：复用市场详情弹层，不切换当前标签；关闭后仍停留在告警页。
const handleLocateHost = (name) => {
  const item = data.value.find((h) => h?.Host?.Name === name)
  if (!item || !hasReport(item)) return
  marketDetailHost.value = name
  selectHost.value = name
  detailMobileTab.value = 'overview'
  handleFetchHostInfo()
  setNodeModalOpen(true)
  loadTrendHistory(name, trendRange.value)
}

const nodeStatusText = (item) => {
  if (!hasReport(item)) return t('monitor-pending')
  return item.status ? t('online') : t('offline')
}

const nodeStatusMeta = (item) => {
  if (!hasReport(item)) return t('monitor-never-report')
  if (item.status) return formatUptime(item.State.Uptime)
  return t('monitor-last-report', { time: formatAgo(item.TimeStamp) })
}

const nodePlatformText = (item) => hasReport(item) ? compactPlatformName(item.Host.Platform) : t('monitor-no-sysdata')

const nodePercentText = (item, value, digits = 2) => hasReport(item) ? `${Number(value || 0).toFixed(digits)}%` : '-'

const nodeCapacityText = (item, value) => hasReport(item) ? formatBytes(value) : '-'

const nodeCoresText = (item) => {
  if (!hasReport(item)) return '-'
  return t('monitor-cores', { n: item.Host.LogicalCores || item.Host.CPU?.length || 0 })
}

const chartSeries = (name) => getHostChartSeries(charts.value, name)

const diskPercent = (item) => {
  if (!item.State.DiskTotal) return 0
  return item.State.DiskUsed / item.State.DiskTotal * 100
}

const memoryPercent = (item) => {
  if (!item.Host.MemTotal) return 0
  return item.State.MemUsed / item.Host.MemTotal * 100
}

const swapPercent = (item) => {
  if (!item.Host.SwapTotal) return 0
  return item.State.SwapUsed / item.Host.SwapTotal * 100
}

const clampPercent = (value) => Math.min(100, Math.max(0, Number(value) || 0))

const resourceLevel = (value) => {
  const percent = clampPercent(value)
  if (percent >= 90) return 'danger'
  if (percent >= 80) return 'warning'
  return 'normal'
}

const resourceStatus = (value) => {
  const level = resourceLevel(value)
  if (level === 'danger') return t('monitor-level-danger')
  if (level === 'warning') return t('monitor-level-warning')
  return t('monitor-level-ok')
}

const resourceBarStyle = (value) => ({ width: `${clampPercent(value)}%` })

const cpuCoresText = (item) => {
  const physical = item.Host.PhysicalCores || item.Host.LogicalCores || item.Host.CPU?.length || 0
  const logical = item.Host.LogicalCores || item.Host.CPU?.length || physical
  return t('monitor-cores-pl', { physical, logical })
}

const normalizeDueTime = (value) => {
  if (!value) return 0
  return Number(value) > 0 && Number(value) < 1000000000000 ? Number(value) * 1000 : value
}

provide('handleChangeType', handleChangeType)

</script>

<template>
  <PublicStatusPage v-if="appReady && statusRoute" :slug="statusSlug" :api-base="apiURL" :site-name="siteName" :dark="dark" @toggle-theme="handleChangeDark" />
  <LandingPage v-else-if="appReady && showLanding" :site-name="siteName" :offline-wait="offlineWait" :dark="dark" @toggle-theme="handleChangeDark" />
  <div v-else-if="appReady" id="monitor-top" class="max-container">
    <div class="header">
      <button class="logo" type="button" :title="siteName" @click="handleNavSelect({ key: 'all' })">
        <span class="brand-mark"><img src="/logo.svg" alt="" /></span>
        <span class="brand-name">{{compactSiteName}}</span>
      </button>
      <AnimeNavBar class="header-nav" :items="navItems" :active-key="navActive" @select="handleNavSelect" />
      <a-space class="header-actions">
        <a-button class="account-btn" :shape="'round'" :title="$t('nav-account')" :aria-label="$t('nav-account')" @click="setAccountPath"><UserCircle aria-hidden="true" /></a-button>
        <HeaderLocale :dark="dark" />
        <a-button class="theme-btn" :shape="'round'" @click="handleChangeDark">
          <template #icon>
            <icon-sun-fill v-if="!dark" />
            <icon-moon-fill v-else />
          </template>
        </a-button>
      </a-space>
    </div>
    <div class="monitor-toolbar" v-if="showNodeList">
      <div class="area-tabs">
        <button type="button" class="area-tab-item" :class="selectArea === 'all' ? 'is-active' : ''" @click="handleSelectArea('all')">
          {{$t('all-area')}}
        </button>
        <button type="button" class="area-tab-item" :class="selectArea === item ? 'is-active' : ''" v-for="item in area" :key="item" @click="handleSelectArea(item)">
          <RegionFlag :region="item" class="area-region-flag" /> {{item}}
        </button>
      </div>
      <div class="monitor-controls">
        <BaseInput class="monitor-select" as="select" :model-value="sortMode" :aria-label="$t('monitor-sort-label')" @update:model-value="handleSortMode"><option value="status">{{ $t('monitor-sort-status') }}</option><option value="name">{{ $t('monitor-sort-name') }}</option><option value="uptime">{{ $t('monitor-sort-uptime') }}</option><option value="cpu">CPU</option><option value="memory">{{ $t('chart-memory') }}</option><option value="disk">{{ $t('chart-disk') }}</option><option value="network">{{ $t('monitor-sort-network') }}</option><option value="traffic">{{ $t('monitor-sort-traffic') }}</option></BaseInput>
        <BaseInput class="monitor-select" as="select" :model-value="groupBy" :aria-label="$t('monitor-group-label')" @update:model-value="handleGroupBy"><option value="none">{{ $t('monitor-group-none') }}</option><option value="region">{{ $t('area') }}</option><option value="status">{{ $t('monitor-group-status') }}</option><option value="platform">{{ $t('system') }}</option></BaseInput>
        <BaseInput v-if="groupBy !== 'none'" class="monitor-select" as="select" :model-value="groupValue" :aria-label="$t('monitor-group-value')" @update:model-value="handleGroupValue"><option value="all">{{ $t('monitor-group-all') }}</option><option v-for="option in groupOptions" :key="option" :value="option">{{ option === 'online' ? $t('online') : option === 'offline' ? $t('offline') : option }}</option></BaseInput>
        <div class="view-switch" role="group" :aria-label="$t('monitor-view-aria')">
          <button type="button" :class="{ 'is-active': viewMode === 'list' }" :aria-pressed="viewMode === 'list'" @click="handleViewMode('list')"><span aria-hidden="true">☷</span><span class="view-label">{{ $t('monitor-view-list') }}</span></button>
          <button type="button" :class="{ 'is-active': viewMode === 'grid' }" :aria-pressed="viewMode === 'grid'" @click="handleViewMode('grid')"><span aria-hidden="true">▦</span><span class="view-label">{{ $t('monitor-view-grid') }}</span></button>
          <button type="button" :class="{ 'is-active': viewMode === 'distribution' }" :aria-pressed="viewMode === 'distribution'" @click="handleViewMode('distribution')"><span aria-hidden="true">◫</span><span class="view-label">{{ $t('monitor-view-distribution') }}</span></button>
        </div>
      </div>
    </div>
    <AccountCenter v-if="accountRoute" :api-base="apiURL" :market-enabled="marketEnabled" :registration-enabled="registrationEnabled" :self-service-node-enabled="selfServiceNodeEnabled" :user-node-limit="userNodeLimit" @navigate="handleAccountNavigate" />
    <section v-else-if="showMarket && !marketEnabled" class="market-disabled"><div class="market-disabled__icon" aria-hidden="true">—</div><h1>{{ $t('market-disabled-title') }}</h1><p>{{ $t('market-disabled-text') }}</p><button type="button" @click="handleMarketDisabledBack">{{ $t('market-disabled-back') }}</button></section>
    <MarketPage v-else-if="marketRoute === 'market'" :api-base="apiURL" :dark="dark" @navigate="handleMarketNavigate" @inspect="handleMarketInspect" />
    <SubmitListing v-else-if="marketRoute === 'submit'" :api-base="apiURL" @navigate="handleMarketNavigate" />
    <StatsCard v-else-if="showNodeList" :type="type" :stats="stats" :dark="dark" @select-metric="handleStatsMetric" />
    <SiteChangelog v-else-if="navActive === 'changelog'" :apiURL="apiURL" />
    <SiteAnnouncement v-else-if="navActive === 'announcement'" :apiURL="apiURL" />
    <SiteAlerts v-else-if="navActive === 'stats'" :hosts="hosts" :host-info="hostInfo" :api-base="apiURL" @locate="handleLocateHost" />
    <FirstNodeOnboarding v-if="showNodeList && monitorDataReady && !data.length" :api-base="apiURL" @start="setAccountPath" />
    <EmptyState v-else-if="showNodeList && !hosts.length" :loading="!monitorDataReady" :text="monitorEmptyText" />
    <RegionDistributionView v-if="showNodeList && hosts.length && viewMode === 'distribution'" :hosts="hosts" @inspect="handleSelectHost" />
    <div class="monitor-card" :class="{ 'is-grid-view': viewMode === 'grid' || marketDetailItem, 'is-market-detail': marketDetailItem }" v-if="(showNodeList && hosts.length && viewMode !== 'distribution') || marketDetailItem">
      <div class="monitor-item" :class="{ 'glow-card': hasReport(item), 'is-active': selectHost === item.Host.Name, 'is-unavailable': !hasReport(item) }" v-for="item in marketDetailItem ? [marketDetailItem] : hosts" @click="marketDetailItem ? undefined : handleSelectHost(item)" :key="item.Host.Name">
        <div class="name">
          <div class="title">
            <RegionFlag :region="hostArea(item)" />
            {{hostDisplayName(item)}}
          </div>
          <div class="status" :class="{ online: item.status, offline: !item.status && hasReport(item), pending: !hasReport(item) }">
            <span>{{nodeStatusText(item)}}</span>
            <span class="status-meta">{{nodeStatusMeta(item)}}</span>
          </div>
        </div>
        <div v-if="viewMode === 'grid'" class="grid-summary">
          <div class="grid-tags">
            <span>{{nodePlatformText(item)}}</span>
            <span v-if="getHostInfo(item.Host.Name).seller">{{getHostInfo(item.Host.Name).seller}}</span>
            <span v-if="getHostInfo(item.Host.Name).price" class="is-price">{{getHostInfo(item.Host.Name).price}}<template v-if="getHostInfo(item.Host.Name).cycle">/{{getHostInfo(item.Host.Name).cycle}}</template></span>
          </div>
          <template v-if="hasReport(item)">
          <div class="capacity-row">
            <span><b>▣</b>{{nodeCoresText(item)}}</span>
            <span><b>▤</b>{{nodeCapacityText(item, item.Host.MemTotal)}}</span>
            <span><b>▰</b>{{nodeCapacityText(item, item.State.DiskTotal)}}</span>
          </div>
          <div class="resource-rows">
            <div class="resource-row">
              <div class="resource-label"><span>CPU</span><strong>{{nodePercentText(item, item.State.CPU, 0)}}</strong></div>
              <div class="resource-track" v-if="hasReport(item)"><span :class="`is-${resourceLevel(item.State.CPU)}`" :style="resourceBarStyle(item.State.CPU)"></span></div>
              <small>{{hasReport(item) ? nodeCoresText(item) : $t('common-no-data')}}</small>
            </div>
            <div class="resource-row">
              <div class="resource-label"><span>{{ $t('chart-memory') }}</span><strong>{{nodePercentText(item, memoryPercent(item), 0)}}</strong></div>
              <div class="resource-track" v-if="hasReport(item)"><span :class="`is-${resourceLevel(memoryPercent(item))}`" :style="resourceBarStyle(memoryPercent(item))"></span></div>
              <small>{{hasReport(item) ? `${formatBytes(item.State.MemUsed)} / ${formatBytes(item.Host.MemTotal)}` : $t('common-no-data')}}</small>
            </div>
            <div class="resource-row">
              <div class="resource-label"><span>{{ $t('chart-disk') }}</span><strong>{{nodePercentText(item, diskPercent(item), 0)}}</strong></div>
              <div class="resource-track" v-if="hasReport(item)"><span :class="`is-${resourceLevel(diskPercent(item))}`" :style="resourceBarStyle(diskPercent(item))"></span></div>
              <small>{{hasReport(item) ? `${formatBytes(item.State.DiskUsed)} / ${formatBytes(item.State.DiskTotal)}` : $t('common-no-data')}}</small>
            </div>
          </div>
          <div class="telemetry-rows">
            <div><span>{{ $t('monitor-net') }}</span><strong>{{hasReport(item) ? `↑ ${formatBytes(item.State.NetOutSpeed)}/s  ↓ ${formatBytes(item.State.NetInSpeed)}/s` : '-'}}</strong></div>
            <div><span>{{ $t('traffic-info') }}</span><strong>{{hasReport(item) ? `↑ ${formatBytes(item.State.NetOutTransfer)}  ↓ ${formatBytes(item.State.NetInTransfer)}` : '-'}}</strong></div>
            <div><span>{{ $t('monitor-load-label') }}</span><strong>{{hasReport(item) ? `${item.State.Load1} | ${item.State.Load5} | ${item.State.Load15}` : '-'}}</strong></div>
          </div>
          </template>
          <div v-else class="grid-pending-summary">
            <span>{{ $t('monitor-waiting') }}</span>
            <small>{{ $t('monitor-waiting-sub') }}</small>
          </div>
          <div class="grid-footer"><span>{{ $t('monitor-due-prefix') }} {{hostInfo[item.Host.Name] ? calculateRemainingDays(hostInfo[item.Host.Name].due_time) : '-'}}</span><span>{{hasReport(item) ? (item.status ? $t('monitor-online-since', { time: formatUptimeZh(item.State.Uptime) }) : $t('monitor-updated-at', { time: formatAgo(item.TimeStamp) })) : $t('monitor-never-report')}}</span></div>
        </div>
        <div v-if="viewMode === 'list'" class="platform" :title="item.Host.Platform">
          <div class="monitor-item-title">{{ $t('system') }}</div>
          <div class="monitor-item-value">{{nodePlatformText(item)}}</div>
        </div>
        <div v-if="viewMode === 'list'" class="cpu">
          <div class="monitor-item-title">CPU</div>
          <div class="monitor-item-value">{{nodePercentText(item, item.State.CPU)}}</div>
          <a-progress v-if="hasReport(item)" class="monitor-item-progress" :status="progressStatus(item.State.CPU)" :percent="item.State.CPU/100" :show-text="false" style="width: 60px" />
        </div>
        <div v-if="viewMode === 'list'" class="mem">
          <div class="monitor-item-title">{{ $t('memory') }}</div>
          <div class="monitor-item-value">{{nodePercentText(item, memoryPercent(item))}}</div>
          <a-progress v-if="hasReport(item)" class="monitor-item-progress" :status="progressStatus(memoryPercent(item))" :percent="memoryPercent(item)/100" :show-text="false" style="width: 60px" />
        </div>
        <div v-if="viewMode === 'list'" class="disk">
          <div class="monitor-item-title">{{ $t('chart-disk') }}</div>
          <div class="monitor-item-value">{{nodePercentText(item, diskPercent(item))}}</div>
          <a-progress v-if="hasReport(item)" class="monitor-item-progress" :status="progressStatus(diskPercent(item))" :percent="diskPercent(item)/100" :show-text="false" style="width: 60px" />
        </div>
        <div v-if="viewMode === 'list'" class="network">
          <div class="monitor-item-title">{{ $t('network') }}{{ $t('monitor-down-up') }}</div>
          <div class="monitor-item-value">{{hasReport(item) ? `${formatBytes(item.State.NetInSpeed)}/s | ${formatBytes(item.State.NetOutSpeed)}/s` : '-'}}</div>
        </div>
        <div v-if="viewMode === 'list'" class="average">
          <div class="monitor-item-title">{{ $t('load') }} (1|5|15)</div>
          <div class="monitor-item-value">{{hasReport(item) ? `${item.State.Load1} | ${item.State.Load5} | ${item.State.Load15}` : '-'}}</div>
        </div>
        <div v-if="viewMode === 'list'" class="uptime" style="width: 120px;">
          <div class="monitor-item-title">{{ $t('due-time-only') }}</div>
          <div class="monitor-item-value">{{hostInfo[item.Host.Name] ? calculateRemainingDays(hostInfo[item.Host.Name].due_time) : '-'}}</div>
        </div>
        <div v-if="selectHost === item.Host.Name && hasReport(item) && isDetailModal(item)" class="detail-modal-backdrop" aria-hidden="true" @click.stop="closeHostDetail"></div>
        <div class="detail" v-if="selectHost === item.Host.Name && hasReport(item)" :role="isDetailModal(item) ? 'dialog' : undefined" :aria-modal="isDetailModal(item) ? 'true' : undefined" :aria-label="isDetailModal(item) ? $t('monitor-detail-aria', { name: item.Host.Name }) : undefined" @click.stop>
          <div class="mobile-detail-topbar">
            <div class="mobile-detail-header">
              <div class="mobile-detail-identity">
                <strong>{{item.Host.Name}}</strong>
                <span :class="{ 'is-online': item.status }">{{item.status ? $t('online') : $t('offline')}}</span>
              </div>
              <button class="mobile-detail-close" type="button" :aria-label="$t('monitor-close-detail')" @click.stop="closeHostDetail">×</button>
            </div>
            <div class="mobile-detail-tabs" role="tablist" :aria-label="$t('monitor-detail-tabs')">
              <button type="button" role="tab" :aria-selected="detailMobileTab === 'overview'" :class="{ 'is-active': detailMobileTab === 'overview' }" @click="handleDetailMobileTab('overview')">{{$t('monitor-tab-overview')}}</button>
              <button type="button" role="tab" :aria-selected="detailMobileTab === 'system'" :class="{ 'is-active': detailMobileTab === 'system' }" @click="handleDetailMobileTab('system')">{{$t('monitor-tab-system')}}</button>
              <button type="button" role="tab" :aria-selected="detailMobileTab === 'network'" :class="{ 'is-active': detailMobileTab === 'network' }" @click="handleDetailMobileTab('network')">{{$t('monitor-tab-network')}}</button>
              <button type="button" role="tab" :aria-selected="detailMobileTab === 'disk'" :class="{ 'is-active': detailMobileTab === 'disk' }" @click="handleDetailMobileTab('disk')">{{$t('monitor-tab-disk')}}</button>
              <button type="button" role="tab" :aria-selected="detailMobileTab === 'trends'" :class="{ 'is-active': detailMobileTab === 'trends' }" @click="handleDetailMobileTab('trends')">{{$t('monitor-tab-trends')}}</button>
            </div>
          </div>
          <button v-if="isDetailModal(item)" class="detail-modal-close" type="button" :aria-label="$t('monitor-close-detail')" @click.stop="closeHostDetail">×</button>
          <div class="purchase-info detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'system' }" v-if="getHostInfo(item.Host.Name).show_purchase_info">
            <div class="purchase-title">{{ $t('monitor-purchase') }}</div>
            <div class="purchase-grid">
              <div>
                <span>{{ $t('monitor-seller') }}</span>
                <strong>{{getHostInfo(item.Host.Name).seller || '-'}}</strong>
              </div>
              <div>
                <span>{{ $t('monitor-price') }}</span>
                <strong>{{getHostInfo(item.Host.Name).price || '-'}}</strong>
              </div>
              <div>
                <span>{{ $t('monitor-cycle') }}</span>
                <strong>{{getHostInfo(item.Host.Name).cycle || '-'}}</strong>
              </div>
              <div>
                <span>{{ $t('bandwidth-info') }}</span>
                <strong>{{getHostInfo(item.Host.Name).bandwidth || '-'}}</strong>
              </div>
              <div>
                <span>{{ $t('monitor-month-traffic') }}</span>
                <strong>{{getHostInfo(item.Host.Name).traffic || '-'}}</strong>
              </div>
              <div>
                <span>{{ $t('buy-url') }}</span>
                <a v-if="getHostInfo(item.Host.Name).buy_url" :href="getHostInfo(item.Host.Name).buy_url" target="_blank" @click.stop="() => {}">{{getHostInfo(item.Host.Name).buy_url}}</a>
                <strong v-else>-</strong>
              </div>
            </div>
          </div>
          <div class="health-grid detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'overview' }">
            <div class="health-card" :class="`is-${resourceLevel(item.State.CPU)}`">
              <div class="health-card-head"><span>CPU</span><span class="health-status">{{resourceStatus(item.State.CPU)}}</span></div>
              <div class="health-value">{{item.State.CPU.toFixed(2)}}<small>%</small></div>
              <div class="health-meta">{{cpuCoresText(item)}}</div>
              <div class="health-track"><span :style="resourceBarStyle(item.State.CPU)"></span></div>
            </div>
            <div class="health-card" :class="`is-${resourceLevel(memoryPercent(item))}`">
              <div class="health-card-head"><span>{{ $t('chart-memory') }}</span><span class="health-status">{{resourceStatus(memoryPercent(item))}}</span></div>
              <div class="health-value">{{memoryPercent(item).toFixed(2)}}<small>%</small></div>
              <div class="health-meta">{{formatBytes(item.State.MemUsed)}} / {{formatBytes(item.Host.MemTotal)}}</div>
              <div class="health-track"><span :style="resourceBarStyle(memoryPercent(item))"></span></div>
            </div>
            <div class="health-card" :class="`is-${resourceLevel(swapPercent(item))}`">
              <div class="health-card-head"><span>Swap</span><span class="health-status">{{item.Host.SwapTotal ? resourceStatus(swapPercent(item)) : $t('monitor-not-enabled')}}</span></div>
              <div class="health-value">{{item.Host.SwapTotal ? swapPercent(item).toFixed(2) : '-'}}<small v-if="item.Host.SwapTotal">%</small></div>
              <div class="health-meta">{{formatBytes(item.State.SwapUsed)}} / {{formatBytes(item.Host.SwapTotal)}}</div>
              <div class="health-track"><span :style="resourceBarStyle(swapPercent(item))"></span></div>
            </div>
            <div class="health-card" :class="`is-${resourceLevel(diskPercent(item))}`">
              <div class="health-card-head"><span>{{ $t('monitor-disk-total-usage') }}</span><span class="health-status">{{resourceStatus(diskPercent(item))}}</span></div>
              <div class="health-value">{{diskPercent(item).toFixed(2)}}<small>%</small></div>
              <div class="health-meta">{{formatBytes(item.State.DiskUsed)}} / {{formatBytes(item.State.DiskTotal)}}</div>
              <div class="health-track"><span :style="resourceBarStyle(diskPercent(item))"></span></div>
            </div>
          </div>

          <section class="mobile-detail-summary detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'overview' }">
            <div class="mobile-summary-cell is-wide"><span>{{$t('system')}}</span><strong>{{item.Host.Platform || '-'}}</strong></div>
            <div class="mobile-summary-cell"><span>{{$t('arch')}}</span><strong>{{item.Host.Arch || '-'}}</strong></div>
            <div class="mobile-summary-cell"><span>{{$t('due-time')}}</span><strong>{{getHostInfo(item.Host.Name).due_time ? moment(normalizeDueTime(getHostInfo(item.Host.Name).due_time)).format('YYYY-MM-DD') : '-'}}</strong></div>
            <div class="mobile-summary-cell"><span>{{$t('monitor-cur-in')}}</span><strong class="tone-in">{{formatBytes(item.State.NetInSpeed)}}/s</strong></div>
            <div class="mobile-summary-cell"><span>{{$t('monitor-cur-out')}}</span><strong class="tone-out">{{formatBytes(item.State.NetOutSpeed)}}/s</strong></div>
            <div class="mobile-summary-cell is-wide"><span>{{$t('monitor-uptime-label')}}</span><strong>{{formatUptimeZh(item.State.Uptime)}}</strong></div>
          </section>

          <div class="detail-overview-grid">
            <section class="detail-panel system-panel detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'system' }">
              <div class="panel-title"><span>{{ $t('monitor-sysinfo') }}</span><span class="panel-chip">{{compactPlatformName(item.Host.Platform)}}</span></div>
              <div class="info-grid">
                <div class="info-cell is-wide"><span class="info-label">{{ $t('hostname') }}</span><strong class="info-value key-value">{{item.Host.Hostname || item.Host.Name}}</strong></div>
                <div class="info-cell is-wide"><span class="info-label">{{ $t('system') }}</span><strong class="info-value">{{item.Host.Platform}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-kernel') }}</span><strong class="info-value">{{item.Host.Kernel || item.Host.PlatformVersion || '-'}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('arch') }}</span><strong class="info-value">{{item.Host.Arch || '-'}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('virtualization') }}</span><strong class="info-value key-value">{{item.Host.Virtualization || '-'}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-cores-label') }}</span><strong class="info-value">{{cpuCoresText(item)}}</strong></div>
                <div class="info-cell is-wide"><span class="info-label">{{ $t('monitor-cpu-model') }}</span><strong class="info-value">{{item.Host.CPUModel || '-'}}</strong></div>
                <div class="info-cell is-wide"><span class="info-label">{{ $t('monitor-gpu') }}</span><strong class="info-value key-value" v-if="item.Host.GPUs.length">{{item.Host.GPUs.join(' · ')}}</strong><strong class="info-value is-muted" v-else>{{ $t('monitor-no-gpu') }}</strong></div>
              </div>
            </section>

            <section class="detail-panel runtime-panel detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'network' }">
              <div class="panel-title"><span>{{ $t('monitor-net-runtime') }}</span><span class="panel-chip is-live">{{ $t('monitor-live') }}</span></div>
              <div class="info-grid">
                <div class="info-cell"><span class="info-label">{{ $t('monitor-cur-in') }}</span><strong class="info-value tone-in">{{formatBytes(item.State.NetInSpeed)}}/s</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-cur-out') }}</span><strong class="info-value tone-out">{{formatBytes(item.State.NetOutSpeed)}}/s</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-cycle-in') }}</span><strong class="info-value tone-in">{{formatBytes(item.State.CycleNetInTransfer)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-cycle-out') }}</span><strong class="info-value tone-out">{{formatBytes(item.State.CycleNetOutTransfer)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-total-in') }}</span><strong class="info-value">{{formatBytes(item.State.NetInTransfer)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-total-out') }}</span><strong class="info-value">{{formatBytes(item.State.NetOutTransfer)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-load-short') }}</span><strong class="info-value">{{item.State.Load1}} / {{item.State.Load5}} / {{item.State.Load15}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-conn') }}</span><strong class="info-value">{{item.State.Processes || 0}} / {{item.State.TCP || 0}} / {{item.State.UDP || 0}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-uptime-label') }}</span><strong class="info-value key-value">{{formatUptimeZh(item.State.Uptime)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-data-updated') }}</span><strong class="info-value tone-success">{{formatAgo(item.TimeStamp)}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-traffic-cycle') }}</span><strong class="info-value">{{ $t('monitor-reset-day', { day: item.State.TrafficResetDay || 1 }) }}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('monitor-next-reset') }}</span><strong class="info-value">{{formatDateStamp(item.State.TrafficNextReset)}}</strong></div>
                <div class="info-cell" v-if="hostInfo[item.Host.Name] && hostInfo[item.Host.Name].due_time"><span class="info-label">{{ $t('due-time') }}</span><strong class="info-value tone-warning">{{moment(normalizeDueTime(hostInfo[item.Host.Name].due_time)).format('YYYY-MM-DD')}}</strong></div>
                <div class="info-cell"><span class="info-label">{{ $t('report-time') }}</span><strong class="info-value">{{formatTimeStamp(item.TimeStamp)}}</strong></div>
              </div>
            </section>

            <section class="detail-panel disk-panel detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'disk' }">
              <div class="panel-title"><span>{{ $t('monitor-disk-detail') }}</span><span class="panel-chip">{{ $t('monitor-partitions', { count: item.State.Disks?.length || 0 }) }}</span></div>
              <div class="disk-grid" v-if="item.State.Disks && item.State.Disks.length">
                <div class="disk-row" :class="`is-${resourceLevel(disk.used_percent)}`" v-for="disk in item.State.Disks" :key="disk.mount">
                  <div class="disk-head"><strong>{{disk.mount}}</strong><span>{{disk.used_percent.toFixed(2)}}%</span></div>
                  <div class="disk-usage">{{formatBytes(disk.used)}} / {{formatBytes(disk.total)}} <small v-if="disk.fs_type">· {{disk.fs_type}}</small></div>
                  <div class="disk-track"><span :style="resourceBarStyle(disk.used_percent)"></span></div>
                </div>
              </div>
              <div class="empty-state" v-else>{{ $t('monitor-no-disk') }}</div>
              <div class="io-strip">
                <div><span>{{ $t('monitor-disk-read') }}</span><strong>{{formatBytes(item.State.DiskReadSpeed)}}/s</strong></div>
                <div><span>{{ $t('monitor-disk-write') }}</span><strong>{{formatBytes(item.State.DiskWriteSpeed)}}/s</strong></div>
              </div>
            </section>
          </div>

          <section class="charts-section detail-tab-section" :class="{ 'is-mobile-active': detailMobileTab === 'trends' }">
            <div class="trend-toolbar">
              <div class="trend-title"><span>{{ $t('monitor-trend-title') }}</span><small>{{ trendSubtitle(item.Host.Name) }}</small></div>
              <div class="trend-range" role="group" :aria-label="$t('monitor-trend-range-aria')">
                <button v-for="range in trendRanges" :key="range" type="button" :class="{ 'is-active': trendRange === range }" :aria-pressed="trendRange === range" @click="handleTrendRange(range, item.Host.Name)">{{ range === 'live' ? $t('monitor-trend-live') : range }}</button>
              </div>
            </div>
            <div v-if="trendLoading" class="trend-state is-loading" role="status">{{ $t('monitor-trend-loading') }}</div>
            <div v-else-if="trendError" class="trend-state is-error" role="alert"><span>{{ $t('monitor-trend-error') }}</span><BaseButton size="sm" @click="loadTrendHistory(item.Host.Name, trendRange)">{{ $t('common-retry') }}</BaseButton></div>
            <div v-else-if="trendEmpty" class="trend-state is-empty" role="status">{{ $t('monitor-trend-empty-fallback') }}</div>
            <div class="charts-grid">
              <div class="chart-panel"><CPU ref="cpuRef" :dark="dark" :live="!trendUsesHistory(item.Host.Name)" :data="trendSeriesFor(item.Host.Name, 'cpu')" /></div>
              <div class="chart-panel"><Mem ref="memRef" :dark="dark" :live="!trendUsesHistory(item.Host.Name)" :max="trendMemoryMax(item)" :data="trendSeriesFor(item.Host.Name, 'mem')" /></div>
              <div class="chart-panel"><NetIn ref="netInRef" :dark="dark" :live="!trendUsesHistory(item.Host.Name)" :data="trendSeriesFor(item.Host.Name, 'net_in')" /></div>
              <div class="chart-panel"><NetOut ref="netOutRef" :dark="dark" :live="!trendUsesHistory(item.Host.Name)" :data="trendSeriesFor(item.Host.Name, 'net_out')" /></div>
            </div>
          </section>
        </div>
      </div>
    </div>
    <SiteUpdateNotice :api-base="apiURL" />
    <SiteFooter :api-base="apiURL" :site-name="siteName" />
  </div>
  <div v-else class="app-loading" :aria-label="$t('monitor-loading')"><img src="/logo.svg" alt="" aria-hidden="true" /></div>
</template>

<style lang="scss">
.market-disabled {
  display: grid;
  max-width: 640px;
  min-height: 320px;
  margin: 48px auto;
  padding: 48px 28px;
  place-items: center;
  align-content: center;
  gap: 12px;
  border: 1px solid rgba(148, 163, 184, .24);
  border-radius: 18px;
  background: rgba(255, 255, 255, .72);
  text-align: center;
  box-shadow: 0 18px 60px rgba(15, 23, 42, .08);
}
.market-disabled__icon {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 50%;
  background: #eef2ff;
  color: #4f46e5;
  font-size: 28px;
  font-weight: 800;
}
.market-disabled h1 { margin: 0; color: #0f172a; font-size: 24px; }
.market-disabled p { max-width: 440px; margin: 0; color: #64748b; line-height: 1.7; }
.market-disabled button { margin-top: 8px; padding: 10px 18px; border: 0; border-radius: 9px; background: #111827; color: #fff; cursor: pointer; }
body[arco-theme='dark'] .market-disabled { border-color: rgba(255,255,255,.12); background: rgba(20,20,20,.82); }
body[arco-theme='dark'] .market-disabled h1 { color: #f8fafc; }
body[arco-theme='dark'] .market-disabled p { color: #a3a3a3; }
body[arco-theme='dark'] .market-disabled__icon { background: #1e1b4b; color: #a5b4fc; }
html {
  // Reserve the vertical scrollbar width so expanding a node does not shift
  // the whole dashboard horizontally. overflow-y is the legacy fallback for
  // browsers that do not support scrollbar-gutter yet.
  overflow-y: scroll;
  scrollbar-gutter: stable;
}

button,
a,
[role='button'],
.area-tab-item,
.hero-card,
.monitor-item {
  -webkit-tap-highlight-color: transparent;
  touch-action: manipulation;
}

html.node-modal-open,
body.node-modal-open {
  overflow: hidden;
}

body {
  margin: 0;
  min-height: 100vh;
  background: #f7f8fa;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
  color: #1f2937;
}

.app-loading {
  display: grid;
  min-height: 100vh;
  place-items: center;

  img {
    display: block;
    width: 58px;
    height: 58px;
    animation: loading-pulse 1s ease-in-out infinite alternate;
  }
}

@keyframes loading-pulse {
  to { opacity: .55; transform: scale(.92); }
}

a {
  text-decoration: none;
}

.max-container {
  margin: 0 auto;
  width: 100%;
  max-width: 1500px;
  padding-bottom: 88px;
}

.header {
  position: sticky;
  top: 0;
  z-index: 120;
  display: grid;
  grid-template-columns: minmax(130px, 1fr) auto minmax(130px, 1fr);
  align-items: center;
  gap: 14px;
  min-height: 88px;
  margin: 10px 14px 4px;
  padding: 40px 0 7px;
  border: 0;
  background: linear-gradient(180deg, rgba(247, 248, 250, .96) 0%, rgba(247, 248, 250, .82) 74%, rgba(247, 248, 250, 0) 100%);
  backdrop-filter: blur(3px);

  .logo {
    justify-self: start;
    min-width: 0;
    padding: 5px 12px 5px 6px;
    border: 1px solid rgba(23, 33, 47, .08);
    border-radius: 999px;
    background: rgba(255, 255, 255, .9);
    box-shadow: 0 8px 24px rgba(15, 23, 42, .07);
    backdrop-filter: blur(14px);
    color: #17212f;
    font: inherit;
  }

  .header-nav {
    justify-self: center;
  }

  .header-actions {
    justify-self: end;
  }

  .theme-btn,
  .account-btn {
    border: 1px solid #d9e1ea!important;
    background-color: #ffffff!important;
    color: #17212f!important;
    box-shadow: 0 8px 24px rgba(15, 23, 42, .07);
    border-radius: 999px;
  }

  .account-btn svg { width: 17px; height: 17px; }
}

.arco-dropdown {
  padding: 4px!important;
  border-radius: 8px!important;
  .arco-dropdown-option {
    border-radius: 4px !important;
    padding: 8px;
    line-height: 13px;
    font-size: 13px;
  }
}

.monitor-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  margin: 22px 14px 8px;
}

.area-tabs {
  flex: 1;
  min-width: 0;
  margin: 0;
  scroll-margin-top: 102px;

  .area-tab-item {
    margin-bottom: 10px;
    margin-right: 10px;
    padding: 8px 14px;
    border-radius: 999px;
    cursor: pointer;
    border: 1px solid rgba(31,41,55,.1);
    background: #fff;
    box-shadow: none;
    display: inline-block;
    color: inherit;
    font: inherit;

    .area-region-flag {
      margin-right: 3px;
      margin-top: -2px;
    }

    &.is-active {
      background: #111827;
      color: #fff;
      border-color: #17212f;
    }

    &:focus-visible { outline: 2px solid #165dff; outline-offset: 2px; }
  }

}

.monitor-controls{display:flex;flex:none;align-items:center;gap:6px}.monitor-select{width:132px}.monitor-controls .view-switch{margin-left:2px}

.view-switch {
  display: inline-flex;
  flex: none;
  gap: 3px;
  padding: 4px;
  border: 1px solid rgba(31, 41, 55, .1);
  border-radius: 12px;
  background: #ffffff;

  button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 32px;
    padding: 0 11px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: #64748b;
    cursor: pointer;
    font: inherit;
    font-size: 12px;
    font-weight: 700;

    span[aria-hidden] { font-size: 16px; line-height: 1; }

    &.is-active {
      background: #111827;
      color: #ffffff;
      box-shadow: 0 5px 14px rgba(15, 23, 42, .18);
    }
  }
}

.monitor-card {
  position: relative;
  margin: 0 auto;
  padding: 14px;

  .monitor-item {
    position: relative;
    margin-bottom: 14px;
    padding: 18px 26px;
    border-radius: 14px;
    border: 1px solid rgba(31,41,55,.08);
    display: block;
    background: #fff;
    box-shadow: none;
    cursor: pointer;

    &.is-unavailable {
      cursor: default;
    }

    // Repeated clicks should toggle the card, not select changing telemetry
    // text. Detail content remains selectable.
    & > .name,
    & > .platform,
    & > .cpu,
    & > .mem,
    & > .disk,
    & > .network,
    & > .average,
    & > .uptime {
      user-select: none;
    }

    &.is-active {
      background: #ffffff;
      border-color: rgba(17,24,39,.22);

      &>.detail {
        margin-top: 15px;
        border-top: 1px solid #eeeeee;
        padding-top: 15px;
        display: block;
      }
    }

    &:hover {
      background: #ffffff;

    }

    .region-flag {
      margin-right: 5px;
    }

    .monitor-item-title {
      margin-bottom: 3px;
      font-size: 11px;
      opacity: .6;
    }

    .monitor-item-value {
      font-weight: 500;
    }

    .monitor-item-progress {
      margin-top: 4px;
      height: 4px;
      display: block;
    }

    .detail {
      width: 100%;
    }

    .name {
      display: inline-block;
      vertical-align: middle;
      width: 230px;

      .title {
        margin-bottom: 5px;
        display: flex;
        align-items: center;
        font-weight: 600;
        font-size: 16px;
      }

      .status {
        display: flex;
        align-items: center;
        &::before {
          margin-right: 10px;
          position: relative;
          display: block;
          content: '';
          width: 8px;
          height: 8px;
          border-radius: 12px;
          background-color: #fb2c36;
        }

        &.online {
          &::before {
            background-color: #00c951;
          }
        }

        &.pending {
          &::before {
            background-color: #94a3b8;
          }
        }

        span {
          font-size: 13px;
          opacity: .6;
        }

        .status-meta {
          margin-left: 6px;
        }
      }
    }

    .platform {
      display: inline-block;
      vertical-align: top;
      width: 150px;

      .monitor-item-value {
        max-width: 140px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
    }

    .cpu {
      display: inline-block;
      vertical-align: top;
      width: 120px;
    }

    .mem {
      display: inline-block;
      vertical-align: top;
      width: 120px;
    }

    .disk {
      display: inline-block;
      vertical-align: top;
      width: 120px;
    }

    .average {
      display: inline-block;
      vertical-align: top;
      width: 200px;
    }

    .network {
      display: inline-block;
      vertical-align: top;
      width: 200px;
    }

    .uptime {
      display: inline-block;
      vertical-align: middle;
      width: 200px;
    }

    .detail {
      display: none;
      cursor: default;
      user-select: text;

      .purchase-info {
        margin: 0 0 16px;
        padding: 14px;
        border: 1px solid #e5e7eb;
        border-radius: 12px;
        background: #f8fafc;

        .purchase-title {
          margin-bottom: 8px;
          font-size: 13px;
          font-weight: 700;
          color: #111827;
        }

        .purchase-grid {
          display: grid;
          grid-template-columns: repeat(4, minmax(0, 1fr));
          gap: 10px;

          div {
            padding: 10px;
            border-radius: 8px;
            background: #fff;
            border: 1px solid #eef0f3;
            min-width: 0;
          }

          span {
            display: block;
            margin-bottom: 5px;
            font-size: 12px;
            color: #6b7280;
          }

          strong,
          a {
            font-size: 13px;
            color: #111827;
            font-weight: 600;
            word-break: break-all;
          }
        }
      }

      .health-grid {
        display: grid;
        grid-template-columns: repeat(4, minmax(0, 1fr));
        gap: 12px;
        margin-bottom: 14px;
      }

      .health-card {
        --health-color: #16a34a;
        --health-soft: #f0fdf4;
        min-width: 0;
        padding: 14px 15px;
        border: 1px solid color-mix(in srgb, var(--health-color) 20%, #e5e7eb);
        border-radius: 12px;
        background: linear-gradient(145deg, var(--health-soft), #ffffff 72%);

        &.is-warning {
          --health-color: #d97706;
          --health-soft: #fffbeb;
        }

        &.is-danger {
          --health-color: #dc2626;
          --health-soft: #fef2f2;
        }

        .health-card-head {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 8px;
          color: #64748b;
          font-size: 12px;
          font-weight: 650;
        }

        .health-status {
          padding: 2px 7px;
          border-radius: 999px;
          background: color-mix(in srgb, var(--health-color) 10%, transparent);
          color: var(--health-color);
          font-size: 11px;
          font-weight: 750;
        }

        .health-value {
          margin-top: 8px;
          color: var(--health-color);
          font-size: 25px;
          font-weight: 800;
          font-variant-numeric: tabular-nums;
          letter-spacing: -.02em;

          small {
            margin-left: 2px;
            font-size: 13px;
            font-weight: 700;
          }
        }

        .health-meta {
          margin-top: 3px;
          overflow: hidden;
          color: #64748b;
          font-size: 11px;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .health-track {
          height: 5px;
          margin-top: 11px;
          overflow: hidden;
          border-radius: 999px;
          background: #e5e7eb;

          span {
            display: block;
            height: 100%;
            border-radius: inherit;
            background: var(--health-color);
            transition: width .25s ease;
          }
        }
      }

      .detail-overview-grid {
        display: grid;
        grid-template-columns: minmax(0, .95fr) minmax(0, 1.15fr) minmax(0, 1.1fr);
        gap: 14px;
        align-items: stretch;
      }

      .detail-panel,
      .chart-panel {
        min-width: 0;
        border: 1px solid #e5e7eb;
        border-radius: 12px;
        background: #ffffff;
      }

      .detail-panel {
        padding: 15px;
      }

      .panel-title,
      .section-heading {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        color: #0f172a;
        font-size: 14px;
        font-weight: 800;
      }

      .panel-title {
        margin-bottom: 14px;
        padding-bottom: 10px;
        border-bottom: 1px solid #eef2f7;

        & > span:first-child {
          display: flex;
          align-items: center;
          gap: 8px;

          &::before {
            width: 7px;
            height: 7px;
            border-radius: 50%;
            background: #2563eb;
            content: '';
          }
        }
      }

      .panel-chip {
        max-width: 58%;
        padding: 3px 8px;
        overflow: hidden;
        border-radius: 999px;
        background: #eff6ff;
        color: #2563eb;
        font-size: 11px;
        font-weight: 700;
        text-overflow: ellipsis;
        white-space: nowrap;

        &.is-live {
          background: #ecfdf5;
          color: #059669;
        }
      }

      .info-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 13px 18px;
      }

      .info-cell {
        min-width: 0;

        &.is-wide {
          grid-column: 1 / -1;
        }
      }

      .info-label {
        display: block;
        margin-bottom: 4px;
        color: #94a3b8;
        font-size: 11px;
      }

      .info-value {
        display: block;
        overflow: hidden;
        color: #1e293b;
        font-size: 12px;
        font-weight: 700;
        line-height: 1.45;
        text-overflow: ellipsis;
        white-space: nowrap;

        &.is-muted {
          color: #94a3b8;
          font-weight: 600;
        }
      }

      .key-value {
        color: #2563eb;
      }

      .tone-in {
        color: #0f766e;
      }

      .tone-out {
        color: #9333ea;
      }

      .tone-success {
        color: #16a34a;
      }

      .tone-warning {
        color: #d97706;
      }

      .disk-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 9px;
      }

      .disk-row {
        --disk-color: #16a34a;
        min-width: 0;
        padding: 10px;
        border: 1px solid #e5e7eb;
        border-radius: 9px;
        background: #f8fafc;

        &.is-warning {
          --disk-color: #d97706;
        }

        &.is-danger {
          --disk-color: #dc2626;
          border-color: #fecaca;
          background: #fef2f2;
        }

        .disk-head {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 8px;

          strong {
            color: #0f172a;
            font-size: 12px;
          }

          span {
            color: var(--disk-color);
            font-size: 12px;
            font-weight: 800;
            font-variant-numeric: tabular-nums;
          }
        }

        .disk-usage {
          margin-top: 4px;
          overflow: hidden;
          color: #64748b;
          font-size: 11px;
          text-overflow: ellipsis;
          white-space: nowrap;

          small {
            color: #94a3b8;
          }
        }

        .disk-track {
          height: 4px;
          margin-top: 8px;
          overflow: hidden;
          border-radius: 999px;
          background: #e2e8f0;

          span {
            display: block;
            height: 100%;
            border-radius: inherit;
            background: var(--disk-color);
          }
        }
      }

      .io-strip {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 9px;
        margin-top: 10px;

        div {
          padding: 9px 10px;
          border-radius: 8px;
          background: #f8fafc;
        }

        span {
          display: block;
          color: #94a3b8;
          font-size: 10px;
        }

        strong {
          display: block;
          margin-top: 3px;
          color: #334155;
          font-size: 12px;
        }
      }

      .empty-state {
        padding: 30px 10px;
        color: #94a3b8;
        font-size: 12px;
        text-align: center;
      }

      .charts-section {
        margin-top: 14px;
      }

      .trend-toolbar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 12px;
        margin-bottom: 10px;
      }

      .trend-title {
        display: grid;
        min-width: 0;
        gap: 2px;

        span {
          color: #0f172a;
          font-size: 14px;
          font-weight: 800;
        }

        small {
          color: #94a3b8;
          font-size: 11px;
          font-weight: 500;
        }
      }

      .trend-range {
        display: inline-grid;
        grid-template-columns: repeat(5, minmax(42px, auto));
        flex: none;
        gap: 3px;
        padding: 3px;
        border: 1px solid #e2e8f0;
        border-radius: 8px;
        background: #f1f5f9;

        button {
          min-height: 30px;
          padding: 0 9px;
          border: 0;
          border-radius: 6px;
          background: transparent;
          color: #64748b;
          cursor: pointer;
          font: inherit;
          font-size: 11px;
          font-weight: 700;

          &.is-active {
            background: #ffffff;
            color: #0f172a;
            box-shadow: 0 1px 4px rgba(15, 23, 42, .1);
          }

          &:focus-visible {
            outline: 2px solid #2563eb;
            outline-offset: 1px;
          }
        }
      }

      .trend-state {
        display: flex;
        min-height: 38px;
        margin-bottom: 10px;
        padding: 7px 10px;
        align-items: center;
        justify-content: center;
        gap: 10px;
        border: 1px solid #dbeafe;
        border-radius: 8px;
        background: #eff6ff;
        color: #1d4ed8;
        font-size: 12px;
        text-align: center;

        &.is-error {
          border-color: #fecaca;
          background: #fef2f2;
          color: #b91c1c;
        }

        &.is-empty {
          border-color: #fde68a;
          background: #fffbeb;
          color: #a16207;
        }
      }

      .section-heading {
        margin-bottom: 10px;

        small {
          color: #94a3b8;
          font-size: 11px;
          font-weight: 500;
        }
      }

      .charts-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 14px;
      }

      .chart-panel {
        padding: 14px 16px 8px;

        .name {
          display: block;
          width: auto;
          color: #0f172a;
          font-weight: 800;
        }
      }
    }
  }
}

.monitor-card:not(.is-grid-view) {
  .monitor-item {
    display: grid;
    grid-template-columns:
      minmax(190px, 1.4fr)
      minmax(120px, 1fr)
      repeat(3, minmax(82px, .75fr))
      minmax(150px, 1.3fr)
      minmax(120px, 1fr)
      minmax(90px, .75fr);
    align-items: center;
    gap: 12px;

    & > .name,
    & > .platform,
    & > .cpu,
    & > .mem,
    & > .disk,
    & > .network,
    & > .average,
    & > .uptime {
      display: block;
      width: auto;
      min-width: 0;
      margin: 0;
      vertical-align: initial;
    }

    & > .uptime {
      width: auto !important;
    }

    & > .detail {
      grid-column: 1 / -1;
    }
  }
}

.monitor-card.is-grid-view {
  --grid-card-min-height: 328px;

  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(310px, 1fr));
  grid-auto-rows: 1fr;
  align-items: stretch;
  gap: 14px;

  .monitor-item {
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    height: 100%;
    min-height: var(--grid-card-min-height);
    min-width: 0;
    margin-bottom: 0;
    padding: 16px;
    border-color: rgba(15, 23, 42, .11);
    border-radius: 16px;
    background: linear-gradient(145deg, rgba(255,255,255,.98), rgba(248,250,252,.92));
    box-shadow: 0 14px 36px rgba(15, 23, 42, .07);

    &:hover {
      border-color: rgba(37, 99, 235, .25);
      box-shadow: 0 18px 44px rgba(15, 23, 42, .1);
      transform: translateY(-2px);
    }

    &.is-unavailable:hover {
      border-color: rgba(15, 23, 42, .11);
      background: linear-gradient(145deg, rgba(255,255,255,.98), rgba(248,250,252,.92));
      box-shadow: 0 14px 36px rgba(15, 23, 42, .07);
      transform: none;
    }

    &.is-active {
      transform: none;
    }

    > .name {
      display: block;
      width: auto;
      padding-bottom: 12px;
      border-bottom: 1px solid rgba(148, 163, 184, .22);

      .title { margin-bottom: 7px; font-size: 17px; }
    }

    > .detail {
      position: fixed;
      top: 50%;
      left: 50%;
      z-index: 1001;
      box-sizing: border-box;
      width: min(1180px, calc(100vw - 40px));
      max-height: calc(100vh - 40px);
      margin: 0;
      padding: 20px;
      overflow-y: auto;
      border: 1px solid rgba(15, 23, 42, .13);
      border-radius: 18px;
      background: #ffffff;
      box-shadow: 0 32px 100px rgba(15, 23, 42, .3);
      cursor: default;
      transform: translate(-50%, -50%);
    }
  }

  .grid-summary {
    display: flex;
    flex: 1;
    flex-direction: column;
    margin-top: 11px;
  }

  .grid-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    min-height: 24px;

    span {
      padding: 4px 7px;
      border-radius: 6px;
      background: #eef2f7;
      color: #64748b;
      font-size: 10px;
      font-weight: 700;
    }

    .is-price { background: #fff1f2; color: #e11d48; }
  }

  .grid-pending-summary {
    display: grid;
    flex: 1;
    min-height: 92px;
    margin-top: 12px;
    padding: 14px;
    place-content: center;
    gap: 5px;
    border: 1px dashed #cbd5e1;
    border-radius: 10px;
    background: rgba(248, 250, 252, .78);
    color: #475569;
    text-align: center;

    span {
      font-size: 13px;
      font-weight: 750;
    }

    small {
      color: #94a3b8;
      font-size: 10px;
    }
  }

  .capacity-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
    margin-top: 12px;
    padding: 11px 0;
    border-top: 1px solid rgba(148, 163, 184, .18);
    border-bottom: 1px solid rgba(148, 163, 184, .18);

    span {
      min-width: 0;
      overflow: hidden;
      color: #334155;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-size: 11px;
      font-weight: 650;
    }

    b { margin-right: 5px; color: #2563eb; }
    span:nth-child(2) b { color: #16a34a; }
    span:nth-child(3) b { color: #ef4444; }
  }

  .resource-rows {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
    padding: 11px 0;
  }

  .resource-row {
    display: grid;
    grid-template-columns: 1fr;
    min-width: 0;

    .resource-label { display: flex; justify-content: space-between; color: #334155; font-size: 12px; }
    .resource-label strong { font-variant-numeric: tabular-nums; }
    small { display: none; }
  }

  .resource-track {
    height: 7px;
    margin-top: 5px;
    overflow: hidden;
    border-radius: 999px;
    background: #e8edf3;

    span { display: block; height: 100%; border-radius: inherit; background: #16a34a; transition: width .25s ease; }
    span.is-warning { background: #f59e0b; }
    span.is-danger { background: #ef4444; }
  }

  .telemetry-rows {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 7px 12px;
    padding: 10px 0;
    border-top: 1px solid rgba(148, 163, 184, .2);

    div { display: block; min-width: 0; color: #475569; font-size: 11px; }
    div:nth-child(3) { grid-column: 1 / -1; }
    strong { display: block; margin-top: 2px; overflow: hidden; color: #1e293b; text-overflow: ellipsis; white-space: nowrap; font-weight: 650; }
  }

  .grid-footer {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    margin-top: auto;
    padding-top: 11px;
    border-top: 1px solid rgba(148, 163, 184, .2);
    color: #64748b;
    font-size: 10px;
  }
}

.detail-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(15, 23, 42, .5);
  backdrop-filter: blur(8px);
  cursor: default;
}

.detail-modal-close {
  position: sticky;
  top: 0;
  z-index: 6;
  display: grid;
  width: 36px;
  height: 36px;
  margin: 0 0 8px auto;
  padding: 0;
  place-items: center;
  border: 1px solid #e2e8f0;
  border-radius: 50%;
  background: rgba(255, 255, 255, .94);
  color: #0f172a;
  box-shadow: 0 8px 20px rgba(15, 23, 42, .12);
  cursor: pointer;
  font: inherit;
  font-size: 24px;
  line-height: 1;
}

.mobile-detail-topbar,
.mobile-detail-summary {
  display: none;
}

.monitor-card.is-grid-view.is-market-detail {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: block;
  margin: 0;
  padding: 0;
  pointer-events: none;

  > .monitor-item {
    width: 0 !important;
    height: 0 !important;
    min-height: 0 !important;
    margin: 0 !important;
    padding: 0 !important;
    border: 0 !important;
    background: transparent !important;
    box-shadow: none !important;
    pointer-events: none;
    transform: none !important;

    > :not(.detail-modal-backdrop):not(.detail) {
      display: none !important;
    }

    > .detail-modal-backdrop,
    > .detail {
      pointer-events: auto;
    }
  }
}

.logo {
  margin-top: 0;
  margin-bottom: 0;
  position: relative;
  cursor: pointer;
  line-height: 42px;
  height: 42px;
  font-size: 16px;
  font-weight: 900;
  color: #17212f;
  display: flex;
  align-items: center;

  .brand-mark {
    margin-right: 10px;
    width: 38px;
    height: 38px;
    border-radius: 14px;
    display: block;
    overflow: hidden;
    background: #fff;

    img {
      display: block;
      width: 100%;
      height: 100%;
      object-fit: cover;
    }
  }

  small {
    margin-left: 10px;
    font-weight: 500;
    color: #6b7684;
  }
}

.monitor-action-bar {
  margin: 0 10px;
  display: inline-block;
  border: 1px solid #e5e5e5;
  background: #ffffff;
  box-shadow: 0 2px 4px 0 rgba(133, 138, 180, 0.14);
  border-radius: 4px;

  .arco-tabs-content {
    display: none;
  }
}

html.node-modal-open .site-footer,
body.node-modal-open .site-footer {
  visibility: hidden;
}

body[arco-theme='dark'] {
  background-color: #111111;
  color: #ffffff;

  .arco-dropdown {
    background-color: #000000!important;
    border: 1px solid rgb(46 46 46)!important;
  }

  .arco-modal {
    background-color: #0e0e0e;
    border: 1px solid rgba(255,255,255,0.05);
  }

  .header {
    background: linear-gradient(180deg, rgba(17, 17, 17, .97) 0%, rgba(17, 17, 17, .84) 74%, rgba(17, 17, 17, 0) 100%);

    .logo {
      border-color: #303030;
      background: rgba(0, 0, 0, .88);
      color: #ffffff;
      box-shadow: 0 8px 24px rgba(0, 0, 0, .3);

      .brand-mark {
        background: #ffffff;
        color: #000000;
      }

      span,
      small {
        color: #ffffff;
      }
    }

    .theme-btn,
    .account-btn {
      border: 1px solid #333333!important;
      background-color: #000000!important;
      color: #ffffff!important;
    }
  }

  .area-tabs {
    .area-tab-item {
      border-color: #333333;
      background: #000000;
      color: #ffffff;
      box-shadow: none;

      &.is-active {
        background: #005fe705;
        color: #005fe7;
        border: 1px solid #005fe7;
      }
    }
  }

  .view-switch {
    border-color: #333333;
    background: #000000;

    button {
      color: #a3a3a3;

      &.is-active {
        background: #ffffff;
        color: #000000;
        box-shadow: none;
      }
    }
  }

  .monitor-card {
    .monitor-item {
      border: 1px solid rgb(255 255 255 / 16%);
      box-shadow: none;
      background-color: #000000;
      color: #ffffff;

      &:hover {
        background-color: #101010;
      }

      &.is-unavailable:hover {
        background-color: #000000;
      }

      .detail {
        border-color: #333333AA;

        .purchase-info,
        .detail-panel,
        .chart-panel {
          border-color: #333333;
          background: #080808;
        }

        .purchase-title,
        .panel-title,
        .section-heading,
        .chart-panel .name {
          color: #ffffff;
        }

        .panel-title {
          border-color: #262626;
        }

        .health-card {
          border-color: color-mix(in srgb, var(--health-color) 35%, #252525);
          background: linear-gradient(145deg, color-mix(in srgb, var(--health-color) 10%, #080808), #050505 72%);

          .health-card-head,
          .health-meta {
            color: #a3a3a3;
          }

          .health-track {
            background: #262626;
          }
        }

        .purchase-info .purchase-grid {
          div {
            border-color: #333333;
            background: #000000;
          }

          span {
            color: #aaaaaa;
          }

          strong,
          a {
            color: #ffffff;
          }
        }

        .panel-chip {
          background: #10213f;
          color: #7db2ff;

          &.is-live {
            background: #092c23;
            color: #55d6a8;
          }
        }

        .info-label,
        .section-heading small {
          color: #737373;
        }

        .trend-title {
          span { color: #ffffff; }
          small { color: #737373; }
        }

        .trend-range {
          border-color: #2b2b2b;
          background: #111111;

          button {
            color: #8d8d8d;

            &.is-active {
              background: #f5f5f5;
              color: #111111;
              box-shadow: none;
            }
          }
        }

        .trend-state {
          border-color: #1e3a5f;
          background: #0c1d33;
          color: #93c5fd;

          &.is-error {
            border-color: #5f2626;
            background: #2a1010;
            color: #fca5a5;
          }

          &.is-empty {
            border-color: #5c4819;
            background: #291f09;
            color: #fcd34d;
          }
        }

        .info-value {
          color: #f5f5f5;

          &.is-muted {
            color: #737373;
          }
        }

        .key-value {
          color: #7db2ff;
        }

        .tone-in {
          color: #5eead4;
        }

        .tone-out {
          color: #d8b4fe;
        }

        .tone-success {
          color: #4ade80;
        }

        .tone-warning {
          color: #fbbf24;
        }

        .disk-row {
          border-color: #292929;
          background: #101010;

          &.is-danger {
            border-color: #7f1d1d;
            background: #2a0d0d;
          }

          .disk-head strong {
            color: #ffffff;
          }

          .disk-usage {
            color: #a3a3a3;
          }

          .disk-track {
            background: #333333;
          }
        }

        .io-strip div {
          background: #101010;
        }

        .io-strip strong {
          color: #e5e5e5;
        }

        .mobile-detail-topbar {
          border-color: #262626;
          background: rgba(8, 8, 8, .96);
        }

        .mobile-detail-identity {
          strong { color: #ffffff; }
          span { color: #a3a3a3; }
          span.is-online { color: #55d6a8; }
        }

        .mobile-detail-close {
          border-color: #333333;
          background: #151515;
          color: #ffffff;
        }

        .mobile-detail-tabs {
          border-color: #2b2b2b;
          background: #111111;

          button {
            color: #8d8d8d;

            &.is-active {
              background: #f5f5f5;
              color: #050505;
            }
          }
        }

        .mobile-summary-cell {
          border-color: #2b2b2b;
          background: #101010;

          span { color: #737373; }
          strong { color: #f5f5f5; }
        }
      }
    }
  }

  .monitor-card.is-grid-view {
    .monitor-item {
      background: #000000;

      &:hover { background: #0b0b0b; }
      &.is-unavailable:hover { background: #000000; }

      > .name { border-color: #292929; }

      > .detail {
        border-color: #303030;
        background: #080808;
        box-shadow: 0 32px 100px rgba(0, 0, 0, .72);
      }
    }

    .grid-tags span { background: #171717; color: #a3a3a3; }
    .grid-tags .is-price { background: #2a0d16; color: #fb7185; }
    .grid-pending-summary { border-color: #333333; background: #080808; color: #d4d4d4; }
    .capacity-row, .telemetry-rows, .grid-footer { border-color: #292929; }
    .capacity-row span, .resource-label, .telemetry-rows div { color: #d4d4d4; }
    .resource-row small, .grid-footer { color: #737373; }
    .resource-track { background: #262626; }
    .telemetry-rows strong { color: #f5f5f5; }
  }

  .detail-modal-backdrop { background: rgba(0, 0, 0, .7); }
  .detail-modal-close { border-color: #333333; background: #111111; color: #ffffff; box-shadow: 0 8px 20px rgba(0,0,0,.4); }

}

@media screen and (max-width: 1200px) {
  .header {
    grid-template-columns: minmax(110px, 1fr) auto minmax(90px, 1fr);
  }

  .monitor-card .monitor-item .detail {
    .detail-overview-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .disk-panel {
      grid-column: 1 / -1;
    }

    .disk-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }

  .monitor-card:not(.is-grid-view) .monitor-item {
    grid-template-columns: minmax(180px, 1.4fr) minmax(120px, 1fr) repeat(3, minmax(86px, 1fr));
    row-gap: 10px;

    & > .network {
      grid-column: 1 / 3;
    }

    & > .average {
      grid-column: 3 / 5;
    }

    & > .uptime {
      grid-column: 5;
    }
  }
}

@media screen and (max-width: 900px) {
  .header {
    grid-template-columns: 48px auto minmax(84px, 1fr);
    gap: 8px;

    .logo {
      padding: 4px;

      & > span:not(.brand-mark),
      small {
        display: none;
      }
    }
  }

  .monitor-card .monitor-item .detail {
    .health-grid,
    .detail-overview-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .system-panel,
    .runtime-panel,
    .disk-panel {
      grid-column: auto;
    }

    .runtime-panel,
    .disk-panel {
      grid-column: 1 / -1;
    }

    .disk-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
}

@media screen and (max-width: 768px) {
  html.node-modal-open {
    scrollbar-gutter: auto;
  }

  .monitor-toolbar {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    min-width: 0;
    margin-right: 8px;
    margin-left: 8px;
  }

  .area-tabs {
    display: flex;
    min-width: 0;
    gap: 6px;
    overflow-x: auto;
    overscroll-behavior-inline: contain;
    scrollbar-width: none;
    white-space: nowrap;

    &::-webkit-scrollbar {
      display: none;
    }

    .area-tab-item {
      flex: none;
      margin: 0;
      padding: 7px 12px;
    }
  }

  .monitor-controls{grid-column:1/-1;display:grid;grid-template-columns:repeat(2,minmax(0,1fr)) auto;width:100%}.monitor-select{width:auto}.monitor-controls .view-switch{grid-column:-1;grid-row:1;margin-left:0}.monitor-controls>.monitor-select:nth-child(3){grid-column:1/3;grid-row:2}.monitor-controls>.monitor-select:nth-child(3)+.view-switch{grid-row:1}

  .view-switch {
    button {
      width: 34px;
      padding: 0;
      justify-content: center;

      span[aria-hidden] { font-size: 17px; }
      .view-label { display: none; }
    }
  }

  .monitor-card.is-grid-view {
    grid-template-columns: 1fr;
  }

  .header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-areas:
      "brand actions"
      "nav nav";
    justify-content: stretch;
    box-sizing: border-box;
    width: auto;
    max-width: none;
    min-height: 0;
    margin: 8px 8px 2px;
    padding: 8px 0 10px;
    gap: 8px 10px;

    .logo {
      position: static;
      grid-area: brand;
      justify-self: start;
      max-width: 100%;
      padding: 3px 10px 3px 4px;

      & > .brand-name {
        display: block !important;
        max-width: min(42vw, 160px);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
    }

    .header-nav {
      grid-area: nav;
      justify-self: stretch;
      width: 100%;
      min-width: 0;
      margin: 0 auto;
    }

    .brand-mark {
      width: 34px;
      height: 34px;
      border-radius: 12px;
    }

    .header-actions {
      position: static;
      grid-area: actions;
      justify-self: end;
      display: inline-flex !important;
      gap: 4px !important;

      .arco-btn {
        width: 34px;
        height: 34px;
        padding: 0;
      }
    }
  }

  .monitor-card .monitor-item .detail .purchase-info .purchase-grid {
    grid-template-columns: 1fr;
  }

  .monitor-card .monitor-item .detail {
    .health-grid,
    .detail-overview-grid,
    .charts-grid {
      grid-template-columns: 1fr;
    }

    .runtime-panel,
    .disk-panel {
      grid-column: auto;
    }
  }

  .monitor-card {
    padding: 8px;
  }

  .monitor-card:not(.is-grid-view) {
    .monitor-item {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px 10px;
      min-width: 0;
      margin-bottom: 8px;
      padding: 12px 14px;
      border-radius: 12px;

      & > .name {
        grid-column: 1 / -1;
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        width: auto;
        min-width: 0;
        margin: 0;
        padding-bottom: 8px;
        border-bottom: 1px solid rgba(148, 163, 184, .18);

        .title {
          min-width: 0;
          margin: 0;
          overflow: hidden;
          font-size: 14px;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .status {
          flex: none;
          white-space: nowrap;

          &::before {
            width: 7px;
            height: 7px;
            margin-right: 6px;
          }

          span {
            font-size: 11px;
          }
        }
      }

      & > .platform {
        grid-column: 1 / -1;
        display: flex;
        align-items: center;
        gap: 8px;
        width: auto;
        min-width: 0;
        margin: 0;

        .monitor-item-title {
          flex: none;
          margin: 0;
        }

        .monitor-item-value {
          max-width: none;
          min-width: 0;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
      }

      & > .cpu,
      & > .mem,
      & > .disk,
      & > .network,
      & > .average,
      & > .uptime {
        display: block;
        width: auto;
        min-width: 0;
        margin: 0;
      }

      & > .network {
        grid-column: 1 / 3;
      }

      & > .average {
        grid-column: 3;
      }

      & > .uptime {
        grid-column: 1 / -1;
        display: flex;
        align-items: center;
        gap: 8px;
        width: auto !important;

        .monitor-item-title {
          margin: 0;
        }
      }

      & > .detail {
        grid-column: 1 / -1;
      }

      .monitor-item-title {
        margin-bottom: 2px;
        font-size: 10px;
      }

      .monitor-item-value {
        overflow: hidden;
        font-size: 12px;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .monitor-item-progress {
        width: 100% !important;
        max-width: 70px;
        margin-top: 3px;
      }
    }

    .monitor-item > .detail {
      position: fixed;
      top: 50%;
      left: 50%;
      z-index: 1001;
      box-sizing: border-box;
      display: block;
      width: calc(100vw - 16px);
      max-height: calc(100vh - 16px);
      margin: 0;
      padding: 12px;
      overflow-y: auto;
      border: 1px solid rgba(15, 23, 42, .13);
      border-radius: 14px;
      background: #ffffff;
      box-shadow: 0 32px 100px rgba(15, 23, 42, .3);
      cursor: default;
      transform: translate(-50%, -50%);

    }
  }

  body[arco-theme='dark'] .monitor-card:not(.is-grid-view) .monitor-item > .detail {
    border-color: #303030;
    background: #080808;
    box-shadow: 0 32px 100px rgba(0, 0, 0, .72);

  }

  .monitor-card.is-grid-view {
    --grid-card-min-height: 240px;

    gap: 8px;
    padding: 8px;

    .monitor-item {
      padding: 12px 14px;
      border-radius: 12px;

      > .name {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        padding-bottom: 8px;

        .title {
          min-width: 0;
          margin: 0;
          overflow: hidden;
          font-size: 15px;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .status {
          flex: none;
          white-space: nowrap;
        }
      }

      > .detail {
        width: calc(100vw - 16px);
        max-height: calc(100vh - 16px);
        padding: 12px;
        border-radius: 14px;
      }
    }

    .grid-summary {
      margin-top: 8px;
    }

    .grid-pending-summary {
      min-height: 72px;
      margin-top: 8px;
      padding: 10px;
    }

    .grid-tags {
      min-height: 0;
      gap: 4px;

      span {
        padding: 3px 6px;
      }
    }

    .capacity-row {
      gap: 4px;
      margin-top: 8px;
      padding: 7px 0;
    }

    .resource-rows {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
      padding: 9px 0;
    }

    .resource-row {
      .resource-label {
        font-size: 11px;
      }

      small {
        display: none;
      }
    }

    .resource-track {
      height: 5px;
      margin-top: 4px;
    }

    .telemetry-rows {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 6px 10px;
      padding: 8px 0;

      div {
        display: block;
        min-width: 0;
      }

      div:nth-child(3) {
        grid-column: 1 / -1;
        display: block;
      }

      strong {
        display: block;
        margin-top: 2px;
      }
    }

    .grid-footer {
      padding-top: 8px;
    }
  }

  .monitor-card .monitor-item > .detail {
    .health-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
      margin-bottom: 10px;
    }

    .health-card {
      padding: 10px;

      .health-value {
        margin-top: 6px;
        font-size: 20px;
      }

      .health-track {
        margin-top: 7px;
      }
    }

  }

  .monitor-card:not(.is-grid-view) .monitor-item > .detail,
  .monitor-card.is-grid-view .monitor-item > .detail {
    position: fixed;
    top: 50%;
    left: 50%;
    z-index: 1001;
    display: block;
    box-sizing: border-box;
    width: min(520px, calc(100vw - 24px));
    height: auto;
    max-height: calc(100vh - 24px);
    max-height: calc(100dvh - 24px);
    margin: 0;
    padding: 0;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
    border: 1px solid rgba(15, 23, 42, .13);
    border-radius: 16px;
    background: #ffffff;
    box-shadow: 0 28px 80px rgba(15, 23, 42, .3);
    transform: translate(-50%, -50%);

    &::-webkit-scrollbar {
      display: none;
    }

    .detail-modal-close {
      display: none;
    }

    .mobile-detail-topbar {
      position: sticky;
      top: 0;
      z-index: 8;
      display: block;
      padding: 9px 12px;
      border-bottom: 1px solid rgba(148, 163, 184, .2);
      background: rgba(255, 255, 255, .96);
      backdrop-filter: blur(14px);
    }

    .mobile-detail-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      min-height: 36px;
    }

    .mobile-detail-identity {
      min-width: 0;

      strong {
        display: block;
        overflow: hidden;
        color: #0f172a;
        font-size: 15px;
        font-weight: 800;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      span {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        margin-top: 2px;
        color: #ef4444;
        font-size: 11px;
        font-weight: 700;

        &::before {
          width: 6px;
          height: 6px;
          border-radius: 50%;
          background: currentColor;
          content: '';
        }

        &.is-online {
          color: #16a34a;
        }
      }
    }

    .mobile-detail-close {
      display: grid;
      flex: none;
      width: 34px;
      height: 34px;
      padding: 0;
      place-items: center;
      border: 1px solid #e2e8f0;
      border-radius: 50%;
      background: #f8fafc;
      color: #334155;
      cursor: pointer;
      font: inherit;
      font-size: 22px;
      line-height: 1;
    }

    .mobile-detail-tabs {
      display: grid;
      grid-template-columns: repeat(5, minmax(0, 1fr));
      gap: 3px;
      margin-top: 8px;
      padding: 3px;
      border: 1px solid #e2e8f0;
      border-radius: 8px;
      background: #f1f5f9;

      button {
        min-width: 0;
        min-height: 32px;
        padding: 6px 2px;
        overflow: hidden;
        border: 0;
        border-radius: 6px;
        background: transparent;
        color: #64748b;
        cursor: pointer;
        font: inherit;
        font-size: 11px;
        font-weight: 700;
        text-overflow: ellipsis;
        white-space: nowrap;

        &.is-active {
          background: #ffffff;
          color: #0f172a;
          box-shadow: 0 1px 4px rgba(15, 23, 42, .1);
        }
      }
    }

    .detail-tab-section:not(.is-mobile-active) {
      display: none !important;
    }

    .health-grid,
    .purchase-info,
    .detail-panel,
    .charts-section,
    .mobile-detail-summary {
      margin-right: 12px;
      margin-left: 12px;
    }

    .trend-toolbar {
      align-items: stretch;
      flex-direction: column;
      gap: 8px;
    }

    .trend-range {
      grid-template-columns: repeat(5, minmax(0, 1fr));
      width: 100%;

      button {
        min-width: 0;
        padding: 0 3px;
      }
    }

    .trend-state {
      align-items: center;
      flex-wrap: wrap;
    }

    .health-grid {
      margin-top: 12px;
    }

    .mobile-detail-summary {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
      margin-top: 0;
      margin-bottom: max(16px, env(safe-area-inset-bottom));
    }

    .mobile-summary-cell {
      min-width: 0;
      padding: 10px 11px;
      border: 1px solid #e5e7eb;
      border-radius: 8px;
      background: #f8fafc;

      &.is-wide {
        grid-column: 1 / -1;
      }

      span {
        display: block;
        margin-bottom: 4px;
        color: #94a3b8;
        font-size: 10px;
      }

      strong {
        display: block;
        overflow: hidden;
        color: #1e293b;
        font-size: 12px;
        font-weight: 750;
        line-height: 1.45;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
    }

    .purchase-info {
      margin-top: 12px;
    }

    .detail-overview-grid {
      display: block;
    }

    .detail-panel,
    .charts-section {
      margin-top: 12px;
      margin-bottom: max(16px, env(safe-area-inset-bottom));
    }

    .charts-grid {
      padding-bottom: 1px;
    }
  }

  body[arco-theme='dark'] .monitor-card:not(.is-grid-view) .monitor-item > .detail,
  body[arco-theme='dark'] .monitor-card.is-grid-view .monitor-item > .detail {
    background: #080808;
  }
}

@media screen and (max-width: 576px) {
  html,
  body,
  #app,
  .max-container {
    width: 100%;
    max-width: 100%;
  }

  .monitor-card .monitor-item .detail {
    .system-panel .info-grid,
    .disk-grid {
      grid-template-columns: 1fr;
    }

    .runtime-panel .info-grid,
    .io-strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .runtime-panel .info-grid {
      gap: 10px 12px;
    }

    .runtime-panel .info-label {
      margin-bottom: 2px;
    }

    .runtime-panel .info-value {
      font-size: 11px;
    }

    .info-cell.is-wide {
      grid-column: auto;
    }
  }
}

@media screen and (max-width: 1920px) {
  .max-container {
    max-width: 1440px;
  }
}

@media screen and (max-width: 1280px) {
  .max-container {
    max-width: 1140px;
  }
}

@media screen and (max-width: 992px) {
  .max-container {
    max-width: 960px;
  }
}

@media screen and (max-width: 768px) {
  .max-container {
    max-width: 720px;
  }
}

@media screen and (max-width: 576px) {
  .max-container {
    max-width: 540px;
  }
}
</style>
