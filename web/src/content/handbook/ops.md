# 运维手册

面向首次部署与日常运维。日常使用见「使用说明」，Deploy Agent 见同页「Deploy Agent」标签，对外 HTTP 接口见「开放接口」。

---

## 1. 全新安装（默认 SQLite）

```bash
# 1. 取得发布包（示例：Linux amd64）
#    bedrock-linux-amd64 + bedrock-agent-linux-amd64（+ .sha256 校验文件）

# 2. 准备空数据目录与配置
mkdir -p ./data
cp config.example.yaml config.yaml
# 按需修改下方「配置参考」中的项；生产务必更换所有密钥

# 3. 启动（空数据目录 → 自动 migration → 种子超管）
./bedrock-linux-amd64 --config ./config.yaml

# 4. 验证
curl -fsS http://127.0.0.1:8080/api/v1/health
# 浏览器打开 http://host:8080，使用 admin.* 配置的种子超管登录
```

仓库侧可复现冒烟：`make smoke-fresh-install`。

---

## 2. 配置参考（config.yaml）

| 配置项                                                               | 默认值                               | 说明                                                  |
| -------------------------------------------------------------------- | ------------------------------------ | ----------------------------------------------------- |
| `server.port` / `server.host`                                        | `8080` / `0.0.0.0`                   | HTTP 监听地址                                         |
| `database.driver`                                                    | `sqlite`                             | `sqlite` / `postgres` / `mysql`                       |
| `database.path`                                                      | `./data/bedrock.sqlite`              | SQLite 文件路径                                       |
| `database.host` / `port` / `name` / `user` / `password` / `ssl_mode` | —                                    | Postgres / MySQL 连接参数                             |
| `jwt.secret`                                                         | `change-me-in-production`            | **生产必改**；JWT 签名密钥                            |
| `jwt.access_ttl` / `jwt.refresh_ttl`                                 | `2h` / `168h`                        | 访问 / 刷新令牌有效期                                 |
| `build.max_concurrent`                                               | `3`                                  | 构建并发上限                                          |
| `build.workspace_dir` / `artifact_dir` / `log_dir` / `cache_dir`     | `./data/*`                           | 工作区 / 制品 / 日志 / 缓存目录                       |
| `storage.root`                                                       | `./data/storage`                     | 对象存储根目录                                        |
| `storage.attachment_max_bytes`                                       | `20971520`（20MB）                   | 附件上传上限                                          |
| `storage.doc_import_max_bytes`                                       | `104857600`（100MB）                 | 文档导入上限                                          |
| `encryption.key`                                                     | 示例值                               | **生产必改**；64 hex（32 字节），须与前端注入密钥一致 |
| `admin.username` / `admin.password`                                  | `admin` / `change-me-on-first-login` | 首启种子超管，**首次登录后立即修改**                  |

---

## 3. 多数据库配置

支持 `sqlite`（默认）、`postgres` / `postgresql`、`mysql`。

- **改 driver ≠ 搬迁数据**：切换引擎前请自行用目标库工具完成数据搬迁，平台不提供跨库迁移。
- 错误的连通性配置会导致 **拒绝启动**。

仓库侧验证手段：

```bash
# 单元级三驱动合同测试（需通过环境变量提供 DSN）
go test ./internal/platform/db/... -tags=contract
# BEDROCK_CONTRACT_POSTGRES_DSN / BEDROCK_CONTRACT_MYSQL_DSN

# 进程级三库冒烟（SQLite 必跑；Postgres / MySQL 可选）
make smoke-three-db
```

---

## 4. 备份指引

平台不提供跨引擎统一物理备份，按驱动选择官方方案：

| 驱动     | 建议                                                      |
| -------- | --------------------------------------------------------- |
| SQLite   | 停写后复制 `database.path` 文件（或使用 SQLite 备份命令） |
| Postgres | `pg_dump` / PITR 等官方工具                               |
| MySQL    | `mysqldump` / 官方备份方案                                |

除数据库外，以下目录需按业务 RPO 一并纳入备份：

- `build.workspace_dir`（工作区）
- `build.artifact_dir`（制品）
- `build.log_dir`（日志）
- `storage.root`（对象存储 / 附件）

---

## 5. 升级与回退

### 5.1 2.0 内部升级：旧 Agent 数据清理

仅适用于从仍保留旧 Agent 单 Run 输出/归档能力的 2.0 版本升级。

升级前必须：

1. 停止 Bedrock，避免 Agent Run 或构建继续写入。
2. 完整备份数据库、`{workspace}/agents/` 和制品根目录，并确认备份可恢复。
3. 确认相关目录有足够空间完成同文件系统隔离移动。

首次应用 `000018_agent_persistent_workspace` 时，Server 会安全清理旧的 `{workspace}/agents/agent-{id}/runs/` 及当时 Agent 归档根下的 `agent-{id}/run-{runID}.zip` / `.tar.gz`。清理先严格校验路径边界与软链祖先，再将目标原子移入同根隔离区，数据库迁移提交后才删除隔离区；异常退出后可幂等续做。

后续版本通过 `000041_agent_run_artifacts` 恢复成功 Run 的产出目录快照归档（`{artifact_dir}/agent-{id}/run-{runID}.zip`，`agent_runs.artifact_path`），语义为按 Run 快照而非旧 per-run 工作区。

路径越界、软链风险、移动或删除失败时 Server 会 **拒绝启动**。此时不要手工跳过 migration：保留现场，修正路径/权限后重试，必要时从升级前备份恢复。清理不影响 Agent 持久根工作区中的其他文件，也不触碰 CI/CD BuildRun 的工作区、归档或下载能力。

### 5.2 发布包回退

1. 停止当前 Server / Agent 进程。
2. 换回上一版本二进制（先校验 SHA256）。
3. 回退前确认 migration 版本与备份；不要期望 2.0 schema 兼容更旧的未声明迁移。
4. 若二进制回退伴随破坏性 schema 变更，从备份还原数据目录。

### 5.3 前端 embed 回滚

Release 将 `web/dist` 拷入 `cmd/server/dist` 后 `go build` embed；回滚步骤见仓库 `docs/release-checklist.md`「前端 embed 回滚」一节。

---

## 6. Deploy Agent

独立二进制 `bedrock-agent`，部署在目标机，与 Server **同版本**发布。安装、配置、平台对接与排障见操作手册左侧 **「Deploy Agent」** 标签页。

---

## 7. 已接受风险

1. **HTTP + 浏览器会话存储**：`access_token`（Web Storage）可能被同机脚本读取；`refresh_token` 为 HttpOnly Cookie（兼容 HTTP 部署不设 Secure）；`password_cipher` **不替代** TLS。生产强烈建议反向代理 HTTPS。
2. **同 UID 执行**：构建脚本、AI CLI、自定义超管命令与 Bedrock 进程同一 OS 用户；RBAC **不是** OS 沙箱。
3. **自定义超管命令 / 开发环境脚本**：仅超管可用；属于任意命令执行，须审计与最小授权。

---

## 8. 日常排障

| 现象              | 排查                                                                           |
| ----------------- | ------------------------------------------------------------------------------ |
| 无法启动          | 检查数据库连通性配置（错误配置会拒绝启动）；查看启动日志中的 migration 错误    |
| 升级后拒绝启动    | 多为旧 Agent 数据清理失败（见 5.1）；按错误提示修正路径/权限，勿跳过 migration |
| 接口探活          | `curl -fsS http://host:8080/api/v1/health`                                     |
| 构建 / 运行日志   | 页面内实时日志（WebSocket）；落盘日志见 `build.log_dir`                        |
| 登录问题          | 确认种子超管未被改密；`jwt.secret` 变更会使全部令牌失效，需重新登录            |
| Deploy Agent 问题 | 见「Deploy Agent」手册 §11 常见故障                                            |
