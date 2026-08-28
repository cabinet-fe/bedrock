# 发布检查单（2.0 GA）

发布前逐项确认。操作细节见 [ops-handbook.md](./ops-handbook.md)。

## 版本与产物

- [ ] Git tag：`vX.Y.Z`（与 `main.version` / Release 备注一致）
- [ ] Changelog / GitHub Release notes（`generate_release_notes` 或手写）
- [ ] Server 二进制：`bedrock-linux-amd64`、`bedrock-linux-arm64`（及需要的 Windows 包）
- [ ] Deploy Agent：`bedrock-agent-<os>-<arch>` 与 Server **同版本**
- [ ] 每个产物附带 SHA256（CI 生成 `*.sha256` / `SHA256SUMS`）
- [ ] 嵌入前端为 **web**（`FRONTEND_DIR=web`）

本地交叉编译：

```bash
make build-linux          # bedrock-linux-amd64（含 web embed）
make build-linux-arm64    # bedrock-linux-arm64
make build-agent-linux    # bedrock-agent-linux-amd64
make build-agent-linux-arm64
make smoke-linux-package  # 产出校验和；Linux amd64 主机可启动冒烟
```

## 质量门禁

- [ ] API 变更已同步到对应 `api/<域>.md`
- [ ] `cd web && vp check && vp build`
- [ ] `go test ./...`（或 CI 等价）
- [ ] `make smoke-fresh-install`
- [ ] `make smoke-api-e2e`
- [ ] `make smoke-restart-recovery`
- [ ] P0–P4 Gate 无未关闭落地阻塞项（原 known-issues.md 已归档删除，限制项见 git 历史）
- [ ] web 切换 Gate 证据见 roadmap/P5-switch-gate.md（已随 2.0 GA 归档）

## 文档

- [ ] PRD / DESIGN / AGENTS 无矛盾
- [ ] 风险说明：HTTP + access Web Storage / refresh HttpOnly Cookie（不设 Secure）、同 UID、自定义超管命令

## 前端 embed 回滚

上一版 `web` 产物可保留 **至少一个发布周期**。

```bash
# 或：检出上一发布 tag 的前端 dist，拷入 cmd/server/dist 后重新 go build
rm -rf cmd/server/dist && cp -r /path/to/previous/dist cmd/server/dist
make build-backend
```

Go embed **只认** `cmd/server/dist`，与来源目录无关。

## 发布包回退

1. 停止进程 → 换回上一版二进制（校验 checksum）→ 按需还原 data 备份 → 启动 → `/api/v1/health` + 登录。
