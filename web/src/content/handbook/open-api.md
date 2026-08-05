# 开放接口（PAT）

面向脚本与外部系统的开放接口：通过**个人访问令牌（PAT）**调用。Web 控制台使用的完整接口集不属于本页范围（契约真源见仓库 `api/` 目录）。

**Base URL**：`{host}/api/v1`（下文路径均省略该前缀）
**数据格式**：JSON，字段统一 `snake_case`

---

## 1. 个人访问令牌（PAT）

- 形态：`br_` 前缀 + hex；服务端存 SHA-256 哈希（鉴权）与 AES-GCM 密文（属主 reveal 取密文，前端用与登录相同密钥解密后复制）；明文不落日志。
- 使用：`Authorization: Bearer br_...`，与登录 JWT 分流校验；PAT 以属主用户身份生效。
- scope 白名单（创建时多选）：`skills:read`、`agents:run`、`docs:read`、`docs:write`、`dev_docs:read`、`dev_docs:write`。每个 scope 映射固定的开放端点（见「3. 开放接口一览」），scope 不足返回 `403 token scope insufficient`。
- 有效期三选一：`expires_in_days`（仅 `30|90|180|365`）、`expires_at`（UTC 绝对时间，须晚于当前，与 `expires_in_days` 互斥）、都不传 = 永不过期。
- 吊销：删除令牌即吊销；元数据中的 `last_used_at` 记录最近使用时间。
- 历史仅哈希、无密文的令牌无法再复制，需删除后重建。
- **不替代 HTTPS/TLS**：生产环境务必经 HTTPS 调用，否则令牌可能被窃听。

## 2. 获取 PAT

页面：资源管理 → 访问令牌 → 创建 / 列表复制。

也可通过 API 管理（登录 JWT 鉴权，适合自动化）：

| 方法   | 路径                           | 权限                     | 说明                                                   |
| ------ | ------------------------------ | ------------------------ | ------------------------------------------------------ |
| GET    | `/resource/tokens`             | `resource_tokens:view`   | 列出本人令牌（分页元数据，含 `copyable`）              |
| POST   | `/resource/tokens`             | `resource_tokens:create` | 创建，201；`data.token` 明文，`data.metadata` 为元数据 |
| GET    | `/resource/tokens/{id}/reveal` | `resource_tokens:view`   | 返回 AES-GCM 密文；客户端解密后复制；无密文时 422      |
| DELETE | `/resource/tokens/{id}`        | `resource_tokens:delete` | 删除（吊销）                                           |

```bash
# 登录换取 JWT（脚本调试可用明文 password；Web 端只发 password_cipher）
curl -fsS -X POST "$HOST/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'
# → 取响应 data.access_token

# 创建 PAT（有效期 90 天，可触发 Agent 运行）
curl -fsS -X POST "$HOST/api/v1/resource/tokens" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-bot","scopes":["agents:run"],"expires_in_days":90}'
# → 响应 data.token（br_...）；之后可用 reveal 取密文并由客户端解密

# 获取已有 PAT 密文（Web 端解密后复制；脚本更宜创建时保存 data.token）
curl -fsS "$HOST/api/v1/resource/tokens/1/reveal" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
# → 响应 data.token_cipher（hex）
```

## 3. 开放接口一览

| scope            | 方法 | 路径                             | 说明                                                   |
| ---------------- | ---- | -------------------------------- | ------------------------------------------------------ |
| `agents:run`     | POST | `/ai/agents/{id}/api-runs`       | 触发 Agent 运行（202 异步）                            |
| `skills:read`    | GET  | `/skills/{id}/package`           | 下载技能包（二进制 ZIP）                               |
| `docs:write`     | POST | `/projects/{id}/docs/push`       | 按路径 upsert 接口文档；`{id}` 可为数字 ID 或项目 slug |
| `docs:read`      | GET  | `/projects/{id}/docs/pull`       | 按路径读取单篇接口文档；`{id}` 可为数字 ID 或项目 slug |
| `docs:read`      | GET  | `/projects/{id}/docs/export`     | 按目录导出接口文档列表（全量同步）；`{id}` 同 push     |
| `dev_docs:write` | POST | `/projects/{id}/dev-docs/push`   | 按路径 upsert 开发文档；`{id}` 可为数字 ID 或项目 slug |
| `dev_docs:read`  | GET  | `/projects/{id}/dev-docs/pull`   | 按路径读取单篇开发文档；`{id}` 可为数字 ID 或项目 slug |
| `dev_docs:read`  | GET  | `/projects/{id}/dev-docs/export` | 按目录导出开发文档列表（全量同步）；`{id}` 同 push     |

以上接口也接受登录 JWT（此时校验 RBAC 权限而非 scope）：`api-runs` 需 `ai_agents:execute`，`package` 需 `ai_skills:download`，接口文档 `push` 需 `project_docs:create`，`pull` / `export` 需 `project_docs:view`；开发文档对应 `project_dev_docs:*`；项目文档接口另要求项目 ACL。

## 4. 通用约定

- **响应信封**：`{ "code": 0, "message": "success", "data": {} }`；成功 `code=0`，失败 `code` 等于 HTTP 状态码且附带 `request_id`。二进制下载（技能包）不经过信封。
- **异步触发**：返回 `202`，`data` 为运行记录（含 `id` 与当前 `status`）。
- **错误码**：`400` 参数无效 / `401` PAT 无效或过期 / `403` scope 不足或无权限 / `404` 资源不存在 / `422` 语义校验失败。

---

## 5. 接口详情

### 5.1 触发 Agent 运行 — scope `agents:run`

`POST /ai/agents/{id}/api-runs` — 可选请求体 `{ "user_prompt": "..." }`（可空或省略）。

- 响应 `202`：`data` 为 AgentRun（`id`、`agent_id`、`trigger_type`、`status`、`user_prompt` 等）。
- 错误：`400` Agent 未启用或工作区非 `ready`；`401` PAT 无效；`403` scope 不足；`404` Agent 不存在。
- 运行直接在 Agent 持久根工作区执行，环境注入 `BEDROCK_AGENT_WORKDIR` 与 `BEDROCK_AGENT_OUTPUT`（固定产出目录）；Run 无专属工作区目录。成功且产出非空时平台快照归档 zip，可通过 `GET /ai/runs/{id}/artifact`（需 `ai_runs:view`）下载。
- `user_prompt` 与智能体配置中的 `system_prompt` 一并作为 CLI 提示词；未传时仅使用系统提示词与工作区约束说明。
- 后续查询：`GET /ai/runs/{id}`（需 `ai_runs:view`）可取回状态、`output_text` 与可选 `artifact_path`。

```bash
curl -fsS -X POST "$HOST/api/v1/ai/agents/1/api-runs" \
  -H "Authorization: Bearer br_..." \
  -H "Content-Type: application/json" \
  -d '{"user_prompt":"总结仓库 README"}'
```

### 5.2 下载技能包 — scope `skills:read`

`GET /skills/{id}/package` — 响应为二进制 ZIP（非 JSON 信封）。适合 Skill 安装器拉取技能包。

```bash
curl -fsS -OJ "$HOST/api/v1/skills/3/package" \
  -H "Authorization: Bearer br_..."
```

### 5.3 按路径推送文档 — scope `docs:write`

`POST /projects/{id}/docs/push` — 按 `api_dir` + `api_doc_name` upsert 文档内容。路径参数 `{id}` 为正整数时按项目 ID；否则按 slug 解析（找不到 → 404）。

| 字段           | 必填 | 说明                                                                            |
| -------------- | ---- | ------------------------------------------------------------------------------- |
| `api_dir`      |      | 目录路径；空表示根目录；`/` 分隔；拒绝 `..`、绝对路径与空段；目录不存在自动创建 |
| `api_doc_name` | *    | 文档名；缺 `.md` 后缀时服务端补齐                                               |
| `api_doc`      | *    | Markdown 内容                                                                   |

- 响应：`201` 新建节点 / `200` 更新已有文档。
- 错误：`400` 参数无效；`403` scope 不足或不满足项目 ACL；`404` 项目不存在。

```bash
curl -fsS -X POST "$HOST/api/v1/projects/my-product/docs/push" \
  -H "Authorization: Bearer br_..." \
  -H 'Content-Type: application/json' \
  -d '{"api_dir":"guides","api_doc_name":"getting-started","api_doc":"# 快速上手\n..."}'
```

### 5.4 按路径读取文档 — scope `docs:read`

`GET /projects/{id}/docs/pull` — 按路径读取文档节点（含 `content`）。路径参数 `{id}` 规则同 push。单篇读取用本接口；全量同步用 export。

| 查询参数       | 必填 | 说明               |
| -------------- | ---- | ------------------ |
| `api_dir`      |      | 目录路径，规则同上 |
| `api_doc_name` | *    | 文档名             |

- 响应：`200`，`data` 为 ApiDocNode。
- 错误：`400` 缺 `api_doc_name`；`404` 路径不存在。

```bash
curl -fsS "$HOST/api/v1/projects/my-product/docs/pull?api_dir=guides&api_doc_name=getting-started" \
  -H "Authorization: Bearer br_..."
```

### 5.5 导出文档列表 — scope `docs:read`

`GET /projects/{id}/docs/export` — 一次返回扁平 `{ path, content }` 列表，供 sync-docs 全量对齐。路径参数 `{id}` 规则同 push。不新增 PAT scope（沿用 `docs:read`）。

| 查询参数  | 必填 | 说明                                                                     |
| --------- | ---- | ------------------------------------------------------------------------ |
| `api_dir` |      | 导出根目录；空表示项目根；规则同 push/pull。合法但目录不存在时返回空列表 |

- 响应：`200`，`data.items` 为数组；每项 `path` 相对导出根（含 `.md`），仅文档无目录行；按 `path` 字典序排序。
- 错误：`400` 非法 `api_dir`（如 `..`）；`403` scope 不足或不满足项目 ACL；`404` 项目不存在。

```bash
# 全量（项目根）
curl -fsS "$HOST/api/v1/projects/my-product/docs/export" \
  -H "Authorization: Bearer br_..."

# 子树：远程 openapi/controllers/User.md → path=controllers/User.md
curl -fsS "$HOST/api/v1/projects/my-product/docs/export?api_dir=openapi" \
  -H "Authorization: Bearer br_..."
```

### 5.6 按路径推送开发文档 — scope `dev_docs:write`

`POST /projects/{id}/dev-docs/push` — 按 `doc_dir` + `doc_name` upsert 开发文档内容。路径参数 `{id}` 规则同接口文档 push。无 AI generate。

| 字段       | 必填 | 说明                                                                            |
| ---------- | ---- | ------------------------------------------------------------------------------- |
| `doc_dir`  |      | 目录路径；空表示根目录；`/` 分隔；拒绝 `..`、绝对路径与空段；目录不存在自动创建 |
| `doc_name` | *    | 文档名；缺 `.md` 后缀时服务端补齐                                               |
| `content`  | *    | Markdown 内容                                                                   |

- 响应：`201` 新建节点 / `200` 更新已有文档。
- 错误：`400` 参数无效；`403` scope 不足或不满足项目 ACL；`404` 项目不存在。

```bash
curl -fsS -X POST "$HOST/api/v1/projects/my-product/dev-docs/push" \
  -H "Authorization: Bearer br_..." \
  -H 'Content-Type: application/json' \
  -d '{"doc_dir":"guides","doc_name":"architecture","content":"# 架构\n..."}'
```

### 5.7 按路径读取开发文档 — scope `dev_docs:read`

`GET /projects/{id}/dev-docs/pull` — 按路径读取开发文档节点（含 `content`）。

| 查询参数   | 必填 | 说明               |
| ---------- | ---- | ------------------ |
| `doc_dir`  |      | 目录路径，规则同上 |
| `doc_name` | *    | 文档名             |

```bash
curl -fsS "$HOST/api/v1/projects/my-product/dev-docs/pull?doc_dir=guides&doc_name=architecture" \
  -H "Authorization: Bearer br_..."
```

### 5.8 导出开发文档列表 — scope `dev_docs:read`

`GET /projects/{id}/dev-docs/export` — 一次返回扁平 `{ path, content }` 列表。

| 查询参数  | 必填 | 说明                                                                     |
| --------- | ---- | ------------------------------------------------------------------------ |
| `doc_dir` |      | 导出根目录；空表示项目根；规则同 push/pull。合法但目录不存在时返回空列表 |

```bash
curl -fsS "$HOST/api/v1/projects/my-product/dev-docs/export?doc_dir=guides" \
  -H "Authorization: Bearer br_..."
```

---

## 6. 其他免认证接口

以下接口不依赖用户会话，供外部系统 / 探活使用：

| 方法 | 路径                                    | 说明                                                                                                                                   |
| ---- | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| GET  | `/health`                               | 健康检查；`data` 含 `status`、`version`、`driver`                                                                                      |
| POST | `/webhook/jobs/:build_job_id/:secret`   | Git 平台 Webhook 回调（202）；优先校验请求签名，也可用 URL `secret`；按 delivery 去重，重复投递返回 202 且 `triggered=0`；校验失败 401 |
| POST | `/webhook/repos/:repository_id/:secret` | 已废弃的仓库级路径，固定返回 `410 Gone`                                                                                                |

---

## 7. 典型集成流程（CI）

```bash
HOST=https://bedrock.example.com
PAT=br_...   # scopes: docs:read, docs:write, agents:run

# 1. 推送接口文档（也可用数字项目 ID）
curl -fsS -X POST "$HOST/api/v1/projects/my-product/docs/push" \
  -H "Authorization: Bearer $PAT" -H 'Content-Type: application/json' \
  -d '{"api_dir":"openapi","api_doc_name":"v2","api_doc":"# API v2\n..."}'

# 2. 单篇读取校验（全量同步改用 docs/export）
curl -fsS "$HOST/api/v1/projects/my-product/docs/pull?api_dir=openapi&api_doc_name=v2" \
  -H "Authorization: Bearer $PAT"

# 2b. 全量导出（sync）
curl -fsS "$HOST/api/v1/projects/my-product/docs/export?api_dir=openapi" \
  -H "Authorization: Bearer $PAT"

# 3. 触发 Agent 运行并记录 run id
RUN_ID=$(curl -fsS -X POST "$HOST/api/v1/ai/agents/1/api-runs" \
  -H "Authorization: Bearer $PAT" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)

# 4. 轮询运行状态（需属主具备 ai_runs:view 权限）
curl -fsS "$HOST/api/v1/ai/runs/$RUN_ID" -H "Authorization: Bearer $PAT"
```
