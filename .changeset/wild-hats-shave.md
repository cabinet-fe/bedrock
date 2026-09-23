---
"bedrock": minor
---

#### 统一工作项模型并交付看板、通知闭环与迭代

- 需求与缺陷合并为 `project_issues`（type=requirement/bug/task），评论、附件、活动统一；迁移 000061 完成数据搬迁（子表 oldID→newID 重挂）后删除旧表，000062 为通知增加 issue_id
- 旧 `/bugs`、`/requirements` 路由保留为兼容别名：repository 层做 type 固定 facade，旧 JSON 形状不变，Chrome 插件与 PAT `bugs:*` 不受影响
- 缺陷状态改 `bug_status` 字典（种子为原 5 值）；新增 `/issues` 统一端点与看板视图（列来自状态字典，终态默认不入板）
- 字段级活动记录（优先级/严重度/经办人/标签 旧值→新值）；协作通知闭环：指派/流转/评论/@提及 → 站内信（复用 WS 通道，不通知操作者），创建/评论自动关注
- 新增迭代（planned/active/closed）、任务类型与基于活动回算的燃尽图
- 前端：需求/缺陷默认看板视图（原生 HTML5 拖拽流转，保留表格切换）、跨项目缺陷看板、迭代面板与自绘 SVG 燃尽图，零新前端依赖
