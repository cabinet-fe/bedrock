# 自定义指令

## 使用内部依赖

本项目使用了一些内部库, 执行任务时请优先使用它们, 避免重复造轮子. 同时你应该使用 `docs-search` 技能去更好的使用它们:

- `@cat-kit/*`（slug：`cat-kit`）：TypeScript 工具库——core 通用工具、http 客户端、fe/be 前后端工具、crypto 安全随机、cli、tsconfig 预设
- `@veltra/*`（slug：`veltra-ui`）：Vue 3 组件与能力库——desktop/mobile 组件、ai 对话、sheet 电子表格、图标、组合式函数、常用工具、指令、样式、组件 vite 自动解析器

- Go / Gin 不在 docs-search 收录范围，其写法规范见 go、gin 技能
