<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseInput from './BaseInput.vue'

const { t } = useI18n()
const props = defineProps({
  amount: { type: [String, Number], default: '' },
  currency: { type: String, default: 'USD' },
  cycle: { type: String, default: 'monthly' },
  required: { type: Boolean, default: false }
})
const emit = defineEmits(['update:amount', 'update:currency', 'update:cycle'])

const amountModel = computed({ get: () => props.amount, set: (value) => emit('update:amount', value) })
const currencyModel = computed({ get: () => props.currency, set: (value) => emit('update:currency', value) })
const cycleModel = computed({ get: () => props.cycle, set: (value) => emit('update:cycle', value) })
</script>

<template>
  <div class="billing-fields">
    <div>
      <span>{{ t('billing-amount') }}</span>
      <BaseInput v-model="amountModel" type="number" min="0.01" max="1000000000" step="0.01" :required="required" inputmode="decimal" />
    </div>
    <div>
      <span>{{ t('billing-currency') }}</span>
      <BaseInput v-model="currencyModel" as="select" :required="required">
        <option value="CNY">CNY</option>
        <option value="USD">USD</option>
        <option value="HKD">HKD</option>
        <option value="EUR">EUR</option>
        <option value="JPY">JPY</option>
      </BaseInput>
    </div>
    <div>
      <span>{{ t('billing-cycle') }}</span>
      <BaseInput v-model="cycleModel" as="select" :required="required">
        <option value="monthly">{{ t('billing-monthly') }}</option>
        <option value="quarterly">{{ t('billing-quarterly') }}</option>
        <option value="semiannual">{{ t('billing-semiannual') }}</option>
        <option value="annual">{{ t('billing-annual') }}</option>
        <option value="one_time">{{ t('billing-one-time') }}</option>
        <option value="custom">{{ t('billing-custom') }}</option>
      </BaseInput>
    </div>
  </div>
</template>

<style scoped>
.billing-fields {
  display: grid;
  grid-template-columns: minmax(120px, 1.3fr) minmax(94px, .8fr) minmax(110px, 1fr);
  gap: 8px;
}
.billing-fields > div {
  min-width: 0;
}
.billing-fields span {
  display: block;
  margin-bottom: 5px;
  color: var(--color-text-3, #86909c);
  font-size: 11px;
}
@media (max-width: 520px) {
  .billing-fields { grid-template-columns: 1fr 1fr; }
  .billing-fields > div:first-child { grid-column: 1 / -1; }
}
</style>
