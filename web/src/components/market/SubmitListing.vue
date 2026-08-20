<script setup>
import { onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import CaptchaInput from './CaptchaInput.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import CopyCommandBox from '@/components/ui/CopyCommandBox.vue'
import BackButton from '@/components/ui/BackButton.vue'
import BaseDatePicker from '@/components/ui/BaseDatePicker.vue'
import BillingFields from '@/components/ui/BillingFields.vue'

const { t } = useI18n()
import MarketGuide from './MarketGuide.vue'

const props = defineProps({
  apiBase: { type: String, default: '' }
})
const emit = defineEmits(['navigate', 'submitted'])

const captchaRef = ref(null)
const captchaId = ref('')
const captchaCode = ref('')
const submitting = ref(false)
const result = ref(null)
const showGuide = ref(false)

const form = reactive({
  email: '',
  password: '',
  password_confirm: '',
  display_name: '',
  region: '',
  specs: '',
  price: '',
	price_amount: '',
	price_currency: 'USD',
	billing_cycle: 'monthly',
  listing_type: 'rent',
  contact: '',
  description: '',
  due_date: ''
})

onMounted(() => {
  try {
    const raw = sessionStorage.getItem('marketSignupPrefill')
    if (!raw) return
    const data = JSON.parse(raw)
    if (data?.email) form.email = String(data.email)
    if (data?.password) {
      form.password = String(data.password)
      form.password_confirm = String(data.password)
    }
    if (data?.display_name) form.display_name = String(data.display_name)
    sessionStorage.removeItem('marketSignupPrefill')
  } catch {}
})

const submit = async () => {
  if (!form.email || !form.password || !form.display_name || !(Number(form.price_amount) > 0) || !form.contact) {
    Message.warning(t('submit-warn-required'))
    return
  }
  if (form.password !== form.password_confirm) {
    Message.warning(t('submit-warn-password'))
    return
  }
  if (!captchaCode.value) {
    Message.warning(t('submit-warn-captcha'))
    return
  }
  submitting.value = true
  try {
    const base = props.apiBase || ''
    const res = await axios.post(`${base}/api/market/submit`, {
      ...form,
	  price: `${form.price_currency} ${form.price_amount}`,
	  price_amount: Number(form.price_amount),
      captcha_id: captchaId.value,
      captcha_code: captchaCode.value
    }, { withCredentials: true })
    result.value = res.data
    Message.success(t('submit-success-tip'))
    emit('submitted', res.data)
  } catch (e) {
    const msg = e?.response?.data || e?.message || t('submit-fail')
    Message.error(typeof msg === 'string' ? msg : t('submit-fail'))
    captchaRef.value?.refresh?.()
  } finally {
    submitting.value = false
  }
}

// 注：复制逻辑已收口到公共组件 CopyCommandBox，本文件不再有 copyText
</script>

<template>
  <div class="submit-page">
    <header class="submit-page__head">
      <BackButton @click="emit('navigate', 'market')">{{ t('submit-back-market') }}</BackButton>
      <h1>{{ t('submit-title') }}</h1>
      <p>
        {{ t('submit-subtitle') }}
        <BaseButton variant="text" @click="showGuide = true">{{ t('submit-guide-link') }}</BaseButton>
      </p>
    </header>

    <MarketGuide :open="showGuide" @close="showGuide = false" @go-submit="showGuide = false" />

    <div v-if="result" class="submit-page__result">
      <h2>{{ t('submit-success-title') }}</h2>
      <p>{{ t('submit-node-id') }}<code>{{ result.node_id }}</code></p>
      <p>{{ t('submit-name-line') }}{{ result.display_name }} · {{ result.region || result.region_code }}</p>
      <label>{{ t('submit-linux-cmd') }}</label>
      <CopyCommandBox :command="result.linux || result.linux_install" />
      <label>{{ t('submit-windows-cmd') }}</label>
      <CopyCommandBox :command="result.windows || result.windows_install" />
      <div class="submit-page__actions">
        <BaseButton variant="primary" @click="emit('navigate', 'owner')">{{ t('submit-go-owner') }}</BaseButton>
        <BaseButton @click="emit('navigate', 'market')">{{ t('submit-back-market') }}</BaseButton>
      </div>
    </div>

    <form v-else class="submit-page__form" @submit.prevent="submit">
      <fieldset>
        <legend>{{ t('submit-account-legend') }}</legend>
        <label>{{ t('submit-email-label') }}</label>
        <BaseInput v-model="form.email" type="email" required autocomplete="email" />
        <label>{{ t('submit-password-label') }}</label>
        <BaseInput v-model="form.password" type="password" required minlength="8" autocomplete="new-password" />
        <label>{{ t('submit-password-confirm-label') }}</label>
        <BaseInput v-model="form.password_confirm" type="password" required minlength="8" autocomplete="new-password" />
      </fieldset>

      <fieldset>
        <legend>{{ t('submit-info-legend') }}</legend>
        <label>{{ t('submit-name-label') }}</label>
        <BaseInput v-model="form.display_name" type="text" required maxlength="64" />
        <label>{{ t('submit-region-label') }}</label>
        <BaseInput v-model="form.region" type="text" maxlength="32" :placeholder="t('submit-region-placeholder')" />
        <label>{{ t('submit-specs-label') }}</label>
        <BaseInput v-model="form.specs" type="text" maxlength="200" :placeholder="t('submit-specs-placeholder')" />
        <label>{{ t('submit-price-label') }}</label>
        <BillingFields v-model:amount="form.price_amount" v-model:currency="form.price_currency" v-model:cycle="form.billing_cycle" required />
        <label>{{ t('submit-type-label') }}</label>
        <BaseInput as="select" v-model="form.listing_type">
          <option value="rent">{{ t('market-type-rent') }}</option>
          <option value="sale">{{ t('market-type-sale') }}</option>
          <option value="transfer">{{ t('market-type-transfer') }}</option>
        </BaseInput>
        <label>{{ t('submit-contact-label') }}</label>
        <BaseInput v-model="form.contact" type="text" required maxlength="120" />
        <label>{{ t('submit-desc-label') }}</label>
        <BaseInput as="textarea" v-model="form.description" rows="1" maxlength="500" autogrow :max-rows="5" />
        <label>{{ t('submit-due-label') }}</label>
        <BaseDatePicker v-model="form.due_date" />
        <label>{{ t('submit-captcha-label') }}</label>
        <CaptchaInput
          ref="captchaRef"
          v-model="captchaCode"
          :api-base="apiBase"
          @update:captcha-id="captchaId = $event"
        />
      </fieldset>

      <BaseButton variant="primary" size="lg" block type="submit" :disabled="submitting">
        {{ submitting ? t('submit-submitting') : t('submit-btn') }}
      </BaseButton>
    </form>
  </div>
</template>

<style scoped>
.submit-page {
  max-width: 640px;
  margin: 0 auto;
  padding: 24px 16px 48px;
}
.submit-page__head h1 {
  margin: 8px 0 6px;
  font-size: 22px;
}
.submit-page__head p,
.submit-page__result p {
  color: var(--color-text-3, #86909c);
  font-size: 14px;
}
.link {
  border: 0;
  background: transparent;
  color: #165dff;
  cursor: pointer;
  padding: 0;
  font-size: 14px;
}
.submit-page__form fieldset {
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: 12px;
  padding: 14px;
  margin: 0 0 14px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.submit-page__form legend {
  padding: 0 6px;
  font-weight: 600;
}
.submit-page__form label {
  font-size: 13px;
  color: var(--color-text-2, #4e5969);
  margin-top: 4px;
}
/* 表单控件与命令复制盒已收口到 BaseInput / CopyCommandBox，旧的 input/.cmd-box 样式移除 */
.submit-page__result {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.submit-page__actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
</style>
