# Bedrock 智能体（Agent）说明

智能体是 Bedrock 平台里的 AI 执行单元：绑定一个 CLI/模型（`cli_key`）和一个工作区（workspace），接收到任务后在工作区里自主执行（写代码、跑命令等），产出结论文本（`output_text`）和可选的制品包（zip，`GET /ai/runs/:id/artifact`）。适合"让 AI 帮我修个问题 / 写个功能 / 做一次排查"这类开放式任务，与一次性的构建/流水线互补。

## 怎么找到智能体 id

- Bedrock Web：「AI → 智能体」列表页，每行直接展示 ID 列。
- 或配好 pat / base_url 后用 CLI 查：

  ```bash
  node <本技能目录>/scripts/bedrock.mjs search --type agents [--keyword 名称]
  ```

把选中的写进 `.bedrock.json`：

```jsonc
{
  "agents": [
    { "name": "xxx智能体", "id": 3 }
  ]
}
```

## 运行前置条件

1. **工作区就绪**：智能体的 `workspace_status` 必须为 `ready`，否则触发直接 400。未就绪时先在 Web 端打开该智能体，让它完成工作区初始化。
2. **PAT scope**：令牌必须包含 `agents:run`，否则 403。
3. 智能体需处于启用状态（列表中 `enabled` 不为 false）。

## 怎么运行

```bash
node <本技能目录>/scripts/bedrock.mjs agent --name "xxx智能体" --prompt "任务描述"
```

- `--prompt`（即 API 的 `user_prompt`）可选：省略时智能体按自身预置的指令/目标执行，适合定时巡检类智能体；有明确任务时务必传。
- 运行是异步的：触发返回 202 和 run id，CLI 默认轮询到终态（可能持续数分钟到更久，`--timeout` 默认 1800s）。
- 终态含义：`success` 任务完成（CLI 会打印完整 `output_text`）；`failed` 执行失败（打印 `error_message`）；`cancelled` 被人取消。
- run id 可用 `status --type agent --run-id N` 随时复查。

## 编写 --prompt 的建议

- 说清目标与验收标准："修复登录接口 500 问题，并补充回归测试" 好于 "修一下登录"。
- 给出必要上下文：仓库/模块路径、报错信息、期望影响范围。
- 一次一个主题；多件事拆成多次运行。

## 排错速查

| 现象 | 原因与处理 |
| --- | --- |
| 触发 400 | 工作区未就绪：到 Web 端打开智能体完成初始化 |
| 触发 403 | PAT 缺 `agents:run` scope，重新创建令牌 |
| 触发 401 | pat 无效/过期/不是 br_ 开头 |
| 长时间 running | 正常现象（自主执行较慢）；到 Web 端或 `status` 查看进度，必要时 `POST /ai/runs/:id/cancel` |
