import type { HighlightPart, SearchMenuItem } from "./types";

/** Shortcut indicator based on operating system */
export const isMac =
  typeof navigator !== "undefined" && /macintosh|mac os x/i.test(navigator.userAgent);
export const shortcutLabel = isMac ? "⌘ K" : "Ctrl K";

/** Keywords map for fast and intelligent fuzzy lookup across languages & pinyin initials */
export const MENU_KEYWORDS: Record<string, string[]> = {
  "/": ["home", "shouye", "sy", "index", "dashboard", "概览", "工作台", "主页"],
  "/handbook": ["handbook", "manual", "shouce", "sc", "help", "docs", "帮助", "指南"],
  "/ops/processes": ["process", "jincheng", "jc", "proc", "pm2", "ops", "运维", "进程"],
  "/ops/dev-environments": ["dev", "env", "environment", "huanjing", "hj", "容器", "环境"],
  "/resource/repositories": ["repo", "repository", "git", "cangku", "ck", "code", "代码仓库"],
  "/resource/servers": ["server", "fuwuqi", "fwq", "host", "node", "服务器", "主机"],
  "/resource/credentials": [
    "credential",
    "pingzheng",
    "pz",
    "secret",
    "key",
    "password",
    "凭证",
    "密钥",
  ],
  "/resource/tokens": ["token", "lingpai", "lp", "auth", "api key", "访问令牌"],
  "/cicd/build-jobs": ["build", "job", "task", "goujian", "gj", "rw", "构建", "编译"],
  "/cicd/build-runs": ["build", "run", "history", "jilu", "jl", "构建记录", "历史"],
  "/cicd/script-jobs": ["script", "shell", "job", "task", "jiaoben", "jb", "脚本任务"],
  "/cicd/script-runs": ["script", "run", "history", "脚本记录"],
  "/cicd/pipelines": ["pipeline", "flow", "liushuixian", "lsx", "ci", "cd", "流水线", "编排"],
  "/cicd/pipeline-runs": ["pipeline", "run", "history", "流水线运行", "执行记录"],
  "/project/projects": ["project", "xiangmu", "xm", "app", "项目列表", "工程"],
  "/project/requirements": ["requirement", "xuqiu", "xq", "issue", "demand", "需求", "迭代"],
  "/project/docs": ["docs", "api", "swagger", "jiekou", "jk", "wendang", "wd", "接口文档"],
  "/project/dev-docs": ["docs", "dev", "kaifa", "kf", "wendang", "开发文档", "架构"],
  "/ai/agents": ["agent", "ai", "bot", "zhinengti", "znt", "智能体"],
  "/ai/runs": ["ai", "run", "history", "record", "运行记录", "智能体执行"],
  "/ai/skills": ["skill", "jineng", "jn", "tool", "ai", "技能", "插件"],
  "/system/users": ["user", "member", "account", "yonghu", "yh", "用户", "成员", "账号"],
  "/system/roles": ["role", "permission", "juese", "js", "角色", "权限组"],
  "/system/resources": ["resource", "perm", "auth", "quanxian", "qx", "zy", "权限资源", "菜单"],
  "/system/dictionaries": ["dictionary", "dict", "zidian", "zd", "数据字典", "配置"],
  "/system/operation-logs": ["log", "audit", "rizhi", "rz", "caozuo", "cz", "操作日志", "审计"],
};

/** Fuzzy score calculation for ranking search results */
export function calculateScore(item: SearchMenuItem, query: string): number {
  if (!query) return 1;

  const q = query.toLowerCase();
  const title = item.title.toLowerCase();
  const path = item.path.toLowerCase();
  const group = item.groupTitle.toLowerCase();

  if (title === q) return 100;
  if (title.startsWith(q)) return 80;
  if (title.includes(q)) return 60;

  for (const kw of item.keywords) {
    const k = kw.toLowerCase();
    if (k === q) return 55;
    if (k.startsWith(q)) return 45;
    if (k.includes(q)) return 35;
  }

  if (path.includes(q)) return 30;
  if (group.includes(q)) return 20;

  return 0;
}

/** Highlight matched substring in text */
export function highlightParts(text: string, query: string): HighlightPart[] {
  const q = query.trim().toLowerCase();
  if (!q || !text) return [{ text, highlight: false }];

  const lower = text.toLowerCase();
  const idx = lower.indexOf(q);
  if (idx === -1) return [{ text, highlight: false }];

  const parts: HighlightPart[] = [];
  let lastIndex = 0;
  let curr = lower.indexOf(q, lastIndex);
  while (curr !== -1) {
    if (curr > lastIndex) {
      parts.push({ text: text.slice(lastIndex, curr), highlight: false });
    }
    parts.push({ text: text.slice(curr, curr + q.length), highlight: true });
    lastIndex = curr + q.length;
    curr = lower.indexOf(q, lastIndex);
  }
  if (lastIndex < text.length) {
    parts.push({ text: text.slice(lastIndex), highlight: false });
  }
  return parts;
}
