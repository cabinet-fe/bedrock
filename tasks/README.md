# 项目中心化改造 — 任务清单总览

## 需求背景

将「项目」升级为平台的核心聚合实体：

1. 一个项目下的**构建任务、脚本任务、流水线、智能体**统一查看与管理（典型场景：某项目含 5 个后端微服务构建 + 1 个前端构建 + 1 个文档智能体）。
2. 项目列表改为**卡片式**展示。
3. **所有平台用户都能看到项目**（列表 + 详情只读），但编辑、归档、成员管理等功能权限仍受现有 ACL 限制。

## 现状摘要（代码摸底结论）

| 领域 | 现状 | 关键位置 |
| --- | --- | --- |
| 项目 | 有成员体系（owner/admin/member/readonly）与对象级 ACL；列表为 ProTable 表格；详情页 3 个 tab（需求/接口文档/开发文档） | `internal/project/service/acl.go`、`web/src/views/projects/projects/pages/main.vue`、`detail.vue` |
| CICD | BuildJob / ScriptJob / BuildPipeline **均无 project_id**；列表靠 `created_by + is_public + data_scope` 过滤 | `internal/cicd/model/models.go:59,173,232`、`internal/cicd/service/build_job_service.go:408` |
| AI | AiAgent / SkillPackage 无 project_id；仅 AgentRun 有 `project_id`（仅 docs_generate 场景使用） | `internal/ai/model/models.go:71,120` |
| 权限 | 全局 RBAC 权限码 + 项目域对象级 ACL；cicd/ai 走角色 data_scope | `internal/rbac/`、`internal/project/service/acl.go:72` |
| PRD | 现行定位「项目管理与 CI/CD 解耦、各自一等公民」，与本需求直接冲突，需修订 | `docs/PRD.md`、`docs/DESIGN.md` D4/D5、§4.4 |

## 关键设计决策

- **D1 关联方式**：`build_jobs` / `script_jobs` / `build_pipelines` / `ai_agents` 四张表新增 **`project_id`（可空、带索引）**。可空的原因：存量数据平滑迁移、允许"不属于任何项目"的资源继续存在。Run 级表不加列，通过 job/agent 间接关联（AgentRun 复用已有 `project_id` 列）。
- **D2 项目可见性**：项目域**所有读接口对全平台登录用户放开**（全局 `project_projects:view` 权限码仍保留作为门槛）；写接口（创建/编辑/归档/删除/成员管理/需求与文档的写）维持现有成员 ACL 不变。非成员的 `my_role` 返回空、能力位 `permissions` 全 false。
- **D3 项目内资源读规则**：cicd/ai 列表与详情接口在**携带 `project_id` 参数时**，跳过 `created_by/is_public` 数据范围过滤（仍需各域 `:view` 权限码）——即"项目内资源对能看项目的人可见"。不带 `project_id` 的原全局列表行为完全不变。**写操作（编辑/触发/删除）规则一律不变**。
- **D4 聚合方式**：项目详情"概览"不做后端聚合接口，由前端并行调各域列表接口（`page_size=1` 取 total、run 列表取最近 N 条）拼装。避免 project 域反向依赖 cicd/ai。
- **D5 未关联资源**：`project_id` 为 NULL 的资源在原 cicd/ai 全局列表页照常展示与管理；全局列表页新增"按项目过滤"下拉。
- **D6 is_public 语义**：字段保留以兼容存量数据与 API，但项目读侧不再依赖它（D2 已全员可读）；在契约文档中标注语义变化，不在本期删字段。
- **D7 Skill 不关联项目**：Skill 是可跨项目复用的能力包，由 Agent 引用，不纳入项目归属。

## 任务总览

| 编号 | 任务 | 文件 | 依赖 |
| --- | --- | --- | --- |
| T1 | 后端：资源与项目关联（migration + 模型 + 列表过滤 + 读规则） | [01-resource-project-binding.md](./01-resource-project-binding.md) | 无 |
| T2 | 后端：项目可见性全员放开 | [02-project-visibility.md](./02-project-visibility.md) | 无（可与 T1 并行） |
| T3 | 前端：项目卡片式列表 | [03-project-card-list.md](./03-project-card-list.md) | T2（语义依赖，UI 可先行） |
| T4 | 前端：项目详情聚合工作台 | [04-project-workspace.md](./04-project-workspace.md) | T1 |
| T5 | 前端：各域资源的项目归属入口 | [05-resource-binding-ui.md](./05-resource-binding-ui.md) | T1 |
| T6 | 契约与产品文档更新 | [06-docs-contracts.md](./06-docs-contracts.md) | T1–T5 完成后定稿 |

推荐实施顺序：**T1、T2 并行 → T3、T4、T5 并行 → T6**。

## 全局验收清单（全部任务完成后）

- [ ] `go build ./... && go test ./...` 通过
- [ ] `go test ./internal/platform/db/... -tags=contract` 三驱动合同测试通过
- [ ] `cd web && vp check`（format + lint + typecheck）通过
- [ ] 端到端手工路径：创建项目 → 在项目内新建/关联 5 个构建任务 + 1 个流水线 + 1 个智能体 → 项目详情各面板可见且可操作 → 无管理权限的普通用户可看到该项目卡片与详情资源列表，但所有写操作按钮不可用/接口返回 403
- [ ] `make smoke` 冒烟通过
- [ ] `api/` 契约文档与实际路由、字段一致（T6 核对清单全部勾选）
