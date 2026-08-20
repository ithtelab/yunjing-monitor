<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({ item: { type: Object, required: true }, apiBase: { type: String, default: '' } })
const { t } = useI18n()
const card = ref(null)
let observer
let reported = false

const reportImpression = () => {
  if (reported) return
  reported = true
  const url = `${props.apiBase || ''}/api/market/ads/impression`
  const body = JSON.stringify({ id: props.item.id })
  if (navigator.sendBeacon) navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }))
  else fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body, keepalive: true }).catch(() => {})
}

onMounted(() => {
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting && entry.intersectionRatio >= 0.5)) {
      reportImpression()
      observer?.disconnect()
    }
  }, { threshold: [0.5] })
  if (card.value) observer.observe(card.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <article ref="card" class="ad-card glow-card" :class="{ 'is-recommended': item.recommended }">
    <a class="ad-card__image" :href="`${apiBase || ''}/r/ad/${encodeURIComponent(item.id)}`" target="_blank" rel="noopener sponsored">
      <img :src="`${apiBase || ''}${item.image_url}`" :alt="item.title" loading="lazy">
      <span class="ad-card__label">{{ t('market-ad-label') }}</span>
      <span v-if="item.recommended" class="ad-card__recommended">{{ t('market-ad-recommended') }}</span>
    </a>
    <div class="ad-card__body">
      <div v-if="item.brand" class="ad-card__brand">{{ item.brand }}</div>
      <h2>{{ item.title }}</h2>
      <p>{{ item.description }}</p>
      <a class="ad-card__cta" :href="`${apiBase || ''}/r/ad/${encodeURIComponent(item.id)}`" target="_blank" rel="noopener sponsored">
        {{ item.button_text || t('market-ad-details') }} <IconArrowUpRight />
      </a>
    </div>
  </article>
</template>

<style scoped>
.ad-card { overflow: hidden; min-width: 0; border: 1px solid var(--color-border-2, #e5e6eb); border-radius: 8px; background: var(--color-bg-2, #fff); box-shadow: 0 8px 24px rgba(15, 23, 42, .06); }
.ad-card.is-recommended { border-color: rgba(22, 93, 255, .34); }
.ad-card__image { position: relative; display: block; aspect-ratio: 16 / 9; overflow: hidden; background: var(--color-fill-2, #f2f3f5); }
.ad-card__image img { display: block; width: 100%; height: 100%; object-fit: cover; transition: transform .25s ease; }
.ad-card:hover .ad-card__image img { transform: scale(1.025); }
.ad-card__label,.ad-card__recommended { position: absolute; top: 10px; padding: 3px 7px; border-radius: 4px; font-size: 11px; line-height: 1.4; }
.ad-card__label { left: 10px; background: rgba(17, 17, 17, .78); color: #fff; }
.ad-card__recommended { right: 10px; background: #165dff; color: #fff; }
.ad-card__body { display: flex; min-height: 168px; padding: 15px; flex-direction: column; }
.ad-card__brand { margin-bottom: 5px; color: #165dff; font-size: 12px; font-weight: 700; }
.ad-card h2 { margin: 0; color: var(--color-text-1, #1d2129); font-size: 17px; line-height: 1.4; }
.ad-card p { display: -webkit-box; margin: 8px 0 14px; overflow: hidden; color: var(--color-text-3, #86909c); font-size: 13px; line-height: 1.65; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.ad-card__cta { display: inline-flex; width: fit-content; margin-top: auto; align-items: center; gap: 5px; color: #165dff; font-size: 13px; font-weight: 700; text-decoration: none; }
.ad-card__cta :deep(svg) { width: 15px; height: 15px; }
body[arco-theme='dark'] .ad-card { border-color: rgba(255,255,255,.16); background: #090909; box-shadow: none; }
body[arco-theme='dark'] .ad-card.is-recommended { border-color: rgba(96,165,250,.5); }
body[arco-theme='dark'] .ad-card__image { background: #171717; }
body[arco-theme='dark'] .ad-card h2 { color: #f5f5f5; }
body[arco-theme='dark'] .ad-card p { color: #a3a3a3; }
@media (max-width: 640px) {
  .ad-card__body { min-height: 145px; padding: 11px 12px; }
  .ad-card__brand { margin-bottom: 3px; }
  .ad-card p { margin: 5px 0 8px; line-height: 1.5; }
}
</style>
