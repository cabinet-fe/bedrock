# 开发规范

## 命名

- 后端分层：repository 方法以 `Find` / `Create` / `Update` / `Delete` / `List` / `Count` 开头；service 构造函数 `New*Service(...)`；handler 按域 `RegisterRoutes(rg *gin.RouterGroup)` 注册
- 前端：views 按域组织（`views/<域>/<功能>`）；components 按功能 kebab-case 目录（如 `project-select`、`repo-select`）；组合式函数 `use-*`（如 `use-permission`、`use-breadcrumb`）
- 测试文件 `*_test.go` 同包

## 目录与代码结构

- 后端分层 `handler → service → repository → model`，单向依赖，禁止跨层与逆向依赖；DI 在 `cmd/server` 手动组装，不引入 DI 框架
- 前端：`api/` 按域封装 HTTP 客户端；`stores/` 集中 Pinia 状态；`composables/` 放组合式函数；跨页面复用组件进 `components/`
- 目录总览见 `.agents/docs/CODE-MAP.md`

## 代码风格

- Go：`gofmt`
- 前端：`cd web && vp check`（format + lint + typecheck），以 vite-plus 配置为准
- 代码注释用英文；用户可见文案与文档（PRD / API 契约 / DESIGN）用中文
- 注释与用户可见文案不得夸大隔离或传输安全；安全边界的具体表述见 `.agents/docs/DESIGN.md`

## 测试

- 后端 `go test ./...`，单测 `*_test.go` 同包
- 涉及 schema：三驱动合同测试（`go test ./internal/platform/db/... -tags=contract`）或明确标注驱动差异
- 涉及 API：对照 `api/` 契约文档
- 涉及状态机 / 权限 / Webhook / 存储路径：必须有单测或集成测
- GA 冒烟：`make smoke`（fresh-install + api-e2e + recovery + 三库 + linux 包）
- 不做容量 / 延迟 SLO 验收（2.0 仅功能 Gate，见 DESIGN.md）

## 接口

- 接口开发与对接遵循 `API-SPEC.md` 规范
- HTTP 契约按域写在 `api/<域>.md`；改契约先改对应域文档，再改后端 handler/service 与前端调用；新增或变更的请求/响应字段必须先写进契约文档，前后端不要各自加一套没记录的结构
- **契约先行（挂钩强制）**：写接口前先写 `api/<域>.md`。`node .agents/scripts/api-sync.mjs` 检查变更：handler 变更必须同步对应契约、响应信封变更必须同步 `API-SPEC.md`、新增路由必须已出现在契约中；pre-commit 钩子执行该检查（`make install-hooks` 安装，`make check-api-contracts` 手动跑含未暂存变更）
- 响应信封 `{ code, message, data?, request_id? }`（`internal/pkg/response.go`）；分页与错误码约定见 `api/` 各域文档
- 通用约定（信封/分页/认证）原位于 `.agents/api.md`，该文件当前缺失（见 ARCHITECTURE 未决），待另行修复

## 数据与存储

- 驱动：`sqlite`（默认）、`postgres` / `postgresql`、`mysql`；启动时连通性失败则拒绝启动，执行未应用 migration
- Schema 变更必须走版本化 migration + `schema_migrations`，**禁止**用 GORM AutoMigrate 替代
- 改 driver ≠ 搬迁数据，只支持全新安装（产品决策，见 DESIGN.md）
- 敏感字段 AES-GCM（`encryption.key` 64 hex）；用户密码 bcrypt
- 制品与文件存储位于 `storage.root`（工作区 / 制品 / 日志 / 对象存储目录）

## 日志

- zap；请求带 `request_id`，错误响应透出

## 版本与发布

- 版本注入：Makefile `VERSION = git describe --tags --always --dirty`，LDFLAGS 写入 `main.version`；前端 `VITE_APP_VERSION`
- 发布检查单 `.agents/docs/release-checklist.md`；安装 / 备份 / 升级 / 回退见 `.agents/docs/ops-handbook.md`
- 发布前过 `make smoke`

## 明确禁止

- cooking `spec.md` 缺少可被 `spec-files.mjs parse` 通过的「影响文件」章节
- 跨层调用，或在 handler 中直接操作 DB
- 用 GORM AutoMigrate 替代版本化 migration
