# CI/CD 脚本任务

无仓库、无制品、无部署的精简脚本任务与执行记录。

通用约定见 [.agents/api.md](../.agents/api.md)。构建任务见 [cicd.md](cicd.md)。

## 脚本任务

### GET /script-jobs — 列出脚本任务

权限：`cicd_script_jobs:view`
查询参数：page: integer, page_size: integer, keyword: string
响应 200：data = ScriptJobPage

### POST /script-jobs — 创建脚本任务

权限：`cicd_script_jobs:create`
请求：{ name*, description, enabled, script_type, script, work_dir, env_var_names, env_vars, trigger_manual, trigger_webhook, trigger_cron, cron_expression, cron_timezone, webhook_type, is_public }
响应 201：data = ScriptJob
说明：自动生成 `webhook_secret`。响应只读字段 `workspace_path` 为绝对路径 `{build.workspace_dir}/scripts/script-{id}/`（跨 run 持久复用，不清空）。`script` 执行前做 `${{...}}` 文本替换；内置变量：`job.id`、`job.name`、`run.id`、`workspace`；用户变量 `${{ env.KEY }}`。未知变量 → 执行失败。

### GET /script-jobs/{id} — 获取脚本任务

权限：`cicd_script_jobs:view`
路径参数：id*: integer
响应 200：data = ScriptJob

### PUT /script-jobs/{id} — 更新脚本任务

权限：`cicd_script_jobs:update`
路径参数：id*: integer
请求：同创建（字段均可选）
响应 200：data = ScriptJob

### DELETE /script-jobs/{id} — 删除脚本任务

权限：`cicd_script_jobs:delete`
路径参数：id*: integer
响应 200

### GET /script-jobs/{id}/webhook-secret — 查看 Webhook 密钥与 URL

权限：`cicd_script_jobs:view`
路径参数：id*: integer
响应 200：{ webhook_secret, webhook_url }
说明：`webhook_url` 形如 `/api/v1/webhook/script-jobs/{id}/{secret}`。

### POST /script-jobs/{id}/webhook-secret/rotate — 轮换 Webhook 密钥

权限：`cicd_script_jobs:update`
路径参数：id*: integer
响应 200：{ webhook_secret, webhook_url }

### POST /script-jobs/{id}/runs — 入队脚本运行

权限：`cicd_script_jobs:execute`
路径参数：id*: integer
响应 202：data = ScriptRun

## 脚本运行

### GET /script-runs — 列出脚本运行

权限：`cicd_script_runs:view`
查询参数：page: integer, page_size: integer, script_job_id: integer, status: string, sort: string
响应 200：data = ScriptRunPage

### GET /script-runs/{id} — 获取脚本运行详情

权限：`cicd_script_runs:view`
路径参数：id*: integer
响应 200：data = ScriptRun

### POST /script-runs/{id}/cancel — 取消脚本运行

权限：`cicd_script_jobs:execute`
路径参数：id*: integer
响应 200：data = ScriptRun

### POST /script-runs/{id}/retry — 重试（新建一次脚本运行）

权限：`cicd_script_jobs:execute`
路径参数：id*: integer
响应 202：data = ScriptRun

### GET /script-runs/{id}/log — 获取脚本日志文本

权限：`cicd_script_runs:view`
路径参数：id*: integer
响应 200：data = text/plain

### GET /ws/script-runs/{id}/logs — 脚本日志 WebSocket（实时）

路径前缀为 `/ws`（非 `/api/v1`）。查询参数 `token` 携带 JWT。

权限：`cicd_script_runs:view`
路径参数：id*: integer
查询参数：token*: string

连接成功后先回放已有日志，再推送实时行；控制帧 `__REFRESH__` 表示元数据变更，客户端应重新 `GET /script-runs/{id}`。

## Webhook

### POST /webhook/script-jobs/{script_job_id}/{secret} — 接收脚本任务 Webhook

认证：不需要
路径参数：script_job_id*: integer, secret*: string
响应 202（可能为重复投递，`triggered=0`）
错误：401
说明：URL secret 校验；按 delivery 去重；**无分支匹配**。有平台签名头时优先校验签名。

## 对象形状

### ScriptJob

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `integer` |  |
| `name` | `string` |  |
| `description` | `string` |  |
| `enabled` | `boolean` |  |
| `script_type` | `string` | bash / node / python / pwsh / powershell / cmd |
| `script` | `string` |  |
| `work_dir` | `string` | 相对脚本工作区的 cwd |
| `workspace_path` | `string` | 只读绝对路径 |
| `env_var_names` | `string[]` |  |
| `env_vars` | `{ key, has_value }[]` | 永不回显明文 |
| `trigger_manual` | `boolean` |  |
| `trigger_webhook` | `boolean` |  |
| `trigger_cron` | `boolean` |  |
| `webhook_type` | `string` | 默认 generic |
| `cron_expression` | `string` |  |
| `cron_timezone` | `string` |  |
| `is_public` | `boolean` |  |
| `created_by` | `integer` |  |
| `created_at` | `string(date-time)` |  |
| `updated_at` | `string(date-time)` |  |

### ScriptRun

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `integer` |  |
| `script_job_id` | `integer` |  |
| `run_number` | `integer` |  |
| `status` | `string` | queued \| running \| success \| failed \| cancelled \| interrupted |
| `stage` | `string` | pending \| running \| idle |
| `trigger_type` | `string` | manual \| webhook \| cron \| retry |
| `triggered_by` | `integer` |  |
| `log_path` | `string` | `{log_dir}/script-{jobID}/run-{NNN}.log` |
| `duration_ms` | `integer` |  |
| `error_message` | `string` |  |
| `snapshot_json` | `string` |  |
| `started_at` | `string(date-time)` |  |
| `finished_at` | `string(date-time)` |  |
| `created_at` | `string(date-time)` |  |
