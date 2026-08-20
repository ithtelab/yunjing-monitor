<script setup>
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import axios from 'axios'
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import PremiumAuth from '@/components/ui/PremiumAuth.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import BaseInput from '@/components/ui/BaseInput.vue'
import CopyCommandBox from '@/components/ui/CopyCommandBox.vue'
import BackButton from '@/components/ui/BackButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import BaseDatePicker from '@/components/ui/BaseDatePicker.vue'
import BillingFields from '@/components/ui/BillingFields.vue'
import { billingOf } from '@/utils/billing.js'
import { formatBytes } from '@/utils/utils.js'

const { t } = useI18n()
import ListingCard from './ListingCard.vue'

const props = defineProps({
  apiBase: { type: String, default: '' }
})
const emit = defineEmits(['navigate'])

const loading = ref(true)
const authenticated = ref(false)
const me = ref(null)
const nodes = ref([])
const installBox = ref(null)
const editing = ref(null)
const saving = ref(false)
const subscriptions = ref([])
const subscriptionsLoading = ref(false)
const subscriptionSaving = ref(false)
const subscriptionEditing = ref(false)
const marketReports = ref([])
const marketAppeals = ref([])
const appealSaving = ref(false)
const appealForm = reactive({ report_id: '', message: '' })

const emptySubscriptionForm = () => ({
  id: '', name: '', regions: '', tags: '', max_price: '', currency: 'CNY', min_memory_gb: '', enabled: true
})
const subscriptionForm = reactive(emptySubscriptionForm())

const loggingIn = ref(false)
const loginError = ref('')
const editForm = reactive({
  node_id: '',
  display_name: '',
  region: '',
  listing_type: 'rent',
  contact: '',
  description: '',
  specs: '',
  price: '',
	price_amount: '',
	price_currency: 'USD',
	billing_cycle: 'monthly',
  due_date: '',
  for_sale: true
})

const api = (path, options = {}) => {
  const base = props.apiBase || ''
  return axios({
    url: `${base}${path}`,
    withCredentials: true,
    ...options
  })
}

const setLoginLock = (locked) => {
  document.body.classList.toggle('owner-login-lock', locked)
}

const refreshMe = async () => {
  const res = await api('/api/owner/me')
  authenticated.value = !!res.data?.authenticated
  me.value = res.data
  return authenticated.value
}

const loadNodes = async () => {
  const res = await api('/api/owner/nodes')
  nodes.value = Array.isArray(res.data) ? res.data : []
}

const loadSubscriptions = async () => {
  subscriptionsLoading.value = true
  try {
    const res = await api('/api/owner/subscriptions')
    subscriptions.value = Array.isArray(res.data) ? res.data : []
  } catch {
    subscriptions.value = []
  } finally {
    subscriptionsLoading.value = false
  }
}

const loadMarketAppeals = async () => {
  try {
    const res = await api('/api/owner/market-appeals')
    marketReports.value = Array.isArray(res.data?.reports) ? res.data.reports : []
    marketAppeals.value = Array.isArray(res.data?.appeals) ? res.data.appeals : []
  } catch {
    marketReports.value = []
    marketAppeals.value = []
  }
}

const bootstrap = async () => {
  loading.value = true
  try {
    if (await refreshMe()) {
      await Promise.all([loadNodes(), loadSubscriptions(), loadMarketAppeals()])
      setLoginLock(false)
    } else {
      setLoginLock(true)
    }
  } catch {
    authenticated.value = false
    setLoginLock(true)
  } finally {
    loading.value = false
  }
}

const handleLogin = async ({ email, password, rememberMe }) => {
  loginError.value = ''
  loggingIn.value = true
  try {
    await api('/api/owner/login', {
      method: 'POST',
      data: { email, password, remember_me: !!rememberMe }
    })
    Message.success(t('owner-login-success'))
    await bootstrap()
  } catch {
    loginError.value = t('owner-login-fail')
    Message.error(t('owner-login-fail'))
  } finally {
    loggingIn.value = false
  }
}

const logout = async () => {
  try {
    await api('/api/owner/logout', { method: 'POST', data: {} })
  } catch {}
  authenticated.value = false
  me.value = null
  nodes.value = []
  subscriptions.value = []
  marketReports.value = []
  marketAppeals.value = []
  editing.value = null
  installBox.value = null
  setLoginLock(true)
}

const splitSubscriptionValues = (value) => String(value || '')
  .split(/[,，\n]/)
  .map((item) => item.trim())
  .filter(Boolean)

const resetSubscriptionForm = () => {
  Object.assign(subscriptionForm, emptySubscriptionForm())
  subscriptionEditing.value = false
}

const openSubscription = (item = null) => {
  Object.assign(subscriptionForm, item ? {
    id: item.id || '',
    name: item.name || '',
    regions: (item.regions || []).join(', '),
    tags: (item.tags || []).join(', '),
    max_price: Number(item.max_price) > 0 ? Number(item.max_price) : '',
    currency: item.currency || 'CNY',
    min_memory_gb: Number(item.min_memory) > 0 ? Number(item.min_memory) / (1024 ** 3) : '',
    enabled: item.enabled !== false
  } : emptySubscriptionForm())
  subscriptionEditing.value = true
}

const saveSubscription = async () => {
  if (!subscriptionForm.name.trim()) {
    Message.warning(t('owner-subscription-name-required'))
    return
  }
  subscriptionSaving.value = true
  try {
    await api('/api/owner/subscriptions', {
      method: 'POST',
      data: {
        action: 'save',
        subscription: {
          id: subscriptionForm.id,
          name: subscriptionForm.name.trim(),
          regions: splitSubscriptionValues(subscriptionForm.regions),
          tags: splitSubscriptionValues(subscriptionForm.tags),
          max_price: Math.max(0, Number(subscriptionForm.max_price) || 0),
          currency: subscriptionForm.currency,
          min_memory: Math.max(0, Math.round((Number(subscriptionForm.min_memory_gb) || 0) * (1024 ** 3))),
          enabled: !!subscriptionForm.enabled
        }
      }
    })
    Message.success(t('owner-subscription-saved'))
    resetSubscriptionForm()
    await loadSubscriptions()
  } catch (error) {
    Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('owner-subscription-save-fail'))
  } finally {
    subscriptionSaving.value = false
  }
}

const toggleSubscription = async (item) => {
  subscriptionSaving.value = true
  try {
    await api('/api/owner/subscriptions', {
      method: 'POST',
      data: { action: 'save', subscription: { ...item, enabled: !item.enabled } }
    })
    await loadSubscriptions()
  } catch {
    Message.error(t('owner-subscription-save-fail'))
  } finally {
    subscriptionSaving.value = false
  }
}

const deleteSubscription = async (item) => {
  if (!confirm(t('owner-subscription-delete-confirm', { name: item.name }))) return
  try {
    await api('/api/owner/subscriptions', { method: 'POST', data: { action: 'delete', id: item.id } })
    Message.success(t('owner-subscription-deleted'))
    if (subscriptionForm.id === item.id) resetSubscriptionForm()
    await loadSubscriptions()
  } catch {
    Message.error(t('owner-subscription-save-fail'))
  }
}

const subscriptionSummary = (item) => {
  const parts = []
  if (item.regions?.length) parts.push(t('owner-subscription-regions-value', { value: item.regions.join(t('common-list-sep')) }))
  if (Number(item.max_price) > 0) parts.push(t('owner-subscription-price-value', { currency: item.currency || 'CNY', price: item.max_price }))
  if (Number(item.min_memory) > 0) parts.push(t('owner-subscription-memory-value', { memory: formatBytes(item.min_memory) }))
  return parts.join(' · ') || t('owner-subscription-any-condition')
}

const appealFor = (reportId) => marketAppeals.value.find((item) => item.report_id === reportId)
const openAppeal = (report) => {
  appealForm.report_id = report.id
  appealForm.message = ''
}
const closeAppeal = (force = false) => {
  if (force || !appealSaving.value) Object.assign(appealForm, { report_id: '', message: '' })
}
const submitAppeal = async () => {
  if (appealForm.message.trim().length < 10) {
    Message.warning(t('owner-appeal-min'))
    return
  }
  appealSaving.value = true
  try {
    await api('/api/owner/market-appeals', { method: 'POST', data: { report_id: appealForm.report_id, message: appealForm.message.trim() } })
    Message.success(t('owner-appeal-success'))
    closeAppeal(true)
    await loadMarketAppeals()
  } catch (error) {
    Message.error(typeof error?.response?.data === 'string' ? error.response.data : t('owner-appeal-fail'))
  } finally {
    appealSaving.value = false
  }
}

const toggleSale = async (item) => {
  try {
    await api('/api/owner/nodes/toggle', {
      method: 'POST',
      data: { node_id: item.node_id, for_sale: !item.for_sale }
    })
    Message.success(item.for_sale ? t('owner-unlisted') : t('owner-listed'))
    await loadNodes()
    if (editing.value?.node_id === item.node_id) {
      editForm.for_sale = !item.for_sale
    }
  } catch {
    Message.error(t('owner-op-fail'))
  }
}

const openEdit = (item) => {
  editing.value = item
  editForm.node_id = item.node_id
  editForm.display_name = item.display_name || ''
  editForm.region = item.region || ''
  editForm.listing_type = item.listing_type || 'rent'
  editForm.contact = item.contact || ''
  editForm.description = item.description || ''
  editForm.specs = item.specs || ''
  editForm.price = item.price || ''
	const billing = billingOf(item)
	editForm.price_amount = billing.structured ? billing.amount : ''
	editForm.price_currency = billing.currency || 'USD'
	editForm.billing_cycle = billing.cycle || 'monthly'
  editForm.due_date = item.due_time ? new Date(Number(item.due_time)).toISOString().slice(0, 10) : ''
  editForm.for_sale = !!item.for_sale
}

const cancelEdit = () => {
  editing.value = null
}

const saveEdit = async () => {
  if (!editForm.node_id) return
  if (!editForm.display_name.trim() || !(Number(editForm.price_amount) > 0) || !editForm.contact.trim()) {
    Message.warning(t('owner-required-tip'))
    return
  }
  saving.value = true
  try {
    await api('/api/owner/nodes/update', {
      method: 'POST',
      data: {
        node_id: editForm.node_id,
        display_name: editForm.display_name.trim(),
        region: editForm.region.trim(),
        listing_type: editForm.listing_type,
        contact: editForm.contact.trim(),
        description: editForm.description,
        specs: editForm.specs,
		price: `${editForm.price_currency} ${editForm.price_amount}`,
		price_amount: Number(editForm.price_amount),
		price_currency: editForm.price_currency,
		billing_cycle: editForm.billing_cycle,
        due_date: editForm.due_date,
        for_sale: !!editForm.for_sale
      }
    })
    Message.success(t('owner-saved'))
    await loadNodes()
    const refreshed = nodes.value.find((n) => n.node_id === editForm.node_id)
    if (refreshed) openEdit(refreshed)
  } catch (e) {
    const msg = e?.response?.data || e?.message || t('owner-save-fail')
    Message.error(typeof msg === 'string' ? msg : t('owner-save-fail'))
  } finally {
    saving.value = false
  }
}

const showInstall = async (item, reset = false) => {
  try {
    const res = await api('/api/owner/nodes/reset-token', {
      method: 'POST',
      data: { node_id: item.node_id, reset }
    })
    installBox.value = res.data
    Message.success(reset ? t('owner-token-reset-ok') : t('owner-install-fetched'))
  } catch (e) {
    const msg = e?.response?.data || t('owner-fetch-fail')
    Message.error(typeof msg === 'string' ? msg : t('owner-fetch-fail'))
  }
}

const removeNode = async (item) => {
  if (!confirm(t('owner-delete-confirm', { name: item.display_name || item.node_id }))) return
  try {
    await api('/api/owner/nodes/delete', {
      method: 'POST',
      data: { node_id: item.node_id }
    })
    Message.success(t('owner-deleted'))
    if (editing.value?.node_id === item.node_id) editing.value = null
    await loadNodes()
  } catch {
    Message.error(t('owner-delete-fail'))
  }
}

// 注：复制逻辑已收口到公共组件 CopyCommandBox，本文件不再有 copyText

onMounted(bootstrap)
onUnmounted(() => setLoginLock(false))
</script>

<template>
  <div class="owner-page" :class="{ 'is-auth-screen': !loading && !authenticated }">
    <EmptyState v-if="loading" loading />

    <!-- Full-viewport centered premium auth (no page scroll; blank clicks do NOT navigate away) -->
    <div v-else-if="!authenticated" class="owner-auth-screen">
      <div class="owner-auth-screen__card glow-card">
        <div class="owner-auth-screen__toolbar">
          <BackButton @click="emit('navigate', 'market')">{{ t('submit-back-market') }}</BackButton>
        </div>
        <PremiumAuth
          initial-mode="login"
          :loading="loggingIn"
          :error-message="loginError"
          @login="handleLogin"
          @go-submit="emit('navigate', 'submit')"
        />
      </div>
    </div>

    <div v-else class="owner-page__body">
      <header class="owner-page__head">
        <BackButton @click="emit('navigate', 'market')">{{ t('submit-back-market') }}</BackButton>
        <h1>{{ t('owner-title') }}</h1>
      </header>

      <div class="owner-page__bar">
        <span>{{ me?.email }}</span>
        <div class="owner-page__bar-actions">
          <BaseButton size="sm" @click="emit('navigate', 'submit')">{{ t('owner-continue-submit') }}</BaseButton>
          <BaseButton size="sm" @click="logout">{{ t('owner-logout') }}</BaseButton>
        </div>
      </div>

      <section class="subscription-section">
        <div class="subscription-section__head">
          <div>
            <h2>{{ t('owner-subscriptions-title') }}</h2>
            <p>{{ t('owner-subscriptions-subtitle') }}</p>
          </div>
          <BaseButton size="sm" variant="primary" @click="openSubscription()">{{ t('owner-subscription-add') }}</BaseButton>
        </div>

        <form v-if="subscriptionEditing" class="subscription-form" @submit.prevent="saveSubscription">
          <div class="subscription-form__head">
            <strong>{{ t(subscriptionForm.id ? 'owner-subscription-edit' : 'owner-subscription-create') }}</strong>
            <BaseButton variant="text" size="sm" @click="resetSubscriptionForm">{{ t('common-close') }}</BaseButton>
          </div>
          <div class="subscription-form__grid">
            <label>{{ t('owner-subscription-name') }}</label>
            <BaseInput v-model="subscriptionForm.name" maxlength="64" :placeholder="t('owner-subscription-name-placeholder')" required />
            <label>{{ t('owner-subscription-regions') }}</label>
            <BaseInput v-model="subscriptionForm.regions" maxlength="300" :placeholder="t('owner-subscription-regions-placeholder')" />
            <label>{{ t('owner-subscription-tags') }}</label>
            <BaseInput v-model="subscriptionForm.tags" maxlength="300" :placeholder="t('owner-subscription-tags-placeholder')" />
            <label>{{ t('owner-subscription-max-price') }}</label>
            <div class="subscription-form__split">
              <BaseInput v-model="subscriptionForm.max_price" type="number" min="0" step="0.01" :placeholder="t('owner-subscription-unlimited')" />
              <BaseInput as="select" v-model="subscriptionForm.currency" :aria-label="t('owner-subscription-currency')">
                <option value="CNY">CNY</option><option value="USD">USD</option><option value="HKD">HKD</option><option value="EUR">EUR</option><option value="JPY">JPY</option>
              </BaseInput>
            </div>
            <label>{{ t('owner-subscription-min-memory') }}</label>
            <BaseInput v-model="subscriptionForm.min_memory_gb" type="number" min="0" step="0.5" :placeholder="t('owner-subscription-unlimited')" />
            <label class="subscription-form__check"><input v-model="subscriptionForm.enabled" type="checkbox" /> {{ t('owner-subscription-enabled') }}</label>
          </div>
          <div class="subscription-form__actions">
            <BaseButton type="submit" size="sm" variant="primary" :disabled="subscriptionSaving">{{ subscriptionSaving ? t('common-saving') : t('common-save') }}</BaseButton>
            <BaseButton size="sm" @click="resetSubscriptionForm">{{ t('common-cancel') }}</BaseButton>
          </div>
        </form>

        <EmptyState v-if="subscriptionsLoading" loading />
        <div v-else-if="subscriptions.length" class="subscription-list">
          <article v-for="item in subscriptions" :key="item.id" class="subscription-item" :class="{ 'is-disabled': !item.enabled }">
            <div class="subscription-item__state" :class="item.enabled ? 'is-enabled' : 'is-disabled'" aria-hidden="true"></div>
            <div class="subscription-item__content">
              <div><strong>{{ item.name }}</strong><span>{{ t(item.enabled ? 'owner-subscription-active' : 'owner-subscription-paused') }}</span></div>
              <p>{{ subscriptionSummary(item) }}</p>
              <small v-if="item.tags?.length">{{ t('owner-subscription-tags-value', { value: item.tags.join(t('common-list-sep')) }) }}</small>
            </div>
            <div class="subscription-item__actions">
              <BaseButton size="sm" @click="openSubscription(item)">{{ t('owner-edit') }}</BaseButton>
              <BaseButton size="sm" :disabled="subscriptionSaving" @click="toggleSubscription(item)">{{ t(item.enabled ? 'owner-subscription-pause' : 'owner-subscription-resume') }}</BaseButton>
              <BaseButton size="sm" variant="danger" @click="deleteSubscription(item)">{{ t('owner-subscription-delete') }}</BaseButton>
            </div>
          </article>
        </div>
        <p v-else class="subscription-empty">{{ t('owner-subscriptions-empty') }}</p>
      </section>

      <section v-if="marketReports.length" class="owner-reports">
        <div class="owner-reports__head"><div><h2>{{ t('owner-reports-title') }}</h2><p>{{ t('owner-reports-subtitle') }}</p></div><span>{{ marketReports.length }}</span></div>
        <article v-for="report in marketReports" :key="report.id" class="owner-report-item">
          <div class="owner-report-item__head"><strong>{{ report.listing_node_id }}</strong><span :class="`is-${report.status}`">{{ t(`owner-report-status-${report.status}`) }}</span></div>
          <small>{{ t(`market-report-${report.category}`) }}</small>
          <p>{{ report.message }}</p>
          <p v-if="report.resolution" class="owner-report-item__resolution">{{ t('owner-report-resolution', { value: report.resolution }) }}</p>
          <div v-if="appealFor(report.id)" class="owner-report-item__appeal">
            <strong>{{ t(`owner-appeal-status-${appealFor(report.id).status}`) }}</strong><span>{{ appealFor(report.id).message }}</span>
            <small v-if="appealFor(report.id).resolution">{{ appealFor(report.id).resolution }}</small>
          </div>
          <BaseButton v-else size="sm" @click="openAppeal(report)">{{ t('owner-appeal-action') }}</BaseButton>
        </article>
      </section>

      <form v-if="appealForm.report_id" class="owner-appeal-form" @submit.prevent="submitAppeal">
        <div><strong>{{ t('owner-appeal-title') }}</strong><BaseButton variant="text" size="sm" @click="closeAppeal">{{ t('common-close') }}</BaseButton></div>
        <BaseInput v-model="appealForm.message" as="textarea" maxlength="1000" rows="4" :placeholder="t('owner-appeal-placeholder')" />
        <BaseButton type="submit" variant="primary" size="sm" :disabled="appealSaving">{{ appealSaving ? t('common-saving') : t('owner-appeal-submit') }}</BaseButton>
      </form>

      <div v-if="installBox" class="install-box">
        <div class="install-box__head">
          <strong>{{ t('owner-install-title', { nodeId: installBox.node_id }) }}</strong>
          <BaseButton variant="text" size="sm" @click="installBox = null">{{ t('common-close') }}</BaseButton>
        </div>
        <label>Linux</label>
        <CopyCommandBox :command="installBox.linux" />
        <label>Windows</label>
        <CopyCommandBox :command="installBox.windows" />
      </div>

      <form v-if="editing" class="edit-box" @submit.prevent="saveEdit">
        <div class="edit-box__head">
          <strong>{{ t('owner-edit-title', { nodeId: editForm.node_id }) }}</strong>
          <BaseButton variant="text" size="sm" @click="cancelEdit">{{ t('common-close') }}</BaseButton>
        </div>
        <div class="edit-grid">
          <label>{{ t('owner-field-name') }}</label>
          <BaseInput v-model="editForm.display_name" type="text" maxlength="64" required />
          <label>{{ t('owner-field-region') }}</label>
          <BaseInput v-model="editForm.region" type="text" maxlength="32" :placeholder="t('owner-region-placeholder')" />
          <label>{{ t('owner-field-specs') }}</label>
          <BaseInput v-model="editForm.specs" type="text" maxlength="200" />
          <label>{{ t('owner-field-price') }}</label>
          <BillingFields v-model:amount="editForm.price_amount" v-model:currency="editForm.price_currency" v-model:cycle="editForm.billing_cycle" required />
          <label>{{ t('owner-field-type') }}</label>
          <BaseInput as="select" v-model="editForm.listing_type">
            <option value="rent">{{ t('market-type-rent') }}</option>
            <option value="sale">{{ t('market-type-sale') }}</option>
            <option value="transfer">{{ t('market-type-transfer') }}</option>
          </BaseInput>
          <label>{{ t('owner-field-contact') }}</label>
          <BaseInput v-model="editForm.contact" type="text" maxlength="120" required />
          <label>{{ t('owner-field-desc') }}</label>
          <BaseInput as="textarea" v-model="editForm.description" rows="1" maxlength="500" autogrow :max-rows="5" />
          <label>{{ t('owner-field-due') }}</label>
          <BaseDatePicker v-model="editForm.due_date" />
          <label class="check"><input v-model="editForm.for_sale" type="checkbox" /> {{ t('owner-for-sale') }}</label>
        </div>
        <div class="edit-box__actions">
          <BaseButton variant="primary" size="sm" type="submit" :disabled="saving">
            {{ saving ? t('common-saving') : t('common-save') }}
          </BaseButton>
          <BaseButton size="sm" @click="cancelEdit">{{ t('common-cancel') }}</BaseButton>
        </div>
      </form>

      <EmptyState v-if="!nodes.length" :text="t('owner-empty')" />
      <div v-else class="owner-page__list">
        <div v-for="item in nodes" :key="item.node_id" class="owner-item">
          <div class="owner-moderation" :class="`is-${item.moderation_status || 'approved'}`">
            <strong>{{ t(`owner-moderation-${item.moderation_status || 'approved'}`) }}</strong>
            <span v-if="item.moderation_status === 'rejected' && item.rejection_reason">{{ t('owner-rejection-reason', { reason: item.rejection_reason }) }}</span>
          </div>
          <ListingCard :item="item" :market-actions="false" />
          <div class="owner-item__actions">
            <BaseButton size="sm" @click="openEdit(item)">{{ t('owner-edit') }}</BaseButton>
            <BaseButton size="sm" @click="toggleSale(item)">
              {{ item.for_sale ? t('owner-unlist') : t('owner-relist') }}
            </BaseButton>
            <BaseButton size="sm" @click="showInstall(item, false)">{{ t('owner-install-cmd') }}</BaseButton>
            <BaseButton size="sm" @click="showInstall(item, true)">{{ t('owner-reset-token') }}</BaseButton>
            <BaseButton variant="danger" size="sm" @click="removeNode(item)">{{ t('owner-delete') }}</BaseButton>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.owner-page {
  max-width: 900px;
  margin: 0 auto;
  padding: 24px 16px 48px;
}

.owner-page.is-auth-screen {
  max-width: none;
  margin: 0;
  padding: 0;
}

/* Fixed viewport center — no page scroll, transparent so the market page shows through */
.owner-auth-screen {
  position: fixed;
  inset: 88px 0 0 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  overflow: hidden;
}

.owner-auth-screen__card {
  width: min(420px, 100%);
  border-radius: 16px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-bg-2, #fff);
  box-shadow:
    0 1px 2px rgba(15, 23, 42, 0.04),
    0 18px 48px rgba(15, 23, 42, 0.18);
  max-height: calc(100vh - 120px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

.owner-auth-screen__toolbar {
  display: flex;
  align-items: center;
  padding: 14px 16px 6px;
}

/* The auth card is a scroll container: keep the glow ring inside the edge,
   otherwise the 1px outset pseudo-element creates phantom scrollbars. */
.owner-auth-screen__card.glow-card::after {
  inset: 0;
}

/* 返回按钮已收口到公共组件 BackButton，旧的 .auth-back 样式移除 */

.owner-page__head h1 {
  margin: 8px 0 0;
  font-size: 22px;
}

/* 按钮已收口到公共组件 BaseButton，旧的 .link / .btn / .btn-primary / .btn-danger 移除 */

.owner-page__bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin: 12px 0 16px;
}

.owner-page__bar-actions {
  display: flex;
  gap: 8px;
}

.subscription-section { margin: 4px 0 24px; padding: 20px 0; border-block: 1px solid var(--color-border-2, #e5e6eb); }
.subscription-section__head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.subscription-section__head h2 { margin: 0; font-size: 16px; }
.subscription-section__head p { margin: 5px 0 0; color: var(--color-text-3, #86909c); font-size: 12px; }
.subscription-form { display: flex; margin-top: 14px; padding: 14px; flex-direction: column; gap: 10px; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 8px; background: var(--color-fill-1, #f7f8fa); }
.subscription-form__head { display: flex; align-items: center; justify-content: space-between; font-size: 13px; }
.subscription-form__grid { display: grid; grid-template-columns: 116px minmax(0, 1fr); align-items: center; gap: 8px 10px; }
.subscription-form__grid > label { color: var(--color-text-2, #4e5969); font-size: 12px; }
.subscription-form__split { display: grid; grid-template-columns: minmax(0, 1fr) 104px; gap: 8px; }
.subscription-form__check { display: flex; grid-column: 1 / -1; align-items: center; gap: 7px; }
.subscription-form__actions { display: flex; gap: 8px; }
.subscription-list { margin-top: 14px; border-top: 1px solid var(--color-border-2, #e5e6eb); }
.subscription-item { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; min-height: 76px; padding: 10px 4px; align-items: center; gap: 10px; border-bottom: 1px solid var(--color-border-2, #e5e6eb); }
.subscription-item.is-disabled { opacity: .68; }
.subscription-item__state { width: 8px; height: 8px; border-radius: 50%; }
.subscription-item__state.is-enabled { background: #00b42a; }
.subscription-item__state.is-disabled { background: #c9cdd4; }
.subscription-item__content { display: grid; min-width: 0; gap: 4px; }
.subscription-item__content > div { display: flex; min-width: 0; align-items: center; gap: 7px; }
.subscription-item__content strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.subscription-item__content span { padding: 1px 5px; border-radius: 4px; background: var(--color-fill-2, #f2f3f5); color: var(--color-text-3, #86909c); font-size: 9px; }
.subscription-item__content p, .subscription-item__content small { overflow: hidden; margin: 0; color: var(--color-text-3, #86909c); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.subscription-item__actions { display: flex; gap: 6px; }
.subscription-empty { margin: 14px 0 0; padding: 18px 0 0; border-top: 1px solid var(--color-border-2, #e5e6eb); color: var(--color-text-3, #86909c); font-size: 12px; text-align: center; }
.owner-reports{margin:0 0 24px;padding:20px 0;border-bottom:1px solid var(--color-border-2,#e5e6eb)}.owner-reports__head{display:flex;align-items:flex-start;justify-content:space-between}.owner-reports__head h2{margin:0;font-size:16px}.owner-reports__head p{margin:5px 0 12px;color:var(--color-text-3,#86909c);font-size:12px}.owner-reports__head>span{font-size:12px}.owner-report-item{display:grid;padding:13px 4px;gap:7px;border-top:1px solid var(--color-border-2,#e5e6eb)}.owner-report-item__head{display:flex;align-items:center;justify-content:space-between;gap:12px}.owner-report-item__head strong{font-size:13px}.owner-report-item__head span{padding:2px 6px;border-radius:4px;background:var(--color-fill-2,#f2f3f5);font-size:10px}.owner-report-item__head span.is-resolved{color:#008f24}.owner-report-item__head span.is-rejected{color:#86909c}.owner-report-item>small{color:#ff7d00;font-size:10px}.owner-report-item>p{margin:0;color:var(--color-text-2,#4e5969);font-size:12px;line-height:1.6}.owner-report-item__resolution{color:#008f24!important}.owner-report-item__appeal{display:grid;padding:9px 10px;gap:4px;border-left:2px solid #165dff;background:var(--color-fill-1,#f7f8fa);font-size:11px}.owner-report-item__appeal small{color:var(--color-text-3,#86909c)}.owner-appeal-form{display:grid;margin:-10px 0 24px;padding:14px;gap:10px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px}.owner-appeal-form>div{display:flex;align-items:center;justify-content:space-between}.owner-appeal-form>button{justify-self:start}

.owner-page__list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.owner-item__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.owner-moderation{display:flex;margin-bottom:8px;padding:8px 10px;align-items:center;gap:8px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-fill-1,#f7f8fa);color:var(--color-text-2,#4e5969);font-size:12px}.owner-moderation strong{flex:none}.owner-moderation span{min-width:0;overflow-wrap:anywhere}.owner-moderation.is-approved{border-color:rgba(0,180,42,.22);background:rgba(0,180,42,.06);color:#008f24}.owner-moderation.is-pending{border-color:rgba(255,125,0,.24);background:rgba(255,125,0,.07);color:#b35400}.owner-moderation.is-rejected{border-color:rgba(208,48,80,.22);background:rgba(208,48,80,.06);color:#b42342}

/* 空态/加载态已收口到公共组件 EmptyState，旧的 .owner-page__empty 移除 */

.install-box,
.edit-box {
  margin-bottom: 16px;
  padding: 12px;
  border-radius: 12px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.install-box__head,
.edit-box__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

/* 表单控件与命令复制盒已收口到 BaseInput / CopyCommandBox，旧的 .edit-box input、.cmd-box 样式移除 */

.edit-grid {
  display: grid;
  grid-template-columns: 100px 1fr;
  gap: 8px 10px;
  align-items: center;
}

.edit-grid label {
  font-size: 13px;
  color: var(--color-text-2, #4e5969);
}

.edit-grid label.check {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 8px;
}

.edit-box__actions {
  display: flex;
  gap: 8px;
  margin-top: 4px;
}

@media (max-width: 640px) {
  .owner-auth-screen {
    inset: 72px 0 0 0;
  }

  .edit-grid {
    grid-template-columns: 1fr;
  }

  .owner-page__bar {
    flex-direction: column;
    align-items: flex-start;
  }

  .subscription-section__head { align-items: center; }
  .subscription-form__grid { grid-template-columns: 1fr; }
  .subscription-form__check { grid-column: auto; }
  .subscription-item { grid-template-columns: auto minmax(0, 1fr); align-items: start; }
  .subscription-item__state { margin-top: 5px; }
  .subscription-item__actions { grid-column: 2; flex-wrap: wrap; }
}

body[arco-theme='dark'] .owner-auth-screen__card {
  background: #1f1f20;
  border-color: rgba(255, 255, 255, 0.1);
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.45);
}
</style>

<style>
/* Global lock while owner login screen is open */
body.owner-login-lock {
  overflow: hidden !important;
}

body.owner-login-lock .site-footer {
  display: none !important;
}
</style>
