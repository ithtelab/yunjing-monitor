<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { OpenNewWindow, Refresh } from '@iconoir/vue'

const props = defineProps({ apiBase: { type: String, default: '' } })
const { t } = useI18n()
const info = ref(null)
const dismissed = ref('')
const loadedVersion = String(import.meta.env.VITE_BUILD_VERSION || 'dev')
let timer

const stalePage = computed(() => {
  const current = info.value?.current
  return current && current !== 'dev' && loadedVersion !== 'dev' && current !== loadedVersion
})
const available = computed(() => Boolean(info.value?.update_available && info.value?.latest !== dismissed.value))
const visible = computed(() => stalePage.value || available.value)

const load = async () => {
  try {
    const response = await fetch(`${props.apiBase || ''}/api/site/version`, { credentials: 'same-origin', cache: 'no-store' })
    if (response.ok) info.value = await response.json()
  } catch {}
}
const dismiss = () => {
  dismissed.value = info.value?.latest || 'dismissed'
  sessionStorage.setItem('monitor_update_dismissed', dismissed.value)
}
const act = () => {
  if (stalePage.value) {
    window.location.reload()
    return
  }
  if (info.value?.release_url?.startsWith('/')) {
    window.location.href = info.value.release_url
  } else if (info.value?.release_url) {
    window.open(info.value.release_url, '_blank', 'noopener,noreferrer')
  }
}
const onFocus = () => load()

onMounted(() => {
  dismissed.value = sessionStorage.getItem('monitor_update_dismissed') || ''
  load()
  timer = window.setInterval(load, 15 * 60 * 1000)
  window.addEventListener('focus', onFocus)
})
onBeforeUnmount(() => {
  window.clearInterval(timer)
  window.removeEventListener('focus', onFocus)
})
</script>

<template>
  <aside v-if="visible" class="update-notice" role="status">
    <button class="update-notice__main" type="button" @click="act">
      <Refresh v-if="stalePage" aria-hidden="true" />
      <OpenNewWindow v-else aria-hidden="true" />
      <span>
        <strong>{{ stalePage ? t('update-page-ready') : t('update-available') }}</strong>
        <small>{{ stalePage ? t('update-refresh') : info.latest }}</small>
      </span>
    </button>
    <button v-if="!stalePage" class="update-notice__close" type="button" :aria-label="t('update-dismiss')" @click="dismiss">×</button>
  </aside>
</template>

<style scoped>
.update-notice{position:fixed;right:18px;bottom:66px;z-index:89;display:flex;align-items:stretch;overflow:hidden;border:1px solid rgba(23,33,47,.12);border-radius:8px;background:rgba(255,255,255,.96);box-shadow:0 12px 32px rgba(15,23,42,.14);backdrop-filter:blur(12px)}
.update-notice button{border:0;background:transparent;color:#17212f;cursor:pointer}.update-notice__main{display:flex;min-height:46px;padding:7px 10px;align-items:center;gap:8px;text-align:left}.update-notice__main:hover{background:#f3f5f7}.update-notice__main svg{width:17px;height:17px;color:#165dff}.update-notice__main span{display:grid;gap:1px}.update-notice__main strong{font-size:12px}.update-notice__main small{color:#64748b;font-size:10px}.update-notice__close{width:30px;border-left:1px solid rgba(23,33,47,.08)!important;font-size:17px}.update-notice__close:hover{background:#f3f5f7}
:global(body[arco-theme='dark'] .update-notice){border-color:rgba(255,255,255,.12);background:rgba(22,22,23,.96);box-shadow:0 12px 32px rgba(0,0,0,.4)}
:global(body[arco-theme='dark'] .update-notice button){color:#f2f3f5}:global(body[arco-theme='dark'] .update-notice__main:hover),:global(body[arco-theme='dark'] .update-notice__close:hover){background:#2b2b2c}:global(body[arco-theme='dark'] .update-notice__main small){color:#a3a3a3}:global(body[arco-theme='dark'] .update-notice__close){border-left-color:rgba(255,255,255,.1)!important}
@media(max-width:640px){.update-notice{right:10px;bottom:82px;max-width:calc(100vw - 20px)}.update-notice__main{min-height:42px;padding:6px 9px}}
</style>
