# CI/CD

构建任务、构建运行、构建流水线、流水线运行、Webhook。

代码仓库 / 服务器 / 凭证见 [resource.md](resource.md)。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [docs/DESIGN.md](../docs/DESIGN.md)。

## 构建任务

### GET /build-jobs — 列出构建任务

权限：`cicd_build_jobs:view`
查询参数：page: integer, page_size: integer, repository_id: integer, keyword: string
响应 200：data = BuildJobPage

### POST /build-jobs — 创建构建任务

权限：`cicd_build_jobs:create`
请求：{ repository_id*, name*, description, enabled, branch, shallow_clone, build_script_type, build_script, post_build_script, work_dir, artifact_paths, output_dir, cache_paths, env_var_names, env_vars, trigger_manual, trigger_webhook, trigger_cron, webhook_secret, webhook_type, webhook_ref_path, webhook_commit_path, webhook_message_path, cron_expression, cron_timezone, max_artifacts, artifact_format, agent_trigger_event, agent_ids, deploy_targets }
响应 201：data = BuildJob
说明：`artifact_paths` 为相对仓库根的制品路径列表（文件或目录，最多约 10 条；须相对、禁止 `..`/绝对路径）。写入优先 `artifact_paths`；若为空且提供 `output_dir` 则视为单元素列表。响应含 `artifact_paths`，并回显 `output_dir` 为第一项（兼容）。`env_var_names` 为宿主机环境变量名称列表（运行时 `LookupEnv`）；`env_vars` 为加密 Key-Value 全量键列表 `[{key, value?}]`（带 value 写入；响应仅回显 `[{key, has_value}]`）。运行时合并顺序：进程环境 → 名称列表注入 → Key-Value 覆盖同名键。`post_build_script` 在主构建脚本成功后、缓存保存/归档前执行（同 shell/cwd/env）；失败则 run=`failed`。响应只读字段 `workspace_path` 为任务 checkout 绝对路径：`{build.workspace_dir}/jobs/job-{id}/`（与 Agent 的 `agents/agent-{id}/` 对齐；旧路径 `repo-{repository_id}/job-{id}/` 不再使用，不自动搬迁）。`build_script` / `post_build_script` 执行前做 `${{...}}` 文本替换（见下文「脚本模板」）；未知变量则构建失败。

### GET /build-jobs/{id} — 获取构建任务（含部署目标）

权限：`cicd_build_jobs:view`
路径参数：id*: integer
响应 200：data = BuildJob

### PUT /build-jobs/{id} — 更新构建任务 / 替换部署目标

权限：`cicd_build_jobs:update`
路径参数：id*: integer
请求：{ name, description, enabled, branch, shallow_clone, build_script_type, build_script, post_build_script, work_dir, artifact_paths, output_dir, cache_paths, env_var_names, env_vars, trigger_manual, trigger_webhook, trigger_cron, webhook_secret, webhook_type, webhook_ref_path, webhook_commit_path, webhook_message_path, cron_expression, cron_timezone, max_artifacts, artifact_format, agent_trigger_event, agent_ids, deploy_targets }
响应 200：data = BuildJob
说明：`artifact_paths` 写入优先于 `output_dir`（见创建说明）。`env_vars` 若提交则为全量键列表（带 value 更新/新建；已有键未带 value 保留；请求中消失的键删除；省略字段则不改）；永不回显明文。

### DELETE /build-jobs/{id} — 删除构建任务

权限：`cicd_build_jobs:delete`
路径参数：id*: integer
响应 200

### GET /build-jobs/{id}/webhook-secret — 查看 Webhook 密钥与 URL

权限：`cicd_build_jobs:view`
路径参数：id*: integer
响应 200

### POST /build-jobs/{id}/webhook-secret/rotate — 轮换 Webhook 密钥

权限：`cicd_build_jobs:update`
路径参数：id*: integer
响应 200

### POST /build-jobs/{id}/runs — 入队构建运行

权限：`cicd_build_jobs:execute`
路径参数：id*: integer
请求：{ branch, trigger_type }
响应 202：data = BuildRun
说明：触发时只需 `cicd_build_jobs:execute`；不要求凭证 `:use`（执行时使用已绑定凭证快照）。

## 构建运行

### GET /build-runs — 列出构建运行

权限：`cicd_build_runs:view`
查询参数：page: integer, page_size: integer, build_job_id: integer, status: string, sort: string
响应 200：data = BuildRunPage

### GET /build-runs/{id} — 获取构建运行详情（含部署尝试）

权限：`cicd_build_runs:view`
路径参数：id*: integer
响应 200：data = BuildRun

### POST /build-runs/{id}/cancel — 取消构建运行

权限：`cicd_build_jobs:execute`
路径参数：id*: integer
响应 200：data = BuildRun

### POST /build-runs/{id}/retry — 重试（新建一次构建运行）

权限：`cicd_build_jobs:execute`
路径参数：id*: integer
响应 202：data = BuildRun

### POST /build-runs/{id}/redeploy — 在同一构建运行上重新部署

权限：`cicd_build_jobs:execute`
路径参数：id*: integer
请求：{ target_ids }
响应 202：data = BuildRun

### GET /build-runs/{id}/artifact — 下载构建制品

权限：`cicd_build_runs:view`
路径参数：id*: integer
响应 200：data = binary

### GET /build-runs/{id}/log — 获取构建日志文本

权限：`cicd_build_runs:view`
路径参数：id*: integer
响应 200：data = text/plain

### GET /ws/build-runs/{id}/logs — 构建日志 WebSocket（实时）

路径前缀为 `/ws`（非 `/api/v1`）。查询参数 `token` 携带 JWT（与其它 WebSocket 一致）。

权限：`cicd_build_runs:view`
路径参数：id*: integer
查询参数：token*: string

连接成功后：

1. 服务端先按行回放已有日志文件（若有）。
2. 后续推送两类文本帧：
   - 日志行：追加到终端输出。
   - 控制帧 `__REFRESH__`：元数据（status / stage / distribution_summary / deploy_attempts 等）已变更；客户端应重新请求 `GET /build-runs/{id}`，勿写入日志视图。

`__REFRESH__` 仅经 WebSocket 广播，不写入日志文件。

## 构建流水线

`graph_json`（v2）为 VueFlow `{nodes,edges}` DAG：

- 节点类型 `type`：`start`（恰 1 个，无入边）、`end`（≥1 个，无出边）、`buildJob`（`data.build_job_id`）、`scriptJob`（`data.script_job_id`）、`agent`（`data.agent_id`）；均引用可复用任务/智能体，保存时校验存在性、DAG 无环、非 start 节点须有入边。
- 边条件 `data.condition`：`on_success`（默认，可省略）/ `on_failure`（匹配 failed/cancelled/interrupted）/ `always`。
- 节点变量覆盖 `data.env_vars`（buildJob/scriptJob）：写 `{key,value}` 设置；只写 `{key}` 保留已存值；读 API 仅回 `{key,has_value}`（值 AES-GCM 加密存储于 graph_json，永不回显明文）。覆盖在运行级生效（run > job env_vars），构建/脚本运行落 `env_overrides_cipher` 快照。

执行语义：start 节点即入口；任务/智能体节点为 **AND-join**——全部前驱终态且每个前驱至少一条入边条件匹配其结果时触发，触发后同步等待对应 Run（`trigger_type=pipeline`）；前驱终态后无边可匹配则该节点标 `skipped` 并向下游传播。end 节点为 **OR-join**——任一入边路径到达即流水线 `success`（无论此前失败过多少节点），仍在运行的其它分支被取消；所有分支走到尽头仍未到达 end → `failed`。流水线内嵌的 agent 节点为**同步**阶段（等待 AgentRun 完成并按结果走分支）；构建事件异步 AgentRun 能力（`agent_ids`/`agent_trigger_event`）保持不变。不做跨任务制品传递。

### GET /build-pipelines — 列出构建流水线

权限：`cicd_pipelines:view`
查询参数：page: integer, page_size: integer, keyword: string
响应 200：data = BuildPipelinePage

### POST /build-pipelines — 创建构建流水线

权限：`cicd_pipelines:create`
请求：{ name*, description, enabled, graph_json, trigger_manual, trigger_webhook, trigger_cron, cron_expression, cron_timezone, webhook_type, webhook_ref_path, webhook_commit_path, webhook_message_path, is_public }
响应 201：data = BuildPipeline
说明：`graph_json` 为 VueFlow `{nodes,edges}`；空图允许保存（编辑器草稿）；非空须为合法 DAG。

### GET /build-pipelines/{id} — 获取构建流水线

权限：`cicd_pipelines:view`
路径参数：id*: integer
响应 200：data = BuildPipeline

### PUT /build-pipelines/{id} — 更新构建流水线

权限：`cicd_pipelines:update`
路径参数：id*: integer
请求：同创建（字段可选）
响应 200：data = BuildPipeline

### DELETE /build-pipelines/{id} — 删除构建流水线

权限：`cicd_pipelines:delete`
路径参数：id*: integer
响应 200

### GET /build-pipelines/{id}/webhook-secret — 查看 Webhook 密钥与 URL

权限：`cicd_pipelines:view`
路径参数：id*: integer
响应 200：{ webhook_secret, webhook_url }

### POST /build-pipelines/{id}/webhook-secret/rotate — 轮换 Webhook 密钥

权限：`cicd_pipelines:update`
路径参数：id*: integer
响应 200：{ webhook_secret, webhook_url }

### POST /build-pipelines/{id}/runs — 入队流水线运行

权限：`cicd_pipelines:execute`
路径参数：id*: integer
请求：{ trigger_type }
响应 202：data = PipelineRun

## 流水线运行

### GET /pipeline-runs — 列出流水线运行

权限：`cicd_pipeline_runs:view`
查询参数：page: integer, page_size: integer, build_pipeline_id: integer, status: string, sort: string
响应 200：data = PipelineRunPage

### GET /pipeline-runs/{id} — 获取流水线运行详情（含 stages）

权限：`cicd_pipeline_runs:view`
路径参数：id*: integer
响应 200：data = PipelineRun

### POST /pipeline-runs/{id}/cancel — 取消流水线运行

权限：`cicd_pipelines:execute`
路径参数：id*: integer
响应 200：data = PipelineRun
说明：仅 `queued` / `running` 可取消。会取消非终态 sibling BuildRun/ScriptRun/AgentRun，进行中的 stage 标为 `cancelled`，未启动 stage 标为 `skipped`。

## Webhook

### POST /webhook/jobs/{build_job_id}/{secret} — 接收构建任务 Webhook

认证：不需要
路径参数：build_job_id*: integer, secret*: string
响应 202（可能为重复投递，`triggered=0`）
错误：401
说明：优先校验签名；也可用 URL 中的 secret。按 delivery 去重。

### POST /webhook/pipelines/{pipeline_id}/{secret} — 接收流水线 Webhook

认证：不需要
路径参数：pipeline_id*: integer, secret*: string
响应 202（可能为重复投递，`triggered=0`）
错误：401
说明：签名/去重同构建任务；首版不做分支匹配（流水线无单一分支）。

### POST /webhook/repos/{repository_id}/{secret} — 已废弃的仓库 Webhook（返回 410）

认证：不需要
状态：已废弃
路径参数：repository_id*: integer, secret*: string
错误：410

## 对象形状

### BuildDeployAttempt

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_run_id` | `integer` |  |  |
| `batch_no` | `integer` |  |  |
| `deploy_target_id` | `integer` |  |  |
| `target_snapshot_json` | `string` |  |  |
| `status` | `string` |  |  |
| `log_path` | `string` |  |  |
| `error_message` | `string` |  |  |
| `started_at` | `string(date-time)` |  |  |
| `finished_at` | `string(date-time)` |  |  |
| `created_at` | `string(date-time)` |  |  |

### BuildJob

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `repository_id` | `integer` |  |  |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `is_public` | `boolean` |  |  |
| `branch` | `string` |  |  |
| `shallow_clone` | `boolean` |  |  |
| `build_script_type` | `string` |  |  |
| `build_script` | `string` |  |  |
| `post_build_script` | `string` |  | 构建成功后、缓存/归档前执行；与主脚本同 cwd/env；失败则 run failed |
| `work_dir` | `string` |  | 相对仓库工作区的构建 cwd；不存在时明确报错 |
| `workspace_path` | `string` |  | 只读；绝对路径 `{workspace}/jobs/job-{id}/` |
| `artifact_paths` | `string[]` |  | 相对仓库根制品路径（文件/目录）；单文件不压缩，多路径打成一包；缺失则构建失败 |
| `output_dir` | `string` |  | 废弃兼容字段：等于 `artifact_paths[0]` |
| `cache_paths` | `string` |  | JSON 数组字符串，相对仓库根的缓存路径列表 |
| `env_var_names` | `string[]` |  | 宿主机环境变量名；运行时 LookupEnv 注入 |
| `env_vars` | `{ key: string, has_value: boolean }[]` |  | 仅投影键与是否有值；永不回显明文（存于 `env_vars_cipher`） |
| `trigger_manual` | `boolean` |  |  |
| `trigger_webhook` | `boolean` |  |  |
| `trigger_cron` | `boolean` |  |  |
| `webhook_secret` | `string` |  | Only present on secret view/rotate |
| `webhook_type` | `string` |  |  |
| `webhook_ref_path` | `string` |  |  |
| `webhook_commit_path` | `string` |  |  |
| `webhook_message_path` | `string` |  |  |
| `cron_expression` | `string` |  |  |
| `cron_timezone` | `string` |  |  |
| `max_artifacts` | `integer` |  |  |
| `artifact_format` | `string` |  |  |
| `agent_trigger_event` | `'artifact_ready' \| 'distribution_finished' \| 'none'` |  | Default artifact_ready; override distribution_finished or none |
| `agent_ids` | `integer[]` |  | Agents executed on the build-event trigger |
| `is_public` | `boolean` |  | 公开只读；默认 false |
| `deploy_targets` | `DeployTarget[]` |  |  |
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### BuildJobCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `repository_id` | `integer` | 是 |  |
| `name` | `string` | 是 |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `is_public` | `boolean` |  |  |
| `branch` | `string` |  |  |
| `shallow_clone` | `boolean` |  |  |
| `build_script_type` | `string` |  |  |
| `build_script` | `string` |  |  |
| `post_build_script` | `string` |  |  |
| `work_dir` | `string` |  |  |
| `artifact_paths` | `string[]` |  | 优先；为空且提供 `output_dir` 时视为单元素 |
| `output_dir` | `string` |  | 废弃；仅当 `artifact_paths` 为空时作为单路径写入 |
| `cache_paths` | `string` |  |  |
| `env_var_names` | `string[]` |  |  |
| `env_vars` | `{ key: string, value?: string }[]` |  | 全量键列表；带 value 设置/更新；key 非空且不得含 `=` / 换行 |
| `trigger_manual` | `boolean` |  |  |
| `trigger_webhook` | `boolean` |  |  |
| `trigger_cron` | `boolean` |  |  |
| `webhook_secret` | `string` |  | Only present on secret view/rotate |
| `webhook_type` | `string` |  |  |
| `webhook_ref_path` | `string` |  |  |
| `webhook_commit_path` | `string` |  |  |
| `webhook_message_path` | `string` |  |  |
| `cron_expression` | `string` |  |  |
| `cron_timezone` | `string` |  |  |
| `max_artifacts` | `integer` |  |  |
| `artifact_format` | `string` |  |  |
| `agent_trigger_event` | `'artifact_ready' \| 'distribution_finished' \| 'none'` |  |  |
| `agent_ids` | `integer[]` |  |  |
| `deploy_targets` | `DeployTarget[]` |  |  |

### BuildJobPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `BuildJob[]` |  |  |

### BuildJobUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `branch` | `string` |  |  |
| `shallow_clone` | `boolean` |  |  |
| `build_script_type` | `string` |  |  |
| `build_script` | `string` |  |  |
| `post_build_script` | `string` |  |  |
| `work_dir` | `string` |  |  |
| `artifact_paths` | `string[]` |  | 优先；省略则不改 |
| `output_dir` | `string` |  | 废弃；仅当未传 `artifact_paths` 或为空时作为单路径写入 |
| `cache_paths` | `string` |  |  |
| `env_var_names` | `string[]` |  |  |
| `env_vars` | `{ key: string, value?: string }[]` |  | 全量键列表；带 value 更新/新建；已有键未带 value 保留；请求中消失的键删除；省略则不改 |
| `trigger_manual` | `boolean` |  |  |
| `trigger_webhook` | `boolean` |  |  |
| `trigger_cron` | `boolean` |  |  |
| `webhook_secret` | `string` |  | Only present on secret view/rotate |
| `webhook_type` | `string` |  |  |
| `webhook_ref_path` | `string` |  |  |
| `webhook_commit_path` | `string` |  |  |
| `webhook_message_path` | `string` |  |  |
| `cron_expression` | `string` |  |  |
| `cron_timezone` | `string` |  |  |
| `max_artifacts` | `integer` |  |  |
| `artifact_format` | `string` |  |  |
| `agent_trigger_event` | `'artifact_ready' \| 'distribution_finished' \| 'none'` |  |  |
| `agent_ids` | `integer[]` |  |  |
| `deploy_targets` | `DeployTarget[]` |  |  |

### BuildRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_job_id` | `integer` |  |  |
| `build_number` | `integer` |  |  |
| `status` | `'queued' \| 'running' \| 'success' \| 'failed' \| 'cancelled' \| 'interrupted'` |  |  |
| `stage` | `'pending' \| 'cloning' \| 'building' \| 'archiving' \| 'distributing' \| 'idle'` |  |  |
| `trigger_type` | `string` |  |  |
| `triggered_by` | `integer` |  |  |
| `branch` | `string` |  |  |
| `commit_hash` | `string` |  |  |
| `commit_message` | `string` |  |  |
| `log_path` | `string` |  |  |
| `artifact_path` | `string` |  | 存储的制品文件路径；无制品时为空 |
| `artifact_kind` | `'file' \| 'archive' \| 'bundle'` |  | 单文件 / 单目录归档 / 多路径打包；用于 redeploy 还原 |
| `duration_ms` | `integer` |  |  |
| `error_message` | `string` |  |  |
| `distribution_summary` | `'none' \| 'running' \| 'all_success' \| 'partial' \| 'all_failed' \| 'cancelled'` |  |  |
| `snapshot_json` | `string` |  |  |
| `started_at` | `string(date-time)` |  |  |
| `finished_at` | `string(date-time)` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `deploy_attempts` | `BuildDeployAttempt[]` |  |  |

### BuildRunPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `BuildRun[]` |  |  |

### DeployTarget

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_job_id` | `integer` |  |  |
| `server_id` | `integer` |  |  |
| `remote_path` | `string` |  |  |
| `method` | `'rsync' \| 'sftp' \| 'scp' \| 'agent' \| 'local'` |  |  |
| `post_deploy_script` | `string` |  |  |
| `sort_order` | `integer` |  |  |

### BuildPipeline

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `enabled` | `boolean` |  |  |
| `graph_json` | `string` |  | VueFlow `{nodes,edges}` v2；节点 `type` + `data.{build_job_id,script_job_id,agent_id,env_vars}`；边 `data.condition`；env_vars 值不回显 |
| `trigger_manual` | `boolean` |  |  |
| `trigger_webhook` | `boolean` |  |  |
| `trigger_cron` | `boolean` |  |  |
| `webhook_secret` | `string` |  | Only present on secret view/rotate |
| `webhook_type` | `string` |  |  |
| `cron_expression` | `string` |  |  |
| `cron_timezone` | `string` |  |  |
| `is_public` | `boolean` |  |  |
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### PipelineRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_pipeline_id` | `integer` |  |  |
| `run_number` | `integer` |  |  |
| `status` | `'queued' \| 'running' \| 'success' \| 'failed' \| 'cancelled'` |  |  |
| `trigger_type` | `string` |  | manual / cron / webhook |
| `triggered_by` | `integer` |  |  |
| `snapshot_json` | `string` |  | 触发时固化的 graph_json |
| `error_message` | `string` |  |  |
| `started_at` | `string(date-time)` |  |  |
| `finished_at` | `string(date-time)` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `stages` | `PipelineStageRun[]` |  | 详情含 |

### PipelineStageRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `pipeline_run_id` | `integer` |  |  |
| `node_id` | `string` |  | 对应 graph 节点 id |
| `node_type` | `'start' \| 'end' \| 'buildJob' \| 'scriptJob' \| 'agent'` |  | 节点类型 |
| `build_job_id` | `integer` |  | buildJob 节点引用；其它类型为 0 |
| `build_run_id` | `integer` |  | 关联 BuildRun；可空 |
| `script_job_id` | `integer` |  | scriptJob 节点引用；其它类型为 0 |
| `script_run_id` | `integer` |  | 关联 ScriptRun；可空 |
| `agent_id` | `integer` |  | agent 节点引用；其它类型为 0 |
| `agent_run_id` | `integer` |  | 关联 AgentRun；可空 |
| `status` | `'pending' \| 'queued' \| 'running' \| 'success' \| 'failed' \| 'cancelled' \| 'skipped' \| 'interrupted'` |  |  |
| `error_message` | `string` |  |  |
| `started_at` | `string(date-time)` |  |  |
| `finished_at` | `string(date-time)` |  |  |
| `created_at` | `string(date-time)` |  |  |

## 脚本模板

`build_script` / `post_build_script` 在启动解释器前做**一次性文本替换**（非 shell 求值）。语法：`${{ path }}`（`path` 两侧可有空白）。

| 变量 | 说明 |
| --- | --- |
| `job.id` / `job.name` | 构建任务 |
| `run.id` / `run.build_number` / `run.branch` / `run.commit` | 本次运行（`commit` 为 hash） |
| `workspace` | 任务 checkout 绝对路径 |
| `env.KEY` | 任务配置的环境变量（`env_var_names` LookupEnv + `env_vars` Key-Value；同名以 Key-Value 为准） |

规则：标识段为 `[A-Za-z_][A-Za-z0-9_]*`，允许点分路径；未知变量 → 构建失败；替换值内若含 `${{` 不再二次展开。可与 bash `$VAR`、Python `{}`、PowerShell `$x` 共存。
