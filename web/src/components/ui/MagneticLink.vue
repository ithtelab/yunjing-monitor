<script setup>
import { onBeforeUnmount, ref } from 'vue'

const props = defineProps({ intensity: { type: Number, default: 0.3 }, range: { type: Number, default: 100 } })
const root = ref(null)
const transform = ref('translate3d(0,0,0)')
let frame = 0
let x = 0, y = 0, vx = 0, vy = 0, tx = 0, ty = 0

const canAnimate = () => window.matchMedia?.('(hover: hover) and (pointer: fine)').matches && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
const tick = () => {
  vx = (vx + (tx - x) * 0.15) * 0.72; vy = (vy + (ty - y) * 0.15) * 0.72
  x += vx; y += vy; transform.value = `translate3d(${x.toFixed(2)}px,${y.toFixed(2)}px,0)`
  if (Math.abs(tx - x) + Math.abs(ty - y) + Math.abs(vx) + Math.abs(vy) > 0.08) frame = requestAnimationFrame(tick)
  else { x = tx; y = ty; transform.value = `translate3d(${x}px,${y}px,0)`; frame = 0 }
}
const animate = () => { if (!frame) frame = requestAnimationFrame(tick) }
const move = (event) => { if (!canAnimate() || !root.value) return; const rect=root.value.getBoundingClientRect();const dx=event.clientX-(rect.left+rect.width/2);const dy=event.clientY-(rect.top+rect.height/2);const distance=Math.hypot(dx,dy);const scale=Math.max(0,1-distance/props.range);tx=dx*props.intensity*scale;ty=dy*props.intensity*scale;animate() }
const reset = () => { tx=0;ty=0;animate() }
onBeforeUnmount(() => cancelAnimationFrame(frame))
</script>

<template><span ref="root" class="magnetic-link" :style="{ transform }" @pointermove="move" @pointerleave="reset"><slot /></span></template>

<style scoped>
.magnetic-link { display: inline-flex; flex: none; will-change: transform; }
@media (hover:none),(prefers-reduced-motion:reduce){.magnetic-link{transform:none!important;will-change:auto}}
</style>
