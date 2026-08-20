<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import IconCheckCircle from '@arco-design/web-vue/es/icon/icon-check-circle'
import IconCloseCircle from '@arco-design/web-vue/es/icon/icon-close-circle'
import IconHome from '@arco-design/web-vue/es/icon/icon-home'
import IconSettings from '@arco-design/web-vue/es/icon/icon-settings'
import IconHistory from '@arco-design/web-vue/es/icon/icon-history'
import IconNotification from '@arco-design/web-vue/es/icon/icon-notification'
import IconBarChart from '@arco-design/web-vue/es/icon/icon-bar-chart'
import IconApps from '@arco-design/web-vue/es/icon/icon-apps'

const props = defineProps({
  items: {
    type: Array,
    default: () => []
  },
  activeKey: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['select'])
const hoveredKey = ref('')
const { t } = useI18n()

const icons = {
  home: IconHome,
  online: IconCheckCircle,
  offline: IconCloseCircle,
  settings: IconSettings,
  changelog: IconHistory,
  announcement: IconNotification,
  stats: IconBarChart,
  market: IconApps
}

const hasHover = computed(() => Boolean(hoveredKey.value))
const itemIcon = (item) => icons[item.icon] || IconHome
</script>

<template>
      <nav class="anime-navbar" :aria-label="t('common-main-nav')">
    <div class="anime-navbar__pill">
      <button
        v-for="item in items"
        :key="item.key"
        class="anime-nav-item"
        :class="{ 'is-active': activeKey === item.key }"
        type="button"
        :aria-current="activeKey === item.key ? 'page' : undefined"
        :aria-label="item.label"
        :title="item.label"
        @click="emit('select', item)"
        @mouseenter="hoveredKey = item.key"
        @mouseleave="hoveredKey = ''"
      >
        <span v-if="activeKey === item.key" class="active-aura" aria-hidden="true">
          <span class="active-shine"></span>
        </span>

        <span class="nav-label">{{ item.label }}</span>
        <component :is="itemIcon(item)" class="nav-icon" />

        <span
          v-if="activeKey === item.key"
          class="anime-mascot"
          :class="{ 'is-reacting': hasHover }"
          aria-hidden="true"
        >
          <span class="mascot-face">
            <span class="mascot-eye is-left"></span>
            <span class="mascot-eye is-right"></span>
            <span class="mascot-cheek is-left"></span>
            <span class="mascot-cheek is-right"></span>
            <span class="mascot-mouth"></span>
            <span class="mascot-sparkle is-one">✦</span>
            <span class="mascot-sparkle is-two">✦</span>
          </span>
          <span class="mascot-tail"></span>
        </span>
      </button>
    </div>
  </nav>
</template>

<style scoped lang="scss">
.anime-navbar {
  display: flex;
  justify-content: center;
  min-width: 0;
  pointer-events: auto;
}

.anime-navbar__pill {
  --edge-angle: 0deg;
  --nav-active-bg: rgba(219, 234, 254, .78);
  --nav-hover-bg: rgba(15, 23, 42, .06);
  --nav-text: rgba(30, 41, 59, .68);
  --nav-text-active: #0f172a;
  position: relative;
  isolation: isolate;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, .32);
  border-radius: 999px;
  background:
    radial-gradient(circle at 13% 28%, rgba(30, 41, 59, .16) 0 1px, transparent 1.4px),
    radial-gradient(circle at 29% 72%, rgba(59, 130, 246, .14) 0 1px, transparent 1.5px),
    radial-gradient(circle at 52% 22%, rgba(30, 41, 59, .12) 0 1px, transparent 1.4px),
    radial-gradient(circle at 71% 67%, rgba(59, 130, 246, .13) 0 1px, transparent 1.5px),
    radial-gradient(circle at 88% 31%, rgba(30, 41, 59, .14) 0 1px, transparent 1.4px),
    rgba(255, 255, 255, .9);
  box-shadow: 0 15px 34px rgba(15, 23, 42, .14), inset 0 1px 0 rgba(255, 255, 255, .92);
  backdrop-filter: blur(18px) saturate(145%);
  animation: nav-enter .55s cubic-bezier(.2, .85, .3, 1.15) both;

  &::after {
    position: absolute;
    inset: -2px;
    z-index: 0;
    padding: 2px;
    border-radius: inherit;
    background: conic-gradient(from var(--edge-angle), transparent 0 58%, rgba(96, 165, 250, .16) 68%, #60a5fa 78%, #ffffff 84%, rgba(147, 197, 253, .5) 90%, transparent 100%);
    content: '';
    pointer-events: none;
    filter: drop-shadow(0 0 7px rgba(96, 165, 250, .48));
    -webkit-mask: linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0);
    -webkit-mask-composite: xor;
    mask-composite: exclude;
    opacity: .52;
    transition: opacity .2s ease;
  }

  &:hover::after {
    opacity: 1;
    animation: edge-orbit 3s linear infinite;
  }
}

:global(body[arco-theme='dark'] .anime-navbar__pill) {
  --nav-active-bg: rgba(37, 99, 235, .18);
  --nav-hover-bg: rgba(255, 255, 255, .09);
  --nav-text: rgba(255, 255, 255, .68);
  --nav-text-active: #ffffff;
  border-color: rgba(255, 255, 255, .12);
  background: rgba(0, 0, 0, .9);
  box-shadow: 0 14px 34px rgba(0, 0, 0, .4), inset 0 1px 0 rgba(255, 255, 255, .05);
}

.anime-nav-item {
  position: relative;
  isolation: isolate;
  min-width: 86px;
  padding: 12px 24px;
  overflow: visible;
  border: 0;
  border-radius: 999px;
  outline: none;
  background: transparent;
  color: var(--nav-text);
  cursor: pointer;
  font: inherit;
  font-size: 14px;
  font-weight: 600;
  transition: color .22s ease, transform .22s ease, background-color .22s ease;

  &::before {
    position: absolute;
    inset: 0;
    z-index: -2;
    border-radius: inherit;
    background: var(--nav-hover-bg);
    content: '';
    opacity: 0;
    transform: scale(.88);
    transition: opacity .2s ease, transform .25s cubic-bezier(.2, .8, .2, 1);
  }

  &:hover {
    color: var(--nav-text-active);

    &::before {
      opacity: 1;
      transform: scale(1);
    }
  }

  &:focus-visible {
    box-shadow: 0 0 0 2px #0f172a, 0 0 0 4px rgba(96, 165, 250, .9);
  }

  &:active {
    transform: scale(.96);
  }

  &.is-active {
    color: var(--nav-text-active);
  }
}

.active-aura {
  position: absolute;
  inset: 0;
  z-index: -1;
  overflow: hidden;
  border: 1px solid rgba(96, 165, 250, .28);
  border-radius: inherit;
  background: var(--nav-active-bg);
  box-shadow: 0 0 14px rgba(59, 130, 246, .25), 0 0 26px rgba(59, 130, 246, .2), 0 0 42px rgba(59, 130, 246, .12);
  animation: aura-pulse 2.4s ease-in-out infinite;
}

.active-shine {
  position: absolute;
  inset: 0;
  background: linear-gradient(100deg, transparent 12%, rgba(147, 197, 253, .26) 48%, transparent 82%);
  transform: translateX(-120%);
  animation: shine 3s ease-in-out infinite;
}

.nav-label {
  position: relative;
  z-index: 2;
}

.nav-icon {
  position: relative;
  z-index: 2;
  display: none;
  margin: auto;
  font-size: 19px;
}

.anime-mascot {
  position: absolute;
  bottom: calc(100% + 7px);
  left: 50%;
  z-index: 5;
  width: 48px;
  height: 48px;
  transform: translateX(-50%);
  pointer-events: none;
  animation: mascot-arrive .45s cubic-bezier(.2, .9, .3, 1.3) both, mascot-float 2.2s .45s ease-in-out infinite;
}

.mascot-face {
  position: absolute;
  top: 0;
  left: 50%;
  width: 40px;
  height: 40px;
  border: 1px solid rgba(15, 23, 42, .08);
  border-radius: 48% 48% 45% 45%;
  background: #ffffff;
  box-shadow: 0 8px 22px rgba(15, 23, 42, .2), inset 0 -4px 8px rgba(148, 163, 184, .13);
  transform: translateX(-50%);
}

.mascot-eye {
  position: absolute;
  top: 15px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #111827;
  transform-origin: center;

  &.is-left { left: 10px; }
  &.is-right { right: 10px; }
}

.mascot-cheek {
  position: absolute;
  top: 23px;
  width: 8px;
  height: 6px;
  border-radius: 50%;
  background: rgba(249, 168, 212, .68);

  &.is-left { left: 5px; }
  &.is-right { right: 5px; }
}

.mascot-mouth {
  position: absolute;
  top: 24px;
  left: 50%;
  width: 16px;
  height: 8px;
  border-bottom: 2px solid #111827;
  border-radius: 50%;
  transform: translateX(-50%);
}

.mascot-tail {
  position: absolute;
  bottom: 0;
  left: 50%;
  z-index: -1;
  width: 16px;
  height: 16px;
  border-radius: 2px;
  background: #ffffff;
  box-shadow: 4px 5px 12px rgba(15, 23, 42, .12);
  transform: translateX(-50%) rotate(45deg);
}

.mascot-sparkle {
  position: absolute;
  z-index: 4;
  color: #facc15;
  font-size: 12px;
  line-height: 1;
  opacity: 0;
  filter: drop-shadow(0 1px 3px rgba(250, 204, 21, .42));
  text-shadow: 0 0 8px rgba(253, 224, 71, .55);
  transform-origin: center;
  will-change: transform, opacity;

  &.is-one { top: -3px; right: -7px; }
  &.is-two { top: -5px; left: -5px; }
}

.anime-mascot.is-reacting {
  .mascot-face {
    animation: mascot-react .48s ease-in-out;
  }

  .mascot-eye {
    animation: blink .3s ease-in-out;
  }

  .mascot-sparkle {
    animation: sparkle-in .55s ease-out both;

    &.is-two { animation-delay: .08s; }
  }
}

@keyframes nav-enter {
  from { opacity: 0; transform: translateY(-14px) scale(.97); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}

@property --edge-angle {
  syntax: '<angle>';
  initial-value: 0deg;
  inherits: false;
}

@keyframes edge-orbit {
  to { --edge-angle: 360deg; }
}

@keyframes aura-pulse {
  0%, 100% { opacity: .76; transform: scale(1); }
  50% { opacity: 1; transform: scale(1.025); }
}

@keyframes shine {
  0%, 20% { transform: translateX(-120%); }
  60%, 100% { transform: translateX(120%); }
}

@keyframes mascot-arrive {
  from { opacity: 0; transform: translateX(-50%) translateY(10px) scale(.65); }
  to { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
}

@keyframes mascot-float {
  0%, 100% { translate: 0 0; }
  50% { translate: 0 -3px; }
}

@keyframes mascot-react {
  0%, 100% { transform: translateX(-50%) rotate(0) scale(1); }
  35% { transform: translateX(-50%) rotate(-5deg) scale(1.07); }
  70% { transform: translateX(-50%) rotate(5deg) scale(1.07); }
}

@keyframes blink {
  0%, 100% { transform: scaleY(1); }
  50% { transform: scaleY(.18); }
}

@keyframes sparkle-in {
  0% { opacity: 0; transform: scale(0) rotate(-20deg); }
  50% { opacity: 1; transform: scale(1.2) rotate(8deg); }
  100% { opacity: .78; transform: scale(1) rotate(0); }
}

@media (max-width: 768px) {
  .anime-navbar {
    width: 100%;
  }

  .anime-navbar__pill {
    width: 100%;
    gap: 2px;
    padding: 5px;
  }

  .anime-nav-item {
    flex: 1 1 20%;
    min-width: 0;
    min-height: 44px;
    padding: 10px 6px;
  }

  .nav-label { display: none; }
  .nav-icon { display: block; }

  .anime-mascot {
    display: none;
  }
}

@media (hover: none) {
  .anime-nav-item:hover {
    color: var(--nav-text);

    &::before {
      opacity: 0;
      transform: scale(.88);
    }
  }

  .anime-nav-item.is-active:hover {
    color: var(--nav-text-active);
  }

  .anime-nav-item:active {
    transform: scale(.96);
  }
}

@media (prefers-reduced-motion: reduce) {
  .anime-navbar__pill,
  .anime-navbar__pill::after,
  .active-aura,
  .active-shine,
  .anime-mascot,
  .mascot-face,
  .mascot-eye,
  .mascot-sparkle {
    animation: none !important;
  }
}
</style>
