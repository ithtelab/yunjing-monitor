<script setup>
import { onMounted, ref, watch } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import BaseInput from '@/components/ui/BaseInput.vue'

const { t } = useI18n()

const props = defineProps({
  apiBase: { type: String, default: '' },
  endpoint: { type: String, default: '/api/market/captcha' },
  modelValue: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'update:captchaId'])

const captchaId = ref('')
const captchaImage = ref('')
const loading = ref(false)

const loadCaptcha = async () => {
  loading.value = true
  try {
    const base = props.apiBase || ''
    const res = await axios.get(`${base}${props.endpoint}`)
    captchaId.value = res.data?.captcha_id || ''
    captchaImage.value = res.data?.captcha_image || ''
    emit('update:captchaId', captchaId.value)
    emit('update:modelValue', '')
  } catch (e) {
    Message.error(t('captcha-load-fail'))
  } finally {
    loading.value = false
  }
}

onMounted(loadCaptcha)
watch(() => props.apiBase, () => {
  if (props.apiBase !== undefined) loadCaptcha()
})

defineExpose({ refresh: loadCaptcha })
</script>

<template>
  <div class="captcha-input">
    <BaseInput
      class="captcha-input__grow"
      type="text"
      maxlength="8"
      autocomplete="off"
      :placeholder="t('captcha-label')"
      :model-value="modelValue"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <button class="captcha-input__img-btn" type="button" :disabled="loading" @click="loadCaptcha" :title="t('captcha-refresh')">
      <img v-if="captchaImage" :src="captchaImage" alt="captcha" />
      <span v-else>{{ loading ? t('common-loading') : t('captcha-click-load') }}</span>
    </button>
  </div>
</template>

<style scoped>
.captcha-input {
  display: flex;
  gap: 10px;
  align-items: stretch;
}
/* 输入框已收口到公共组件 BaseInput（class 落在外层包装 div，撑满剩余宽度） */
.captcha-input__grow {
  flex: 1;
  min-width: 0;
}
.captcha-input__img-btn {
  width: 130px;
  height: 38px;
  padding: 0;
  border-radius: 8px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: #f7f8fa;
  cursor: pointer;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: #86909c;
}
.captcha-input__img-btn img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
body[arco-theme='dark'] .captcha-input__img-btn {
  background: #2a2a2b;
  border-color: rgba(255, 255, 255, 0.12);
}
</style>
