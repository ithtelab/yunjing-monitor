<script setup>
/**
 * RegionFlag - 使用本地 SVG 旗帜，避免 Windows 把国旗 Emoji 降级成 US/GB 字母。
 * region 接受两位地区代码；UK 会沿用 normalizeRegionCode 规范为 GB。
 */
import { computed } from 'vue'
import { normalizeRegionCode } from '@/utils/monitor'

defineOptions({ name: 'RegionFlag' })

const props = defineProps({
  region: { type: String, default: '' }
})

const flagClass = computed(() => {
  const code = normalizeRegionCode(props.region)
  return /^[A-Z]{2}$/.test(code) ? `fi-${code.toLowerCase()}` : ''
})
</script>

<template>
  <span v-if="flagClass" class="region-flag fi" :class="flagClass" aria-hidden="true"></span>
</template>

<style scoped>
.region-flag.fi {
  display: inline-block;
  flex: none;
  width: 1.333em;
  height: 1em;
  border-radius: 2px;
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.08);
  vertical-align: -0.1em;
}
</style>
