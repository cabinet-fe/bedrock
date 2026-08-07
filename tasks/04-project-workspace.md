# T4 前端：项目详情聚合工作台

## 目标

项目详情页升级为统一工作台：在现有 需求 / 接口文档 / 开发文档 三个 tab 之上，新增 **概览、构建任务、脚本任务、流水线、智能体** 面板，实现"一个项目下的所有资源统一查看与管理"。

## 现状依据

- 详情页：`web/src/views/projects/projects/pages/detail.vue` — `u-tabs`，tab 与 `?tab=` query 双向同步，按权限动态过滤 tab
- 现有面板：`requirements-panel.vue`、`docs-panel.vue`（按 doc-kind 复用）
- 各域 API 客户端：`web/src/api/cicd.ts`（listBuildJobs 等透传 query）、`web/src/api/ai.ts`；类型在 `web/src/api/types.ts`
- 后端过滤能力由 T1 提供：`/build-jobs?project_id=`、`/script-jobs?project_id=`、`/build-pipelines?project_id=`、`/ai/agents?project_id=`、四个 run 列表的 `project_id` 过滤
- 聚合策略按 D4：无后端聚合接口，前端并行请求拼装

## 改动清单

### 1. API 客户端

- `web/src/api/types.ts` + `cicd.ts` / `ai.ts`：各 ListQuery 类型增加 `project_id?: number`；资源对象类型增加 `project_id?: number | null`

### 2. 概览 tab（新增 `overview-panel.vue`）

- 统计卡片行：构建任务数、脚本任务数、流水线数、智能体数、需求数（并行调各列表接口 `page_size=1` 取 `total`）
- 最近运行区：并行调 `build-runs / script-runs / pipeline-runs / ai/runs`（`project_id=<id>`、`page_size=5`、按时间倒序），合并为一个时间线列表，项内含类型徽标、名称、状态、耗时、触发者，点击跳对应 run 详情
- 加载失败/无权限的单个区块降级显示，不拖垮整个面板

### 3. 资源面板（各新增一个 panel 组件）

- `build-jobs-panel.vue`：表格列 名称/仓库/分支/启用/标签/最近运行状态；操作：触发构建（`cicd_build_jobs` 写权限 + 任务可写时可用）、编辑（跳 cicd 页并预填）、查看运行历史
- `script-jobs-panel.vue`：名称/类型/启用/最近状态；操作：触发、编辑
- `pipelines-panel.vue`：名称/启用/最近运行状态；操作：触发、打开画布编辑器（跳 `/cicd/pipelines/:id/editor`）
- `agents-panel.vue`：名称/CLI/启用/最近运行；操作：触发运行、查看运行历史、编辑
- 每个面板顶部提供"新建"按钮 → 跳对应域页面并以 query 携带 `?project_id=<id>` 预填（预填消费逻辑在 T5 实现，本任务先把入口接上）
- 行内写操作一律复用目标域的权限判定（`hasPermission('<域>:<action>')`），面板本身只做查看聚合 + 跳转，不在项目页内重写编辑表单

### 4. tab 组装与权限

- `detail.vue` 注册 5 个新 tab，纳入 `?tab=` 同步
- 面板显隐规则：用户无对应域 `:view` 权限码时，该 tab 不渲染（与现有"按权限动态过滤 tab"一致）；全部资源面板均无权限时至少保留概览
- 非项目成员：所有面板只读可见（T2/T1 已放开读），写按钮按权限码天然隐藏

### 5. 菜单入口兼容

- `/project/requirements|docs|dev-docs` 三个 scope-entry 菜单页（`web/src/router/index.ts:226-290`）本期保留不动

## 实施步骤

1. 补 API 类型与客户端参数
2. 概览面板（并行请求 + 降级）
3. 四个资源面板（共用列表查询与空态逻辑，避免重复抽象过度——先复制后收敛，三个以上再抽）
4. detail.vue 组装 + 权限过滤
5. `vp check` + 多角色手工验证

## 验证项

### 自动化

- [ ] `cd web && vp check` 通过

### 手工 UI 验证（核心验收路径）

- [ ] 准备一个项目：关联 ≥2 个构建任务、≥1 个脚本任务、≥1 个流水线、≥1 个智能体，并各有运行记录
- [ ] 概览 tab：各统计数字与各域全局列表实际数量一致；最近运行时间线合并正确、排序正确、点击跳转正确
- [ ] 四个资源面板：列表内容与对应全局列表页按 project 过滤的结果一致；空项目各面板空态正常
- [ ] "新建"入口跳转后表单已预填当前项目（配合 T5）
- [ ] `?tab=` 刷新/分享链接直达对应 tab 仍工作

### 权限验证

- [ ] 无 `cicd_*:view` 权限的用户：对应 tab 不出现，其余面板正常
- [ ] 普通成员（非项目成员，有各域 view 权限）：所有面板数据可见，但触发/编辑等写按钮不可用（点击后接口也应 403，前后端一致）
- [ ] 某个资源面板接口失败（如人为断 cicd 服务路由）时概览其余区块不受影响

## 依赖与备注

- 依赖 T1（后端过滤与读规则）；建议与 T5 同期联调（新建预填）
- 注意控制并行请求数量与 loading 态；概览面板不做自动刷新（后续可加）
