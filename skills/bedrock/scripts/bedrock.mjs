#!/usr/bin/env node
// Bedrock 客户端 CLI：校验项目根目录 .bedrock.json 配置，触发构建 / 脚本任务 / 流水线 / 智能体并轮询到终态。
// 需要 Node.js >= 24（使用内置 fetch 与现代语法）。

import fs from "node:fs";
import path from "node:path";

const MIN_NODE_MAJOR = 24;
const CONFIG_NAME = ".bedrock.json";
const API_PREFIX = "/api/v1";

const KINDS = {
  build: {
    section: "builds",
    label: "构建任务",
    listPath: "/build-jobs",
    triggerPath: (id) => `/build-jobs/${id}/runs`,
    statusPath: (id) => `/build-runs/${id}`,
    logPath: (id) => `/build-runs/${id}/log`,
    scope: "builds:run",
    hasLog: true,
    runNumberKey: "build_number",
  },
  script: {
    section: "scripts",
    label: "脚本任务",
    listPath: "/script-jobs",
    triggerPath: (id) => `/script-jobs/${id}/runs`,
    statusPath: (id) => `/script-runs/${id}`,
    logPath: (id) => `/script-runs/${id}/log`,
    scope: "scripts:run",
    hasLog: true,
    runNumberKey: "run_number",
  },
  pipeline: {
    section: "pipelines",
    label: "流水线",
    listPath: "/build-pipelines",
    triggerPath: (id) => `/build-pipelines/${id}/runs`,
    statusPath: (id) => `/pipeline-runs/${id}`,
    logPath: null,
    scope: "pipelines:run",
    hasLog: false,
    runNumberKey: "run_number",
  },
  agent: {
    section: "agents",
    label: "智能体",
    listPath: "/ai/agents",
    triggerPath: (id) => `/ai/agents/${id}/api-runs`,
    statusPath: (id) => `/ai/runs/${id}`,
    logPath: null,
    scope: "agents:run",
    hasLog: false,
    runNumberKey: null,
  },
};

const SEARCH_TYPES = {
  builds: "build",
  scripts: "script",
  pipelines: "pipeline",
  agents: "agent",
};

const SUCCESS_STATUS = "success";
const TERMINAL_STATUSES = new Set(["success", "failed", "cancelled", "interrupted"]);

const TEMPLATE = `{
  // Bedrock 访问令牌（PAT），以 br_ 开头。在 Bedrock Web「资源 → 访问令牌」创建，
  // 勾选需要的 scope：builds:run / scripts:run / pipelines:run / agents:run
  "pat": "",
  // Bedrock 服务器地址，如 http://192.168.1.10:8080（末尾不要带 /api/v1，带了也会被自动忽略）
  "base_url": "",
  // 构建任务：name 任意便于记忆，id 为 Bedrock 构建任务 ID
  "builds": [
    // { "name": "xx项目", "id": 1 }
  ],
  // 脚本任务
  "scripts": [
    // { "name": "xxx脚本任务", "id": 1 }
  ],
  // 流水线
  "pipelines": [
    // { "name": "xxx流水线", "id": 1 }
  ],
  // 智能体（说明见技能目录 references/agents.md）
  "agents": [
    // { "name": "xxxx智能体", "id": 1 }
  ]
}
`;

// ---------- 基础工具 ----------

function out(msg) {
  console.log(msg);
}

function die(code, msg) {
  console.error(msg.trim());
  process.exit(code);
}

function ts() {
  return new Date().toLocaleTimeString("zh-CN", { hour12: false });
}

function maskSecret(s) {
  return s.length > 10 ? `${s.slice(0, 8)}****` : "****";
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseArgs(argv) {
  const opts = {};
  const positional = [];
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg.startsWith("--")) {
      const key = arg.slice(2);
      const next = argv[i + 1];
      if (next === undefined || next.startsWith("--")) {
        opts[key] = true;
      } else {
        opts[key] = next;
        i++;
      }
    } else {
      positional.push(arg);
    }
  }
  return { opts, positional };
}

// 允许配置文件里写 // 与 /* */ 注释（jsonc）；解析时先剥掉注释，注意不碰字符串内的内容（如 http://）。
function stripJsonComments(text) {
  let result = "";
  let inString = false;
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (inString) {
      result += c;
      if (c === "\\") {
        result += text[i + 1] ?? "";
        i += 2;
        continue;
      }
      if (c === '"') inString = false;
      i++;
      continue;
    }
    if (c === '"') {
      inString = true;
      result += c;
      i++;
      continue;
    }
    if (c === "/" && text[i + 1] === "/") {
      while (i < text.length && text[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && text[i + 1] === "*") {
      i += 2;
      while (i < text.length && !(text[i] === "*" && text[i + 1] === "/")) i++;
      i += 2;
      continue;
    }
    result += c;
    i++;
  }
  return result;
}

function findConfigFile(explicit) {
  if (explicit) return path.resolve(explicit);
  if (process.env.BEDROCK_CONFIG) return path.resolve(process.env.BEDROCK_CONFIG);
  let dir = process.cwd();
  for (;;) {
    const candidate = path.join(dir, CONFIG_NAME);
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

function requireConfigFile(opts) {
  const file = findConfigFile(opts.config);
  if (!file) {
    die(
      2,
      [
        `未找到 ${CONFIG_NAME}（从当前目录 ${process.cwd()} 向上查找也没有）。`,
        "",
        "请先初始化配置：",
        `  node ${path.resolve(process.argv[1] ?? "bedrock.mjs")} init`,
        "",
        "然后填入 pat（br_ 开头的访问令牌）与 base_url（如 http://192.168.1.10:8080），",
        "并在 builds / scripts / pipelines / agents 中登记要操作的 { name, id }。",
      ].join("\n"),
    );
  }
  return file;
}

function normalizeBaseUrl(raw) {
  let s = String(raw ?? "").trim().replace(/\/+$/, "");
  if (!s) return null;
  if (!/^https?:\/\//i.test(s)) {
    die(2, `base_url 需要带协议，例如 http://192.168.1.10:8080，当前值："${raw}"`);
  }
  if (s.toLowerCase().endsWith(API_PREFIX)) s = s.slice(0, -API_PREFIX.length);
  return s;
}

function validateEntries(section, entries, file) {
  if (entries === undefined) return [];
  if (!Array.isArray(entries)) {
    die(2, `${file} 中 "${section}" 应为数组，例如 [{ "name": "xx项目", "id": 1 }]`);
  }
  const seen = new Map();
  for (const entry of entries) {
    if (!entry || typeof entry !== "object" || !Number.isInteger(entry.id) || entry.id <= 0 || typeof entry.name !== "string" || !entry.name.trim()) {
      die(2, `${file} 中 "${section}" 存在非法条目：每条需要 { "name": "非空名称", "id": 正整数 }，收到：${JSON.stringify(entry)}`);
    }
    if (seen.has(entry.name)) {
      console.error(`⚠ "${section}" 中存在重名条目 "${entry.name}"，--name 匹配将取第一个`);
    } else {
      seen.set(entry.name, entry);
    }
  }
  return entries;
}

function loadConfig(opts) {
  const file = requireConfigFile(opts);
  const raw = fs.readFileSync(file, "utf8");
  let parsed;
  try {
    parsed = JSON.parse(stripJsonComments(raw));
  } catch (err) {
    die(2, `${file} 不是合法 JSON（已支持 // 注释）：${err.message}`);
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    die(2, `${file} 顶层应为一个对象`);
  }
  const pat = process.env.BEDROCK_PAT || parsed.pat;
  const baseRaw = process.env.BEDROCK_BASE_URL || parsed.base_url;
  if (!pat || typeof pat !== "string") {
    die(2, `${file} 缺少 "pat"（Bedrock 访问令牌，br_ 开头，可在 Bedrock Web「资源 → 访问令牌」创建）`);
  }
  if (!pat.startsWith("br_")) {
    die(2, `${file} 的 "pat" 应以 br_ 开头（服务端据此识别 PAT），当前值形如 "${String(pat).slice(0, 6)}…"`);
  }
  const base_url = normalizeBaseUrl(baseRaw);
  if (!base_url) {
    die(2, `${file} 缺少 "base_url"（Bedrock 服务器地址，例如 http://192.168.1.10:8080）`);
  }
  const config = { file, pat, base_url, sections: {} };
  for (const def of Object.values(KINDS)) {
    config.sections[def.section] = validateEntries(def.section, parsed[def.section], file);
  }
  return config;
}

// ---------- API 访问 ----------

async function apiRequest(config, method, apiPath, body, scopeHint) {
  const url = `${config.base_url}${API_PREFIX}${apiPath}`;
  let res;
  try {
    res = await fetch(url, {
      method,
      headers: {
        Authorization: `Bearer ${config.pat}`,
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (err) {
    die(1, `无法连接 ${config.base_url}：${err.cause?.code ?? err.message}\n请检查 base_url 与网络（也可用 doctor --remote 验证）。`);
  }
  const text = await res.text();
  let envelope = null;
  try {
    envelope = JSON.parse(text);
  } catch {
    // 非 JSON 响应，下面按原文报错
  }
  if (!res.ok || !envelope || envelope.code !== 0) {
    const message = envelope?.message ?? text.slice(0, 400);
    const lines = [`请求失败 ${res.status} ${method} ${url}`, message];
    if (envelope?.request_id) lines.push(`request_id: ${envelope.request_id}`);
    if (res.status === 401) lines.push("提示：PAT 无效或已过期，检查 pat 是否以 br_ 开头、是否被删除，或在 Bedrock Web 重新生成。");
    if (res.status === 403 && scopeHint) lines.push(`提示：当前 PAT 可能缺少 scope ${scopeHint}。`);
    die(1, lines.join("\n"));
  }
  return envelope.data;
}

async function fetchText(config, apiPath) {
  const url = `${config.base_url}${API_PREFIX}${apiPath}`;
  let res;
  try {
    res = await fetch(url, { headers: { Authorization: `Bearer ${config.pat}` } });
  } catch (err) {
    die(1, `无法连接 ${config.base_url}：${err.cause?.code ?? err.message}`);
  }
  if (!res.ok) die(1, `获取失败 ${res.status} ${url}\n${(await res.text()).slice(0, 400)}`);
  return res.text();
}

// ---------- 任务条目解析 ----------

function listEntries(config) {
  return Object.values(KINDS).flatMap((def) =>
    config.sections[def.section].map((entry) => ({ kind: def.section === "builds" ? "build" : def.section === "scripts" ? "script" : def.section === "pipelines" ? "pipeline" : "agent", def, entry })),
  );
}

function resolveEntry(kind, opts, config) {
  const def = KINDS[kind];
  const entries = config.sections[def.section];
  const available = entries.map((e) => `${e.name}(id=${e.id})`).join("、") || "（空）";
  if (opts.name) {
    const wanted = String(opts.name);
    const exact = entries.filter((e) => e.name === wanted);
    if (exact.length >= 1) return exact[0];
    const fuzzy = entries.filter((e) => e.name.toLowerCase().includes(wanted.toLowerCase()));
    if (fuzzy.length === 1) return fuzzy[0];
    die(2, `"${def.section}" 中找不到唯一匹配 "${wanted}"。已登记：${available}`);
  }
  if (opts.id !== undefined && opts.id !== true) {
    const hit = entries.find((e) => e.id === Number(opts.id));
    if (hit) return hit;
    die(2, `"${def.section}" 中没有 id=${opts.id}。已登记：${available}`);
  }
  if (entries.length === 1) return entries[0];
  die(2, `${def.label}未指定任务（--name / --id），且 "${def.section}" 不止一条或为空。已登记：${available}`);
}

// ---------- 触发与等待 ----------

function buildTriggerBody(kind, opts) {
  if (kind === "build") {
    const body = { trigger_type: "manual" };
    if (opts.branch && opts.branch !== true) body.branch = opts.branch;
    return body;
  }
  if (kind === "pipeline") return { trigger_type: "manual" };
  if (kind === "agent") {
    const body = {};
    if (opts.prompt !== undefined && opts.prompt !== true) body.user_prompt = opts.prompt;
    return body;
  }
  return {};
}

async function triggerRun(kind, entry, opts, config) {
  const def = KINDS[kind];
  const run = await apiRequest(config, "POST", def.triggerPath(entry.id), buildTriggerBody(kind, opts), def.scope);
  const runId = run?.id;
  if (!runId) die(1, `触发响应中缺少 run id：${JSON.stringify(run).slice(0, 300)}`);
  const number = def.runNumberKey && run[def.runNumberKey] ? ` #${run[def.runNumberKey]}` : "";
  out(`[${ts()}] 已触发${def.label}「${entry.name}」 run_id=${runId}${number} status=${run.status ?? "unknown"}`);
  return runId;
}

async function waitRun(kind, runId, opts, config) {
  const def = KINDS[kind];
  const interval = Math.max(1, Number(opts["poll-interval"] ?? 5));
  const timeout = Math.max(interval, Number(opts.timeout ?? 1800));
  const deadline = Date.now() + timeout * 1000;
  let last = null;
  for (;;) {
    const run = await apiRequest(config, "GET", def.statusPath(runId), undefined, def.scope);
    const status = run?.status ?? "unknown";
    if (status !== last) {
      out(`[${ts()}] 状态: ${status}`);
      last = status;
    }
    if (TERMINAL_STATUSES.has(status)) return run;
    if (Date.now() > deadline) {
      die(1, `等待超时（${timeout}s），运行仍在进行。可稍后执行：\n  node ${process.argv[1]} status --type ${kind} --run-id ${runId}${def.hasLog ? `\n  node ${process.argv[1]} log --type ${kind} --run-id ${runId}` : ""}`);
    }
    await sleep(interval * 1000);
  }
}

function printRunSummary(kind, run) {
  const fields = {
    id: run.id,
    status: run.status,
    duration_ms: run.duration_ms,
    error_message: run.error_message,
    artifact_path: run.artifact_path,
  };
  if (kind === "pipeline" && Array.isArray(run.stages)) {
    out("阶段:");
    for (const stage of run.stages) {
      out(`  - ${stage.node_type ?? "?"} status=${stage.status ?? "?"} build_run=${stage.build_run_id ?? "-"} script_run=${stage.script_run_id ?? "-"} agent_run=${stage.agent_run_id ?? "-"}`);
    }
  }
  out(`结果: ${JSON.stringify(Object.fromEntries(Object.entries(fields).filter(([, v]) => v !== undefined && v !== null && v !== "")))}`);
}

async function reportResult(kind, entry, run, opts, config) {
  const def = KINDS[kind];
  const runId = run.id;
  const duration = run.duration_ms !== undefined ? `，耗时 ${(run.duration_ms / 1000).toFixed(1)}s` : "";
  if (run.status === SUCCESS_STATUS) {
    out(`✅ ${def.label}「${entry.name}」成功 run_id=${runId}${duration}`);
    if (kind === "pipeline" && Array.isArray(run.stages)) {
      out("阶段:");
      for (const stage of run.stages) {
        out(`  - ${stage.node_type ?? "?"} status=${stage.status ?? "?"} build_run=${stage.build_run_id ?? "-"} script_run=${stage.script_run_id ?? "-"} agent_run=${stage.agent_run_id ?? "-"}`);
      }
    }
    if (kind === "agent" && run.output_text) {
      out("---- 智能体输出 ----");
      out(run.output_text);
      out("---- 输出结束 ----");
    }
    if (run.artifact_path) out(`制品: ${run.artifact_path}`);
    return;
  }
  out(`❌ ${def.label}「${entry.name}」${run.status} run_id=${runId}${duration}`);
  printRunSummary(kind, run);
  if (def.hasLog) {
    const tail = Math.max(1, Number(opts.tail ?? 80));
    const log = await fetchText(config, def.logPath(runId));
    const lines = log.split("\n");
    out(`---- 日志（最后 ${Math.min(tail, lines.length)} 行 / 共 ${lines.length} 行，完整日志: log --type ${kind} --run-id ${runId}） ----`);
    out(lines.slice(-tail).join("\n"));
  }
  process.exit(1);
}

async function runKind(kind, opts, config) {
  const entry = resolveEntry(kind, opts, config);
  const runId = await triggerRun(kind, entry, opts, config);
  if (opts["no-wait"] === true) {
    out(`已选择 --no-wait，稍后可用 status --type ${kind} --run-id ${runId} 查询`);
    return;
  }
  const run = await waitRun(kind, runId, opts, config);
  await reportResult(kind, entry, run, opts, config);
}

// ---------- 子命令 ----------

function cmdInit(opts) {
  const target = opts.config && opts.config !== true ? path.resolve(opts.config) : path.join(process.cwd(), CONFIG_NAME);
  if (fs.existsSync(target) && opts.force !== true) {
    die(2, `${target} 已存在；直接编辑它即可。如需覆盖重建，加 --force（会清空现有内容）。`);
  }
  fs.writeFileSync(target, TEMPLATE, "utf8");
  const ignored = addGitignore(path.dirname(target));
  out(`已创建 ${target}`);
  out(ignored ? `已确保 ${path.join(path.dirname(target), ".gitignore")} 忽略 .bedrock.json` : ".gitignore 已包含 .bedrock.json，无需修改");
  out("");
  out("下一步：");
  out("  1. 填入 pat（br_ 开头）与 base_url（如 http://192.168.1.10:8080）");
  out("  2. 在 builds / scripts / pipelines / agents 中登记 { name, id }");
  out("     不知道 id？填好 pat 和 base_url 后执行 search 子命令查询，例如：");
  out(`       node ${path.resolve(process.argv[1] ?? "bedrock.mjs")} search --type builds`);
  out(`  3. node ${path.resolve(process.argv[1] ?? "bedrock.mjs")} doctor --remote 校验配置与连通性`);
}

function addGitignore(dir) {
  const file = path.join(dir, ".gitignore");
  let content = "";
  if (fs.existsSync(file)) content = fs.readFileSync(file, "utf8");
  if (content.split(/\r?\n/).some((line) => line.trim() === CONFIG_NAME)) return false;
  if (content && !content.endsWith("\n")) content += "\n";
  content += `${CONFIG_NAME}\n`;
  fs.writeFileSync(file, content, "utf8");
  return true;
}

function cmdDoctor(opts) {
  const file = requireConfigFile(opts);
  const config = loadConfig(opts);
  out(`配置文件: ${file}`);
  out(`✓ pat: ${maskSecret(config.pat)}`);
  out(`✓ base_url: ${config.base_url}`);
  let total = 0;
  for (const def of Object.values(KINDS)) {
    const entries = config.sections[def.section];
    total += entries.length;
    out(`${def.label} (${def.section}): ${entries.length} 条`);
    for (const entry of entries) out(`  - ${entry.name} (id=${entry.id})`);
  }
  if (total === 1) out("提示: 只登记了一个任务，可直接用 run 子命令或 /bedrock 直接运行。");
  if (opts.remote === true) {
    const url = `${config.base_url}${API_PREFIX}/health`;
    out(`远程检查: GET ${url} ...`);
    return fetch(url)
      .then(async (res) => {
        const text = await res.text();
        if (res.ok) out(`✓ 服务器可达: ${text.slice(0, 120)}`);
        else die(1, `✗ 服务器返回 ${res.status}: ${text.slice(0, 200)}`);
      })
      .catch((err) => die(1, `✗ 无法连接 ${url}: ${err.cause?.code ?? err.message}`));
  }
}

async function cmdRun(opts) {
  const config = loadConfig(opts);
  const all = listEntries(config);
  if (all.length === 0) {
    die(2, `${config.file} 尚未登记任何任务。请在 builds / scripts / pipelines / agents 中添加 { name, id }。`);
  }
  if (all.length > 1) {
    out("配置中有多个任务，请指定要运行哪一个：");
    for (const { def, entry } of all) out(`  ${def.label}: ${entry.name} (id=${entry.id}) → ${def.section === "builds" ? "build" : def.section === "scripts" ? "script" : def.section === "pipelines" ? "pipeline" : "agent"} --name "${entry.name}"`);
    process.exit(2);
  }
  const { kind, entry } = all[0];
  out(`配置中只有一个任务：${KINDS[kind].label}「${entry.name}」，直接运行。`);
  await runKind(kind, { ...opts, name: entry.name }, config);
}

async function cmdSearch(opts) {
  const type = typeof opts.type === "string" ? opts.type : undefined;
  if (!type || !SEARCH_TYPES[type]) {
    die(2, "用法: search --type builds|scripts|pipelines|agents [--keyword 关键字]");
  }
  const config = loadConfig(opts);
  const def = KINDS[SEARCH_TYPES[type]];
  const params = new URLSearchParams({ page: "1", page_size: "50" });
  if (opts.keyword && opts.keyword !== true) params.set("keyword", String(opts.keyword));
  const data = await apiRequest(config, "GET", `${def.listPath}?${params}`, undefined, def.scope);
  const items = data?.items ?? [];
  out(`共 ${data?.total ?? items.length} 条${opts.keyword && opts.keyword !== true ? `（关键字: ${opts.keyword}）` : ""}`);
  for (const item of items) {
    const enabled = item.enabled === false ? "禁用" : "启用";
    out(`  id=${item.id}  ${enabled}  ${item.name}`);
  }
}

async function cmdStatus(opts) {
  const type = typeof opts.type === "string" ? opts.type : undefined;
  if (!type || !KINDS[type]) die(2, "用法: status --type build|script|pipeline|agent --run-id N");
  if (opts["run-id"] === undefined || opts["run-id"] === true) die(2, "缺少 --run-id");
  const config = loadConfig(opts);
  const def = KINDS[type];
  const run = await apiRequest(config, "GET", def.statusPath(Number(opts["run-id"])), undefined, def.scope);
  out(JSON.stringify(run, null, 2));
}

async function cmdLog(opts) {
  const type = typeof opts.type === "string" ? opts.type : undefined;
  if (type !== "build" && type !== "script") die(2, "用法: log --type build|script --run-id N [--tail N]（流水线与智能体没有纯文本日志）");
  if (opts["run-id"] === undefined || opts["run-id"] === true) die(2, "缺少 --run-id");
  const config = loadConfig(opts);
  const tail = Math.max(1, Number(opts.tail ?? 200));
  const log = await fetchText(config, KINDS[type].logPath(Number(opts["run-id"])));
  const lines = log.split("\n");
  out(lines.slice(-tail).join("\n"));
}

// ---------- 入口 ----------

function printUsage() {
  out(`Bedrock 客户端 CLI（配置: 项目根目录 ${CONFIG_NAME}，Node.js >= ${MIN_NODE_MAJOR}）

用法: node bedrock.mjs <子命令> [选项]

子命令:
  init               在当前目录创建 ${CONFIG_NAME} 模板并写入 .gitignore（已存在则拒绝，--force 覆盖）
  doctor [--remote]  校验配置完整性；--remote 同时检查服务器连通性
  run                配置中恰好只有一个任务时直接运行；多个/零个时列出供选择
  build              触发构建:    --name 名称 | --id N [--branch 分支]
  script             运行脚本任务: --name 名称 | --id N
  pipeline           运行流水线:   --name 名称 | --id N
  agent              运行智能体:   --name 名称 | --id N [--prompt "任务描述"]
  search             查询服务器任务列表: --type builds|scripts|pipelines|agents [--keyword 关键字]
  status             查询一次运行: --type build|script|pipeline|agent --run-id N
  log                构建/脚本运行日志: --type build|script --run-id N [--tail N]

通用选项:
  --config <路径>        指定配置文件（默认从当前目录向上查找 ${CONFIG_NAME}）
  --no-wait              只触发不等待完成
  --timeout <秒>         等待超时（默认 1800）
  --poll-interval <秒>   轮询间隔（默认 5）

环境变量: BEDROCK_PAT / BEDROCK_BASE_URL 优先于配置文件；BEDROCK_CONFIG 指定配置路径。

任务触发后脚本会轮询到终态（success / failed / cancelled / interrupted），
失败时自动输出日志尾部。退出码: 0 成功, 1 运行失败/请求失败, 2 配置或用法错误。`);
}

const nodeMajor = Number(process.versions.node.split(".")[0]);
if (nodeMajor < MIN_NODE_MAJOR) {
  die(2, `需要 Node.js >= ${MIN_NODE_MAJOR}（当前 ${process.versions.node}）。`);
}

const [command, ...rest] = process.argv.slice(2);
const { opts } = parseArgs(rest);

switch (command) {
  case undefined:
  case "help":
  case "--help":
  case "-h":
    printUsage();
    break;
  case "init":
    cmdInit(opts);
    break;
  case "doctor":
    cmdDoctor(opts);
    break;
  case "run":
    await cmdRun(opts);
    break;
  case "build":
    await runKind("build", opts, loadConfig(opts));
    break;
  case "script":
    await runKind("script", opts, loadConfig(opts));
    break;
  case "pipeline":
    await runKind("pipeline", opts, loadConfig(opts));
    break;
  case "agent":
    await runKind("agent", opts, loadConfig(opts));
    break;
  case "search":
    await cmdSearch(opts);
    break;
  case "status":
    await cmdStatus(opts);
    break;
  case "log":
    await cmdLog(opts);
    break;
  default:
    die(2, `未知子命令 "${command}"\n\n` + "运行 `node bedrock.mjs` 查看用法。");
}
