# T1 后端：资源与项目关联

## 目标

为构建任务、脚本任务、流水线、智能体建立到项目的直接关联，并使各域列表/详情接口支持按项目过滤；项目维度下的读操作按 D3 规则放宽，写操作规则不变。

## 现状依据

- `internal/cicd/model/models.go`：`BuildJob`(:59)、`ScriptJob`(:173)、`BuildPipeline`(:232) 均无 project 字段
- `internal/ai/model/models.go`：`AiAgent`(:71) 无 project 字段；`AgentRun`(:120) 已有 `ProjectID *uint`（仅 docs_generate 场景写入，见 `internal/ai/service/agent_service.go:533`）
- cicd 数据范围过滤：`internal/cicd/service/build_job_service.go:408`（List）、`:580`（requireJobRead）、`:587`（requireJobWrite）
- ai 列表：`internal/ai/handler/handler.go:215`（agents 仅分页）、`internal/ai/repository/ai_repo.go:161`（ListRuns 仅 agentID/status）
- migration 模式：`internal/platform/migration/migrations/NNNNNN_*.go`，当前最新 `000044`，下一版本 `000045`；局部 model + `HasColumn/AddColumn` 幂等写法；按 version 字典序执行
- 跨域 repository 注入有先例：`cmd/server/main.go:173` 处 cicd service 注入了 resource 域的 repoRepo

## 改动清单

### 1. Migration

- 新建 `internal/platform/migration/migrations/000045_resource_project_id.go`：
  - 为 `build_jobs`、`script_jobs`、`build_pipelines`、`ai_agents` 四张表各加 `project_id` 列（可空整数 + 普通索引）
  - 参照 `000044_build_job_tags_repo_type.go` 的写法：`init()` 注册、局部 migration 专用结构体（不引用业务 model）、`HasColumn` 幂等判断
  - 三驱动（sqlite/postgres/mysql）均可执行

### 2. 模型

- `internal/cicd/model/models.go`：`BuildJob`、`ScriptJob`、`BuildPipeline` 各加 `ProjectID *uint`（`gorm:"index"`，json `project_id`）
- `internal/ai/model/models.go`：`AiAgent` 加 `ProjectID *uint`（同上）
- Run 级模型不加列

### 3. 列表与详情接口（cicd）

- `BuildJob` / `ScriptJob` / `BuildPipeline`：
  - repository 的 List 增加 `projectID *uint` 过滤条件
  - service 的 List（如 `build_job_service.go:408`）：**当 `projectID != nil` 时跳过 `created_by/is_public` 数据范围过滤**（D3）；不带时行为不变
  - `requireJobRead`（:580）：资源已关联项目时放行读；`requireJobWrite`（:587）**不变**
  - handler 列表接受 `project_id` query 参数并做 uint 解析与校验
- Run 级列表支持 `project_id`（用于项目详情"最近运行"）：
  - `GET /build-runs`：`build_job_id IN (SELECT id FROM build_jobs WHERE project_id = ?)` 子查询
  - `GET /script-runs`、`GET /pipeline-runs`：同模式（分别经 script_jobs / build_pipelines）
  - run 读权限随列表同样按 D3 放宽；cancel/retry/redeploy 等写操作不变

### 4. 列表与详情接口（ai）

- `GET /ai/agents`：增加 `project_id` 过滤参数（agents 列表当前无数据范围过滤，仅需加条件）
- `GET /ai/runs`：`ListRuns`（`ai_repo.go:161`）增加 `projectID` 参数，直接按 `agent_runs.project_id` 列过滤
- Agent 详情读：已关联项目时按 D3 放宽；写不变

### 5. 创建/更新接受 project_id

- 四资源的 Create/Update 请求 DTO 增加 `project_id *uint`
- service 层校验：非空时项目必须存在且未删除（注入 project 域 repository，参照 main.go:173 的跨域注入先例；仅校验存在性，不校验调用者对项目的成员权限）
- `cmd/server/main.go` DI 组装相应调整

### 6. AgentRun 的 project_id 回填通用化

- `internal/ai/service/agent_service.go`：手动触发 / API 触发 / cron / build_event 创建 Run 时，若 Agent 有 `project_id` 则写入 `Run.ProjectID`（docs_generate 场景显式传入的优先级更高，不被覆盖）

### 7. 测试

- migration 合同测试（三驱动）
- 各域 List 的 project_id 过滤与 D3 放宽规则单测/集成测：
  - 带 `project_id`：非创建者、非公开资源也可见（读）
  - 不带 `project_id`：过滤行为与改造前一致（回归）
  - 写接口（更新任务、触发运行）对非授权用户仍 403

## 实施步骤

1. 写 migration `000045` 并本地 sqlite 启动验证表结构
2. 改四个 model + 各域 repository/service/handler
3. 改 DI 组装，跑通编译
4. 补测试，跑合同测试与全量测试

## 验证项

### 自动化

- [ ] `go build ./...` 通过
- [ ] `go test ./internal/platform/db/... -tags=contract` 三驱动合同测试通过（新增列与索引在三驱动下均生效）
- [ ] `go test ./internal/cicd/... ./internal/ai/... ./internal/project/...` 通过（含新增用例）
- [ ] 存量 sqlite 库（`data/bedrock.sqlite`）启动后 migration 幂等：重启二次执行不报错

### API 验证（启动本地服务后用 curl 验证）

- [ ] 创建构建任务时传 `project_id`，响应体包含该字段；传不存在的 `project_id` 返回 400/404
- [ ] `GET /api/v1/build-jobs?project_id=<id>` 仅返回该项目任务；`GET /api/v1/script-jobs?project_id=`、`/build-pipelines?project_id=`、`/ai/agents?project_id=` 同理
- [ ] `GET /api/v1/build-runs?project_id=<id>`（及 script-runs / pipeline-runs / ai/runs）正确经 job/agent 关联过滤
- [ ] 用一个对项目内某构建任务**无创建者身份且任务 is_public=false** 的普通角色账号（需有 `cicd_build_jobs:view`）：带 `project_id` 列表**可见**该任务；不带 `project_id` 的全局列表**不可见**（回归原规则）；`PUT /build-jobs/:id` 仍 403
- [ ] 手动触发一个有 project_id 的 Agent，`GET /ai/runs/:id` 返回的 `project_id` 已回填

### 回归

- [ ] 存量数据（project_id 为 NULL）在全局列表行为与改造前完全一致
- [ ] Webhook / cron 触发链路不受影响，运行记录正常生成

## 依赖与备注

- 无前置任务，可与 T2 并行
- 不在本期范围：项目成员身份不授予项目内资源的写权限（写仍走全局 RBAC + 原数据范围规则）
