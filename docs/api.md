# 云镜监控 API 说明

> 整理日期：2026-07-19  
> 用途：本地对接备忘 + 明天做「接口文档页」的素材  
> 范围：当前 `vps-server` 已注册的 HTTP 接口（以代码为准）

---

## 0. 通用约定

### 0.1 Base URL

本地默认：`http://127.0.0.1:3000`  
生产：部署时的 `PUBLIC_URL`（建议 HTTPS）

### 0.2 鉴权身份

| 身份 | Cookie / Header | 说明 |
|------|-----------------|------|
| **公开** | 无 | 浏览市场、拉节点列表、站点内容 |
| **Admin** | Cookie `monitor_admin` | 后台管理；写操作需同源 Origin |
| **Owner** | Cookie `monitor_owner` | 第三方上架账号；写操作需同源 Origin |
| **Agent** | Header `X-Node-ID` + `Authorization: Bearer <token>` | 节点上报 |

- Admin / Owner Session：内存存储，约 24h，进程重启失效。  
- 写接口（POST）普遍调用 `validAdminOrigin`：有 `Origin` 时须同源或在 `CORS_ORIGINS` 白名单。  
- 响应多为 JSON；错误时常为纯文本 + 对应 HTTP 状态码。

### 0.3 推荐文档页展示策略（明天做页面时）

| 分组 | 是否默认展示 | 说明 |
|------|--------------|------|
| 公开 | ✅ 展开 | 访客/卖家需要 |
| Owner | ✅ 展开 | 上架对接需要 |
| Admin | ⚠️ 默认折叠 | 避免抢前台注意力；可只给管理员 |
| Agent | ✅ 展开（简表） | 安装与运维需要 |
| 安装脚本/静态 | 可选 | 非 JSON API |

**不要做默认开放的「填参试调台」**（公网易被扫）；文档页只读即可。

---

## 1. 公开接口（无需登录）

### 1.1 市场

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/market/listings` | 在售列表；`?region=` 可按地区码/名过滤 |
| GET | `/api/market/categories` | 地区分类（从在售 listing 聚合，非独立表） |
| GET | `/api/market/captcha` | 图形验证码：`captcha_id` + base64 PNG |
| POST | `/api/market/submit` | 注册/登录 Owner + 创建节点 + 上架 + 返回安装命令 |

#### `GET /api/market/listings`

- Query：`region`（可选，如 `HK` / `香港` / `all`）  
- 仅返回 `for_sale=true`  
- 字段概要（`MarketListingView`）：

```json
{
  "node_id": "n_xxx",
  "display_name": "香港-CN2-GIA",
  "region": "香港",
  "region_code": "HK",
  "for_sale": true,
  "listing_type": "rent",
  "contact": "tg:@xxx",
  "description": "...",
  "specs": "4C8G",
  "price": "¥299/月",
  "pinned": false,
  "online": false,
  "last_seen": 0,
  "created_at": 0,
  "updated_at": 0,
  "logical_cores": 0,
  "mem_total": 0,
  "disk_total": 0
}
```

公开响应**不含** `owner_id`、token。

#### `GET /api/market/categories`

```json
[{ "id": "HK", "name": "香港", "node_count": 3 }]
```

#### `GET /api/market/captcha`

```json
{
  "captcha_id": "...",
  "captcha_image": "data:image/png;base64,..."
}
```

- 一次性校验，约 5 分钟过期。

#### `POST /api/market/submit`

- 需：Origin 合法 + 验证码 + IP 限流（约 30s 一次）  
- Body 示例：

```json
{
  "email": "seller@example.com",
  "password": "password123",
  "password_confirm": "password123",
  "display_name": "香港-CN2-GIA",
  "region": "香港",
  "specs": "4核 8G 80G",
  "price": "¥299/月",
  "listing_type": "rent",
  "contact": "tg:@xxx",
  "description": "简介",
  "captcha_id": "...",
  "captcha_code": "AB12C"
}
```

- `listing_type`：`rent` | `sale` | `transfer`（也接受中文 出租/出售/转让）  
- 邮箱已存在且密码正确：登录后继续上架；密码错误：401  
- 成功：`Set-Cookie: monitor_owner=...`，并返回 `node_id`、`linux` / `windows` 安装命令等  
- 密码：8–128 字符，bcrypt 存储  

### 1.2 监控与站点

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/nodes` | 公开节点指标列表（Akile 兼容结构，含缓存） |
| GET | `/api/site/content` | 站点公告 + 更新日志 HTML（读 `data/content.json`） |
| GET | `/config.json` | 前台配置：`socket` / `apiURL` / `siteName` / `offlineWait` / `landingEnabled` |
| GET | `/info` | 节点 HostInfo 列表（购买信息等） |
| GET | `/ws` | WebSocket，推送与 `/api/nodes` 同构的主机 JSON |

### 1.3 安装 / 下载（脚本与二进制）

| Method | Path | 功能 |
|--------|------|------|
| GET | `/install/agent-linux.sh` | Linux 安装脚本模板 |
| GET | `/install/agent-windows.ps1` | Windows 安装脚本 |
| GET | `/uninstall/agent-linux.sh` | Linux 卸载 |
| GET | `/uninstall/agent-windows.ps1` | Windows 卸载 |
| GET | `/download/<name>` | 下载内嵌 agent 二进制（名称校验） |

---

## 2. Owner 接口（Cookie `monitor_owner`）

| Method | Path | 鉴权 | 功能 |
|--------|------|------|------|
| POST | `/api/owner/login` | Origin | 邮箱+密码登录，设 cookie |
| POST | `/api/owner/logout` | Origin | 登出 |
| GET | `/api/owner/me` | Cookie（可未登录） | `{ authenticated, id, email, ... }` |
| GET | `/api/owner/nodes` | Owner | 自己的上架列表（含 has_token 等） |
| POST | `/api/owner/nodes/toggle` | Owner + Origin | `{ "node_id", "for_sale" }` |
| POST | `/api/owner/nodes/reset-token` | Owner + Origin | `{ "node_id", "reset": bool }`；返回 linux/windows 命令 |
| POST | `/api/owner/nodes/update` | Owner + Origin | 更新销售字段 / 显示名地区 / for_sale |
| POST | `/api/owner/nodes/delete` | Owner + Origin | 删除自己节点（含 listing） |

说明：

- 无独立 `POST /api/owner/register`：注册合在 `POST /api/market/submit`。  
- 所有节点写操作会校验 `listing.owner_id === 当前 Owner`，否则 404。  
- `reset-token`：`reset=false` 尽量回看已有明文 token；`true` 轮换（旧 Agent 会掉线）。

### `POST /api/owner/nodes/update` Body 示例

```json
{
  "node_id": "n_xxx",
  "display_name": "香港-CN2",
  "region": "香港",
  "listing_type": "rent",
  "contact": "tg:@xxx",
  "description": "...",
  "specs": "4C8G",
  "price": "¥199/月",
  "for_sale": true
}
```

字段均可选（除 `node_id`），按有值部分更新。

---

## 3. Admin 接口（Cookie `monitor_admin`）

### 3.1 账号与设置

| Method | Path | 功能 |
|--------|------|------|
| POST | `/api/admin/login` | `{ username, password }`，配置文件中的管理员账密 |
| POST | `/api/admin/logout` | 登出 |
| GET | `/api/admin/me` | `{ authenticated: bool }` |
| GET/POST | `/api/admin/settings` | 读/写 `site_name`、`landing_enabled` |
| GET/POST | `/api/admin/site/content` | 读/写站点公告 + 更新日志（`data/content.json`） |

POST body：

```json
{
  "announcement": "<h2>公告</h2><p>…</p>",
  "changelog": "<h3>2026-07-19 · 主题</h3><ul><li>…</li></ul>"
}
```

- 需 Admin Cookie + 写操作 Origin 校验  
- 字段为 HTML 字符串，单字段上限约 256KB 字符  
- 保存后公开 `GET /api/site/content` 立即读到新内容（无需重启）

### 3.2 节点

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/admin/nodes` | 管理端节点列表（在线、token 状态等） |
| POST | `/api/admin/nodes` | 添加 planned 节点：`node_id?` + `display_name` + `region?` |
| GET | `/api/admin/nodes/export` | 导出节点备份 JSON |
| POST | `/api/admin/nodes/import` | 导入合并备份 |
| POST | `/api/admin/install-command` | Query：`node_id`、`platform?`、`reset?`；生成安装/卸载命令 |

### 3.3 市场管理

| Method | Path | 功能 |
|--------|------|------|
| GET | `/api/admin/market/listings` | **全部** listing（含下架），响应含 `owner_id` |
| POST | `/api/admin/market/pin` | `{ "node_id", "pinned": bool }` |
| POST | `/api/admin/market/delete` | `{ "node_id", "delete_node": bool }`：`false` 仅删上架；`true` 删节点+数据 |

### 3.4 兼容旧路径（Admin）

| Method | Path | 功能 |
|--------|------|------|
| POST | `/info` | 更新 HostInfo（套餐/购买信息等），需 Admin + Origin |
| POST | `/delete` | `{ "name": "<node_id>" }` 删除节点，需 Admin + Origin |

### 3.5 后台页面

| Method | Path | 功能 |
|--------|------|------|
| GET | `/admin` | 管理后台 HTML（侧栏：节点管理 / 市场管理 / 站点内容） |
| GET | `/admin#content` | 后台 · 站点内容（公告 + 更新日志编辑） |

---

## 4. Agent 接口

| Method | Path | 鉴权 | 功能 |
|--------|------|------|------|
| GET | `/api/agent/ping` | NodeID + Bearer | 探活 |
| POST | `/api/agent/report` | NodeID + Bearer | 上报 `agent.Metrics` JSON |

Header 示例：

```http
X-Node-ID: n_xxx
Authorization: Bearer <plaintext-token>
```

Token 在中心端存 SHA-256 哈希（及可选明文供回看）；上报成功会使节点列表缓存失效。

---

## 5. 前端路由（非 API，文档页可附）

| Path | 说明 |
|------|------|
| `/` | 监控或 Landing（由设置决定） |
| `/monitor` | 监控台 |
| `/market` | 公开市场 |
| `/market/submit` | 上架表单 |
| `/market/owner` | Owner 面板 |
| `/admin` | 管理后台 |
| `/admin#nodes` | 后台 · 节点管理 |
| `/admin#market` | 后台 · 市场管理 |

---

## 6. 安全备忘（文档页可放「安全说明」折叠）

1. 路径可公开列出；**能力**靠 cookie / bcrypt / captcha / 限流。  
2. 公开 listing 会暴露联系方式、价格等——属市场产品语义。  
3. 不在文档中放真实密码、token 示例。  
4. 生产使用强 `AUTH_SECRET` / `ADMIN_PASS`、HTTPS、谨慎配置 `CORS_ORIGINS`。  
5. `POST /api/market/submit`：验证码一次一用 + IP 约 30s 限流。

---

## 7. 明天做接口页 · 建议任务清单

1. 前台新增只读页：`/docs` 或 `/api-docs`（路径模式，与 `/market` 一致）。  
2. 数据源：本文件可拆成 `web/src/data/api-docs.js` 结构化 JSON，或直接维护本 md 由构建拷贝。  
3. UI：分组卡片/表格；Admin 默认折叠；支持「复制路径」。  
4. 导航：顶栏加「接口」**或**仅市场页脚 / Admin 侧栏入口（二选一，避免过挤）。  
5. **不做**默认可用的在线试调；若以后要做，仅 Admin 可见 + 二次确认。  
6. 与 `docs/marketplace-design.md` 交叉链接。

---

## 8. 相关文件

| 文件 | 说明 |
|------|------|
| `internal/server/server.go` | 路由注册 |
| `internal/server/market_handlers.go` | 市场公开 + Admin 市场 |
| `internal/server/owner_handlers.go` | Owner |
| `internal/server/admin_handlers.go` | Admin 节点/设置/安装命令 |
| `internal/server/auth.go` | Admin / Agent 鉴权、Origin |
| `docs/marketplace-design.md` | 市场功能设计 |
| `data/content.json` | 前台公告 / 更新日志 |

---

*晚安。明天按第 7 节做页面即可。*
