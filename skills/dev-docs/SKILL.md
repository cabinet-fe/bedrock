---
name: dev-docs
description: >-
  Syncs a local Markdown directory into a Bedrock project's 开发文档 (dev docs)
  space via PAT and POST /projects/{id|slug}/dev-docs/push. Use when the user
  asks to push, upload, or mirror local docs into project 开发文档, or when an
  agent needs to publish Markdown under a project with scope dev_docs:write.
---

# Dev Docs Push

把本地目录下的 `.md` 镜像推送到 Bedrock 产品项目的**开发文档**空间。

主入口是脚本；本文件说明何时调用与路径/鉴权约定。

## When to use

- 用户要把本地 Markdown（设计文档、指南、手册等）同步进项目「开发文档」
- Agent 工作区已写好 docs，需要入库到指定项目
- **不要**用本技能推「接口文档」（那是 `java-api-docs` / `docs:write`）

## Requirements

| 项 | 说明 |
| ---- | ---- |
| Runtime | Node ≥ 18（内置 `fetch`）或 bun；无 npm 依赖 |
| PAT | scope **`dev_docs:write`**；属主须满足目标项目成员 ACL |
| Env | `PAT`、`BEDROCK_HOST`（服务根，无尾斜杠） |

Token 勿写入技能目录。可用工作区 `.env` 或 `--env-file`。

## Command

路径相对本技能根目录。从任意 cwd 执行均可（`--dir` 用绝对路径或相对 cwd）。

```bash
node scripts/push_docs.mjs --slug <项目ID或slug> --dir <本地目录> [--prefix <远程根>] [--env-file <path>] [--dry-run]
# 或
bun scripts/push_docs.mjs --slug my-product --dir ./docs
```

| 参数 / 环境变量 | 含义 |
| ----------------- | ---- |
| `--slug` / `BEDROCK_PROJECT` / `PROJECT_SLUG` | 项目数字 ID 或 slug（路径参数） |
| `--dir` / `DEV_DOCS_DIR` | 本地 Markdown 根目录 |
| `--prefix` / `DEV_DOCS_PREFIX` | 可选；加在远程 `doc_dir` 前 |
| `--env-file` | 显式 `.env`；否则 `$BEDROCK_AGENT_ENV_FILE` → `$BEDROCK_AGENT_WORKDIR/.env` → `./.env` |
| `--dry-run` | 只打印将推送的映射，不发请求 |
| `PAT` | Bearer 令牌（需 `dev_docs:write`） |
| `BEDROCK_HOST` | 如 `https://bedrock.example.com` |

标准输出 JSON：`{ pushed[], failed[], summary }`；有失败时退出码非 0。

## Path mapping

递归收集 `--dir` 下全部 `.md`（跳过名以 `.` 开头的隐藏文件/目录）。相对路径 → API 字段：

| 本地（相对 `--dir`） | `doc_dir` | `doc_name` |
| -------------------- | --------- | ---------- |
| `README.md` | `""`（根） | `README.md` |
| `guides/architecture.md` | `guides` | `architecture.md` |
| `a/b/c.md` + `--prefix product` | `product/a/b` | `c.md` |

请求体：`{ doc_dir, doc_name, content }` → `POST {BEDROCK_HOST}/api/v1/projects/{slug}/dev-docs/push`。

服务端：`doc_dir` 空=根；`/` 分隔；拒绝 `..` / 绝对路径 / 空段；目录不存在则创建；`doc_name` 缺 `.md` 时补齐；`201` 新建 / `200` 更新。

## Errors

脚本对常见鉴权失败给出明确提示：

- **401** — PAT 无效或未正确 Bearer 传递
- **403** — 缺 `dev_docs:write`，或属主不满足项目 ACL（响应含 `token scope insufficient` 时会点明 scope）
- **404** — 项目 ID/slug 不存在

先 `--dry-run` 核对映射，再正式推送。

## Example

```bash
# .env
# PAT=br_...
# BEDROCK_HOST=http://127.0.0.1:8080

node skills/dev-docs/scripts/push_docs.mjs --slug my-product --dir ./docs --dry-run
node skills/dev-docs/scripts/push_docs.mjs --slug my-product --dir ./docs --prefix engineering
```
