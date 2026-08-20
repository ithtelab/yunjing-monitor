<script setup>
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { useI18n } from 'vue-i18n'
import BaseButton from '@/components/ui/BaseButton.vue'

const props = defineProps({ apiBase: { type: String, default: '' } })
const emit = defineEmits(['start'])
const { t } = useI18n()
const authenticated = ref(false)

onMounted(async () => {
  try {
    const response = await axios.get(`${props.apiBase || ''}/api/account/me`, { withCredentials: true })
    authenticated.value = response.data?.authenticated === true || Boolean(response.data?.account?.email || response.data?.user?.email)
  } catch {
    authenticated.value = false
  }
})

const start = () => {
  localStorage.setItem('monitor-onboarding-intent', 'create-node')
  emit('start')
}
</script>

<template>
  <section class="first-node glow-card">
    <div class="first-node__mark" aria-hidden="true"><span></span><span></span><span></span></div>
    <div class="first-node__copy">
      <span class="first-node__eyebrow">{{ t('onboarding-eyebrow') }}</span>
      <h2>{{ t('onboarding-title') }}</h2>
      <p>{{ t('onboarding-subtitle') }}</p>
      <ol>
        <li><b>1</b><span><strong>{{ t('onboarding-step-account') }}</strong><small>{{ t('onboarding-step-account-desc') }}</small></span></li>
        <li><b>2</b><span><strong>{{ t('onboarding-step-install') }}</strong><small>{{ t('onboarding-step-install-desc') }}</small></span></li>
        <li><b>3</b><span><strong>{{ t('onboarding-step-online') }}</strong><small>{{ t('onboarding-step-online-desc') }}</small></span></li>
      </ol>
    </div>
    <BaseButton variant="primary" size="lg" @click="start">{{ t(authenticated ? 'onboarding-create-now' : 'onboarding-login-start') }}</BaseButton>
  </section>
</template>

<style scoped>
.first-node{display:grid;grid-template-columns:96px minmax(0,1fr) auto;width:min(900px,calc(100% - 28px));margin:24px auto 48px;padding:26px;align-items:center;gap:24px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.first-node__mark{position:relative;display:grid;width:76px;height:76px;place-items:center;border-radius:8px;background:var(--color-fill-2,#f2f3f5)}.first-node__mark span{position:absolute;width:38px;height:7px;border-radius:2px;background:#165dff}.first-node__mark span:first-child{transform:translateY(-15px)}.first-node__mark span:last-child{transform:translateY(15px)}.first-node__mark span:nth-child(2){width:24px}.first-node__eyebrow{color:#165dff;font-size:10px;font-weight:800}.first-node h2{margin:5px 0;font-size:20px}.first-node p{margin:0;color:var(--color-text-3,#86909c);font-size:12px}.first-node ol{display:flex;margin:18px 0 0;padding:0;gap:16px;list-style:none}.first-node li{display:flex;min-width:0;align-items:flex-start;gap:7px}.first-node li>b{display:grid;width:20px;height:20px;flex:none;place-items:center;border-radius:50%;background:rgba(22,93,255,.08);color:#165dff;font-size:10px}.first-node li>span{display:grid;gap:2px}.first-node li strong{font-size:11px}.first-node li small{color:var(--color-text-3,#86909c);font-size:9px;line-height:1.4}
body[arco-theme='dark'] .first-node{background:#232324;border-color:rgba(255,255,255,.1)}
@media(max-width:720px){.first-node{grid-template-columns:56px minmax(0,1fr);margin-top:14px;padding:15px;gap:12px}.first-node__mark{width:52px;height:52px}.first-node__mark span{width:28px;height:5px}.first-node__mark span:first-child{transform:translateY(-10px)}.first-node__mark span:last-child{transform:translateY(10px)}.first-node__mark span:nth-child(2){width:18px}.first-node h2{font-size:17px}.first-node ol{grid-column:1/-1;display:grid;margin-top:8px;gap:8px}.first-node>.base-btn{grid-column:1/-1;width:100%;height:42px}}
</style>
