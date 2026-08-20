import { createApp } from 'vue'
import Button from '@arco-design/web-vue/es/button'
import Dropdown from '@arco-design/web-vue/es/dropdown'
import Grid from '@arco-design/web-vue/es/grid'
import Progress from '@arco-design/web-vue/es/progress'
import Space from '@arco-design/web-vue/es/space'
import IconArrowDown from '@arco-design/web-vue/es/icon/icon-arrow-down'
import IconArrowUp from '@arco-design/web-vue/es/icon/icon-arrow-up'
import IconCheck from '@arco-design/web-vue/es/icon/icon-check'
import IconDownCircle from '@arco-design/web-vue/es/icon/icon-down-circle'
import IconLanguage from '@arco-design/web-vue/es/icon/icon-language'
import IconMoonFill from '@arco-design/web-vue/es/icon/icon-moon-fill'
import IconQuestionCircle from '@arco-design/web-vue/es/icon/icon-question-circle'
import IconSunFill from '@arco-design/web-vue/es/icon/icon-sun-fill'
import IconUpCircle from '@arco-design/web-vue/es/icon/icon-up-circle'
import App from './App.vue'
import i18n from '@/locales'
import { installGlowFollow } from '@/components/ui/glow-card'
import '@/components/ui/glow-card.css'
import 'flag-icons/css/flag-icons.min.css'
import '@arco-design/web-vue/es/button/style/css.js'
import '@arco-design/web-vue/es/dropdown/style/css.js'
import '@arco-design/web-vue/es/grid/style/css.js'
import '@arco-design/web-vue/es/message/style/css.js'
import '@arco-design/web-vue/es/progress/style/css.js'
import '@arco-design/web-vue/es/space/style/css.js'

const arcoPlugins = [Button, Dropdown, Grid, Progress, Space]
const arcoIcons = {
  IconArrowDown,
  IconArrowUp,
  IconCheck,
  IconDownCircle,
  IconLanguage,
  IconMoonFill,
  IconQuestionCircle,
  IconSunFill,
  IconUpCircle,
}

const installDocumentMetadata = () => {
  if (!document.querySelector('link[rel="manifest"]')) {
    const manifest = document.createElement('link')
    manifest.rel = 'manifest'
    manifest.href = '/manifest.webmanifest'
    document.head.append(manifest)
  }

  let themeColor = document.querySelector('meta[name="theme-color"]')
  if (!themeColor) {
    themeColor = document.createElement('meta')
    themeColor.name = 'theme-color'
    document.head.append(themeColor)
  }
  themeColor.content = '#111111'
}

const registerServiceWorker = () => {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch((error) => {
      console.warn('Service worker registration failed:', error)
    })
  }, { once: true })
}

installDocumentMetadata()
installGlowFollow()

const app = createApp(App)
app.use(i18n)
arcoPlugins.forEach((plugin) => app.use(plugin))
Object.entries(arcoIcons).forEach(([name, component]) => app.component(name, component))
app.mount('#app')

registerServiceWorker()
