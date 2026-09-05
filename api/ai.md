# AI

Agents、运行记录、Skills。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。
AI CLI 运行时管理（列表/检测/安装/升级/卸载/安装源）已迁入资源管理域，见 [resource.md](resource.md)。

## Agents

工作区与制品语义：

- 每个 Agent 唯一对应持久根工作区 `{workspace}/agents/agent-{id}/`；所有 Run 直接在该根目录执行，跨 Run 复用，启动新 Run 时不清空根目录已有文件。
- 绑定仓库以 `{agentRoot}/repo-{repositoryID}-{sanitizedBranch}/` 目录存在（分支名中的 `/`、空格等不安全字符归一为 `-`）；创建/更新 Agent 后**异步**通过 `GitCloneOrPull` 初始化工作区（`workspace_status`：`pending` → `ready` / `failed`），每次 Run 执行前再增量同步；不再软链构建任务工作区。仅 `workspace_status=ready` 时可创建 Run。
- 每个 Agent 另有一个固定产出目录 `{agentRoot}/{output_dir}`（`output_dir` 默认为相对名 `output`）。CLI 注入 `BEDROCK_AGENT_WORKDIR`（根）与 `BEDROCK_AGENT_OUTPUT`（固定产出目录）。不创建 `runs/run-{id}/output` 或任何 per-run 输出子目录；后续 Run 复用同一产出目录且不清空既有内容（便于缓存与增量写入），由 Agent/CLI 自行覆盖需要更新的文件。
- Agent 可配置任意键值环境变量：AES-GCM 加密存于 `env_vars_cipher`；API 仅回显 `{key, has_value}`；Sync/Run 时解密写入 `{agentRoot}/.env`、注入 `cmd.Env`，并设置 `BEDROCK_AGENT_ENV_FILE`（工作区 `.env` 同 UID 可见）。
- AgentRun **成功**时将产出目录快照归档为 `{artifact_dir}/agent-{id}/run-{runID}.zip`，并写入 `artifact_path`（`artifact_kind=archive`）；空目录不归档；归档失败只记日志、不阻断成功态。可通过 `GET /ai/runs/:id/artifact` 下载。此能力与 CI/CD BuildRun 制品相互独立。
- 构建事件触发（`AgentTrigger.build_event` / `BuildJob.agent_ids`）与工作区绑定解耦，语义不变。
- **智能体不归属项目**：同一 Agent 可被多个项目复用（Skills 同理）。`GET /ai/runs` 可带 `project_id` 过滤（仅匹配 Run 上显式写入的值，如 `docs_generate`）。

### GET /ai/agents — 列出 Agents

权限：`ai_agents:view`
查询参数：page: integer, page_size: integer
响应 200

### POST /ai/agents — 创建 Agent

权限：`ai_agents:create`
请求：{ name, description, enabled, cli_key, system_prompt, skill_ids, repo_bindings, env_vars, output_dir, stream_output, timeout_sec }
响应 201
说明：持久化元数据与 bindings 后立即返回，`workspace_status=pending`；后台异步初始化持久根工作区 `{workspace}/agents/agent-{id}/`（技能解压到 `.agents/skills`，每个 `repo_bindings` 项 checkout 到 `repo-{repository_id}-{sanitizedBranch}/`，环境变量写入 `.env`）。成功 → `ready`，失败 → `failed` 并写入 `workspace_error`（不回滚删除 Agent）。`output_dir` 为相对产出目录名，默认 `output`。同一 Agent 内 `(repository_id, branch)` 唯一；`branch` 缺省为 `main`。保存时不校验远程分支是否存在。`env_vars` 为全量键列表：`[{key, value?}]`，带 `value` 则写入；响应不回显明文。

### GET /ai/agents/{id} — 获取 Agent

权限：`ai_agents:view`
路径参数：id*: integer
响应 200
说明：含 `workspace_status`（`pending` | `ready` | `failed`）与 `workspace_error`；`env_vars` 为 `[{key, has_value}]`，不回显明文值。

### PUT /ai/agents/{id} — 更新 Agent

权限：`ai_agents:update`
路径参数：id*: integer
请求：{ name, description, enabled, cli_key, system_prompt, skill_ids, repo_bindings, env_vars, output_dir, stream_output, timeout_sec }
响应 200
说明：更新元数据后立即返回并将 `workspace_status` 置为 `pending`，后台重新异步初始化工作区（含仓库 checkout 与 `.env`），不清空其中已有非绑定文件。`env_vars` 若提交则为全量键列表：带 `value` 则更新/新建；已有键未带 `value` 则保留旧密文；请求中消失的键删除；省略该字段则不改环境变量。

### DELETE /ai/agents/{id} — 删除 Agent

权限：`ai_agents:delete`
路径参数：id*: integer
响应 200
说明：删除 Agent、其触发器与运行记录，并清理 `{workspace}/agents/agent-{id}/` 与 `{artifact_dir}/agent-{id}/`。

### GET /ai/agents/{id}/triggers — 列出触发器

权限：`ai_agents:view`
路径参数：id*: integer
响应 200

### POST /ai/agents/{id}/triggers — 创建触发器

权限：`ai_agents:update`
路径参数：id*: integer
请求：{ type*, enabled, cron_expression, cron_timezone, build_job_id, build_event }
响应 201
说明：类型包括 manual、api、cron（IANA 时区；不重叠、不补跑错过的任务）、build_event。

### PUT /ai/agents/{id}/triggers/{tid} — 更新触发器

权限：`ai_agents:update`
路径参数：id*: integer, tid*: integer
请求：{ type*, enabled, cron_expression, cron_timezone, build_job_id, build_event }
响应 200

### DELETE /ai/agents/{id}/triggers/{tid} — 删除触发器

权限：`ai_agents:update`
路径参数：id*: integer, tid*: integer
响应 200

### POST /ai/agents/{id}/runs — 手动触发 Agent 运行

权限：`ai_agents:execute`
路径参数：id*: integer
请求：{ user_prompt? }（可空；触发时附加的用户提示词，与智能体 `system_prompt` 一并传给 CLI）
响应 202
错误：400（智能体未启用或 `workspace_status` 非 `ready`，如「智能体工作区未初始化完成」）
说明：直接在 Agent 持久根工作区执行；环境提供 `BEDROCK_AGENT_WORKDIR` 与 `BEDROCK_AGENT_OUTPUT`（固定产出目录）。不创建 Run 专属工作区目录；成功后对固定产出目录做快照归档（见制品端点）。

### POST /ai/agents/{id}/api-runs — API 触发 Agent 运行（需 PAT scope）

权限：`ai_agents:execute`
路径参数：id*: integer
请求：{ user_prompt? }（可空；与手动触发相同）
响应 202
错误：401 / 403 / 400（工作区未就绪）
说明：JWT with `ai_agents:execute` or PAT with scope `agents:run`。请求体可省略。

### GET /ai/runs — 列出 Agent 运行记录

权限：`ai_runs:view`
查询参数：page: integer, page_size: integer, agent_id: integer, status: string, project_id: integer
响应 200

### GET /ai/runs/{id} — 获取 Agent 运行记录

权限：`ai_runs:view`
路径参数：id*: integer
响应 200
说明：返回状态、日志/文本输出、`work_dir`；成功且已归档时含 `artifact_path` / `artifact_kind`。

### GET /ai/runs/{id}/artifact — 下载 Agent 运行制品

权限：`ai_runs:view`
路径参数：id*: integer
响应 200：文件附件（`Content-Disposition: attachment`）
错误：404（无制品或文件缺失）
说明：仅当 Run 成功且产出目录非空并已快照归档时可用；文件名为 `run-{id}.zip`。

### POST /ai/runs/{id}/cancel — 取消 Agent 运行

权限：`ai_agents:execute`
路径参数：id*: integer
响应 200：Cancelled

## Skills

Skills 为跨项目复用的能力包，由 Agent 引用，**不**归属产品项目（无 `project_id`）。

### GET /skills — 列出 Skills

权限：`ai_skills:view`
查询参数：page: integer, page_size: integer
响应 200
说明：公开 Skill 需 view 权限可见；私有仅创建者可见。

### POST /skills — 创建 Skill

权限：`ai_skills:create`
请求：multipart: { name, description, visibility, file* }
响应 201
错误：422
说明：需要 SKILL.md；防 Zip Slip / zip bomb；默认上限 50MB（经 StorageService）。

### GET /skills/{id} — 获取 Skill

权限：`ai_skills:view`
路径参数：id*: integer
响应 200

### PUT /skills/{id} — 覆盖更新 Skill

权限：`ai_skills:update`
路径参数：id*: integer
请求：multipart: { name, description, visibility, file* }
响应 200：Updated

### DELETE /skills/{id} — 删除 Skill

权限：`ai_skills:delete`
路径参数：id*: integer
响应 200

### GET /skills/{id}/package — 下载 Skill 包

权限：`ai_skills:download`
路径参数：id*: integer
响应 200：data = binary
错误：401 / 403
说明：JWT 需 `ai_skills:download`，或 PAT scope `skills:read`。

### GET /skills/{id}/files — 技能文件树

权限：`ai_skills:view`
路径参数：id*: integer
响应 200：`SkillFileNode[]`
说明：返回工作副本目录树（目录优先、名称排序）。内置与上传技能均可读。首次访问时若工作副本缺失，会从 ZIP 解压到 `{storage.root}/skills/{id}/`。

### GET /skills/{id}/files/content — 读取技能文件

权限：`ai_skills:view`
路径参数：id*: integer
查询参数：path*: string（相对技能根，禁止 `..`）
响应 200：`SkillFileContent`
错误：400 / 403 / 404
说明：文本文件返回 `content`；含空字节或非 UTF-8 时 `binary=true` 且 `content` 为空。单文件上限 2MB。

### PUT /skills/{id}/files/content — 写入技能文件

权限：`ai_skills:update`
路径参数：id*: integer
请求：`{ path*, content }`
响应 200：`SkillFileContent`
错误：403 / 404 / 422
说明：仅 `source=uploaded` 且创建者/超管可写。直接覆盖磁盘工作副本，并重打包 ZIP 更新 `package_digest`。内置技能返回 403。

### POST /skills/{id}/files — 新建文件或目录

权限：`ai_skills:update`
路径参数：id*: integer
请求：`{ path*, kind*: 'file' | 'dir', content? }`
响应 201：`SkillFileNode`
错误：403 / 409
说明：同写入权限；`kind=file` 时可带初始 `content`。

### DELETE /skills/{id}/files — 删除文件或目录

权限：`ai_skills:update`
路径参数：id*: integer
查询参数：path*: string
响应 200：`{ deleted: true }`
错误：403 / 404
说明：不可删除根目录或 `SKILL.md`（含包含它的目录）。

### POST /skills/{id}/files/rename — 重命名/移动

权限：`ai_skills:update`
路径参数：id*: integer
请求：`{ from_path*, to_path* }`
响应 200：`SkillFileNode`
错误：403 / 404 / 409
说明：不可将 `SKILL.md` 改名为其他名称；目标路径已存在返回 409。

## Providers

服务商与模型管理。服务商 API Key 经 AES-GCM 安全加密落库，查询接口与页面不回传明文，仅回显 `has_api_key`；模型关联指定服务商。

### GET /ai/providers — 列出服务商

权限：`ai_providers:view`
查询参数：page: integer, page_size: integer
响应 200
说明：不回显明文 API Key，仅回显 `has_api_key`。

### POST /ai/providers — 创建服务商

权限：`ai_providers:create`
请求：{ name*, api_url*, api_key?, enabled?, notes? }
响应 201
错误：400
说明：`api_key` 使用 AES-GCM 安全加密存储；响应仅回显 `has_api_key`。

### GET /ai/providers/{id} — 获取服务商

权限：`ai_providers:view`
路径参数：id*: integer
响应 200
错误：404
说明：不回显明文 API Key，仅回显 `has_api_key`。

### PUT /ai/providers/{id} — 更新服务商

权限：`ai_providers:update`
路径参数：id*: integer
请求：{ name?, api_url?, api_key?, enabled?, notes? }
响应 200
错误：400 / 404
说明：`api_key` 留空表示保留既有密钥密文，传入非空新密钥时重新加密存储。

### DELETE /ai/providers/{id} — 删除服务商

权限：`ai_providers:delete`
路径参数：id*: integer
响应 200：`{ deleted: true }`
错误：404
说明：级联删除该服务商下所有模型。

### GET /ai/providers/{id}/models — 列出服务商模型

权限：`ai_providers:view`
路径参数：id*: integer
查询参数：page: integer, page_size: integer
响应 200
说明：按 `sort_order` 升序、`id` 升序排列。

### POST /ai/providers/{id}/models — 创建模型

权限：`ai_providers:create`
路径参数：id*: integer
请求：{ name*, model_id*, enabled?, sort_order?, reasoning_efforts?, default_params?, notes? }
响应 201
错误：400 / 404
说明：校验关联服务商存在性、`model_id` 重复性，以及 `default_params` 为合法 JSON 对象格式。

### GET /ai/providers/{id}/models/{mid} — 获取模型详情

权限：`ai_providers:view`
路径参数：id*: integer, mid*: integer
响应 200
错误：404

### PUT /ai/providers/{id}/models/{mid} — 更新模型

权限：`ai_providers:update`
路径参数：id*: integer, mid*: integer
请求：{ name?, model_id?, enabled?, sort_order?, reasoning_efforts?, default_params?, notes? }
响应 200
错误：400 / 404
说明：校验 `model_id` 重复性与 `default_params` 为合法 JSON 对象格式。

### DELETE /ai/providers/{id}/models/{mid} — 删除模型

权限：`ai_providers:delete`
路径参数：id*: integer, mid*: integer
响应 200：`{ deleted: true }`
错误：404

## Chat & Sessions

用户隔离的会话与历史消息持久化管理，以及 OpenAI 兼容流式对话代理端点。所有接口要求有效登录认证，全员可用（无需管理员权限），严格按当前登录用户过滤数据，禁止跨用户访问。

### GET /ai/chat/sessions — 列出会话列表

权限：已登录用户
查询参数：page?: integer, page_size?: integer
响应 200：`ChatSession[]`（分页包）
说明：按 `updated_at` 倒序排列，仅返回当前登录用户的会话。

### POST /ai/chat/sessions — 创建会话

权限：已登录用户
请求：`{ title*, model_id? }`
响应 201：`ChatSession`
错误：400
说明：创建属于当前登录用户的新会话。

### PUT /ai/chat/sessions/{id} — 更新会话

权限：已登录用户
路径参数：id*: integer
请求：`{ title?, model_id? }`
响应 200：`ChatSession`
错误：400 / 404
说明：仅允许会话创建者修改会话标题或绑定的当前模型；其他用户访问返回 404。

### DELETE /ai/chat/sessions/{id} — 删除会话

权限：已登录用户
路径参数：id*: integer
响应 200：`{ deleted: true }`
错误：404
说明：仅允许会话创建者删除；级联删除该会话下的所有历史消息。其他用户访问返回 404。

### GET /ai/chat/sessions/{id}/messages — 获取会话历史消息

权限：已登录用户
路径参数：id*: integer
响应 200：`ChatMessage[]`
错误：404
说明：仅允许会话创建者获取；按消息创建时间升序排列。其他用户访问返回 404。

### POST /ai/chat/sessions/{id}/messages — 新增会话消息

权限：已登录用户
路径参数：id*: integer
请求：`{ role*, content*, reasoning_content? }`
响应 201：`ChatMessage`
错误：400 / 404
说明：仅允许会话创建者添加；更新会话的 `updated_at`。

### GET /ai/chat/models — 获取可用对话模型列表

权限：已登录用户
响应 200：`AiModel[]`
说明：返回所有已启用的服务商下处于启用状态的模型列表，按排序权重升序排列，供前端对话界面直接选用。

### POST /ai/chat/completions — 流式对话代理

权限：已登录用户
完整路径：`POST /api/v1/ai/chat/completions`
请求：`ChatCompletionRequest`
响应 200：Server-Sent Events (`text/event-stream`)
错误：400 / 404 / 502
说明：OpenAI 兼容端点。根据请求的 `model` 匹配已启用的服务商与模型配置，解密服务商 API Key 注入 HTTP Authorization 请求头，透传 `reasoning_effort` 与默认模型参数，向上游 OpenAI 兼容端点发起流式请求并实时以 SSE 格式转发回前端。若请求携带 `session_id`，且该会话属于当前用户，服务端将在对话完成时持久化问答消息。

## 对象形状

### ChatSession

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  | 会话 ID |
| `user_id` | `integer` |  | 所属用户 ID |
| `title` | `string` |  | 会话标题 |
| `model_id` | `string` |  | 关联模型标识 |
| `created_at` | `string` |  | 创建时间 |
| `updated_at` | `string` |  | 更新时间 |

### ChatSessionInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` | 是 | 会话标题 |
| `model_id` | `string` |  | 关联模型标识 |

### ChatMessage

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  | 消息 ID |
| `session_id` | `integer` |  | 所属会话 ID |
| `user_id` | `integer` |  | 所属用户 ID |
| `role` | `string` |  | 角色（`user` \| `assistant` \| `system` 等） |
| `content` | `string` |  | 文本正文 |
| `reasoning_content` | `string` |  | 思考/推理内容（可空） |
| `created_at` | `string` |  | 创建时间 |
| `updated_at` | `string` |  | 更新时间 |

### ChatMessageInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `role` | `string` | 是 | 角色（`user` \| `assistant`） |
| `content` | `string` | 是 | 消息正文 |
| `reasoning_content` | `string` |  | 思考/推理内容 |

### ChatCompletionRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | `string` | 是 | 模型标识 |
| `messages` | `ChatCompletionMessage[]` | 是 | 消息上下文列表 |
| `stream` | `boolean` |  | 是否流式返回（代理强制或推荐 true） |
| `reasoning_effort` | `string` |  | 推理等级（`low` \| `medium` \| `high` 等） |
| `session_id` | `integer` |  | 可选关联的持久化会话 ID |

### ChatCompletionMessage

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `role` | `string` | 是 | 角色（`user` \| `assistant` \| `system` \| `tool`） |
| `content` | `string \| object[]` |  | 消息正文（支持纯文本或结构化多模态/内容块） |
| `name` | `string` |  | 可选发送者名称 |
| `tool_call_id` | `string` |  | tool 角色消息关联的函数调用 ID |
| `tool_calls` | `object[]` |  | assistant 角色返回的工具调用列表 |
| `reasoning_content` | `string` |  | 思考/推理内容（可空） |

### SkillPackage

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `integer` |  |
| `name` | `string` |  |
| `description` | `string` |  |
| `visibility` | `'public' \| 'private'` |  |
| `source` | `'uploaded' \| 'builtin'` | 上传可编辑；内置只读 |
| `editable` | `boolean` | 当前调用方是否可改文件（API 计算字段） |
| `package_digest` | `string` |  |
| `size_bytes` | `integer` |  |
| `created_by` | `integer` |  |
| `created_at` | `string` |  |
| `updated_at` | `string` |  |

### SkillFileNode

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `name` | `string` |  |
| `path` | `string` | 相对技能根的 POSIX 路径 |
| `kind` | `'file' \| 'dir'` |  |
| `size` | `integer` | 文件大小（字节）；目录可省略 |
| `children` | `SkillFileNode[]` | 仅目录 |

### SkillFileContent

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `path` | `string` |  |
| `content` | `string` | 文本内容；二进制时为空 |
| `size` | `integer` |  |
| `binary` | `boolean` |  |
| `editable` | `boolean` |  |

### AgentTriggerInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | `'manual' \| 'api' \| 'cron' \| 'build_event'` | 是 |  |
| `enabled` | `boolean` |  |  |
| `cron_expression` | `string` |  |  |
| `cron_timezone` | `string` |  | IANA timezone |
| `build_job_id` | `integer` |  |  |
| `build_event` | `'artifact_ready' \| 'distribution_finished'` |  |  |

### AiAgent

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `cli_key` | `string` |  |  |
| `system_prompt` | `string` |  |  |
| `skill_ids` | `integer[]` |  |  |
| `repo_bindings` | `{ repository_id: integer, branch: string }[]` |  |  |
| `env_vars` | `{ key: string, has_value: boolean }[]` |  | 仅投影键与是否有值；永不回显明文 |
| `output_dir` | `string` |  |  |
| `stream_output` | `boolean` |  |  |
| `timeout_sec` | `integer` |  |  |
| `workspace_status` | `'pending' \| 'ready' \| 'failed'` |  | 异步工作区初始化状态；存量默认 `ready` |
| `workspace_error` | `string` |  | `failed` 时的失败原因；成功时为空 |
| `created_by` | `integer` |  |  |
| `created_at` | `string` |  |  |
| `updated_at` | `string` |  |  |

### AiAgentInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `cli_key` | `string` |  |  |
| `system_prompt` | `string` |  |  |
| `skill_ids` | `integer[]` |  | 解压到工作区 `.agents/skills/{name}/`（按 Skill 名称；ZIP 内含 SKILL.md 的包装目录与 `__MACOSX` 会剥离） |
| `repo_bindings` | `{ repository_id: integer, branch: string }[]` |  | 在 `{agentRoot}/repo-{repository_id}-{sanitizedBranch}/` checkout 指定分支；同 Agent 内 `(repository_id, branch)` 唯一；`branch` 默认 `main` |
| `env_vars` | `{ key: string, value?: string }[]` |  | 全量键列表；带 `value` 则设置/更新；已有键未带 `value` 则保留；请求中消失的键删除；key 非空且不得含 `=` / 换行 |
| `output_dir` | `string` |  | 相对产出目录名；默认 `output`；路径为 `{agentRoot}/{output_dir}`，跨 Run 固定复用 |
| `stream_output` | `boolean` |  | 启用后使用 CLI 默认可读流式输出；关闭时部分 CLI 仅输出最终摘要（如 Reasonix `-p`），默认 `false` |
| `timeout_sec` | `integer` |  |  |

### AgentRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `agent_id` | `integer` |  |  |
| `trigger_type` | `string` |  |  |
| `status` | `string` |  |  |
| `work_dir` | `string` |  | Agent 持久根工作区；同一 Agent 的 Run 复用相同路径 |
| `artifact_path` | `string` |  | 成功快照归档绝对路径；空目录或未归档时为空 |
| `artifact_kind` | `string` |  | 归档时为 `archive` |
| `build_run_id` | `integer` |  |  |
| `project_id` | `integer` |  | 可空；仅显式传入时写入（如 `docs_generate`） |
| `doc_node_id` | `integer` |  |  |
| `user_prompt` | `string` |  | 触发时附加的用户提示词；可空 |
| `error_message` | `string` |  |  |
| `output_text` | `string` |  |  |
| `duration_ms` | `integer` |  | 运行耗时（毫秒）；未结束或未开始时为 `0` |
| `started_at` | `string` |  | 开始时间；未开始时为空 |
| `finished_at` | `string` |  | 结束时间；未结束时为空 |
| `created_at` | `string` |  |  |

### AiProvider

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  | 服务商名称 |
| `api_url` | `string` |  | OpenAI 兼容 API 地址 |
| `has_api_key` | `boolean` |  | 是否已配置 API Key（不回显明文） |
| `enabled` | `boolean` |  | 启用状态 |
| `notes` | `string` |  | 备注说明 |
| `created_by` | `integer` |  | 创建者用户 ID |
| `created_at` | `string` |  | 创建时间 |
| `updated_at` | `string` |  | 更新时间 |

### AiProviderInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 | 服务商名称 |
| `api_url` | `string` | 是 | OpenAI 兼容 API 地址 |
| `api_key` | `string` |  | API Key；新建时提供则加密，更新时留空保留旧密文 |
| `enabled` | `boolean` |  | 是否启用，默认 true |
| `notes` | `string` |  | 备注说明 |

### AiModel

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `provider_id` | `integer` |  | 关联服务商 ID |
| `name` | `string` |  | 显示名称 |
| `model_id` | `string` |  | 模型标识（如 gpt-4o） |
| `enabled` | `boolean` |  | 启用状态 |
| `sort_order` | `integer` |  | 排序权重（升序） |
| `reasoning_efforts` | `{ value: string, label: string }[]` |  | 推理等级档位选项列表 |
| `default_params` | `object` |  | 默认模型参数配置（如温度等 JSON 对象） |
| `notes` | `string` |  | 备注说明 |
| `created_at` | `string` |  | 创建时间 |
| `updated_at` | `string` |  | 更新时间 |

### AiModelInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 | 显示名称 |
| `model_id` | `string` | 是 | 模型标识 |
| `enabled` | `boolean` |  | 是否启用，默认 true |
| `sort_order` | `integer` |  | 排序权重，默认 0 |
| `reasoning_efforts` | `{ value: string, label: string }[]` |  | 推理等级档位选项列表 |
| `default_params` | `object` |  | 默认参数配置（必须为合法 JSON 对象） |
| `notes` | `string` |  | 备注说明 |

CLI 相关对象形状（CliDetectResult、CliCheckUpdateResult、CliExecuteResult、CliInstallSourceInput、CliRuntimeDefinition）见 [resource.md](resource.md)。
