<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps({
  text: { type: String, default: '' },
  duration: { type: Number, default: 0.7 },
  speed: { type: Number, default: 0.035 },
  delay: { type: Number, default: 0 },
  characterSet: { type: String, default: '0123456789,+~' }
})

const output = ref(props.text)
let timer
let delayTimer

const stop = () => { window.clearInterval(timer); window.clearTimeout(delayTimer) }
const play = () => {
  stop()
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches || !props.text) { output.value = props.text; return }
  delayTimer = window.setTimeout(() => {
    const steps = Math.max(1, Math.ceil(props.duration / props.speed))
    let step = 0
    timer = window.setInterval(() => {
      const progress = step / steps
      output.value = [...props.text].map((char, index) => {
        if (/\s/.test(char)) return char
        if (progress * props.text.length > index) return char
        return props.characterSet[Math.floor(Math.random() * props.characterSet.length)]
      }).join('')
      step += 1
      if (step > steps) { window.clearInterval(timer); output.value = props.text }
    }, props.speed * 1000)
  }, props.delay)
}

watch(() => props.text, (next, previous) => { output.value = next; if (next !== previous) play() }, { immediate: true })
onBeforeUnmount(stop)
</script>

<template><span class="text-scramble" :aria-label="text"><span aria-hidden="true">{{ output }}</span></span></template>

<style scoped>
.text-scramble { display: inline-block; min-width: 4ch; font-variant-numeric: tabular-nums; white-space: nowrap; }
</style>
