# 磐石（Bedrock）

[中文](./README.md) | [English](./README.en.md)

项目开发基石平台：在同一套系统上覆盖**宿主机运维**、**CI/CD 交付**、**产品协作**（需求与接口文档）以及 **AI 智能体与开放 Agent Skills**，帮助团队把「写代码 → 构建 → 部署 → 协作 → 智能化」串成闭环。

## 功能概览

| 域 | 能力 |
| --- | --- |
| **仪表盘** | 可配置总览入口，卡片与菜单均受 RBAC 约束 |
| **CI/CD** | 代码仓库、构建任务 / 执行、制品下载、Webhook / Cron；分发支持 rsync / sftp / scp / local / Deploy Agent |
| **资源管理** | 共享仓库、服务器、凭证、AI CLI 运行时、个人访问令牌（PAT） |
| **项目管理** | 产品项目、成员 ACL、需求、Markdown 接口文档树；与 CI/CD 松耦合，按需关联 |
| **AI** | 智能体编排、异步 Agent Run、开放 [Agent Skills](https://agentskills.io/specification) 资产库 |
| **运维** | 进程管理、开发环境（Go / Node / Java / Python 等）；写操作仅超级管理员 |
| **系统管理** | 用户、多角色 RBAC、字典、操作日志 |

## 部署形态

```text
┌─────────────────────────────────────────────┐
│  Bedrock Server（单体二进制）                 │
│  - Go API / WebSocket / 调度器 / Cron         │
│  - 前端静态资源 embed                         │
│  - 本机构建执行 + 本机 AI CLI                 │
└──────────────────────┬──────────────────────┘
                       │ rsync / sftp / scp / agent / local
                       ▼
┌─────────────────────────────────────────────┐
│  目标服务器 + 可选 Deploy Agent（独立二进制） │
└─────────────────────────────────────────────┘
```

| 组件 | 支持 |
| --- | --- |
| Server 生产 | Linux amd64 / arm64 |
| Server 开发 | macOS |
| Deploy Agent | Linux / Windows（独立发布，与 Server 同版本） |
| 数据库 | `sqlite`（默认，零外部依赖）、`postgres` / `postgresql`、`mysql` |
| 前端 | 生产 embed 进 Server；开发态 Vite 代理 |

## 快速部署（发布包）

适合生产或试用：使用预构建二进制 + 默认 SQLite。

```bash
# 1. 取得发布包（示例：Linux amd64）
#    bedrock-linux-amd64、bedrock-agent-linux-amd64（及 .sha256）

# 2. 准备数据目录与配置
mkdir -p ./data
cp config.example.yaml config.yaml

# 3. 生产前务必修改（见 config.yaml）
#    - encryption.key：64 位 hex
#    - jwt.secret
#    - admin.username / admin.password（首启种子超管）

# 4. 启动（空库 → 自动 migration → 种子超管）
./bedrock-linux-amd64 --config ./config.yaml

# 5. 验证
curl -fsS http://127.0.0.1:8080/api/v1/health
# 浏览器打开 http://<host>:8080 ，使用超管账号登录
```

切换到 PostgreSQL / MySQL：改 `database.driver` 及相关连接项后重启即可，**无需重编**。改驱动**不会**搬迁数据，需自行完成迁移。

更完整的安装、备份、回退与 Deploy Agent 说明见 [.agents/docs/ops-handbook.md](./.agents/docs/ops-handbook.md)。

## 从源码构建与启动

### 环境要求

- Go **1.26+**
- [Bun](https://bun.sh/)（前端）与仓库约定的 `vp` 工作流
- 可选：PostgreSQL / MySQL（非默认）

### 开发（前后端联调）

```bash
# 准备配置（encryption.key 需与前端注入一致）
cp config.example.yaml config.yaml

# 同时启动后端 :8080（-tags dev）+ 前端 Vite 代理
make dev
```

前端开发服务由 Vite 代理到后端 API；浏览器访问 Vite 给出的本地地址即可。

### 构建单体发布物

```bash
make build                 # web → cmd/server/dist → ./bedrock
./bedrock --config ./config.yaml

# 交叉编译（生产）
make build-linux           # bedrock-linux-amd64
make build-linux-arm64     # bedrock-linux-arm64
make build-agent-linux
make build-agent-linux-arm64
make checksums
```

### 常用检查

```bash
go test ./...
cd web && bun install && vp check
make smoke                 # fresh-install + api-e2e + recovery + 三库 + linux 包
```

## 配置要点

完整示例见 [`config.example.yaml`](./config.example.yaml)。常用项：

| 配置 | 说明 |
| --- | --- |
| `server.host` / `server.port` | 默认 `0.0.0.0:8080` |
| `database.driver` | `sqlite` \| `postgres` \| `mysql` |
| `database.path` | SQLite 文件路径（默认 `./data/bedrock.sqlite`） |
| `encryption.key` | 64 hex；生产必须更换；开发需与 `VITE_BEDROCK_ENCRYPTION_KEY` 一致 |
| `admin.*` | 首次空库启动时写入的超级管理员 |
| `build.*` / `storage.root` | 工作区、制品、日志、对象存储目录 |

环境变量可用 `BEDROCK_` 前缀覆盖（Viper）。

## 文档

| 文档 | 内容 |
| --- | --- |
| [.agents/docs/PRD.md](./.agents/docs/PRD.md) | 产品需求真源 |
| [.agents/docs/DESIGN.md](./.agents/docs/DESIGN.md) | 技术设计真源 |
| [.agents/docs/ops-handbook.md](./.agents/docs/ops-handbook.md) | 安装、备份、升级与回退 |
| [.agents/docs/release-checklist.md](./.agents/docs/release-checklist.md) | 发布检查单 |
| [api/README.md](./api/README.md) | HTTP API 契约（按域拆分） |
| [AGENTS.md](./AGENTS.md) | 仓库协作与常用命令 |

## 安全边界（请务必阅读）

1. **同 UID 执行**：构建脚本、AI CLI、自定义超管命令与 Bedrock 进程同一 OS 用户；RBAC **不是** OS 沙箱。
2. **HTTP 与会话**：生产强烈建议 HTTPS。`access_token` 存 Web Storage；`refresh_token` 为 HttpOnly Cookie（不设 Secure）。
3. **自定义命令**：仅超级管理员可维护与执行，须最小授权并审计。

详见 [.agents/docs/DESIGN.md](./.agents/docs/DESIGN.md) 与 [.agents/docs/ops-handbook.md](./.agents/docs/ops-handbook.md)。
