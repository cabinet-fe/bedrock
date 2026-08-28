# T6 契约与产品文档更新

## 目标

使 `api/` 契约文档、`.agents/docs/` 产品文档与实际实现一致，正式记录"项目中心化"的定位调整。

## 现状依据

- 接口开发遵循 [API-SPEC](../API-SPEC.md)；契约按域拆分在 `api/`（AGENTS.md 测试门禁要求"涉及 API：对照 api/ 契约文档"）
- `.agents/docs/PRD.md` 现行定位"项目管理与 CI/CD 解耦"（约 L26、L32、L52、L716 附近）——与本需求矛盾，必须修订
- `.agents/docs/DESIGN.md` D4/D5 与 §4.4（项目 ACL 与角色数据权限）需补充新读规则
- `ROADMAP.md` / `known-issues.md` 已随 docs/ 清理归档删除（见 git 历史），本任务不再涉及

## 改动清单

### 1. API 契约

- `api/cicd.md`、`api/cicd-scripts.md`：
  - BuildJob / ScriptJob / BuildPipeline 对象形状与 Create/Update 请求增加 `project_id`
  - 各列表端点参数表增加 `project_id`；说明 D3 读规则（携带 project_id 时不受 created_by/is_public 数据范围限制；写操作规则不变）
  - build-runs / script-runs / pipeline-runs 列表参数增加 `project_id`
- `api/ai.md`：
  - AiAgent 形状与输入增加 `project_id`；`GET /ai/agents`、`GET /ai/runs` 参数表增加 `project_id`
  - 说明 AgentRun 的 `project_id` 回填规则（Agent 归属自动带出，docs_generate 显式指定优先）
- `api/project.md`：
  - 明确"项目域读接口对全体认证用户开放（仍需 `project_projects:view`）"；`my_role` 可为空、`permissions` 能力位语义
  - `is_public` 字段标注：不再影响读可见性，保留兼容
  - T5 若加了 `project_name` 等响应字段，同步补充

### 2. 产品文档

- `.agents/docs/PRD.md`：修订项目域定位——项目从"独立协作域"升级为"研发资源聚合根"，构建任务/脚本任务/流水线/智能体可归属项目；保留"归属可选（可空）"与"写权限仍走各域 RBAC"的边界说明
- `.agents/docs/DESIGN.md`：D4/D5/§4.4 补充 D2/D3 读规则（项目域读全员放开；项目内资源列表读放宽、写不变）；安全表述不得夸大（AGENTS.md 要求）
- （ROADMAP.md / known-issues.md 已归档删除，无需更新）

### 3. AGENTS.md

- 目录结构无新增目录则无需改动；若 T3–T5 新增了需要说明的约定（如项目选择器组件位置），在对应描述处补一句

## 验证项

- [ ] 逐条核对：`api/project.md` 中的路由、参数、响应字段与 `internal/project/handler/project_handler.go` 实际注册一致
- [ ] 逐条核对：`api/cicd.md` / `cicd-scripts.md` / `ai.md` 中新增 `project_id` 字段与各 handler 实际解析一致（参数名、类型、可空性）
- [ ] PRD/DESIGN 中不再存在与"项目统一管理 CI/CD 资源"相矛盾的表述（全文搜索"解耦"等关键词确认）
- [ ] 按 API-SPEC 自检清单过一遍变更端点（错误码、分页信封、字段命名风格一致）
- [ ] 新成员 onboarding 视角：仅读 docs 即可理解"项目 = 聚合根 + 全员可见 + 写受限"的模型

## 依赖与备注

- 依赖 T1–T5 完成后定稿（避免文档与实现漂移）
- 此任务不含代码改动，但属于交付的一部分，不可省略
