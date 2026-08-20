const clampInt = (value, min, max, fallback) => {
  const n = Number.parseInt(value, 10)
  return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : fallback
}

const canUseBoundary = (boundary, used, minGap, allowConsecutive) => {
  if (allowConsecutive) return true
  return used.every((value) => Math.abs(value - boundary) > minGap)
}

const findBoundary = (desired, serverCount, used, settings) => {
  if (canUseBoundary(desired, used, settings.minServerGap, settings.allowConsecutive)) return desired
  if (settings.conflictStrategy === 'stack') return settings.allowConsecutive ? desired : null

  const candidates = []
  for (let offset = 1; offset <= serverCount + 1; offset += 1) {
    if (settings.conflictStrategy === 'rotate') {
      candidates.push((desired + offset) % (serverCount + 1))
    } else {
      if (desired + offset <= serverCount) candidates.push(desired + offset)
    }
  }
  return candidates.find((value) => canUseBoundary(value, used, settings.minServerGap, false)) ?? null
}

export const normalizeAdLayout = (value = {}) => ({
  desktopInterval: clampInt(value.desktop_interval, 1, 100, 3),
  mobileInterval: clampInt(value.mobile_interval, 1, 100, 2),
  maxAds: clampInt(value.max_ads, 0, 1000, 0),
  minServerGap: clampInt(value.min_server_gap, 0, 100, 1),
  allowConsecutive: Boolean(value.allow_consecutive),
  conflictStrategy: ['shift', 'rotate', 'stack'].includes(value.conflict_strategy) ? value.conflict_strategy : 'shift'
})

export const buildMarketFeed = (listings = [], advertisements = [], rawSettings = {}, mobile = false) => {
  const settings = normalizeAdLayout(rawSettings)
  const ads = settings.maxAds > 0 ? advertisements.slice(0, settings.maxAds) : advertisements.slice()
  const serverCount = listings.length
  const interval = mobile ? settings.mobileInterval : settings.desktopInterval
  const used = []
  const placed = []
  let automaticIndex = 0

  for (const ad of ads) {
    let desired
    switch (ad.position_mode) {
      case 'start': desired = 0; break
      case 'end': desired = serverCount; break
      case 'after':
      case 'exclusive': desired = clampInt(mobile ? ad.mobile_after : ad.desktop_after, 0, serverCount, 0); break
      default:
        automaticIndex += 1
        desired = Math.min(serverCount, interval * automaticIndex)
    }
    const boundary = findBoundary(desired, serverCount, used, settings)
    if (boundary === null) continue
    used.push(boundary)
    placed.push({ boundary, ad })
  }

  const byBoundary = new Map()
  for (const item of placed) {
    const current = byBoundary.get(item.boundary) || []
    current.push(item.ad)
    byBoundary.set(item.boundary, current)
  }

  const feed = []
  for (let boundary = 0; boundary <= serverCount; boundary += 1) {
    for (const ad of byBoundary.get(boundary) || []) feed.push({ kind: 'ad', key: `ad:${ad.id}`, item: ad })
    if (boundary < serverCount) feed.push({ kind: 'listing', key: `listing:${listings[boundary].node_id}`, item: listings[boundary] })
  }
  return feed
}
