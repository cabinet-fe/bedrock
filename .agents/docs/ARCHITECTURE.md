# 架构

## 业务架构

磐石（Bedrock）：项目开发基石平台，把「写代码 → 构建 → 部署 → 协作 → 智能化」串成闭环，面向开发团队。核心域：

- **CI/CD**：代码仓库、构建任务/执行、制品下载、Webhook/Cron；分发支持 rsync / sftp / scp / local / Deploy Agent
- **资源管理**：共享仓库、服务器、凭证、AI CLI 运行时、个人访问令牌（PAT）
- **项目管理**：产品项目、成员 ACL、需求、Markdown 接口文档树；与 CI/CD 松耦合，按需关联
- **AI**：智能体编排、异步 Agent Run、开放 Agent Skills 资产库
- **运维**：进程管理、开发环境（Go/Node/Java/Python 等）；写操作仅超级管理员
- **系统管理**：用户、多角色 RBAC、字典、操作日志；仪表盘卡片与菜单受 RBAC 约束


## 技术架构

架构形态与 PROJECT.md 一致：前后端代码不分离（同一仓库），但开发上遵循前后端分离——Vue SPA 经 Vite 代理联调，生产由 Go 二进制 embed 前端构建产物，对外只有 Server 一个进程。

- 单体 **Bedrock Server**（Go/Gin）：HTTP API（`/api/v1`）、WebSocket（`/ws`）、调度器、Cron、本机构建执行、本机 AI CLI，全部内嵌同一进程；前端静态资源 embed
- 独立 **Deploy Agent**（Go，独立二进制）：在远端目标机执行部署，与 Server 同版本发布，支持 Linux/Windows
- 数据：GORM + sqlite（默认，零外部依赖）/ postgres / mysql；版本化 migration + `schema_migrations`；启动时连通性失败拒绝启动
- 认证：JWT（access）+ 刷新 Cookie + PAT（Bearer）；RBAC 多角色权限判定
- 配置：Viper，`BEDROCK_` 前缀环境变量覆盖；`encryption.key`（64 hex，AES-GCM）敏感字段加密
- 日志：zap，请求带 `request_id`

### 技术栈

| 层 | 选型 | 备注 |
| --- | --- | --- |
| 语言 / runtime | Go 1.26+（Server/Agent）；TypeScript（前端） |  |
| 框架 | Gin；GORM；Vue 3.5+ / Pinia / Vue Router |  |
| UI | `@veltra/*`（desktop / ai / styles 等） | 工具库 `@cat-kit/*` |
| 数据 | sqlite / postgres / mysql | 版本化 migration，禁止仅 AutoMigrate |
| 构建 / 包管理 | Makefile；`go build`；bun + vite-plus（`vp`） | 生产 embed web 产物 |
| 测试 | `go test ./...`（含 `-tags=contract` 三驱动合同测试）；`vp check`；`make smoke` |  |
| 部署 | 单体二进制 + Deploy Agent；rsync / sftp / scp / local / agent 分发 | Server 生产 Linux amd64/arm64，开发 macOS |
| 认证 | JWT + PAT（Bearer）；bcrypt（密码）、AES-GCM（敏感字段） |  |
| 日志 | zap（请求 `request_id`） |  |
| 配置 | Viper（`BEDROCK_` 前缀） |  |

## 未决

- `.agents/api.md`（响应信封/分页/认证通用契约，被 `api/*.md` 引用）当前缺失，待另行修复
- 无其它
