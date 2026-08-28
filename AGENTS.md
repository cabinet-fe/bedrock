# AGENTS

Agent 入口索引。详细内容在 `.agents/docs/`，**按需读取，禁止一次加载全部**。

## 文档

| 文件 | 何时读 | 何时更新 |
| --- | --- | --- |
| `.agents/docs/PROJECT.md` | 需要知道项目类别与仓库结构 | 仅 setup：类别、组织结构、全栈形态变了 |
| `.agents/docs/ARCHITECTURE.md` | 业务/技术架构、技术栈 | 仅 setup：换栈、改分层、加/删应用边界。implement 禁止改 |
| `.agents/docs/DEV-STANDARDS.md` | 写代码、做 review | 仅 setup：规范或偏好变了 |
| `.agents/docs/SMELLS.md` | 写代码时按坏味道边写边收；review 对照 | 仅 setup：技能包模板变了 |
| `.agents/docs/CODE-MAP.md` | 定位模块；按模块/路径检索，禁止全文加载 | implement / sync-context：模块表增删行，或某模块路径、入口、职责、依赖边变了。只改相关行。架构级变化先 setup 改 ARCHITECTURE，再由 setup 同步本文件。模块内部加文件不算。 |
| `.agents/docs/CONTEXT/index.md` | 先读模块索引，再打开当前条目。禁止加载整个 CONTEXT。按变更路径定位：运行 `node .agents/scripts/spec-files.mjs query <路径...>` | archive：新模块入库；sync-context：改动推翻已有条目或新增未入库能力 |
| `.agents/docs/PRD.md` | 产品需求真源（2.0 基线） | 仅用户确认的需求变更 |
| `.agents/docs/DESIGN.md` | 技术设计真源（决策、领域模型、安全边界） | 仅用户确认的设计变更 |
| `.agents/docs/ops-handbook.md` | 安装、备份、升级与回退操作手册 | 运维行为变化时 |
| `.agents/docs/release-checklist.md` | 发布检查单 | 发布流程变化时 |
| `dsh-integration-design.md` | DSH 交互会话集成设计稿（待评审，仓库根） | 评审通过后实现时 |