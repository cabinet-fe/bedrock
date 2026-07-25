# 项目协作

项目、成员、需求、评论、附件、文档发布。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [docs/DESIGN.md](../docs/DESIGN.md)。

## 项目

### GET /projects — 列出项目

权限：`project_projects:view`
查询参数：page: integer, page_size: integer, keyword: string, status: 'active' | 'archived', sort: string
响应 200：data = ProductProjectPage
错误：403

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

### GET /projects/{id}/docs — 获取项目文档树

权限：`project_docs:view`
路径参数：id*: integer
响应 200：Tree with published and draft content; Markdown must be sanitized at render time

### POST /projects/{id}/docs — 创建目录或文档节点

权限：`project_docs:create`
路径参数：id*: integer
请求：{ parent_id, kind*, name*, sort_order, repository_id, draft_content }
响应 201

### POST /projects/{id}/docs/upload — 上传单个 Markdown 为草稿文档

权限：`project_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201
错误：413

### POST /projects/{id}/docs/import-zip — 导入 Markdown zip 为草稿文档

权限：`project_docs:create`
路径参数：id*: integer
请求：multipart: { parent_id, file* }
响应 201：Imported
错误：400 / 413
说明：ZIP 条目有 Zip Slip、条目数、体积与压缩比防护。默认包限额 100MB。

### POST /projects/{id}/docs/push — 按路径推送文档草稿（外部 API）

鉴权：JWT 需 `project_docs:create` + 项目 ACL；或 PAT scope `docs:write` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
请求：{ api_dir, api_doc_name*, api_doc* }
响应 201：新建文档节点；200：更新已有草稿
错误：400 / 403 / 404
说明：按 `api_dir` + `api_doc_name` upsert 草稿，不自动发布。`api_dir` 为空表示根；`/` 分隔；拒绝 `..`、绝对路径、空段。目录不存在则创建。`api_doc_name` 无 `.md` 后缀时服务端补齐。

### POST /projects/{id}/docs/publish-path — 按路径发布文档草稿（外部 API）

鉴权：JWT 需 `project_docs:update` + 项目 ACL；或 PAT scope `docs:publish` + 项目 ACL
路径参数：id*: integer | string（正整数按项目 ID；否则按 slug 解析，找不到 → 404）
请求：{ api_dir, api_doc_name* }
响应 200：Published
错误：400 / 403 / 404 / 409
说明：解析路径后用当前 `content_version` 发布；无草稿 → 400；路径不存在 → 404；版本冲突 → 409。

### POST /projects/{id}/docs/generate — 通过 AI 生成文档（异步）

权限：`project_docs:execute`
路径参数：id*: integer
请求：{ agent_id*, node_id }
响应 202：data = object
错误：400 / 501
说明：创建异步 AgentRun。成功时只写入 `draft_content`（以及草稿元数据 / 可选 `draft_source_run_id`），不会自动发布。AI CLI 与 Bedrock 同 UID，无沙箱。

### GET /projects/{id}/docs/{nodeID} — 获取文档节点

权限：`project_docs:view`
路径参数：id*: integer, nodeID*: integer
响应 200
错误：404

### PUT /projects/{id}/docs/{nodeID} — 重命名节点或写入文档草稿

权限：`project_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ name, repository_id, draft_content }
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

### POST /projects/{id}/docs/{nodeID}/publish — 发布文档草稿

权限：`project_docs:update`
路径参数：id*: integer, nodeID*: integer
请求：{ expected_version* }
响应 200：Published
错误：409

### GET /projects/{id}/docs/{nodeID}/diff — 比较草稿与已发布文档

权限：`project_docs:view`
路径参数：id*: integer, nodeID*: integer
响应 200：data = ApiDocDiff

## 对象形状

### ApiDocDiff

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `node_id` | `integer` |  |  |
| `content_version` | `integer` |  |  |
| `has_draft` | `boolean` |  |  |
| `published_lines` | `integer` |  |  |
| `draft_lines` | `integer` |  |  |
| `added_lines` | `integer` |  |  |
| `removed_lines` | `integer` |  |  |

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
| `draft_content` | `string` |  |  |

### ApiDocNodeUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `repository_id` | `integer` |  |  |
| `draft_content` | `string` |  |  |

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
| `created_by` | `integer` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |
| `my_role` | `'owner' \| 'admin' \| 'member' \| 'readonly'` |  |  |
| `permissions` | `ProjectCapabilities` | 是 |  |

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
