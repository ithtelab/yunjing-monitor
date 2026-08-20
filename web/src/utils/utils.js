import i18n from '@/locales'

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
const BIT_UNITS = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps', 'Pbps', 'Ebps', 'Zbps', 'Ybps']
const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

const translate = (key, values) => i18n.global.t(key, values)
const finiteNumber = (value) => Number.isFinite(Number(value)) ? Number(value) : 0

const scaleUnit = (rawValue, units) => {
  let value = finiteNumber(rawValue)
  let unitIndex = 0
  while (Math.abs(value) >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  return { value, unit: units[unitIndex] }
}

const timestampMilliseconds = (value) => {
  const numeric = Number(value)
  if (!Number.isFinite(numeric) || numeric <= 0) return null
  return numeric < 1e12 ? numeric * 1000 : numeric
}

const dateParts = (timestamp) => {
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return null
  const pad = (value) => String(value).padStart(2, '0')
  return {
    date: `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`,
    time: `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`,
  }
}

export const formatBytes = (bytes, precision = 2) => {
  const scaled = scaleUnit(bytes, BYTE_UNITS)
  const digits = Math.max(0, Math.min(4, finiteNumber(precision)))
  return `${scaled.value.toFixed(digits)} ${scaled.unit}`
}

export const formatBandwithBytes = (bytes, precision = 2) => {
  const scaled = scaleUnit(finiteNumber(bytes) * 8, BIT_UNITS)
  const digits = Math.max(0, Math.min(4, finiteNumber(precision)))
  return `${scaled.value.toFixed(digits)} ${scaled.unit}`
}

export const calculateRemainingDays = (expireTime) => {
  const expiresAt = timestampMilliseconds(expireTime)
  if (!expiresAt) return '-'
  return translate('time-days', { n: Math.ceil((expiresAt - Date.now()) / DAY) })
}

export const formatTime = (inputSeconds) => {
  const total = Math.max(0, Math.floor(finiteNumber(inputSeconds)))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  return [
    translate('time-hours', { n: hours }),
    translate('time-minutes', { n: minutes }),
    translate('time-seconds', { n: seconds }),
  ].join(' ')
}

export const formatTimeStamp = (timestamp) => {
  const milliseconds = timestampMilliseconds(timestamp)
  const parts = milliseconds ? dateParts(milliseconds) : null
  return parts ? `${parts.date} ${parts.time}` : '-'
}

export const formatDateStamp = (timestamp) => {
  const milliseconds = timestampMilliseconds(timestamp)
  return milliseconds ? (dateParts(milliseconds)?.date || '-') : '-'
}

export const formatUptime = (inputSeconds) => {
  let seconds = Math.max(0, Math.floor(finiteNumber(inputSeconds)))
  const days = Math.floor(seconds / 86400)
  seconds %= 86400
  const hours = Math.floor(seconds / 3600)
  seconds %= 3600
  const minutes = Math.floor(seconds / 60)
  seconds %= 60
  return `${days}d ${hours}h ${minutes}m ${seconds}s`
}

export const formatUptimeZh = (inputSeconds) => {
  const seconds = Math.max(0, Math.floor(finiteNumber(inputSeconds)))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days) return `${translate('time-days', { n: days })} ${translate('time-hours', { n: hours })}`
  if (hours) return `${translate('time-hours', { n: hours })} ${translate('time-minutes', { n: minutes })}`
  return translate('time-minutes', { n: minutes })
}

export const formatAgo = (timestamp) => {
  const milliseconds = timestampMilliseconds(timestamp)
  if (!milliseconds) return '-'
  const elapsed = Math.max(0, Date.now() - milliseconds)
  if (elapsed < MINUTE) return translate('time-ago-seconds', { n: Math.floor(elapsed / SECOND) })
  if (elapsed < HOUR) return translate('time-ago-minutes', { n: Math.floor(elapsed / MINUTE) })
  if (elapsed < DAY) return translate('time-ago-hours', { n: Math.floor(elapsed / HOUR) })
  return translate('time-ago-days', { n: Math.floor(elapsed / DAY) })
}
