<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/ui/EmptyState.vue'
import RegionFlag from '@/components/ui/RegionFlag.vue'
import { hostArea } from '@/utils/monitor'

const props = defineProps({ hosts: { type: Array, default: () => [] } })
const emit = defineEmits(['inspect'])
const { t } = useI18n()

const groups = computed(() => {
  const result = new Map()
  props.hosts.forEach((item) => {
    const region = hostArea(item) || t('market-no-region')
    if (!result.has(region)) result.set(region, { region, nodes: [], online: 0 })
    const group = result.get(region)
    group.nodes.push(item)
    if (item.status) group.online += 1
  })
  return [...result.values()].sort((a, b) => b.nodes.length - a.nodes.length || a.region.localeCompare(b.region))
})
</script>

<template>
  <section class="region-distribution">
    <EmptyState v-if="!groups.length" :text="t('monitor-distribution-empty')" />
    <div v-else class="region-distribution__grid">
      <article v-for="group in groups" :key="group.region" class="region-group glow-card">
        <header><div><RegionFlag :region="group.region" /><strong>{{ group.region }}</strong></div><span>{{ t('monitor-distribution-online', { online: group.online, total: group.nodes.length }) }}</span></header>
        <div class="region-availability"><i :style="{ width: `${group.nodes.length ? group.online / group.nodes.length * 100 : 0}%` }"></i></div>
        <div class="region-node-list"><button v-for="item in group.nodes" :key="item.Host.Name" type="button" @click="emit('inspect', item)"><span :class="item.status ? 'is-online' : 'is-offline'"></span><strong>{{ item.Host.Name }}</strong><small>{{ t(item.status ? 'online' : 'offline') }}</small></button></div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.region-distribution{width:min(1120px,calc(100% - 28px));min-height:260px;margin:20px auto}.region-distribution__grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:12px}.region-group{display:grid;padding:14px;gap:11px;border:1px solid var(--color-border-2,#e5e6eb);border-radius:8px;background:var(--color-bg-2,#fff)}.region-group>header{display:flex;align-items:center;justify-content:space-between;gap:10px}.region-group>header>div{display:flex;min-width:0;align-items:center;gap:7px}.region-group header strong{overflow:hidden;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.region-group header span{color:var(--color-text-3,#86909c);font-size:9px}.region-availability{height:4px;overflow:hidden;border-radius:2px;background:var(--color-fill-3,#e5e6eb)}.region-availability i{display:block;height:100%;background:#00b42a}.region-node-list{display:grid}.region-node-list button{display:grid;grid-template-columns:auto minmax(0,1fr) auto;min-height:34px;padding:0 3px;align-items:center;gap:7px;border:0;border-top:1px solid var(--color-border-2,#e5e6eb);background:transparent;color:inherit;cursor:pointer;text-align:left}.region-node-list button>span{width:7px;height:7px;border-radius:50%}.region-node-list .is-online{background:#00b42a}.region-node-list .is-offline{background:#f53f3f}.region-node-list strong{overflow:hidden;font-size:10px;text-overflow:ellipsis;white-space:nowrap}.region-node-list small{color:var(--color-text-3,#86909c);font-size:9px}body[arco-theme='dark'] .region-group{background:#232324;border-color:rgba(255,255,255,.1)}
@media(max-width:720px){.region-distribution{margin-top:12px}.region-distribution__grid{grid-template-columns:1fr}.region-group{padding:12px}}
</style>
