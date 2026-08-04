# 操作手册（2.0 GA）

面向运维与首次部署。产品行为以 [DESIGN.md](./DESIGN.md) 为准；分期 Gate 见 [ROADMAP.md](./ROADMAP.md)。

---

## 1. 全新安装（默认 SQLite）

2.0 **只支持全新安装**，**不提供** 1.x → 2.0 数据迁移。

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

本节仅适用于从仍保留旧 Agent 单 Run 输出/归档能力的 **2.0 版本**升级；仍不支持 1.x → 2.0 数据迁移。

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

独立二进制与 Server **同版本**发布：`bedrock-agent-linux-amd64` / `bedrock-agent-linux-arm64` 等。Agent 部署在目标机，不嵌入 Server。
