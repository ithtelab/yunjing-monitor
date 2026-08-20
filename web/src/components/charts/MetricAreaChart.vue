<script setup>
import Highcharts from 'highcharts'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { configureHighcharts } from '@/utils/highcharts'
import { formatBytes } from '@/utils/utils'

const props = defineProps({
  title: { type: String, required: true },
  data: { type: Array, default: () => [] },
  live: { type: Boolean, default: true },
  color: { type: String, default: '#1769e0' },
  maximum: { type: Number, default: undefined },
  dark: { type: Boolean, default: false },
  valueType: {
    type: String,
    default: 'percent',
    validator: (value) => ['percent', 'bytes', 'rate'].includes(value),
  },
})

const container = ref(null)
let instance = null

const visiblePoints = (points) => {
  const values = Array.isArray(points) ? points : []
  return props.live ? values.slice(-60) : values
}

const metricText = (value, compact = false) => {
  const numeric = Number(value) || 0
  if (props.valueType === 'percent') return `${numeric.toFixed(compact ? 0 : 2)}%`
  const bytes = formatBytes(numeric, compact ? 0 : 2)
  return props.valueType === 'rate' ? `${bytes}/s` : bytes
}

const timestampText = (timestamp) => new Intl.DateTimeFormat(undefined, {
  month: props.live ? undefined : '2-digit',
  day: props.live ? undefined : '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: props.live ? '2-digit' : undefined,
  hour12: false,
}).format(new Date(timestamp))

const buildOptions = () => ({
  chart: {
    animation: false,
    backgroundColor: 'transparent',
    renderTo: container.value,
    spacing: [4, 2, 2, 2],
    type: 'areaspline',
  },
  credits: { enabled: false },
  legend: { enabled: false },
  title: { text: undefined },
  xAxis: {
    labels: { style: { color: props.dark ? '#8f98a8' : '#748095' } },
    lineColor: props.dark ? 'rgba(255, 255, 255, .14)' : 'rgba(74, 89, 110, .18)',
    tickColor: props.dark ? 'rgba(255, 255, 255, .14)' : 'rgba(74, 89, 110, .18)',
    title: { text: undefined },
    type: 'datetime',
  },
  yAxis: {
    gridLineColor: props.dark ? 'rgba(255, 255, 255, .08)' : 'rgba(74, 89, 110, .1)',
    max: props.maximum,
    min: 0,
    startOnTick: true,
    title: { text: undefined },
    labels: {
      style: { color: props.dark ? '#8f98a8' : '#748095' },
      formatter() {
        return metricText(this.value, true)
      },
    },
  },
  tooltip: {
    backgroundColor: props.dark ? 'rgba(20, 22, 26, .96)' : 'rgba(255, 255, 255, .98)',
    borderColor: props.dark ? 'rgba(255, 255, 255, .15)' : 'rgba(74, 89, 110, .16)',
    borderRadius: 9,
    borderWidth: 1,
    style: { color: props.dark ? '#f5f7fa' : '#172033' },
    formatter() {
      return `<span style="font-size:12px;font-weight:500">${timestampText(this.x)}</span><br>`
        + `<span style="font-size:14px;font-weight:700">${props.title}: ${metricText(this.y)}</span>`
    },
  },
  plotOptions: {
    series: {
      animation: false,
      lineWidth: 2,
      marker: { enabled: false },
      states: { inactive: { opacity: 1 } },
      turboThreshold: 0,
    },
  },
  series: [{
    color: props.color,
    data: visiblePoints(props.data),
    fillColor: `${props.color}20`,
    threshold: 0,
  }],
})

const replaceData = () => {
  if (!instance) return
  instance.series[0].setData(visiblePoints(props.data), true, false, false)
}

const applyTheme = () => {
  if (!instance) return
  const options = buildOptions()
  instance.update({
    xAxis: options.xAxis,
    yAxis: options.yAxis,
    tooltip: options.tooltip,
  }, true, false, false)
}

onMounted(() => {
  configureHighcharts(Highcharts)
  instance = Highcharts.chart(buildOptions())
})

onBeforeUnmount(() => {
  instance?.destroy()
  instance = null
})

watch(() => [props.data, props.live], replaceData, { deep: true })
watch(() => props.maximum, (maximum) => instance?.yAxis[0].update({ max: maximum }, true))
watch(() => props.dark, applyTheme)
</script>

<template>
  <section class="metric-chart" :class="{ 'is-dark': dark }" :aria-label="title">
    <h3>{{ title }}</h3>
    <div ref="container" class="metric-chart__canvas"></div>
  </section>
</template>

<style scoped>
.metric-chart h3 {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 700;
}

.metric-chart.is-dark h3 {
  color: #f4f6fa;
}

.metric-chart__canvas {
  width: 100%;
  height: 150px;
}
</style>
