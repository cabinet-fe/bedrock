# API 规范

## 路由与数据格式

- HTTP API 统一使用 `/api/v1` 前缀，WebSocket 使用 `/ws`。
- JSON 字段命名统一使用 `snake_case`，前后端 DTO 和枚举值保持一致。
- 所有响应使用 `{ code, message, data? }` 信封；错误响应还应包含 `request_id`。
- 异步创建返回 `202`，并在 `data` 中提供资源 ID 和当前状态。
- 需要防止重复提交的写接口应支持 `Idempotency-Key`。

### 信封里的 `code`

- 成功：`code` 为 `0`；`message` 为 `success`（200）或 `created`（201）。
- 失败：`code` 等于对应的 HTTP 状态码（如 400、401、403），并带上 `request_id`。

常见错误码含义如下:

| HTTP    | 场景                           |
| ------- | ------------------------------ |
| 400     | 参数/JSON/登录 cipher 无效     |
| 401     | 未认证、密钥错误、PAT 无效     |
| 403     | RBAC/ACL/超管门控/PAT scope    |
| 404     | 资源不存在                     |
| 409     | 版本冲突、状态不允许、引用冲突 |
| 422     | 语义校验失败（如缺 SKILL.md）  |
| 429     | 限流（可选，首期可占位）       |
| 500/503 | 内部错误/依赖不可用            |

## 列表与分页

- 分页请求使用 `page` 和 `page_size`，后端统一通过 `internal/pkg` 中的分页工具解析。
- 分页结果放在响应的 `data` 中，字段为 `items`、`total`、`page`、`page_size` 和 `total_pages`。
- 不分页的列表直接作为 `data` 返回，不需要额外包装。

## HTTP 认证

- 请求通过 `Authorization: Bearer <access_token>` 携带访问令牌。
- `refresh_token` 由服务端写入 HttpOnly Cookie；有效期取 `jwt.refresh_ttl`，未配置时为 7 天。为兼容 HTTP 部署，该 Cookie 不设置 `Secure`。
- 收到 `401` 后，客户端调用 `POST /auth/refresh`，刷新成功后重试原请求。
- 前端统一使用基于 `@cat-kit/http` 的客户端，并启用 `credentials: true`。
