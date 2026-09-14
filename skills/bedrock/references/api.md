# Bedrock HTTP API 摘要

服务端为 Go (gin) 单体，路由前缀 `/api/v1`；完整契约见仓库 `api/` 目录（`api/cicd.md`、`api/cicd-scripts.md`、`api/ai.md`、`api/resource.md`、`api/project.md`），通用约定见 `API-SPEC.md`。`scripts/bedrock.mjs` 已封装以下全部调用，一般无需直接发 HTTP；本文用于排查与扩展。

## 认证与信封

- PAT 认证：请求头 `Authorization: Bearer br_xxx`。服务端按 `br_` 前缀识别 PAT（否则按 JWT 解析），PAT 创建接口为 `POST /resource/tokens`（需先用 JWT 登录），响应 `data.token` 是明文令牌，只在创建时返回一次。
- PAT scope：`builds:run` / `scripts:run` / `pipelines:run` / `agents:run`（触发类）与 `skills:read`、`docs:read|write`、`dev_docs:read|write`、`bugs:read|write`。触发接口缺 scope 时返回 403。
- 响应信封：`{ code, message, data?, request_id? }`，`code=0` 为成功；分页响应 `data = { items, total, page, page_size, total_pages }`。
- 写接口支持 `Idempotency-Key` 头幂等。列表接口通用参数：`page`、`page_size`、`keyword`、`sort=<field>@asc|desc`。
- 健康检查：`GET /api/v1/health`（免认证）。

## 端点速查

| 能力 | 触发 | 查状态 | 日志 |
| --- | --- | --- | --- |
| 构建 | `POST /build-jobs/:id/runs`，body `{branch?, trigger_type?}`，202 | `GET /build-runs/:id` | `GET /build-runs/:id/log`（text/plain） |
| 脚本任务 | `POST /script-jobs/:id/runs`，body 可空，202 | `GET /script-runs/:id` | `GET /script-runs/:id/log`（text/plain） |
| 流水线 | `POST /build-pipelines/:id/runs`，body `{trigger_type?}`，202 | `GET /pipeline-runs/:id` | 无纯文本日志，看 `stages[]` |
| 智能体 | `POST /ai/agents/:id/api-runs`，body `{user_prompt?}`，202（PAT 专用；JWT 用 `/ai/agents/:id/runs`） | `GET /ai/runs/:id` | 实时日志走 WS，最终结果看 `output_text` |

列表查询（用于 `search` 与按 name 找 id）：

- `GET /build-jobs?page&page_size&keyword` → items 含 `id/name/repository_id/branch/enabled`
- `GET /script-jobs?page&page_size&keyword`
- `GET /build-pipelines?page&page_size&keyword`
- `GET /ai/agents?page&page_size` → items 含 `id/name/enabled/cli_key/workspace_status`

辅助端点：`POST /build-runs/:id/cancel`、`POST /build-runs/:id/retry`、`GET /build-runs/:id/artifact`；脚本运行与流水线运行同理（`/script-runs/:id/cancel` 等）；智能体 `POST /ai/runs/:id/cancel`、`GET /ai/runs/:id/artifact`（成功后产出 zip）。

## 缺陷（bug 命令组）

绑定与闭环流程见 `bugs.md`；完整契约见仓库 `api/project.md`（缺陷部分）。

- 项目解析：`GET /projects?page&page_size&keyword` — PAT `bugs:read` 返回精简 `items`（仅 `id` / `name` / `slug`），供 `search --type projects` 与 `bugs.project_slug` → 项目 ID 解析。
- 列表：`GET /projects/bugs?project_id&assignee&exclude_closed=true&status&page&page_size` — 跨项目聚合查询；`assignee` 接受用户名或用户 ID（两者同时传以 `assignee` 为准）；`exclude_closed=true` 一次拉取「未关闭」口径（排除 `closed`）；响应分页 `items/total/page/page_size/total_pages`，条目为 ProjectBug（含 `project_name`、`assignee_username` 等附加字段）。
- 详情：`GET /projects/{id}/bugs/{bugID}` — 描述、状态、severity / priority、经办人、分支等（读需 `bugs:read`）。
- 评论：`GET /projects/{id}/bugs/{bugID}/comments`（数组）、`POST .../comments`，body `{ content }`（写需 `bugs:write`）。
- 活动记录：`GET /projects/{id}/bugs/{bugID}/activities` — `action ∈ create / status_change / comment`，流转自动记录 `from_status → to_status`。
- 流转：`PUT /projects/{id}/bugs/{bugID}/status`，body `{ status, comment? }`（写需 `bugs:write`）；状态机五种合法状态 `open` / `in_progress` / `resolved` / `closed` / `rejected`，其余值 400。
- 附件：`GET .../attachments` 列表、`GET .../attachments/{attachmentID}/download` 下载（读需 `bugs:read`；CLI 暂未封装，图片预览到平台 Web 查看）。

## 状态机

- 构建/脚本/流水线运行：`queued → running → success | failed | cancelled`（构建另有 `interrupted`）。
- 智能体运行：`pending → queued → running → success | failed | cancelled`。
- 终态即上表右侧四种；`success` 之外都算失败。

## 关键对象字段

- BuildRun：`id, build_number, status, stage, duration_ms, error_message, artifact_path, deploy_attempts`
- ScriptRun：`id, run_number, status, stage, duration_ms, error_message, log_path`
- PipelineRun：`id, run_number, status, stages[]`，每个 stage 为 `{ node_type, build_run_id, script_run_id, agent_run_id, status }`
- AgentRun：`id, status, output_text, work_dir, artifact_path, user_prompt, duration_ms, error_message`

## 实时日志（WebSocket，CLI 暂未封装）

`WS /ws/build-runs/:id/logs?token=<JWT>`、`/ws/script-runs/:id/logs?token=`、`/ws/ai/runs/:id/logs?token=`。注意 WS 不带 `/api/v1` 前缀、挂在 `/ws` 下，认证用 query 参数 `token`（JWT；PAT 能否用于 WS 以服务端实现为准）。构建 WS 中 `__REFRESH__` 控制帧表示需要重拉详情。
