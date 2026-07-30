---
name: java-api-docs
description: >-
  用 Node 脚本扫描 Java Spring Controller 的接口与 DTO，并结合
  <out>/<project>/.sync.json 中绑定的 git 提交号，按 Controller 生成或增量更新
  面向 AI 的 API Markdown。适用于分析 Java/Spring REST API、从后端源码生成
  API 文档，或依据 Controller 编写前端调用。默认产出到工作区 api-docs（若已有
  output/ 下的 .sync.json 则自动复用），支持多仓库分项目输出；对外 path 由手维
  网关 JSON 前缀与 Controller 映射拼接。
---

# Java API 文档

扫描 Spring Web MVC Controller → 写出便于 AI 前端代码生成的 Markdown。

脚本只负责**省 token**（列接口路径与文档文件名、查网关前缀、列字段、查变更），**不能替代**读关键源码与用人话写说明。接口说明、业务语义、请求/响应示例，必须结合 Controller / Service / DTO 的真实意图用中文写清楚。

## 硬性门禁（先读再跑；违反即错误）

1. **先查变更，再谈生成。** 多仓/多项目：先跑 `sync_status.mjs`；单项目：先跑 `changed_since.mjs`。
2. **看 `action` 字段，不要只看 mode：**
   - `action: "noop"`（或 `upToDate: true` / `allUpToDate: true`）→ **立即结束该项目（或整个任务）**。禁止 `list_endpoints` / `resolve_types` / 写 md / `stamp`。
   - `action: "wrong_repo"` → 用 `suggestedRepoRoot` 重跑 `changed_since`，**禁止**因此全量重生成。
   - `action: "update_docs"` → 只更新返回的 `docFiles` / `files`。
   - `action: "full_scan"` → 才允许全量。
3. **产出根必须稳定。** 未传 `--out` 时脚本会自动发现已有的 `output/` / `api-docs/`（含 `.sync.json`）。若工作区已有文档，**必须**继续用同一目录，禁止另起一个空的 `api-docs` 导致「找不到 sync → 假全量」。
4. **多仓工作区按项目选对 `repoRoot`。** `stamp` 会写入 `repoRel`；`sync_status` / `changed_since` 会校验。用错仓会出现 `wrong_repo`，不是全量信号。
5. **path 字符级照抄脚本，禁止「规范化」。** 不得按英语习惯、`@Tag`、类名把 path 段改成单数/复数（如 `/users` → `/user`）。写完后必须跑 `verify_docs.mjs`；未通过禁止 `stamp` / `push`。
6. **多模块单体仓的 `--repo-root` 必须指到具体模块**（含该模块 `application.yml` 的目录），或显式传 `--service` / `--project`。禁止把整仓根当唯一线索却不传 project（脚本遇多个 `spring.application.name` 会拒绝静默取第一个）。
7. **对象必须展开字段。** 禁止「返回 `UserInfo` / `XxxDTO`」却无字段表；写对象请求/响应前必须 `resolve_types`；同名 DTO 可集中到本文 `## 数据模型`，接口处锚点引用。

## 必须遵守

- 运行时：Node ≥ 24，ESM `scripts/*.mjs`，**不安装 npm 包**（只用 Node 内置模块 + `git`；推送脚本另用全局 `fetch`）
- **双 srcRoot（多模块单体仓）**：
  - `list_endpoints` / `verify_docs` / `changed_since` 的 Controller 扫描：`<module>-biz/.../src/main/java`
  - `resolve_types` 的 DTO/Entity 解析：`<module>-api/.../src/main/java`（或项目约定中的 api 模块）
  - 禁止把 biz 的 srcRoot 传给 `resolve_types`，也禁止用 api 根扫 Controller
- **禁止整包读入 `list_endpoints` JSON**（全量可达数百 KB）。增量必须用 `--files` 或 `changed_since` 的 `docFiles` / `files` 分批；全量时按 `controllers[]` **逐个**处理，每次只保留当前 Controller 的 endpoints 子集
- **`--files` 须传相对 srcRoot 的路径**（如 `com/.../FooController.java`），不能只传文件名
- 产出根默认：工作区 `api-docs/`；若已存在 `output/`（或其它候选）下的同步记录则自动复用（可用 `--out` 覆盖）；相对路径相对 `process.cwd()`；配合 `--workspace` 时脚本会把 `--out` 解析到真实产出根
- 单仓与多仓同一布局：`<out>/<project>/`（多仓靠 project 子目录区分）
- **一 Controller 一文件**：`<out>/<project>/<kebab>.md`（见「输出目录」）
- 约定页：运行技能时生成**唯一**的 `<out>/_conventions.md`；接口文档用 `../_conventions.md` 引用
- 规范输入：只读本技能 [references/project-conventions.md](references/project-conventions.md)；**不要**把 `_conventions.md` 当输入
- 网关前缀：只读 [references/ic-gateway-dev.json](references/ic-gateway-dev.json)，由 `list_endpoints` 按 `service`/`id` 查找并注入；**禁止手算**
- 同步记录：每个项目一份 `<out>/<project>/.sync.json`（`baseCommit` + `docs[]` + `repoRel`）；**不要**写进 Markdown 正文
- 表格类型：`string` / `number` / `boolean` / `object` / `T[]` / `Record<string, T>`；说明列可保留 Java 类型名
- 响应信封（如 `R`）只在模块头写一次；各接口表格只描述 `data` 里的内容
- 不得臆造业务字段；未解析类型标 `needs_source`，并只打开那一个文件（或查项目约定）
- 文档写成功后务必执行 `stamp_commit.mjs`（带上本次 `--docs`）
- **全程中文**（接口说明、鉴权摘要、字段说明、示例旁注）

## 脚本 vs 理解

| 脚本能做的 | 必须由 Agent 做人的 |
|------------|---------------------|
| `sync_status`：多项目 `action` 汇总、`allUpToDate` | 对 noop 项目直接跳过 |
| `list_endpoints`：METHOD、完整 path、servicePath、参数、鉴权、按 Controller 分组与 `docFile` | 接口说明、业务语义、鉴权可读摘要、示例 JSON |
| `resolve_types`：字段树、继承、`needs_source` | 前端友好表格；泛型/Map；解释字段含义 |
| `changed_since` / `stamp_commit`：变更范围、`docFiles`、同步提交号、`repoRel` | 判断改哪些 `<kebab>.md`；核对文档与源码意图 |
| `verify_docs`：MD 中 `METHOD path` 与脚本集合比对 | `ok:false` 时按 diff 改 MD，禁止靠感觉改 path |
| `ensure_conventions`：生成唯一 `_conventions.md` | 保持 `project-conventions.md` 为规范源 |
| `push_docs`：镜像推送 `<out>/**/*.md` 到产品项目文档 | 确认 `--slug`、PAT scope 与项目 ACL |

**脚本边界**：列接口路径、文档文件名、类型字段、变更范围、校验已写 path；网关前缀来自手维 JSON 查找。**不再**解析 Spring Gateway YAML，也不解释 StripPrefix / RewritePath 等网关细节。

**禁止**：把脚本 JSON 原样堆进 Markdown；**禁止**把全量 `list_endpoints` 输出整包读入对话；禁止只抄注解源码当「文档」；禁止手算网关前缀；禁止在 `action=noop` 时继续生成；禁止跳过 `verify_docs` 直接 stamp。

打开源码范围：脚本列出的 Controller / DTO，以及为理解行为所必需的少量 Service。

## 文档写作规范

### 禁止

- **只点名类型、不展开字段**：禁止「返回 `UserInfo` 对象」「`body` 为 `XxxDTO`」这类空话却不写字段表。读者必须能在**同一文档**里看到每个对象字段（名 / 类型 / 说明）
- 斜体占位字段名（描述性伪字段）：如 `_(继承 BaseDTO)_`、`_(见约定)_` 等——不得用斜体占位代替真实字段或省略展开
- 把 `@PreAuthorize(...)` / SpEL 原文塞进文档
- 把 `_extends_*`、`needs_source` 当表格「字段名」粘贴
- 臆造网关 Path 前缀（必须以 `list_endpoints` 的 `gateway` / `path` 为准）
- **改写 path 段的单复数或拼写**（即使 `@Tag(description="user")`、示例旧文、或英语习惯暗示单数）：必须以脚本 `path` 字符级一致；写完用 `verify_docs` 门禁

**例外（必须用）**：整段请求体 / 信封 `data` 不是对象（标量、数组等）时，表格字段名分别写作 `_(body)_`、`_(data)_`，类型写实际类型（如 `boolean`、`string[]`）。这是约定字段名，不是禁止的斜体占位。

### 鉴权

写成可读摘要，例如：`需要登录；权限 {moduleCode}:create`。脚本提供 `authSummary`（可贴改）与原始 `auth`（仅供核对，勿贴正文）。

类级 `@SecurityRequirement(name = HttpHeaders.AUTHORIZATION)` 等 OpenAPI 安全声明会被识别为「需要登录」（无具体权限码时）；与 `@HasPermission` / `@PreAuthorize` 并存时合并摘要，**勿**把 OpenAPI 原文堆进文档。

### 类型与表格

- **每个对象必须有字段表**：请求体 / 响应 `data` / 嵌套对象（如 `records[]` 单条）均须先 `resolve_types`，再写成 Markdown 表。Java 类型名可写在说明或小节标题里，**不能代替**字段表
- **同一 DTO 多处复用**：可在文末（或「通用说明」后）设 `## 数据模型`，把 `UserInfo` 等定义**写全一次**；各接口响应处写「见 [UserInfo](#userinfo)」并仍可用一行类型提示——但定义本身必须在本文内可点开，禁止只留裸类型名
- 字段名：真实 JSON 名；整段 body/data 为标量/数组等非对象时，字段名用 `_(body)_` / `_(data)_`
- 类型：`string` / `number` / `boolean` / `object` / `T[]` / `Record<string, T>`
- 继承：合并父类字段，或写「另含父类字段，见 [API 约定](../_conventions.md)」
- `Map<String, Object>` → `Record<string, object>`，并说明 key/value
- `R<T>`：模块头写信封；接口响应表只写 `data` 内容
- 分页：按约定展开；无源码时写「见 [API 约定](../_conventions.md) · 分页」
- `needs_source`：只打开那一个源文件补字段；仍解析不出则标注「未解析」并列出已知字段，禁止用一句话搪塞

### Path（网关 + 服务）

文档代码块里的 path **必须**用 `list_endpoints` 返回的 `path`（字符级一致）：

- `servicePath`：Controller/方法映射（服务内）
- `path`：`gatewayPrefix + servicePath`（对外完整路径）
- `gateway.matched === false`：用 `servicePath`，并在模块头或接口旁写「⚠ 网关前缀未匹配」+ 简述 `gateway.warning`；**不要**自行猜测 `/admin`、`/common-resource` 等
- 写完跑 `verify_docs.mjs`：`ok: false` 时按 `missingInDocs` / `extraInDocs` 改 MD，禁止 stamp

### 示例

成功示例须与响应表一致（信封 + `data`）；值要像真实业务。

完整章节结构见 [references/template.md](references/template.md)；成品示例见 [references/example.md](references/example.md)。

## 输出目录

### `--out`（产出根）

- **默认**：工作区根下 `api-docs`；若已存在 `output/` / `api-docs/` / `docs/` 下的 `<project>/.sync.json`，未传 `--out` 时**自动复用**已有目录（避免误开空目录导致假全量）
- 可覆盖：`--out /abs/path` 或 `--out other-dir`

### 布局（按 Controller 拆分）

```text
api-docs/
  _conventions.md                 # 唯一约定页（ensure_conventions 生成）
  <project>/
    .sync.json                    # baseCommit + docs[]
    file.md                       # FileController
    sys-user.md                   # SysUserController
    table-info.md                 # TableInfoController
```

**文件名规则**：去掉类名末尾 `Controller`，再转 kebab-case：

| 类名 | 文件 |
|------|------|
| `FileController` | `file.md` |
| `SysUserController` | `sys-user.md` |
| `OAuth2ClientController` | `oauth2-client.md` |
| `DynModuleListConfigController` | `dyn-module-list-config.md` |

**`<project>` 优先级**：`--project` → 根 `pom.xml` 的 `<artifactId>`（跳过 parent）→ 仓库目录名。

### 示例

```text
api-docs/
  _conventions.md
  ic-common-resource/
    .sync.json
    common.md
    table-info.md
  ic-upms-biz/
    .sync.json
    sys-user.md
    file.md
```

旧布局若仍有单一的 `<project>.md`：全量重生成时拆成多个 `<kebab>.md`，并删除旧单文件；`changed_since` 会提示 `legacySingleDoc`。

## 网关前缀（`ic-gateway-dev.json`）

配置文件：[references/ic-gateway-dev.json](references/ic-gateway-dev.json)（手维；增补路由时直接改 JSON，不再维护 Spring Gateway YAML 副本）。

### 如何使用

1. `list_endpoints` 用 `JSON.parse` 读配置，按 `routes[].service` / `routes[].id` 匹配服务名。
2. 匹配候选（脚本自动收集，**按此顺序**试配）：`--service` → `--project` → `srcRoot` 的 `spring.application.name` → `repoRoot` 的同名字段（仅当不歧义）。多模块单体仓若 `repoRoot` 下扫到多个不同 name，**不会**静默取第一个——请 `--repo-root` 指到具体模块，或传 `--service` / `--project`。
3. **完整 path** = `join(prefix, servicePath)`，其中 `prefix` 来自命中路由的 `prefix` 字段。

### 找不到前缀时

- `gateway.matched = false`，`gateway.warning` 说明原因
- 接口 `path` 回退为 `servicePath`
- 文档必须标明「⚠ 网关前缀未匹配」，**禁止**静默拼一个看起来像的前缀
- 可把新路由补进 `references/ic-gateway-dev.json` 后再跑脚本

可覆盖网关文件：`list_endpoints … --gateway-json /path/to.json`（或 `--gateway`）。

## 脚本

路径相对本技能根目录。从**工作区根**执行，以便发现已有 `output/` / `api-docs/`。

```bash
node scripts/ensure_conventions.mjs [--out <dir>] [--dry-run]
node scripts/sync_status.mjs [--out <dir>] [--workspace <dir>]
node scripts/list_endpoints.mjs <srcRoot> [--files a.java,b.java] [--project name] [--service name] [--repo-root dir] [--gateway-json path]
node scripts/resolve_types.mjs <srcRoot> TypeA,TypeB
node scripts/changed_since.mjs <repoRoot> [baseCommit] [--out <dir>] [--project <name>] [--workspace <dir>]
node scripts/verify_docs.mjs <srcRoot> [--out <dir>] [--project <name>] [--service name] [--repo-root dir] [--docs a.md,b.md] [--files a.java,b.java]
node scripts/stamp_commit.mjs <repoRoot> [--out <dir>] [--project <name>] [--workspace <dir>] [--docs a.md,b.md] [--gateway-prefix /x] [--gateway-service name] [--dry-run]
node scripts/push_docs.mjs --slug <项目标识> [--out <dir>] [--env-file <path>] [--dry-run]
```

| 脚本 | 标准输出 |
|------|----------|
| `ensure_conventions.mjs` | 写入（或 `--dry-run` 预览）`<out>/_conventions.md` |
| `sync_status.mjs` | 多项目汇总：`allUpToDate`、`summary.noop|update_docs|full_scan`、每项 `action` |
| `list_endpoints.mjs` | 接口 JSON：`path`/`servicePath`/`docFile`、`controllers[]`、`gateway` |
| `resolve_types.mjs` | 字段树；未解析 → `needs_source`；父类无源码 → `extendsUnresolved` |
| `changed_since.mjs` | `{ action, mode, upToDate, files[], controllers[], docFiles[], agentHint, … }` |
| `verify_docs.mjs` | `{ ok, missingInDocs[], extraInDocs[], missingDocs[], unresolvedTypes[], … }`；失败退出码 1 |
| `stamp_commit.mjs` | 已写入（或预览）的 `.sync.json`（含 `repoRel`） |
| `push_docs.mjs` | `{ pushed[], failed[], dryRun, summary }`；失败非 0 退出 |

| 参数 | 脚本 | 含义 |
|------|------|------|
| `--out <dir>` | `ensure_conventions` / `sync_status` / `changed_since` / `stamp_commit` / `push_docs` | 产出根；省略时自动发现已有 sync，否则默认 `api-docs` |
| `--project <name>` | `changed_since` / `stamp_commit` / `list_endpoints` | 项目子目录名；并参与网关匹配 |
| `--workspace <dir>` | `sync_status` / `changed_since` / `stamp_commit` | 工作区根（默认 cwd）；用于 `repoRel` 与邻仓探测 |
| `--service` / `--gateway-json` / `--repo-root` | `list_endpoints` | 服务名、网关 JSON、读 application.yml 的仓根 |
| `--docs a.md,b.md` | `stamp_commit` / `verify_docs` | 写入 `.sync.json` 的 `docs` 列表；或只校验这些文档 |
| `--files a,b` | `list_endpoints` / `verify_docs` | 只扫这些文件 |
| `[baseCommit]` | `changed_since` | 覆盖 `.sync.json` 中的上次提交 |
| `--slug <id>` | `push_docs` | Bedrock 产品项目标识（路径参数；可为数字 ID 或 slug） |
| `--env-file <path>` | `push_docs` | 显式 `.env`；否则按 `$BEDROCK_AGENT_ENV_FILE` → `$BEDROCK_AGENT_WORKDIR/.env` → `./.env` |

### 推送到产品项目（`push_docs`）

把 `<out>` 下全部 `.md`（跳过隐藏目录；**不推** `.sync.json`）按相对路径镜像到开放 API：

- `ic-upms-biz/sys-user.md` → `api_dir=ic-upms-biz` + `api_doc_name=sys-user.md`
- `_conventions.md` → `api_dir=`（根）+ `api_doc_name=_conventions.md`

**必需环境变量**（智能体管理里配置，平台写入工作区 `.env`；勿写进技能目录）：

| 变量 | 说明 |
|------|------|
| `PAT` | 访问令牌；至少 `docs:write` |
| `BEDROCK_HOST` | 服务根地址，**无尾斜杠**（请求 `{host}/api/v1/projects/{slug}/docs/push`） |

PAT 还须满足目标项目的**成员 ACL**。文件中的键**不覆盖**已存在的 `process.env`（便于本机调试）。

## 工作流

1. **读规范** — 只读 [references/project-conventions.md](references/project-conventions.md)。若几乎为空，用本文「文档写作规范」占位（信封 `code`/`msg`/`data`、成功码 `0`），并提醒用户补全该文件。
2. **确认产出根** — 若工作区已有文档目录（常见 `output/`），后续命令统一 `--out` 该目录（或不传，让脚本自动发现）。
3. **生成约定页** — `ensure_conventions.mjs`（写出唯一 `<out>/_conventions.md`）。若仅增量且约定页已存在，可跳过。
4. **查变更（门禁）**
   - **多仓 / 「更新所有文档」**：`sync_status.mjs [--out …]`
     - `allUpToDate: true` → **整任务结束**
     - 否则只处理 `summary` 里非 `noop` 的项目
   - **单项目**：`changed_since.mjs <repoRoot> --project …`
     - `action: "noop"` → **结束该项目**
     - `action: "wrong_repo"` → 换 `suggestedRepoRoot` 重跑
     - `action: "update_docs"` → `list_endpoints … --files …`，只改 `docFiles`
     - `action: "full_scan"` → 全量 `list_endpoints`（不带 `--files`）
5. **解析类型** — 仅对要写的接口：`resolve_types.mjs <api-srcRoot> TypeA,TypeB`（api 模块 srcRoot，见「双 srcRoot」）。**写 md 前必须已拿到字段树**；未跑 `resolve_types` 不得写对象响应/请求体。
6. **写文档** — 每个 Controller 写到 `<out>/<project>/<docFile>`（如 `sys-user.md`），模块头链接 `../_conventions.md`；path **字符级**用脚本的完整 `path`（禁止改单复数）。对象字段必须落表（或落 `## 数据模型` 再锚点引用），禁止「返回 `Xxx`」空话。
7. **校验 path** — `verify_docs.mjs <srcRoot> --project … --repo-root <模块目录> [--docs …]`；`ok: false` → 按 `missingInDocs` / `extraInDocs` 改 MD 后重跑，**禁止** stamp。
8. **更新同步记录** — `stamp_commit.mjs … --docs common.md,table-info.md`（可加 `--gateway-prefix` / `--gateway-service`）；可用 `--dry-run` 核对。会写入 `repoRel`。
9. **核对** — `METHOD path` 唯一且已通过 `verify_docs`；每个对象请求/响应都能看到字段表（无「只提类型名」）；无描述性斜体占位（`_(继承 …)_` 等）；非对象 body/data 已用 `_(body)_`/`_(data)_`；鉴权为人话；表格与源码一致；约定页存在且被引用；`.sync.json` 为当前 HEAD 且 `docs` 齐全；网关未匹配处已标注。
10. **推送到产品项目文档**（可选）— `push_docs.mjs --slug <项目标识> [--out …] [--dry-run]`。需工作区 `.env`（或 `--env-file`）含 `PAT`、`BEDROCK_HOST`；确认 scope / ACL。

### `.sync.json`

```json
{
  "baseCommit": "<完整 sha>",
  "updatedAt": "yyyy-MM-dd",
  "project": "ic-common-resource",
  "docs": ["common.md", "table-info.md"],
  "layout": "per-controller",
  "repoRel": "repo-2",
  "gatewayPrefix": "/common-resource",
  "gatewayService": "common-resource",
  "module": "ic-common-resource"
}
```

- `docs`：该项目已维护的控制器文档文件名（增量时合并更新）
- `repoRel`：相对工作区的 git 仓库路径（`stamp` 写入；多仓时供 `sync_status` / `changed_since` 选对仓）
- `gatewayPrefix` / `gatewayService`：可选，便于下次核对
- 旧字段 `docFile` 仅兼容；新流程以 `docs` + `layout: "per-controller"` 为准

需全量扫描：该项目无 `.sync.json`、`baseCommit` 在**正确**仓库中不存在、非 git 仓库、仍为旧单文件布局，或用户明确要求全量。  
**注意**：`baseCommit` 在「另一个」仓里存在时返回 `wrong_repo`，不是全量。

## 检查清单

```
- [ ] 已读 references/project-conventions.md（未把 _conventions.md 当输入）
- [ ] 已确认 --out（与已有文档目录一致；或让脚本自动发现）
- [ ] 多仓已跑 sync_status；单仓已跑 changed_since；已按 action 分支（noop 已跳过）
- [ ] 未在 action=noop / allUpToDate 时继续 list_endpoints 或写 md
- [ ] 需要写入时已跑 list_endpoints；核对 gateway.matched；已 resolve_types 并读过源码
- [ ] 每个 Controller 一个 <kebab>.md；path 字符级来自脚本；未匹配已标注；未改单复数
- [ ] 已跑 verify_docs 且 ok:true（失败已按 missing/extra/unresolvedTypes 修正）
- [ ] 说明与示例是中文人话；无描述性斜体占位；非对象 body/data 用 `_(body)_`/`_(data)_`；鉴权为可读摘要
- [ ] 每个对象请求/响应已展开字段表（或本文 `## 数据模型` 锚点）；无「返回 `Xxx` 对象」却无定义
- [ ] Markdown 在 <out>/<project>/，模块头链接 ../_conventions.md
- [ ] 已 stamp（含 --docs）当前提交到该项目 .sync.json（含 repoRel）
- [ ] METHOD+path 唯一；无臆造字段 / 无臆造网关前缀
- [ ] 若需入库：已 push_docs（--slug；PAT/BEDROCK_HOST）；failed 为空
```
