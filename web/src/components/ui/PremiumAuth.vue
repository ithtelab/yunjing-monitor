<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import IconEmail from '@arco-design/web-vue/es/icon/icon-email'
import IconLock from '@arco-design/web-vue/es/icon/icon-lock'
import IconEye from '@arco-design/web-vue/es/icon/icon-eye'
import IconEyeInvisible from '@arco-design/web-vue/es/icon/icon-eye-invisible'
import IconLoading from '@arco-design/web-vue/es/icon/icon-loading'
import IconExclamationCircleFill from '@arco-design/web-vue/es/icon/icon-exclamation-circle-fill'
import IconInfoCircleFill from '@arco-design/web-vue/es/icon/icon-info-circle-fill'
import { useI18n } from 'vue-i18n'
import ParticleButton from '@/components/ui/ParticleButton.vue'

const { t } = useI18n()

const props = defineProps({
  initialMode: { type: String, default: 'login' }, // login | reset
  loading: { type: Boolean, default: false },
  errorMessage: { type: String, default: '' },
  successMessage: { type: String, default: '' }
})

const emit = defineEmits(['login', 'mode-change', 'go-submit'])

const authMode = ref(props.initialMode === 'reset' ? 'reset' : 'login')
const showPassword = ref(false)
const fieldTouched = reactive({})

// 注册在上架流程中自动完成（提交第一条上架即建号），登录框只保留登录。
const formData = reactive({
  email: '',
  password: '',
  rememberMe: false
})

const errors = reactive({
  email: '',
  password: '',
  general: ''
})

const isLoading = computed(() => props.loading)
const successMessage = computed(() => props.successMessage)
const generalError = computed(() => props.errorMessage || errors.general)

watch(
  () => props.initialMode,
  (mode) => {
    authMode.value = mode === 'reset' ? 'reset' : 'login'
  }
)

watch(authMode, (mode) => {
  emit('mode-change', mode)
  Object.keys(errors).forEach((k) => {
    errors[k] = ''
  })
  if (mode === 'login') {
    const savedEmail = localStorage.getItem('userEmail')
    const rememberMe = localStorage.getItem('rememberMe') === 'true'
    if (savedEmail) {
      formData.email = savedEmail
      formData.rememberMe = rememberMe
    }
  }
})

onMounted(() => {
  const savedEmail = localStorage.getItem('userEmail')
  const rememberMe = localStorage.getItem('rememberMe') === 'true'
  if (savedEmail && authMode.value === 'login') {
    formData.email = savedEmail
    formData.rememberMe = rememberMe
  }
})

function validateField(field, value) {
  let error = ''
  switch (field) {
    case 'email':
      if (!value || !String(value).trim()) error = t('auth-email-required')
      else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(value))) error = t('auth-email-invalid')
      break
    case 'password':
      if (!value) error = t('auth-password-required')
      break
    default:
      break
  }
  return error
}

function handleInput(field, value) {
  formData[field] = value
  if (fieldTouched[field]) {
    errors[field] = validateField(field, value)
  }
}

function handleBlur(field) {
  fieldTouched[field] = true
  errors[field] = validateField(field, formData[field])
}

function validateForm() {
  let ok = true
  ;['email', 'password'].forEach((field) => {
    const err = validateField(field, formData[field])
    errors[field] = err
    if (err) ok = false
  })
  return ok
}

function setMode(mode) {
  authMode.value = mode
  showPassword.value = false
}

async function handleSubmit() {
  errors.general = ''
  if (authMode.value !== 'login' || !validateForm()) return

  if (formData.rememberMe) {
    localStorage.setItem('userEmail', formData.email)
    localStorage.setItem('rememberMe', 'true')
  } else {
    localStorage.removeItem('userEmail')
    localStorage.removeItem('rememberMe')
  }
  emit('login', {
    email: formData.email,
    password: formData.password,
    rememberMe: formData.rememberMe
  })
}

const title = computed(() => (authMode.value === 'reset' ? t('auth-reset-title') : t('auth-welcome')))
const subtitle = computed(() =>
  authMode.value === 'reset' ? t('auth-reset-subtitle') : t('auth-login-subtitle')
)
</script>

<template>
  <div class="premium-auth" role="dialog" aria-modal="true" aria-labelledby="auth-title">
    <div v-if="successMessage" class="auth-banner auth-banner--success">
      <IconInfoCircleFill class="auth-banner__icon" />
      <span>{{ successMessage }}</span>
    </div>

    <div v-if="generalError" class="auth-banner auth-banner--error">
      <IconExclamationCircleFill class="auth-banner__icon" />
      <span>{{ generalError }}</span>
    </div>

    <div class="auth-header">
      <h2 id="auth-title">{{ title }}</h2>
      <p>{{ subtitle }}</p>
    </div>

    <form class="auth-form" @submit.prevent="handleSubmit">
      <!-- Reset：本站未接入邮件服务，无自助找回；引导联系管理员 -->
      <div v-if="authMode === 'reset'" class="auth-panel">
        <div class="auth-panel__intro">
          <IconLock class="auth-panel__hero-icon" />
          <h3>{{ t('auth-reset-hero') }}</h3>
          <p>{{ t('auth-reset-desc-1') }}<br />{{ t('auth-reset-desc-2') }}</p>
        </div>

        <ParticleButton block size="lg" @click="setMode('login')">{{ t('auth-back-login') }}</ParticleButton>
      </div>

      <!-- Login -->
      <div v-else class="auth-panel">
        <div class="field">
          <div class="field-input" :class="{ 'is-error': errors.email }">
            <IconEmail class="field-input__icon" />
            <input
              :value="formData.email"
              type="email"
              :placeholder="t('auth-email-label')"
              :aria-label="t('auth-email-label')"
              autocomplete="email"
              @input="handleInput('email', $event.target.value)"
              @blur="handleBlur('email')"
            />
          </div>
          <p v-if="errors.email" class="field-error">
            <IconExclamationCircleFill /> {{ errors.email }}
          </p>
        </div>

        <div class="field">
          <div class="field-input" :class="{ 'is-error': errors.password }">
            <IconLock class="field-input__icon" />
            <input
              :value="formData.password"
              :type="showPassword ? 'text' : 'password'"
              :placeholder="t('auth-password-label')"
              :aria-label="t('auth-password-label')"
              autocomplete="current-password"
              @input="handleInput('password', $event.target.value)"
              @blur="handleBlur('password')"
            />
            <button
              type="button"
              class="field-input__toggle"
              :aria-label="showPassword ? t('auth-hide-password') : t('auth-show-password')"
              @click="showPassword = !showPassword"
            >
              <IconEyeInvisible v-if="showPassword" />
              <IconEye v-else />
            </button>
          </div>
          <p v-if="errors.password" class="field-error">
            <IconExclamationCircleFill /> {{ errors.password }}
          </p>
        </div>

        <div class="auth-meta">
          <label class="check">
            <input
              type="checkbox"
              :checked="formData.rememberMe"
              @change="handleInput('rememberMe', $event.target.checked)"
            />
            <span>{{ t('auth-remember') }}</span>
          </label>
          <button type="button" class="text-link" @click="setMode('reset')">{{ t('auth-forgot') }}</button>
        </div>

        <ParticleButton type="submit" block size="lg" :disabled="isLoading" :show-icon="!isLoading">
          <IconLoading v-if="isLoading" class="spin" />
          <span v-else>{{ t('auth-login') }}</span>
        </ParticleButton>

        <p class="auth-hint">
          {{ t('auth-no-account') }}
          <button type="button" class="text-link" @click="emit('go-submit')">
            {{ t('auth-auto-signup') }}
          </button>
        </p>
      </div>
    </form>
  </div>
</template>

<style scoped>
.premium-auth {
  width: 100%;
  max-width: 420px;
  padding: 4px 24px 24px;
  box-sizing: border-box;
}

.auth-banner {
  margin-bottom: 16px;
  padding: 12px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  animation: fade-slide-top 0.25s ease both;
}

.auth-banner--success {
  background: rgba(34, 197, 94, 0.15);
  border: 1px solid rgba(74, 222, 128, 0.3);
  color: #15803d;
}

.auth-banner--error {
  background: rgba(239, 68, 68, 0.12);
  border: 1px solid rgba(239, 68, 68, 0.28);
  color: #dc2626;
}

.auth-banner__icon {
  font-size: 16px;
  flex-shrink: 0;
}

.auth-header {
  text-align: center;
  margin-bottom: 28px;
}

.auth-header h2 {
  margin: 0 0 8px;
  font-size: 24px;
  font-weight: 700;
  line-height: 1.25;
  color: var(--color-text-1, #1d2129);
}

.auth-header p {
  margin: 0;
  font-size: 14px;
  color: var(--color-text-3, #86909c);
}

.auth-form {
  width: 100%;
}

.auth-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  animation: fade-slide-right 0.28s ease both;
}

.auth-panel__intro {
  text-align: center;
  margin-bottom: 8px;
}

.auth-panel__hero-icon {
  display: block;
  margin: 0 auto 12px;
  font-size: 48px;
  color: #165dff;
}

.auth-panel__intro h3 {
  margin: 0 0 8px;
  font-size: 20px;
  font-weight: 600;
}

.auth-panel__intro p {
  margin: 0;
  font-size: 13px;
  color: var(--color-text-3, #86909c);
  line-height: 1.5;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field-input {
  position: relative;
  display: flex;
  align-items: center;
}

.field-input__icon {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 20px;
  color: var(--color-text-3, #86909c);
  pointer-events: none;
  z-index: 1;
}

.field-input input {
  width: 100%;
  height: 48px;
  padding: 12px 16px 12px 40px;
  border-radius: 12px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: color-mix(in srgb, var(--color-fill-2, #f2f3f5) 50%, transparent);
  color: inherit;
  font: inherit;
  font-size: 14px;
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;
}

.field-input input::placeholder {
  color: var(--color-text-3, #86909c);
}

.field-input input:focus {
  border-color: rgba(22, 93, 255, 0.45);
  box-shadow: 0 0 0 3px rgba(22, 93, 255, 0.12);
}

.field-input.is-error input {
  border-color: #f53f3f;
}

.field-input__toggle {
  position: absolute;
  right: 12px;
  top: 50%;
  transform: translateY(-50%);
  border: 0;
  background: transparent;
  color: var(--color-text-3, #86909c);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  font-size: 20px;
  transition: color 0.15s ease;
}

.field-input__toggle:hover {
  color: var(--color-text-1, #1d2129);
}

.field-input:has(.field-input__toggle) input {
  padding-right: 48px;
}

.field-error {
  margin: 4px 0 0;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #f53f3f;
}

.auth-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 24px;
}

.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
  color: var(--color-text-3, #86909c);
  user-select: none;
}

.check input {
  width: 16px;
  height: 16px;
  accent-color: #165dff;
  cursor: pointer;
}

/* 登录/返回按钮已收口到公共组件 ParticleButton（黑色主调），旧的 .auth-submit 蓝色样式移除 */

.auth-hint {
  margin: 0;
  text-align: center;
  font-size: 12px;
  color: var(--color-text-3, #86909c);
}

.auth-hint .text-link {
  font-size: inherit;
}

.text-link {
  border: 0;
  background: transparent;
  color: #165dff;
  cursor: pointer;
  padding: 0;
  font-size: 13px;
  transition: color 0.15s ease;
}

.text-link:hover {
  color: rgba(22, 93, 255, 0.8);
}

.spin {
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@keyframes fade-slide-right {
  from { opacity: 0; transform: translateX(12px); }
  to { opacity: 1; transform: translateX(0); }
}

@keyframes fade-slide-top {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}

body[arco-theme='dark'] .field-input input {
  background: rgba(255, 255, 255, 0.04);
  border-color: rgba(255, 255, 255, 0.12);
}

body[arco-theme='dark'] .auth-banner--success {
  color: #86efac;
}
</style>
