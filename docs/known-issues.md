# 已知问题（2.0 GA）

仅列 **非阻塞**、产品已接受或明确不做的项。阻塞项必须在对应阶段 Gate 关闭前清零。

## 明确不做（非缺陷）

- 1.x → 2.0 数据迁移
- 多活粘滞会话、远程构建 Runner、S3 作为 GA 必达
- 容量/延迟 SLO 验收
- 显式 deny ACL；非项目域对象 ACL
- AI 上下文自动注入需求/历史会话（首期仅提示词 + 仓库）

## P1 清单勾选说明

`docs/roadmap/P1.md` 任务清单在实现期未逐项勾选。P5 对照代码与 P2–P4 已通过 Gate，确认 P1 交付物已落地（动态 RBAC、Repository/BuildJob/BuildRun、分发 summary、重启恢复、Webhook、三库合同测试脚手架等）。P1 清单已在 P5 统一勾选关闭。

## 非阻塞已知限制

| 项 | 说明 |
| --- | --- |
| 三库进程冒烟 | CI 默认跑 SQLite；Postgres/MySQL 需本地/CI 服务 + `BEDROCK_SMOKE_*` / contract DSN |
| Linux 包启动冒烟 | 交叉编译在 macOS/CI 均可产出 amd64/arm64；**本机执行**启动冒烟仅在 Linux amd64 主机自动跑 |
| Playwright | 需已启动后端或 `E2E_BASE_URL`；CI 以 API smoke 为主路径证据 |
| 流水线节点同项目 | 流水线 `project_id` 与节点引用的 BuildJob / ScriptJob / Agent **不强制**同属一项目 |
| 项目卡片统计 | 卡片列表不做后端聚合统计；概览由前端并行调各域列表拼装 |
| 项目全员可见门槛 | 无内置普通业务角色种子；「全员可见」依赖管理员给目标角色配置 `project_projects:view`（超管除外） |

无未关闭落地阻塞项时，本文件应保持「仅非阻塞」状态。
