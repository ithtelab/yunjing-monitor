# 服务器市场功能设计文档

> 设计日期：2026-07-18
> 基于现有监控面板（单用户自管理架构）扩展

---

## 一、目标

允许第三方（游客）将自己的服务器通过本面板出售/出租，形成一个公开的服务器市场展示页面。

### 核心约束

- 不改变现有单用户 Admin 体验
- 复用现有 Agent 上报链路（NodeID + Bearer Token 校验）
- 引入第三种身份：节点**所有者（Owner）**
- 按区域自动分类展示

---

## 二、身份模型

| 身份 | 鉴权方式 | 来源 | 能力 |
|------|---------|------|------|
| **Admin**（现有） | cookie session `monitor_admin` | 配置密码登录 | 所有 + 市场审核/置顶/删除 |
| **Owner**（新增） | cookie session `monitor_owner` | 邮箱+密码注册登录 | 管理自己节点：状态/下架/重置 token/改销售信息 |
| **Agent**（现有） | NodeID + Bearer Token（`X-Node-ID` + `Authorization`） | 安装命令下发 | 上报指标 |

游客登录态走与 Admin 平行的 Session 管理，但 Owner 各自只能看到自己的节点。

---

## 三、游客完整流程图

```
┌─────────────────────────────────────────────────────────────────┐
│ 游客打开 /market                                               │
│   ├── 浏览市场列表（按地区分类筛选）                             │
│   └── 点“上架服务器” → /market/submit                          │
│                                                                 │
│ 上架表单：                                                       │
│   - 邮箱（必填，唯一标识）                                       │
│   - 密码（必填，注册用）                                         │
│   - 密码确认                                                     │
│   - 节点显示名（如“香港-CN2-GIA”，提取区域关键词）               │
│   - 服务器规格描述（CPU/内存/带宽等）                            │
│   - 价格                                                         │
│   - 售卖类型（出租 / 出售 / 转让）                               │
│   - 联系方式（QQ / Telegram / 邮箱）                             │
│   - 简介文本                                                     │
│   - 图形验证码（防机器人）                                       │
│                                                                 │
│ 提交后：                                                         │
│   1. 后端校验邮箱唯一 + 密码强度 + 验证码                       │
│   2. 创建 Owner 账号（邮箱 + bcrypt密码哈希）                   │
│   3. 生成 NodeID + Agent Token                                  │
│   4. store.SetNodeToken(nodeID, hashToken)                      │
│   5. 创建市场记录（owner_id, node_id, 销售字段, for_sale=true） │
│   6. 根据显示名自动归类到地区分类（"香港"→ hk 分类）            │
│   7. 返回带 token 的一键安装命令（Linux/Windows 各一条）         │
│   8. 自动登录游客面板 /market/owner                              │
│                                                                 │
│ 游客在目标服务器跑完命令 → Agent 启动上报 →                     │
│   节点出现于 /api/nodes + 市场页                                 │
│                                                                 │
│ 游客面板 /market/owner：                                         │
│   - 自己所有上架列表                                             │
│   - 在线/离线状态                                                │
│   - 重新查看安装命令（token 已哈希存，需获取原始或重置）         │
│   - 重置 Agent Token                                             │
│   - 下架（for_sale=false）                                       │
│   - 重新上架                                                     │
│   - 编辑销售信息（规格/价格/联系方式等）                         │
│   - 完全删除（取消注册）                                         │
└─────────────────────────────────────────────────────────────────┘
```

---

## 四、数据模型变更

### 4.1 节点新增字段（domain/types.go / json_store / sqlite_store）

```go
// Node extras for marketplace
type MarketplaceInfo struct {
    ForSale      bool     `json:"for_sale"`       // 是否在售
    OwnerID      string   `json:"owner_id"`       // 关联 Owner
    DisplayName  string   `json:"display_name"`   // 市场显示名
    Region       string   `json:"region"`         // 地区（如 "香港"）
    RegionID     string   `json:"region_id"`      // 地区标识（如 "hk"）
    Price        string   `json:"price"`          // 价格文本
    ListingType  string   `json:"listing_type"`   // 出租/出售/转让
    Contact      string   `json:"contact"`        // 联系方式
    Description  string   `json:"description"`    // 简介
    Pinned       bool     `json:"pinned"`         // Admin 置顶标记
    CreatedAt    int64    `json:"created_at"`
    UpdatedAt    int64    `json:"updated_at"`
}
```

### 4.2 新 Store：Owners 表

```go
type Owner struct {
    ID          string `json:"id"`           // uuid
    Email       string `json:"email"`        // 唯一
    PasswordHash string `json:"password_hash"` // bcrypt
    CreatedAt   int64  `json:"created_at"`
    LastLogin   int64  `json:"last_login"`
}
```

### 4.3 新 Store：Regions（地区分类）

```go
type Category struct {
    ID      string `json:"id"`       // "hk", "jp", "sg", "us" ...
    Name    string `json:"name"`     // "香港", "日本", "新加坡", "美国" ...
    Order   int    `json:"order"`    // 排序权重
    NodeCount int   `json:"node_count"` // 该分类在售节点数
}
```

自动创建规则：递交时从 `DisplayName` 提取关键词，命中已有分类归入，未命中自动创建。

关键词映射示例：`{"香港": "hk", "日本": "jp", "东京": "jp", "新加坡": "sg", "美国": "us", "洛杉矶": "us-la", "圣何塞": "us-sjc"}`

---

## 五、Store 接口变更

在 `application/store.go` 新增：

```go
type Store interface {
    // === 现有 ===
    ValidNodeToken(nodeID, tokenHash string) bool
    SetNodeToken(nodeID, tokenHash string, maxNodes int) error
    UpsertReport(agent.Metrics, int) error
    UpsertInfo(HostInfo) error
    Delete(name string) error
    AkileHosts() []AkileHost
    // ...

    // === 新增：市场 ===
    CreateOwner(email, password string) (*Owner, error)
    AuthenticateOwner(email, password string) (*Owner, error)
    GetOwnerByID(id string) (*Owner, error)
    
    CreateMarketListing(ownerID, nodeID, displayName, region, regionID, price, listingType, contact, description string) error
    UpdateMarketListing(nodeID string, updates map[string]interface{}) error
    GetOwnerListings(ownerID string) ([]MarketListing, error)
    GetMarketListings() ([]MarketListing, error)  // 公开市场数据
    
    ToggleForSale(nodeID string, forSale bool) error   // Owner 下架/上架
    RepaceNodeToken(nodeID string) (string, error)     // Owner 重置 token

    // Admin
    AdminPinNode(nodeID string, pinned bool) error
    AdminDeleteListing(nodeID string) error

    // Regions / Categories
    GetCategories() ([]Category, error)
    AutoCreateCategory(name, id string) error
}
```

---

## 六、后端新增 API

### 6.1 公开端点（无需登录）

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/market/listings` | 市场所有在售列表（公开数据） |
| GET | `/api/market/categories` | 可用分类列表 |
| POST | `/api/market/submit` | 游客上架（含图形验证码） |
| GET | `/api/market/captcha` | 获取图形验证码（图片 base64） |

### 6.2 Owner 端点（需 Owner Session）

| Method | Path | 功能 |
|--------|------|------|
| POST | `/api/owner/register` | Owner 注册（整合进 submit 或独立） |
| POST | `/api/owner/login` | Owner 登录 |
| POST | `/api/owner/logout` | Owner 登出 |
| GET | `/api/owner/me` | 当前 Owner 信息 |
| GET | `/api/owner/nodes` | 自己所有节点列表 |
| POST | `/api/owner/nodes/toggle` | 下架/上架（`{"node_id":"","for_sale":bool}`） |
| POST | `/api/owner/nodes/reset-token` | 重置 token（返回新 token） |
| POST | `/api/owner/nodes/update` | 更新销售信息 |
| POST | `/api/owner/nodes/delete` | 删除自己节点 |

### 6.3 Admin 端点（需 Admin Session）

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/admin/market/listings` | 所有市场列表（含隐藏） |
| POST | `/api/admin/market/pin` | 置顶/取消置顶 |
| POST | `/api/admin/market/delete` | 强制删除 |

---

## 七、前端变更

### 7.1 新增页面

- **MarketPage.vue** — 市场主页：卡片列表、地区分类筛选、搜索
- **SubmitListing.vue** — 上架表单页（含图形验证码、注册）
- **OwnerPanel.vue** — Owner 登录/管理面板（管理自己节点）
- **OwnerLogin.vue** — Owner 登录页
- **CaptchaInput.vue** — 图形验证码组件

### 7.2 路由

```js
// web/src/router.js（需新建或扩展现有）
/market            -> MarketPage
/market/submit      -> SubmitListing
/market/owner       -> OwnerPanel（需登录）
/market/owner/login -> OwnerLogin
```

### 7.3 导航栏改动

修改 `AnimeNavBar.vue`，在现有项中增加"市场"：

```
概览 管理 市场（新） 语言
```

### 7.4 组件依赖

- 需要一个图形验证码组件（纯前端 canvas + 后端校验）
- i18n 补中英文市场的文案

### 7.5 展示形式

市场卡片：

```
+-------------------------------------------+
|  📍 香港-CN2-GIA            置顶 ⭐      |
|  ───────────────────────────────────────── |
|  CPU: 4核 | 内存: 8G | 硬盘: 80G SSD      |
|  带宽: 1Gbps @ 不限流量                    |
|  ───────────────────────────────────────── |
|  💰 ¥299/月  |  出租                      |
|  📞 Telegram: @xxx                         |
|  ───────────────────────────────────────── |
|  📊 在线  |  稳定性: 7天 99.8%            |
+-------------------------------------------+
```

---

## 八、图形验证码方案

使用简化方案（零外部依赖）：

- 后端：生成图片验证码（go-captcha 库或纯 math puzzle）
- 只校验格式 + 一次有效 + 过期时间 5 分钟
- 前端：展示 base64 图片 + 输入框
- 存储：内存（验证码 pool）或 短生存时间到 store

**不配 SMTP，不实际发邮件。**

---

## 九、实施批次

### 批次 1（MVP）— 核心链路先跑通

目标和改动量对比：

| 模块 | 说明 | 后端新文件 | 前端新页面 |
|------|------|-----------|-----------|
| ① 数据层 | Owner + MarketListing + Category 存储（json_store + sqlite_store）| ~3 个新文件 | - |
| ② 公开提交 API | POST /api/market/submit（含邮箱注册、生成 NodeID/Token、市场记录）| ~2 个新文件 | - |
| ③ 市场展示 API | GET /api/market/listings + GET /api/market/categories | 复用已有文件 | - |
| ④ Owner 面板 API | POST login/GET me/GET nodes/POST toggle/reset/delete | ~2 个新文件 | - |
| ⑤ 图形验证码 | captcha handler + 校验逻辑 | ~1 个新文件 | 1 个组件 |
| ⑥ 前端市场页 | MarketPage + SubmitListing | - | 2 个页面 |
| ⑦ 前端 Owner 面板 | OwnerPanel + OwnerLogin + CaptchaInput | - | 3 个页面 |
| ⑧ 导航栏改造 | AnimeNavBar 加"市场" | - | 1 个组件改动 |
| ⑨ Admin 市场管理 | Admin handlers 新增市场 Tab | ~1 个新文件 | 集成到 admin 后台 |
| ⑩ 构建 | 重新构建 web/dist | - | - |

**实施顺序：** ① → ② → ③ → ⑤ → ④ → ⑥ → ⑦ → ⑧ → ⑨ → ⑩

建议 🟢 **从后端 (①-③) 先做起**，后端 API 可独立测试（curl），前端依赖后端就位。

---

## 十、安全注意事项

1. **Owner 密码**：必须 bcrypt 存储，不允许明文或 md5
2. **图形验证码**：服务端存储，一次校验即失效，防重放
3. **Owner Session**：与 Admin Session 平行的独立 cookie 域，不能互相串
4. **Node Token 生成**：复用 `newAgentToken()`（crypto/rand 32 字节 + base64url）
5. **安装命令泄漏**：`/api/owner/nodes/reset-token` 必须重新鉴权 Owner 身份
6. **速率限制**：公开提交端点加 IP 频率限制（如 1 次/30 秒），防刷注册
7. **CORS**：市场公开 API 不可暴露至任意 Origin，复用 `validAdminOrigin` 检查
8. **分类注入**：地区分类名称 sanitize（不能包含 html/特殊字符）

---

## 十一、现有代码参考

| 功能 | 参考文件 | 说明 |
|------|---------|------|
| Admin Session | `internal/server/session.go` | Owner Session 复用同一 SessionStore（Kind+Subject，cookie 名区分） |
| Agent Token 生成 | `internal/server/auth.go` | `newAgentToken()` 直接复用 |
| Token 存储 | `SetNodeToken` / `GetNodeToken` | 明文 token 可回看；Owner 重置复用 `issueOrReuseNodeToken` |
| 地区推断 | `internal/server/region.go` | 市场分类从 listing 的 region_code 聚合，不单独建 Category 表 |
| 公开节点列表 | `handleNodes` / `AkileHosts` | 市场列表独立 API，监控首页仍含全部节点 |
| 安装命令 | `buildInstallCommands`（market_handlers） | Admin / Owner 共用 |
| 前后端构建 | `web/` 源码 → `internal/server/web/dist/` | 构建后复制到 dist |
| 导航栏 | `web/src/components/ui/AnimeNavBar.vue` | 已添加「市场」项 → `/market` |

## 十二、落地实现（2026-07-18）

相对上文设计的裁剪：

1. **Listing 独立表/字段**，不嵌进 `HostInfo`；`DisplayName/Region` 仍走 PlannedNode。
2. **无 Category 持久化**，`GET /api/market/categories` 从在售 listing 聚合。
3. **SessionStore** 升级为 `Subject + Kind`；Admin cookie `monitor_admin`，Owner cookie `monitor_owner`。
4. **前端无 vue-router**，用路径 `/market` `/market/submit` `/market/owner` + `history.pushState`。
5. **验证码** 零依赖 PNG 位图 + 内存一次性校验。
6. **限流** 提交接口 IP 30s 一次。

### 后端文件

- `domain/types.go` — Owner / MarketListing / MarketListingView / MarketCategory
- `application/store.go` — 市场 Store 接口
- `json_store_market.go` / `sqlite_store_market.go` — 双后端
- `session.go` — 带身份 Session
- `captcha.go` — 图形验证码
- `market_handlers.go` / `owner_handlers.go` — API
- `market_test.go` — 覆盖提交/列表/Owner/验证码/持久化

### 前端文件

- `web/src/components/market/*` — MarketPage / SubmitListing / OwnerPanel / ListingCard / CaptchaInput
- `App.vue` — 路径路由 + 导航
- `AnimeNavBar.vue` — market icon
- locales — `nav-market`

### 本地验证

```bash
go get golang.org/x/crypto/bcrypt
go mod tidy
go test ./internal/server/ -count=1
cd web && npm install && npm run build
# 将 web/dist 同步到 internal/server/web/dist 后重启服务
# 打开 /market
```
