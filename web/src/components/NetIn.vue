<script setup>
import { useI18n } from 'vue-i18n'
import MetricAreaChart from '@/components/charts/MetricAreaChart.vue'

defineProps({
  data: { type: Array, default: () => [] },
  live: { type: Boolean, default: true },
  dark: { type: Boolean, default: false },
})

const { t } = useI18n()
</script>

<template>
  <MetricAreaChart :title="t('chart-network-up')" value-type="rate" color="#c9762b" :data="data" :live="live" :dark="dark" />
</template>
