# 操作手册（2.0 GA）

面向运维与首次部署。产品行为以 [DESIGN.md](./DESIGN.md) 为准；分期 Gate 见 ROADMAP.md（已随 2.0 GA 归档）。

---

## 1. 全新安装（默认 SQLite）

### 1.1 一键安装与更新（install.sh，推荐）

Linux 服务器（amd64/arm64）执行：

```bash
curl -fsSL https://raw.githubusercontent.com/cabinet-fe/bedrock/main/scripts/install.sh | bash
# 中国大陆可加镜像前缀（任意 gh 代理均可）：
curl -fsSL https://gh-proxy.com/https://raw.githubusercontent.com/cabinet-fe/bedrock/main/scripts/install.sh | bash
```

- 交互菜单：安装 **Bedrock Server（主体）** / **Deploy Agent（代理分发工具）** / 更新已安装组件 / 查看状态；子命令 + `--yes` 可全非交互（`--help` 查看参数）
- 下载 GitHub Release 产物并校验 SHA256（平台级 `.sha256`）；GitHub 直连不可达时自动改走镜像（内置 `gh-proxy.com`、`ghfast.top`，`--mirror` / `BEDROCK_MIRROR` 可自定义，下载失败自动轮换源）
- Server 安装目录默认 `/opt/bedrock`（非 root 为 `~/bedrock`），Agent 为 `/opt/bedrock-agent`（`~/bedrock-agent`）；生成 `config.yaml`（`encryption.key`/`jwt.secret` 随机 64 hex、超管密码随机生成仅打印一次，文件权限 600），数据落 `<安装目录>/data`
- 服务托管：root 且有 systemd 时安装 `bedrock` / `bedrock-agent` 单元（开机自启、`TimeoutStopSec=45` 匹配 Server 30s 优雅停机）；否则 nohup + `<目录>/.<name>.pid` 与 `<目录>/<name>.log`。安装/更新时把登录 shell 的 `PATH`（交互探测）与 `HOME` 固化进服务环境（systemd `Environment=` 行 / nohup `env PATH=`），构建脚本不依赖服务进程被谁启动；安装后新装的用户级工具由引擎执行时按常见目录兜底补全（`internal/engine/env_path.go`）
- 更新（`install.sh update [--version TAG] [--dir DIR]`）：下载新版本并校验 → 优雅停机（SIGTERM，最长 60s，超时 SIGKILL 兜底）→ 停机窗口备份 SQLite 到 `<目录>/backups/`（保留 3 份）→ 替换二进制（旧版留存 `<bin>.bak`）→ 重启 → 健康检查（Server `/api/v1/health`，Agent `/healthz`）；健康检查失败自动回滚 `.bak` 并恢复运行
- 安装 / 重装（`server` / `agent` 子命令）同样先优雅停旧进程、预检端口占用（被绕过服务管理器的进程占用时报错退出，不动二进制）；健康检查失败会自动打印最近 30 行日志（journalctl 或 `<目录>/<name>.log`），进程启动即退时秒级报错不等满窗口，存在 `.bak` 时自动回滚恢复
- `config.yaml` 永不覆盖（重装/更新均保留），已有配置时端口/管理员/token 以配置文件为准，`--port` 等参数与现配置冲突会明示不生效；版本对比依赖 `--version` 输出；`install.sh status` 查看版本/服务/健康
- **bedctl 命令行工具**：安装/更新成功后脚本自动把自身装为 `bedctl`（root: `/usr/local/bin/bedctl`，非 root: `~/.local/bin/bedctl`），并把各组件安装目录与下载源记入状态文件（root: `/etc/bedrock/bedctl.env`，非 root: `~/.bedrock/bedctl.env`），后续命令无需重复 `--dir` / `--mirror`：

```bash
bedctl install server|agent [选项]   # 安装（同 install.sh server|agent）
bedctl update [server|agent]         # 更新
bedctl status                        # 状态
bedctl start|stop|restart [server|agent]  # 服务管理（缺省作用于全部已安装组件）
bedctl logs [server|agent] [-n N]    # 最近日志（默认 100 行）
bedctl doctor [server|agent]         # 体检：服务/端口/监听地址/本机健康/防火墙
bedctl self-update                   # bedctl 自我升级
```

### 1.1.1 外部访问不通排查（bedctl doctor）

云部署「安全组已放行但仍连不上」时，在服务器上运行 `bedctl doctor`（非 root 时自动降级为只读检查；建议 sudo 运行以启用防火墙自动检查），按其输出处理。常见层级从内到外：

1. **服务本身没起来**（最常见）：doctor 显示「服务: 未运行 / 端口无监听」时先看 `bedctl logs`；健康检查失败的具体原因会打印最近 30 行日志。
2. **配置只绑了回环**：`config.yaml` 的 `server.host` 为 `127.0.0.1` / `localhost` 时本机健康检查通过但外部不可达，改为 `0.0.0.0` 后 `bedctl restart server`。
3. **宿主机防火墙**：安全组之外还有一层，CentOS 系 firewalld / Ubuntu 系 ufw 需放行端口（root 下 doctor 会自动检查并给出放行命令）。
4. **云平台层**：安全组入方向 TCP 端口（源 0.0.0.0/0）、EIP 已绑定实例、用公网 IP 而非内网 IP 访问。
5. **反向代理**：配了 Nginx/HTTPS 时检查反代监听与 upstream。
6. **定位方法**：外部机器 `curl -v http://<公网IP>:<端口>/` —— 超时＝安全组/防火墙，连接拒绝＝服务未监听。

### 1.2 手动安装

```bash
# 1. 取得发布包（示例：Linux amd64）
#    bedrock-linux-amd64 + bedrock-agent-linux-amd64（+ .sha256）

# 2. 准备空数据目录与配置
mkdir -p ./data
cp config.example.yaml config.yaml   # 或参考仓库 config.yaml
# database.driver: sqlite
# database.path: ./data/bedrock.sqlite
# encryption.key: 64 hex（生产务必更换）
# admin.username / admin.password: 首启种子超管

# 3. 启动（空目录 → migration → 种子超管）
./bedrock-linux-amd64 --config ./config.yaml

# 4. 验证
curl -fsS http://127.0.0.1:8080/api/v1/health
# 浏览器打开 http://host:8080 使用超管登录
```

可复现冒烟：

```bash
make smoke-fresh-install
```

---

## 2. 多数据库配置

支持 `sqlite`（默认）、`postgres` / `postgresql`、`mysql`。

- **改 driver ≠ 搬迁数据**。切换引擎前请自行用目标库工具完成数据搬迁（平台不提供跨库迁移）。
- 错误的连通性配置会导致 **拒绝启动**。
- 合同测试 / 冒烟：

```bash
# 单元级三驱动合同（需 DSN 时设置环境变量）
go test ./internal/platform/db/... -tags=contract
# BEDROCK_CONTRACT_POSTGRES_DSN / BEDROCK_CONTRACT_MYSQL_DSN

# 进程级冒烟（SQLite 必跑；Postgres/MySQL 可选）
make smoke-three-db
# BEDROCK_SMOKE_POSTGRES=1 BEDROCK_DB_*...
# BEDROCK_SMOKE_MYSQL=1 BEDROCK_DB_*...
```

---

## 3. 备份指引（不假装统一物理备份）

| 驱动 | 建议 |
| --- | --- |
| SQLite | 停写或使用备份命令/文件复制 `database.path`；同时备份 `build.*` / `storage.root` 等数据目录 |
| Postgres | `pg_dump` / PITR 等官方工具 |
| MySQL | `mysqldump` / 官方备份方案 |

平台可提供备份**指引**与（若有）SQLite 辅助命令；**不会**声称跨引擎统一物理备份。

工作区、制品、日志、对象存储目录需按业务 RPO 一并纳入备份范围。

---

## 4. 2.0 内部升级：旧 Agent 数据清理

本节仅适用于从仍保留旧 Agent 单 Run 输出/归档能力的 **2.0 版本**升级。

升级前必须：

1. 停止 Bedrock，避免 Agent Run 或构建继续写入。
2. 完整备份数据库、`{workspace}/agents/` 和制品根目录，并确认备份可恢复。
3. 确认相关目录有足够空间完成同文件系统隔离移动。

首次应用 `000018_agent_persistent_workspace` 时，Server 会安全清理旧的 `{workspace}/agents/agent-{id}/runs/`，以及当时 Agent 归档根下的 `agent-{id}/run-{runID}.zip` / `run-{runID}.tar.gz`。该 migration 保留 `ai_agents.output_dir`，删除 `artifact_format` / `max_artifacts` / `agent_runs.artifact_path`。清理先严格校验路径边界与软链祖先，再将目标原子移入同根隔离区；数据库迁移提交后才删除隔离区，异常退出后可幂等续做。

后续 `000041_agent_run_artifacts` 重新引入 `agent_runs.artifact_path` / `artifact_kind`：Run 成功时对固定 `output_dir` 做快照 zip 归档（`{artifact_dir}/agent-{id}/run-{runID}.zip`），可供下载；与 000018 清理的旧 per-run 工作区语义不同。

路径越界、软链风险、移动或删除失败时，Server 会拒绝升级启动。清理不会清空 Agent 持久根工作区中的其他文件，也不会触碰 CI/CD BuildRun 的工作区、归档或下载能力。遇到失败时不要手工跳过 migration；保留现场，根据错误修正路径/权限后重试，必要时从升级前备份恢复。

---

## 5. 已接受风险（产品内可见 + 文档）

1. **HTTP + 浏览器会话存储**：`access_token`（Web Storage）可能被同机脚本读取；`refresh_token` 为 HttpOnly Cookie（不设 Secure）；`password_cipher` **不替代** TLS。生产强烈建议 HTTPS。
2. **同 UID 执行**：构建脚本、AI CLI、自定义超管命令与 Bedrock 进程同一 OS 用户；RBAC **不是** OS 沙箱。
3. **自定义超管命令 / 开发环境脚本**：仅超管；任意命令执行，须审计与最小授权。

---

## 6. 前端 embed 与回滚

- 默认 `FRONTEND_DIR=web`；Release 将 `web/dist` 拷入 `cmd/server/dist` 后 `go build` embed。
- 回滚步骤见 [release-checklist.md](./release-checklist.md#前端-embed-回滚)。

---

## 7. 发布包回退

1. 停止当前 Server / Agent 进程。
2. 换回上一版本二进制（校验 SHA256）。
3. **不要**对 2.0 schema 期望兼容更旧的未声明迁移；回退前确认 migration 版本与备份。
4. 数据目录从备份还原（若二进制回退伴随破坏性 schema 变更）。

---

## 8. Deploy Agent

独立二进制与 Server **同版本**发布：`bedrock-agent-linux-amd64` / `bedrock-agent-linux-arm64` 等。Agent 部署在目标机，不嵌入 Server。完整操作说明见平台内「操作手册 → Deploy Agent」。
