<script setup>
/**
 * AppDialog —— 全站通用对话框（shadcn/Radix Dialog 的 Vue 3 移植）
 *
 * 结构：深色遮罩 + 居中卡片（右上角 X）+ 可滚动内容区 + 吸底 footer 插槽。
 * 关闭方式：X 按钮 / Esc / footer 按钮；点击遮罩不关闭（防误触，见产品决策）。
 *
 * 复用全站效果：
 *   - 内容卡片自带 glow-card 边缘发光（全局类，坐标监听 main.js 已装载）；
 *     因卡片带 overflow:hidden 裁剪圆角，按 glow-card.css 的约定覆盖 inset: 0。
 *   - footer 按钮由调用方传 BaseButton / ParticleButton，本组件不内置样式。
 *   - 打开期间锁住 body 滚动（app-dialog-open），层级 200 高于站点 header(120)。
 *
 * 用法：
 *   <AppDialog :open="show" title="市场使用指南" @close="show = false">
 *     <p>内容…</p>
 *     <template #footer>
 *       <BaseButton @click="show = false">关闭</BaseButton>
 *       <ParticleButton variant="primary" @click="ok">确定</ParticleButton>
 *     </template>
 *   </AppDialog>
 */
import { onUnmounted, watch } from 'vue'
import IconClose from '@arco-design/web-vue/es/icon/icon-close'
import { useI18n } from 'vue-i18n'

defineOptions({ name: 'AppDialog' })

const { t } = useI18n()

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' }
})
const emit = defineEmits(['close'])

const close = () => emit('close')

const onKeydown = (event) => {
  if (event.key === 'Escape' && props.open) close()
}

watch(
  () => props.open,
  (open) => {
    document.body.classList.toggle('app-dialog-open', open)
    if (open) document.addEventListener('keydown', onKeydown)
    else document.removeEventListener('keydown', onKeydown)
  },
  { immediate: true }
)

onUnmounted(() => {
  document.body.classList.remove('app-dialog-open')
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="app-dialog">
      <div class="app-dialog__overlay" aria-hidden="true"></div>
      <div
        class="app-dialog__content glow-card"
        role="dialog"
        aria-modal="true"
        :aria-label="title || undefined"
      >
        <button type="button" class="app-dialog__close" :aria-label="t('common-close')" @click="close">
          <IconClose />
        </button>
        <div v-if="title" class="app-dialog__title">{{ title }}</div>
        <div class="app-dialog__body">
          <slot />
        </div>
        <div v-if="$slots.footer" class="app-dialog__footer">
          <slot name="footer" />
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.app-dialog {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}

.app-dialog__overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.8);
  animation: app-dialog-fade-in 0.2s ease both;
}

.app-dialog__content {
  position: relative;
  display: flex;
  flex-direction: column;
  width: min(512px, 100%);
  max-height: min(640px, 80vh);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: 16px;
  background: var(--color-bg-2, #fff);
  box-shadow: 0 18px 48px rgba(15, 23, 42, 0.18);
  overflow: hidden; /* 让 body/footer 跟随圆角 */
  animation: app-dialog-zoom-in 0.2s ease both;
}

/* glow-card.css 约定：带 overflow 的卡片发光环收 inset: 0，避免被裁/幻影滚动条 */
.app-dialog__content.glow-card::after {
  inset: 0;
}

.app-dialog__close {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 4;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.6;
  transition: opacity 0.15s ease, background 0.15s ease;
}

.app-dialog__close:hover {
  opacity: 1;
  background: var(--color-fill-2, #f2f3f5);
}

.app-dialog__title {
  padding: 20px 48px 0 24px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--color-text-1, #1d2129);
}

.app-dialog__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px 24px 20px;
  overscroll-behavior: contain;
}

.app-dialog__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px;
  border-top: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-bg-2, #fff);
}

body[arco-theme='dark'] .app-dialog__content,
body[arco-theme='dark'] .app-dialog__footer {
  background: #1f1f20;
  border-color: rgba(255, 255, 255, 0.1);
}

body[arco-theme='dark'] .app-dialog__close:hover {
  background: rgba(255, 255, 255, 0.08);
}

@keyframes app-dialog-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes app-dialog-zoom-in {
  from { opacity: 0; transform: scale(0.95) translateY(8px); }
  to { opacity: 1; transform: scale(1) translateY(0); }
}

@media (prefers-reduced-motion: reduce) {
  .app-dialog__overlay,
  .app-dialog__content {
    animation: none;
  }
}
</style>

<style>
/* 打开弹窗期间锁住页面滚动（全局，非 scoped） */
body.app-dialog-open {
  overflow: hidden !important;
}
</style>
