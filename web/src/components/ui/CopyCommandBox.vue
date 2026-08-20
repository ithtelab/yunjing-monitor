<script setup>
/**
 * CopyCommandBox —— 命令展示 + 一键复制盒
 *
 * 为什么要有它：
 *   安装命令（Linux / Windows）的「code 框 + 复制按钮」曾在 OwnerPanel、
 *   SubmitListing 各复制了两份（结构、样式、copyText 函数完全一致）。
 *   本组件把「展示 + 复制 + 消息提示」整体收口，业务页只传命令字符串。
 *
 * 效果：灰底圆角盒内展示等宽命令文本（自动换行），右侧一枚复制按钮，
 *       点击写入剪贴板并用 Arco Message 弹出成功/失败提示。
 *
 * 用法：
 *   <CopyCommandBox :command="installBox.linux" />
 *   <CopyCommandBox :command="cmd" button-text="复制命令" />
 */
import Message from '@arco-design/web-vue/es/message'
import { useI18n } from 'vue-i18n'
import BaseButton from './BaseButton.vue'

const props = defineProps({
  // 要展示并复制的命令文本
  command: { type: String, default: '' },
  // 复制按钮文案（留空用全局默认「复制」）
  buttonText: { type: String, default: '' }
})

const { t } = useI18n()

// 写入剪贴板并反馈结果；剪贴板 API 失败（非 HTTPS、权限拒绝）时提示手动复制
async function copy() {
  try {
    await navigator.clipboard.writeText(props.command)
    Message.success(t('common-copied'))
  } catch {
    Message.error(t('common-copy-fail'))
  }
}
</script>

<template>
  <div class="copy-command-box">
    <code>{{ command }}</code>
    <BaseButton size="sm" @click="copy">{{ buttonText || t('common-copy') }}</BaseButton>
  </div>
</template>

<style scoped>
.copy-command-box {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 10px;
  border-radius: 10px;
  background: #f7f8fa;
  border: 1px solid var(--color-border-2, #e5e6eb);
}

/* 命令文本：等宽字体、占满剩余宽度、超长 token 也能换行 */
.copy-command-box code {
  flex: 1;
  font-size: 12px;
  word-break: break-all;
  white-space: pre-wrap;
}

body[arco-theme='dark'] .copy-command-box {
  background: #2a2a2b;
  border-color: rgba(255, 255, 255, 0.12);
}
</style>
