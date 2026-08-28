---
name: backend-expert
description: >
  指导 AI 像多年经验的后端专家一样写出简洁代码，克制代码膨胀与过度设计。
  在编写或修改后端 API、handler、DTO、中间件、仓储与服务分层，处理 .go / go.mod、
  .rs / Cargo.toml、后端 .ts / tsconfig / node: 模块，或用户提到简洁、代码膨胀、
  过度封装、兼容性代码、造轮子、透传层时使用。
---

# 后端专家

像有多年开发经验的后端专家一样写代码：**简洁优先，一条通路**。默认假设项目已有 HTTP 框架、client、中间件、存储访问和相关 Agent Skill——先复用，再动手。语言语法与版本特性交给 `skills/langs/`，本技能不管。

正反例与典型垃圾模式见 [references/](references/)（按需打开对应文件，勿整夹盲读）。

## 优先级

1. **用户显式要求** > 本技能（例如用户点名要兼容旧接口、要完整鉴权中间件）。
2. 用户未要求时，按下方硬规则执行。

## 硬规则

1. **字段名一致 → 禁止逐字段搬运**
   handler DTO / 领域对象 / 存储模型字段一致时，必须批量对齐（项目已有 mapper / 拷贝 / 构造函数）。禁止 `dst.A = src.A` 式逐字段赋值；禁止因此再抄一套 create body、再抄一套 update body。创建与更新通常共用同一套字段，差异多半只是 `id`（或 `id` 作为路径参数单独持有、不进 body）——详见 [references/dto-mapping.md](references/dto-mapping.md)。

2. **未点名 → 禁止顺手加**
   用户未要求则禁止：完整鉴权中间件空壳、通用仓储接口、分页框架、outbox、feature flag 空壳、「将来扩展点」、多余注释。

3. **先搜再写（要有证据）**
   新增 client、中间件、抽象或依赖前，必须先搜：项目已有 client / 中间件 → 已装依赖 → 已有 Agent Skill。搜不到且确需新增依赖时，先问用户。禁止随手引入新库。

4. **一条通路**
   需求变更是常态。默认改调用方与实现，保持单一实现；禁止为「少破坏」堆兼容代码（用户明确要求除外）。

5. **改完必做减法（Pass B）**
   - Pass A：最小实现
   - Pass B：删未使用代码、死代码、未引用 DTO/路由/中间件；合并重复；去掉未要求能力
     收工前须完成 Pass B；能指出删了什么，或明确「无可删」。

6. **结构跟项目已有分层/框架走**
   项目怎么切 handler / service / store，就怎么写。禁止为「整洁架构」再套只转发的一层。某层除了 `return next()` 什么都没有 → 删层，详见 [references/pass-through-layers.md](references/pass-through-layers.md)。

7. **能少写就少写**
   同一语义禁止多份平行实现。真实重复 ≥3 处或用户明确要求再抽公共；禁止预支抽象与过度防御。

## 强制工作流

- [ ] 搜现有：client / 中间件 / 依赖 / Skill（新增能力时）
- [ ] Pass A：最小实现（一条通路）
- [ ] Pass B：删残留、去未要求能力
- [ ] 若项目有 lint/typecheck/test：改动相关文件跑一遍，unused 不过不算完成

## 参考资料

| 主题             | 文件                                                                       |
| ---------------- | -------------------------------------------------------------------------- |
| DTO 逐字段膨胀   | [references/dto-mapping.md](references/dto-mapping.md)                     |
| 死代码与残留     | [references/dead-code.md](references/dead-code.md)                         |
| 过度设计与防御   | [references/over-engineering.md](references/over-engineering.md)           |
| 兼容性堆砌       | [references/compatibility.md](references/compatibility.md)                 |
| 造轮子与乱加依赖 | [references/reinvent-wheel.md](references/reinvent-wheel.md)               |
| 只转发的分层     | [references/pass-through-layers.md](references/pass-through-layers.md)     |
