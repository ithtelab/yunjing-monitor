const currencies = new Set(['CNY', 'USD', 'HKD', 'EUR', 'JPY'])
const cycles = new Set(['monthly', 'quarterly', 'semiannual', 'annual', 'one_time', 'custom'])

export const normalizeBillingCycle = (value) => {
  const cycle = String(value || '').trim().toLowerCase()
  if (['month', 'monthly', '月', '月付', '每月'].includes(cycle)) return 'monthly'
  if (['quarter', 'quarterly', '季', '季度', '季付'].includes(cycle)) return 'quarterly'
  if (['half-year', 'semiannual', 'semi-annual', '半年', '半年付'].includes(cycle)) return 'semiannual'
  if (['year', 'yearly', 'annual', '年', '年付', '每年'].includes(cycle)) return 'annual'
  if (['once', 'one-time', 'one_time', '一次', '一次性'].includes(cycle)) return 'one_time'
  if (['custom', '自定义'].includes(cycle)) return 'custom'
  return cycle
}

export const parseLegacyBilling = (price, legacyCycle = '') => {
  const raw = String(price || '').trim()
  const compact = raw.replaceAll(',', '')
  const upper = compact.toUpperCase()
  const amount = Number(compact.match(/[0-9]+(?:\.[0-9]+)?/)?.[0] || 0)
  if (!(amount > 0)) return { amount: 0, currency: '', cycle: 'custom', structured: false }

  let currency = ''
  if (upper.includes('HK$') || upper.includes('HKD')) currency = 'HKD'
  else if (upper.includes('US$') || upper.includes('USD') || compact.includes('$')) currency = 'USD'
  else if (upper.includes('JPY') || upper.includes('JP¥') || upper.includes('JP￥')) currency = 'JPY'
  else if (upper.includes('CNY') || upper.includes('RMB') || compact.includes('¥') || compact.includes('￥')) currency = 'CNY'
  else if (upper.includes('EUR') || compact.includes('€')) currency = 'EUR'
  if (!currency) return { amount: 0, currency: '', cycle: 'custom', structured: false }

  const cycleText = `${legacyCycle} ${raw}`.toLowerCase()
  let cycle = 'custom'
  if (cycleText.includes('半年') || cycleText.includes('semi')) cycle = 'semiannual'
  else if (cycleText.includes('季度') || cycleText.includes('季付') || cycleText.includes('quarter')) cycle = 'quarterly'
  else if (cycleText.includes('月') || cycleText.includes('month')) cycle = 'monthly'
  else if (cycleText.includes('年') || cycleText.includes('year') || cycleText.includes('annual')) cycle = 'annual'
  else if (cycleText.includes('一次') || cycleText.includes('one-time')) cycle = 'one_time'
  return { amount, currency, cycle, structured: true }
}

export const billingOf = (item = {}, legacyCycle = '') => {
  const amount = Number(item.price_amount || 0)
  const currency = String(item.price_currency || '').trim().toUpperCase()
  const cycle = normalizeBillingCycle(item.billing_cycle)
  if (amount > 0 && currencies.has(currency) && cycles.has(cycle)) {
    return { amount, currency, cycle, structured: true, legacy: false }
  }
  const parsed = parseLegacyBilling(item.price, legacyCycle || item.cycle)
  return { ...parsed, legacy: parsed.structured }
}

export const cnyValue = (amount, currency, exchange) => {
  const value = Number(amount)
  if (!(value >= 0)) return null
  const code = String(currency || '').toUpperCase()
  if (code === 'CNY') return value
  const rates = exchange?.rates || {}
  const cny = Number(rates.CNY || exchange?.rate || 0)
  const source = Number(rates[code] || (code === 'USD' ? 1 : 0))
  if (!(cny > 0) || !(source > 0)) return null
  return value / source * cny
}

export const monthlyCNY = (billing, exchange) => {
  if (!billing?.structured) return null
  const value = cnyValue(billing.amount, billing.currency, exchange)
  if (value === null) return null
  const divisors = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 }
  const divisor = divisors[billing.cycle]
  return divisor ? value / divisor : null
}

export const renewalCNY = (billing, exchange) => {
  if (!billing?.structured || !['monthly', 'quarterly', 'semiannual', 'annual'].includes(billing.cycle)) return null
  return cnyValue(billing.amount, billing.currency, exchange)
}

export const formatMoney = (value, currency = 'CNY', locale = 'zh-CN') => {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  try {
    return new Intl.NumberFormat(locale, {
      style: 'currency', currency, maximumFractionDigits: amount < 10 ? 2 : 0
    }).format(amount)
  } catch {
    return `${currency} ${amount.toFixed(2)}`
  }
}

export const normalizedPrice = (item, exchange) => {
  const billing = billingOf(item)
  if (!billing.structured) return Number.POSITIVE_INFINITY
  return cnyValue(billing.amount, billing.currency, exchange) ?? Number.POSITIVE_INFINITY
}
