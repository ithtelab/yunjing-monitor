<script setup>
/**
 * SiteChangelog —— 更新日志（时间线视图）
 *
 * 内容与公告共用 /api/site/content（运维手编 content.json 的 changelog 字段，
 * 书写惯例：<h3>日期 · 标题</h3> + <ul>条目</ul>）。本组件把整段 HTML 解析成
 * 「日期 + 标题 + 条目」的结构化数据交给 AppTimeline 渲染：
 *   - <h3> 里识别到 YYYY-MM-DD（兼容 - / . 分隔）→ 新时间线条目；
 *   - 无日期的 <h3>（如「相关接口」「其它」）→ 归到时间线末尾，原顺序保留；
 *   - 一个条目都解析不到 → 回退为原始 HTML 渲染，内容怎么写都不会白屏。
 * 内容经 DOMPurify 净化（与 SiteContentPanel 同一白名单）。
 */
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import DOMPurify from 'dompurify'
import { useI18n } from 'vue-i18n'
import AppTimeline from '@/components/ui/AppTimeline.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const { t } = useI18n()

const props = defineProps({
  apiURL: { type: String, default: '' }
})

const text = ref('')
const structured = ref([])
const loading = ref(true)
const error = ref(false)

const purifyConfig = {
  ALLOWED_TAGS: ['h1','h2','h3','h4','h5','h6','p','br','hr','b','strong','i','em','u','s','del','mark','small','a','ul','ol','li','blockquote','code','pre','span','div','table','thead','tbody','tr','td','th','img'],
  ALLOWED_ATTR: ['href','title','target','rel','src','alt','style','class'],
  ALLOW_DATA_ATTR: false
}

const load = async () => {
  loading.value = true
  error.value = false
  text.value = ''
  structured.value = []
  const base = props.apiURL || ''
  try {
    const response = await axios.get(`${base}/api/site/release-notes`)
    if (!Array.isArray(response.data?.all)) throw new Error('invalid release notes response')
    structured.value = response.data.all
    if (!structured.value.length && typeof response.data?.legacy_html === 'string') {
      text.value = response.data.legacy_html
    }
  } catch {
    try {
      const response = await axios.get(`${base}/api/site/content`)
      text.value = typeof response.data?.changelog === 'string' ? response.data.changelog : ''
    } catch {
      error.value = true
    }
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.apiURL, load)

const sanitized = computed(() => DOMPurify.sanitize(text.value || '', purifyConfig))

const DATE_RE = /(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})/

const RELEASE_TYPE_KEYS = {
  feature: 'changelog-type-feature',
  fix: 'changelog-type-fix',
  improvement: 'changelog-type-improvement',
  security: 'changelog-type-security',
  maintenance: 'changelog-type-maintenance'
}

const RELEASE_TYPE_PATTERNS = [
  ['fix', /^\s*(?:修复|修正|解决|Fix(?:ed)?)/i],
  ['security', /^\s*(?:安全|加密|权限|防护|风控|Security)/i],
  ['improvement', /^\s*(?:优化|调整|改进|重构|统一|升级|改为|兼容|Improve(?:d|ment)?)/i],
  ['maintenance', /^\s*(?:文档|说明|维护|部署指引|README|Docs?)/i],
  ['feature', /^\s*(?:新增|增加|支持|接入|Add(?:ed)?|New)/i]
]

const normalizeReleaseType = (value) => (
  Object.hasOwn(RELEASE_TYPE_KEYS, value) ? value : 'improvement'
)

const classifyReleaseItem = (value, fallbackType = 'improvement') => {
  const text = String(value || '').trim()
  const matched = RELEASE_TYPE_PATTERNS.find(([, pattern]) => pattern.test(text))
  const type = matched?.[0] || normalizeReleaseType(fallbackType)
  const displayText = matched
    ? text.replace(matched[1], '').replace(/^[：:·\-—–|\s]+/, '').trim() || text
    : text
  return { type, text: displayText }
}

const appendReleaseItem = (list, value, fallbackType) => {
  const { type, text } = classifyReleaseItem(value, fallbackType)
  if (!text) return
  const item = document.createElement('li')
  const badge = document.createElement('span')
  badge.className = `release-type-badge release-type-${type}`
  badge.textContent = t(RELEASE_TYPE_KEYS[type])
  item.appendChild(badge)
  item.appendChild(document.createTextNode(text))
  list.appendChild(item)
}

function parseChangelog(html) {
  if (!html) return []
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html')
  const root = doc.body.firstElementChild
  if (!root) return []

  const items = []
  let current = null
  const flush = () => {
    if (current) items.push(current)
    current = null
  }

  Array.from(root.children).forEach((el) => {
    const tag = el.tagName.toLowerCase()
    if (tag === 'h1') return // 页面自带标题，跳过内容里的 h1

    if (tag === 'h2' || tag === 'h3' || tag === 'h4') {
      const heading = (el.textContent || '').replace(/\s+/g, ' ').trim()
      const m = heading.match(DATE_RE)
      flush()
      if (m) {
        const date = `${m[1]}-${m[2].padStart(2, '0')}-${m[3].padStart(2, '0')}`
        const title = heading.replace(m[0], '').replace(/^[·\-—–|\s]+/, '').trim()
        current = { date, title: title || t('changelog-version-update'), parts: [] }
      } else {
        current = { date: '', title: heading || t('changelog-other'), parts: [] }
      }
      return
    }

    // 首个小标题之前出现的零散内容，归入一个隐式小节，不丢弃
    if (!current) current = { date: '', title: t('changelog-update'), parts: [] }
    if (tag === 'ul' || tag === 'ol') {
      el.querySelectorAll('li').forEach((li) => current.parts.push(`<li>${li.innerHTML}</li>`))
    } else {
      current.parts.push(el.outerHTML)
    }
  })
  flush()

  // 只有含实际内容的条目才进入时间线
  return items
    .filter((it) => it.parts.length || it.title)
    .map(({ parts, ...it }) => ({
      ...it,
      html: parts.length ? `<ul>${parts.join('')}</ul>` : ''
    }))
}

const releaseItemsHTML = (values, fallbackType) => {
  const list = document.createElement('ul')
  ;(Array.isArray(values) ? values : []).forEach((value) => appendReleaseItem(list, value, fallbackType))
  return list.childElementCount ? list.outerHTML : ''
}

const items = computed(() => {
  if (structured.value.length) {
    return structured.value.map((note) => ({
      date: String(note?.date || ''),
      title: [note?.version, note?.title].filter(Boolean).join(' · ') || t('changelog-update'),
      html: releaseItemsHTML(note?.items, note?.type)
    }))
  }
  return parseChangelog(sanitized.value)
})
</script>

<template>
  <div class="site-changelog">
    <EmptyState v-if="loading" loading />
    <EmptyState v-else-if="error" :text="$t('site-content-load-error')">
      <BaseButton size="sm" @click="load">{{ $t('common-retry') }}</BaseButton>
    </EmptyState>
    <AppTimeline v-else-if="items.length" :items="items" />
    <!-- 内容格式不符合「日期小标题」惯例时的兜底：原样渲染 -->
    <div v-else-if="sanitized" class="sc-body" v-html="sanitized"></div>
    <EmptyState v-else :text="$t('site-content-empty')" />
  </div>
</template>

<style scoped lang="scss">
.site-changelog {
  max-width: 720px;
  margin: 16px auto 0;
  padding: 0 14px;

  .sc-body {
    font-size: 13px;
    line-height: 1.7;
    color: var(--color-text-3, #86909c);

    :deep(ul),
    :deep(ol) {
      padding-left: 18px;
    }

    :deep(code) {
      background: rgba(23, 33, 47, .06);
      padding: 2px 6px;
      border-radius: 4px;
    }
  }

  :deep(.release-type-badge) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 38px;
    height: 20px;
    margin: 0 8px 2px 0;
    padding: 0 7px;
    border: 1px solid transparent;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 650;
    line-height: 18px;
    vertical-align: middle;
    white-space: nowrap;
  }

  :deep(.release-type-feature) {
    color: #168344;
    background: rgba(35, 195, 101, .11);
    border-color: rgba(35, 195, 101, .2);
  }

  :deep(.release-type-fix) {
    color: #c93636;
    background: rgba(245, 63, 63, .1);
    border-color: rgba(245, 63, 63, .2);
  }

  :deep(.release-type-improvement) {
    color: #2f69c9;
    background: rgba(22, 93, 255, .1);
    border-color: rgba(22, 93, 255, .2);
  }

  :deep(.release-type-security) {
    color: #b85d0a;
    background: rgba(255, 125, 0, .11);
    border-color: rgba(255, 125, 0, .22);
  }

  :deep(.release-type-maintenance) {
    color: #5f6b7a;
    background: rgba(134, 144, 156, .12);
    border-color: rgba(134, 144, 156, .22);
  }
}

body[arco-theme='dark'] .site-changelog .sc-body :deep(code) {
  background: rgba(255, 255, 255, .1);
}

body[arco-theme='dark'] .site-changelog :deep(.release-type-feature) { color: #6edb98; }
body[arco-theme='dark'] .site-changelog :deep(.release-type-fix) { color: #ff8f8f; }
body[arco-theme='dark'] .site-changelog :deep(.release-type-improvement) { color: #8eb5ff; }
body[arco-theme='dark'] .site-changelog :deep(.release-type-security) { color: #ffb86b; }
body[arco-theme='dark'] .site-changelog :deep(.release-type-maintenance) { color: #b9c1cc; }
</style>
