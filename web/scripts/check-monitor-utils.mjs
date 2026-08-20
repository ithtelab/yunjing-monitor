import assert from 'node:assert/strict'

import {
  compactPlatformName,
  getHostChartSeries,
  hostArea,
  normalizeMonitorHosts,
  regionFlag
} from '../src/utils/monitor.js'
import {
  DEFAULT_CHART_LOCALE,
  normalizeChartLocale
} from '../src/utils/chartLocale.js'
import { buildMarketFeed, normalizeAdLayout } from '../src/utils/marketAds.js'
import { billingOf, cnyValue, monthlyCNY, normalizedPrice, parseLegacyBilling } from '../src/utils/billing.js'

const charts = {}
const result = normalizeMonitorHosts([
  {
    Host: {
      Name: 'UK-node-1',
      CPU: 'not-an-array',
      MemTotal: '2048',
      LogicalCores: '4',
      GPUs: ['Test GPU', '']
    },
    State: {
      CPU: '42.5',
      MemUsed: '1024',
      DiskTotal: '4096',
      DiskUsed: '2048',
      NetInSpeed: 'invalid',
      NetOutSpeed: '512',
      NetInTransfer: '1024',
      NetOutTransfer: '2048',
      Disks: [
        {
          mount: '/',
          fs_type: 'ext4',
          total: '4096',
          used: '2048',
          used_percent: '50.25'
        }
      ]
    },
    TimeStamp: '95'
  },
  {
    Host: {
      Name: 'CN-pending'
    },
    TimeStamp: 0
  },
  {
    State: {
      CPU: 99
    }
  },
  null
], 100, 10, charts)

assert.deepEqual(result.areas, ['UK', 'CN'])
assert.equal(result.hosts.length, 2)
assert.equal(result.hosts[0].status, 1)
assert.equal(result.hosts[1].status, 0)
assert.equal(result.hosts[0].Host.MemTotal, 2048)
assert.deepEqual(result.hosts[0].Host.CPU, [])
assert.deepEqual(result.hosts[0].Host.GPUs, ['Test GPU'])
assert.equal(result.hosts[0].State.CPU, 42.5)
assert.equal(result.hosts[0].State.NetInSpeed, 0)
assert.equal(result.hosts[0].State.NetOutSpeed, 512)
assert.equal(result.hosts[0].State.Disks[0].used_percent, 50.25)
assert.equal(result.hosts[1].Host.Platform, 'unknown')
assert.deepEqual(result.hosts[1].Host.GPUs, [])
assert.equal(result.hosts[1].State.TrafficResetDay, 1)

assert.equal(charts['UK-node-1'].cpu.length, 1)
assert.deepEqual(charts['UK-node-1'].net_in, [[95000, 0]])
assert.deepEqual(charts['UK-node-1'].net_out, [[95000, 512]])
// Same TimeStamp must not append a second chart point (WS cache re-push).
normalizeMonitorHosts([
  {
    Host: { Name: 'UK-node-1', MemTotal: 2048 },
    State: { CPU: 42.5, MemUsed: 1024, NetInSpeed: 0, NetOutSpeed: 512 },
    TimeStamp: 95
  }
], 100, 10, charts)
assert.equal(charts['UK-node-1'].cpu.length, 1)
// Pending hosts (TimeStamp 0) must not create chart series noise.
assert.equal(charts['CN-pending'], undefined)
assert.deepEqual(getHostChartSeries(charts, 'missing-node'), {
  cpu: [],
  mem: [],
  net_in: [],
  net_out: []
})
assert.equal(regionFlag('UK-node-1'), '🇬🇧')
assert.equal(compactPlatformName('Microsoft Windows 11 专业版'), 'Windows 11 专业版')
assert.equal(compactPlatformName('Windows Server 2022 Datacenter'), 'Windows Server 2022 Datacenter')
assert.equal(compactPlatformName('Ubuntu 24.04 LTS'), 'Ubuntu 24.04 LTS')
assert.equal(hostArea({ Host: { Name: 'US-node-1' } }), 'US')
assert.equal(hostArea({ Host: { Name: 'ces' } }), '')

const emptyResult = normalizeMonitorHosts({ bad: 'shape' }, 100, 10, {})
assert.deepEqual(emptyResult, { areas: [], hosts: [] })

assert.equal(normalizeChartLocale(''), DEFAULT_CHART_LOCALE)
assert.equal(normalizeChartLocale('zh'), 'zh-CN')
assert.equal(normalizeChartLocale('en'), 'en-US')
assert.equal(normalizeChartLocale('en_US'), 'en-US')
assert.equal(normalizeChartLocale('not a locale'), DEFAULT_CHART_LOCALE)

const marketListings = [1, 2, 3, 4, 5, 6].map((n) => ({ node_id: `node-${n}` }))
const marketAds = [1, 2, 3].map((n) => ({ id: `ad_${n}`, position_mode: 'auto' }))
assert.deepEqual(normalizeAdLayout({}), {
  desktopInterval: 3, mobileInterval: 2, maxAds: 0, minServerGap: 1, allowConsecutive: false, conflictStrategy: 'shift'
})
assert.deepEqual(buildMarketFeed(marketListings, marketAds, {}, false).map((x) => x.key), [
  'listing:node-1', 'listing:node-2', 'listing:node-3', 'ad:ad_1', 'listing:node-4', 'listing:node-5', 'listing:node-6', 'ad:ad_2'
])
assert.deepEqual(buildMarketFeed(marketListings, marketAds, { mobile_interval: 2, min_server_gap: 0 }, true).map((x) => x.key), [
  'listing:node-1', 'listing:node-2', 'ad:ad_1', 'listing:node-3', 'listing:node-4', 'ad:ad_2', 'listing:node-5', 'listing:node-6', 'ad:ad_3'
])
const positionedAds = [
  { id: 'ad_start', position_mode: 'start' },
  { id: 'ad_after', position_mode: 'after', desktop_after: 2 },
  { id: 'ad_end', position_mode: 'end' }
]
assert.deepEqual(buildMarketFeed(marketListings.slice(0, 3), positionedAds, { min_server_gap: 0 }, false).map((x) => x.key), [
  'ad:ad_start', 'listing:node-1', 'listing:node-2', 'ad:ad_after', 'listing:node-3', 'ad:ad_end'
])

assert.deepEqual(parseLegacyBilling('$0.50/月'), { amount: 0.5, currency: 'USD', cycle: 'monthly', structured: true })
assert.deepEqual(parseLegacyBilling('¥99', '年'), { amount: 99, currency: 'CNY', cycle: 'annual', structured: true })
assert.deepEqual(parseLegacyBilling('JP¥980/月'), { amount: 980, currency: 'JPY', cycle: 'monthly', structured: true })
assert.equal(billingOf({ price_amount: 12, price_currency: 'USD', billing_cycle: 'monthly' }).legacy, false)
const exchange = { rate: 7.2, rates: { USD: 1, CNY: 7.2, HKD: 7.8 } }
assert.equal(cnyValue(1, 'USD', exchange), 7.2)
assert.equal(monthlyCNY(billingOf({ price: '$12/年' }), exchange), 7.2)
assert.equal(normalizedPrice({ price: '联系询价' }, exchange), Number.POSITIVE_INFINITY)
