/**
 * glow-card.js —— 光斑跟随卡片的坐标写入器（配合同目录 glow-card.css 使用）
 *
 * 背景：该效果曾被 6 个组件以 class="glow-card" 引用，但鼠标跟随 JS 却
 * 放在根组件 App.vue 里——子组件依赖根组件的副作用，删掉就全局失效。
 * 现收口为独立模块，由 main.js 在启动时装载一次，App.vue 不再承担此职责。
 *
 * 实现：单个 document 级 mousemove 委托监听（passive，零节流），
 * 命中 .glow-card 元素时把光标在卡片内的坐标写入 --mx / --my CSS 变量，
 * CSS 侧仅 :hover 时显示光环，因此静止/移出均无开销。
 */

let installed = false // 防止重复装载（如 HMR 场景）

export function installGlowFollow() {
  if (installed) return
  installed = true
  document.addEventListener('mousemove', (event) => {
    const card = event.target instanceof Element ? event.target.closest('.glow-card') : null
    if (!card) return
    const rect = card.getBoundingClientRect()
    card.style.setProperty('--mx', `${event.clientX - rect.left}px`)
    card.style.setProperty('--my', `${event.clientY - rect.top}px`)
  }, { passive: true })
}
