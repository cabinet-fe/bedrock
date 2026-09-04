# 系统管理

用户、角色、RBAC 资源、菜单、字典、操作日志、通知。

通用约定（信封、分页、认证）见 [.agents/api.md](../.agents/api.md)。
业务语义与权限模型见 [DESIGN.md](../.agents/docs/DESIGN.md)。

## 用户

### GET /users — 列出用户

权限：`system_users:view`
查询参数：page: integer, page_size: integer
响应 200：data = UserPage
错误：401 / 403

### POST /users — 创建用户

权限：`system_users:create`
请求：{ username*, password*, display_name, email, is_active, role_ids }
响应 201：data = User
错误：400 / 403
说明：`role_ids` 不可包含内置 `super_admin` 角色。

### GET /users/{id} — 获取用户

权限：`system_users:view`
路径参数：id*: integer
响应 200：data = User
错误：403 / 404

### PUT /users/{id} — 更新用户

权限：`system_users:update`
路径参数：id*: integer
请求：{ display_name, email, password, is_active, role_ids }
响应 200：data = User
错误：400 / 403
说明：`role_ids` 不可包含内置 `super_admin` 角色；超管用户的内置角色绑定由服务端维持。

### DELETE /users/{id} — 删除用户

权限：`system_users:delete`
路径参数：id*: integer
响应 200
错误：400 / 403

## 角色

### GET /roles — 列出角色

权限：`system_roles:view`
查询参数：page: integer, page_size: integer
响应 200：data = RolePage
错误：403

### POST /roles — 创建角色

权限：`system_roles:create`
请求：{ name*, code*, description, data_scope, permissions }
响应 201：data = Role
错误：400 / 403
说明：`data_scope` 为 `self` | `all`，缺省 `self`。

### GET /roles/{id} — 获取角色

权限：`system_roles:view`
路径参数：id*: integer
响应 200：data = Role
错误：404

### PUT /roles/{id} — 更新角色

权限：`system_roles:update`
路径参数：id*: integer
请求：{ name, description, data_scope }
响应 200：data = Role
错误：400
说明：内置角色不可改；`data_scope` 为 `self` | `all`。

### DELETE /roles/{id} — 删除角色

权限：`system_roles:delete`
路径参数：id*: integer
响应 200
错误：400
说明：内置角色（`type=builtin`）不可删除。

### PUT /roles/{id}/permissions — 替换角色权限码

权限：`system_roles:update`
路径参数：id*: integer
请求：{ permissions* }（功能 `full_code[]`）
响应 200：data = Role
错误：400 / 403
说明：拒绝内置角色；拒绝写入不存在或 `super_admin_only` 的功能。

### GET /roles/permission-catalog — 角色绑权目录（三层）

权限：`system_roles:update`
响应 200：data = { items: PermissionCatalogGroup[] }
错误：403
说明：分组 → 菜单 → 功能；分组不参与勾选。`super_admin_only` 项由前端禁勾选，服务端绑权亦拒绝。

## 菜单分组

### GET /menu-groups — 列出菜单分组

权限：`system_resources:view`
响应 200：data = { items: MenuGroup[] }
错误：403

### POST /menu-groups — 创建菜单分组

权限：`system_resources:create`
请求：{ name*, code*, route_prefix, sort_key, enabled }
响应 201：data = MenuGroup
错误：400 / 403
说明：`code` 全局唯一且不含 `.`。

### GET /menu-groups/{id} — 获取菜单分组

权限：`system_resources:view`
路径参数：id*: integer
响应 200：data = MenuGroup
错误：404

### PUT /menu-groups/{id} — 更新菜单分组

权限：`system_resources:update`
路径参数：id*: integer
请求：{ name, code, route_prefix, sort_key, enabled }
响应 200：data = MenuGroup
错误：400

### DELETE /menu-groups/{id} — 删除菜单分组

权限：`system_resources:delete`
路径参数：id*: integer
响应 200
错误：400
说明：分组下仍有菜单时拒绝删除。

## RBAC 资源

### GET /rbac/resources — 列出 RBAC 资源树

权限：`system_resources:view`
查询参数：keyword: string, type: string(menu|action|card), enabled: boolean, group_id: integer
响应 200：data = object
错误：403
说明：返回树形 `items`（菜单 → 功能）。有筛选时匹配 code / full_code / 标题 / 路由，并保留匹配节点的祖先以维持树结构。

### POST /rbac/resources — 创建 RBAC 资源

权限：`system_resources:create`
请求：{ code*, type*, group_id, parent_id, enabled, sort_key, title, route, hidden, super_admin_only }
响应 201：data = RbacResource
错误：400 / 403
说明：菜单必须带 `group_id`（`parent_id` 为空）；功能必须挂菜单 `parent_id`。`super_admin_only` 仅超管可设。`code` 不含 `.`；功能 `full_code = {menu.code}:{code}`。

### GET /rbac/resources/{id} — 获取 RBAC 资源

权限：`system_resources:view`
路径参数：id*: integer
响应 200：data = RbacResource
错误：404

### PUT /rbac/resources/{id} — 更新 RBAC 资源

权限：`system_resources:update`
路径参数：id*: integer
请求：{ code, group_id, enabled, sort_key, title, route, hidden, super_admin_only }
响应 200：data = RbacResource
错误：400
说明：改菜单 `code` 时级联重算子功能 `full_code` 并清理失效 `role_permissions`。`super_admin_only` 仅超管可改。

### DELETE /rbac/resources/{id} — 删除 RBAC 资源

权限：`system_resources:delete`
路径参数：id*: integer
响应 200
错误：400

### PUT /rbac/resources/{id}/icon — 更新资源图标

权限：`system_resources:update`
路径参数：id*: integer
请求：{ icon_base64*, icon_mime }
响应 200：data = RbacResource
错误：400 / 403
说明：仅菜单类型资源可设置图标。需要 `system_resources:update`。

## 字典

### GET /dictionaries — 列出字典

权限：`system_dictionaries:view`
查询参数：page: integer, page_size: integer
响应 200：data = DictionaryPage
错误：403
说明：列表项预加载 `items`。

### POST /dictionaries — 创建字典

权限：`system_dictionaries:create`
请求：{ name*, code*, description, items }
响应 201：data = Dictionary
错误：400

### GET /dictionaries/code/{code} — 按编码获取字典（含字典项）

权限：登录即可
路径参数：code*: string
响应 200：data = Dictionary
错误：404

### GET /dictionaries/{id} — 获取字典

权限：`system_dictionaries:view`
路径参数：id*: integer
响应 200：data = Dictionary
错误：404

### PUT /dictionaries/{id} — 更新字典

权限：`system_dictionaries:update`
路径参数：id*: integer
请求：{ name, description, items }
响应 200：data = Dictionary
错误：400

### DELETE /dictionaries/{id} — 删除字典

权限：`system_dictionaries:delete`
路径参数：id*: integer
响应 200

## 操作日志

### GET /operation-logs — 列出操作日志

权限：`system_operation_logs:view`
查询参数：page: integer, page_size: integer, user_id: integer, action: string, resource_type: string, from: string(date), to: string(date), sort: string
响应 200：data = OperationLogPage
错误：403

### DELETE /operation-logs — 清空操作日志

权限：`system_operation_logs:clear` 或 `system_operation_logs:delete`
响应 200
错误：403



## 通知

### GET /notifications — 列出当前用户通知

查询参数：page: integer, page_size: integer, is_read: boolean（可选；传 false 仅查未读，传 true 仅查已读，缺省查全部）
响应 200：data = NotificationPage
错误：400（is_read 无法解析）、401

### PUT /notifications/read-all — 全部标为已读

响应 200
错误：401

### PUT /notifications/{id}/read — 单条标为已读

路径参数：id*: integer
响应 200
错误：401

## 对象形状

### DictItem

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `dictionary_id` | `integer` |  |  |
| `label` | `string` |  |  |
| `value` | `string` |  |  |
| `sort_order` | `integer` |  |  |
| `enabled` | `boolean` |  |  |

### Dictionary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `code` | `string` |  |  |
| `description` | `string` |  |  |
| `items` | `DictItem[]` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### DictionaryCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `code` | `string` | 是 |  |
| `description` | `string` |  |  |
| `items` | `DictItem[]` |  |  |

### DictionaryPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `Dictionary[]` |  |  |

### DictionaryUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` |  |  |
| `description` | `string` |  |  |
| `items` | `DictItem[]` |  |  |

### MenuGroup

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `code` | `string` |  | 不含 `.` |
| `route_prefix` | `string` |  | 如 `/system` |
| `sort_key` | `integer` |  |  |
| `enabled` | `boolean` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### Notification

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `user_id` | `integer` |  |  |
| `type` | `string` |  | e.g. build_run_success, build_run_failed, agent_run_success |
| `title` | `string` |  |  |
| `message` | `string` |  |  |
| `build_run_id` | `integer` |  |  |
| `agent_run_id` | `integer` |  |  |
| `is_read` | `boolean` |  |  |
| `created_at` | `string(date-time)` |  |  |

### NotificationPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `Notification[]` |  |  |

### OperationLog

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `user_id` | `integer` |  |  |
| `username` | `string` |  |  |
| `action` | `string` |  |  |
| `resource_type` | `string` |  |  |
| `resource_id` | `string` |  |  |
| `details` | `string` |  |  |
| `ip_address` | `string` |  |  |
| `created_at` | `string(date-time)` |  |  |

### OperationLogPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `OperationLog[]` |  |  |

### Page

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |

### PermissionCatalogGroup

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `code` | `string` |  |  |
| `menus` | `PermissionCatalogMenu[]` |  |  |

### PermissionCatalogMenu

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `code` | `string` |  |  |
| `full_code` | `string` |  | 等于 `code` |
| `title` | `string` |  |  |
| `super_admin_only` | `boolean` |  |  |
| `hidden` | `boolean` |  |  |
| `enabled` | `boolean` |  |  |
| `features` | `PermissionCatalogFeature[]` |  |  |

### PermissionCatalogFeature

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `code` | `string` |  |  |
| `full_code` | `string` |  | `{menu.code}:{code}`，绑权用 |
| `type` | `'action' \| 'card'` |  |  |
| `title` | `string` |  |  |
| `super_admin_only` | `boolean` |  |  |
| `enabled` | `boolean` |  |  |

### RbacResource

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `code` | `string` |  | 不含 `.` |
| `full_code` | `string` |  | 菜单=`code`；功能=`{menu.code}:{code}` |
| `type` | `'menu' \| 'action' \| 'card'` |  |  |
| `group_id` | `integer` |  | 仅菜单 |
| `parent_id` | `integer` |  | 仅功能 |
| `super_admin_only` | `boolean` |  |  |
| `hidden` | `boolean` |  | 仅菜单；true 不进用户导航 |
| `enabled` | `boolean` |  |  |
| `sort_key` | `integer` |  |  |
| `title` | `string` |  |  |
| `route` | `string` |  |  |
| `icon_base64` | `string` |  |  |
| `icon_mime` | `string` |  |  |
| `children` | `RbacResource[]` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### RbacResourceCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | `string` | 是 | 不含 `.` |
| `type` | `'menu' \| 'action' \| 'card'` | 是 |  |
| `group_id` | `integer` |  | 菜单必填 |
| `parent_id` | `integer` |  | 功能必填 |
| `enabled` | `boolean` |  |  |
| `sort_key` | `integer` |  |  |
| `title` | `string` |  |  |
| `route` | `string` |  |  |
| `hidden` | `boolean` |  |  |
| `super_admin_only` | `boolean` |  | 仅超管可设 |

### RbacResourceUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | `string` |  |  |
| `group_id` | `integer` |  |  |
| `enabled` | `boolean` |  |  |
| `sort_key` | `integer` |  |  |
| `title` | `string` |  |  |
| `route` | `string` |  |  |
| `hidden` | `boolean` |  |  |
| `super_admin_only` | `boolean` |  | 仅超管可改 |

### Role

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `name` | `string` |  |  |
| `code` | `string` |  |  |
| `description` | `string` |  |  |
| `type` | `'builtin' \| 'custom'` |  | 内置超管为 `builtin` |
| `data_scope` | `'self' \| 'all'` |  | 数据权限；多角色取最宽（任一 `all` 即 `all`） |
| `permissions` | `RolePermission[]` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### RoleCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | `string` | 是 |  |
| `code` | `string` | 是 |  |
| `description` | `string` |  |  |
| `data_scope` | `'self' \| 'all'` |  | 缺省 `self` |
| `permissions` | `string[]` |  |  |

### RolePage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `Role[]` |  |  |

### RolePermission

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `role_id` | `integer` |  |  |
| `permission` | `string` |  |  |

### User

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | `integer` |  |  |
| `username` | `string` |  |  |
| `display_name` | `string` |  |  |
| `email` | `string` |  |  |
| `avatar` | `string` |  |  |
| `is_active` | `boolean` |  |  |
| `is_super_admin` | `boolean` |  |  |
| `role_ids` | `integer[]` |  |  |
| `created_at` | `string(date-time)` |  |  |
| `updated_at` | `string(date-time)` |  |  |

### UserCreateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `username` | `string` | 是 |  |
| `password` | `string` | 是 |  |
| `display_name` | `string` |  |  |
| `email` | `string` |  |  |
| `is_active` | `boolean` |  |  |
| `role_ids` | `integer[]` |  |  |

### UserPage

组合：`Page` + `inline`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | `any[]` | 是 |  |
| `total` | `integer` | 是 |  |
| `page` | `integer` | 是 |  |
| `page_size` | `integer` | 是 |  |
| `total_pages` | `integer` | 是 |  |
| `items` | `User[]` |  |  |

### UserUpdateRequest

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `display_name` | `string` |  |  |
| `email` | `string` |  |  |
| `password` | `string` |  |  |
| `is_active` | `boolean` |  |  |
| `role_ids` | `integer[]` |  |  |
