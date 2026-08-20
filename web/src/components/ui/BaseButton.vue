<script setup>
/**
 * BaseButton —— 全站公共基础按钮
 *
 * 为什么要有它：
 *   历史上 .btn / .btn-primary / .btn-danger 在 MarketPage、OwnerPanel、
 *   SubmitListing 三个组件里各复制了一份，尺寸还不一致（高 34/36px、圆角 8/10px）。
 *   现在统一收口到本组件：业务页面只声明「variant + size」，不再自己写按钮样式。
 *
 * 风格：黑色主调（对齐管理后台 admin_assets.go 的 #111 按钮审美）。
 *
 * 用法示例：
 *   <BaseButton variant="primary" size="md" @click="save">保存</BaseButton>
 *   <BaseButton variant="danger" size="sm">删除</BaseButton>
 *   <BaseButton variant="text">关闭</BaseButton>
 *   <BaseButton variant="primary" size="lg" block type="submit">登录</BaseButton>
 */
defineOptions({ name: 'BaseButton' })

const props = defineProps({
  // 按钮变体：primary 黑底主按钮 | default 白底描边 | danger 红色危险 | text 纯文字
  variant: {
    type: String,
    default: 'default',
    validator: (v) => ['primary', 'default', 'danger', 'text'].includes(v)
  },
  // 尺寸：sm 34px（列表操作区）| md 36px（工具栏/表单，默认）| lg 48px（登录等大按钮）
  size: {
    type: String,
    default: 'md',
    validator: (v) => ['sm', 'md', 'lg'].includes(v)
  },
  // 是否撑满父容器宽度（登录、提交等整宽按钮）
  block: { type: Boolean, default: false },
  // 原生 type，放在 form 里需要触发提交时传 "submit"
  type: { type: String, default: 'button' },
  disabled: { type: Boolean, default: false }
})

const emit = defineEmits(['click'])

// 禁用态下拦截点击，保证 @click 不会被意外触发
function handleClick(event) {
  if (props.disabled) return
  emit('click', event)
}
</script>

<template>
  <button
    class="base-btn"
    :class="[`base-btn--${variant}`, `base-btn--${size}`, { 'base-btn--block': block }]"
    :type="type"
    :disabled="disabled"
    @click="handleClick"
  >
    <slot />
  </button>
</template>

<style scoped>
/* ========== 基础骨架（布局 + 过渡，变体只改配色） ========== */
.base-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  box-sizing: border-box;
  white-space: nowrap;
  user-select: none;
  border: 1px solid transparent;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: pointer;
  transition:
    background 0.15s ease,
    border-color 0.15s ease,
    color 0.15s ease,
    opacity 0.15s ease,
    transform 0.1s ease;
}

.base-btn:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgba(17, 17, 17, 0.15);
}

.base-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* ========== 尺寸（与原各页面按钮高度一一对应） ========== */
.base-btn--sm { height: 34px; padding: 0 12px; border-radius: 8px; font-size: 13px; }
.base-btn--md { height: 36px; padding: 0 14px; border-radius: 10px; font-size: 14px; }
.base-btn--lg { height: 48px; padding: 0 24px; border-radius: 12px; font-size: 14px; font-weight: 500; }

/* 整宽按钮 */
.base-btn--block { width: 100%; }

/* ========== 变体 ========== */
/* 主按钮：黑底白字（管理后台同款） */
.base-btn--primary {
  background: #111;
  border-color: #111;
  color: #fff;
}
.base-btn--primary:hover:not(:disabled) {
  background: #2a2a2a;
  border-color: #2a2a2a;
}

/* 默认：白底灰描边，悬停时描边与文字压黑 */
.base-btn--default {
  background: var(--color-bg-2, #fff);
  border-color: var(--color-border-2, #e5e6eb);
}
.base-btn--default:hover:not(:disabled) {
  border-color: #111;
  color: #111;
}

/* 危险：红字红描边（沿用旧 .btn-danger 配色） */
.base-btn--danger {
  background: var(--color-bg-2, #fff);
  border-color: #f98981;
  color: #f53f3f;
}
.base-btn--danger:hover:not(:disabled) {
  background: rgba(245, 63, 63, 0.06);
}

/* 文字：无边无底，灰字悬停压黑（padding 写在变体里，覆盖尺寸的内边距） */
.base-btn--text {
  background: transparent;
  border-color: transparent;
  color: var(--color-text-3, #86909c);
  padding: 0 4px;
}
.base-btn--text:hover:not(:disabled) {
  color: #111;
}

/* ========== 深色模式：主按钮反转为白底黑字 ========== */
body[arco-theme='dark'] .base-btn--primary {
  background: #f5f5f5;
  border-color: #f5f5f5;
  color: #111;
}
body[arco-theme='dark'] .base-btn--primary:hover:not(:disabled) {
  background: #fff;
  border-color: #fff;
}
body[arco-theme='dark'] .base-btn--default {
  background: transparent;
  border-color: rgba(255, 255, 255, 0.16);
}
body[arco-theme='dark'] .base-btn--default:hover:not(:disabled) {
  border-color: #f5f5f5;
  color: #f5f5f5;
}
body[arco-theme='dark'] .base-btn--text:hover:not(:disabled) {
  color: #f5f5f5;
}
</style>
