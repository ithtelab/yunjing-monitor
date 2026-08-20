<script setup>
import { useI18n } from 'vue-i18n'
import MetricAreaChart from '@/components/charts/MetricAreaChart.vue'

defineProps({
  data: { type: Array, default: () => [] },
  live: { type: Boolean, default: true },
  max: { type: Number, default: undefined },
  dark: { type: Boolean, default: false },
})

const { t } = useI18n()
</script>

<template>
  <MetricAreaChart :title="t('chart-memory')" value-type="bytes" color="#7158cf" :data="data" :live="live" :maximum="max" :dark="dark" />
</template>
