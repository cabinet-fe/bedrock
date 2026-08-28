# 认证

登录、刷新、登出、当前用户。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。
个人访问令牌（PAT）的管理接口（`/resource/tokens`）已迁入资源管理域，见 [resource.md](resource.md)；auth 中间件继续消费 Bearer PAT 做鉴权（`Authorization: Bearer br_...`）。

## 认证

### POST /auth/login — 登录

认证：不需要
请求：{ username*, password_cipher, password }
响应头：写入 HttpOnly `refresh_token`（Max-Age 取 jwt.refresh_ttl，不设 Secure；Path=/api/v1/auth）
响应 200：data = LoginResponse
错误：400 / 401

### POST /auth/refresh — 刷新访问令牌

认证：不需要
请求：{ refresh_token }
响应头：轮换 HttpOnly `refresh_token`（不设 Secure；Path=/api/v1/auth）
响应 200：data = AccessTokenResponse
错误：401
说明：从 Cookie 读取 `refresh_token`（登录/刷新时 Set-Cookie）。非浏览器客户端也可在 JSON body 里传 `refresh_token`。会轮换 Cookie；响应体只返回新的 `access_token`。

### POST /auth/logout — 登出

响应头：清除 `refresh_token` Cookie
响应 200
错误：401

### GET /auth/me — 当前用户（含权限与菜单）

响应 200：data = MeResponse
错误：401 / 404

## 对象形状

### AccessTokenResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `access_token` | `string` | 是 |  |

### LoginRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `username` | `string` | 是 |  |
| `password_cipher` | `string` |  | hex(IV ‖ AES-256-CBC PKCS#7 密文)；Web 优先 |
| `password` | `string` |  | 明文密码，仅调试；Web 不得发送 |

### LoginResponse

`refresh_token` 只通过 Set-Cookie（HttpOnly）下发，不在 JSON 里返回。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `access_token` | `string` | 是 | JWT access token（Bearer），客户端自行保存 |
| `user` | `User` | 是 |  |
| `permissions` | `string[]` |  | 功能 `full_code` 列表 |
| `menus` | `MenuGroupNode[]` |  | 两层导航，对齐 `u-group-nav` |

### MeResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `user` | `User` |  |  |
| `permissions` | `string[]` |  | 功能 `full_code` 列表 |
| `menus` | `MenuGroupNode[]` |  | 两层导航，对齐 `u-group-nav` |

### MenuGroupNode

侧边栏分组；`children` 映射为 GroupNav 子项（`path` ← 前端路由）。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` |  | 分组标题 |
| `children` | `MenuItemNode[]` |  |  |

### MenuItemNode

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` |  | 菜单标题 |
| `path` | `string` |  | 前端路由 |
| `icon` | `string` |  | 可选；data URL 或 Base64 |

过滤规则：菜单 `hidden=false`、enabled、非超管去掉 `super_admin_only`、需具备 `{menuCode}:view`；空分组不返回。

PAT 对象形状（PATCreateResponse、PersonalAccessToken）见 [resource.md](resource.md)。

