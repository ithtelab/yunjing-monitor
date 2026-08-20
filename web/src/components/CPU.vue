<script setup>
import MetricAreaChart from '@/components/charts/MetricAreaChart.vue'

defineProps({
  data: { type: Array, default: () => [] },
  live: { type: Boolean, default: true },
  dark: { type: Boolean, default: false },
})
</script>

<template>
  <MetricAreaChart title="CPU" value-type="percent" color="#1769e0" :data="data" :live="live" :dark="dark" />
</template>
