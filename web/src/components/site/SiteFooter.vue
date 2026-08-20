<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { OpenNewWindow } from '@iconoir/vue'
import MagneticLink from '@/components/ui/MagneticLink.vue'
import TextScramble from '@/components/ui/TextScramble.vue'

const props = defineProps({ apiBase: { type: String, default: '' }, siteName: { type: String, default: '云镜监控' } })
const { t } = useI18n()
const config = ref(null)
const stats = ref({ online: 0, today_ips: 0, today_views: 0, total_views: 0 })
const statsReady = ref(false)
let heartbeat
let statsRefresh
let refreshing = false

const randomID = () => crypto.randomUUID?.().replaceAll('-', '') || `${Date.now()}${Math.random().toString(36).slice(2)}`
const getVisitorID = () => { let id=localStorage.getItem('monitor_visitor_id');if(!id||id.length<16){id=randomID();localStorage.setItem('monitor_visitor_id',id)}return id }
const postVisit = async (pageView = false) => {
  try {
    const response = await fetch(`${props.apiBase || ''}/api/site/visit`, { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({ visitor_id:getVisitorID(), event_id:pageView?randomID():'', page_view:pageView }), credentials:'same-origin', keepalive:true })
    if (response.ok) { stats.value = await response.json(); statsReady.value = true }
  } catch {}
}
const refreshStats = async () => {
  if (refreshing || document.visibilityState !== 'visible') return
  refreshing = true
  try {
    const response = await fetch(`${props.apiBase || ''}/api/site/visitor-stats`, { credentials:'same-origin', cache:'no-store' })
    if (response.ok) { stats.value = await response.json(); statsReady.value = true }
  } catch {} finally { refreshing = false }
}
const syncVisibleStats = () => { if (document.visibilityState === 'visible') postVisit(false) }
const startStats = () => {
  postVisit(true)
  heartbeat=window.setInterval(()=>{if(document.visibilityState==='visible')postVisit(false)},60000)
  statsRefresh=window.setInterval(refreshStats,5000)
  document.addEventListener('visibilitychange',syncVisibleStats)
  window.addEventListener('focus',refreshStats)
}
const load = async () => { try { const response=await fetch(`${props.apiBase || ''}/api/site/footer`,{credentials:'same-origin'});if(!response.ok)return;config.value=await response.json();if(!config.value.visitor_stats_hidden)startStats() } catch {} }
const formatted = (value) => new Intl.NumberFormat().format(Number(value) || 0)
const statItems = computed(() => (config.value?.visitor_stats_items || []).map((key,index) => ({ key, label:t(`footer-stat-${key.replaceAll('_','-')}`), value:formatted(stats.value[key]), delay:index*100 })))
const copyright = computed(() => config.value?.text || `© ${new Date().getFullYear()} ${props.siteName} ${t('footer-copyright')}`)
onMounted(load)
onBeforeUnmount(() => {
  window.clearInterval(heartbeat)
  window.clearInterval(statsRefresh)
  document.removeEventListener('visibilitychange',syncVisibleStats)
  window.removeEventListener('focus',refreshStats)
})
</script>

<template>
  <footer v-if="config && !config.hidden" class="site-footer">
    <div class="site-footer__inner" :class="{ 'has-links': config.links?.length }">
      <span class="site-footer__copy">{{ copyright }}</span>
      <div v-if="!config.visitor_stats_hidden && statsReady" class="site-footer__stats" :aria-label="t('footer-stats')">
        <div v-for="item in statItems" :key="item.key" class="footer-stat" :class="`footer-stat--${item.key}`"><TextScramble :text="item.value" :delay="item.delay" /><span>{{ item.label }}</span></div>
      </div>
      <div v-if="config.links?.length" class="site-footer__links-row">
        <span class="site-footer__links-title">{{ config.links_title || t('footer-links') }}</span>
        <nav class="site-footer__links" :aria-label="config.links_title || t('footer-links')">
          <MagneticLink v-for="link in config.links" :key="link.url">
            <a :href="link.url" :target="link.new_tab ? '_blank' : undefined" :rel="link.new_tab ? 'noopener noreferrer' : undefined">{{ link.label }}<OpenNewWindow aria-hidden="true" /></a>
          </MagneticLink>
        </nav>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.site-footer{position:fixed;right:0;bottom:0;left:0;z-index:90;width:100%;border-top:1px solid rgba(23,33,47,.09);background:rgba(247,248,250,.94);backdrop-filter:blur(10px)}
.site-footer__inner{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);width:calc(100% - 64px);min-height:48px;margin:0 auto;padding:6px 0;align-items:center;gap:18px;color:#64748b}
.site-footer__copy{grid-column:1;justify-self:start;font-size:11px;white-space:nowrap}
.site-footer__stats{grid-column:3;display:flex;min-width:0;align-items:center;justify-content:flex-end;justify-self:end}.footer-stat{--stat-color:#475569;display:inline-flex;min-width:58px;padding:0 7px;align-items:baseline;justify-content:center;gap:3px;border-left:1px solid rgba(148,163,184,.24)}.footer-stat::before{content:"";width:4px;height:4px;flex:none;border-radius:50%;align-self:center;background:var(--stat-color)}.footer-stat:first-child{border-left:0}.footer-stat--online{--stat-color:#16a34a}.footer-stat--today_ips{--stat-color:#0284c7}.footer-stat--today_views{--stat-color:#7c3aed}.footer-stat--total_views{--stat-color:#d97706}.footer-stat :deep(.text-scramble){min-width:0;color:var(--stat-color);font-size:13px;font-weight:800;line-height:1}.footer-stat>span{font-size:8px;white-space:nowrap}
.site-footer__links-row{grid-column:2;grid-row:1;display:flex;min-width:0;align-items:center;justify-content:center;justify-self:center;gap:8px}.site-footer__links-title{flex:none;font-size:10px;font-weight:700}.site-footer__links{display:flex;max-width:320px;min-width:0;justify-content:center;gap:6px;flex-wrap:nowrap;overflow-x:auto;scrollbar-width:none}.site-footer__links::-webkit-scrollbar{display:none}
.site-footer__links a{display:inline-flex;height:24px;padding:0 8px;align-items:center;gap:3px;border-radius:999px;background:#eef0f3;color:#1f2937;font-size:10px;text-decoration:none;white-space:nowrap;transition:background-color .18s,color .18s}.site-footer__links a:hover{background:#111;color:#fff}.site-footer__links svg{width:11px;height:11px}
:global(body[arco-theme='dark'] .site-footer){border-top-color:rgba(255,255,255,.09);background:#0d0d0d}
:global(body[arco-theme='dark'] .site-footer__inner){color:#a3a3a3}
:global(body[arco-theme='dark'] .footer-stat--online){--stat-color:#4ade80}
:global(body[arco-theme='dark'] .footer-stat--today_ips){--stat-color:#38bdf8}
:global(body[arco-theme='dark'] .footer-stat--today_views){--stat-color:#c084fc}
:global(body[arco-theme='dark'] .footer-stat--total_views){--stat-color:#fbbf24}
:global(body[arco-theme='dark'] .site-footer__links a){background:#242424;color:#e5e5e5}
:global(body[arco-theme='dark'] .site-footer__links a:hover){background:#f5f5f5;color:#111}
@media(max-width:640px){.site-footer__inner{grid-template-columns:minmax(0,1fr) auto;width:100%;min-height:68px;padding:5px 10px calc(5px + env(safe-area-inset-bottom,0px));gap:3px 8px}.site-footer__stats{grid-column:1/-1;grid-row:1;width:100%;justify-self:stretch;justify-content:center}.footer-stat{min-width:0;padding:0 9px;flex:1}.site-footer__copy{grid-column:1/-1;grid-row:2;justify-self:stretch;overflow:hidden;text-align:center;text-overflow:ellipsis}.site-footer__inner.has-links .site-footer__copy{grid-column:1;text-align:left}.site-footer__links-row{grid-column:2;grid-row:2;max-width:48vw;justify-self:end}.site-footer__links-title{display:none}.site-footer__links{max-width:100%}}
</style>
