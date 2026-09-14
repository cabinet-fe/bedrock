# 缺陷工作流（bug 命令组）

`bedrock.mjs bug` 命令组用于「拉取 → 修复 → 回写」缺陷闭环：按 `.bedrock.jsonc` 的 `bugs` 绑定拉取未关闭缺陷，查看详情与评论，修复后回写缺陷状态并自动评论。

## 绑定配置

`.bedrock.jsonc` 增加 `bugs` 段（`init` 模板已包含）：

```jsonc
{
  "bugs": {
    "project_slug": "my-project",  // 绑定项目 slug，bug 命令按它定位项目
    "developer": "zhangsan"        // 绑定开发者：用户名或用户 ID，bug list 默认按其过滤 assignee
  }
}
```

- `project_slug`：服务器上项目的 slug。不知道 slug 时先配好 `BEDROCK_PAT` 与 `base_url`，用 `search --type projects` 查询可见项目的 id / slug / 名称。
- `developer`：用户名或用户 ID 均可（服务端两者都能解析）。
- 绑定缺失时 bug 命令退出码 2，并打印上述示例进入配置引导；`doctor` 会校验该段：未配置提示「bug 命令组不可用」，配置不完整会指出缺哪一项。

## PAT scope

| 命令 | 需要 scope |
| --- | --- |
| `bug list` / `bug show` | `bugs:read` |
| `bug status` / `bug comment` | `bugs:write` |

PAT 缺 scope 时服务端返回 403，脚本会提示缺少的 scope：在 Bedrock Web「资源 → 访问令牌」创建勾选了 `bugs:read` / `bugs:write` 的 PAT，并更新 `BEDROCK_PAT`。未配置 PAT 时按既有引导设置 `BEDROCK_PAT=br_xxx`。

## 命令

```bash
<cli> bug list                              # 绑定项目 + 绑定开发者(assignee) + 未关闭
<cli> bug list --status open                # 只看指定状态（open|in_progress|resolved|closed|rejected）
<cli> bug list --all                        # 含已关闭（closed）
<cli> bug list --page 2                     # 翻页（默认每页 50 条）
<cli> bug show 123                          # 详情：状态、描述、评论、活动记录
<cli> bug status 123 --to resolved          # 流转状态（五种合法状态之一）
<cli> bug comment 123 --content "修复说明"  # 发表评论
```

`bug list` 输出示例：

```
项目 my-project 中 assignee=zhangsan 的缺陷（未关闭，共 2 条，第 1/1 页）
  #12  [open]  登录页在 Safari 下白屏
        严重=high 优先级=urgent 经办=张三 更新=2026-09-14 10:2
  #15  [in_progress]  导出报表缺少合计行
        严重=normal 优先级=normal 经办=张三 更新=2026-09-13 18:40
```

## 闭环流程

1. `bug list` — 拉取绑定项目下 assignee 为绑定开发者且未关闭的缺陷。
2. `bug show <id>` — 阅读描述、评论与活动，定位问题（描述常含插件报单自动附上的来源网址，截图附件需到平台 Web 查看）。
3. 本地修复并验证。
4. `bug status <id> --to resolved` — 回写状态，平台自动记录流转活动。
5. `bug comment <id> --content "..."` — 评论说明，建议包含：根因、修复方式、验证结果、关联提交。

## 注意

- 缺陷状态机：`open` / `in_progress` / `resolved` / `closed` / `rejected`；`--to` 传其它值直接报用法错误。
- 退出码：0 成功、1 请求失败、2 配置/用法错误；401 = 令牌无效，403 = 缺 scope。
- API 细节见 [api.md](api.md)，完整契约见仓库 `api/project.md`。
