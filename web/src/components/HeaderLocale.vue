<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { LANGUAGE_OPTIONS, normalizeLocale } from '@/locales'

defineProps({
  dark: { type: Boolean, default: false },
})

const { locale } = useI18n()
const root = ref(null)
const open = ref(false)

const selectedLanguage = computed(() => (
  LANGUAGE_OPTIONS.find(({ code }) => code === locale.value) || LANGUAGE_OPTIONS[0]
))

const chooseLanguage = (code) => {
  const next = normalizeLocale(code)
  locale.value = next
  document.documentElement.lang = next
  localStorage.setItem('locale', next)
  open.value = false
}

const handleOutsideClick = (event) => {
  if (!root.value?.contains(event.target)) open.value = false
}

const handleKeydown = (event) => {
  if (event.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('pointerdown', handleOutsideClick)
  document.addEventListener('keydown', handleKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleOutsideClick)
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <div ref="root" class="language-picker" :class="{ 'is-dark': dark }">
    <button
      type="button"
      class="language-picker__trigger"
      :aria-expanded="open"
      aria-haspopup="menu"
      :title="selectedLanguage.label"
      @click="open = !open"
    >
      <icon-language aria-hidden="true" />
      <span>{{ selectedLanguage.shortLabel }}</span>
    </button>

    <Transition name="language-menu">
      <div v-if="open" class="language-picker__menu" role="menu">
        <button
          v-for="language in LANGUAGE_OPTIONS"
          :key="language.code"
          type="button"
          role="menuitemradio"
          :aria-checked="locale === language.code"
          :class="{ 'is-selected': locale === language.code }"
          @click="chooseLanguage(language.code)"
        >
          <span class="language-picker__code">{{ language.shortLabel }}</span>
          <span>{{ language.label }}</span>
          <icon-check v-if="locale === language.code" aria-hidden="true" />
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.language-picker {
  position: relative;
}

.language-picker__trigger {
  display: inline-flex;
  min-width: 54px;
  width: auto;
  height: 32px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 9px;
  border: 1px solid rgba(38, 49, 65, .14);
  border-radius: 16px;
  background: rgba(255, 255, 255, .88);
  color: #273142;
  cursor: pointer;
  font: inherit;
  font-size: 11px;
  font-weight: 700;
  box-shadow: 0 2px 8px rgba(24, 34, 48, .04);
  backdrop-filter: blur(12px);
  transition: border-color .16s ease, background .16s ease, box-shadow .16s ease;
}

.language-picker__trigger:hover,
.language-picker__trigger[aria-expanded='true'] {
  border-color: rgba(23, 105, 224, .35);
  background: #fff;
  box-shadow: 0 5px 16px rgba(25, 38, 58, .1);
}

.language-picker__trigger svg {
  width: 17px;
  height: 17px;
}

.language-picker__menu {
  position: absolute;
  z-index: 1200;
  top: calc(100% + 8px);
  right: 0;
  width: 166px;
  padding: 6px;
  border: 1px solid #e4e8ee;
  border-radius: 11px;
  background: #fff;
  box-shadow: 0 16px 42px rgba(25, 34, 48, .16);
}

.language-picker__menu button {
  display: grid;
  width: 100%;
  min-height: 36px;
  grid-template-columns: 28px 1fr 18px;
  align-items: center;
  padding: 0 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: #394454;
  cursor: pointer;
  font: inherit;
  font-size: 13px;
  text-align: left;
}

.language-picker__menu button:hover,
.language-picker__menu button.is-selected {
  background: #f2f5f8;
  color: #111827;
}

.language-picker__code {
  color: #7a8696;
  font-size: 10px;
  font-weight: 800;
}

.language-picker__menu svg {
  width: 15px;
  height: 15px;
}

.language-menu-enter-active,
.language-menu-leave-active {
  transition: opacity .14s ease, transform .14s ease;
}

.language-menu-enter-from,
.language-menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.language-picker.is-dark .language-picker__trigger {
  border-color: #333840;
  background: rgba(9, 10, 12, .94);
  color: #f5f7fa;
  box-shadow: 0 4px 18px rgba(0, 0, 0, .22);
}

.language-picker.is-dark .language-picker__trigger:hover,
.language-picker.is-dark .language-picker__trigger[aria-expanded='true'] {
  border-color: #4e6f9e;
  background: #15181c;
}

.language-picker.is-dark .language-picker__menu {
  border-color: #30343a;
  background: #101113;
  color: #f5f7fa;
  box-shadow: 0 18px 46px rgba(0, 0, 0, .45);
}

.language-picker.is-dark .language-picker__menu button {
  color: #d7dbe1;
}

.language-picker.is-dark .language-picker__menu button:hover,
.language-picker.is-dark .language-picker__menu button.is-selected {
  background: #22252a;
  color: #fff;
}
</style>
