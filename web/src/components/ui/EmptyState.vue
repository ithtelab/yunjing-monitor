<script setup>
/**
 * EmptyState —— 全站统一空态 / 加载态占位
 *
 * 为什么要有它：
 *   「加载中…」「暂无 xxx」曾分散在各页面手写 div + 雷同 CSS（居中灰字）。
 *   本组件统一观感，并支持通过默认插槽放一个引导操作的按钮。
 *
 * 效果：居中灰字占位；loading 时固定显示「加载中…」。
 *
 * 用法：
 *   <EmptyState v-if="loading" loading />
 *   <EmptyState v-else-if="!list.length" text="暂无在售服务器" />
 *   <EmptyState text="你还没有上架任何服务器">
 *     <BaseButton variant="primary" @click="goSubmit">去上架</BaseButton>
 *   </EmptyState>
 */
import { useI18n } from 'vue-i18n'

const props = defineProps({
  // 空态文案（留空则用全局默认「暂无数据」）
  text: { type: String, default: '' },
  // 加载态：优先于 text，固定显示「加载中…」
  loading: { type: Boolean, default: false }
})

const { t } = useI18n()
</script>

<template>
  <div class="empty-state">
    <p class="empty-state__text">{{ loading ? t('common-loading') : (text || t('common-no-data')) }}</p>
    <!-- 可选的引导操作区：放按钮等，如「去上架」 -->
    <div v-if="$slots.default" class="empty-state__actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  text-align: center;
  padding: 48px 12px;
  color: var(--color-text-3, #86909c);
  font-size: 14px;
}

.empty-state__text {
  margin: 0;
}

.empty-state__actions {
  margin-top: 12px;
  display: flex;
  justify-content: center;
  gap: 8px;
}
</style>
