<script setup>
/**
 * ParticleButton —— 点击爆粒子的按钮（React 版 ParticleButton 的 Vue 3 移植）
 *
 * 原版依赖 framer-motion + lucide-react + shadcn Button，本移植版零新增依赖：
 *   - 粒子动画：CSS keyframes + CSS 变量（--dx / --dy / --delay），替代 framer-motion
 *   - 点击图标：内联 SVG（lucide mouse-pointer-click，ISC 协议），替代 lucide-react
 *   - 按钮本体：复用项目公共组件 BaseButton（黑色风格）
 *
 * 交互效果：
 *   点击瞬间按钮轻微下压（scale .95），从按钮中心向外上方爆开 6 颗粒子，
 *   successDuration 毫秒后清除粒子并派发 success 事件。
 *   （修复了原版丢弃用户 onClick 的问题：点击事件会正常透传。）
 *
 * 用法：
 *   <ParticleButton variant="primary" size="lg" block @click="login">登录</ParticleButton>
 */
import { ref } from 'vue'
import BaseButton from './BaseButton.vue'

// inheritAttrs: false —— 手动把 $attrs 透传给 BaseButton，避免落到 Teleport 上
defineOptions({ name: 'ParticleButton', inheritAttrs: false })

const props = defineProps({
  // 粒子效果持续时长（毫秒），到点后清除粒子并派发 success
  successDuration: { type: Number, default: 1000 },
  // 是否在文字后显示「点击」小图标
  showIcon: { type: Boolean, default: true },
  // —— 以下 props 原样透传给 BaseButton ——
  variant: { type: String, default: 'primary' },
  size: { type: String, default: 'md' },
  block: { type: Boolean, default: false },
  type: { type: String, default: 'button' },
  disabled: { type: Boolean, default: false }
})

const emit = defineEmits(['click', 'success'])

const particles = ref([]) // 当前这轮粒子的数据（含随机偏移量）
const bursting = ref(false) // 是否处于爆散中（控制按钮下压态）
const btnRef = ref(null)
let clearTimer = null

// 以按钮中心为原点生成 6 颗粒子（粒子用 fixed 定位，坐标相对视口）
function spawnParticles() {
  const el = btnRef.value?.$el // BaseButton 根元素即 <button>
  if (!el) return
  const rect = el.getBoundingClientRect()
  const cx = rect.left + rect.width / 2
  const cy = rect.top + rect.height / 2
  particles.value = Array.from({ length: 6 }, (_, i) => ({
    id: `${Date.now()}-${i}`,
    left: `${cx}px`,
    top: `${cy}px`,
    // 奇偶交错向左右散开 20~70px，向上飘 20~70px（复刻原版运动轨迹）
    dx: `${(i % 2 ? 1 : -1) * (Math.random() * 50 + 20)}px`,
    dy: `${-(Math.random() * 50 + 20)}px`,
    delay: `${i * 0.1}s`
  }))
}

function handleClick(event) {
  if (props.disabled) return
  spawnParticles()
  bursting.value = true
  clearTimeout(clearTimer)
  clearTimer = setTimeout(() => {
    bursting.value = false
    particles.value = []
    emit('success')
  }, props.successDuration)
  emit('click', event)
}
</script>

<template>
  <!-- 粒子渲染到 body 下，避免被父容器 overflow 裁剪 -->
  <Teleport to="body">
    <span
      v-for="p in particles"
      :key="p.id"
      class="particle-btn__particle"
      :style="{ left: p.left, top: p.top, '--dx': p.dx, '--dy': p.dy, '--delay': p.delay }"
      aria-hidden="true"
    />
  </Teleport>

  <BaseButton
    ref="btnRef"
    v-bind="$attrs"
    class="particle-btn"
    :class="{ 'particle-btn--pressing': bursting }"
    :variant="variant"
    :size="size"
    :block="block"
    :type="type"
    :disabled="disabled"
    @click="handleClick"
  >
    <slot />
    <!-- lucide mouse-pointer-click 内联图标（避免新增图标库依赖） -->
    <svg
      v-if="showIcon"
      class="particle-btn__icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M14 4.1 12 6" />
      <path d="m5.1 8-2.9-.8" />
      <path d="m6 12-1.9 2" />
      <path d="M7.2 2.2 8 5.1" />
      <path
        d="M9.037 9.69a.498.498 0 0 1 .653-.653l11 4.5a.5.5 0 0 1-.074.949l-4.349 1.041a1 1 0 0 0-.74.739l-1.04 4.35a.5.5 0 0 1-.95.074z"
      />
    </svg>
  </BaseButton>
</template>

<style scoped>
/* 点击瞬间下压反馈（对应原版的 scale-95 + duration-100） */
.particle-btn {
  transition: transform 0.1s ease;
}
.particle-btn--pressing {
  transform: scale(0.95);
}

.particle-btn__icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

/* 粒子本体：4px 圆点，fixed 定位在按钮中心，沿 (--dx, --dy) 抛散 */
.particle-btn__particle {
  position: fixed;
  width: 4px;
  height: 4px;
  border-radius: 999px;
  background: #111;
  pointer-events: none;
  z-index: 9999;
  animation: particle-btn-burst 0.6s ease-out both;
  animation-delay: var(--delay, 0s);
}

/* 深色模式下粒子反白（对应原版 dark:bg-white） */
body[arco-theme='dark'] .particle-btn__particle {
  background: #fff;
}

/* 0→1→0 缩放 + 抛物线抛散，复刻 framer-motion 的 scale/x/y 关键帧 */
@keyframes particle-btn-burst {
  0% {
    transform: translate(-50%, -50%) scale(0);
    opacity: 1;
  }
  50% {
    transform: translate(calc(-50% + var(--dx) / 2), calc(-50% + var(--dy) / 2)) scale(1);
    opacity: 1;
  }
  100% {
    transform: translate(calc(-50% + var(--dx)), calc(-50% + var(--dy))) scale(0);
    opacity: 0;
  }
}
</style>
