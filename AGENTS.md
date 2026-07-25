# AGENTS.md

## 架构

### 代码架构

前后端代码不分离, 但开发上遵循前后端分离

## 技术栈

### 前端技术栈

| 项     | 技术                                       |
| ------ | ------------------------------------------ |
| 框架   | Vue 3.5+ / TypeScript / Pinia / Vue Router |
| 构建   | vite-plus                                  |
| UI     | `@veltra/*`                                |
| 工具库 | `@cat-kit/*`                               |
| 包管理 | bun                                        |

### 后端技术栈

| 项     | 技术                                      | 说明                            |
| ------ | ----------------------------------------- | ------------------------------- |
| 语言   | Go 1.26+                                  | 单体 Server + 独立 Deploy Agent |
| Web    | Gin                                       | `/api/v1`、`/ws`                |
| ORM    | GORM                                      | sqlite / postgres / mysql       |
| Schema | 版本化 Go migration + `schema_migrations` | **禁止**仅靠 AutoMigrate        |
| 认证   | JWT + PAT                                 | Bearer                          |
| 日志   | zap                                       | 请求 `request_id`               |
| 配置   | Viper                                     | `BEDROCK_` 前缀                 |

## 常用命令

启动开发服务前先检查是否已在运行。完整目标见 `Makefile`。

```bash
make dev              # 后端 :8080 + 前端 Vite（FRONTEND_DIR 默认 web）
make build            # web → cmd/server/dist → Go 二进制
go test ./...
cd web && vp check    # format + lint + typecheck
```

## 目录结构

只列目录与职责，不枚举文档文件。**目录变更时请同步更新本树。**

```text
.
├── cmd/                        # 进程入口
│   ├── server/                 # HTTP Server：DI 组装、embed web 构建产物
│   └── agent/                  # Deploy Agent（远端部署执行）
├── internal/                   # 后端业务与基础设施（Go）
│   ├── platform/               # 配置、DB、migration、健康检查
│   ├── middleware/             # Gin 中间件（CORS 等）
│   ├── auth/                   # 登录、JWT / PAT、当前用户
│   ├── rbac/                   # 角色与权限判定
│   ├── system/                 # 用户、角色、字典、操作日志、权限资源（菜单/功能）
│   ├── resource/               # 仓库、服务器、凭证、访问令牌等资源
│   ├── cicd/                   # 构建任务 / 构建运行 API
│   ├── engine/                 # 流水线执行引擎（调度、构建、分发）
│   ├── deployer/               # 部署传输（SSH / rsync / SFTP 等）
│   ├── ops/                    # 运维：进程、开发环境等
│   ├── project/                # 项目、需求、文档
│   ├── ai/                     # AI Agent / Skill / Run
│   ├── dashboard/              # 仪表盘聚合数据
│   ├── storage/                # 制品与文件存储
│   ├── ws/                     # WebSocket
│   └── pkg/                    # 跨域公共库（加密、分页、响应信封等）
├── api/                        # HTTP 契约（Markdown，按域拆分）
├── web/                        # Vue 3 前端（约定见 .agents/fe.md）
│   └── src/
│       ├── api/                # 按域封装的 HTTP 客户端
│       ├── assets/             # 静态资源
│       ├── components/         # 跨页面可复用组件
│       ├── composables/        # 组合式函数（权限、面包屑等）
│       ├── content/            # 内置手册等 Markdown 内容
│       ├── lib/                # 纯工具与第三方薄封装
│       ├── pages/              # 壳层页面：登录、布局、首页
│       ├── router/             # 路由定义
│       ├── stores/             # Pinia 状态
│       ├── theme/              # 主题 token
│       └── views/              # 业务页面（按域）
│           ├── system/         # 用户、角色、权限资源、字典、操作日志
│           ├── resource/       # 仓库、服务器、凭证、令牌
│           ├── cicd/           # 构建任务 / 运行
│           ├── ops/            # 进程、开发环境
│           ├── projects/       # 项目、需求、文档
│           ├── ai/             # Agent / Skill / Run
│           └── help/           # 帮助手册
├── scripts/                    # 工程脚本（含 smoke 冒烟）
├── docs/                       # 产品文档
├── config.yaml                 # 本地配置（示例见 config.example.yaml）
├── Makefile                    # 构建与开发入口
```

## 开发约定

- 接口开发和接口对接遵循 [API-SPEC](API-SPEC.md) 规范

## 后端开发指南

### 偏好

- 尽可能地使用当前语言版本的最新特性

### 分层

`handler → service → repository → model`，单向依赖，禁止跨层与逆向依赖。

- **model**：纯数据结构 + GORM tag
- **repository**：仅 CRUD；方法名以 `Find` / `Create` / `Update` / `Delete` / `List` / `Count` 开头
- **service**：业务编排；构造函数 `New*Service(...)`
- **handler**：校验与响应；`RegisterRoutes(rg *gin.RouterGroup)` 按域注册
- **DI**：在 `cmd/server` 手动组装，不引入 DI 框架

### 数据库

- 驱动：`sqlite`（默认）、`postgres`/`postgresql`、`mysql`
- 启动：连通性失败则**拒绝启动**；执行未应用 migration
- **改 driver ≠ 搬迁数据**；只支持全新安装——产品决策见 DESIGN / ROADMAP
- 敏感字段 AES-GCM；用户密码 bcrypt
- Schema 变更必须走版本化 migration，不得用 AutoMigrate 替代

### 代码风格

- Go：`gofmt`；测试 `*_test.go` 同包

### 测试门禁

- 涉及 schema：三驱动合同测试（`go test ./internal/platform/db/... -tags=contract`）或明确标注驱动差异
- 涉及 API：对照 [`api/`](./api/README.md) 契约文档
- 涉及状态机 / 权限 / Webhook / 存储路径：必须有单测或集成测
- GA 冒烟：`make smoke` / `scripts/smoke/*`
- 不做容量/延迟 SLO 验收（见 ROADMAP）

### 安全表述（工程卫生）

注释与用户可见文案不得夸大隔离或传输安全；具体安全边界与用户说明措辞见 [docs/DESIGN.md](./docs/DESIGN.md)。

### 禁止事项（后端工程）

1. 跨层调用，或在 handler 中直接操作 DB。
2. 用 GORM AutoMigrate **替代**版本化 migration。
