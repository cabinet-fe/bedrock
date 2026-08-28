# DSH

DSH（DeepSeek Harness）交互会话。Bedrock 托管 DSH 子进程；前端只调用本域 REST 与 WebSocket，不直连 DSH 端口。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。

本域契约是 Bedrock 对外形状，**不是** DSH 的 JSON-RPC / SSE 原始信封。不开放 `goals.*` / `workspace.*` / `subagent.*` REST。

## 模块可用性

需登录。`GET /dsh/status` 在 `dsh.enabled=false` 时仍返回 HTTP 200，`data.enabled=false`。

其余 `/dsh/*` 与 `/ws/dsh/*`：当 `dsh.enabled=false`、进程不可用、连接失败或超时时，HTTP 503，信封 `code=503`，`message` 整串为 `dsh-unavailable`。`GET /dsh/status` 的 `degraded=true` 时，**新建**会话同样 503 `dsh-unavailable`；已有会话不因此主动断开。

## 权限与属主

| 权限码 | 含义 | 挂载 |
| --- | --- | --- |
| `dsh_chat:view` | 查看状态/会话/历史/模型；**创建与删除会话** | GET 全部；`POST /dsh/sessions`；`DELETE /dsh/sessions/{id}` |
| `dsh_chat:send` | 发消息、取消、切换模型 | `POST .../prompt`、`.../cancel`、`.../select-model` |
| `dsh_chat:approve` | 审批 / 提问应答 | `POST .../respond` |

所有带 `{id}` 的 REST 与 WS：`session.user_id` 须等于当前用户，或当前用户为超管；否则 HTTP 403，信封 `message` 整串为 `forbidden`。

## 错误映射

信封 `code` 仍为 HTTP 整数（与全局信封一致），不用 DSH 原始 error.code 作为 `code`。

| 场景 | HTTP | `message` |
| --- | --- | --- |
| DSH `session-not-found` | 404 | 整串 `dsh-session-not-found` |
| DSH `session-conflict` | 409 | 整串 `dsh-session-conflict` |
| DSH `model-unavailable` | 503 | 整串 `dsh-model-unavailable` |
| 连接失败 / 超时 / 模块关闭 / 降级后新建会话 | 503 | 整串 `dsh-unavailable` |
| DSH `cancelled` | 200 | 成功信封；`data` 含 `cancelled: true` |
| 其它 DSH `bad-request` | 400 | DSH 的 message **文本**；不透传 details 内部结构 |
| 属主失败 | 403 | 整串 `forbidden` |
| 活跃会话数达到 `max_sessions` | 400 | （参数错误，无额外专用码） |
| `POST .../respond` 未命中 pending 登记表 | 409 | 整串 `dsh-pending-not-found`（**唯一**约定，无 400 变体） |

## 状态

### GET /dsh/status — DSH 托管状态

权限：`dsh_chat:view`
响应 200：data = DshStatus
错误：401 / 403
说明：`enabled=false` 时仍为 200，且 `enabled` 为 `false`。其余字段在未启用时可省略。

## 会话

### GET /dsh/sessions — 列出当前用户的会话

权限：`dsh_chat:view`
查询参数：page: integer, page_size: integer
响应 200：data = DshSessionPage
错误：401 / 403 / 503（`dsh-unavailable`）
说明：列表项含 `title` / `status` / `updated_at` / `agent_preset`。默认仅当前用户的会话。

### POST /dsh/sessions — 创建会话

权限：`dsh_chat:view`
请求：{ title, agent_preset }
响应 201：data = DshSession
错误：401 / 403 / 400（请求指定 `cwd` 或其它路径字段；活跃会话达 `max_sessions`）/ 503（`dsh-unavailable`，含 `degraded=true`）
说明：请求体**仅**允许 `title?`、`agent_preset?`。**拒绝**请求指定 `cwd`（cwd 由服务端生成为 `{dsh.workspace_root}/{user_id}/` 下 8 位 shortId 目录）。`agent_preset` 缺省为 `standard`。

### GET /dsh/sessions/{id} — 获取会话详情

权限：`dsh_chat:view`
路径参数：id*: integer
响应 200：data = DshSession
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`）
说明：`running` 由 WS 事件驱动，REST 详情带回当前快照。

### DELETE /dsh/sessions/{id} — 删除会话

权限：`dsh_chat:view`
路径参数：id*: integer
响应 200
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`）
说明：取消 DSH 侧会话、删除本地行并清理该会话目录。

### GET /dsh/sessions/{id}/history — 拉取会话历史

权限：`dsh_chat:view`
路径参数：id*: integer
查询参数：before_seq: integer, max_messages: integer
响应 200：data = DshHistory
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`）
说明：`events[]` 每项与下方 WS 帧结构一致（同一套 `type` 与字段），不是 DSH 原始 MuxFrame 信封。WS 断线后用 `before_seq` 补拉。

### POST /dsh/sessions/{id}/prompt — 发送消息

权限：`dsh_chat:send`
路径参数：id*: integer
请求：{ content*, mode }
响应 200：data = DshAccepted
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 409（`dsh-session-conflict`）/ 400（其它 DSH bad-request）/ 503（`dsh-unavailable`）
说明：`content` 为 `DshPromptPart[]`。`mode` 为 `'queue' | 'steer'`，可省略。若 DSH 返回 `cancelled`，HTTP 200 且 `data.cancelled=true`。

### POST /dsh/sessions/{id}/cancel — 取消当前回合

权限：`dsh_chat:send`
路径参数：id*: integer
响应 200：data = DshAccepted
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`）
说明：若 DSH 返回 `cancelled`，HTTP 200 且 `data.cancelled=true`。

### GET /dsh/sessions/{id}/models — 列出本会话可用模型

权限：`dsh_chat:view`
路径参数：id*: integer
响应 200：data = DshModelCatalog
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`）

### POST /dsh/sessions/{id}/select-model — 切换本会话模型

权限：`dsh_chat:send`
路径参数：id*: integer
请求：{ provider*, model* }
响应 200
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-model-unavailable` 或 `dsh-unavailable`）/ 400（其它 DSH bad-request）

### POST /dsh/sessions/{id}/respond — 应答审批或提问

权限：`dsh_chat:approve`
路径参数：id*: integer
请求：{ rpc_id*, ok*, value, code }
响应 200：data = DshAccepted
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 409（`dsh-pending-not-found`，未命中 pending 登记表；**不要**用 400 表示该情况）/ 503（`dsh-unavailable`）/ 400（其它 DSH bad-request）
说明：`rpc_id` 取自 `approval/requested` 或 `question/requested` 帧。`ok=true` 时带 `value`（审批 outcome 如 `allowed-once` / `rejected`，或提问答案）。`ok=false` 且 `code` 为 `cancelled` 表示取消该 pending。须命中 pending 且属主匹配。`approval_mode=manual`（默认）必须人工应答；超过 `pending_ttl` 由后端按拒绝处理并广播 `approval/resolved`。`approval_mode=auto` 时审批由后端自动 `allowed-once`，前端仍可能收到 `approval/resolved`。`question/requested` **禁止**自动应答。成功应答除通用写操作中间件外另记操作日志。

## WebSocket

### GET /ws/dsh/sessions/{id}/events — 会话实时事件

路径前缀为 `/ws`（非 `/api/v1`）。查询参数 `token` 携带 JWT（与 `/ws/ai/runs/{id}/logs` 相同）；校验 WebSocket Origin（CORS 配置）。

权限：`dsh_chat:view`
路径参数：id*: integer
查询参数：token*: string
错误：401 / 403（`forbidden`）/ 404（`dsh-session-not-found`）/ 503（`dsh-unavailable`，含 `dsh.enabled=false`；升级前以 HTTP 返回）

连接成功后推送 JSON text 帧（`DshWsFrame`）。同一会话多个 WS 客户端共享**一个** DSH `events.mux` 订阅，经频道 `dsh-session:` 后接 Bedrock 会话 id 分发。后端每 30s 发送 `{ "type": "ping" }`；前端可回 `pong`，不做强制。每会话环形缓冲最近 200 条，供 WS 重连后补拉；缺口仍用 `GET .../history`。

`type` 取值：`session/event`、`approval/requested`、`approval/resolved`、`question/requested`、`question/resolved`、`session/queue`、`session/jobs`、`ping`、`error`。

## 对象形状

### DshStatus

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `enabled` | `boolean` | 是 | 配置是否启用 DSH |
| `running` | `boolean` |  | 子进程是否在跑 |
| `version` | `string` |  | 探活得到的版本 |
| `degraded` | `boolean` |  | 重启过于频繁；为 true 时禁止新建会话 |
| `port` | `integer` |  | 本机监听端口（仅 loopback） |
| `restart_count` | `integer` |  | 重启次数 |
| `last_restart_at` | `string` |  | 最近一次重启时间 |

### DshSession

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 | Bedrock 会话 id |
| `dsh_session_id` | `string` |  | DSH 侧 sessionId |
| `user_id` | `integer` |  | 属主 |
| `title` | `string` |  |  |
| `status` | `'active' \| 'archived'` |  |  |
| `agent_preset` | `string` |  | 缺省 `standard` |
| `workspace_dir` | `string` |  | 服务端生成的绝对 cwd；API **不接受**客户端指定 |
| `running` | `boolean` |  | 当前是否有进行中回合（WS 事件驱动） |
| `last_seq` | `integer` |  | 最近消费的事件 seq |
| `last_activity_at` | `string` |  |  |
| `created_at` | `string` |  |  |
| `updated_at` | `string` |  |  |

### DshSessionPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `DshSession[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |

### DshSessionCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` |  |  |
| `agent_preset` | `string` |  |  |

不得包含 `cwd`。

### DshPromptPart

`type` 为 `'text'` 时用 `text`；为 `'image'` 时用 `media_type`、`data`，`name` 可选。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | `'text' \| 'image'` | 是 |  |
| `text` | `string` |  | `type=text` |
| `media_type` | `string` |  | `type=image` |
| `data` | `string` |  | `type=image` |
| `name` | `string` |  | `type=image`，可选 |

### DshPromptRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `content` | `DshPromptPart[]` | 是 |  |
| `mode` | `'queue' \| 'steer'` |  |  |

### DshRespondRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `rpc_id` | `string` | 是 | 来自 requested 帧 |
| `ok` | `boolean` | 是 |  |
| `value` | `object` |  | `ok=true` 时的应答体 |
| `code` | `string` |  | `ok=false` 时可为 `cancelled` |

### DshAccepted

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `accepted` | `boolean` | 是 |  |
| `cancelled` | `boolean` |  | DSH 返回 `cancelled` 时为 `true` |

### DshModelCatalog

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `providers` | `DshProvider[]` |  |  |
| `models` | `DshModel[]` |  |  |

### DshProvider

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` |  |  |
| `name` | `string` |  |  |

### DshModel

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `provider` | `string` |  |  |
| `model` | `string` |  |  |

### DshSelectModelRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `provider` | `string` | 是 |  |
| `model` | `string` | 是 |  |

### DshHistory

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `events` | `DshWsFrame[]` | 是 | 与 WS 帧结构一致 |
| `has_more` | `boolean` | 是 |  |

### DshWsFrame

统一帧；字段按 `type` 出现。`session_id` 为 Bedrock 会话 id。`rpc_id` 在 `approval/requested`、`question/requested` 上回显，供 `POST .../respond` 使用。内层 `event` 与 DSH MuxFrame 对齐并归一为 snake_case，但外层仍是本契约的包装帧。

| `type` | 其它字段 |
| --- | --- |
| `session/event` | `session_id`, `event`（如 `{ type, seq, data }`，示例 `type=assistant/message`） |
| `approval/requested` | `session_id`, `rpc_id`, `approval_id`, `tool_name`, `call_id`, `reason` |
| `approval/resolved` | `session_id`, `approval_id`, `outcome` |
| `question/requested` | `session_id`, `rpc_id`, `questions`（`DshQuestion[]`） |
| `question/resolved` | `session_id`, `question_rpc_id`, `outcome` |
| `session/queue` | `session_id`, `items`（`DshQueueItem[]`） |
| `session/jobs` | `session_id`, `jobs`（`DshJob[]`） |
| `ping` | （无其它字段；后端心跳） |
| `error` | `code`（如 `dsh-unavailable`）, `message` |

### DshQuestion

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` |  |  |
| `question` | `string` |  |  |
| `options` | `any[]` |  |  |
| `multi_select` | `boolean` |  |  |

### DshQueueItem

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` |  |  |
| `placement` | `string` |  | 如 `queued` |
| `message` | `object` |  | `{ role, content }` |

### DshJob

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` |  |  |
| `kind` | `string` |  | 如 `bash` |
| `label` | `string` |  |  |
| `status` | `string` |  | 如 `running` |
