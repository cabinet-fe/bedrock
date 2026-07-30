# 项目约定（IC 平台）

> Agent 生成 API 文档前**只读本文件**作为规范输入。不要把产出目录里的 `_conventions.md` 当作输入（那是技能生成给读者的约定页）。

## 1. 统一响应信封

- 类型名：`R<T>`
- 字段：`code`（int）、`msg`（string）、`data`（T）；`ok` 仅只读序列化，示例可不写
- 成功码：`0`；失败码：`1`
- 鉴权失败时 `code` 可能为 HTTP `401` / `403`

示例：

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

## 2. 模块布局（api vs biz）

- **Controller** 在 `*-biz/.../src/main/java`；`list_endpoints` / `verify_docs` 对该 srcRoot 扫描
- **DTO / Entity / VO** 多在 `*-api/.../src/main/java`；`resolve_types` 应对 api 模块 srcRoot
- 同一单体仓内 biz 与 api 为不同 srcRoot，勿混用

## 3. 共享基类 / 公共字段

继承链：`BaseDTO` → `BaseBusinessEntity` → `BaseEntity`。

### `BaseEntity`（审计 / 租户 / 逻辑删除）

| 字段       | 类型   | 说明                       |
| ---------- | ------ | -------------------------- |
| creatorId  | string | 创建者 ID                  |
| createBy   | string | 创建者                     |
| createTime | string | 创建时间（见 §8 日期格式） |
| updateBy   | string | 更新者                     |
| updateTime | string | 更新时间                   |
| tenantId   | string | 租户 ID                    |
| deleted    | number | 逻辑删除标志               |

### `BaseBusinessEntity`（业务单据）

| 字段              | 类型   | 说明                                |
| ----------------- | ------ | ----------------------------------- |
| orgCode           | string | 组织代码                            |
| invoiceSerial     | string | 单据编号                            |
| moduleCode        | string | 表单/模块编码                       |
| processInstanceId | string | 流程实例 ID（JSON 为字符串，见 §8） |
| reviewStatus      | string | 审批状态                            |
| text1…text5       | string | 预留文本（null 不输出）             |
| date1…date5       | string | 预留日期（null 不输出）             |
| number1…number5   | number | 预留数值（null 不输出）             |

### `BaseDTO`

在业务基类之上增加 `_shared`：

| 字段        | 类型                     | 说明                                |
| ----------- | ------------------------ | ----------------------------------- |
| moduleCode  | string                   | 模块编码                            |
| taskUser    | Record\<string, string\> | 任务用户                            |
| action      | string                   | 操作：`DRAFT` / `SAVE` / `SUBMIT`   |
| orgCode     | string                   | 组织代码                            |
| attachments | object[]                 | 附件分组：`groupId`、`categories[]` |

`attachments[].categories[]`：`categoryId`（string）、`fileIds`（string[]）。

写文档时：子类表格合并上述字段；勿写 `_(继承 BaseDTO)_`。业务仓 DTO 若解析为 `extendsUnresolved`，直接按本节展开。

### `ConditionVO`

| 字段  | 类型   | 说明     |
| ----- | ------ | -------- |
| value | any    | 字段值   |
| logic | string | 连接类型 |
| type  | string | 字段类型 |

## 4. 分页与查询条件

- 平台**并存**两种分页包装：
  - **`IcPage<T>`**（常见）：`current`、`size`；响应 `records`、`total`；常带 `moduleCode`、`condition`、`sort`
  - **MyBatis `Page<T>`**（`com.baomidou.mybatisplus.extension.plugins.pagination.Page`）：按方法签名使用，勿统一猜
- **文档跟方法签名**：Controller 方法参数/返回是什么分页类型，文档就写什么；禁止一律写成 `IcPage`
- 常见 `IcPage` 字段：
  - `moduleCode`：模块编码（鉴权常用）
  - `condition`：`Record<string, ConditionVO>`，key 为字段名
  - `sort`：`Record<string, string>`
  - `query`：服务端用，**前端请求体不要写**
- `ConditionVO`：`value`（任意）、`logic`（连接类型字符串）、`type`（`default` / `date`，不是枚举名）

### HTTP METHOD 与 `@RequestBody`

- 平台存在 **`GET` + `@RequestBody`**（如 SysArea 分页）；文档 **METHOD 必须跟 `@GetMapping` / `@PostMapping` 等注解**，禁止按 REST 习惯改成 POST

## 5. 认证与权限

- 认证：OAuth2 JWT；请求头 `Authorization: Bearer {token}`
- 两种常见写法（可并存）：
  1. **类级 `@SecurityRequirement(name = HttpHeaders.AUTHORIZATION)`**（OpenAPI）：表示需登录，通常无具体权限码 → 文档写 `需要登录`
  2. **`@HasPermission` / `@PreAuthorize` / `@Secured`**：权限码 `{resource}:{action}`（如 `tableInfo:view`），动态模块常用 `{moduleCode}:create|view|update|delete`
- 文档写法：`需要登录；权限 tableInfo:view` / `需要登录；权限 {moduleCode}:create`——**不要**贴 SpEL / OpenAPI 原文
- 方法标 `@AnonymousAccess` 时可匿名；默认需认证

## 6. 网关与服务

| 服务     | 应用名（`spring.application.name`） | 本地端口 | 网关对外前缀（见 `ic-gateway-dev.json`） |
| -------- | ----------------------------------- | -------- | ---------------------------------------- |
| 网关     | `ic-gateway`                        | 3000     | —                                        |
| 认证     | `ic-auth`                           | 3001     | `/auth`                                  |
| UPMS     | `ic-upms-biz`                       | 3002     | `/admin`                                 |
| 通用资源 | `common-resource`                   | 3014     | `/common-resource`                       |
| 工作流   | `workflow`                          | 8083     | `/workflow`                              |
| 监控     | `ic-monitor`                        | —        | `/monitor`                               |
| 合同     | `ic-contract`                       | —        | `/contract`                              |

### 路径规则（以手维网关 JSON 为准）

技能内 [ic-gateway-dev.json](ic-gateway-dev.json)：

1. 每条路由含 `service` / `id` / `prefix`；脚本按服务名（及 `ic-` / `-biz` 变体）匹配后取 `prefix`。
2. **完整对外 path** = 网关前缀 + Controller/方法映射（服务内 path）。
   例：服务内 `/tableInfo/schema/list` → 对外 `/common-resource/tableInfo/schema/list`。
3. **path 字符级照抄**：源码映射若有拼写错误（如 `/busienss-files`），文档须原样保留，**禁止「修正」**。

`list_endpoints.mjs` 读取该 JSON 并写入每条接口的 `path` / `servicePath` / `gateway*`。**Agent 禁止手算前缀**；若 `gateway.matched=false`，文档使用 `servicePath` 并醒目标记「网关前缀未匹配」。写完后用 `verify_docs.mjs` 比对 MD 中的 `METHOD path` 与脚本结果，未通过禁止 stamp。

服务名匹配顺序：`--service` → `--project` → 模块自身 `spring.application.name`；多模块仓根若出现多个不同 name 则忽略（勿静默取第一个）。

- 服务侧一般无 `context-path`（路径即 Controller 映射）
- 若增补了新路由，同步更新本表与技能内 `references/ic-gateway-dev.json`（手改 JSON，不再维护 Gateway YAML 副本）

### 扫描范围外

- **OAuth2 / Spring Authorization Server** 等非 `@RestController` 配置端点不在本技能扫描范围（如 token 端点由授权服务器配置类提供）

## 7. 枚举、日期与其它

- 日期时间：`yyyy-MM-dd HH:mm:ss`；日期 `yyyy-MM-dd`；时间 `HH:mm:ss`；时区 `Asia/Shanghai`
- **`Long` / `long`：JSON 序列化为字符串**——表格类型写 `string`（或注明 JSON 字符串）；示例里大整数 ID 用 `"1888…"`。若脚本标为 `number`，**以本约定为准**写 `string`
- 枚举：默认按枚举常量名；`ConditionVO.type` 传 code 字符串（`default` / `date`）

## 8. 文档产出路径（技能侧）

- `--out` 默认：工作区根下 `api-docs`（可用参数覆盖）
- `--project`：优先用户指定；否则根 `pom.xml` 的 `<artifactId>`；再否则目录名
- **按 Controller 拆分**：`<out>/<project>/<kebab>.md`
  - 类名去掉 `Controller` 后缀再转 kebab-case：`FileController` → `file.md`；`SysUserController` → `sys-user.md`；`OAuth2ClientController` → `oauth2-client.md`
- 约定页：运行 `ensure_conventions.mjs` 生成唯一的 `<out>/_conventions.md`；接口文档用 `../_conventions.md` 引用
- 同步：`<out>/<project>/.sync.json` 的 `docs` 为该项目已生成的 md 文件名列表；`baseCommit` 驱动增量
