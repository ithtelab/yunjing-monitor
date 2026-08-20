<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { SmartphoneDevice } from '@iconoir/vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  siteName: { type: String, default: '云镜监控' }
})

// 16 个真实厂商图标，位置一比一对应参考组件 demo 的 16 个坐标
const providers = [
  { id: 'aliyun', name: '阿里云', icon: '/providers/aliyun.svg', pos: { top: '10%', left: '10%' } },
  { id: 'huawei', name: '华为云', icon: '/providers/huawei-64.png', pos: { top: '20%', right: '8%' } },
  { id: 'tencent', name: '腾讯云', icon: '/providers/tencent-64.png', pos: { top: '80%', left: '10%' } },
  { id: 'aws', name: 'AWS', icon: '/providers/aws-64.png', pos: { bottom: '10%', right: '10%' } },
  { id: 'azure', name: 'Azure', icon: '/providers/azure-64.png', pos: { top: '5%', left: '30%' } },
  { id: 'google', name: 'Google Cloud', icon: '/providers/google-cloud-64.png', pos: { top: '5%', right: '30%' } },
  { id: 'cloudflare', name: 'Cloudflare', icon: '/providers/cloudflare-64.png', pos: { bottom: '8%', left: '25%' } },
  { id: 'digitalocean', name: 'DigitalOcean', icon: '/providers/digitalocean-64.png', pos: { top: '40%', left: '15%' } },
  { id: 'oracle', name: 'Oracle Cloud', icon: '/providers/oracle-64.png', pos: { top: '75%', right: '25%' } },
  { id: 'vultr', name: 'Vultr', icon: '/providers/vultr-64.png', pos: { top: '90%', left: '70%' } },
  { id: 'akamai', name: 'Akamai', icon: '/providers/akamai-64.png', pos: { top: '50%', right: '5%' } },
  { id: 'ovh', name: 'OVHcloud', icon: '/providers/ovh-64.png', pos: { top: '55%', left: '5%' } },
  { id: 'hetzner', name: 'Hetzner', icon: '/providers/hetzner-64.png', pos: { top: '5%', left: '55%' } },
  { id: 'ucloud', name: 'UCloud', icon: '/providers/ucloud-64.png', pos: { bottom: '5%', right: '45%' } },
  { id: 'linode', name: 'Linode', icon: '/providers/linode.svg', pos: { top: '25%', right: '20%' } },
  { id: 'racknerd', name: 'RackNerd', icon: '/providers/racknerd-64.png', pos: { top: '60%', left: '30%' } }
].map((provider, index) => ({
  ...provider,
  delay: index * 0.08,
  floatDuration: 5 + ((index * 0.618) % 1) * 5,
  motionDepth: 0.62 + (index % 5) * 0.1,
  motionSpin: index % 2 === 0 ? 1 : -1
}))

// 与参考组件一致的弹簧参数：stiffness 300 / damping 20，斥力半径 150px、最大力 50px
const SPRING_STIFFNESS = 300
const SPRING_DAMPING = 20
const REPEL_RADIUS = 150
const REPEL_FORCE = 50

const springs = new Map()
let rafId = 0
let lastTime = 0
const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const motionAvailable = ref(!reducedMotion && window.matchMedia('(pointer: coarse)').matches && ('DeviceOrientationEvent' in window || 'DeviceMotionEvent' in window))
const motionEnabled = ref(false)
const motionPermissionDenied = ref(false)
const motionBaseline = { beta: null, gamma: null }
const sensorGravity = { x: 0, y: 0, z: 0 }
const gravityTarget = { x: 0, y: 0, rotation: 0 }
let motionListening = false
let lastShakeAt = 0
let hasGravitySample = false

const clamp = (value, min, max) => Math.max(min, Math.min(max, value))

const screenAxes = (x, y) => {
  const angle = (Number(window.screen?.orientation?.angle ?? window.orientation) || 0) * Math.PI / 180
  const cos = Math.cos(angle)
  const sin = Math.sin(angle)
  return { x: x * cos + y * sin, y: -x * sin + y * cos }
}

const setIconRef = (id, element) => {
  if (element) {
    const provider = providers.find((item) => item.id === id)
    if (!springs.has(id)) {
      springs.set(id, {
        el: element,
        x: 0,
        y: 0,
        rotation: 0,
        vx: 0,
        vy: 0,
        vr: 0,
        tx: 0,
        ty: 0,
        motionDepth: provider?.motionDepth || 0.8,
        motionSpin: provider?.motionSpin || 1
      })
    }
    else springs.get(id).el = element
  } else {
    springs.delete(id)
  }
}

const springTick = (time) => {
  const dt = Math.min((time - lastTime) / 1000 || 0, 0.064)
  lastTime = time
  springs.forEach((s) => {
    const targetX = s.tx + gravityTarget.x * s.motionDepth
    const targetY = s.ty + gravityTarget.y * s.motionDepth
    const targetRotation = gravityTarget.rotation * s.motionSpin * s.motionDepth
    s.vx += (SPRING_STIFFNESS * (targetX - s.x) - SPRING_DAMPING * s.vx) * dt
    s.vy += (SPRING_STIFFNESS * (targetY - s.y) - SPRING_DAMPING * s.vy) * dt
    s.vr += (260 * (targetRotation - s.rotation) - 18 * s.vr) * dt
    s.x += s.vx * dt
    s.y += s.vy * dt
    s.rotation += s.vr * dt
    s.el.style.transform = `translate3d(${s.x.toFixed(2)}px, ${s.y.toFixed(2)}px, 0) rotate(${s.rotation.toFixed(2)}deg)`
  })
  rafId = window.requestAnimationFrame(springTick)
}

const handlePointerMove = (event) => {
  if (reducedMotion) return
  springs.forEach((s) => {
    const rect = s.el.getBoundingClientRect()
    const dx = event.clientX - (rect.left + rect.width / 2)
    const dy = event.clientY - (rect.top + rect.height / 2)
    const distance = Math.hypot(dx, dy)
    if (distance < REPEL_RADIUS && distance > 0) {
      const force = (1 - distance / REPEL_RADIUS) * REPEL_FORCE
      s.tx = (-dx / distance) * force
      s.ty = (-dy / distance) * force
    } else {
      s.tx = 0
      s.ty = 0
    }
  })
}

const resetIcons = () => {
  springs.forEach((s) => { s.tx = 0; s.ty = 0 })
}

const resetMotionBaseline = () => {
  motionBaseline.beta = null
  motionBaseline.gamma = null
}

const handleDeviceOrientation = (event) => {
  if (typeof event.beta !== 'number' || typeof event.gamma !== 'number') return
  if (motionBaseline.beta === null) {
    motionBaseline.beta = event.beta
    motionBaseline.gamma = event.gamma
    return
  }

  const deltaBeta = clamp(event.beta - motionBaseline.beta, -40, 40)
  const deltaGamma = clamp(event.gamma - motionBaseline.gamma, -40, 40)
  const mapped = screenAxes(deltaGamma, deltaBeta)
  gravityTarget.x = clamp(mapped.x / 28, -1, 1) * 30
  gravityTarget.y = clamp(mapped.y / 28, -1, 1) * 30
  gravityTarget.rotation = clamp(mapped.x / 28, -1, 1) * 4
}

const handleDeviceMotion = (event) => {
  const acceleration = event.acceleration
  const includingGravity = event.accelerationIncludingGravity
  let x = typeof acceleration?.x === 'number' ? acceleration.x : Number.NaN
  let y = typeof acceleration?.y === 'number' ? acceleration.y : Number.NaN
  let z = typeof acceleration?.z === 'number' ? acceleration.z : Number.NaN

  if (![x, y, z].every(Number.isFinite)) {
    const rawX = typeof includingGravity?.x === 'number' ? includingGravity.x : Number.NaN
    const rawY = typeof includingGravity?.y === 'number' ? includingGravity.y : Number.NaN
    const rawZ = typeof includingGravity?.z === 'number' ? includingGravity.z : Number.NaN
    if (![rawX, rawY, rawZ].every(Number.isFinite)) return
    if (!hasGravitySample) {
      sensorGravity.x = rawX
      sensorGravity.y = rawY
      sensorGravity.z = rawZ
      hasGravitySample = true
      return
    }
    sensorGravity.x = sensorGravity.x * .88 + rawX * .12
    sensorGravity.y = sensorGravity.y * .88 + rawY * .12
    sensorGravity.z = sensorGravity.z * .88 + rawZ * .12
    x = rawX - sensorGravity.x
    y = rawY - sensorGravity.y
    z = rawZ - sensorGravity.z
  }

  const magnitude = Math.hypot(x, y, z)
  const now = performance.now()
  if (magnitude < 6 || now - lastShakeAt < 180) return
  lastShakeAt = now

  const mapped = screenAxes(x, y)
  const strength = Math.min((magnitude - 6) * 7, 72)
  const angle = Math.atan2(-mapped.y, mapped.x)
  let index = 0
  springs.forEach((s) => {
    const spread = (index % 3 - 1) * .18
    s.vx += Math.cos(angle + spread) * strength * s.motionDepth
    s.vy += Math.sin(angle + spread) * strength * s.motionDepth
    s.vr += (index % 2 === 0 ? 1 : -1) * strength * .04
    index += 1
  })
}

const startMotion = () => {
  if (motionListening) return
  resetMotionBaseline()
  window.addEventListener('deviceorientation', handleDeviceOrientation, { passive: true })
  window.addEventListener('devicemotion', handleDeviceMotion, { passive: true })
  window.screen?.orientation?.addEventListener?.('change', resetMotionBaseline)
  window.addEventListener('orientationchange', resetMotionBaseline, { passive: true })
  motionListening = true
  motionEnabled.value = true
}

const stopMotion = () => {
  window.removeEventListener('deviceorientation', handleDeviceOrientation)
  window.removeEventListener('devicemotion', handleDeviceMotion)
  window.screen?.orientation?.removeEventListener?.('change', resetMotionBaseline)
  window.removeEventListener('orientationchange', resetMotionBaseline)
  motionListening = false
  motionEnabled.value = false
  gravityTarget.x = 0
  gravityTarget.y = 0
  gravityTarget.rotation = 0
  sensorGravity.x = 0
  sensorGravity.y = 0
  sensorGravity.z = 0
  hasGravitySample = false
  resetMotionBaseline()
}

const toggleMotion = async () => {
  if (motionEnabled.value) {
    stopMotion()
    return
  }

  try {
    const permissionRequests = []
    if (typeof window.DeviceOrientationEvent?.requestPermission === 'function') {
      permissionRequests.push(window.DeviceOrientationEvent.requestPermission())
    }
    if (typeof window.DeviceMotionEvent?.requestPermission === 'function') {
      permissionRequests.push(window.DeviceMotionEvent.requestPermission())
    }
    const permissions = await Promise.all(permissionRequests)
    if (permissions.some((permission) => permission !== 'granted')) {
      motionPermissionDenied.value = true
      return
    }
    motionPermissionDenied.value = false
    startMotion()
  } catch {
    motionPermissionDenied.value = true
  }
}

const handleIconError = (event) => {
  // 图标加载失败时隐藏整个磁贴，不显示任何占位文字
  const tile = event.currentTarget.closest('.fi-pos')
  if (tile) tile.style.display = 'none'
}

onMounted(() => {
  if (!reducedMotion) {
    rafId = window.requestAnimationFrame(springTick)
    if (motionAvailable.value && typeof window.DeviceOrientationEvent?.requestPermission !== 'function') startMotion()
  }
})

onUnmounted(() => {
  window.cancelAnimationFrame(rafId)
  stopMotion()
})
</script>

<template>
  <section class="landing-hero" @mousemove="handlePointerMove" @mouseleave="resetIcons">
    <div class="hero-icons" :aria-label="t('landing-icons-aria')">
      <div
        v-for="provider in providers"
        :key="provider.id"
        :ref="(element) => setIconRef(provider.id, element)"
        class="fi-pos"
        :style="provider.pos"
      >
        <div class="fi-enter" :style="{ animationDelay: `${provider.delay}s` }">
          <div class="fi-tile" :style="{ animationDuration: `${provider.floatDuration}s` }">
            <img :src="provider.icon" alt="" role="presentation" loading="eager" @error="handleIconError">
          </div>
        </div>
      </div>
    </div>

    <button
      v-if="motionAvailable"
      class="motion-toggle"
      :class="{ 'is-active': motionEnabled, 'is-denied': motionPermissionDenied }"
      type="button"
      :aria-label="motionEnabled ? t('landing-motion-off') : t('landing-motion-on')"
      :aria-pressed="motionEnabled"
      :title="motionPermissionDenied ? t('landing-motion-denied') : (motionEnabled ? t('landing-motion-off') : t('landing-motion-on'))"
      @click.stop="toggleMotion"
    >
      <SmartphoneDevice :width="20" :height="20" :stroke-width="1.8" aria-hidden="true" />
    </button>

    <div class="hero-content">
      <h1>{{ t('landing-title') }}</h1>
      <p>{{ t('landing-desc', { siteName }) }}</p>
      <div class="hero-actions">
        <a class="shimmer-cta" href="/monitor">
          <span class="shimmer-spark-container" aria-hidden="true"><span class="shimmer-spark"><span class="shimmer-spark-gradient"></span></span></span>
          <span class="shimmer-label">{{ t('landing-cta') }}</span>
          <span class="shimmer-highlight" aria-hidden="true"></span>
          <span class="shimmer-backdrop" aria-hidden="true"></span>
        </a>
      </div>
    </div>
  </section>
</template>

<style scoped lang="scss">
/* 一比一对应参考组件：relative w-full h-screen min-h-[700px] flex items-center justify-center overflow-hidden bg-background */
.landing-hero {
  --landing-bg: #f7f9fc;
  --landing-ink: #0b1220;
  --landing-ink-70: rgba(11, 18, 32, .7);
  --landing-muted: #627087;
  --tile-bg: rgba(255, 255, 255, .8);
  --tile-border: rgba(15, 23, 42, .1);
  position: relative;
  display: flex;
  width: 100%;
  height: 100vh;
  min-height: 700px;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: var(--landing-bg);
}

/* absolute inset-0 图标层 */
.hero-icons { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.fi-pos { position: absolute; will-change: transform; }

.motion-toggle {
  position: absolute;
  top: max(16px, env(safe-area-inset-top));
  right: max(16px, env(safe-area-inset-right));
  z-index: 30;
  display: none;
  width: 42px;
  height: 42px;
  padding: 0;
  place-items: center;
  border: 1px solid var(--tile-border);
  border-radius: 50%;
  background: var(--tile-bg);
  color: var(--landing-muted);
  box-shadow: 0 10px 28px rgba(15, 23, 42, .12);
  backdrop-filter: blur(12px);
  cursor: pointer;
  touch-action: manipulation;
  -webkit-tap-highlight-color: transparent;
}

.motion-toggle.is-active {
  border-color: rgba(37, 99, 235, .35);
  color: #2563eb;
  box-shadow: 0 10px 28px rgba(37, 99, 235, .2), inset 0 0 0 3px rgba(37, 99, 235, .08);
}

.motion-toggle.is-denied { color: #dc2626; }
.motion-toggle.is-active svg { animation: motion-phone 1.8s ease-in-out infinite; }

@keyframes motion-phone {
  0%, 100% { transform: rotate(0deg); }
  35% { transform: rotate(-7deg); }
  65% { transform: rotate(7deg); }
}

/* 入场：opacity 0 scale .5 → 1，0.6s cubic-bezier(0.22,1,0.36,1)，按索引错峰 0.08s */
.fi-enter { animation: fi-enter .6s cubic-bezier(.22, 1, .36, 1) both; }
@keyframes fi-enter {
  from { opacity: 0; transform: scale(.5); }
  to { opacity: 1; transform: scale(1); }
}

/* 磁贴：w-16 h-16 md:w-20 md:h-20 p-3 rounded-3xl shadow-xl bg-card/80 backdrop-blur-md border-border/10 */
.fi-tile {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 64px;
  height: 64px;
  padding: 12px;
  border: 1px solid var(--tile-border);
  border-radius: 24px;
  background: var(--tile-bg);
  box-shadow: 0 20px 25px -5px rgba(0,0,0,.1), 0 8px 10px -6px rgba(0,0,0,.1);
  backdrop-filter: blur(12px);
  animation: fi-float 7s ease-in-out infinite alternate;
}

/* 漂浮：y [0,-8,0,8,0] x [0,6,0,-6,0] rotate [0,5,0,-5,0]，mirror 循环 */
@keyframes fi-float {
  0% { transform: translate(0, 0) rotate(0deg); }
  25% { transform: translate(6px, -8px) rotate(5deg); }
  50% { transform: translate(0, 0) rotate(0deg); }
  75% { transform: translate(-6px, 8px) rotate(-5deg); }
  100% { transform: translate(0, 0) rotate(0deg); }
}

/* 图标：w-8 h-8 md:w-10 md:h-10 */
.fi-tile img { width: 32px; height: 32px; object-fit: contain; }

/* 前景内容：relative z-10 text-center px-4 */
.hero-content { position: relative; z-index: 10; padding: 0 16px; text-align: center; }

/* 标题：text-5xl md:text-7xl font-bold tracking-tight，foreground → foreground/70 竖向渐变裁切 */
.hero-content h1 {
  margin: 0;
  color: transparent;
  background: linear-gradient(to bottom, var(--landing-ink), var(--landing-ink-70));
  background-clip: text;
  -webkit-background-clip: text;
  font-size: 48px;
  font-weight: 700;
  letter-spacing: -.025em;
  line-height: 1.1;
}

/* 副标题：mt-6 max-w-xl text-lg text-muted-foreground */
.hero-content > p { max-width: 576px; margin: 24px auto 0; color: var(--landing-muted); font-size: 18px; line-height: 1.8; }

/* CTA：mt-10，size lg + px-8 py-6 text-base font-semibold */
.hero-actions { margin-top: 40px; }
/* ShimmerButton 一比一复刻：--spread 90deg / shimmer #fff / radius 100px / speed 3s / cut 0.05em / bg 纯黑 */
.shimmer-cta {
  --spread: 90deg;
  --shimmer-color: #ffffff;
  --radius: 100px;
  --speed: 3s;
  --cut: 0.05em;
  --bg: rgba(0, 0, 0, 1);
  position: relative;
  z-index: 0;
  display: inline-flex; /* 参考组件是 button（自动收缩），a 标签需用 inline-flex 才不会撑满整行 */
  cursor: pointer;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  white-space: nowrap;
  border: 1px solid rgba(255, 255, 255, .1);
  padding: 12px 24px;
  color: #fff;
  background: var(--bg);
  border-radius: var(--radius);
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, .25);
  transform: translateZ(0);
  transition: transform .3s ease-in-out;
}
.shimmer-cta:active { transform: translateY(1px); }

/* spark container：absolute inset-0 -z-30 blur-[2px] [container-type:size] */
.shimmer-spark-container { position: absolute; inset: 0; z-index: -30; overflow: visible; filter: blur(2px); container-type: size; }
/* spark：inset-0 h-[100cqh] [aspect-ratio:1] + shimmer-slide */
.shimmer-spark { position: absolute; inset: 0; height: 100cqh; aspect-ratio: 1; border-radius: 0; animation: shimmer-slide var(--speed) ease-in-out infinite alternate; }
/* spark 渐变：-inset-full conic-gradient + spin-around */
.shimmer-spark-gradient {
  position: absolute;
  inset: -100%;
  width: auto;
  rotate: 0deg;
  translate: 0 0;
  background: conic-gradient(from calc(270deg - (var(--spread) * 0.5)), transparent 0, var(--shimmer-color) var(--spread), transparent var(--spread));
  animation: spin-around calc(var(--speed) * 2) infinite linear;
}

/* 文字：text-sm lg:text-lg font-medium leading-none tracking-tight text-white */
.shimmer-label { position: relative; z-index: 1; color: #fff; font-size: 14px; font-weight: 500; letter-spacing: -.025em; line-height: 1; text-align: center; white-space: pre-wrap; }

/* Highlight：inset shadow，hover / active 变化 */
.shimmer-highlight {
  position: absolute;
  width: 100%;
  height: 100%;
  border-radius: 16px;
  box-shadow: inset 0 -8px 10px #ffffff1f;
  transform: translateZ(0);
  transition: box-shadow .3s ease-in-out;
  pointer-events: none;
}
.shimmer-cta:hover .shimmer-highlight { box-shadow: inset 0 -6px 10px #ffffff3f; }
.shimmer-cta:active .shimmer-highlight { box-shadow: inset 0 -10px 10px #ffffff3f; }

/* backdrop：-z-20 [inset:var(--cut)]，露出边缘的 shimmer */
.shimmer-backdrop { position: absolute; z-index: -20; background: var(--bg); border-radius: var(--radius); inset: var(--cut); }

@keyframes shimmer-slide {
  to { transform: translate(calc(100cqw - 100%), 0); }
}
@keyframes spin-around {
  0% { transform: translateZ(0) rotate(0); }
  15%, 35% { transform: translateZ(0) rotate(90deg); }
  65%, 85% { transform: translateZ(0) rotate(270deg); }
  100% { transform: translateZ(0) rotate(360deg); }
}

@media (max-width: 767px) and (pointer: coarse) {
  .motion-toggle { display: grid; }
}

@media (min-width: 768px) {
  .fi-tile { width: 80px; height: 80px; }
  .fi-tile img { width: 40px; height: 40px; }
  .hero-content h1 { font-size: 72px; }
}

/* lg:text-lg */
@media (min-width: 1024px) { .shimmer-label { font-size: 18px; } }

:global(body[arco-theme='dark'] .landing-hero) { --landing-bg: #080a0f; --landing-ink: #f8fafc; --landing-ink-70: rgba(248,250,252,.7); --landing-muted: #8e9aaf; --tile-bg: rgba(13,17,24,.8); --tile-border: rgba(255,255,255,.1); }

@media (prefers-reduced-motion: reduce) {
  .fi-tile, .fi-enter, .shimmer-spark, .shimmer-spark-gradient { animation: none !important; }
}
</style>
