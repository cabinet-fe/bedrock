# T3 前端：项目卡片式列表

## 目标

将项目列表页从 ProTable 表格改造为卡片网格，作为"项目中心化"的入口形态；保留搜索、状态过滤、新建与行内操作能力。

## 现状依据

- 列表页：`web/src/views/projects/projects/pages/main.vue` — ProTable，列：名称/标识/状态/公开/标签/更新时间/操作；行操作按 `rowData.permissions` 能力位显隐（:184）；新建/编辑用 `FormDialog`，成员管理弹窗 `members-panel.vue`
- 数据：`web/src/api/projects.ts` 列表返回 `ProductProjectView`（含 `my_role`、`permissions`）
- 权限：`web/src/composables/use-permission.ts` `hasPermission`；前端约定见 `.agents/fe.md`，UI 组件用 `@veltra/*`

## 改动清单

### 1. 卡片网格

- 重写 `main.vue`：ProTable → 卡片网格（CSS Grid，响应式：宽屏 3–4 列、窄屏 1–2 列）
- 新增卡片组件 `web/src/views/projects/projects/components/project-card.vue`（或就近放在 pages 同级 components 目录，遵循现有目录习惯）：
  - 内容：项目名称、状态徽标（active/archived）、描述（2 行截断）、标签、slug、`my_role` 角标、更新时间
  - 点击卡片主体 → 进入详情 `/project/projects/:id`
  - 右上角操作菜单：进入 / 成员 / 编辑 / 归档 / 解散，显隐逻辑完全沿用现有 `permissions` 能力位 + `hasPermission`
- 卡片视觉遵循 `@veltra/*` 现有卡片/面板样式与主题 token（`web/src/theme/`），不引入新依赖

### 2. 页头与状态

- 保留：keyword 搜索（name/slug/tags）、status 过滤（全部/活跃/已归档）、新建按钮
- 分页：沿用后端分页（分页器或"加载更多"），空状态/加载态齐全
- 归档/解散/成员管理交互：复用现有 `FormDialog`、`members-panel.vue`、确认弹窗逻辑，不重写

### 3. 兼容

- 列表接口与类型不变（T2 后非成员也能拿到 `my_role` 为空的视图，卡片直接展示）
- 路由 `/project/projects` 不变

## 实施步骤

1. 先写 `project-card.vue` 展示组件
2. 改造 `main.vue` 网格 + 过滤 + 分页，迁移现有操作逻辑
3. `vp check`，手工多角色验证

## 验证项

### 自动化

- [ ] `cd web && vp check`（format + lint + typecheck）通过

### 手工 UI 验证

- [ ] 创建至少 6–8 个项目，验证网格在多行、不同视口宽度下布局正常、卡片内容截断不撑破
- [ ] 搜索 keyword、切换状态过滤、翻页/加载更多结果正确
- [ ] 卡片点击跳转详情正确；操作菜单各动作（编辑保存、归档、解散、打开成员面板）功能与改造前一致
- [ ] 空状态（无项目/搜索无结果）展示正常

### 权限验证（依赖 T2 语义）

- [ ] 超管/管理员：看到全部卡片，操作齐全
- [ ] 普通成员（非项目成员）：看到全部卡片，`my_role` 角标缺失或为空，操作菜单仅剩"进入"
- [ ] 项目 owner/admin/member/readonly：各自卡片操作项与改造前表格行操作完全一致（逐项对照）

## 依赖与备注

- 语义依赖 T2（全员可见后卡片才有意义）；纯 UI 改造可与 T2 并行开发、联调时合并验证
- 卡片第一版**不展示**任务统计数字（统计在 T4 概览面板呈现，避免列表 N+1 请求）
