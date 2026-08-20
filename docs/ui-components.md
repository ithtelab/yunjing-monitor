# 前端公共组件手册

> 面向后续开发（人类或 AI）：**写 UI 前先查本手册，能套用的组件不要自己造轮子。**
> 所有公共组件自带详细中文注释，本文档是总索引 + 用法速查。

## 目录约定

| 位置 | 放什么 |
|---|---|
| `web/src/components/ui/` | **全站通用**组件与效果（按钮、输入框、特效…）。新增公共组件一律放这里 |
| `web/src/components/market/` | 仅服务器市场业务使用的组件 |
| `web/src/components/site/` | 仅站点内容展示（公告/日志/统计）使用的组件 |

**收编标准**：同一结构/样式/逻辑出现 **2 次及以上**，就必须提升为公共组件放进 `ui/`，禁止复制粘贴。

**硬性约定**：
- 组件必须写中文注释（头部块注释说明「为什么有它、效果、用法」）
- 深色模式一律用 `body[arco-theme='dark'] .xxx { ... }` 覆盖
- 颜色优先用 Arco CSS 变量：`var(--color-bg-2, #fff)`、`var(--color-border-2, #e5e6eb)`、`var(--color-text-1/2/3, ...)`
- 按钮主风格为**黑色系**（`#111` 底白字），对齐管理后台审美；不要再用旧的 Arco 蓝 `#165dff` 做按钮

## i18n 约定（全站已五语覆盖：zh / en / ja / ko / de）

- **所有用户可见文案必须走 i18n，禁止硬编码中文**（组件内的中文注释不受限）
- 组件内：`import { useI18n } from 'vue-i18n'` → `const { t } = useI18n()`，模板用 `t('key')` 或 `$t('key')`
- 非组件 JS（如 `utils/utils.js`）：`import i18n from '@/locales'` → `i18n.global.t('key', { n })`
- key 命名：kebab-case + 模块前缀，已有的前缀体系——
  `common-`（通用小文案）、`auth-`（登录）、`market-`（市场/卡片/教程）、`owner-`（我的上架）、
  `submit-`（上架表单）、`captcha-`（验证码）、`alert-`（到期告警）、`monitor-`（监控详情）、
  `time-`（时长/相对时间）、`landing-`（落地页）、`guide-`（市场教程弹窗）、`nav-` / `chart-`（导航/图表）
- 带变量的文案用命名插值：`t('alert-days-left', { days })` → `"剩 {days} 天"`
- 翻译里含 HTML（如 `.hl` 高亮）时，模板用 `v-html="t('guide-s1-desc')"` 渲染（仅限自家翻译，禁止渲染接口数据）
- **新增 key 必须 5 个语言文件同步**（`web/src/locales/{zh,en,ja,ko,de}.json`），缺一个就补一个
- 语言名称本身（简体中文 / English / にほんご / 한국어 / Deutsch）按惯例不翻译

## 目录约定

## 组件速查表

| 组件 | 一句话用途 | 位置 |
|---|---|---|
| [BaseButton](#basebutton) | 全站统一按钮（黑主调，4 变体 × 3 尺寸） | `ui/BaseButton.vue` |
| [ParticleButton](#particlebutton) | 点击爆粒子特效按钮（登录/重要提交用） | `ui/ParticleButton.vue` |
| [BackButton](#backbutton) | 统一返回按钮（左箭头 + 灰字） | `ui/BackButton.vue` |
| [BaseInput](#baseinput) | input / textarea / select 三合一，带聚焦环 + 光斑 + 自动增高 | `ui/BaseInput.vue` |
| [BaseDatePicker](#basedatepicker) | 可清除的日期选择器，支持键盘、深色与自动向上展开 | `ui/BaseDatePicker.vue` |
| [CopyCommandBox](#copycommandbox) | 命令展示 + 一键复制盒 | `ui/CopyCommandBox.vue` |
| [EmptyState](#emptystate) | 空态 / 加载态占位（可插引导按钮） | `ui/EmptyState.vue` |
| [glow-card](#glow-card) | 光斑跟随卡片（**全局 class，非组件**） | `ui/glow-card.css` + `.js` |
| `RegionFlag` | 跨平台一致的 SVG 地区旗帜（避免 Windows Emoji 降级为字母） | `ui/RegionFlag.vue` |
| [PremiumAuth](#premiumauth) | 卖家登录/重置密码面板 | `ui/PremiumAuth.vue` |
| [AnimeNavBar](#animenavbar) | 胶囊动画导航栏 | `ui/AnimeNavBar.vue` |
| [CaptchaInput](#captainput) | 图形验证码输入（市场专用） | `market/CaptchaInput.vue` |
| [ListingCard](#listingcard) | 服务器上架卡片（市场专用） | `market/ListingCard.vue` |

---

## BaseButton

全站唯一按钮组件。历史上 `.btn` 在三个页面各抄一份且尺寸不一致，已全部收口到此。

**效果**：黑色主调；`primary` 黑底白字 hover 变浅黑，`default` 白底灰边 hover 描边压黑，`danger` 红字红边（删除类操作），`text` 纯文字灰字 hover 压黑。深色模式下 `primary` 自动反转为白底黑字。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `variant` | String | `'default'` | `primary` 黑底主按钮 / `default` 白底描边 / `danger` 危险 / `text` 纯文字 |
| `size` | String | `'md'` | `sm` 34px（列表操作区）/ `md` 36px（工具栏表单）/ `lg` 48px（登录等大按钮） |
| `block` | Boolean | `false` | 撑满父容器宽度 |
| `type` | String | `'button'` | 原生 type，表单内提交传 `"submit"` |
| `disabled` | Boolean | `false` | 禁用（自动拦截 click） |

**Events**：`@click`

```vue
<BaseButton variant="primary" size="lg" block type="submit">登录</BaseButton>
<BaseButton size="sm" @click="edit(item)">编辑</BaseButton>
<BaseButton variant="danger" size="sm" @click="remove(item)">删除</BaseButton>
<BaseButton variant="text" @click="close">关闭</BaseButton>
```

## ParticleButton

在 BaseButton 之上加「点击成功」粒子特效（React 版 framer-motion 组件的 Vue 移植，零新增依赖）。

**效果**：点击瞬间按钮下压 `scale(.95)`，从按钮中心向外上方爆开 6 颗小圆点（奇偶左右交错、逐个延迟 0.1s），粒子在深色模式下自动反白。粒子渲染 Teleport 到 `body`，不会被父容器裁剪。

**Props**：继承 BaseButton 全部 props（`variant/size/block/type/disabled`，默认 `variant="primary"`），额外有：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `successDuration` | Number | `1000` | 粒子持续毫秒数，到点清除并派发 `success` |
| `showIcon` | Boolean | `true` | 文字后是否显示「点击」小图标（lucide 内联 SVG） |

**Events**：`@click`、`@success`（粒子播完后触发）

```vue
<ParticleButton type="submit" block size="lg" :disabled="loading" @click="login">
  登录
</ParticleButton>
```

**适用场景**：登录、提交上架等「需要正反馈」的关键按钮；普通列表操作按钮用 BaseButton 即可。

## BackButton

全站统一返回入口，替代旧时各页手写的返回按钮（`.auth-back` / 手写「←」文字）。

**效果**：左箭头图标（IconArrowLeft）+ 灰色文字，hover 变浅灰底深色字，无边无底不抢视觉。

**Props**：无。**Events**：`@click`。**插槽**：默认文案「返回」，可自定义。

```vue
<BackButton @click="emit('navigate', 'market')">返回市场</BackButton>
<BackButton @click="goBack" />
```

## BaseInput

input / textarea / select 三合一，消灭表单控件样式重复。样式融合 originui Textarea 设计 token，并内建 glow-card 光斑跟随。

**效果**：白底灰描边圆角 8px、高 38px；微阴影立体感；占位符弱化 70%；**聚焦时描边压黑 + 3px 黑色发光环**（与 BaseButton 一致）；**hover 时光斑沿边框跟随鼠标、聚焦时常亮**（替换元素不支持 `::after`，光斑挂在内部包装 div 上）；深色模式自动切暗底、光环反白。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `as` | String | `'input'` | 渲染成 `input` / `textarea` / `select` |
| `modelValue` | String/Number | `''` | `v-model` 绑定值 |
| `autogrow` | Boolean | `false` | textarea 自动增高：随内容撑高（配 `rows="1"` 用） |
| `maxRows` | Number | `0` | autogrow 封顶行数，`0` = 不封顶 |

**属性落点**：`type / placeholder / maxlength / required / rows` 等原生属性 → 内部原生控件；`class / style` → 外层包装 div（用于 `flex: 1` 等布局）。复选框用原生 `<input type="checkbox">`，不走本组件。

```vue
<BaseInput v-model="form.email" type="email" required autocomplete="email" />
<BaseInput as="textarea" v-model="form.description" rows="1" maxlength="500" autogrow :max-rows="5" />
<BaseInput as="select" v-model="form.listing_type">
  <option value="rent">出租</option>
  <option value="sale">出售</option>
</BaseInput>
```

## BaseDatePicker

服务器到期日期公共组件，`v-model` 使用 `YYYY-MM-DD` 字符串，空字符串表示未设置。支持前后翻月、今天、清除、方向键与 PageUp/PageDown；靠近屏幕底部时自动向上展开，避免被固定底栏遮挡。

```vue
<BaseDatePicker v-model="form.due_date" />
```

## CopyCommandBox

命令文本展示 + 一键复制（内置复制逻辑与消息提示，调用方零 JS）。

**效果**：灰底圆角盒内等宽字体展示命令（自动换行、超长 token 不断盒），右侧一枚复制按钮；点击写入剪贴板并弹 Arco Message 成功/失败提示。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `command` | String | `''` | 要展示并复制的命令文本 |
| `buttonText` | String | `'复制'` | 按钮文案 |

```vue
<label>Linux 安装命令</label>
<CopyCommandBox :command="installBox.linux" />
```

## EmptyState

列表/页面无数据时的统一占位，替代各页手写的 `__empty` div。

**效果**：居中灰字；`loading` 时固定显示「加载中…」；可通过默认插槽放引导操作的按钮。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `text` | String | `'暂无数据'` | 空态文案 |
| `loading` | Boolean | `false` | 加载态，优先于 `text` |

```vue
<EmptyState v-if="loading" loading />
<EmptyState v-else-if="!list.length" text="暂无在售服务器" />
<EmptyState text="你还没有上架任何服务器">
  <BaseButton variant="primary" @click="goSubmit">去上架</BaseButton>
</EmptyState>
```

## glow-card

光斑跟随卡片 —— **不是组件，是全局效果 class**。样式与鼠标监听已在 `main.js` 全局装载，任何元素直接加 class 即可。

**效果**：鼠标悬停时，一圈柔光沿卡片边框跟随鼠标移动（光心 240px 径向渐变，1.5px 边环），移出淡出；深色模式光环变白色系；触屏（`hover:none`）与减弱动效（`prefers-reduced-motion`）下自动禁用。静止零开销。

```vue
<article class="my-card glow-card">...</article>
<button class="sa-sum-card glow-card">...</button>
```

**注意事项**：
- 卡片元素需自带 `border-radius`（光环继承 `border-radius: inherit`）
- 若卡片是**滚动容器**，必须在使用处覆盖 `inset` 防幻影滚动条：
  ```css
  .my-scroll-card.glow-card::after { inset: 0; }
  ```
  （参考 `OwnerPanel.vue` 的 `.owner-auth-screen__card`）
- 实现文件：`ui/glow-card.css`（样式）+ `ui/glow-card.js`（坐标写入，单 document 委托监听）；**不要**在业务组件里自己写 mousemove

## PremiumAuth

卖家登录/重置密码面板（用于「我的上架」等需要身份验证的入口）。登录主按钮已是 ParticleButton。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `initialMode` | String | `'login'` | `login` / `reset` |
| `loading` | Boolean | `false` | 登录请求中（按钮转圈、禁用） |
| `errorMessage` | String | `''` | 顶部红色错误横幅 |
| `successMessage` | String | `''` | 顶部绿色成功横幅 |

**Events**：`@login({ email, password, rememberMe })`、`@mode-change(mode)`、`@go-submit`

```vue
<PremiumAuth
  initial-mode="login"
  :loading="loggingIn"
  :error-message="loginError"
  @login="handleLogin"
  @go-submit="emit('navigate', 'submit')"
/>
```

## AnimeNavBar

胶囊样式动画导航栏（站点顶部主导航）。

**Props**：

| Prop | 类型 | 默认 | 说明 |
|---|---|---|---|
| `items` | Array | `[]` | 导航项，元素形如 `{ key, label, icon }`；`icon` 可选值：`home / online / offline / settings / changelog / announcement / stats / market` |
| `activeKey` | String | `''` | 当前选中项 key |

**Events**：`@select(item)`

## CaptchaInput

图形验证码输入框（市场专用）：左侧输入框 + 右侧验证码图片（点击刷新）。通过 `defineExpose` 暴露 `refresh()` 供父组件在提交失败后刷新验证码。

**Props**：`apiBase`（String）、`v-model`（验证码文本）
**Events**：`@update:captcha-id`

```vue
<CaptchaInput
  ref="captchaRef"
  v-model="captchaCode"
  :api-base="apiBase"
  @update:captcha-id="captchaId = $event"
/>
```

## ListingCard

服务器上架信息卡（市场专用）：显示名、地区（含旗帜）、在线状态、规格、价格、联系方式、简介，自带 `glow-card` 光效与置顶样式。

**Props**：`item`（Object，必填）—— 上架对象，常用字段 `node_id / display_name / region / region_code / online / specs / price / contact / description / listing_type(rent|sale|transfer) / pinned`

```vue
<ListingCard v-for="item in listings" :key="item.node_id" :item="item" />
```

## MagneticLink

磁性链接容器。桌面精细指针悬停时按距离轻微跟随，离开后使用弹簧插值复位；触屏和减少动态效果模式自动禁用位移。

**Props**：`intensity`（Number，默认 `0.3`）、`range`（Number，默认 `100`）

## TextScramble

数字/短文本扰动展示。仅在文本值变化时播放，保留最终文本作为无障碍名称，减少动态效果模式直接显示结果。

**Props**：`text`（String）、`duration`（Number）、`speed`（Number）、`delay`（Number）、`characterSet`（String）

---

## 还没收编、新需求可以直接用的

- **API 请求**：业务组件直接手写 axios + `withCredentials`，可做 `utils/api.js` 统一封装

新增公共组件时，记得回本文件登记。
