# 智能体会话底座集成实现方案（v4：opencode 先行 + Provider 抽象）

> 状态：实现方案（待评审）
> 取代 [dsh-integration-implementation-plan.md](./dsh-integration-implementation-plan.md)（v3）作为实现口径；v3 保留为 DSH 协议实测参考与未来 DSH 适配器的实现依据。
> **v4 战略变更**（相对 v3「智能体整体绑定 DSH」）：
> 1. **opencode 先行**：DSH 尚处开发者预览（0.1.x-rc）且不稳定；opencode 1.18 契约稳定开放（OpenAPI 3.1）、版本节奏快。会话底座默认后端切换为 `opencode serve`。
> 2. **Provider 抽象**：会话底座域（`internal/harness`）按接口抽象（会话 CRUD / 流 / 审批 / 目录），`opencode` 适配器先行，`dsh` 适配器将来增量补齐——两边会话语义已核实近乎同构（§2.4），切换成本 ≈ 一个适配器 + 一个智能体编译器。
> 3. v3 的领域决策不变：智能体 = 会话预设/定义；运行历史 = 会话记录（run 详情页看会话流，不再看进程日志）；公开交互会话 REST/WS 保留为服务底座。

---

## 1. 总览

```
┌────────────────────────────────────────────────────────────────────┐
│  Bedrock Server（单体二进制，唯一对外入口）                            │
│                                                                      │
│  internal/ai（智能体域，保留 CRUD）      internal/harness（会话底座）  │
│  ┌─────────────────────┐               ┌──────────────────────────┐ │
│  │ Agent 配置(定义/模型/ │──执行切换──▶│  provider.Provider 接口    │ │
│  │ 提示词/技能/触发器)   │  v5 起       │  ├─ opencode 适配器(先行) │ │
│  │ AgentRun(run↔session)│              │  └─ dsh 适配器(后续)      │ │
│  └─────────────────────┘               │  process/stream/approval │ │
│        ▲ REST /api/v1/ai/*             └───────────┬──────────────┘ │
│        │                                            │ REST + SSE     │
│        │         ┌──────────────────────────────────▼──────────┐    │
│        │         │   opencode serve 子进程（--hostname 127.0.0.1）│    │
│        │         │   会话=一次运行；agent 定义/技能落 agent 工作区  │    │
│        │         └──────────────────────────────────────────────┘    │
│  ┌─────┴─────────────────────────────────────────────────────────┐  │
│  │ REST /api/v1/harness/* + WS /ws/harness/sessions/:id/events     │  │
│  │ （JWT + RBAC：harness_chat:view/send/approve）                   │  │
│  └────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────┘
        ▲
        │ 前端（bedrock web）：agent 配置页（定义/模型选择器）
        │                     + run 详情页（UAiChat 会话视图：回放 + 实时流）
```

不变的安全边界：opencode serve 仅 `127.0.0.1` + Basic Auth（bedrock 生成随机密码）；JWT + RBAC 是唯一入口；审批/提问/打断闭环；流式输出（用户发送后 ≤1s 首帧到达）。

---

## 2. 协议核实结论（opencode v1.18.29，2026-09-15 本机实测）

核实方式：本机 `opencode serve` 拉取 OpenAPI 3.1 spec（`GET /doc`，162 端点）逐项解析 + 官方文档 + 常驻内存实测。

### 2.1 已核实硬事实

1. **进程形态**：`opencode serve --port X --hostname 127.0.0.1`，无头 HTTP 服务；`OPENCODE_SERVER_PASSWORD` 启用 Basic Auth（用户名默认 `opencode`）。spec 未声明 securitySchemes，认证仅靠 env 开关。空载常驻 RSS ≈ 404 MB（对照：dsh web 556 MB）。
2. **会话创建**：`POST /api/session {id?, agent?, model?, location:{directory}}`——**`location.directory` 必填，单进程多工作区成立**（每 agent 工作区一个会话，无需多进程）。
3. **发消息**：`POST /api/session/{id}/prompt {prompt:{text, files?}, delivery:"queue"|"steer", resume?}`——与 DSH 的 `mode:"queue"|"steer"` 同构。
4. **会话内切换**：`POST /api/session/{id}/agent {agent}`（切智能体定义）、`POST /api/session/{id}/model {model:ModelRef}`；`ModelRef = {id, providerID, variant?}`。
5. **终态信号**：`POST /api/session/{id}/wait`（"wait for a session agent loop to become idle"）+ 消息状态事件（事件 payload 细名 M0 核实）。
6. **事件流**：SSE。`GET /api/event`（服务级总线）；**`GET /api/session/{id}/event?after=<seq>`（"replay durable events after an aggregate sequence, then continue"）**——与 DSH `session/subscribed` 基线 lastSeq 同构，回放+实时一体。
7. **审批/提问双频道**（与 DSH approval/question 同构）：
   - 拉取：`GET /api/permission/request?location=`、`GET /api/question/request?location=`；
   - 应答：`POST /api/session/{id}/permission/{requestID}/reply`、`POST /api/session/{id}/question/{requestID}/reply|reject`。
8. **历史/消息**：`GET /api/session/{id}/history`、`GET /api/session/{id}/message`、`GET /api/session/{id}/context`。
9. **取消**：`POST /api/session/{id}/interrupt`；压缩：`POST /api/session/{id}/compact`。
10. **目录**：`GET /api/provider`、`GET /api/model?location=`（按发布序排模型，智能体配置页数据源）；`GET /api/agent`（agent 目录）；`GET /api/skill?location=`（已注册技能）。
11. **健康/进程**：`GET /global/health`、`POST /global/dispose`、`POST /global/upgrade`；`GET /api/location` 当前工作目录。
12. **agent 定义文件模型**（文档核实，M2 快照测试确认）：`{workspace}/.opencode/agents/<name>.md`（项目级）或 `~/.config/opencode/agents/`（全局）；YAML frontmatter（`description` 必填、`mode`、`model: provider/model-id`、`temperature`、`permission`（per-tool ask/allow/deny + glob）、`steps` 等）+ Markdown 正文 = 系统提示词。文件即真相，无 DSH 的「每进程挂载一次」问题，**不需要内容哈希换 id**。
13. **技能文件模型**（文档核实）：`<dir>/<name>/SKILL.md`，frontmatter `name`（小写连字符，须与目录名一致）+ `description`；发现位置含 **`.agents/skills/`（项目级）**——与 bedrock 技能目录约定天然一致；另有 `.opencode/skills/`、`.claude/skills/`、`~/.config/opencode/skills/` 等。`opencode.json` 可按 glob 配 `permission.skill`（allow/deny/ask）。
14. **导出差异**：无原生 session export 端点 → bedrock 侧用 history/message 自拼 JSONL 导出（§6）。
15. **实验端点**：`/experimental/*`（workspace、worktree、control-plane 等）v1 一律不依赖。

### 2.2 待核实清单（M0/M2 集成时落实）

> **M0 已落档（2026-09-15，v1.18.29 本机实测 + 集成测试 `internal/harness/provider/oc`）**，结论如下：

- **SSE 事件 payload 细名与形状（已钉死）**：
  - 信封：per-session 流 `{"id":"evt_*","type":"<name>","durable":{"aggregateID":"ses_*","seq":N,"version":V},"data":{...}}`；服务级总线 `/api/event` 同形另加 `"location":{"directory":...}`。`after=<seq>` 引用 `durable.seq`；prompt 响应带 `admittedSeq`（排队基线）。
  - durable 类型（可回放，per-session 流全集）：`session.next.prompt.admitted` / `prompted` / `step.started|ended|failed` / `text.started|ended` / `tool.input.started|ended` / `tool.called` / `tool.success|failed` / `reasoning.started|ended` / `compaction.*` / `revert.*`。
  - **瞬态类型（仅总线、不可回放）**：`text.delta`、`reasoning.delta`、`tool.input.delta`、`permission.v2.asked|replied`、`question.v2.asked|replied|rejected`、`session.idle`、`session.status`、`session.error`。→ 流桥（M3）实时分片必须走总线；回放用 `text.ended.text` 全文兜底（`step.ended` 带 `finish`/`cost`/`tokens`）。
  - steer 语义：立即 `prompt.admitted`（占 seq），当前 step 结束后才 `prompted`。
  - **无事件时 SSE 不发响应头**（新会话先订阅后 prompt 会让 HTTP 客户端阻塞至首事件）；回放语义下先 prompt 再订阅（`after=0`）等价，客户端已按此设计并在集成测试固化。
- **permission reply 体（已钉死）**：`POST /api/session/{id}/permission/{requestID}/reply`，体 `{"reply":"once"|"always"|"reject","message?":string}`，成功 204。question：`{"answers":[[label,...],...]}`（每题一个选中 label 数组）→ 204；`reject` 无请求体。实测 once 应答后工具立即执行、durable 流出 `tool.success`。
- **agent 定义/技能发现时机（实测有异步延迟，M2 需轮询对账）**：目录（`/api/agent`、`/api/skill`、`/api/model`）按 location 惰性加载；新目录首次查询可能返回空列表（含内置 agent 全缺），秒级后填充；项目实例已加载后再写入的 `.md` 不保证即时可见（观察到 >5 分钟未拾取）。→ M2 编译 `bedrock-*.md` 后必须轮询 `GET /api/agent?location=` 直到出现（带超时兜底）。`/api/model` 对新目录冷启动同样可能短暂为空（已实测：显式指定 model 仍可正常 prompt）。
- **`.opencode/agents/`（复数）路径（已确认可用）**：单复数均可（内置技能文档表：`.opencode/agent/<name>.md` 或 `.opencode/agents/<name>.md`，全局 `~/.config/opencode/agent(s)/`）。**修正 §3.3**：项目级技能目录是 `.opencode/skills/<name>/SKILL.md`（或单数 `skill/`），**`.agents/skills/` 项目级不发现**——实测仅 HOME 级 `~/.agents/skills/`、`~/.claude/skills/` 自动加载。技能同步目标目录改为 `{agentWorkspace}/.opencode/skills/<name>/`，v4 §3.3 的 `.agents/skills/` 假设作废。
- **补充实测**（契约适配已按此实现）：
  - `POST /api/session/{id}/wait` 在 v1.18.29 恒 503（`"Session wait is not available yet"`，busy/idle 皆然）→ 终态判定改事件驱动（`step.ended` + 队列排空）；客户端 `Wait` 返回 `ErrWaitUnavailable` 供识别。
  - 目录端点 `location` 查询参数是**对象**，bracket 编码 `?location[directory]=/path`（JSON 字符串与 dot 形式均被拒）。
  - 列表响应统一 `{"location":{...},"data":[...]}` 信封；`GET /api/session/{id}/message` **最新在前**（含 user 消息，content 为分片数组）。
  - 事件流解析：SSE `data:` 行、`:` 注释心跳可忽略（与 DSH 相同）。

### 2.2.1 遗留（M2 处理）

- agent 发现延迟的兜底策略（轮询超时后是否重建项目实例/重启 serve）在 M2 编译器落地时定型。
- `permission.v2.*` 事件与 REST 拉取（`GET /api/permission/request?location[...]=`）双通道以谁为准（事件先行 + REST 对账）在 M3 流桥定型。

### 2.3 契约漂移警示

官方文档（`/session/:id/permissions/:permissionID`）与本机 v1.18.29 spec（`/api/session/{id}/permission/{requestID}/reply`）**已对不上**——opencode 也在快速演进。对策：契约快照测试（`GET /doc` spec 存黄金文件，升级必 diff）+ bedrock 侧锁定 opencode 版本，升级作为一次显式变更。

### 2.4 与 DSH 的同构映射（Provider 抽象的依据）

| 概念 | dsh（v3 实测） | opencode（本机实测） |
| --- | --- | --- |
| 建会话 | `session.create {cwd, agentPreset?}` | `POST /api/session {location:{directory}, agent?}` |
| 发消息 | `prompt {mode: queue\|steer}` | `prompt {delivery: queue\|steer}` |
| 选模型 | `selectModel {provider, model}` | `POST model {model:{providerID, id}}` |
| 选预设/定义 | `agentPreset`（YAML 插件行） | `POST agent {agent}`（markdown 定义） |
| 历史 | `session.history {beforeSeq}` | `GET history` / `GET event?after=` |
| 取消 | `session.cancel` | `POST interrupt` |
| 终态 | WS `host/session-status {running}` | `POST wait` + 消息状态事件 |
| 审批 | `approval/requested` + respond | `permission/request` + `reply` |
| 提问 | `question/requested` + respond | `question/request` + `reply\|reject` |
| 模型目录 | `llm.models` | `GET /api/provider` + `/api/model` |
| 技能 | `customSkillDirs` | `.agents/skills/` 原生发现 |
| 事件回放 | `subscribed` 基线 lastSeq | `event?after=<seq>` |
| 传输 | WS（events.mux/host） | SSE（`/api/event`、per-session `/event`） |
| 导出 | 原生 `session.export` | 无（bedrock 自拼） |

---

## 3. 核心映射：智能体 = opencode agent 定义

### 3.1 配置字段映射（ai_agents 表）

沿用 v3 §3.1 的字段决策，仅语义换绑：

| 字段 | 改造后 |
| --- | --- |
| `cli_key` | **废弃**（保留列停用）；新增 `agent_def` 语义由编译产物承载（§3.2） |
| `system_prompt` | agent 定义 Markdown 正文 |
| `skill_ids` | 技能复制进 `{workspace}/.agents/skills/`（§3.3） |
| `repo_bindings` | 不变：会话 `location.directory` = agent 工作区（复用 `SyncAgentWorkspace`） |
| `env_vars` | 不变：写 `{workspace}/.env`（0600）+ 提示词说明（v3 §3.5 原样适用） |
| `output_dir` / `timeout_sec` | 保留；`stream_output` 废弃（永远流式） |
| **新增** | `model_provider` + `model_id`（`GET /api/provider`/`/api/model` 校验）、`approval_mode`（manual/auto，§4.4） |

注：v3 的 `session_preset`（4 个 DSH 基座预设）不再需要——opencode 内置 build/plan 等 agent，无自定义提示词且无技能的智能体直接用内置 `build`，零编译成本。

### 3.2 agent 定义编译（internal/harness/provider/oc/compile.go）

```
输入：agent.SystemPrompt + agent.SkillIDs + agent.ModelProvider/ModelID + agentKey + approval_mode
输出：{agentWorkspace}/.opencode/agents/bedrock-<agentKey>.md
规则：
  1. 无自定义提示词、无技能、无模型覆写 → 不编译，会话直接用内置 build agent
  2. frontmatter：
     description: "<agent.Name>: <agent.Description 摘要>"
     model: "<provider>/<model-id>"          // 仅当配置了模型
     permission: {...}                        // 仅 approval_mode=auto 时编译放行规则（M2 实测确认
                                              //   per-tool 规则名：bash/edit-write 等；主机制仍是运行时自动应答）
  3. 正文 = agent.SystemPrompt（追加 .env 读取提示，v3 §3.5）
  4. 原子写（tmp+rename）；无挂载缓存问题 → 修改即时对后续会话生效（发现时机 M2 核实）
清理：删除工作区中无 agent 引用的 bedrock-*.md
```

对照 v3 §3.2：无内容哈希换 id、无 providerName 冲突问题、无 YAML 插件行变换——编译器显著简化。

### 3.3 技能注入

- 目标目录：`{agentWorkspace}/.agents/skills/<name>/`（opencode 原生发现，**零配置**；不再需要 v3 的 customSkillDirs 注入与 .bedrock-skills 约定）。
- 结构与校验：`<name>/SKILL.md`，frontmatter `name` 须与目录名一致（小写连字符）→ bedrock 技能名在同步时做一次规范化校验。
- 同步时机：`SyncAgentWorkspace` 扩展一步 `syncAgentSkills(agent)`（复制技能存储根，保证会话期不可变）。
- 技能权限：`approval_mode=auto` 时在 agent 定义 `permission.skill` 编译 allow（`bedrock-*` 或全量）；manual 默认 ask。

### 3.4 工作区与会话 directory

- 会话 `location.directory` = `{workspace}/agents/agent-{id}/`；并发 run 沿用 per-agent 串行队列。
- 公开 REST `POST /api/v1/harness/sessions` **不接受 directory 输入**（防路径穿越）；agent 执行走内部入口 `CreateAgentSession(agent, userID)`（v3 §3.4 边界不变）。

---

## 4. AgentRun ↔ 会话：状态机与执行路径

### 4.1 agent_runs 表变更（migration 000049 同批）

```sql
ALTER TABLE agent_runs ADD COLUMN harness_session_id TEXT;      -- 会话 id（唯一索引，可空=存量旧 run）
ALTER TABLE agent_runs ADD COLUMN harness_session_status TEXT;  -- 镜像会话侧状态（冗余，供列表查询）
ALTER TABLE agent_runs ADD COLUMN final_output TEXT;            -- 终态最终 assistant 文本（OutputText 兼容替身）
-- ai_agents：
ALTER TABLE ai_agents ADD COLUMN model_provider TEXT;
ALTER TABLE ai_agents ADD COLUMN model_id TEXT;
ALTER TABLE ai_agents ADD COLUMN approval_mode TEXT NOT NULL DEFAULT 'manual';
-- 存量：cli_key 保留列停用；旧 run 无 harness_session_id → 详情页降级显示 output_text（§7）
```

### 4.2 执行时序（替换 CLI 子进程路径）

```
ManualRun/APIRun/DocsGenerateRun/OnBuildEvent/Cron（触发不变）
  → 1. agent 定义编译（§3.2，幂等） + SyncAgentWorkspace（含 .agents/skills/ + .env）
  → 2. POST /api/session {location:{directory: agentWorkspace}, agent: "bedrock-<agentKey>"|"build",
                           model: {providerID, id}?}            → 记 harness_session_id
  → 3. POST /api/session/{id}/prompt {prompt:{text: runPrompt}, delivery:"queue"}
       runPrompt 拼装规则不变（manual/api/docs/build_event/cron）
  → 4. 事件驱动状态迁移（§4.3）；可选 POST wait 做终态对齐
  → 5. 终态：写回 status/duration_ms/finished_at/error_message/final_output
```

### 4.3 状态机（internal/harness/stream 提供统一帧，internal/ai 消费）

| AgentRun 状态 | 触发 |
| --- | --- |
| `running` | prompt accepted（prompt 响应成功 / 首个消息状态事件 running） |
| `success` | `wait` 返回 idle 且期间无错误；或终态消息事件 completed 且无后续工具调用 |
| `failed` | 消息状态 error / 服务错误事件；prompt 被拒（模型不可用等）；超时后 interrupt 未收敛 |
| `interrupted` | 用户取消 → `POST interrupt` accepted（沿用 CancelRun 语义） |
| `cancelled` | 触发方标记（沿用现枚举） |

- 终态兜底（防漏判）：prompt 后 60s 无终态 → `GET /api/session/{id}/message` 尾页核对 + `GET /api/permission/request` 无 pending；仍不收敛 → interrupt 后判 interrupted。
- `final_output`：终态后消息尾页最后一条 assistant 文本。
- opencode 崩溃/重启：run → `failed`（`harness-unavailable`）；进程托管 60s 内拉起（§5 M1）。

### 4.4 审批策略

| 触发类型 | approval_mode |
| --- | --- |
| manual / api（用户在场） | 继承 agent 配置（默认 manual：前端应答） |
| cron / build_event / docs_generate / pipeline（无人值守） | agent 级默认 **auto**，否则永久卡审批 |

- auto 实现以**运行时自动应答为主**：stream 服务对 permission 请求自动 `reply` 允许（编译层 allow 规则为优化，双保险）；question **不自动**，无人值守触发时记录并继续。
- 审批全程审计（v3 §8 不变）。

### 4.5 超时/取消/产物

- `timeout_sec`：bedrock 侧计时 → `POST interrupt` → `interrupted`；`POST /api/v1/ai/runs/:id/cancel` 同路径。
- 「下载产物」→ `GET /api/v1/harness/sessions/{id}/export`（bedrock 用 history+message 自拼 JSONL，§6）。

---

## 5. 后端模块改造清单

### 5.1 internal/harness（会话底座域，新增）

| 文件 | 内容 | 里程碑 |
| --- | --- | --- |
| `provider/provider.go` | Provider 接口：`CreateSession/Prompt/SelectModel/History/EventStream(after)/Wait/Interrupt/ReplyPermission/ReplyQuestion/ListModels/ListAgents/Export`；统一帧模型（消息分片/工具调用/审批/提问/状态） | M0 |
| `provider/oc/client.go` | REST 客户端（Basic Auth；`GET /doc` spec 快照对比工具） | M0 |
| `provider/oc/sse.go` | SSE 客户端（`/api/event` 总线 + per-session `event?after=` 回放）→ 统一帧 | M0 |
| `process.go` | 进程托管：`opencode serve --hostname 127.0.0.1 --port {cfg}`、`OPENCODE_SERVER_PASSWORD` 随机生成与持久、探活（`/global/health`）/重启/degraded | M1 |
| `service/session.go` | 会话 CRUD + 懒恢复 + 归档/上限；`CreateAgentSession(agent, userID)` 内部入口 | M2 |
| `provider/oc/compile.go` | §3.2 agent 定义编译 + 对账清理 | M2 |
| `service/skills.go` | §3.3 技能目录同步（.agents/skills/） | M2 |
| `service/stream.go` | 单事件流去重；统一帧分发；pending 审批/提问表 + TTL；auto 审批；ring buffer 200 | M3 |
| `handler/handler.go` + `ws_handler.go` | §6 REST + WS（复用 internal/ai 的 ws 模式） | M3 |
| RBAC seed / audit / `api/harness.md` | `harness_chat:view/send/approve`；应答审计；契约文档 | M5 |

`internal/dsh` 现状（client/methods/sse 雏形）：**保留不动**，作为将来 `provider/dsh` 的起点（协议依据见 v3 文档 §2）。

### 5.2 internal/ai（执行路径切换）

同 v3 §5.2，差异点：

- `agent_service.go`：执行路径换绑 harness Provider（§4.2）；CreateAgent/UpdateAgent 校验 `model_provider/model_id`（查 harness 模型目录缓存）；CancelRun → interrupt；超时/终态写回。
- `workspace.go`：`SyncAgentWorkspace` 扩展 `.agents/skills/` + `.env`；删除 CLI args 拼接（appendNonStreamingOutputArgs 等）。
- `cli_lookup.go` / `cli.go`：移除或降级为「不支持 CLI 运行时」报错（存量 cli_key 忽略）。
- `handler/handler.go`：`GET /api/v1/ai/agents-defs`、`GET /api/v1/ai/models`（透传 harness 目录，权限 ai_agents:view）。

### 5.3 前端（bedrock web）

| 页面 | 改动 |
| --- | --- |
| `views/ai/agents/pages/main.vue` | 表单：CLI 选择器 → 模型选择器（provider 分组）+ approval_mode；不再需要 DSH 预设选择器（默认 build） |
| `views/ai/runs/pages/detail.vue` | BuildLogViewer → UAiChat 会话视图：`harness_session_id` 存在 → bedrock transport（history 回放 + WS 实时流）；旧 run 保留日志降级渲染 |
| `views/ai/runs/pages/main.vue` | 列表加「会话」列（跳详情） |

---

## 6. API 契约（bedrock 对外）

| 方法/路径 | 权限 | 说明 |
| --- | --- | --- |
| `POST /api/v1/harness/sessions` | `harness_chat:send` | 建会话（不含 directory；位置由当前用户上下文/内部入口决定） |
| `GET /api/v1/harness/sessions` / `GET .../{id}` | `harness_chat:view` | 会话列表/详情 |
| `POST .../sessions/{id}/messages` | `harness_chat:send` | 发消息（透传 delivery） |
| `POST .../sessions/{id}/interrupt` | `harness_chat:send` | 打断 |
| `POST .../sessions/{id}/permissions/{reqId}` | `harness_chat:approve` | 审批应答 |
| `POST .../sessions/{id}/questions/{reqId}` | `harness_chat:approve` | 提问应答 |
| `GET .../sessions/{id}/export` | `harness_chat:view` | JSONL 导出（bedrock 拼装） |
| `GET /api/v1/harness/models` | `harness_chat:view` | 模型目录透传（配置页） |
| `GET /api/v1/harness/agents` | `harness_chat:view` | agent 目录透传（内置 + bedrock-* 编译产物） |
| `WS /ws/harness/sessions/:id/events` | `harness_chat:view` | 统一帧：回放（after 基线）+ 实时流 |
| `GET /api/v1/ai/models` | `ai_agents:view` | 同目录（agent 配置页复用） |

WS 帧契约：统一帧模型（v3 §9.2 口径）+ 回放基线；`harness.enabled=false` 时执行类端点 503。

配置：`harness.enabled`、`harness.backend: "opencode"`（预留 `"dsh"`）、`harness.bin`、`harness.port`、`harness.approval_mode` 兜底。

---

## 7. 数据迁移与兼容

1. 存量 `agent_runs`（无 harness_session_id）：正常显示，详情页降级旧输出渲染；不迁移。
2. 存量 `ai_agents.cli_key`：保留列停用；新 run 走 harness。
3. `harness.enabled=false`：run 执行返回 503（不回退 CLI——智能体只走会话底座）；agents/skills/triggers CRUD 照常。
4. 旧 CLI 代码（CLIRunner、cli_lookup、args 拼接）切换后删除；「AI CLI 运行时」管理 UI 标废弃。

---

## 8. 里程碑（评审通过后执行）

| 里程碑 | 内容 | 验收 |
| --- | --- | --- |
| M0 | oc 契约客户端（REST+SSE）+ Provider 接口 + 统一帧模型 + spec 快照测试 | `go test ./internal/harness/...`；对真实 serve 收 `/api/event`；per-session `event?after=` 回放可用；§2.2 待核实项落档 |
| M1 | 进程托管 + 状态接口 | `make dev` 自动拉起；health 探活；kill 后 60s 恢复；degraded 上报 |
| M2 | 会话域 + agent 定义编译 + 技能同步 + models/agents 目录端点 | curl 全链路建会话；编译的 bedrock-* agent 可被会话使用且技能可见 |
| M3 | 流桥 + 审批/提问闭环 | bedrock WS 实时收分片/工具/审批/提问；应答闭环；ring buffer |
| M4 | 智能体执行切换：run→session 状态机、wait/interrupt、approval_mode、旧 CLI 移除 | manual/cron/docs/pipeline 四类触发全链路；取消/超时/失败路径；enabled=false 503 |
| M5 | RBAC/审计/契约/迁移/测试 | 权限 seed；`api/harness.md`；api-e2e；三库合同；make smoke 扩展 |
| M6 | 前端：配置表单（模型/approval）+ run 详情会话视图 + 旧 run 降级 | 浏览器端到端：配置→运行→实时流→审批→回放 |

依赖：M6 依赖 ultra-ui `@veltra/ai` session 模式（A1–A4，不变）。

---

## 9. 测试与验收要点

- **状态机矩阵**：{manual, cron, docs, pipeline, build_event} × {正常, 拒绝审批, 提问, 超时, 用户取消, serve 崩溃, 模型不可用, prompt 被拒} → run 终态断言。
- **契约快照**：`GET /doc` spec 黄金文件 diff 门禁；`opencode serve` 升级必须过快照审查。
- **agent 编译黄金文件**：{有/无提示词} × {有/无技能} × {有/无模型} × {manual/auto} 的 bedrock-*.md 快照测试。
- **技能注入**：`.agents/skills/` 结构断言；会话内 `GET /api/skill` 可见（集成测）。
- **env 注入**：`.env` 写入 + 提示词提示（集成测）。
- **并发**：同一 agent 连续两次 run（串行队列）；不同 agent 并发 run（同进程多 directory 会话）。
- **验收**：run 详情首帧 ≤1s；无人值守触发不卡审批；一个 run = 一个 sessionId（幂等断言）；serve 崩溃 60s 恢复且 run 标记 failed。

---

## 10. 风险与对策

| 风险 | 对策 |
| --- | --- |
| opencode 契约漂移（文档 vs spec 已现偏差，§2.3） | spec 快照黄金文件 + 锁版本 + 升级走显式变更评审 |
| SSE 事件 payload 细名未核实（§2.2） | M0 对真实 serve 落档 + 统一帧适配器单测隔离 |
| agent 定义/技能发现时机未知（配置变更是否即时生效） | M2 集成测钉死；不即时则对账重建会话 |
| 常驻内存 404 MB | 单实例摊薄全部会话（对照 dsh 556 MB 同级）；监控 RSS 告警 |
| Basic Auth 仅 env 开关、spec 未声明 | bedrock 托管必设随机密码 + 仅 127.0.0.1 绑定；不暴露端口 |
| 无人值守 run 卡审批/提问 | approval_mode auto（运行时自动应答）；question 记录继续；pending_ttl 兜底拒绝 |
| 导出无原生端点 | bedrock 自拼 JSONL（history+message），契约文档标注非上游能力 |
| 未来 DSH 适配器语义偏差（WS/SSE、预设模型不同） | Provider 接口 + 统一帧已是唯一缝合层；dsh 侧协议资产在 v3 文档，映射表见 §2.4 |

---

## 附录 A：DSH 适配器（后续增量）

前置：DSH 脱离开发者预览、版本稳定后再评估。实现口径直接沿用 v3 文档（协议硬事实 §2、预设生成 §3.2、技能注入 §3.3、mux/host WS 客户端 §5.1），落位 `internal/harness/provider/dsh/`；现有 `internal/dsh` 代码为其雏形。与 opencode 的语义映射见本文 §2.4；差异仅传输层（WS vs SSE）、预设编译（YAML vs Markdown）与模型目录方法名。

### 观察名单：reasonix（2026-09-15 核实）

`reasonix serve`（esengine/DeepSeek-Reasonix v1.38，Go 单二进制，MIT，多模型）同为常驻多会话 HTTP+SSE 形态，托管配套完善（token/password 认证、pid/port 文件、反代支持），cache-first 的 DeepSeek 前缀缓存对 cron/无人值守 run 的成本有吸引力。**暂不入选**：serve 的 HTTP API 无对外契约——实测 `/openapi.json`、`/doc` 均回落其 Web UI SPA，官方指南只述行为不列端点；机器级接口只有 ACP v1（stdio，每会话一进程，回到 per-run 子进程模型）与 CLI JSON 子命令（one-shot）。入选条件（任一满足即评估）：公开 HTTP API 契约/OpenAPI、ACP over HTTP、或 DSH 运行时对齐落地后随 dsh 适配器一并评估（其仓库已有 DSH_EXECUTION_MIGRATION / DSH_RUNTIME_ALIGNMENT 对齐文档）。
