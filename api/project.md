# 项目协作

项目、成员、需求、缺陷、评论、附件、文档发布。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。

**读可见性**：持有 `project_projects:view`（或对应子域 `:view`）且满足数据范围：角色 `data_scope=all`、超管或 `project_projects:manage_all` 可读全部项目；`data_scope=self`（默认）仅可读本人为成员或创建人的项目。写操作（创建/更新/归档/删除/成员管理及需求/文档写）仍走项目成员 ACL 或 `manage_all`。非成员响应中 `my_role` 可为空（omit），`permissions` 能力位均为 false。`is_public` 字段保留兼容，**不再影响**项目读可见性。

## 项目

### GET /projects — 列出项目

权限：`project_projects:view`
查询参数：page: integer, page_size: integer, keyword: string, status: 'active' | 'archived', sort: string
响应 200：data = ProductProjectPage
错误：403
说明：`data_scope=self` 时仅列出本人为成员或创建人的项目；`data_scope=all`、超管或 `manage_all` 可列出全部。写能力由每条 `permissions` / `my_role` 表达。

### POST /projects — 创建项目（创建者成为 Owner）

权限：`project_projects:create`
请求：{ name*, slug*, description, repository_id, tags }
响应 201

### GET /projects/meta/requirement-statuses — 列出需求状态选项

权限：`project_requirements:view`
响应 200：data = RequirementStatusOptions
错误：403

### GET /projects/meta/user-options — 列出可选用户（添加成员等）

权限：`project_projects:update`
查询参数：keyword: string
响应 200：data = UserOptions
错误：403

### GET /projects/{id} — 获取项目

权限：`project_projects:view`
路径参数：id*: integer
响应 200：data = ProductProjectView
错误：404

### PUT /projects/{id} — 更新项目

权限：`project_projects:update`
路径参数：id*: integer
请求：{ name, slug, description, status, repository_id, clear_repository, tags }
响应 200
错误：403

### DELETE /projects/{id} — 解散项目

权限：`project_projects:delete`
路径参数：id*: integer
响应 200
错误：403

### POST /projects/{id}/archive — 归档项目

权限：`project_projects:update`
路径参数：id*: integer
响应 200：Archived
错误：403

### GET /projects/{id}/members — 列出项目成员

权限：`project_projects:view`
路径参数：id*: integer
响应 200

### POST /projects/{id}/members — 添加非 Owner 成员

权限：`project_projects:update`
路径参数：id*: integer
请求：{ user_id*, role* }
响应 201
错误：403

### PUT /projects/{id}/members/{userID} — 修改非 Owner 成员角色

权限：`project_projects:update`
路径参数：id*: integer, userID*: integer
请求：{ role* }
响应 200
错误：403

### DELETE /projects/{id}/members/{userID} — 移除非 Owner 成员

权限：`project_projects:update`
路径参数：id*: integer, userID*: integer
响应 200
错误：409

### POST /projects/{id}/members/transfer-owner — 转让项目所有者

权限：`project_projects:update`
路径参数：id*: integer
请求：{ user_id* }
响应 200
错误：403

### GET /projects/{id}/requirements — 列出需求

权限：`project_requirements:view`
路径参数：id*: integer
查询参数：page: integer, page_size: integer, keyword: string, status: string, priority: 'low' | 'normal' | 'high' | 'urgent', assignee_id: integer, sort: string
响应 200

### POST /projects/{id}/requirements — 创建需求

权限：`project_requirements:create`
路径参数：id*: integer
请求：{ title*, description, status, priority, assignee_id, repository_id, tags }
响应 201
错误：403

### GET /projects/{id}/requirements/{requirementID} — 获取需求

权限：`project_requirements:view`
路径参数：id*: integer, requirementID*: integer
响应 200
错误：404

### PUT /projects/{id}/requirements/{requirementID} — 更新需求

权限：`project_requirements:update`
路径参数：id*: integer, requirementID*: integer
请求：{ title*, description, status, priority, assignee_id, repository_id, tags }
响应 200
错误：403

### DELETE /projects/{id}/requirements/{requirementID} — 删除需求

权限：`project_requirements:delete`
路径参数：id*: integer, requirementID*: integer
响应 200
错误：403

### GET /projects/{id}/requirements/{requirementID}/comments — 列出需求评论

权限：`project_requirements:view`
路径参数：id*: integer, requirementID*: integer
响应 200

### POST /projects/{id}/requirements/{requirementID}/comments — 添加需求评论

权限：`project_requirements:create`
路径参数：id*: integer, requirementID*: integer
请求：{ content* }
响应 201

### PUT /projects/{id}/requirements/{requirementID}/comments/{commentID} — 编辑需求评论

权限：`project_requirements:update`
路径参数：id*: integer, requirementID*: integer, commentID*: integer
请求：{ content* }
响应 200

### DELETE /projects/{id}/requirements/{requirementID}/comments/{commentID} — 删除需求评论

权限：`project_requirements:delete`
路径参数：id*: integer, requirementID*: integer, commentID*: integer
响应 200

### GET /projects/{id}/requirements/{requirementID}/attachments — 列出需求附件

权限：`project_requirements:view`
路径参数：id*: integer, requirementID*: integer
响应 200

### POST /projects/{id}/requirements/{requirementID}/attachments — 上传需求附件（默认限额 20MB）

权限：`project_requirements:update`
路径参数：id*: integer, requirementID*: integer
请求：multipart: { file* }
响应 201
错误：413

### DELETE /projects/{id}/requirements/{requirementID}/attachments/{attachmentID} — 删除需求附件

权限：`project_requirements:update`
路径参数：id*: integer, requirementID*: integer, attachmentID*: integer
响应 200

### GET /projects/{id}/requirements/{requirementID}/attachments/{attachmentID}/download — 下载需求附件

权限：`project_requirements:view`
路径参数：id*: integer, requirementID*: integer, attachmentID*: integer
响应 200：data = binary

## 缺陷

### GET /projects/bugs — 列出跨项目缺陷

权限：`project_bugs:view`
查询参数：page: integer, page_size: integer, keyword: string, project_id: integer, status: 'open' | 'in_progress' | 'resolved' | 'closed' | 'rejected', severity: 'low' | 'normal' | 'high' | 'critical', priority: 'low' | 'normal' | 'high' | 'urgent', assignee_id: integer, sort: string
响应 200：data = ProjectBugPage
错误：403
说明：跨项目聚合查询，按用户项目访问权限及 `project_bugs:view` 权限过滤数据。`data_scope=self` 且非超管时仅列出本人为成员或创建人的项目的缺陷；`data_scope=all`、超管或 `manage_all` 可列出全部。

### GET /projects/{id}/bugs — 列出项目缺陷

权限：`project_bugs:view`
路径参数：id*: integer
查询参数：page: integer, page_size: integer, keyword: string, status: 'open' | 'in_progress' | 'resolved' | 'closed' | 'rejected', severity: 'low' | 'normal' | 'high' | 'critical', priority: 'low' | 'normal' | 'high' | 'urgent', assignee_id: integer, sort: string
响应 200：data = ProjectBugPage
错误：403 / 404

### POST /projects/{id}/bugs — 创建缺陷

权限：`project_bugs:create`
路径参数：id*: integer
请求：ProjectBugCreateRequest
响应 201：data = ProjectBug
错误：400 / 403 / 404

### POST /projects/{id}/bugs/ai-extract — AI 智能提单解析

权限：`project_bugs:create`
路径参数：id*: integer
请求：BugAIExtractRequest
响应 200：data = BugAIExtractResponse
错误：400 / 403 / 500
说明：接收错误堆栈/日志文本，调用大模型智能解析提取结构化信息（title, description, severity, priority）。

### GET /projects/{id}/bugs/{bugID} — 获取缺陷详情

权限：`project_bugs:view`
路径参数：id*: integer, bugID*: integer
响应 200：data = ProjectBug
错误：403 / 404

### PUT /projects/{id}/bugs/{bugID} — 更新缺陷

权限：`project_bugs:update`
路径参数：id*: integer, bugID*: integer
请求：ProjectBugUpdateRequest
响应 200：data = ProjectBug
错误：400 / 403 / 404

### DELETE /projects/{id}/bugs/{bugID} — 删除缺陷

权限：`project_bugs:delete`
路径参数：id*: integer, bugID*: integer
响应 200：data = { id: integer }
错误：403 / 404

### PUT /projects/{id}/bugs/{bugID}/status — 流转缺陷状态

权限：`project_bugs:update`
路径参数：id*: integer, bugID*: integer
请求：BugStatusTransitionRequest
响应 200：data = ProjectBug
错误：400 / 403 / 404
说明：仅允许在限定的五种合法状态（`open`、`in_progress`、`resolved`、`closed`、`rejected`）间流转，流转成功自动记录流转活动。

### GET /projects/{id}/bugs/{bugID}/activities — 列出缺陷活动记录

权限：`project_bugs:view`
路径参数：id*: integer, bugID*: integer
响应 200：data = ProjectBugActivity[]
错误：403 / 404

### POST /projects/{id}/bugs/{bugID}/ai-analyze — AI 根因分析

权限：`project_bugs:execute`
路径参数：id*: integer, bugID*: integer
请求：BugAIAnalyzeRequest
响应 200：data = BugAIAnalyzeResponse
错误：400 / 403 / 500
说明：结合缺陷上下文（标题、描述、代码仓、分支等）调用大模型生成根因推断与修复建议，并持久化回填至缺陷 `ai_analysis` 字段。

### POST /projects/{id}/bugs/{bugID}/dispatch-agent — 派发 Agent 自动化排查

权限：`project_bugs:execute`
路径参数：id*: integer, bugID*: integer
请求：BugDispatchAgentRequest
响应 202：data = BugDispatchAgentResponse
错误：400 / 403 / 500
说明：指定绑定了对应代码仓的 Bedrock Agent 异步执行代码排查，触发生成 `AgentRun`（记录 `project_id` 与 `bug_id` 关联），并将 `run_id` 记录至缺陷，缺陷详情可实时查看排查状态与执行日志。

### GET /projects/{id}/bugs/{bugID}/comments — 列出缺陷评论

权限：`project_bugs:view`
路径参数：id*: integer, bugID*: integer
响应 200：data = ProjectBugComment[]
错误：403 / 404

### POST /projects/{id}/bugs/{bugID}/comments — 添加缺陷评论

权限：`project_bugs:create`
路径参数：id*: integer, bugID*: integer
请求：ProjectBugCommentRequest
响应 201：data = ProjectBugComment
错误：400 / 403 / 404

### PUT /projects/{id}/bugs/{bugID}/comments/{commentID} — 编辑缺陷评论

权限：`project_bugs:update`
路径参数：id*: integer, bugID*: integer, commentID*: integer
请求：ProjectBugCommentRequest
响应 200：data = ProjectBugComment
错误：400 / 403 / 404

### DELETE /projects/{id}/bugs/{bugID}/comments/{commentID} — 删除缺陷评论

权限：`project_bugs:delete`
路径参数：id*: integer, bugID*: integer, commentID*: integer
响应 200
错误：403 / 404

### GET /projects/{id}/bugs/{bugID}/attachments — 列出缺陷附件

权限：`project_bugs:view`
路径参数：id*: integer, bugID*: integer
响应 200：data = ProjectBugAttachment[]
错误：403 / 404

### POST /projects/{id}/bugs/{bugID}/attachments — 上传缺陷附件（默认限额 20MB）

权限：`project_bugs:update`
路径参数：id*: integer, bugID*: integer
请求：multipart: { file* }
响应 201：data = ProjectBugAttachment
错误：400 / 403 / 404 / 413

### DELETE /projects/{id}/bugs/{bugID}/attachments/{attachmentID} — 删除缺陷附件

权限：`project_bugs:update`
路径参数：id*: integer, bugID*: integer, attachmentID*: integer
响应 200
错误：403 / 404

### GET /projects/{id}/bugs/{bugID}/attachments/{attachmentID}/download — 下载缺陷附件

权限：`project_bugs:view`
路径参数：id*: integer, bugID*: integer, attachmentID*: integer
响应 200：data = binary
错误：403 / 404

### GET /projects/{id}/docs — 获取项目文档树

权限：`project_docs:view`
路径参数：id*: integer
响应 200：文档树（节点**不含** `content`；正文用 `GET .../docs/{nodeID}` / pull / export）
说明：Markdown 渲染前须消毒

### POST /projects/{id}/docs — 创建目录或文档节点

权限：`project_docs:create`
路径参数：id*: integer
请求：{ parent_id, kind*, name*, sort_order, repository_id, content }
响应 201

### POST /projects/{id}/docs/upload — 上传单个 Markdown 文档

权限：`project_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201
错误：413

### POST /projects/{id}/docs/import-zip — 导入 Markdown zip

权限：`project_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201：Imported
错误：400 / 413
说明：ZIP 条目有 Zip Slip、条目数、体积与压缩比防护。默认包限额 100MB。

### POST /projects/{id}/docs/push — 按路径推送文档（外部 API）

鉴权：JWT 需 `project_docs:create` + 项目 ACL；或 PAT scope `docs:write` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
请求：{ api_dir, api_doc_name*, api_doc* }
响应 201：新建文档节点；200：更新已有文档
错误：400 / 403 / 404
说明：按 `api_dir` + `api_doc_name` upsert `content`。`api_dir` 为空表示根；`/` 分隔；拒绝 `..`、绝对路径、空段。目录不存在则创建。`api_doc_name` 无 `.md` 后缀时服务端补齐。

### GET /projects/{id}/docs/pull — 按路径读取文档（外部 API）

鉴权：JWT 需 `project_docs:view` + 项目 ACL；或 PAT scope `docs:read` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
查询参数：api_dir, api_doc_name*
响应 200：ApiDocNode（含 `content`）
错误：400 / 403 / 404
说明：路径规则同 push。单篇读取用 pull；全量同步用 export。

### GET /projects/{id}/docs/export — 按目录导出文档列表（外部 API）

鉴权：JWT 需 `project_docs:view` + 项目 ACL；或 PAT scope `docs:read` + 项目 ACL（同 pull）
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
查询参数：api_dir
响应 200：`{ items: [{ path, content }] }`
错误：400 / 403 / 404
说明：一次返回扁平文档列表，供 sync 全量对齐。`api_dir` 为空表示项目根；规则同 push/pull（拒绝 `..`、绝对路径、空段）。`path` 相对导出根（有 `api_dir` 则相对该子树），`/` 分隔，含 `.md` 文件名；仅 `kind=doc`，无目录行。合法但目录不存在时返回 `items: []`。按 `path` 字典序稳定排序。

### POST /projects/{id}/docs/generate — 通过 AI 生成文档（异步）

权限：`project_docs:execute`
路径参数：id*: integer
请求：{ agent_id*, node_id }
响应 202：data = object
错误：400 / 501
说明：创建异步 AgentRun。成功时写入 `content`（以及可选 `draft_source_run_id`）。AI CLI 与 Bedrock 同 UID，无沙箱。

### GET /projects/{id}/docs/{nodeID} — 获取文档节点

权限：`project_docs:view`
路径参数：id*: integer, nodeID*: integer
响应 200：ApiDocNode（含 `content`）
错误：404

### PUT /projects/{id}/docs/{nodeID} — 重命名节点或写入文档内容

权限：`project_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ name, repository_id, content }
响应 200

### DELETE /projects/{id}/docs/{nodeID} — 删除文档节点及其子节点

权限：`project_docs:delete`
路径参数：id*: integer, nodeID*: integer
响应 200

### POST /projects/{id}/docs/{nodeID}/move — 移动文档节点

权限：`project_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ parent_id, sort_order }
响应 200

### GET /projects/{id}/dev-docs — 获取项目开发文档树

权限：`project_dev_docs:view`
路径参数：id*: integer
响应 200：文档树（节点**不含** `content`；正文用 `GET .../dev-docs/{nodeID}` / pull / export）
说明：Markdown 渲染前须消毒

### POST /projects/{id}/dev-docs — 创建目录或开发文档节点

权限：`project_dev_docs:create`
路径参数：id*: integer
请求：{ parent_id, kind*, name*, sort_order, repository_id, content }
响应 201

### POST /projects/{id}/dev-docs/upload — 上传单个 Markdown 开发文档

权限：`project_dev_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201
错误：413

### POST /projects/{id}/dev-docs/import-zip — 导入 Markdown zip（开发文档）

权限：`project_dev_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201：Imported
错误：400 / 413
说明：ZIP 防护同接口文档导入。

### POST /projects/{id}/dev-docs/push — 按路径推送开发文档（外部 API）

鉴权：JWT 需 `project_dev_docs:create` + 项目 ACL；或 PAT scope `dev_docs:write` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
请求：{ doc_dir, doc_name*, content* }
响应 201：新建文档节点；200：更新已有文档
错误：400 / 403 / 404
说明：按 `doc_dir` + `doc_name` upsert `content`。`doc_dir` 为空表示根；`/` 分隔；拒绝 `..`、绝对路径、空段。目录不存在则创建。`doc_name` 无 `.md` 后缀时服务端补齐。无 AI generate。

### GET /projects/{id}/dev-docs/pull — 按路径读取开发文档（外部 API）

鉴权：JWT 需 `project_dev_docs:view` + 项目 ACL；或 PAT scope `dev_docs:read` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
查询参数：doc_dir, doc_name*
响应 200：DevDocNode（含 `content`）
错误：400 / 403 / 404
说明：路径规则同 push。单篇读取用 pull；全量同步用 export。

### GET /projects/{id}/dev-docs/export — 按目录导出开发文档列表（外部 API）

鉴权：JWT 需 `project_dev_docs:view` + 项目 ACL；或 PAT scope `dev_docs:read` + 项目 ACL（同 pull）
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
查询参数：doc_dir
响应 200：`{ items: [{ path, content }] }`
错误：400 / 403 / 404
说明：语义同接口文档 export；字段为 `doc_dir`。

### GET /projects/{id}/dev-docs/{nodeID} — 获取开发文档节点

权限：`project_dev_docs:view`
路径参数：id*: integer, nodeID*: integer
响应 200：DevDocNode（含 `content`）
错误：404

### PUT /projects/{id}/dev-docs/{nodeID} — 重命名节点或写入开发文档内容

权限：`project_dev_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ name, repository_id, content }
响应 200

### DELETE /projects/{id}/dev-docs/{nodeID} — 删除开发文档节点及其子节点

权限：`project_dev_docs:delete`
路径参数：id*: integer, nodeID*: integer
响应 200

### POST /projects/{id}/dev-docs/{nodeID}/move — 移动开发文档节点

权限：`project_dev_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ parent_id, sort_order }
响应 200

## 对象形状

### ApiDocNodeMoveRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `parent_id` | `integer` |  |  |
| `sort_order` | `integer` |  |  |

### ApiDocNodeRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `parent_id` | `integer` |  |  |
| `kind` | `'dir' \| 'doc'` | 是 |  |
| `name` | `string` | 是 |  |
| `sort_order` | `integer` |  |  |
| `repository_id` | `integer` |  |  |
| `content` | `string` |  |  |

### ApiDocNodeUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `repository_id` | `integer` |  |  |
| `content` | `string` |  |  |

### Error

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | `integer` | 是 |  |
| `message` | `string` | 是 |  |
| `request_id` | `string` |  |  |

### ProductProject

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `slug` | `string` |  |  |
| `description` | `string` |  |  |
| `status` | `'active' \| 'archived'` |  |  |
| `owner_id` | `integer` |  |  |
| `repository_id` | `integer` |  |  |
| `tags` | `string` |  |  |
| `is_public` | `boolean` |  | 保留兼容；**不再影响**读可见性；默认 false |
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### ProductProjectCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `slug` | `string` | 是 |  |
| `description` | `string` |  |  |
| `repository_id` | `integer` |  |  |
| `tags` | `string` |  |  |
| `is_public` | `boolean` |  | 保留兼容；不再影响读可见性 |

### ProductProjectPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `ProductProjectView[]` |  |  |

### ProductProjectUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `slug` | `string` |  |  |
| `description` | `string` |  |  |
| `status` | `'active' \| 'archived'` |  |  |
| `repository_id` | `integer` |  |  |
| `clear_repository` | `boolean` |  |  |
| `tags` | `string` |  |  |
| `is_public` | `boolean` |  | 保留兼容；不再影响读可见性 |

### ProductProjectView

组合：`ProductProject` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `slug` | `string` |  |  |
| `description` | `string` |  |  |
| `status` | `'active' \| 'archived'` |  |  |
| `owner_id` | `integer` |  |  |
| `repository_id` | `integer` |  |  |
| `tags` | `string` |  |  |
| `is_public` | `boolean` |  | 保留兼容；不再影响读可见性 |
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |
| `my_role` | `'owner' \| 'admin' \| 'member' \| 'readonly'` |  | 非成员可为空（omit） |
| `permissions` | `ProjectCapabilities` | 是 | 当前调用方可执行的项目级写能力；非成员通常全 false |

### ProjectCapabilities

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `update` | `boolean` | 是 |  |
| `archive` | `boolean` | 是 |  |
| `delete` | `boolean` | 是 |  |
| `manage_members` | `boolean` | 是 |  |
| `transfer_owner` | `boolean` | 是 |  |

### ProjectMember

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `project_id` | `integer` |  |  |
| `user_id` | `integer` |  |  |
| `role` | `'owner' \| 'admin' \| 'member' \| 'readonly'` |  |  |
| `username` | `string` |  | 响应附加，不落库 |
| `display_name` | `string` |  | 响应附加，不落库 |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### ProjectMemberRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `user_id` | `integer` | 是 |  |
| `role` | `'admin' \| 'member' \| 'readonly'` | 是 |  |

### UserOption

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 |  |
| `username` | `string` | 是 |  |
| `display_name` | `string` |  |  |

### UserOptions

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `UserOption[]` | 是 |  |

### ProjectMemberRoleRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `role` | `'admin' \| 'member' \| 'readonly'` | 是 |  |

### RequirementCommentRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `content` | `string` | 是 |  |

### RequirementRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` | 是 |  |
| `description` | `string` |  |  |
| `status` | `string` |  | Enabled value from the requirement_status dictionary. |
| `priority` | `'low' \| 'normal' \| 'high' \| 'urgent'` |  |  |
| `assignee_id` | `integer` |  |  |
| `repository_id` | `integer` |  |  |
| `tags` | `string` |  |  |

### RequirementStatusOption

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `label` | `string` | 是 |  |
| `value` | `string` | 是 |  |
| `sort_order` | `integer` | 是 |  |
| `enabled` | `boolean` | 是 |  |

### RequirementStatusOptions

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `RequirementStatusOption[]` | 是 |  |

### BugAIExtractRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `content` | `string` | 是 | 错误堆栈、控制台日志或缺陷描述文本 |

### BugAIExtractResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` | 是 | AI 提炼的缺陷标题 |
| `description` | `string` |  | AI 整理的问题描述/复现步骤 |
| `severity` | `'low' \| 'normal' \| 'high' \| 'critical'` |  | AI 建议的严重程度 |
| `priority` | `'low' \| 'normal' \| 'high' \| 'urgent'` |  | AI 建议的优先级 |

### BugAIAnalyzeRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `prompt` | `string` |  | 可选的用户补充分析提示 |

### BugAIAnalyzeResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `ai_analysis` | `string` | 是 | AI 分析得出的根因推断与修复建议 |

### BugDispatchAgentRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `agent_id` | `integer` | 是 | 指定执行排查任务的 Bedrock Agent ID |
| `user_prompt` | `string` |  | 用户附加的排查指示或提示词 |

### BugDispatchAgentResponse

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `agent_run_id` | `integer` | 是 | 触发生成的 AgentRun 任务 ID |

### BugStatusTransitionRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `status` | `'open' \| 'in_progress' \| 'resolved' \| 'closed' \| 'rejected'` | 是 | 目标状态 |
| `comment` | `string` |  | 流转备注或说明 |

### ProjectBug

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 |  |
| `project_id` | `integer` | 是 |  |
| `title` | `string` | 是 |  |
| `description` | `string` |  |  |
| `status` | `'open' \| 'in_progress' \| 'resolved' \| 'closed' \| 'rejected'` | 是 |  |
| `severity` | `'low' \| 'normal' \| 'high' \| 'critical'` | 是 |  |
| `priority` | `'low' \| 'normal' \| 'high' \| 'urgent'` | 是 |  |
| `assignee_id` | `integer` |  | 经办人用户 ID |
| `repository_id` | `integer` |  | 关联代码仓库 ID |
| `branch` | `string` |  | 关联分支名 |
| `last_agent_run_id` | `integer` |  | 最近一次排查关联的 AgentRun ID |
| `ai_analysis` | `string` |  | AI 根因分析内容 |
| `created_by` | `integer` | 是 |  |
| `updated_by` | `integer` | 是 |  |
| `created_at` | `string(date-time)` | 是 |  |
| `updated_at` | `string(date-time)` | 是 |  |
| `project_name` | `string` |  | 跨项目列表附加，不落库 |
| `assignee_name` | `string` |  | 经办人姓名，响应附加，不落库 |
| `assignee_username` | `string` |  | 经办人用户名，响应附加，不落库 |
| `creator_name` | `string` |  | 创建人姓名，响应附加，不落库 |
| `creator_username` | `string` |  | 创建人用户名，响应附加，不落库 |
| `repository_name` | `string` |  | 代码仓库名称，响应附加，不落库 |

### ProjectBugActivity

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 |  |
| `bug_id` | `integer` | 是 |  |
| `action` | `string` | 是 | 活动动作（status_change, create, comment, agent_dispatch） |
| `from_status` | `string` |  | 变更前状态 |
| `to_status` | `string` |  | 变更后状态 |
| `comment` | `string` |  | 备注信息 |
| `created_by` | `integer` | 是 | 操作人 ID |
| `creator_name` | `string` |  | 操作人姓名，响应附加，不落库 |
| `creator_username` | `string` |  | 操作人用户名，响应附加，不落库 |
| `created_at` | `string(date-time)` | 是 |  |

### ProjectBugAttachment

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 |  |
| `bug_id` | `integer` | 是 |  |
| `storage_object_id` | `integer` | 是 |  |
| `filename` | `string` | 是 |  |
| `file_size` | `integer` |  | 附件体积（字节），响应附加，不落库 |
| `content_type` | `string` |  | MIME 类型，响应附加，不落库 |
| `created_by` | `integer` | 是 |  |
| `creator_name` | `string` |  | 上传人姓名，响应附加，不落库 |
| `creator_username` | `string` |  | 上传人用户名，响应附加，不落库 |
| `created_at` | `string(date-time)` | 是 |  |

### ProjectBugComment

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` | 是 |  |
| `bug_id` | `integer` | 是 |  |
| `content` | `string` | 是 | 评论正文 |
| `created_by` | `integer` | 是 |  |
| `creator_name` | `string` |  | 评论人姓名，响应附加，不落库 |
| `creator_username` | `string` |  | 评论人用户名，响应附加，不落库 |
| `created_at` | `string(date-time)` | 是 |  |
| `updated_at` | `string(date-time)` | 是 |  |

### ProjectBugCommentRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `content` | `string` | 是 | 评论正文 |

### ProjectBugCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` | 是 | 缺陷标题 |
| `description` | `string` |  | 缺陷详细描述或复现步骤 |
| `severity` | `'low' \| 'normal' \| 'high' \| 'critical'` |  | 严重程度，默认 normal |
| `priority` | `'low' \| 'normal' \| 'high' \| 'urgent'` |  | 优先级，默认 normal |
| `assignee_id` | `integer` |  | 经办人用户 ID |
| `repository_id` | `integer` |  | 关联代码仓库 ID |
| `branch` | `string` |  | 关联分支名 |

### ProjectBugPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `ProjectBug[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |

### ProjectBugUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | `string` |  | 缺陷标题 |
| `description` | `string` |  | 缺陷详细描述或复现步骤 |
| `severity` | `'low' \| 'normal' \| 'high' \| 'critical'` |  | 严重程度 |
| `priority` | `'low' \| 'normal' \| 'high' \| 'urgent'` |  | 优先级 |
| `assignee_id` | `integer` |  | 经办人用户 ID（传 0 可清空） |
| `repository_id` | `integer` |  | 关联代码仓库 ID（传 0 可清空） |
| `branch` | `string` |  | 关联分支名 |

