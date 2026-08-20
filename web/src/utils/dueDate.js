const normalizedTimeZone = (value) => {
  const timeZone = String(value || '').trim()
  if (!timeZone) return undefined
  try {
    new Intl.DateTimeFormat('en-US', { timeZone }).format(0)
    return timeZone
  } catch {
    return undefined
  }
}

export const normalizeDueTimestamp = (value) => {
  const numeric = Number(value)
  if (!Number.isFinite(numeric) || numeric <= 0) return 0
  return numeric < 1000000000000 ? numeric * 1000 : numeric
}

const calendarOrdinal = (value, timeZone) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const zone = normalizedTimeZone(timeZone)
  if (!zone) return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / 86400000

  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: zone,
    year: 'numeric',
    month: 'numeric',
    day: 'numeric'
  }).formatToParts(date)
  const values = Object.fromEntries(parts.map((part) => [part.type, Number(part.value)]))
  return Date.UTC(values.year, values.month - 1, values.day) / 86400000
}

export const calendarDaysUntil = (dueValue, nowValue = Date.now(), timeZone = '') => {
  const due = normalizeDueTimestamp(dueValue)
  if (!due) return null
  const dueDay = calendarOrdinal(due, timeZone)
  const today = calendarOrdinal(nowValue, timeZone)
  if (dueDay === null || today === null) return null
  return dueDay - today
}
