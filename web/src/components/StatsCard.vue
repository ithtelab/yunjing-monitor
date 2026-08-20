<script setup>
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatBandwithBytes, formatBytes } from '@/utils/utils'

const props = defineProps({
  stats: {
    type: Object,
    default: () => ({
      total: 0,
      online: 0,
      offline: 0,
      traffic_up: 0,
      traffic_down: 0,
      bandwidth_up: 0,
      bandwidth_down: 0,
    }),
  },
  type: { type: String, default: 'all' },
  dark: { type: Boolean, default: false },
})

const emit = defineEmits(['select-metric'])
const changeType = inject('handleChangeType', () => {})
const { t } = useI18n()

const statusCards = computed(() => [
  { key: 'all', label: t('server-total'), value: props.stats.total || 0, symbol: '∑' },
  { key: 'online', label: t('server-online'), value: props.stats.online || 0, symbol: '✓' },
  { key: 'offline', label: t('server-offline'), value: props.stats.offline || 0, symbol: '!' },
])
</script>

<template>
  <section class="summary-strip" :class="{ 'is-dark': dark }" :aria-label="t('server-total')">
    <button
      v-for="card in statusCards"
      :key="card.key"
      type="button"
      class="summary-strip__status glow-card"
      :class="[`is-${card.key}`, { 'is-active': type === card.key }]"
      :aria-pressed="type === card.key"
      @click="changeType(card.key)"
    >
      <span class="summary-strip__head">
        <span class="summary-strip__label">{{ card.label }}</span>
        <i class="summary-strip__symbol" aria-hidden="true">{{ card.symbol }}</i>
      </span>
      <span class="summary-strip__value">
        <i class="summary-strip__dot" aria-hidden="true"></i>
        <strong>{{ card.value }}</strong>
        <small>{{ t('server-unit') }}</small>
      </span>
    </button>

    <button type="button" class="summary-strip__traffic glow-card" @click="emit('select-metric', 'traffic')">
      <span class="summary-strip__head">
        <span class="summary-strip__label">{{ t('stats-traffic-bandwidth') }}</span>
        <span class="summary-strip__detail">›</span>
      </span>
      <span class="summary-strip__traffic-groups">
        <span class="summary-strip__traffic-group">
          <small>{{ t('traffic-info') }}</small>
          <span><b class="is-upload"><icon-arrow-up aria-hidden="true" />{{ formatBytes(stats.traffic_up) }}</b><b class="is-download"><icon-arrow-down aria-hidden="true" />{{ formatBytes(stats.traffic_down) }}</b></span>
        </span>
        <span class="summary-strip__traffic-group">
          <small>{{ t('bandwidth-info') }}</small>
          <span><b><icon-up-circle aria-hidden="true" />{{ formatBandwithBytes(stats.bandwidth_up) }}</b><b><icon-down-circle aria-hidden="true" />{{ formatBandwithBytes(stats.bandwidth_down) }}</b></span>
        </span>
      </span>
    </button>
  </section>
</template>

<style scoped>
.summary-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) minmax(280px, 1.2fr);
  gap: 14px;
  margin: 24px 14px 0;
  --card-bg: linear-gradient(145deg, #ffffff 0%, #fbfcfe 100%);
  --card-border: rgba(26, 39, 58, .1);
  --card-text: #172033;
  --muted-text: #758197;
  --hover-shadow: 0 12px 28px rgba(24, 38, 60, .09);
}

.summary-strip button {
  position: relative;
  overflow: hidden;
  min-width: 0;
  min-height: 104px;
  padding: 16px 18px;
  border: 1px solid var(--card-border);
  border-radius: 14px;
  background: var(--card-bg);
  color: var(--card-text);
  cursor: pointer;
  font: inherit;
  text-align: left;
  box-shadow: 0 1px 2px rgba(20, 30, 45, .025);
  transition: border-color .18s ease, box-shadow .18s ease, transform .18s ease;
}

.summary-strip button::before {
  position: absolute;
  top: 0;
  right: 18px;
  left: 18px;
  height: 1px;
  background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--accent, #1769e0) 55%, transparent), transparent);
  content: '';
  opacity: .65;
}

.summary-strip button:hover {
  border-color: color-mix(in srgb, var(--accent, #1769e0) 35%, var(--card-border));
  box-shadow: var(--hover-shadow);
  transform: translateY(-2px);
}

.summary-strip button:focus-visible {
  outline: 2px solid #1769e0;
  outline-offset: 2px;
}

.summary-strip__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 13px;
}

.summary-strip__label {
  color: var(--muted-text);
  font-size: 12px;
  font-weight: 750;
  letter-spacing: .015em;
}

.summary-strip__symbol {
  display: grid;
  width: 23px;
  height: 23px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--accent) 22%, transparent);
  border-radius: 7px;
  background: color-mix(in srgb, var(--accent) 8%, transparent);
  color: var(--accent);
  font-size: 11px;
  font-style: normal;
  font-weight: 850;
}

.summary-strip__value {
  display: flex;
  align-items: center;
  gap: 7px;
}

.summary-strip__value strong {
  font-size: 27px;
  font-weight: 850;
  letter-spacing: -.04em;
  line-height: 1;
}

.summary-strip__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent) 10%, transparent);
}

.summary-strip__value small {
  align-self: flex-end;
  margin-bottom: 1px;
  color: var(--muted-text);
  font-size: 12px;
  font-weight: 650;
}

.summary-strip__status.is-all { --accent: #1769e0; }
.summary-strip__status.is-online { --accent: #079455; }
.summary-strip__status.is-offline { --accent: #d92d20; }
.summary-strip__traffic { --accent: #8263d7; }

.summary-strip__status.is-active {
  border-color: color-mix(in srgb, var(--accent) 62%, transparent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 8%, transparent);
}

.summary-strip__detail {
  color: var(--muted-text);
  font-size: 19px;
  line-height: 1;
}

.summary-strip__traffic-groups {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.summary-strip__traffic-group {
  min-width: 0;
}

.summary-strip__traffic-group > small {
  display: block;
  margin-bottom: 5px;
  color: var(--muted-text);
  font-size: 10px;
  font-weight: 650;
}

.summary-strip__traffic-group > span {
  display: flex;
  align-items: center;
  gap: 9px;
}

.summary-strip__traffic-group b {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  font-weight: 720;
  white-space: nowrap;
}

.summary-strip__traffic-group b.is-upload { color: #b66b29; }
.summary-strip__traffic-group b.is-download { color: #8653bb; }

.summary-strip__traffic-group svg {
  width: 13px;
  flex: none;
}

.summary-strip.is-dark {
  --card-bg: linear-gradient(145deg, #15171b 0%, #0f1114 100%);
  --card-border: rgba(255, 255, 255, .11);
  --card-text: #f4f6fa;
  --muted-text: #919aaa;
  --hover-shadow: 0 14px 34px rgba(0, 0, 0, .32);
}

.summary-strip.is-dark button::after {
  position: absolute;
  width: 90px;
  height: 90px;
  top: -55px;
  right: -35px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  content: '';
  filter: blur(20px);
  pointer-events: none;
}

@media (max-width: 768px) {
  .summary-strip {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
    margin: 12px 8px 0;
  }

  .summary-strip button {
    min-height: 78px;
    padding: 11px 10px;
    border-radius: 11px;
  }

  .summary-strip__head {
    margin-bottom: 8px;
  }

  .summary-strip__label {
    overflow: hidden;
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .summary-strip__symbol {
    display: none;
  }

  .summary-strip__value {
    gap: 6px;
  }

  .summary-strip__value strong {
    font-size: 21px;
  }

  .summary-strip__traffic {
    grid-column: 1 / -1;
  }

  .summary-strip__traffic-groups {
    gap: 8px;
  }
}

@media (max-width: 520px) {
  .summary-strip__traffic-group > span {
    flex-wrap: wrap;
    gap: 3px 9px;
  }
}
</style>
