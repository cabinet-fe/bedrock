---
name: bedrock
description: 通过 Bedrock CLI 触发构建、运行脚本任务/流水线、调用智能体，并轮询状态与抓取日志。配置存于项目根目录 .bedrock.jsonc（含访问令牌）。当用户要求构建/部署项目、跑流水线、执行脚本任务、运行智能体（如 "/bedrock 构建xxx"、"跑一下流水线"、提到 bedrock），或工作区存在 .bedrock.jsonc 且用户要求运行其中任务时使用；配置缺失时引导用户补全并生成 .bedrock.jsonc 与 .gitignore。
---

# bedrock

通过本技能目录下的 `scripts/bedrock.mjs`（Node.js ≥ 24）操作 Bedrock 服务器。下文命令中的 `<cli>` 均指 `node <本技能目录>/scripts/bedrock.mjs`。

配置文件为项目根目录 `.bedrock.jsonc`（JSONC 格式，支持 `//` 注释；从当前目录向上自动查找）：

```jsonc
{
  "pat": "br_xxx",        // 访问令牌，Bedrock Web「资源 → 访问令牌」创建
  "base_url": "http://192.168.1.10:8080",  // 服务器地址，不带 /api/v1 结尾
  "builds":    [{ "name": "xx项目", "id": 1 }],
  "scripts":   [{ "name": "xxx脚本任务", "id": 1 }],
  "pipelines": [{ "name": "xxx流水线", "id": 1 }],
  "agents":    [{ "name": "xxxx智能体", "id": 1 }]
}
```

`.bedrock.jsonc` 含访问令牌，属于敏感文件：绝不能提交到 git（`init` 子命令会自动写入 `.gitignore`，改动配置后复查一下）、不要在回复中完整展示 pat。

## 第一步：检查配置

```bash
<cli> doctor
```

- 输出全部 ✓ 且已登记任务 → 进入「选择任务并运行」。
- 文件不存在 / 缺 pat / 缺 base_url / 任务未登记 → 进入「配置引导」。

## 配置引导（配置缺失时）

需要向用户收集：① base_url；② pat；③ 要登记的任务（构建/脚本/流水线/智能体，name + id）。一次性把缺失项问清楚，不要挤牙膏。

1. 生成模板（自动把 `.bedrock.jsonc` 追加进 .gitignore；若提示已存在则直接编辑现有文件）：

   ```bash
   <cli> init
   ```

2. 把用户给的 pat、base_url 填入模板（保持合法 JSONC，注释可保留）。用户不知道 id 很正常——先填 pat 与 base_url，再用服务器查询帮用户挑：

   ```bash
   <cli> search --type builds                  # 也支持 scripts / pipelines / agents
   <cli> search --type agents --keyword 订单   # 按名称过滤
   ```

   把结果（id + 名称）展示给用户选择，选中后写入 `.bedrock.jsonc` 对应数组。

3. 智能体的填写与使用说明 → 先读 `references/agents.md` 再向用户解释或提问。
4. 校验：`<cli> doctor --remote`（会实际请求服务器健康检查）。
5. 提醒用户：PAT 创建时需勾选 `builds:run` / `scripts:run` / `pipelines:run` / `agents:run` 中对应 scope，否则触发会 403。

## 选择任务并运行

1. **用户明确了任务**（如 `/bedrock 构建xxx任务`、"跑一下部署流水线"）：动词映射类型——构建/部署→`build`、脚本任务→`script`、流水线→`pipeline`、智能体/agent→`agent`；任务名传 `--name`（支持部分匹配）。

   ```bash
   <cli> build --name "xx项目" --branch main
   <cli> script --name "xxx脚本任务"
   <cli> pipeline --name "xxx流水线"
   <cli> agent --name "xxxx智能体" --prompt "修复登录超时问题并自测"
   ```

   `--name` 在配置中匹配不到时，用 `search` 到服务器上找候选，问用户是否采用，并把选中的登记进 `.bedrock.jsonc`，下次就不用再查。

2. **配置里只有一个任务**：直接运行，不要提问。快捷方式：`<cli> run`（自动识别类型）。

3. **配置里有多个任务且用户没指定**：交互式提问让用户选。先问类型（仅当多种类型都有配置时；用 AskUserQuestion），再问具体任务。AskUserQuestion 每题最多 4 个选项，超出时改为文字列出编号让用户回复。问题里直接展示配置中的任务名（可附 id），不要让用户猜。

## 运行与结果汇报

默认行为：触发（服务器返回 202 表示已排队）→ 脚本轮询到终态（默认 5s 间隔、30 分钟超时，`--timeout` / `--poll-interval` / `--no-wait` 可调）→ 输出结果。构建/脚本失败时脚本自动打印日志尾部；智能体成功时打印完整 `output_text`。

完成后向用户汇报：任务名、run id、最终状态、耗时；失败时转述关键错误与日志要点（不要整段粘贴长日志）。超时或 `--no-wait` 时给出后续查询命令（脚本超时会自动提示 `status` / `log` 用法）：

```bash
<cli> status --type build --run-id 123
<cli> log --type build --run-id 123 --tail 100
```

## 注意

- 触发类命令退出码：0 成功、1 运行或请求失败、2 配置/用法错误；401 = 令牌无效，403 = 缺 scope。
- 智能体运行前置条件是工作区就绪（`workspace_status = ready`），详见 `references/agents.md`；API 细节（字段、状态机、WebSocket 日志）见 `references/api.md`。
- pat 也可用环境变量 `BEDROCK_PAT` / `BEDROCK_BASE_URL` 覆盖（CI 场景），脚本会优先读环境变量。
