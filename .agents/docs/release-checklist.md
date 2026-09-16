# 发布检查单（2.0 GA）

发布前逐项确认。操作细节见 [ops-handbook.md](./ops-handbook.md)。

## 版本与产物

**所有发版产物由 CI 构建**：`.github/workflows/release.yml` 由 push `v*` tag 触发，gate（测试，2 分片并行）与 frontend 构建同时跑；交叉编译 Server + Agent（`main.version` 注入 tag 名）只依赖 frontend 产物、与 gate 并行，最后 publish 等 gate 与 binaries 全绿后汇总 SHA256 并发布 GitHub Release。发版**禁止**本地构建待发布产物（`make build-linux` / `build-win` / `build-agent-*` / `checksums`），本地编出的二进制和校验和既不进 Release 也不进仓库。

发版步骤：

1. 过完「质量门禁」后打 tag 并推送：`git tag vX.Y.Z && git push origin vX.Y.Z`
2. 盯 CI 到全部绿：`gh run watch`（或 `gh run list --workflow=release.yml`）
3. 逐项核对 Release 产物：
   - [ ] Server 二进制：`bedrock-linux-amd64`、`bedrock-linux-arm64`、`bedrock-windows-amd64.exe`
   - [ ] Deploy Agent：`bedrock-agent-<os>-<arch>` 与 Server **同版本**
   - [ ] 每个产物附带 SHA256（CI 生成 `*.sha256` / `SHA256SUMS`）
   - [ ] Release notes 由 CI 从上一 tag 起的变更自动生成
   - [ ] 嵌入前端为 **web**（CI 先以 `FRONTEND_DIR=web` 构建前端再交叉编译）

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
