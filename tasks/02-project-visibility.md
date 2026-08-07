# T2 后端：项目可见性全员放开

## 目标

所有平台登录用户均可见全部项目（列表 + 详情及项目域内所有读接口），写操作维持现有对象级 ACL 不变。对应设计决策 D2。

## 现状依据

- 对象级 ACL：`internal/project/service/acl.go` — `Require`(:72) 对象级判定、`CanListProjects`(:105) 列表可见性、`roleAllows`(:125) 成员角色能力表
- 列表 SQL 过滤：`internal/project/repository/project_repo.go:54-83` — 非 all 用户追加 `id IN (成员) OR is_public OR created_by = me`
- 读响应：`ProductProjectView` 携带 `my_role` + `permissions{update,archive,delete,manage_members,transfer_owner}` 能力位（`project_service.go:193` `projectCapabilities`）
- 路由：`internal/project/handler/project_handler.go:31-91`，除 3 个开放 API（docs push/pull/export）外均挂 `rbacmw.RequirePermission` + 对象级 ACL
- 前端已消费 `permissions` 能力位控制按钮显隐（`web/src/views/projects/projects/pages/main.vue:184`），天然兼容非成员场景

## 改动清单

### 1. 列表可见性

- `acl.go` `CanListProjects`：放开为任意认证用户可列全部项目（全局 `project_projects:view` 中间件仍是门槛）
- `project_repo.go` `ListProjects`：移除 `all=false` 时的成员/公开/自建 where 过滤（保留 keyword/status 过滤与分页；函数签名可保留，实现简化并更新注释）

### 2. 对象级读放开

- `acl.go` `Require`：read 类 action 对任意认证用户放行（项目详情、成员列表、需求列表/详情、接口文档树、开发文档树、附件下载等全部读接口）
- write 类 action（项目 update/archive/delete、成员管理、owner 转让、需求与文档的增改删）**判定逻辑不变**
- `projectCapabilities`：非成员返回 `my_role` 为空（null/空串，按现有类型自然处理）且 `permissions` 全 false——确认序列化结果对前端兼容

### 3. 内置角色权限确认

- 检查 rbac 种子/内置角色数据：普通成员角色（非管理员）是否默认拥有 `project_projects:view`
- 若缺失：通过 migration 补充 seed 或在变更说明中明确要求管理员配置（优先补 seed，保证"全员可见"开箱即用）

### 4. is_public 语义标注

- 字段保留、写入逻辑不动；读侧不再依赖
- 代码注释更新：`internal/project/model/models.go` 的 `IsPublic` 字段注释标注"不再影响读可见性，保留兼容"（契约文档表述在 T6 处理）

### 5. 测试

- 非成员/无成员身份用户：列表可见全部项目、GET 详情 200、`my_role` 为空、能力位全 false
- 非成员写操作（PUT/DELETE/归档/成员管理/需求创建）仍 403
- 成员角色（owner/admin/member/readonly）原有能力矩阵不回归

## 实施步骤

1. 改 `acl.go` 与 `project_repo.go`
2. 跑项目域现有测试，修正受影响断言
3. 补非成员读/写边界用例
4. 确认内置角色权限 seed

## 验证项

### 自动化

- [ ] `go build ./... && go test ./internal/project/...` 通过（含新增边界用例）
- [ ] `go test ./...` 无其他域回归（dashboard 等聚合项目数据的接口仍正常）

### API 验证

- [ ] 普通角色账号（有 `project_projects:view`、非任何项目成员）：`GET /api/v1/projects` 返回**全部**项目（含他人私有项目）
- [ ] 同一账号 `GET /api/v1/projects/:id`（他人创建的 is_public=false 项目）返回 200，`my_role` 为空，`permissions` 各项全 false
- [ ] 同一账号读需求列表 / 文档树 / 成员列表均 200
- [ ] 同一账号 `PUT /projects/:id`、`POST /projects/:id/archive`、`POST /projects/:id/members`、`POST /projects/:id/requirements` 均 403
- [ ] 无 `project_projects:view` 权限码的角色：上述读接口仍 403（全局权限码门槛有效）
- [ ] 项目 owner/admin/member/readonly 四类成员的原有能力矩阵逐一核对不回归（尤其 readonly 不能写、member 不能删项目）

### 回归

- [ ] 三个开放 API（docs push/pull/export，PAT scope 链路）行为不变
- [ ] 前端项目列表页/详情页在非成员视角下按钮全部隐藏（T3 之前用现有表格页验证即可）

## 依赖与备注

- 无前置任务，可与 T1 并行
- 风险点：放开读意味着项目内的需求、文档内容对全员可见——这是需求本意（"所有成员都可以看到该项目"），实施前与需求方确认一次即可
