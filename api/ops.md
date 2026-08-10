# 运维与仪表盘

仪表盘卡片，以及进程 / 开发环境相关接口。开发环境页同时管理「开发语言」与「智能体 CLI」；CLI 的 HTTP 路径见 [resource.md](resource.md)（权限同为 `ops_dev_environments:*`）。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [docs/DESIGN.md](../docs/DESIGN.md)。

## 仪表盘

### GET /dashboard/layout — 获取当前用户仪表盘布局

权限：`dashboard:view`
响应 200：data = DashboardLayout
错误：403

### PUT /dashboard/layout — 保存当前用户仪表盘布局

权限：`dashboard:view`
请求：{ cards* }
响应 200：data = DashboardLayout
错误：403

### GET /dashboard/build-summary — 构建摘要卡片数据

权限：`cicd_build_runs:view`
响应 200：data = BuildSummary
错误：403

### GET /dashboard/agent-run-summary — 智能体运行摘要卡片数据

权限：`ai_runs:view`
响应 200：data = AgentRunSummary
错误：403

### GET /dashboard/system-info — 系统信息卡片数据

权限：`dashboard:system_info`
响应 200：data = SystemInfo
错误：403
说明：有权限的非超管也可见；不因此获得运维写权限。

### GET /dashboard/system-status — 系统状态卡片数据

权限：`dashboard:system_status`
响应 200：data = SystemStatus
错误：403
说明：`disk_*` 为关键数据目录所在分区的宿主机磁盘占用；`directories` 为各关键目录自身占用大小（非分区剩余空间）。

### GET /dashboard/script-run-summary — 脚本运行摘要卡片数据

权限：`cicd_script_runs:view`
响应 200：data = ScriptRunSummary
错误：403

### GET /dashboard/pipeline-run-summary — 流水线运行摘要卡片数据

权限：`cicd_pipeline_runs:view`
响应 200：data = PipelineRunSummary
错误：403

### GET /dashboard/task-overview — 任务概览卡片数据

权限：`dashboard:view`（分项按 `cicd_build_jobs:view` / `cicd_script_jobs:view` / `cicd_pipelines:view` 返回对应计数，无权限项为 `null`）
响应 200：data = TaskOverview
错误：403

### GET /dashboard/my-projects — 我的项目卡片数据

权限：`project_projects:view`
响应 200：data = MyProject[]
错误：403
说明：返回当前用户作为成员或创建者的项目，按 `updated_at` 倒序，最多 10 条。

### GET /ws/dashboard — 仪表盘实时推送

认证：查询参数 `token`（JWT / PAT），与 REST 相同；校验 WebSocket Origin（CORS 配置）。
权限：有 `dashboard:view` 订阅 `dashboard:runs`；有 `dashboard:system_status` 额外订阅 `dashboard:system-status`。
推送消息（JSON text）：
- `{"type":"run_changed","run_type":"build|script|pipeline","run_id":N,"status":"..."}` — 运行状态变更（不含敏感数据，卡片数据仍走各自 REST）
- `{"type":"system_status","data":{...}}` — 系统状态全量（服务端单例采集，约 3s，仅在有订阅者时采样）

## 运维

### GET /ops/processes — 列出主机进程（仅超管）

权限：`ops_processes:view`
查询参数：keyword: string, pid: integer, port: integer, sort: string
响应 200：data = object
错误：403
说明：返回过滤后的完整进程列表，不分页。

### POST /ops/processes/{pid}/kill — 按 PID 结束进程

权限：`ops_processes:execute`
路径参数：pid*: integer
响应 200：Terminated
错误：403

### GET /ops/dev-environments — 列出开发环境

权限：`ops_dev_environments:view`
响应 200：data = object
错误：403

### POST /ops/dev-environments — 创建自定义开发环境

权限：`ops_dev_environments:create`
请求：{ name*, executable*, description, detect_script, install_script, upgrade_script, uninstall_script, versions_script, switch_script, default_version }
响应 201
错误：403
说明：自定义脚本以 Bedrock 进程 UID 运行，无沙箱。

### PUT /ops/dev-environments/{id} — 更新自定义开发环境

权限：`ops_dev_environments:update`
路径参数：id*: integer
请求：{ name*, executable*, description, detect_script, install_script, upgrade_script, uninstall_script, versions_script, switch_script, default_version }
响应 200：data = DevEnvironment
错误：403

### DELETE /ops/dev-environments/{id} — 删除自定义开发环境

权限：`ops_dev_environments:delete`
路径参数：id*: integer
响应 200
错误：403

### POST /ops/dev-environments/{id}/detect — 检测开发环境

权限：`ops_dev_environments:execute`
路径参数：id*: integer
响应 200：data = DevEnvironmentDetectResult
错误：403

### POST /ops/dev-environments/{id}/install — 安装开发环境（异步）

权限：`ops_dev_environments:execute`
路径参数：id*: integer
请求：{ version }
响应 202：data = DevEnvJob
错误：403

### POST /ops/dev-environments/{id}/upgrade — 升级开发环境（异步）

权限：`ops_dev_environments:execute`
路径参数：id*: integer
请求：{ version }
响应 202：data = DevEnvJob
错误：403

### POST /ops/dev-environments/{id}/uninstall — 卸载开发环境（异步）

权限：`ops_dev_environments:execute`
路径参数：id*: integer
请求：{ version }
响应 202：data = DevEnvJob
错误：403

### POST /ops/dev-environments/{id}/switch — 切换开发环境版本（异步）

权限：`ops_dev_environments:execute`
路径参数：id*: integer
请求：{ version }
响应 202：data = DevEnvJob
错误：403

### GET /ops/dev-environments/{id}/sources — 列出安装源

权限：`ops_dev_environments:view`
路径参数：id*: integer
响应 200：data = DevEnvInstallSourceList
错误：403

### POST /ops/dev-environments/{id}/sources — 添加安装源

权限：`ops_dev_environments:create`
路径参数：id*: integer
请求：{ name*, base_url*, priority*, enabled* }
响应 201：data = DevEnvInstallSource
错误：403

### PUT /ops/dev-environments/{id}/sources/{sourceId} — 更新安装源

权限：`ops_dev_environments:update`
路径参数：id*: integer, sourceId*: integer
请求：{ name*, base_url*, priority*, enabled* }
响应 200：data = DevEnvInstallSource
错误：403

### DELETE /ops/dev-environments/{id}/sources/{sourceId} — 删除安装源

权限：`ops_dev_environments:delete`
路径参数：id*: integer, sourceId*: integer
响应 200
错误：403

### POST /ops/dev-environments/{id}/sources/{sourceId}/ping — 探测安装源

权限：`ops_dev_environments:execute`
路径参数：id*: integer, sourceId*: integer
响应 200：data = DevEnvInstallSourcePingResult
错误：403

### GET /ops/dev-environments/{id}/jobs — 列出开发环境任务

权限：`ops_dev_environments:view`
路径参数：id*: integer
查询参数：page: integer, page_size: integer, status: string
响应 200：data = DevEnvJobPage
错误：403

### GET /ops/dev-environments/{id}/jobs/{jobId} — 获取开发环境任务

权限：`ops_dev_environments:view`
路径参数：id*: integer, jobId*: integer
响应 200：data = DevEnvJob
错误：403

### GET /ops/dev-environments/{id}/jobs/{jobId}/logs — 获取开发环境任务日志

权限：`ops_dev_environments:view`
路径参数：id*: integer, jobId*: integer
响应 200：data = text/plain
错误：403

### POST /ops/dev-environments/{id}/jobs/{jobId}/retry — 重试开发环境任务

权限：`ops_dev_environments:execute`
路径参数：id*: integer, jobId*: integer
响应 202：data = DevEnvJob
错误：403

## 对象形状

### AgentRunSummary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `running` | `integer` |  |  |
| `queued` | `integer` |  | 含 queued 与 pending |
| `success_rate` | `number` |  |  |
| `recent` | `DashboardRecentAgentRun[]` |  |  |

### BuildSummary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `running` | `integer` |  |  |
| `queued` | `integer` |  |  |
| `success_rate` | `number` |  |  |
| `recent` | `DashboardRecentBuildRun[]` |  |  |

### ScriptRunSummary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `running` | `integer` |  |  |
| `queued` | `integer` |  |  |
| `success_rate` | `number` |  |  |
| `recent` | `DashboardRecentScriptRun[]` |  |  |

### PipelineRunSummary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `running` | `integer` |  |  |
| `queued` | `integer` |  |  |
| `success_rate` | `number` |  |  |
| `recent` | `DashboardRecentPipelineRun[]` |  |  |

### TaskOverview

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `build_jobs` | `integer \| null` |  | 无 `cicd_build_jobs:view` 时为 `null` |
| `script_jobs` | `integer \| null` |  | 无 `cicd_script_jobs:view` 时为 `null` |
| `pipelines` | `integer \| null` |  | 无 `cicd_pipelines:view` 时为 `null` |

### MyProject

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `slug` | `string` |  |  |
| `status` | `string` |  |  |
| `my_role` | `string` |  | 项目成员角色；非成员为空 |

### DashboardCardLayout

12 列网格几何（GridStack）。`order` 由服务端按 `y * 12 + x` 归一；旧数据缺 `x/y/w/h` 时按卡片默认几何补全。

默认几何：`build_summary` `(0,0) 6×4`，`agent_run_summary` `(6,0) 6×4`，`system_info` `(0,4) 6×3`，`system_status` `(6,4) 6×3`，`script_run_summary` `(0,7) 6×4`，`pipeline_run_summary` `(6,7) 6×4`，`cicd_task_overview` `(0,11) 6×3`，`my_projects` `(6,11) 6×3`。

校验：`w`/`h` 最小 2，`w` 最大 12；未知或无权限 `id` 拒绝。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `'build_summary' \| 'agent_run_summary' \| 'system_info' \| 'system_status' \| 'script_run_summary' \| 'pipeline_run_summary' \| 'cicd_task_overview' \| 'my_projects'` | 是 |  |
| `visible` | `boolean` | 是 |  |
| `order` | `integer` | 是 | 由 `y * 12 + x` 归一，兼容旧客户端 |
| `x` | `integer` | 是 | 列起点（0-based） |
| `y` | `integer` | 是 | 行起点（0-based） |
| `w` | `integer` | 是 | 宽度（列数，2–12） |
| `h` | `integer` | 是 | 高度（行数，≥2） |

### DashboardLayout

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cards` | `DashboardCardLayout[]` | 是 |  |

### DashboardRecentBuildRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_job_id` | `integer` |  |  |
| `build_number` | `integer` |  |  |
| `status` | `string` |  |  |
| `branch` | `string` |  |  |
| `created_at` | `string(date-time)` |  |  |

### DashboardRecentAgentRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `agent_id` | `integer` |  |  |
| `agent_name` | `string` |  |  |
| `trigger_type` | `string` |  |  |
| `status` | `string` |  |  |
| `created_at` | `string(date-time)` |  |  |

### DashboardRecentScriptRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `script_job_id` | `integer` |  |  |
| `job_name` | `string` |  | 父任务名称 |
| `run_number` | `integer` |  |  |
| `status` | `string` |  |  |
| `created_at` | `string(date-time)` |  |  |

### DashboardRecentPipelineRun

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `build_pipeline_id` | `integer` |  |  |
| `pipeline_name` | `string` |  | 父流水线名称 |
| `run_number` | `integer` |  |  |
| `status` | `string` |  |  |
| `created_at` | `string(date-time)` |  |  |

### DevEnvInstallSource

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `environment_id` | `integer` |  |  |
| `name` | `string` |  |  |
| `base_url` | `string` |  |  |
| `priority` | `integer` |  |  |
| `enabled` | `boolean` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### DevEnvInstallSourceInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `base_url` | `string` | 是 |  |
| `priority` | `integer` | 是 |  |
| `enabled` | `boolean` | 是 |  |

### DevEnvInstallSourceList

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `DevEnvInstallSource[]` |  |  |

### DevEnvInstallSourcePingResult

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `ok` | `boolean` |  |  |
| `detail` | `string` |  |  |

### DevEnvJob

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `environment_id` | `integer` |  |  |
| `operation` | `'install' \| 'upgrade' \| 'uninstall' \| 'switch'` |  |  |
| `requested_version` | `string` |  |  |
| `status` | `'queued' \| 'running' \| 'success' \| 'failed' \| 'interrupted'` |  |  |
| `source_id` | `integer` |  |  |
| `command_snapshot` | `string` |  |  |
| `error_message` | `string` |  |  |
| `created_by` | `integer` |  |  |
| `started_at` | `string(date-time)` |  |  |
| `finished_at` | `string(date-time)` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `environment` | `DevEnvironment` |  |  |
| `source` | `DevEnvInstallSource` |  |  |

### DevEnvJobPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `DevEnvJob[]` |  |  |

### DevEnvironment

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `kind` | `'builtin' \| 'custom'` |  |  |
| `executable` | `string` |  |  |
| `description` | `string` |  |  |
| `detect_script` | `string` |  |  |
| `install_script` | `string` |  |  |
| `upgrade_script` | `string` |  |  |
| `uninstall_script` | `string` |  |  |
| `versions_script` | `string` |  |  |
| `switch_script` | `string` |  |  |
| `default_version` | `string` |  |  |
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |
| `sources` | `DevEnvInstallSource[]` |  |  |

### DevEnvironmentDetectResult

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `detected` | `boolean` |  |  |
| `output` | `string` |  |  |

### DevEnvironmentInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `executable` | `string` | 是 |  |
| `description` | `string` |  |  |
| `detect_script` | `string` |  |  |
| `install_script` | `string` |  |  |
| `upgrade_script` | `string` |  |  |
| `uninstall_script` | `string` |  |  |
| `versions_script` | `string` |  |  |
| `switch_script` | `string` |  |  |
| `default_version` | `string` |  |  |

### DevEnvironmentJobInput

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `version` | `string` |  |  |

### DirectoryUsage

关键数据目录的占用大小（目录树合计），不是所在分区的剩余空间。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `path` | `string` |  | 目录绝对/配置路径 |
| `used_bytes` | `integer` |  | 该目录内容占用字节数 |

### ProcessInfo

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `pid` | `integer` |  |  |
| `name` | `string` |  |  |
| `cpu_percent` | `number` |  |  |
| `memory_bytes` | `integer` |  |  |
| `username` | `string` |  |  |
| `num_threads` | `integer` |  |  |
| `status` | `string` |  | OS process status (e.g. R |
| `start_time` | `integer` |  | Process start time as Unix epoch milliseconds |
| `cmdline` | `string` |  |  |
| `ports` | `integer[]` |  |  |

### SystemInfo

Complete read-only system information; this does not grant operations write access.

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `version` | `string` |  |  |
| `os` | `string` |  |  |
| `arch` | `string` |  |  |
| `runtime` | `string` |  |  |
| `hostname` | `string` |  |  |
| `start_time` | `string(date-time)` |  |  |

### SystemStatus

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cpu_usage_percent` | `number` |  |  |
| `memory_used_bytes` | `integer` |  |  |
| `memory_total_bytes` | `integer` |  |  |
| `memory_usage_percent` | `number` |  |  |
| `disk_used_bytes` | `integer` |  | 宿主机数据盘已用字节（关键目录所在分区） |
| `disk_total_bytes` | `integer` |  | 宿主机数据盘总容量 |
| `disk_free_bytes` | `integer` |  | 宿主机数据盘可用字节 |
| `disk_usage_percent` | `number` |  | 宿主机数据盘占用百分比 |
| `health` | `'ok' \| 'degraded'` |  |  |
| `directories` | `DirectoryUsage[]` |  | 关键目录各自占用大小 |
| `collected_at` | `string(date-time)` |  |  |
