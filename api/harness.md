# Harness

智能体会话底座（opencode serve 托管）。前端只调用本域 REST 与 WebSocket，不直连 harness 后端端口。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。

本域契约是 Bedrock 对外形状，**不是** opencode 原始 REST/SSE 信封。会话以 harness 后端存储为唯一登记处（Bedrock 重启后按需懒恢复）。

## BYOK 提供商配置注入

平台「AI 服务商」中启用的提供商/模型（`api/ai.md` Providers 节）自动渲染为 opencode 的 BYOK 提供商，**无需在服务器上手工配置 opencode**：

- 渲染目标为各会话目录的 `opencode.json`（opencode 按 `location.directory` 逐目录加载）：工作区根（`GET /ai/models` 目录锚点）、每个智能体工作区（Run 前由工作区同步刷新）、每个聊天用户目录（会话/目录读取前刷新）。
- 提供商 id 为 `bedrock-p{providerID}`（稳定，不随改名漂移）；默认模型取目录排序第一的启用模型，写入配置 `model`/`small_model`，并同时作为无显式模型会话的建会话默认（防 opencode 冷启动回退到宿主机用户的第三方 provider）。
- 模型默认推理强度：`ai_agents.reasoning_effort` 渲染为该智能体工作区里所选模型的 `options.reasoningEffort`（opencode 以 `reasoning_effort` 进入每次请求）；聊天目录不设。
- 已对 opencode 1.18.x 实测的行为边界：自定义提供商的 `apiKey` **不支持** `{file:}`/`{env:}` 引用（原样发送），因此密钥以明文写入目录配置（0600，同 UID 可见 —— 与工作区 `.env` 同一威胁模型，平台无 OS 沙箱）；opencode 内置的免费模型（provider `opencode`）无法通过 `enabled_providers`/`disabled_providers` 过滤，会与 `bedrock-*` 一并列出；agent 定义 frontmatter 的模型参数不透传，故推理强度走模型级 options。

## 模块可用性

需登录。`harness.enabled=false` 时本域全部 REST 与 WS 端点（含查看类，后端不存在）返回 HTTP 503，信封 `code=503`，`message` 整串为 `harness-unavailable`；执行类端点（建会话 / 发消息 / 应答 / 打断 / WS）同样 503，**不回退** CLI 子进程执行。

## 权限

| 权限码 | 含义 | 挂载 |
| --- | --- | --- |
| `harness_chat:view` | 查看会话列表 / 详情 / 消息 / 导出 / 模型与 agent 目录；WS 订阅 | GET 全部；WS |
| `harness_chat:send` | 建会话、发消息、打断 | `POST /harness/sessions`、`POST .../messages`、`POST .../interrupt` |
| `harness_chat:approve` | 审批 / 提问应答 | `POST .../permissions/{reqId}`、`POST .../questions/{reqId}` |

权限资源 seed 在 `harness_chat` 隐藏菜单下（不进导航，仅作 API 权限挂载）。

## 错误映射

信封 `code` 为 HTTP 整数（与全局信封一致）。

| 场景 | HTTP | `message` |
| --- | --- | --- |
| 模块关闭 / 后端不可用 | 503 | 整串 `harness-unavailable` |
| 应答未命中 pending 登记表（已应答 / 自动应答 / TTL 已拒） | 409 | 整串 `harness-pending-not-found` |
| 会话不存在 | 404 | `harness-session-not-found` |
| 会话 id 非法 | 400 | 参数错误文案 |
| 请求体含 `directory` / `cwd` 等路径字段 | 400 | 参数错误文案 |

## 会话

### POST /harness/sessions — 创建会话

权限：`harness_chat:send`
请求：{ agent, model }
响应 201：data = HarnessSession
错误：401 / 403 / 400（请求指定 `directory` 或 `cwd` 等任何路径字段）/ 503（`harness-unavailable`）
说明：请求体**仅**允许 `agent?`、`model?`；工作目录由服务端生成为 `{workspace}/harness/users/user-{userID}`（客户端不可指定，防路径穿越）。`model` 为 `{ provider, id }`，可省略。同一用户目录超过上限时最旧的非归档会话被归档。

### GET /harness/sessions — 列出当前用户的会话

权限：`harness_chat:view`
查询参数：page: integer, page_size: integer
响应 200：data = HarnessSessionPage
错误：401 / 403 / 503（`harness-unavailable`）
说明：列出当前用户目录下非归档会话，最新在前。

### GET /harness/sessions/{id} — 获取会话详情

权限：`harness_chat:view`
路径参数：id*: string
响应 200：data = HarnessSessionInfo
错误：401 / 403 / 404（`harness-session-not-found`）/ 503（`harness-unavailable`）
说明：id 为 harness 会话 id（`agent_runs.harness_session_id` 同源）。

### GET /harness/sessions/{id}/messages — 拉取消息历史

权限：`harness_chat:view`
路径参数：id*: string
响应 200：data = HarnessMessagePage
错误：401 / 403 / 404 / 503（`harness-unavailable`）
说明：服务端按时间序返回（旧→新）后分页；`content` 为后端原始消息部件数组，透传不做改写（user 消息顶层 `text` 归一化为单条 `{type: text}` 部件）。

### POST /harness/sessions/{id}/messages — 发送消息

权限：`harness_chat:send`
路径参数：id*: string
请求：{ text*, delivery }
响应 202：data = HarnessPromptAck
错误：401 / 403 / 404 / 400（`text` 为空或 `delivery` 非法）/ 503（`harness-unavailable`）
说明：`delivery` 为 `'queue' | 'steer'`，缺省 `queue`。进度经 WS 统一帧推送。

### POST /harness/sessions/{id}/interrupt — 打断当前执行

权限：`harness_chat:send`
路径参数：id*: string
响应 200
错误：401 / 403 / 404 / 503（`harness-unavailable`）

### POST /harness/sessions/{id}/permissions/{reqId} — 审批应答

权限：`harness_chat:approve`
路径参数：id*: string, reqId*: string
请求：{ reply* }
响应 200
错误：401 / 403 / 404 / 409（`harness-pending-not-found`）/ 400（`reply` 非 `once` / `always` / `reject`）/ 503（`harness-unavailable`）
说明：`reply` 为 `'once' | 'always' | 'reject'`。须命中流桥 pending 登记表；`approval_mode=auto` 的会话由后端自动 `once`。成功应答**记操作日志**（`harness_permission_reply`）。

### POST /harness/sessions/{id}/questions/{reqId} — 提问应答

权限：`harness_chat:approve`
路径参数：id*: string, reqId*: string
请求：{ answers* }
响应 200
错误：401 / 403 / 404 / 409（`harness-pending-not-found`）/ 400（`answers` 形状非法）/ 503（`harness-unavailable`）
说明：`answers` 为二维字符串数组，每个子数组按序回答 `question` 帧中的一问。成功应答**记操作日志**（`harness_question_reply`）。

### GET /harness/sessions/{id}/export — 导出会话 JSONL

权限：`harness_chat:view`
路径参数：id*: string
响应 200：`Content-Type: application/x-ndjson`，每行一个消息对象（Bedrock 用 history + message 自拼，不依赖后端原生导出端点）
错误：401 / 403 / 404 / 503（`harness-unavailable`）

## 目录

### GET /harness/models — 模型目录

权限：`harness_chat:view`
响应 200：data = HarnessModel[]
错误：401 / 403 / 503（`harness-unavailable`）
说明：透传当前用户聊天目录的模型目录（`bedrock-p*` 平台提供商 + opencode 内置免费模型，见「BYOK 提供商配置注入」）。

### GET /harness/agents — agent 目录

权限：`harness_chat:view`
响应 200：data = HarnessAgent[]
错误：401 / 403 / 503（`harness-unavailable`）
说明：透传可见 agent 定义（内置 + 编译产物 `bedrock-*`）。

## WebSocket

### GET /ws/harness/sessions/{id}/events — 会话统一帧流

路径前缀为 `/ws`（非 `/api/v1`）。查询参数 `token` 携带 JWT（与 `/ws/ai/runs/{id}/logs` 相同）；校验 WebSocket Origin（CORS 配置）。

权限：`harness_chat:view`
路径参数：id*: string
查询参数：token*: string, after: integer（可选，seq 基线，缺省 0）
错误：401 / 403 / 404 / 503（`harness-unavailable`，含 `harness.enabled=false`；升级前以 HTTP 返回）

连接成功后推送 JSON text 帧（`HarnessFrame`）。顺序：

1. **回放基线**：服务端环形缓冲（每会话最近 200 帧）中 `seq > after` 的持久帧（`message_text` / `tool_call` / `tool_result` / `status`），按序推送；瞬态帧（`message_delta` 等）不回放。
2. **pending 补发**：当前未应答的 `permission` / `question` 帧各推一次（前端按 `requestId` 幂等）。
3. **实时流**：统一帧实时推送，与回放基线按 `seq` 衔接，无丢帧、无重复。

同一会话多个连接共享流桥的一个后端事件订阅，每连接各收一份，事件不多发。前端完整历史用 `GET .../messages` 拉取，WS 回放仅覆盖环形缓冲窗口。连接只读；应答经 REST 端点提交。

## 对象形状

### HarnessSession

创建响应。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 | harness 会话 id |
| `directory` | `string` | 是 | 服务端生成的工作目录；API **不接受**客户端指定 |
| `title` | `string` |  |  |
| `agent` | `string` |  |  |
| `model` | `HarnessModelRef` |  |  |

### HarnessSessionInfo

列表 / 详情项。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 |  |
| `title` | `string` |  |  |
| `directory` | `string` |  |  |
| `agent` | `string` |  |  |
| `model` | `HarnessModelRef` |  |  |
| `created_at` | `string` | 是 |  |
| `updated_at` | `string` | 是 |  |
| `archived_at` | `string` |  | 非空表示已归档 |

### HarnessSessionPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `HarnessSessionInfo[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |

### HarnessSessionCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `agent` | `string` |  |  |
| `model` | `HarnessModelRef` |  |  |

不得包含 `directory` / `cwd` 等路径字段。

### HarnessModelRef

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `provider` | `string` | 是 | 模型服务商 id |
| `id` | `string` | 是 | 模型 id |

### HarnessMessageRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `text` | `string` | 是 |  |
| `delivery` | `'queue' \| 'steer'` |  | 缺省 `queue` |

### HarnessPromptAck

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 | 用户消息 id |
| `admitted_seq` | `integer` | 是 | 入队序号 |

### HarnessPermissionReplyRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `reply` | `'once' \| 'always' \| 'reject'` | 是 |  |

### HarnessQuestionReplyRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `answers` | `string[][]` | 是 | 每个子数组按序回答一问 |

### HarnessMessagePage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `HarnessMessage[]` | 是 | 旧→新 |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |

### HarnessMessage

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 |  |
| `role` | `string` | 是 | `user` / `assistant` |
| `agent` | `string` |  |  |
| `model` | `HarnessModelRef` |  |  |
| `content` | `any[]` |  | 后端原始消息部件数组，透传；user 消息顶层 `text` 归一化为单条 `{type: text}` 部件 |

### HarnessModel

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 |  |
| `provider` | `string` | 是 |  |
| `name` | `string` |  |  |
| `family` | `string` |  |  |

### HarnessAgent

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `description` | `string` |  |  |
| `mode` | `string` |  | `primary` / `subagent` / `all` |
| `native` | `boolean` | 是 | 内置为 `true` |
| `hidden` | `boolean` | 是 |  |

### HarnessFrame

统一帧（WS text 帧 JSON）。`kind` 选择内层载荷字段（恰好一个非空）。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `seq` | `integer` | 是 | 持久帧序号；瞬态帧为 0 |
| `eventId` | `string` |  | 后端事件 id（诊断用） |
| `sessionId` | `string` | 是 |  |
| `kind` | `string` | 是 | 见下 |
| `status` | `object` |  | `kind=status` |
| `messageDelta` | `object` |  | `kind=message_delta`（瞬态，不回放） |
| `messageText` | `object` |  | `kind=message_text` |
| `reasoningDelta` | `object` |  | `kind=reasoning_delta`（瞬态） |
| `toolCall` | `object` |  | `kind=tool_call` |
| `toolResult` | `object` |  | `kind=tool_result` |
| `permission` | `object` |  | `kind=permission` |
| `question` | `object` |  | `kind=question` |

`kind` 取值：`status`（`prompt_admitted` / `prompted` / `step_started` / `step_ended` / `step_failed` / `error` / `idle`）、`message_delta`、`message_text`、`reasoning_delta`、`tool_call`、`tool_result`、`permission`、`question`。

`permission` 载荷：`{ requestId, action, resources?, save? }`，应答走 `POST .../permissions/{reqId}`。
`question` 载荷：`{ requestId, questions: [{ question, header?, options?, multiple? }] }`，应答走 `POST .../questions/{reqId}`。
