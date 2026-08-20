<template>
  <div class="site-content-panel glow-card">
    <div class="scp-title">{{ title }}</div>
    <EmptyState v-if="loading" loading />
    <EmptyState v-else-if="error" :text="$t('site-content-load-error')">
      <BaseButton size="sm" @click="load">{{ $t('common-retry') }}</BaseButton>
    </EmptyState>
    <div v-else-if="html" class="scp-body" v-html="html"></div>
    <EmptyState v-else :text="$t('site-content-empty')" />
  </div>
</template>

<script setup>
import {ref, computed, onMounted, watch} from 'vue'
import axios from 'axios'
import DOMPurify from 'dompurify'
import BaseButton from '@/components/ui/BaseButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps({
  apiURL: {type: String, default: ''},
  field: {type: String, required: true}, // 'announcement' | 'changelog'
  title: {type: String, default: ''}
})

const text = ref('')
const loading = ref(true)
const error = ref(false)

// 内容由运维手工编辑 content.json，属可信来源；仍经 DOMPurify 净化再渲染，
// 防御手误混入危险标签。允许常见排版标签与链接，禁止脚本/事件/内联脚本。
const purifyConfig = {
  ALLOWED_TAGS: ['h1','h2','h3','h4','h5','h6','p','br','hr','b','strong','i','em','u','s','del','mark','small','a','ul','ol','li','blockquote','code','pre','span','div','table','thead','tbody','tr','td','th','img'],
  ALLOWED_ATTR: ['href','title','target','rel','src','alt','style','class'],
  ALLOW_DATA_ATTR: false
}
const html = computed(() => DOMPurify.sanitize(text.value || '', purifyConfig))

const load = async () => {
  loading.value = true
  error.value = false
  try {
    const base = props.apiURL || ''
    const res = await axios.get(`${base}/api/site/content`)
    text.value = (res.data && typeof res.data[props.field] === 'string') ? res.data[props.field] : ''
  } catch (e) {
    text.value = ''
    error.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [props.apiURL, props.field], load)
</script>

<style scoped lang="scss">
.site-content-panel {
  margin: 16px auto 0;
  padding: 28px 32px;
  border: 1px solid rgba(23, 33, 47, .08);
  border-radius: 14px;
  background: #fff;
  max-width: 920px;

  .scp-title {
    font-size: 20px;
    font-weight: 800;
    margin-bottom: 20px;
    padding-bottom: 12px;
    border-bottom: 1px solid rgba(23, 33, 47, .08);
    letter-spacing: .02em;
  }

  .scp-body {
    line-height: 1.85;
    font-size: 14.5px;
    color: #1f2533;
    word-break: break-word;

    :deep(h1) { font-size: 22px; font-weight: 800; margin: 20px 0 12px; }
    :deep(h2) { font-size: 18px; font-weight: 700; margin: 18px 0 10px; }
    :deep(h3) { font-size: 16px; font-weight: 700; margin: 14px 0 8px; }
    :deep(h4), :deep(h5), :deep(h6) { font-size: 14.5px; font-weight: 700; margin: 12px 0 6px; }
    :deep(p) { margin: 8px 0; }
    :deep(ul), :deep(ol) { margin: 8px 0; padding-left: 24px; }
    :deep(li) { margin: 4px 0; }
    :deep(a) { color: #005fe7; text-decoration: none; }
    :deep(a:hover) { text-decoration: underline; }
    :deep(blockquote) {
      margin: 12px 0; padding: 10px 16px;
      border-left: 4px solid #005fe7;
      background: rgba(0, 95, 231, .06);
      color: #445064; border-radius: 6px;
    }
    :deep(code) {
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      background: rgba(23, 33, 47, .06); padding: 2px 6px; border-radius: 4px;
      font-size: 13px;
    }
    :deep(pre) {
      background: #0d1117; color: #e6edf3; padding: 14px 16px;
      border-radius: 8px; overflow-x: auto; margin: 12px 0;
      font-size: 13px; line-height: 1.6;
    }
    :deep(pre code) { background: none; padding: 0; color: inherit; }
    :deep(hr) { border: none; border-top: 1px solid rgba(23,33,47,.1); margin: 18px 0; }
    :deep(table) { border-collapse: collapse; width: 100%; margin: 12px 0; }
    :deep(th), :deep(td) { border: 1px solid rgba(23,33,47,.12); padding: 8px 12px; text-align: left; }
    :deep(th) { background: rgba(23,33,47,.04); font-weight: 700; }
    :deep(img) { max-width: 100%; border-radius: 8px; margin: 8px 0; }
    :deep(mark) { background: #fff3a3; padding: 1px 4px; border-radius: 3px; }
    :deep(.notice-intro) {
      margin-bottom: 20px;
      padding: 22px 24px;
      border: 1px solid #111;
      border-bottom: 3px solid #22c55e;
      background: #14171b;
      color: #f8fafc;
    }
    :deep(.notice-intro h2) { margin: 5px 0 7px; font-size: 20px; }
    :deep(.notice-intro p) { margin: 0; color: #c4cad3; }
    :deep(.notice-kicker) {
      color: #4ade80;
      font-size: 11px;
      font-weight: 800;
      text-transform: uppercase;
    }
    :deep(.notice-contact-grid) {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 6px 0 18px;
      border-top: 1px solid rgba(23, 33, 47, .1);
      border-bottom: 1px solid rgba(23, 33, 47, .1);
    }
    :deep(.notice-section) { padding: 18px 20px; }
    :deep(.notice-section + .notice-section) { border-left: 1px solid rgba(23, 33, 47, .1); }
    :deep(.notice-section h3) { margin: 0 0 7px; }
    :deep(.notice-section p) { margin: 0 0 12px; color: #596579; }
    :deep(.notice-actions) { display: flex; flex-wrap: wrap; gap: 8px; }
    :deep(.notice-action) {
      display: inline-flex;
      min-height: 30px;
      padding: 4px 10px;
      align-items: center;
      border: 1px solid #171717;
      border-radius: 6px;
      background: #171717;
      color: #fff;
      font-size: 12px;
      font-weight: 700;
      transition: transform .15s ease, background-color .15s ease;
    }
    :deep(.notice-action:hover) { background: #303030; text-decoration: none; transform: translateY(-1px); }
    :deep(.notice-warning) {
      margin-top: 18px;
      border-left-color: #f59e0b;
      background: #fff7ed;
      color: #7c2d12;
    }
  }
}

@media (max-width: 640px) {
  .site-content-panel {
    padding: 22px 18px;

    .scp-body :deep(.notice-contact-grid) { grid-template-columns: 1fr; }
    .scp-body :deep(.notice-section) { padding: 16px 4px; }
    .scp-body :deep(.notice-section + .notice-section) {
      border-top: 1px solid rgba(23, 33, 47, .1);
      border-left: 0;
    }
  }
}

body[arco-theme='dark'] {
  .site-content-panel {
    border-color: rgb(255 255 255 / 16%);
    background: #000;
    color: #fff;

    .scp-title { border-bottom-color: rgb(255 255 255 / 16%); }
    .scp-body { color: #e6e9ef; }
    .scp-body :deep(a) { color: #5b9dff; }
    .scp-body :deep(blockquote) { background: rgba(91, 157, 255, .1); color: #c9d4e3; border-left-color: #5b9dff; }
    .scp-body :deep(code) { background: rgb(255 255 255 / 10%); }
    .scp-body :deep(th) { background: rgb(255 255 255 / 6%); }
    .scp-body :deep(hr) { border-top-color: rgb(255 255 255 / 16%); }
    .scp-body :deep(.notice-intro) { border-color: #333; border-bottom-color: #4ade80; background: #171717; }
    .scp-body :deep(.notice-intro p),
    .scp-body :deep(.notice-section p) { color: #aeb9c8; }
    .scp-body :deep(.notice-kicker) { color: #4ade80; }
    .scp-body :deep(.notice-contact-grid),
    .scp-body :deep(.notice-section + .notice-section) { border-color: rgb(255 255 255 / 16%); }
    .scp-body :deep(.notice-action) { border-color: #e5e5e5; background: #f5f5f5; color: #111; }
    .scp-body :deep(.notice-action:hover) { background: #d4d4d4; }
    .scp-body :deep(.notice-warning) { border-left-color: #f59e0b; background: #2a1c0d; color: #fed7aa; }
  }
}
</style>
