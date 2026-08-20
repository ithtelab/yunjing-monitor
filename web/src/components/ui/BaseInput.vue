<script setup>
/**
 * BaseInput —— 全站公共表单控件（input / textarea / select 三合一）
 *
 * 为什么要有它：
 *   SubmitListing 的提交表单与 OwnerPanel 的编辑表单，曾为 input/select/textarea
 *   各写了一份逐字相同的样式。本组件统一收口，并融合了 originui Textarea 的
 *   设计 token：聚焦发光环、微阴影、弱化占位符、禁用态。
 *
 * 效果：
 *   - 白底灰描边圆角控件，深色模式自动切暗底
 *   - 聚焦时描边压黑 + 3px 黑色发光环（与 BaseButton 的 focus 环一致）
 *   - 自带光斑跟随（glow-card）：hover 光环随鼠标移动，聚焦（:focus-within）常亮
 *   - textarea 支持 autogrow：1 行起步随内容撑高，max-rows 封顶
 *
 * 用法：
 *   <BaseInput v-model="form.email" type="email" required placeholder="邮箱" />
 *   <BaseInput as="textarea" v-model="form.description" rows="1" maxlength="500" autogrow :max-rows="5" />
 *   <BaseInput as="select" v-model="form.listing_type">
 *     <option value="rent">出租</option>
 *   </BaseInput>
 *
 * 属性落点约定：
 *   - type/placeholder/maxlength/required/rows 等原生属性 → 内部原生控件
 *   - class / style → 外层包装 div（用于 flex:1 等布局场景）
 *   复选框/单选框不在本组件范围内，用原生 <input type="checkbox">。
 */
import { computed, nextTick, onMounted, useAttrs, watch } from 'vue'

defineOptions({ name: 'BaseInput', inheritAttrs: false })

const props = defineProps({
  // 渲染成哪种控件：input（默认）| textarea | select
  as: {
    type: String,
    default: 'input',
    validator: (v) => ['input', 'textarea', 'select'].includes(v)
  },
  // v-model 绑定值
  modelValue: { type: [String, Number], default: '' },
  // textarea 自动增高：随内容撑高（替代手写 rows）
  autogrow: { type: Boolean, default: false },
  // autogrow 封顶行数，0 = 不封顶
  maxRows: { type: Number, default: 0 }
})

const emit = defineEmits(['update:modelValue'])

// input/textarea 走 input 事件、select 走 change 事件，统一收敛为 v-model
function onChange(event) {
  emit('update:modelValue', event.target.value)
}

// class/style 留给外层包装 div，其余属性透传给内部原生控件
const attrs = useAttrs()
const inputAttrs = computed(() => {
  const { class: _class, style: _style, ...rest } = attrs
  return rest
})

/**
 * 自动增高（移植自 originui autogrowing textarea）：
 * 高度先归零再按 scrollHeight 撑开，maxRows 时换算行高封顶
 */
async function adjustHeight() {
  if (!props.autogrow || props.as !== 'textarea') return
  await nextTick()
  const textarea = controlRef
  if (!textarea) return
  textarea.style.height = 'auto'
  const style = window.getComputedStyle(textarea)
  const borderHeight = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth)
  const paddingHeight = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
  const lineHeight = parseFloat(style.lineHeight) || 20
  const maxHeight = props.maxRows ? lineHeight * props.maxRows + borderHeight + paddingHeight : Infinity
  textarea.style.height = `${Math.min(textarea.scrollHeight + borderHeight, maxHeight)}px`
}

// 内部控件引用（动态 component 的 ref 即原生 DOM 元素）
let controlRef = null
function setControlRef(el) {
  controlRef = el
}

watch(() => props.modelValue, adjustHeight)
onMounted(adjustHeight)
</script>

<template>
  <!-- 包装层：input/textarea/select 是替换元素，不支持 ::after 伪元素，
       光斑（glow-card）必须挂在这层 div 上；:focus-within 让聚焦时光环常亮 -->
  <div class="base-input__wrap glow-card" :class="$attrs.class" :style="$attrs.style">
    <component
      :is="as"
      :ref="setControlRef"
      class="base-input"
      :class="{ 'base-input--textarea': as === 'textarea', 'base-input--autogrow': autogrow }"
      v-bind="inputAttrs"
      :value="modelValue"
      @input="onChange"
      @change="onChange"
    >
      <slot v-if="as === 'select'" />
    </component>
  </div>
</template>

<style scoped>
.base-input__wrap {
  border-radius: 8px; /* 光斑环继承此圆角 */
}

/* 聚焦输入时光环常亮（hover 跟随由全局 glow-card 监听负责） */
.base-input__wrap:focus-within::after {
  opacity: 1;
}

.base-input {
  width: 100%;
  height: 38px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  background: var(--color-bg-2, #fff);
  color: inherit;
  font: inherit;
  font-size: 14px;
  box-sizing: border-box;
  /* originui token：微阴影 + 过渡 */
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.base-input::placeholder {
  color: var(--color-text-3, #86909c);
  opacity: 0.7;
}

/* 聚焦态：描边压黑 + 3px 发光环（黑色系，与 BaseButton 一致） */
.base-input:focus {
  outline: none;
  border-color: #111;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05), 0 0 0 3px rgba(17, 17, 17, 0.15);
}

.base-input:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

/* 多行文本：取消固定高度、上下补内边距、仅允许纵向拉伸 */
.base-input--textarea {
  height: auto;
  padding: 10px 12px;
  resize: vertical;
}

/* 自动增高模式：禁止手动拖拽，高度由脚本接管 */
.base-input--autogrow {
  resize: none;
  overflow-y: auto;
}

body[arco-theme='dark'] .base-input {
  background: #2a2a2b;
  border-color: rgba(255, 255, 255, 0.12);
  box-shadow: none;
}

body[arco-theme='dark'] .base-input:focus {
  border-color: #f5f5f5;
  box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.2);
}
</style>
