# DSH 集成实现方案（v3：智能体整体绑定 DSH）

> 状态：实现方案（待评审）
> 前置：[dsh-integration-design.md](./dsh-integration-design.md)（设计稿 v1，仅作协议背景）；本文件取代其实现口径。
> **v3 战略变更**（相对 v2 的「并行新增交互模块」）：智能体模块**整体绑定 DSH**——
> 1. **智能体 = 会话预设**：agent 配置的「会话模式（极简/创造/PTC/标准）+ 模型选择 + 系统提示词 + 技能」全部落到 DSH 的 preset / session 语义上；`cli_key`（CLI 运行时）废弃。
> 2. **运行历史 = 会话记录**：一次 AgentRun ↔ 一个 DSH sessionId；run 详情页不再看进程输出（BuildLogViewer），而是**直接看会话输出**（UAiChat 会话视图，实时流 + 历史回放）。
> 3. 设计稿的通用交互会话能力（`/api/v1/dsh/sessions` REST + WS）**保留**，作为智能体运行的服务底座；公开 REST 与内部执行路径共用同一服务层。

---

## 1. 总览

```
┌──────────────────────────────────────────────────────────────────┐
│  Bedrock Server（单体二进制，唯一对外入口）                         │
│                                                                    │
│  internal/ai（智能体域，保留 CRUD）       internal/dsh（DSH 服务层）│
│  ┌─────────────────────┐               ┌────────────────────────┐ │
│  │ Agent 配置(模式/模型/ │──执行切换──▶│  session/preset/skills │ │
│  │ 提示词/技能/触发器)   │  v5 起       │  process/client/mux   │ │
│  │ AgentRun(run↔session)│             │  stream/approval       │ │
│  └─────────────────────┘               └───────────┬────────────┘ │
│        ▲ REST /api/v1/ai/*                          │ JSON RPC + WS│
│        │                                            │ (仅127.0.0.1)│
│        │         ┌──────────────────────────────────▼──────────┐  │
│        │         │        dsh 子进程（--profile web --no-open）  │  │
│        │         │        预设=agent 配置；会话=一次运行          │  │
│        │         └─────────────────────────────────────────────┘  │
│  ┌─────┴───────────────────────────────────────────────────────┐ │
│  │ REST /api/v1/dsh/* + WS /ws/dsh/sessions/:id/events（JWT+RBAC）│ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
        ▲
        │ 前端（bedrock web）：agent 配置页（模式/模型选择器）+ run 详情页（UAiChat 会话视图）
```

不变的安全边界：DSH 仅 `127.0.0.1`、仅进程内访问；JWT + RBAC 是唯一入口；审批/提问/打断闭环；流式输出（用户发送后 ≤1s 首帧到达）。

---

## 2. 协议核实结论（与 v2 相同，摘要）

已实测 + 源码核实的硬事实（完整证据见 v2 §2）：

1. **事件流是 WebSocket 不是 SSE**：`/api/events.mux`、`/api/events.host` 是 WebSocket upgrade 路由（HTTP GET → 426）；帧 = `{rpcId, method, payload}`；WS 单向下推，应答走 HTTP `POST /api/respond`。→ `sse.go` 作废，换 gorilla/websocket 客户端。
2. **方法名**：`agentPreset.list/select`（无 s）；`agentPresets.*` 404。
3. **MuxFrame 全集**：`session/subscribed`（基线 lastSeq）、`session/event`（SessionEvent：user/message、assistant/chunk、assistant/message、tool/call、tool/result…）、`approval/requested|resolved`、`question/requested|resolved`、`session/queue`、`session/jobs`、`session/projection`、`stream/error`。
4. **HostFrame 全集**（本版新增使用）：`host/session-status {sessionId, running}`（**运行终态信号**）、`host/agent-error {sessionId, message}`（无轮次位置的 agent 失败）、`host/session-added|removed`、workspace/archived 系列。
5. `session.create {cwd?, sessionId?, agentPreset?}` → `{sessionId, agentPreset?}`；`session.prompt {sessionId, mode:"queue"|"steer", content:[text|image], clientTimeZone?}`；`session.selectModel {sessionId, provider, model, reasoningEffort?}` → `{selected}`；`session.history {sessionId, beforeSeq?, maxMessages?}`；`session.cancel`。
6. `agentPreset.list` → `{presets:[{id, trust, isDefault, name?, description?, broken?}], authorable, hasDocument}`；实测 4 个系统预设：**standard（标准）/ code（PTC，默认）/ minimal（极简）/ cordis（创造）**；`authorable: true`。
7. **预设文件模型**：`$DSH_HOME/.agent-presets/<id>/` = `agent.cordis.yml`（顶层插件行列表）+ 可选 `preset.yml`（显示名/描述）；`PRESET_ID = /^[a-z0-9][a-z0-9-]*$/`；发现机制每次调用重读磁盘（运行中写入立即可见）；预设由 roster **每进程挂载一次**，会话按 scope 父级加入 → **内容变更必须换 id**（内容哈希）。
8. **技能文件模型**：`dsh-skill-filesystem` 配置含 `customSkillDirs: string[]`（额外技能根，`<dir>/<skill>/SKILL.md` 结构）；`providerName` 默认 `"filesystem"`；另有 `<dshHome>/skills/`、cwd `.dsh/skills/` 两个默认根。
9. `llm.models` → `{groups: [{id, name, models: [{id, name?, contextWindow?, maxTokens?, reasoning?}]}], failures}`（host 级模型目录，agent 配置页模型选择器数据源）。
10. `agentPreset.read`（特权方法，loopback 钉死）：bedrock 进程内客户端（loopback + 无 Origin）可通过信任围栏调用，取基座预设组成文本 → 作为生成 agent 专属预设的模板。
11. `GET /api/session.export?sessionId=` → 会话日志导出（run 详情「下载产物」的替代）。

---

## 3. 核心映射：智能体 = 会话预设

### 3.1 配置字段映射（ai_agents 表）

| 现有字段 | 现状语义 | 改造后 |
| --- | --- | --- |
| `cli_key` | CLI 运行时（codex/claude_code…） | **废弃**；改为 `session_preset`（DSH 预设 id：`minimal`/`code`/`standard`/`cordis`，来自 `agentPreset.list`） |
| `system_prompt` | 拼进 CLI 参数 | persona 覆盖：见 §3.2 生成规则 |
| `skill_ids` | 技能包 → CLI 参数/工作副本 | 技能目录注入：见 §3.3 |
| `repo_bindings` | 工作区 git checkout | 不变：会话 cwd = agent 工作区（复用 `SyncAgentWorkspace`） |
| `env_vars` | 子进程环境变量 | **已知局限**：DSH 会话无 env 注入点 → 写 `{agentWorkspace}/.env`（权限 600）+ persona 提示词说明读取方式（见 §3.5）；敏感值不进对话历史 |
| `output_dir` / `stream_output` / `timeout_sec` | CLI 产物/流式开关/超时 | `output_dir` 保留（agent 自行产出）；`stream_output` 废弃（永远流式）；`timeout_sec` 保留（bedrock 侧超时 → cancel） |
| **新增** | — | `session_preset`（模式）、`model_provider` + `model_id`（模型选择，来自 `llm.models` 目录，与 DSH 一致）、`approval_mode`（manual/auto，见 §4.4） |

### 3.2 专属预设生成规则（internal/dsh/service/preset.go）

```
输入：basePresetId（用户选的模式）+ agent.SystemPrompt + agent 技能清单 + agentKey
输出：预设 id = "bedrock-" + agentKey + "-" + sha256(输入)[0:8]   // 满足 PRESET_ID；内容哈希 → 变更换 id
生成流程：
  1. agentPreset.read(basePresetId) → 基座组成文本（特权方法，loopback 可调）
  2. YAML 解析（顶层插件行列表）
  3. 变换：
     a. 找 persona 行（id: persona）→ config.text = agent.SystemPrompt（空则保留基座原文；
        支持 {{cwd}}/{{model}} 占位符透传）
     b. skill-filesystem 行（id: skill-filesystem）→ 追加 config：
        providerName: "bedrock-<agentKey>"        // ★ 必须唯一，防与基座 "filesystem" 冲突
        customSkillDirs: ["<agentSkillDir>"]      // §3.3
        watch: false                              // 技能变更走「重生成预设」而非热加载，省 watcher
     c. （v2）按 agent 配置裁剪工具行（如禁用 web/subagent）
  4. 原子写 <dsh.home>/.agent-presets/<id>/agent.cordis.yml（tmp+rename，幂等；并发同 id 安全）
  5. 可选写 preset.yml（name = agent.Name，description = agent.Description）
优化：agent 无自定义提示词且无技能时，直接用基座预设 id（不生成），零额外挂载成本。
清理：agentPreset.list 对账，删除无会话引用的 bedrock-* 旧哈希预设目录。
```

### 3.3 技能注入（internal/dsh/service/skills.go）

- 技能工作副本目录：`{agentWorkspace}/.bedrock-skills/`（由 workspace 同步步骤维护，**按 agent 隔离**，不污染 `$DSH_HOME/skills/` 与 cwd `.dsh/skills/`）。
- 结构：`<skillName>/SKILL.md` + 支持文件（复用 `SkillService.ListFiles/ReadFile` 从技能工作副本复制，或直接引用技能存储根并建软链——实现时选一，默认**复制**保证会话期不可变）。
- 同步时机：`SyncAgentWorkspace` 扩展一步 `syncAgentSkills(agent)`；agent 技能变更 → 同步 + 预设哈希变化 → 新预设 id。
- 发现：preset 的 skill-filesystem 行 `customSkillDirs` 指向该目录（§3.2b）。**无需 DSH 重启**（preset 热发现）。

### 3.4 工作区与会话 cwd

- **会话 cwd = agent 工作区** `{workspace}/agents/agent-{id}/`（复用现有 `SyncAgentWorkspace`：repo checkout + 技能目录 + `.env`）。
- 同一 cwd 可挂多个会话（workspace 语义天然支持多会话）；**并发 run 复用现有串行语义**（AgentService 的 per-agent 执行队列保留，避免同工作区并发写冲突）。
- 公开 REST `POST /api/v1/dsh/sessions` **不接受 cwd 输入**（维持设计稿 §5.2 防路径穿越）；agent 执行的 cwd 由内部服务传入（`CreateAgentSession(agent, userID)`），不经过公开路由。

### 3.5 环境变量（明确局限与对策）

DSH 预设是插件行组合，**没有 per-session env 注入点**（shell-env 属 host 平面，标准预设注释明示）。对策（v1）：
1. `agent.EnvVars` 解密后写入 `{agentWorkspace}/.env`（0600，agent 工作区本就私有）；
2. persona 覆盖文本追加一句：「工作区 `.env` 中提供环境变量（KEY=VALUE 格式），需要时读取它，不要向用户复述其值」；
3. 工作区清理/删除 run 时保留（工作区是 agent 级持久目录，与现行为一致）。
风险记录：模型可能不读取 .env → 验收用例覆盖「env 依赖型 agent 任务」；v2 若 DSH 提供会话级 env 机制再迁移。

---

## 4. AgentRun ↔ DSH 会话：状态机与执行路径

### 4.1 agent_runs 表变更（migration 000049 同批）

```sql
ALTER TABLE agent_runs ADD COLUMN dsh_session_id TEXT;          -- DSH sessionId（唯一索引，可空=存量旧 run）
ALTER TABLE agent_runs ADD COLUMN dsh_session_status TEXT;      -- 镜像 DSH 侧状态（冗余，供列表查询免 RPC）
ALTER TABLE agent_runs ADD COLUMN final_output TEXT;            -- 终态时取最终 assistant 文本（OutputText 兼容替身）
-- ai_agents：
ALTER TABLE ai_agents ADD COLUMN session_preset TEXT NOT NULL DEFAULT 'standard';
ALTER TABLE ai_agents ADD COLUMN model_provider TEXT;
ALTER TABLE ai_agents ADD COLUMN model_id TEXT;
ALTER TABLE ai_agents ADD COLUMN approval_mode TEXT NOT NULL DEFAULT 'manual';
-- 存量：cli_key 保留列但停止使用（数据兼容）；旧 run 无 dsh_session_id → 详情页降级显示 output_text（§7）
```

### 4.2 执行时序（替换 CLI 子进程路径）

```
ManualRun/APIRun/DocsGenerateRun/OnBuildEvent/Cron（触发不变）
  → 1. 确保预设生成（§3.2，幂等） + SyncAgentWorkspace（含技能目录/.env）
  → 2. session.create {cwd: agentWorkspace, agentPreset: <生成的 id>}   → 记 dsh_session_id
  → 3. 若 agent.model_id 非空 → session.selectModel {provider, model}
  → 4. session.prompt {mode:"queue", content:[{type:"text", text: runPrompt}]}
         runPrompt = UserPrompt（manual/api）｜docs 快照拼装（docs_generate）｜触发上下文拼装（build_event/pipeline/cron）
  → 5. 事件驱动状态迁移（§4.3）
  → 6. 终态：写回 status/duration_ms/finished_at/error_message/final_output
```

- run 状态 `queued`：入队（沿用现有串行队列）→ `running`：prompt 返回 accepted（或首个 host/session-status running:true）。
- **不重放历史、不发全量消息**：会话由 DSH 持有，输出即会话。

### 4.3 状态机（internal/dsh/service/stream.go 提供事件，internal/ai 消费）

| AgentRun 状态 | 触发 |
| --- | --- |
| `running` | prompt accepted；或 `host/session-status running:true` |
| `success` | `host/session-status running:false`（首个）且期间无 error 帧；或终态 `assistant/message` 无后续工具调用且流空闲（兜底，见下） |
| `failed` | `stream/error` 或 `host/agent-error`；或 selectModel/prompt 被拒（model-unavailable 等）；或超时后 cancel 未收敛 |
| `interrupted` | 用户取消 → `session.cancel` accepted（与现有 CancelRun 语义对齐：interrupted=cancelled 之一，沿用现枚举） |
| `cancelled` | 触发方标记（沿用现枚举语义） |

- **终态兜底**（防止漏判）：prompt 后若 60s 无 running:false 且 `session.history` 尾页无 in-flight（无 streaming 消息）、无 pending 审批 → 判定终态；pending 审批超时（pending_ttl）自动拒绝后同样收敛。
- `duration_ms`：prompt accepted → running:false。
- `final_output`：终态后 `session.history` 尾页最后一条 assistant/message 的纯文本（供旧 API 兼容字段）。

### 4.4 审批策略（关键补漏）

| 触发类型 | 建议 approval_mode |
| --- | --- |
| manual / api（用户在场） | 继承 agent 配置（默认 manual：前端可应答） |
| cron / build_event / docs_generate / pipeline（无人值守） | **agent 级 approval_mode 默认 auto**（否则管道/定时任务会永久卡在审批） |

- agent 级 `approval_mode`（manual|auto）> 全局 `dsh.approval_mode` 兜底。
- auto 时由 stream 服务对 `approval/requested` 自动 `respond{ok:true}`（允许一次），仅广播 resolved；提问（question/requested）**不支持自动**，无人值守触发时记录并继续（v2：向触发方通知）。
- 审批全程审计（设计稿 §8）。

### 4.5 超时与取消

- `timeout_sec`：bedrock 侧计时，超时 → `session.cancel` → 状态 `interrupted`（沿用现 CancelRun 语义）。
- `POST /api/v1/ai/runs/:id/cancel` → 现 CancelRun 逻辑改为调 `session.cancel`（不再 kill 子进程）。
- DSH 崩溃/重启：run 进入 `failed`（`dsh-unavailable`），DSH 侧会话由 `$DSH_HOME/sessions` 持久化可恢复（重新绑定或手工续跑）。

### 4.6 产物与旧字段

- 详情页「下载产物」→ `GET /api/v1/dsh/sessions/{id}/export`（DSH 会话日志导出，JSONL）。
- `output_text`：新 run 不再写日志文本，改存 `final_output`（§4.3）；旧 run 保留原值。
- `log_path`：废弃（新 run 不写）。
- StreamOutput 字段：保留列、不再读取。

---

## 5. 后端模块改造清单

### 5.1 internal/dsh（服务层，新增/修正）

| 文件 | 内容 | 里程碑 |
| --- | --- | --- |
| `service/methods.go` | 修正方法名 `agentPreset.list/select`；新增 `MethodAgentPresetRead`、`MethodLLMModels` 已备；`MethodSessionExport` | M0 |
| `service/mux.go`（新，替换 sse.go） | WS 客户端：events.mux + events.host；帧解析 `{rpcId, method, payload}`；指数退避重连；subscribed 基线；HostFrame 归一 | M0 |
| `service/process.go`（新） | 进程托管（§v2 M1 不变）：`dsh --profile web --port X --no-open`、DSH_HOME、探活/重启/degraded、凭证 env 透传 | M1 |
| `service/session.go`（新） | 会话 CRUD + 懒恢复 + 归档/上限；`CreateAgentSession(agent, userID)` 内部入口（cwd=agent 工作区） | M2 |
| `service/preset.go`（新） | §3.2 生成/对账/清理 | M2 |
| `service/skills.go`（新） | §3.3 技能目录同步 | M2 |
| `service/stream.go`（新） | 单 MuxClient 去重；帧→统一帧；pending 审批/提问表 + TTL；auto 审批；ring buffer 200 | M3 |
| `handler/handler.go` + `ws_handler.go` | §6 REST + WS（复用 internal/ai 的 ws 模式） | M3 |
| RBAC seed / audit / `api/dsh.md` | `dsh_chat:view/send/approve`；respond 审计；契约文档 | M5 |

### 5.2 internal/ai（执行路径切换）

| 文件 | 改动 |
| --- | --- |
| `service/agent_service.go` | CreateAgent/UpdateAgent：cli_key 校验 → session_preset/model 校验（查 `agentPreset.list` / `llm.models` 缓存）；run 执行路径（§4.2）：替换 CLIRunner 调用为 dsh session 流程；CancelRun → session.cancel；超时；终态写回 |
| `service/workspace.go` | SyncAgentWorkspace 扩展：技能目录（§3.3）+ `.env`（§3.5）；删除 CLI args 拼接相关（appendNonStreamingOutputArgs/appendFullPermissionArgs/agentWorkspaceScopeHint 等） |
| `service/agent_env.go` | 解密逻辑复用，写盘 `.env` |
| `service/cli_lookup.go` / `cli.go` 相关 | 移除或降级为「不支持 CLI 运行时」报错（数据兼容：存量 cli_key 忽略） |
| `handler/handler.go` | CreateAgent 入参校验变更；`GET /api/v1/ai/presets`、`GET /api/v1/ai/models`（透传 dsh 目录，供配置页，权限 ai_agents:view） |
| `model/models.go` | 新字段（§4.1） |

### 5.3 前端（bedrock web）

| 页面 | 改动 |
| --- | --- |
| `views/ai/agents/pages/main.vue` | 表单：CLI 选择器 → **会话模式选择器**（`GET /api/v1/ai/presets`，显示 DSH 提供的 id/名称/描述，保持与 DSH 一致）+ **模型选择器**（`GET /api/v1/ai/models`，provider 分组）；新增 approval_mode 选择 |
| `views/ai/runs/pages/detail.vue` | **BuildLogViewer → UAiChat 会话视图**：`dsh_session_id` 存在 → 挂 `createBedrockTransport({baseURL, token, sessionId})`（ultra-ui 方案 A4），history 回放 + WS 实时流；旧 run（无 session_id）→ 保留原日志降级渲染 |
| `views/ai/runs/pages/main.vue` | 列表加「会话」列（跳详情）；状态标签沿用 |

---

## 6. API 契约修订（相对设计稿 §9）

新增端点（其余不变）：

| 方法/路径 | 权限 | 说明 |
| --- | --- | --- |
| `GET /api/v1/dsh/presets` | `dsh_chat:view` | `agentPreset.list` 透传：`{presets:[{id,name,description,trust,is_default}], authorable}`；agent 配置页模式选择器 |
| `GET /api/v1/dsh/models` | `dsh_chat:view` | `llm.models` host 目录透传：`{groups:[{id,name,models:[{id,name,context_window?,max_tokens?,reasoning?}]}], failures}`；模型选择器（**与 DSH 中提供的一致**） |
| `GET /api/v1/dsh/sessions/{id}/export` | `dsh_chat:view` | 会话日志导出（run 产物下载） |
| `GET /api/v1/ai/presets`、`GET /api/v1/ai/models` | `ai_agents:view` | 透传 dsh 目录（agent 配置页复用；dsh.enabled=false 时 503） |

WS 帧契约：沿用设计稿 §9.2 + 修订（`session/subscribed` 首帧基线、`stream/error` 透传、帧含 `rpc_id` 供应答回显）——不变。

---

## 7. 数据迁移与兼容

1. `ai_agents.cli_key`：保留列、停止使用；存量 agent 建新 run 时 `session_preset` 取默认 `standard`（或迁移脚本按 cli_key 映射：claude_code→standard、codex→standard、无→minimal）。
2. 存量 `agent_runs`（无 dsh_session_id）：列表/详情正常显示；详情页降级为旧输出渲染（output_text/log 已有数据）；不迁移会话。
3. `dsh.enabled=false`：`/api/v1/ai/*` 的 run 执行返回 503（明确错误，不再回退 CLI——**智能体只绑定 DSH**）；agents/skills/triggers CRUD 照常。
4. 旧 CLI 代码（CLIRunner、cli_lookup、CLI args 拼接）：切换后删除；`resource` 域的「AI CLI 运行时」管理 UI 标注废弃（后续版本移除）。

---

## 8. 里程碑（评审通过后执行）

| 里程碑 | 内容 | 验收 |
| --- | --- | --- |
| M0 | 协议修正：方法名 + WS 客户端（mux.go）+ 单测 | `go test ./internal/dsh/...`；对真实 dsh 连 mux 收 subscribed |
| M1 | 进程托管 + 状态接口 | `make dev` 自动拉起；status 返回版本/端口；kill 后 60s 恢复 |
| M2 | 会话域 + preset/技能生成 + presets/models 目录端点 | curl 全链路建会话；agent 专属预设可被 `agentPreset.list` 看到且创建生效 |
| M3 | 流桥 + 审批/提问闭环 + host 流 | WS 实时收到 chunk/工具/审批/提问事件；审批应答闭环 |
| M4 | **智能体执行切换**：run→session 状态机、超时/取消、approval_mode、旧 CLI 路径移除 | manual/cron/docs/pipeline 四类触发全链路；取消/超时/失败路径；`dsh.enabled=false` 503 |
| M5 | RBAC/审计/契约/迁移/测试（含存量兼容） | 权限 seed；`api/dsh.md`；api-e2e；三库合同；make smoke 扩展 |
| M6 | 前端：agent 配置表单（模式/模型）+ run 详情页会话视图（UAiChat）+ 旧 run 降级 | 浏览器端到端：配置→运行→实时流→审批→详情回放 |

依赖：M4/M6 依赖 ultra-ui `@veltra/ai` 的 session 模式（A1–A4，见 ultra-ui 方案）。

---

## 9. 测试与验收要点（新增）

- **状态机矩阵**：{manual, cron, docs, pipeline, build_event} × {正常, 拒绝审批, 提问, 超时, 用户取消, DSH 崩溃, selectModel 失败, prompt 被拒} → run 终态断言。
- **预设生成黄金文件**：4 种基座 × {有/无自定义提示词} × {有/无技能} 的 agent.cordis.yml 快照测试；providerName 唯一性断言（两个 agent 预设并存挂载不冲突——真实 DSH 集成测）。
- **技能注入**：agent 技能目录生成 SKILL.md 结构；会话内 `skill.list` 可见（集成测）。
- **env 注入**：`.env` 写入 + persona 提示（集成测断言模型可用工具读到）。
- **并发**：同一 agent 连续两次 run（串行队列）；不同 agent 并发 run。
- **验收**：run 详情页首帧 ≤1s；无人值守触发不卡审批；一个 run = 一个 sessionId（幂等断言）；DSH 崩溃 60s 恢复且 run 标记 failed。

---

## 10. 风险与对策（增量）

| 风险 | 对策 |
| --- | --- |
| 专属预设与基座 skill-filesystem providerName 冲突（挂载即崩） | §3.2b 强制唯一 providerName + M2 集成测覆盖「多 agent 预设并存」 |
| 每预设一个 skill-filesystem watcher 的成本 | `watch: false` + 技能变更走内容哈希换预设（§3.2/3.3） |
| 预设「每进程挂载一次」→ 旧挂载不随文件变更 | 内容哈希 id（§3.2）+ 对账清理；agent 配置变更后新 run 必用新预设 |
| 无人值守 run 卡在审批 | agent 级 approval_mode 默认 auto（§4.4）；pending_ttl 超时自动拒绝兜底 |
| env vars 无注入点 | `.env` + persona 提示（§3.5）；验收覆盖 |
| 并发 run 同工作区写冲突 | 沿用 per-agent 串行执行队列（§3.4） |
| DSH 升级破坏预设格式（persona 行 id 变化等） | preset.go 的 YAML 变换集中隔离；变换失败 → run 报错并提示 DSH 版本兼容；M2 集成测门禁 |
| 模型选择与 DSH 目录漂移 | 配置页/校验都实时查 `llm.models`；selectModel 失败 → run failed + 明确错误码 |
