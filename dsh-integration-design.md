# DSH 交互会话集成设计(Bedrock 托管模式)

> 状态:设计稿(v1,待评审)
> 适用范围:Bedrock 2.0(fresh install only)
> 关联文档:[DESIGN.md](./.agents/docs/DESIGN.md)、[PRD.md](./.agents/docs/PRD.md)、[api/README.md](./api/README.md)

## 1. 背景与目标

### 1.1 背景

网页系统(B/S 架构)需要 DSH(DeepSeek Harness)的完整 Agent 能力:**过程可见、可审批、可追问、可打断**,而不是命令行一次性执行(`dsh headless` 无交互)。

DSH 的 `dsh --profile web` 本身就是 HTTP 服务端:官方浏览器 GUI 只是其 `/api/*`(JSON RPC)+ `/api/events.mux`(SSE)协议的一个客户端。因此接入方 = 实现一个"替代浏览器"的客户端。

### 1.2 决策记录

| 决策         | 结论                                                          | 理由                                                                                               |
| ------------ | ------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| 反向代理     | **不用 Nginx**                                                | 由 Bedrock 服务端直接实现全部接入(进程托管、API 客户端、鉴权、流桥接),单一二进制部署形态不变       |
| DSH 进程托管 | **Bedrock 托管**                                              | 启动拉起、健康探活、崩溃自动重启、优雅关闭;避免外部 systemd 依赖                                   |
| 集成形态     | **新增交互会话模块,不动现有 `internal/ai`**                   | 现有 ai_agents 是"异步 Run + CLI 子进程"模型,交互会话是"长连接 + 实时流"模型,语义不同;并存互不干扰 |
| 前端         | 本次仅后端;**前端组件在 ultra-ui 项目**,按本文第 9 节契约对接 | 后端先行,契约先行                                                                                  |
| 本次交付     | **仅方案文档,不实现**                                         | 实现按第 11 节里程碑推进                                                                           |

### 1.3 目标

1. Bedrock 启动时自动拉起 DSH 子进程(仅绑 `127.0.0.1`),提供健康状态查询与自动恢复;
2. 新增 `/api/v1/dsh/*` 交互会话 API:建会话、发消息、历史、取消、模型选择、审批/提问应答;
3. 新增 `/ws/dsh/*` WebSocket,把 DSH 的 SSE 实时事件桥接给前端(复用现有 ws.Hub 与 token 鉴权模式);
4. 复用现有 JWT + RBAC 鉴权,会话按用户隔离;审批与提问默认人工确认(可配置为自动放行);
5. 全程不暴露 DSH 端口到公网:DSH 只被 Bedrock 进程内访问。

---

## 2. 总体架构

```
┌──────────────────────────────────────────────────────────────┐
│  Bedrock Server(单体二进制, 唯一对外入口)                     │
│                                                              │
│  ┌────────────┐   ┌──────────────────────┐  ┌─────────────┐  │
│  │ REST/WS    │──▶│ internal/dsh         │  │ DSH 子进程   │  │
│  │ handler    │   │  ├ service/process   │──▶│ dsh --profile│  │
│  │ (Gin)      │   │  ├ service/client    │──▶│ web --port X │  │
│  │ JWT + RBAC │   │  ├ service/session   │  │ --no-open   │  │
│  │            │   │  ├ service/stream    │──▶│ (127.0.0.1) │  │
│  └────────────┘   │  └ repository/model   │  └─────────────┘  │
│        ▲          └──────────────────────┘        ▲           │
│        │                    │                     │           │
│        │         JSON RPC + SSE(进程内 HTTP)       │           │
│        └────────────────────┼─────────────────────┘           │
│                             │                                 │
└─────────────────────────────┼─────────────────────────────────┘
                              │ 仅 127.0.0.1, 无公网端口
┌─────────────────────────────┴─────────────────────────────────┐
│ 前端(ultra-ui)                                                │
│  /api/v1/dsh/* (REST) + /ws/dsh/sessions/:id/events (WS)      │
└────────────────────────────────────────────────────────────────┘
```

数据流(核心场景):

```
用户 → POST /api/v1/dsh/sessions/{id}/prompt
     → session service → DSH POST /api/session.prompt → {accepted:true}
     → 后端挂 /api/events.mux (SSE) 收实时帧
     → 桥接到 ws.Hub 通道 dsh-session:<id> → 前端 WS 收到事件帧
     → (若 agent 请求批准/提问) 前端确认 → POST .../respond
     → 后端调 DSH POST /api/respond → agent 继续
```

---

## 3. DSH 进程托管(service/process)

### 3.1 生命周期

| 阶段     | 行为                                                                                                                                                                                                                    |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 启动     | `dsh.enabled=true` 时,Bedrock 在 HTTP 监听前拉起子进程:`<bin> --profile web --port <port> --no-open`;注入 `DSH_HOME=<dsh.home>`、工作目录 `cwd=<dsh.workspace_root>`;stdout/stderr 写 `{log_dir}/dsh/dsh.log`(按天轮转) |
| 就绪探活 | 轮询 `POST /api/host.describe`(本地信封),`startup_timeout` 内成功即就绪;失败则 `logger.Fatal`(与 DB 连通性同级,拒绝带病启动)                                                                                            |
| 运行监控 | 每 `health_interval` 探活一次;连续失败 ≥3 次或进程退出 → 判定异常                                                                                                                                                       |
| 自动重启 | 异常时先 SIGTERM 旧进程,再按启动流程拉起;重启计数与最近重启时间暴露在状态接口;连续重启 ≥5 次/10 分钟则进入 `degraded`(仅影响新会话,已有 DSH 会话由 DSH 端持久化可恢复)                                                  |
| 优雅关闭 | Bedrock 收到 SIGTERM 时先 SIGTERM DSH 子进程,等待退出(超时 10s 后 SIGKILL)                                                                                                                                              |
| 端口冲突 | 启动前探测 `<port>` 是否被占;被占且非本进程 → 启动失败(配置错误,拒绝带病启动)                                                                                                                                           |

### 3.2 关键约束

- **`--host 0.0.0.0` 被 DSH 故意拒绝**(源码级限制,防 RCE 暴露),Bedrock 托管天然只能走 `127.0.0.1` + 信任围栏(Host 头为 loopback 即通过),这是设计意图,不做绕过。
- DSH 的 HTTP 客户端必须由 Bedrock 进程内实现(`service/client`),**禁止**把 DSH 端口配置成公网监听或映射出去。
- DSH 侧会话持久化在 `$DSH_HOME/sessions`,Bedrock 重启后可通过 `session.create` 传原 `sessionId` 恢复。

---

## 4. DSH API 客户端(service/client)

### 4.1 协议要点(已在 DSH v0.1.1-rc.2 源码核实)

- 统一信封:`POST /api/<method>` 请求 `{type:"client-request", rpcId, method, payload}`;响应 `{type:"server-response", rpcId, result:{ok:true, value}|{ok:false, error:{code,message,details}}}`。
- 实时流:`GET /api/events.mux` → `text/event-stream`;首帧注释 `: connected`,其后每帧 `data: {rpcId, payload:<MuxFrame>}\n\n`。
- 应答:`POST /api/respond` 请求 `{type:"client-response", rpcId, result:{ok:true, value}}`,返回 `{accepted:true|false, reason?}`。

### 4.2 客户端结构(单文件收敛契约,便于 DSH 版本升级时集中适配)

```text
internal/dsh/service/client.go
├── RpcClient
│   ├── Call(ctx, method string, payload any) (any, error)   // 信封编解码 + rpcId 原子自增
│   ├── CallRaw(ctx, method string, payload any, out any) error  // 强类型解码
│   └── Respond(ctx, rpcID, result) (accepted bool, reason string, err error)
├── MuxClient(长连接)
│   ├── Open(ctx, onFrame func(MuxFrame)) error               // SSE 解析 + 指数退避重连
│   ├── PendingApproval / PendingQuestion 登记表(rpcId → 会话上下文)
│   └── LastSeq 追踪(断线后 history 补拉对齐)
└── methods.go   // 方法名常量 + payload/value 结构体(仅声明本模块用到的子集)
```

### 4.3 方法映射表(本模块用到的最小集合)

| DSH 方法                                    | 用途                | 备注                                                        |
| ------------------------------------------- | ------------------- | ----------------------------------------------------------- |
| `host.describe`                             | 健康探活 / 版本信息 | 状态接口透出                                                |
| `session.create`                            | 建会话/恢复         | payload `{cwd, sessionId?, agentPreset?}`                   |
| `session.list`                              | 会话列表(可选)      | 后端以自己 DB 为准,DSH 侧仅用于对账                         |
| `session.history`                           | 历史分页            | `{sessionId, beforeSeq?, maxMessages?}`                     |
| `session.prompt`                            | 发消息              | `{sessionId, mode:"queue"\|"steer", content:[text\|image]}` |
| `session.cancel`                            | 取消当前回合        | `{sessionId}` → `{accepted:true}`                           |
| `session.models` / `session.selectModel`    | 模型选择            | 每会话独立                                                  |
| `agentPresets.list` / `agentPresets.select` | agent 形态/工具集   | 建会话时可指定                                              |
| `llm.providers` / `llm.models`              | 供应商与模型目录    | 只读,权限 `dsh_chat:view`                                   |

`goals.*`、`workspace.*`、`subagent.*` 等域 v1 不开放,后续按需扩展(扩方法 = 在 methods.go 加一行 + handler 加路由)。

### 4.4 错误映射

| DSH 错误码          | Bedrock 对外表现                                      |
| ------------------- | ----------------------------------------------------- |
| `session-not-found` | 404 `dsh-session-not-found`(提示重建)                 |
| `session-conflict`  | 409,details 带已有 sessionId,响应体建议"复用已有会话" |
| `cancelled`         | 200 但 value 标记 `cancelled:true`(正常取消)          |
| `model-unavailable` | 503 `dsh-model-unavailable`                           |
| 连接失败/超时       | 503 `dsh-unavailable`(触发 3.1 监控)                  |
| 其他 `bad-request`  | 400 透传 message(不透传 details 中的内部结构)         |

---

## 5. 会话模型与数据隔离(service/session + repository/model)

### 5.1 表结构(版本化 migration `000049_dsh_sessions.go`)

```sql
CREATE TABLE dsh_sessions (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id           INTEGER NOT NULL,              -- 属主(auth_users.id)
  dsh_session_id    TEXT    NOT NULL UNIQUE,       -- DSH 侧 sessionId
  title             TEXT    NOT NULL DEFAULT '',
  workspace_dir     TEXT    NOT NULL,              -- 绝对路径, 会话 cwd
  agent_preset      TEXT    NOT NULL DEFAULT 'standard',
  status            TEXT    NOT NULL DEFAULT 'active',  -- active | archived
  last_seq          INTEGER NOT NULL DEFAULT 0,    -- 最近消费的 SSE seq(重连补拉)
  last_activity_at  DATETIME NOT NULL,
  created_at        DATETIME NOT NULL,
  updated_at        DATETIME NOT NULL
);
CREATE INDEX idx_dsh_sessions_user ON dsh_sessions(user_id, updated_at);
```

### 5.2 目录策略

- 每个会话独立 cwd:`{dsh.workspace_root}/{user_id}/{shortId}/`(shortId 为 bedrock 生成的 8 位随机串),与现有 `{workspace}/agents/agent-{id}/` 并行、互不共享;
- 建会话前由 service 创建目录;删除会话时清理该目录;
- 目录不允许来自 API 输入 —— `cwd` 不接受请求体指定,只接受"从 `dsh.workspace_root` 下按用户隔离生成"(防路径穿越/越权读文件);
- 需要指向已有项目目录的场景(v2 再议):单独接口 + 管理员权限 + 目录存在性校验。

### 5.3 会话生命周期

| 操作 | 行为                                                                                                                               |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 创建 | DB 插入(status=active)→ 目录创建 → DSH `session.create` → 回写 `dsh_session_id`;DSH 失败则回滚 DB 行并清理目录                     |
| 恢复 | 服务重启后懒恢复:首次访问时若 DSH 侧会话仍在,`session.create` 传原 `sessionId` 挂回;不在则重建(旧消息保留在 DB 之外,由 DSH 持久化) |
| 归档 | 空闲 > `dsh.session_idle_ttl`(默认 72h)置 archived;不主动删 DSH 会话(保留可恢复),仅清理本地行与目录需显式 DELETE                   |
| 删除 | 显式 `DELETE /api/v1/dsh/sessions/:id`:DSH `session.cancel` + 本地行删除 + 目录清理                                                |

### 5.4 权限与越权

- 所有 `:id` 资源操作先校验 `session.user_id == 当前用户 || 超管`,否则 403;
- RBAC 资源(seed 于 `rbac_resources.go`,随 migration 下发):

| 权限码             | 含义                    | 挂载动作                        |
| ------------------ | ----------------------- | ------------------------------- |
| `dsh_chat:view`    | 查看会话/历史/状态/模型 | GET 全部                        |
| `dsh_chat:send`    | 发消息、取消、模型切换  | POST prompt/cancel/select-model |
| `dsh_chat:approve` | 审批/提问应答           | POST respond                    |

- 审批动作写操作日志(复用 auditSvc),记录 sessionId、rpcId、outcome、操作人。

---

## 6. SSE → WebSocket 桥(service/stream + handler/ws_handler)

### 6.1 设计

- 前端连接:`GET /ws/dsh/sessions/:id/events?token=<JWT>`(完全复用现有 `/ws/ai/runs/:id/logs` 的 token 鉴权与 `WebSocketCheckOrigin` 模式);
- 后端每个活跃会话持有**一个** DSH `events.mux` 订阅(进程级去重:同一会话多个 WS 客户端共享一个 MuxClient,通过 ws.Hub 的 `BroadcastToChannel("dsh-session:<id>")` 分发);
- 帧格式:包装后的统一 WS 帧(见第 9 节契约),前端无需感知 DSH 原始协议;
- 重连:SSE 断线自动重连(指数退避 1s/2s/4s…上限 30s),恢复后以 `session.history` 按 `last_seq` 补拉缺口再续流;WS 断线由前端重连,后端缓存最近 N 条事件(环形缓冲 200 条)供补拉。

### 6.2 审批 / 提问闭环

```
DSH SSE: approval/requested {approvalId, toolName, reason}
  → stream service: 登记 pending(approvalId → rpcId, 会话上下文, 时间戳)
  → 推 WS 帧 {type:"approval/requested", ...}
  → 前端弹窗 → POST /api/v1/dsh/sessions/:id/respond {rpc_id, outcome}
  → session service: 校验 pending 存在 + 属主 → DSH /api/respond → 推 WS approval/resolved
```

- 审批模式(`dsh.approval_mode`):
  - `manual`(默认):必须人工应答;pending 超时(`dsh.pending_ttl`,默认 10 分钟)自动按拒绝处理并广播;
  - `auto`:后端自动应答 `allowed-once`,仅广播 resolved(信任 DSH 进程与 agent preset 的工具面);
- 提问(`question/requested`)同上,答案结构透传(单选/多选/自定义),不支持自动模式,必须人工。

### 6.3 心跳与保活

- DSH SSE 自带 `: connected` 注释帧保活;
- 后端 → 前端 WS 每 30s 发 `{type:"ping"}` 心跳;前端可回 `pong` 不做强制要求。

---

## 7. 配置变更(config.yaml)

```yaml
# ── DSH 交互会话 ──────────────────────────────────────────────
dsh:
  enabled: true # false = 模块整体关闭, 路由返回 503
  bin: "dsh" # 可执行文件; 或用绝对路径
  home: "./data/dsh" # DSH_HOME(会话持久化/插件/预设)
  workspace_root: "./data/dsh-workspaces" # 会话 cwd 根(按用户隔离)
  port: 17800 # 仅监听 127.0.0.1; 需与现有服务端口错开
  startup_timeout: "60s" # 就绪探活超时
  health_interval: "10s" # 运行期探活间隔
  auto_restart: true
  approval_mode: "manual" # manual | auto
  pending_ttl: "10m" # 审批/提问 pending 超时
  session_idle_ttl: "72h" # 空闲归档
  log_dir: "" # 缺省继承 build.log_dir
```

- Viper 环境变量覆盖:`BEDROCK_DSH_ENABLED`、`BEDROCK_DSH_PORT` …(沿用现有约定);
- `enabled=false` 时:不拉起进程,`/api/v1/dsh/status` 返回 `{enabled:false}`,其余路由 503。

---

## 8. 安全设计

| 威胁                  | 对策                                                                        |
| --------------------- | --------------------------------------------------------------------------- |
| 公网直连 DSH          | DSH 仅绑 127.0.0.1,仅 Bedrock 进程内访问;不提供任何端口转发配置             |
| 无认证调用 `/api/*`   | 所有 DSH 调用发生在进程内 client,唯一入口是 Bedrock 的 JWT + RBAC 路由      |
| 越权访问他人会话      | 属主校验(user_id)+ 超管豁免;目录按 `{workspace_root}/{user_id}/` 隔离       |
| 路径穿越              | cwd 不接受用户输入,由服务端按规则生成                                       |
| 危险工具执行(bash/fs) | 默认 `manual` 审批,拒绝即 `rejected`;`auto` 模式仅限受信任部署;审批全程审计 |
| 审批绕过              | respond 必须命中 pending 登记表且属主匹配;超时自动拒绝                      |
| 模型/凭证泄露         | `llm.*`、`credentials.*`、`settings.*` 域不开放为通用 API,只暴露白名单方法  |
| DSH 进程被注入        | 子进程环境仅注入 DSH 所需变量,不注入 Bedrock 密钥                           |

---

## 9. 前端对接契约(供 ultra-ui 使用,本次不实现)

### 9.1 REST 端点(响应信封沿用 `pkg.Success/Error/Paginated`)

| 方法/路径                                                         | 权限               | 请求 → 响应                                                                                                        |
| ----------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `GET /api/v1/dsh/status`                                          | `dsh_chat:view`    | → `{enabled, running, version?, degraded?, port?, restart_count?, last_restart_at?}`                               |
| `GET /api/v1/dsh/sessions?page=&page_size=`                       | `dsh_chat:view`    | → 分页列表(含 title/status/updated_at/agent_preset)                                                                |
| `POST /api/v1/dsh/sessions`                                       | `dsh_chat:view`    | `{title?, agent_preset?}` → 201 `{id, dsh_session_id, title, status, workspace_dir}`                               |
| `GET /api/v1/dsh/sessions/{id}`                                   | `dsh_chat:view`    | → 详情(含 `running` 由 WS 事件驱动)                                                                                |
| `DELETE /api/v1/dsh/sessions/{id}`                                | `dsh_chat:view`    | → 200 删除会话与目录                                                                                               |
| `GET /api/v1/dsh/sessions/{id}/history?before_seq=&max_messages=` | `dsh_chat:view`    | → `{events:[], has_more}`(事件结构与 WS 帧一致)                                                                    |
| `POST /api/v1/dsh/sessions/{id}/prompt`                           | `dsh_chat:send`    | `{content:[{type:"text",text}\|{type:"image",media_type,data,name?}], mode?:"queue"\|"steer"}` → `{accepted:true}` |
| `POST /api/v1/dsh/sessions/{id}/cancel`                           | `dsh_chat:send`    | → `{accepted:true}`                                                                                                |
| `GET /api/v1/dsh/sessions/{id}/models`                            | `dsh_chat:view`    | → 模型/供应商列表                                                                                                  |
| `POST /api/v1/dsh/sessions/{id}/select-model`                     | `dsh_chat:send`    | `{provider, model}` → 200                                                                                          |
| `POST /api/v1/dsh/sessions/{id}/respond`                          | `dsh_chat:approve` | `{rpc_id, ok:true, value:{...}}` 或 `{rpc_id, ok:false, code:"cancelled"}` → `{accepted:true}`                     |

错误码:`503 dsh-unavailable` / `404 dsh-session-not-found` / `409 dsh-session-conflict` / `403 forbidden` / `400 bad-request`。

### 9.2 WebSocket(`/ws/dsh/sessions/{id}/events?token=`)帧结构

```jsonc
// 统一帧
{ "type": "session/event",   "session_id": 1, "event": { "type": "assistant/message", "seq": 42, "data": { "message": { "role": "assistant", "content": [...] } } } }
{ "type": "approval/requested", "session_id": 1, "approval_id": "a1", "tool_name": "bash", "call_id": "c1", "reason": "..." }
{ "type": "approval/resolved",  "session_id": 1, "approval_id": "a1", "outcome": "allowed-once" }
{ "type": "question/requested", "session_id": 1, "questions": [ { "id": "q1", "question": "...", "options": [...], "multi_select": false } ] }
{ "type": "question/resolved",  "session_id": 1, "question_rpc_id": "r1", "outcome": "answered" }
{ "type": "session/queue", "session_id": 1, "items": [ { "id": "m1", "placement": "queued", "message": { "role": "user", "content": [...] } } ] }
{ "type": "session/jobs",  "session_id": 1, "jobs": [ { "id": "j1", "kind": "bash", "label": "...", "status": "running" } ] }
{ "type": "ping" }                                  // 后端心跳
{ "type": "error", "code": "dsh-unavailable", "message": "..." }
```

> 设计意图:事件帧与 DSH 原始 MuxFrame 结构对齐(字段名 snake_case 归一),前端渲染层可直接消费;`respond` 的 `rpc_id` 取 `approval/requested` / `question/requested` 帧中回显的 rpcId。

### 9.3 前端流程要点

1. 打开会话页 → 建/选会话 → `GET history` 渲染存量 → 连接 WS;
2. 发消息 `POST prompt` → 等 WS 事件流(assistant/chunk 增量、tool/* 过程、approval/question);
3. 收到 approval/question → 弹窗 → `POST respond`;
4. 断线 → 重连 WS → `GET history?before_seq=<本地最大seq>` 补拉;
5. 取消 → `POST cancel`,本地 UI 标记中断。

---

## 10. 测试与验收

| 层级     | 内容                                                                                                                    |
| -------- | ----------------------------------------------------------------------------------------------------------------------- |
| 单测     | client 信封编解码、SSE 帧解析、respond 校验(pending 命中/属主/超时)、目录隔离规则、错误映射                             |
| 集成测试 | 测试环境安装 `dsh` 二进制,`enabled=true` 起真实进程:建会话→prompt→收事件→审批→应答→取消 全链路;进程 kill 后自动重启断言 |
| 契约测试 | `api/dsh.md` 文档与 handler 一致(沿用现有 api-e2e 方式);三驱动 DB 合同测试覆盖新表                                      |
| 冒烟     | `make smoke` 扩展:DSH 模块启停、降级路径                                                                                |
| 验收     | 无 Nginx、无公网 DSH 端口;多用户会话隔离;审批/提问/打断全交互闭环;DSH 崩溃 60s 内自动恢复                               |

---

## 11. 实现里程碑(评审通过后执行)

| 里程碑 | 内容                                                     | 产出                                                               |
| ------ | -------------------------------------------------------- | ------------------------------------------------------------------ |
| M1     | 配置 + 进程托管 + 健康状态                               | `internal/dsh/service/process.go`、config 段、`/api/v1/dsh/status` |
| M2     | API 客户端 + 会话 CRUD + prompt/history/cancel/models    | `client.go`、`session.go`、`000049_dsh_sessions.go`、REST 路由     |
| M3     | SSE→WS 桥 + 审批/提问闭环 + 心跳/重连                    | `stream.go`、`ws_handler.go`、respond 路由                         |
| M4     | RBAC 资源 + 审计 + 错误码 + `api/dsh.md` 契约文档 + 测试 | 权限 seed、契约文档、单测/集成测                                   |
| M5     | 与 ultra-ui 联调、冒烟扩展、DESIGN.md 决策回填           | 联调记录、验收                                                     |

### 12. 风险与对策

| 风险                             | 对策                                                                                          |
| -------------------------------- | --------------------------------------------------------------------------------------------- |
| DSH 协议演进导致客户端失效       | 全部契约收敛在 `client.go`/`methods.go`;升级 DSH 时跑 M2 集成测试门禁                         |
| 单 DSH 进程承载多会话的资源/并发 | `session.list` 对账 + 会话上限(`dsh.max_sessions` 可配,默认 64);空闲归档回收                  |
| 同一 cwd 并发冲突                | 目录按会话唯一生成,天然无冲突;`session-conflict` 兜底复用                                     |
| DSH 进程成为单点                 | 自动重启 + `degraded` 降级 + 状态接口可观测;会话数据在 `$DSH_HOME/sessions` 持久化,恢复成本低 |
| 审批人工确认阻塞长任务           | `pending_ttl` 超时自动拒绝 + 前端醒目提示;`approval_mode:auto` 作为受信任环境选项             |
