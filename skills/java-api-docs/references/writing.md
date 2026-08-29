# 文档写作规范

写 md **时**再读本文件。开工不要读。脚本只列 path / 字段；说明、语义、示例必须结合 Controller / Service / DTO 用中文写清。

完整章节结构见 [template.md](template.md)；成品见 [example.md](example.md)。

## 禁止

- **只点名类型、不展开字段**：禁止「返回 `UserInfo` 对象」「`body` 为 `XxxDTO`」却无字段表。读者必须在**同一文档**看到每个对象字段（名 / 类型 / 说明）
- 斜体占位字段名：如 `_(继承 BaseDTO)_`、`_(见约定)_` ——不得代替真实字段
- 把 `@PreAuthorize(...)` / SpEL 原文塞进文档
- 把 `_extends_*`、`needs_source` 当表格「字段名」粘贴
- 臆造网关 Path 前缀（必须以 `list_endpoints` 的 `gateway` / `path` 为准）
- **改写 path 段的单复数或拼写**（即使 `@Tag`、旧文、英语习惯暗示单数）：必须与脚本 `path` 字符级一致；写完用 `verify_docs` 门禁
- 把脚本 JSON 原样堆进 Markdown；禁止只抄注解源码当「文档」

**例外（必须用）**：整段请求体 / 信封 `data` 不是对象（标量、数组等）时，表格字段名分别写作 `` `_(body)_` ``、`` `_(data)_` ``（**必须带反引号**，否则 `_…_` 被吃成斜体），类型写实际类型（如 `boolean`、`string[]`）。

## 鉴权

写成可读摘要，例如：`需要登录；权限 {moduleCode}:create`。脚本提供 `authSummary`（可贴改）与原始 `auth`（仅供核对，勿贴正文）。

类级 `@SecurityRequirement(name = HttpHeaders.AUTHORIZATION)` 等 OpenAPI 安全声明会被识别为「需要登录」（无具体权限码时）；与 `@HasPermission` / `@PreAuthorize` 并存时合并摘要，**勿**把 OpenAPI 原文堆进文档。

## 类型与表格

- **每个对象必须有字段表**：请求体 / 响应 `data` / 嵌套对象（如 `records[]` 单条）均须先 `resolve_types`，再写成 Markdown 表。Java 类型名可写在说明或小节标题里，**不能代替**字段表
- **同一 DTO 多处复用**：可在文末设 `## 数据模型`，把定义**写全一次**；各接口响应处写「见 [UserInfo](#userinfo)」——定义必须在本文内可点开
- 字段名：真实 JSON 名；整段 body/data 为标量/数组等非对象时，字段名用 `` `_(body)_` `` / `` `_(data)_` ``
- 类型：`string` / `number` / `boolean` / `object` / `T[]` / `Record<string, T>`；说明列可保留 Java 类型名
- 继承：合并父类字段（`resolve_types` 会对 `BaseDTO` / `BaseBusinessEntity` / `BaseEntity` / `BaseShared` 注入约定字段）；勿写 `_(继承 …)_`
- **`_shared`（嵌套，非顶层散字段）**：`BaseDTO` 子类请求体必须有字段名 `` `_shared` ``（类型 `object`），其子字段为 `moduleCode` / `action` / `orgCode` / `taskUser` / `attachments`。**禁止摊平**；保存类示例必须含 `"_shared": { ... }`。表格里字段名建议一律反引号包裹
- `Map<String, Object>` → `Record<string, object>`，并说明 key/value
- `R<T>`：模块头写信封；接口响应表只写 `data` 内容
- 分页：按约定展开；无源码时写「见 [API 约定](../_conventions.md) · 分页」
- `needs_source`：只打开那一个源文件补字段；仍解析不出则标注「未解析」并列出已知字段
- 不得臆造业务字段

## 必填列（请求侧）

路径参数 / 查询参数 / 请求体字段表必须有「必填」列（写 `是` / `否`）。**响应体与数据模型表不写必填列**。

1. **路径 / 查询参数**：跟 `list_endpoints` 每条 param 的 `required`。
2. **请求体字段**：跟 `resolve_types` 的 `required` + `requiredSource`：
   - `validation`：`@NotNull` / `@NotBlank` / `@NotEmpty` → `是`；`@Nullable` → `否`
   - `schema`：`@Schema(required=true)` 或 `requiredMode=REQUIRED` → `是`；`required=false` / `NOT_REQUIRED` → `否`
   - `platform`：公共基类约定（如 `_shared.moduleCode`/`action`/`orgCode` 为 `是`；审计字段多为 `否`）
   - `default`：**脚本无注解信号**——不得直接抄成全员「否」。结合 Controller/Service 校验、创建 vs 更新语义判断；仍无法判断时写 `否`，说明标注「源码无校验注解」。
3. **整段 body 非对象**（`` `_(body)_` ``）：必填写 `是`。

## Path（网关 + 服务）

文档代码块里的 path **必须**用 `list_endpoints` 返回的 `path`（字符级一致）：

- `servicePath`：Controller/方法映射（服务内）
- `path`：`gatewayPrefix + servicePath`（对外完整路径）
- `gateway.matched === false`：用 `servicePath`，并写「⚠ 网关前缀未匹配」+ `gateway.warning`；**不要**自行猜测 `/admin`、`/common-resource` 等
- 写完跑 `verify_docs.mjs`：`ok: false` 时按 `missingInDocs` / `extraInDocs` 改 MD，禁止 stamp

网关前缀只读 [ic-gateway-dev.json](ic-gateway-dev.json)，由 `list_endpoints` 按 service/id 查找。禁止手算。找不到时 `gateway.matched=false`，path 回退为 `servicePath`。

## 示例

成功示例须与响应表一致（信封 + `data`）；值要像真实业务。全程中文（接口说明、鉴权摘要、字段说明、示例旁注）。
