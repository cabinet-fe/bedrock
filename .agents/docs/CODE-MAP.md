# 代码地图

## 树

```text
bedrock
├── cmd/                        # 进程入口
│   ├── server/                 # HTTP Server：DI 组装、embed web 构建产物（main.go、embed_dev/prod.go）
│   ├── agent/                  # Deploy Agent（远端部署执行）
│   └── bedctl/                 # 安装/更新器 CLI（逻辑在 internal/bedctl）
├── internal/                   # 后端业务与基础设施（Go）
│   ├── platform/               # 配置、DB、migration、seed、健康检查
│   ├── middleware/             # Gin 中间件（CORS 等）
│   ├── auth/                   # 登录、JWT / PAT、当前用户
│   ├── rbac/                   # 角色与权限判定
│   ├── system/                 # 用户、角色、字典、操作日志、权限资源（菜单/功能）
│   ├── resource/               # 仓库、服务器、凭证、访问令牌等资源
│   ├── cicd/                   # 构建任务 / 脚本任务 / 构建运行 / 流水线 API
│   ├── engine/                 # 流水线执行引擎（调度、构建、分发）
│   ├── deployer/               # 部署传输（SSH / rsync / SFTP 等）
│   ├── ops/                    # 运维：进程、开发环境等
│   ├── project/                # 项目、需求、缺陷、文档
│   ├── ai/                     # AI Agent / Skill / Run
│   ├── harness/                # 会话底座：Provider 抽象 + opencode 适配器（REST/SSE）+ serve 进程托管 + 会话服务 + 流桥
│   ├── dashboard/              # 仪表盘聚合数据
│   ├── storage/                # 制品与文件存储
│   ├── ws/                     # WebSocket
│   └── pkg/                    # 跨域公共库（加密、分页、响应信封等）
├── api/                        # HTTP 契约（Markdown，按域拆分）
├── web/                        # Vue 3 前端
│   └── src/
│       ├── api/                # 按域封装的 HTTP 客户端
│       ├── assets/             # 静态资源
│       ├── components/         # 跨页面可复用组件（含 project-select、repo-select）
│       ├── composables/        # 组合式函数（权限、面包屑等）
│       ├── content/            # 内置手册等 Markdown 内容
│       ├── lib/                # 纯工具与第三方薄封装
│       ├── pages/              # 壳层页面：登录、布局、首页
│       ├── router/             # 路由定义
│       ├── stores/             # Pinia 状态
│       ├── theme/              # 主题 token
│       └── views/              # 业务页面（按域）
│           ├── system/         # 用户、角色、权限资源、字典、操作日志、备份、邮件设置
│           ├── resource/       # 仓库、服务器、凭证、令牌
│           ├── cicd/           # 构建任务 / 脚本任务 / 运行 / 流水线
│           ├── ops/            # 进程、开发环境
│           ├── projects/       # 项目、需求、文档
│           ├── ai/             # Agent / Skill / Run / 服务商与模型
│           ├── profile/        # 个人设置（自助改邮箱、改密码）
│           └── help/           # 帮助手册
├── extension/                   # Chrome MV3 报单插件（popup 报单弹窗、options 设置页、shared、icons）
├── .agents/                     # 工程底座：docs 文档（.agents/docs/）、scripts 脚本、cooking
├── .githooks/                   # git 钩子（pre-commit：API 契约先行检查，make install-hooks 安装）
├── scripts/                    # 工程脚本（smoke 冒烟、install.sh 引导安装器）
├── config.yaml                 # 本地配置（示例见 config.example.yaml）
└── Makefile                    # 构建与开发入口
```

## 模块

| 模块 | 路径 | 职责 | 主要入口 |
| --- | --- | --- | --- |
| HTTP Server | `cmd/server` | 进程入口：DI 组装、embed web 产物、启动迁移与种子 | `main.go` |
| Deploy Agent | `cmd/agent` | 远端部署执行（独立二进制） | agent main |
| bedctl 安装器 | `cmd/bedctl` + `internal/bedctl` | 安装/更新/服务管理器（Go 二进制，随 Release 附带）：镜像记忆与下载校验、semver 比较、systemd/nohup 托管（真优雅停机 + 端口复查 + 孤儿清理）、`update --port` / `port` 改端口、doctor/self-update | `bedctl/main.go`、`internal/bedctl/install.go`、`update.go`、`service*.go` |
| platform | `internal/platform` | 配置（Viper）、DB 连接、版本化 migration、seed、健康检查 | `platform/config`、`platform/db` |
| middleware | `internal/middleware` | Gin 中间件（CORS 等） | `middleware/cors.go` |
| auth | `internal/auth` | 登录、JWT / PAT、当前用户 | `auth/handler.RegisterRoutes` |
| rbac | `internal/rbac` | 角色与权限判定 | `rbac/handler.RegisterRoutes` |
| system | `internal/system` | 用户、角色、字典、操作日志、通知、权限资源、系统备份与恢复、邮件 SMTP 配置 | `system/handler.RegisterRoutes` |
| resource | `internal/resource` | 仓库、服务器、凭证、访问令牌等资源 | resource handler |
| cicd | `internal/cicd` | 构建任务/脚本任务/构建运行/流水线/Webhook API | `cicd/handler.RegisterRoutes` |
| engine | `internal/engine` | 流水线执行引擎（调度、构建、分发） | `engine/pipeline_distribute.go` 等 |
| deployer | `internal/deployer` | 部署传输（SSH / rsync / SFTP / local / agent） | deployer 包 |
| ops | `internal/ops` | 进程管理、开发环境 | ops handler |
| project | `internal/project` | 项目、成员、统一工作项（project_issues：需求/缺陷/任务，含评论/附件/字段级活动/关注者）、看板（终态默认不入板）、迭代与燃尽、文档；旧 `/bugs`、`/requirements` 路径为兼容别名（repository 层 type 固定 facade） | `project/handler.RegisterRoutes`、`issue_handler.RegisterRoutes` |
| ai | `internal/ai` | AI Agent / Skill / Run / 服务商与模型 / 对话；Agent 运行经 harness 会话执行（run↔会话状态机 / interrupt / final_output / 模型与 agent 定义目录透传，`harness.enabled=false` 时执行类端点 503，不回退 CLI）；BYOK：启用的服务商/模型渲染为各会话目录 `opencode.json`（`HarnessConfigService`，提供商 id `bedrock-p*`，baseURL 指向本机 ChatProxy + loopback token，agent 默认推理强度写模型 options，CRUD 后刷新根锚点） | `ai/handler.RegisterRoutes` |
| harness | `internal/harness` | 会话底座：Provider 接口与统一帧模型、opencode 适配器（REST + SSE 回放续流 / 总线）、serve 进程托管（127.0.0.1 / 随机密码持久化 / 启动预检清理残留与外来占用 / 探活重启 / 快速崩溃指数退避 / degraded）、会话服务（CRUD 透传 / 懒恢复 / 归档上限 / `CreateAgentSession` 内部入口 / 用户聊天会话入口 / 目录配置 prep + 默认模型防冷启动回退）、agent 定义编译（`bedrock-*.md` 原子写 + 对账清理）、BYOK 工作区配置渲染（`opencode.json` 原子写 / 空目录清理）与技能注入（`.agents/skills/` 规范化同步）、流桥（单事件流去重 / 统一帧分发 / pending 审批提问 + TTL / auto 自动应答 / ring buffer 200 / active 探测合成 idle + 连接空闲快照）、REST `/api/v1/harness/*` + WS `/ws/harness/sessions/:id/events`（应答审计落库，`harness.enabled=false` 时 503） | `harness/process.go`、`harness/provider`、`harness/provider/oc`、`harness/service`、`harness/handler` |
| dashboard | `internal/dashboard` | 仪表盘聚合数据 | `dashboard/handler.RegisterRoutes` |
| storage | `internal/storage` | 制品与文件存储 | storage 包 |
| ws | `internal/ws` | WebSocket 通道 | ws 包 |
| pkg | `internal/pkg` | 跨域公共库：加密、分页、响应信封、排序 | `pkg/response.go`、`pkg/crypto.go` |
| API 契约 | `api/` | HTTP 契约文档（按域拆分） | `api/README.md` |
| Web 前端 | `web/` | Vue 3 前端（开发态 Vite，生产 embed） | `web/src/router/index.ts`、`web/src/api/http.ts` |
| 浏览器插件 | `extension/` | Chrome MV3 报单插件：弹窗报缺陷、截图附件、PAT 鉴权 | `extension/manifest.json` |
| 工程脚本 | `scripts/` | 冒烟测试；`install.sh` 为 bedctl 引导脚本（下载 Go 安装器后转交，保留 v1.x 兼容标记） | `scripts/smoke/*`、`scripts/install.sh` |
| 文档 | `.agents/docs/` | 产品与技术文档 | `PRD.md`、`DESIGN.md`、`ops-handbook.md`、`release-checklist.md` |

## 依赖

```mermaid
graph TD
  server[cmd/server] --> platform
  server --> middleware
  server --> auth
  server --> system
  server --> resource
  server --> cicd
  server --> ops
  server --> project
  server --> ai
  server --> dashboard
  server --> ws
  auth --> platform
  auth --> rbac
  system --> rbac
  system --> platform
  resource --> platform
  cicd --> engine
  cicd --> resource
  cicd --> platform
  engine --> deployer
  deployer --> platform
  ops --> platform
  project --> platform
  ai --> platform
  ai --> harness
  dashboard --> system
  dashboard --> cicd
  dashboard --> project
  dashboard --> ai
  web --> api
  extension --> api
```

## 关键路径

- **启动**：`cmd/server/main.go` → platform 加载配置（Viper，`BEDROCK_` 前缀）→ 连接 DB（失败拒绝启动）→ 执行未应用 migration → 首启种子超管 → DI 组装 → 注册 `/api/v1` 与 `/ws` 路由 → 提供 embed 前端静态资源
- **请求**：HTTP → middleware（CORS 等）→ 鉴权（auth：JWT / PAT）→ 权限判定（rbac）→ handler 校验 → service 编排 → repository CRUD → model（GORM）
- **构建运行**：触发（手动 / Webhook / Cron）→ cicd API → engine 调度 → 本机构建执行 → deployer 分发（rsync / sftp / scp / local / Deploy Agent）→ 制品入库（storage）
- **前端联调**：开发态 Vite 代理到后端 API；生产构建产物由 `make build` 复制进 `cmd/server/dist` 并 embed
