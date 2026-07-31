# API 契约

Bedrock 的 HTTP 接口约定写在本目录的 Markdown 里，按业务域拆分。

通用约定（响应信封、分页、认证方式）见 [.agents/api.md](../.agents/api.md)。
领域行为和权限语义见 [docs/DESIGN.md](../docs/DESIGN.md)。

## 域索引

| 文件 | 内容 |
| --- | --- |
| [auth.md](auth.md) | 认证（login / refresh / logout / me） |
| [system.md](system.md) | 用户、角色、菜单分组、RBAC 资源、字典、操作日志、通知 |
| [resource.md](resource.md) | 代码仓库、服务器、凭证、AI CLI、个人访问令牌（PAT） |
| [cicd.md](cicd.md) | 构建任务、构建运行、构建流水线、流水线运行、Webhook |
| [cicd-scripts.md](cicd-scripts.md) | 脚本任务、脚本运行、脚本 Webhook |
| [ops.md](ops.md) | 仪表盘卡片与运维（进程、开发环境） |
| [project.md](project.md) | 项目（成员、需求、评论、附件、文档发布） |
| [ai.md](ai.md) | Agents、运行记录、Skills |

## 怎么改契约

1. 先改对应域的 `api/<域>.md`（路径、字段、响应、错误码）。
2. 再改后端 handler / service 和前端调用，让实现与文档一致。
3. 需要时补测试（单测或 `make smoke-api-e2e`）。

新增或变更的请求/响应字段必须先写进契约文档，前后端不要各自加一套没记录的结构。

改哪个域就改哪个文件，不要把无关接口塞进同一文件。
