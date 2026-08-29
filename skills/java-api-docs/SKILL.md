---
name: java-api-docs
description: >-
  用 Node 脚本扫描 Java Spring Controller 与 DTO，按 Controller 生成或增量更新
  面向 AI 的 API Markdown。用户点名模块/接口时立刻跑 prepare.mjs --force，禁止先探索仓库。
  适用于分析 Java/Spring REST API、从后端源码生成 API 文档，或依据 Controller 编写前端调用。
---

# Java API 文档

扫描 Spring Controller → `<out>/<project>/<kebab>.md`。脚本列 path / 字段 / 变更；说明和示例必须读源码用人话写。

技能根 = 本文件所在目录。脚本 = `技能根/scripts/`。工作区 = `$BEDROCK_AGENT_WORKDIR` 或 cwd。产出 = `$BEDROCK_AGENT_OUTPUT` 或脚本自动发现（常见 `output/`）。

## 第一动作（现在就跑，禁止先逛）

读完本段**立刻执行**下面那条命令。禁止：Glob/Grep 找 Java、列目录熟悉结构、读 ARCHITECTURE/CODE-MAP、读 template/example/writing.md、用对话复述门禁。

用户点名了模块 / 项目 / Controller，或说「重新生成 / 重生 / 再生成」：

```bash
node <技能根>/scripts/prepare.mjs --project <用户说的名字> --force
```

只说「更新 / 同步文档」、没点名：

```bash
node <技能根>/scripts/prepare.mjs
```

看 JSON 的 `action` / `agentHint`（不要猜）：

| action | 立刻做什么 |
| --- | --- |
| `noop` / `allUpToDate` | 结束，一句话汇报。禁止 `list_endpoints` / 写 md / stamp |
| `wrong_repo` | 用 `suggestedRepoRoot` 再跑 prepare，禁止因此全量 |
| `need_project` / `not_found` | 把 `candidates` 列给用户，停止 |
| `update_docs` | 只处理返回的 `docFiles` / `listFiles` |
| `full_scan` | 全量 `list_endpoints`（不带 `--files`） |

`prepare` 已给出 `bizSrcRoot` / `apiSrcRoot` / `next.list_endpoints`。照抄。不要自己找路径。`--force` 仅当用户点名重生；未点名禁止 `--force` 全仓。

## 接着干活

1. 若 `conventionsMissing`：先跑 `next.ensure_conventions`。
2. 照 `next.list_endpoints` 跑。禁止把全量 JSON 整包读进对话；按 `controllers[]` **逐个**处理。
3. 对要写的对象类型：照 `next.resolve_types` 把 `<TypeA,TypeB>` 换成实际类型。必须用 **apiSrcRoot**，禁止把 biz 传给 `resolve_types`。
4. **现在才读** [references/writing.md](references/writing.md)、[references/project-conventions.md](references/project-conventions.md)，对照 [references/template.md](references/template.md) 写 md。每个 Controller 一个文件；path **字符级**抄脚本；对象必须落字段表。
5. 照 `next.verify_docs` 跑；`ok: false` 按 diff 改 MD 后重跑，禁止 stamp。
6. 照 `next.stamp` 跑（带本次 `--docs`）。
7. 可选推送：`node <技能根>/scripts/push_docs.mjs --slug <项目标识>`（默认增量；禁止无故 `--all`）。

打开源码范围：脚本列出的 Controller / DTO，以及为理解行为所必需的少量 Service。

## 硬规则（干活时核对，不是开工前研究）

- 双 srcRoot：Controller 扫 `*-biz/.../src/main/java`；DTO 解析 `*-api/.../src/main/java`。`prepare` 已拆好。
- `--files` 须相对 srcRoot（如 `com/.../FooController.java`），用 prepare 的 `listFiles`。
- path 禁止改单复数；以 `list_endpoints` 的 `path` 为准。
- 对象必须 `resolve_types` 后落表。BaseDTO 请求体嵌套 `_shared`，禁止摊平。
- 产出根必须稳定：不要另起空的 `api-docs` 导致假全量。
- 多模块 `--repo-root` 用 prepare 给的 `moduleRoot`，不要把整仓根当唯一线索。
- 未跑 `verify_docs` 禁止 stamp；`action=noop` 禁止继续生成。

## 输出

```text
<out>/
  _conventions.md
  <project>/
    .sync.json          # baseCommit + docs[] + repoRel
    sys-user.md         # SysUserController
```

文件名：去掉类名末尾 `Controller` 再 kebab-case。`<project>`：`--project` → pom `artifactId` → 目录名。一 Controller 一文件。

## 脚本

从工作区根执行。路径相对技能根。入口只有 `prepare.mjs`；其余由它的 `next` 给出。

```bash
node scripts/prepare.mjs [--project <名>] [--force] [--docs a.md] [--out <dir>] [--workspace <dir>]
node scripts/ensure_conventions.mjs [--out <dir>]
node scripts/list_endpoints.mjs <bizSrcRoot> [--files a.java] [--project name] [--repo-root dir]
node scripts/resolve_types.mjs <apiSrcRoot> TypeA,TypeB
node scripts/verify_docs.mjs <bizSrcRoot> --project name --repo-root <moduleRoot> [--docs a.md]
node scripts/stamp_commit.mjs <repoRoot> --project name --docs a.md,b.md
node scripts/push_docs.mjs --slug <项目标识> [--docs a.md] [--all]
```

`sync_status.mjs` / `changed_since.mjs` 由 prepare 内部调用，不要开工先跑它们。

推送默认只推相对远程有变化的 md。需工作区 `.env` 的 `PAT`、`BEDROCK_HOST`。`--env-file` → `$BEDROCK_AGENT_ENV_FILE` → `$BEDROCK_AGENT_WORKDIR/.env` → `./.env`。

## 检查清单

```
- [ ] 第一动作是 prepare.mjs；未先逛工作区
- [ ] 已按 action 分支（noop 已跳过；点名重生用了 --force）
- [ ] 已跑 list_endpoints / resolve_types；path 字符级来自脚本
- [ ] 写 md 时才读 writing.md；每个对象有字段表；_shared 未摊平
- [ ] verify_docs ok:true 后才 stamp
- [ ] 若需入库：push_docs 默认增量；未无故 --all
```
