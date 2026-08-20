import { createI18n } from 'vue-i18n'
import de from './de.json'
import en from './en.json'
import ja from './ja.json'
import ko from './ko.json'
import zh from './zh.json'

export const LANGUAGE_OPTIONS = Object.freeze([
  { code: 'zh', label: '简体中文', shortLabel: '中' },
  { code: 'en', label: 'English', shortLabel: 'EN' },
  { code: 'ja', label: '日本語', shortLabel: '日' },
  { code: 'ko', label: '한국어', shortLabel: '한' },
  { code: 'de', label: 'Deutsch', shortLabel: 'DE' },
])

const messages = { de, en, ja, ko, zh }
const supported = new Set(LANGUAGE_OPTIONS.map(({ code }) => code))

export const normalizeLocale = (candidate) => {
  const code = String(candidate || '').trim().toLowerCase().split(/[-_]/)[0]
  return supported.has(code) ? code : 'zh'
}

const browserLocale = typeof navigator === 'undefined' ? '' : navigator.language
const storedLocale = typeof localStorage === 'undefined' ? '' : localStorage.getItem('locale')
const initialLocale = normalizeLocale(storedLocale || browserLocale)

if (typeof document !== 'undefined') {
  document.documentElement.lang = initialLocale
}

const i18n = createI18n({
  fallbackLocale: 'zh',
  legacy: false,
  locale: initialLocale,
  messages,
  missingWarn: false,
})

export default i18n
