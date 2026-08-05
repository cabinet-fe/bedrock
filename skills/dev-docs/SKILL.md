---
name: dev-docs
description: >-
  Generates or incrementally updates per-repo Markdown under <out>/<repoKey>/,
  gated by .sync.json (baseCommit vs HEAD + local docs[] existence), then
  optionally mirrors those files into a Bedrock project's 开发文档 via PAT and
  POST /projects/{id|slug}/dev-docs/push. Use when documenting code repos into
  project 开发文档, syncing local docs, or publishing Markdown with scope
  dev_docs:write.
---

# Dev Docs

按**代码仓库**为单位：用 `.sync.json` 门禁决定是否跑 agent 写 md，再可选推送到 Bedrock 产品项目的**开发文档**空间。

脚本只负责**省 token**（比对 commit、查本地缺文件、打戳、镜像推送），**不能替代**读代码与用人话写说明。

## 硬性门禁（先读再跑；违反即错误）

1. **先查同步状态，再谈写文档。** 多仓：先跑 `sync_status.mjs`；单仓：先跑 `changed_since.mjs`。
2. **看 `action` 字段：**
   - `action: "noop"`（或 `upToDate: true` / `allUpToDate: true`）→ **立即跳过该仓（或整个任务）**。禁止写 md / `stamp` / `push`。
   - `action: "wrong_repo"` → 用 `suggestedRepoRoot` 重跑 `changed_since`，**禁止**因此盲目重写。
   - `action: "run_agent"` → 才允许读该仓代码、写/补齐该仓产出目录下的 md。
3. **noop 仅当** `baseCommit === HEAD` **且** `docs[]` 本地文件齐全。缺本地 md 即使 commit 一致也必须 `run_agent`。
4. **不做文件级 git diff。** commit 不一致即整仓更新，不返回「改了哪几个 md」。
5. **产出根必须稳定。** 未传 `--out`/`--dir` 时脚本会用 `$BEDROCK_AGENT_OUTPUT` 或自动发现已有 `output/` / `dev-docs/`（含 `.sync.json`）。禁止另起空目录导致「找不到 sync → 假全量」。
6. **多仓工作区按仓选对 `repoRoot`。** `stamp` 写入 `repoRel`；`sync_status` / `changed_since` 会校验。
7. **写成功后务必 `stamp_commit`**（带上本次 `--docs`），再按需 `push_docs`。

## When to use

- 用户要为工作区代码仓生成/更新开发文档，并可选入库到项目「开发文档」
- Agent 工作区已有代码 checkout，需要按增量门禁写 md 再推送
- **不要**用本技能推「接口文档」（那是 `java-api-docs` / `docs:write`）

## Requirements

| 项 | 说明 |
| ---- | ---- |
| Runtime | Node ≥ 18（内置 `fetch`）或 bun；无 npm 依赖；需本机 `git` |
| PAT（仅 push） | scope **`dev_docs:write`**；属主须满足目标项目成员 ACL |
| Env | `PAT`、`BEDROCK_HOST`（推送时）；可选 `BEDROCK_AGENT_OUTPUT` 作产出根 |

Token 勿写入技能目录。可用工作区 `.env` 或 `--env-file`。

## 目录约定

```text
{out}/                          # --out/--dir 或 $BEDROCK_AGENT_OUTPUT；默认发现/落盘为 dev-docs
  <repoKey>/                    # 建议用仓目录名，如 repo-1-main
    .sync.json                  # baseCommit + docs[] + repoRel
    README.md
    guides/architecture.md
  <repoKey2>/
    .sync.json
    ...
```

`.sync.json` 示例：

```json
{
  "baseCommit": "<full sha>",
  "updatedAt": "yyyy-MM-dd",
  "docs": ["README.md", "guides/architecture.md"],
  "repoRel": "repo-1-main"
}
```

- `docs[]`：相对该仓产出子目录的路径（**可含子路径**，不只 basename）
- `repoRel`：相对工作区的 git 仓路径，供选对仓
- 缺文件判定只查**本地**；web 删文要重生 → 同步删掉本地对应 `.md`

## 工作流

路径相对本技能根目录。

```bash
# 1) 多仓总览（或单仓 changed_since）
node scripts/sync_status.mjs --out <out> --workspace <工作区根>
# 或
node scripts/changed_since.mjs <repoRoot> --out <out> [--repo-key <name>]

# 2) action=noop → 跳过该仓；wrong_repo → 换 suggestedRepoRoot 重跑
# 3) action=run_agent → 读该仓代码，写/补齐 <out>/<repoKey>/**/*.md
# 4) 打戳
node scripts/stamp_commit.mjs <repoRoot> --out <out> --docs README.md,guides/architecture.md

# 5) 可选推送（--dir 通常为该仓产出子目录）
node scripts/push_docs.mjs --slug <项目ID或slug> --dir <out>/<repoKey> [--prefix <远程根>] [--dry-run]
```

| 脚本 | 行为 |
| ---- | ---- |
| `sync_status.mjs` | 扫 `<out>/<repoKey>/.sync.json`，汇总 `allUpToDate` / 每仓 `action` |
| `changed_since.mjs` | 单仓：`baseCommit===HEAD` 且 docs 齐全 → `noop`；否则 `run_agent`（可带 `missingDocs`） |
| `stamp_commit.mjs` | 写 `baseCommit=HEAD`，并集合并 `docs[]`，写 `repoRel` |
| `push_docs.mjs` | 镜像推送 `--dir` 下全部 `.md`（不读 `.sync.json`） |

## push_docs 参数

| 参数 / 环境变量 | 含义 |
| ----------------- | ---- |
| `--slug` / `BEDROCK_PROJECT` / `PROJECT_SLUG` | 项目数字 ID 或 slug |
| `--dir` / `DEV_DOCS_DIR` | 本地 Markdown 根（通常 `<out>/<repoKey>`） |
| `--prefix` / `DEV_DOCS_PREFIX` | 可选；加在远程 `doc_dir` 前 |
| `--env-file` | 显式 `.env`；否则 `$BEDROCK_AGENT_ENV_FILE` → `$BEDROCK_AGENT_WORKDIR/.env` → `./.env` |
| `--dry-run` | 只打印将推送的映射，不发请求 |
| `PAT` | Bearer 令牌（需 `dev_docs:write`） |
| `BEDROCK_HOST` | 如 `https://bedrock.example.com` |

标准输出 JSON：`{ pushed[], failed[], summary }`；有失败时退出码非 0。

## Path mapping（push）

递归收集 `--dir` 下全部 `.md`（跳过名以 `.` 开头的隐藏文件/目录，故 `.sync.json` 不会被推）。相对路径 → API 字段：

| 本地（相对 `--dir`） | `doc_dir` | `doc_name` |
| -------------------- | --------- | ---------- |
| `README.md` | `""`（根） | `README.md` |
| `guides/architecture.md` | `guides` | `architecture.md` |
| `a/b/c.md` + `--prefix product` | `product/a/b` | `c.md` |

请求体：`{ doc_dir, doc_name, content }` → `POST {BEDROCK_HOST}/api/v1/projects/{slug}/dev-docs/push`。

## Errors（push）

- **401** — PAT 无效或未正确 Bearer 传递
- **403** — 缺 `dev_docs:write`，或属主不满足项目 ACL
- **404** — 项目 ID/slug 不存在

先 `--dry-run` 核对映射，再正式推送。

## Example

```bash
# 工作区：repo-1-main/（git）+ 产出 dev-docs/
node skills/dev-docs/scripts/changed_since.mjs ./repo-1-main --out ./dev-docs
# → run_agent 时写 md …
node skills/dev-docs/scripts/stamp_commit.mjs ./repo-1-main --out ./dev-docs \
  --docs README.md,guides/architecture.md
node skills/dev-docs/scripts/push_docs.mjs --slug my-product \
  --dir ./dev-docs/repo-1-main --dry-run
```
