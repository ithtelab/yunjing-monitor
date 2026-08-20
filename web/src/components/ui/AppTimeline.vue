<script setup>
/**
 * AppTimeline —— 通用时间线列表（React Timeline 的 Vue 3 移植，零新增依赖）
 *
 * 布局：
 *   桌面端  [右对齐日期] [竖线+圆点] [标题+内容]，竖线跨条目连续；
 *   移动端  日期收到标题上方，竖线+圆点在左。
 * 效果（替代原版依赖）：
 *   - framer-motion 入场 → CSS keyframes + --i 变量交错延迟；
 *   - lucide ChevronDown → 内联 SVG，展开时旋转 180°；
 *   - 最新一条（首条）圆点蓝色高亮，其余灰色。
 *
 * items: [{ date: '2026-07-19', title: '服务器市场', html?: '<ul>…</ul>', description?: '纯文本' }]
 *   - date 可解析的按时间倒序；无日期条目保持在末尾（原顺序）。
 *   - html 由调用方负责净化（本站更新日志已在 SiteChangelog 用 DOMPurify 处理）。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineOptions({ name: 'AppTimeline' })

const props = defineProps({
  items: { type: Array, default: () => [] },
  initialCount: { type: Number, default: 5 },
  // 展开/收起按钮文案（留空用全局默认）
  showMoreText: { type: String, default: '' },
  showLessText: { type: String, default: '' }
})

const { t } = useI18n()

const showAll = ref(false)

const ts = (d) => {
  const t = new Date(d).getTime()
  return Number.isFinite(t) ? t : -Infinity
}

const sorted = computed(() =>
  props.items
    .map((item, i) => ({ ...item, __i: i }))
    .sort((a, b) => ts(b.date) - ts(a.date) || a.__i - b.__i)
)

const visible = computed(() =>
  showAll.value ? sorted.value : sorted.value.slice(0, props.initialCount)
)

const hiddenCount = computed(() => Math.max(0, sorted.value.length - props.initialCount))
</script>

<template>
  <div class="tl">
    <ul class="tl-list">
      <li
        v-for="(item, i) in visible"
        :key="`${item.date}-${i}`"
        class="tl-item"
        :class="{ 'is-latest': i === 0 }"
        :style="{ '--i': i }"
      >
        <div class="tl-date">
          <time v-if="item.date" :datetime="item.date">{{ item.date }}</time>
        </div>
        <div class="tl-rail" aria-hidden="true"><span class="tl-dot"></span></div>
        <div class="tl-body">
          <h3 class="tl-title">{{ item.title }}</h3>
          <div v-if="item.html" class="tl-desc" v-html="item.html"></div>
          <p v-else-if="item.description" class="tl-desc">{{ item.description }}</p>
        </div>
      </li>
    </ul>

    <div v-if="hiddenCount > 0" class="tl-more">
      <button type="button" class="tl-more-btn" @click="showAll = !showAll">
        <span>{{ showAll ? (showLessText || t('common-collapse')) : `${showMoreText || t('common-expand-more')}（${hiddenCount}）` }}</span>
        <svg
          class="tl-chevron"
          :class="{ 'is-open': showAll }"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.tl-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.tl-item {
  display: grid;
  grid-template-columns: 110px 20px minmax(0, 1fr);
  gap: 14px;
  animation: tl-in 0.35s ease both;
  animation-delay: calc(var(--i, 0) * 60ms);
}

/* 日期列：右对齐、灰字，悬停条目时压深 */
.tl-date {
  padding-top: 2px;
  text-align: right;
  font-size: 13px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-3, #86909c);
  transition: color 0.15s ease;
}

.tl-item:hover .tl-date {
  color: var(--color-text-1, #1d2129);
}

/* 竖线轨道：贯通整条时间线 */
.tl-rail {
  position: relative;
}

.tl-rail::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 2px;
  margin-left: -1px;
  background: var(--color-border-2, #e5e6eb);
}

.tl-item:first-child .tl-rail::before {
  top: 10px;
}

.tl-item:last-child .tl-rail::before {
  bottom: auto;
  height: 10px;
}

/* 圆点：默认灰，最新一条蓝色高亮 */
.tl-dot {
  position: absolute;
  top: 5px;
  left: 50%;
  transform: translateX(-50%);
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #c9cdd4;
  border: 2px solid var(--color-bg-1, #f7f8fa);
  transition: background 0.15s ease, box-shadow 0.15s ease;
}

.tl-item.is-latest .tl-dot {
  background: #165dff;
  box-shadow: 0 0 0 3px rgba(22, 93, 255, 0.18);
}

.tl-item:hover .tl-dot {
  background: #165dff;
}

/* 内容列 */
.tl-body {
  padding-bottom: 26px;
  min-width: 0;
}

.tl-title {
  margin: 0 0 6px;
  font-size: 15px;
  font-weight: 650;
  line-height: 1.4;
  color: var(--color-text-2, #4e5969);
  transition: color 0.15s ease;
}

.tl-item.is-latest .tl-title,
.tl-item:hover .tl-title {
  color: var(--color-text-1, #1d2129);
}

.tl-desc {
  font-size: 13px;
  line-height: 1.7;
  color: var(--color-text-3, #86909c);
}

.tl-desc :deep(ul),
.tl-desc :deep(ol) {
  margin: 0;
  padding-left: 18px;
}

.tl-desc :deep(li) {
  margin-bottom: 3px;
}

.tl-desc :deep(li:last-child) {
  margin-bottom: 0;
}

.tl-desc :deep(code) {
  background: rgba(23, 33, 47, 0.06);
  padding: 2px 6px;
  border-radius: 4px;
}

.tl-desc :deep(p) {
  margin: 0 0 6px;
}

/* 展开更多 */
.tl-more {
  display: flex;
  justify-content: center;
  margin-top: 4px;
}

.tl-more-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 14px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--color-text-3, #86909c);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.tl-more-btn:hover {
  background: var(--color-fill-2, #f2f3f5);
  color: var(--color-text-1, #1d2129);
}

.tl-chevron {
  width: 15px;
  height: 15px;
  transition: transform 0.2s ease;
}

.tl-chevron.is-open {
  transform: rotate(180deg);
}

/* 移动端：日期收到标题上方 */
@media (max-width: 768px) {
  .tl-item {
    grid-template-columns: 20px minmax(0, 1fr);
    gap: 12px;
  }

  .tl-rail {
    grid-row: 1 / span 2;
    grid-column: 1;
  }

  .tl-date {
    grid-column: 2;
    padding-top: 0;
    text-align: left;
  }

  .tl-body {
    grid-column: 2;
    padding-bottom: 22px;
  }

  .tl-dot {
    top: 3px;
  }
}

/* 深色模式 */
body[arco-theme='dark'] .tl-rail::before {
  background: rgba(255, 255, 255, 0.12);
}

body[arco-theme='dark'] .tl-dot {
  background: #5a6068;
  border-color: #17171a;
}

body[arco-theme='dark'] .tl-item.is-latest .tl-dot,
body[arco-theme='dark'] .tl-item:hover .tl-dot {
  background: #7aa5ff;
}

body[arco-theme='dark'] .tl-desc :deep(code) {
  background: rgba(255, 255, 255, 0.1);
}

@media (prefers-reduced-motion: reduce) {
  .tl-item {
    animation: none;
  }

  .tl-chevron {
    transition: none;
  }
}

@keyframes tl-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
