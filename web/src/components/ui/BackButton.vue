<script setup>
/**
 * BackButton —— 全站统一返回按钮
 *
 * 为什么要有它：
 *   「返回市场」曾有两种长相：OwnerPanel 的 .auth-back（图标+文字+20 行专属 CSS）
 *   和 SubmitListing 的手写「←」文字按钮。本组件统一为：左箭头图标 + 灰字，
 *   hover 浅灰底深字，新增页面一行搞定返回入口。
 *
 * 用法：
 *   <BackButton @click="emit('navigate', 'market')">返回市场</BackButton>
 *   <BackButton @click="goBack" />   <!-- 默认文案「返回」 -->
 */
import IconArrowLeft from '@arco-design/web-vue/es/icon/icon-arrow-left'
import { useI18n } from 'vue-i18n'

defineOptions({ name: 'BackButton' })

const emit = defineEmits(['click'])
const { t } = useI18n()
</script>

<template>
  <button type="button" class="back-button" @click="emit('click', $event)">
    <IconArrowLeft class="back-button__icon" />
    <span><slot>{{ t('common-back') }}</slot></span>
  </button>
</template>

<style scoped>
.back-button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 8px 4px 4px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--color-text-3, #86909c);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}

.back-button:hover {
  background: var(--color-fill-2, #f2f3f5);
  color: var(--color-text-1, #1d2129);
}

.back-button__icon {
  font-size: 16px;
}

body[arco-theme='dark'] .back-button:hover {
  background: rgba(255, 255, 255, 0.08);
}
</style>
