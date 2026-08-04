#!/usr/bin/env node

/**
 * 将本地目录下 Markdown 镜像推送到 Bedrock 项目「开发文档」。
 *
 * 典型用法:
 *   node scripts/push_docs.mjs --slug my-project --dir ./docs
 *   bun scripts/push_docs.mjs --slug my-project --dir ./docs --prefix guides
 *   node scripts/push_docs.mjs --slug my-project --dir ./docs --dry-run
 */

import fs from 'fs';
import path from 'path';

// ── env file ───────────────────────────────────────────────────────────────

function parseEnvText(text) {
  const out = {};
  const lines = String(text || '').replace(/\r\n/g, '\n').split('\n');
  for (const raw of lines) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq <= 0) continue;
    const key = line.slice(0, eq).trim();
    if (!key || key.includes('\n')) continue;
    let value = line.slice(eq + 1);
    if (!value.startsWith('"') && !value.startsWith("'")) {
      const hash = value.indexOf(' #');
      if (hash >= 0) value = value.slice(0, hash);
      value = value.trim();
    } else {
      value = value.trim();
    }
    value = unquoteEnvValue(value);
    out[key] = value;
  }
  return out;
}

function unquoteEnvValue(value) {
  if (value.length >= 2) {
    const q = value[0];
    if ((q === '"' || q === "'") && value[value.length - 1] === q) {
      const inner = value.slice(1, -1);
      if (q === '"') {
        return inner
          .replace(/\\n/g, '\n')
          .replace(/\\r/g, '\r')
          .replace(/\\t/g, '\t')
          .replace(/\\\\/g, '\\')
          .replace(/\\"/g, '"');
      }
      return inner;
    }
  }
  return value;
}

function resolveEnvFilePath(envFileOpt, opts = {}) {
  const env = opts.env || process.env;
  const cwd = path.resolve(opts.cwd || process.cwd());
  const candidates = [];

  const explicit =
    envFileOpt != null && String(envFileOpt).trim() ? String(envFileOpt).trim() : null;
  if (explicit) {
    candidates.push(path.isAbsolute(explicit) ? path.resolve(explicit) : path.resolve(cwd, explicit));
  }
  if (env.BEDROCK_AGENT_ENV_FILE && String(env.BEDROCK_AGENT_ENV_FILE).trim()) {
    const p = String(env.BEDROCK_AGENT_ENV_FILE).trim();
    candidates.push(path.isAbsolute(p) ? path.resolve(p) : path.resolve(cwd, p));
  }
  if (env.BEDROCK_AGENT_WORKDIR && String(env.BEDROCK_AGENT_WORKDIR).trim()) {
    candidates.push(path.resolve(String(env.BEDROCK_AGENT_WORKDIR).trim(), '.env'));
  }
  candidates.push(path.join(cwd, '.env'));

  const seen = new Set();
  const unique = [];
  for (const c of candidates) {
    if (seen.has(c)) continue;
    seen.add(c);
    unique.push(c);
  }

  for (const p of unique) {
    try {
      if (fs.existsSync(p) && fs.statSync(p).isFile()) {
        return { path: p, candidates: unique };
      }
    } catch {
      // ignore
    }
  }
  return { path: null, candidates: unique };
}

function loadEnvFile(envFileOpt, opts = {}) {
  const target = opts.target || process.env;
  const lookupEnv = opts.env || process.env;
  const resolved = resolveEnvFilePath(envFileOpt, { cwd: opts.cwd, env: lookupEnv });
  if (!resolved.path) {
    return { loaded: false, path: null, keys: [], candidates: resolved.candidates };
  }
  const text = fs.readFileSync(resolved.path, 'utf8');
  const parsed = parseEnvText(text);
  const keys = [];
  for (const [k, v] of Object.entries(parsed)) {
    if (Object.prototype.hasOwnProperty.call(target, k) && target[k] !== undefined) {
      continue;
    }
    target[k] = v;
    keys.push(k);
  }
  keys.sort();
  return { loaded: true, path: resolved.path, keys, candidates: resolved.candidates };
}

// ── HTTP ───────────────────────────────────────────────────────────────────

function normalizeHost(host) {
  const raw = String(host || '').trim();
  if (!raw) throw new Error('BEDROCK_HOST 不能为空');
  return raw.replace(/\/+$/, '');
}

function apiURL(host, apiPath) {
  const base = normalizeHost(host);
  const p = apiPath.startsWith('/') ? apiPath : `/${apiPath}`;
  return `${base}/api/v1${p}`;
}

async function postJSON(url, opts) {
  const token = String(opts.token || '').trim();
  if (!token) throw new Error('PAT 不能为空');
  const fetchImpl = opts.fetchImpl || globalThis.fetch;
  if (typeof fetchImpl !== 'function') {
    throw new Error('当前运行时无 fetch；请使用 Node ≥ 18 或 bun');
  }
  const res = await fetchImpl(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(opts.body ?? {}),
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { raw: text };
    }
  }
  return { ok: res.ok, status: res.status, data, text };
}

function rawErrorMessage(res) {
  const d = res.data;
  if (d && typeof d === 'object') {
    const obj = /** @type {Record<string, unknown>} */ (d);
    if (typeof obj.message === 'string' && obj.message) return obj.message;
    if (typeof obj.error === 'string' && obj.error) return obj.error;
    if (obj.error && typeof obj.error === 'object') {
      const e = /** @type {Record<string, unknown>} */ (obj.error);
      if (typeof e.message === 'string' && e.message) return e.message;
    }
  }
  if (res.text) return res.text.slice(0, 200);
  return `HTTP ${res.status}`;
}

/**
 * 针对 401 / 403 / scope 给出可操作提示。
 * @param {{ status: number, data: unknown, text: string }} res
 */
function formatPushError(res) {
  const msg = rawErrorMessage(res);
  if (res.status === 401) {
    return `认证失败 (401): ${msg}。请检查 PAT 是否有效、是否以 Authorization: Bearer 传递。`;
  }
  if (res.status === 403) {
    const scopeHint = /scope/i.test(msg)
      ? '令牌缺少 `dev_docs:write` scope。'
      : '可能是 scope 不足（需 `dev_docs:write`）或令牌属主不满足目标项目成员 ACL。';
    return `权限不足 (403): ${msg}。${scopeHint}`;
  }
  if (res.status === 404) {
    return `未找到 (404): ${msg}。请确认 --slug 对应的项目 ID/slug 存在。`;
  }
  return msg;
}

// ── CLI ────────────────────────────────────────────────────────────────────

function usage() {
  console.error(`用法: node push_docs.mjs --slug <项目标识> --dir <本地目录> [选项]

把本地目录下全部 .md 推送到 Bedrock 项目开发文档（POST /projects/{slug}/dev-docs/push）。
相对 --dir 的路径镜像为 doc_dir + doc_name；可用 --prefix 加远程根前缀。
跳过隐藏目录/文件（名以 . 开头）。

所需环境变量（来自 env 文件或 process.env）:
  PAT             访问令牌（需 dev_docs:write）
  BEDROCK_HOST    服务根地址，无尾斜杠（请求 {host}/api/v1/...）

可选环境变量（CLI 优先）:
  BEDROCK_PROJECT / PROJECT_SLUG   项目 ID 或 slug
  DEV_DOCS_DIR                     本地源目录
  DEV_DOCS_PREFIX                  远程根前缀

env 文件加载顺序（第一个存在的）:
  --env-file → $BEDROCK_AGENT_ENV_FILE → $BEDROCK_AGENT_WORKDIR/.env → ./.env
  （文件中的键不覆盖已存在的 process.env，便于本机调试）

选项:
  --slug <id>           产品项目标识（必需；路径参数，可为数字 ID 或 slug）
  --dir <dir>           本地 Markdown 根目录（必需）
  --prefix <path>       远程 doc_dir 前缀（可选；如 guides）
  --env-file <path>     显式指定 .env 路径（也可用 --env-file=path）
  --dry-run             只列出将推送的文件，不发请求
  -h, --help            显示帮助

注意:
  - PAT 还须满足目标项目的成员 ACL；token 勿写入技能目录
  - 可用 bun 或 node 运行
  - Node ≥20 内置同名选项：若路径可能不存在，请用
    node -- scripts/push_docs.mjs --env-file <path> ...`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = {
    slug: null,
    dir: null,
    prefix: null,
    envFile: null,
    dryRun: false,
  };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--slug') {
      opts.slug = args[++i];
    } else if (a.startsWith('--slug=')) {
      opts.slug = a.slice('--slug='.length);
    } else if (a === '--dir' || a === '--source') {
      opts.dir = args[++i];
    } else if (a.startsWith('--dir=')) {
      opts.dir = a.slice('--dir='.length);
    } else if (a.startsWith('--source=')) {
      opts.dir = a.slice('--source='.length);
    } else if (a === '--prefix' || a === '--remote-root') {
      opts.prefix = args[++i];
    } else if (a.startsWith('--prefix=')) {
      opts.prefix = a.slice('--prefix='.length);
    } else if (a.startsWith('--remote-root=')) {
      opts.prefix = a.slice('--remote-root='.length);
    } else if (a === '--env-file' || a === '--envFile') {
      opts.envFile = args[++i];
    } else if (a.startsWith('--env-file=')) {
      opts.envFile = a.slice('--env-file='.length);
    } else if (a.startsWith('--envFile=')) {
      opts.envFile = a.slice('--envFile='.length);
    } else if (a === '--dry-run') {
      opts.dryRun = true;
    } else if (a === '-h' || a === '--help') {
      usage();
    } else if (a.startsWith('-')) {
      console.error(`未知参数: ${a}`);
      usage();
    } else {
      console.error(`未知参数: ${a}`);
      usage();
    }
  }
  return {
    slug: opts.slug != null ? String(opts.slug).trim() : null,
    dir: opts.dir != null ? String(opts.dir).trim() : null,
    prefix: opts.prefix != null ? String(opts.prefix).trim() : null,
    envFile: opts.envFile,
    dryRun: opts.dryRun,
  };
}

function displayPath(cwd, abs) {
  const rel = path.relative(cwd, abs);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) return abs;
  return rel || '.';
}

/**
 * 规范化远程前缀：去首尾 /，拒绝 .. / 绝对路径 / 空段。
 * @param {string|null|undefined} raw
 * @returns {string}
 */
function normalizePrefix(raw) {
  if (raw == null || !String(raw).trim()) return '';
  let p = String(raw).trim().replace(/\\/g, '/');
  if (p.startsWith('/')) {
    throw new Error(`--prefix 不能是绝对路径: ${raw}`);
  }
  const parts = p.split('/').filter((s) => s.length > 0);
  if (!parts.length) return '';
  for (const seg of parts) {
    if (seg === '.' || seg === '..') {
      throw new Error(`--prefix 含非法段: ${raw}`);
    }
  }
  return parts.join('/');
}

/**
 * 递归收集 dir 下全部 .md；跳过隐藏目录/文件（名以 . 开头）。
 * @param {string} dirRoot
 * @returns {string[]} 绝对路径，已排序
 */
function collectMarkdownFiles(dirRoot) {
  const abs = path.resolve(dirRoot);
  if (!fs.existsSync(abs) || !fs.statSync(abs).isDirectory()) {
    throw new Error(`源目录不存在或不是目录: ${abs}`);
  }
  const out = [];
  const stack = [abs];
  while (stack.length) {
    const cur = stack.pop();
    let entries;
    try {
      entries = fs.readdirSync(cur, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const ent of entries) {
      if (ent.name.startsWith('.')) continue;
      const full = path.join(cur, ent.name);
      if (ent.isDirectory()) {
        stack.push(full);
      } else if (ent.isFile() && /\.md$/i.test(ent.name)) {
        out.push(full);
      }
    }
  }
  out.sort();
  return out;
}

/**
 * 相对 dir 的路径 → { doc_dir, doc_name }（正斜杠），可选 prefix。
 * `guides/arch.md` → doc_dir="guides"；`README.md` → doc_dir=""。
 * prefix="product" + `guides/arch.md` → doc_dir="product/guides"。
 * @param {string} dirRoot
 * @param {string} absFile
 * @param {string} prefix
 */
function toDocPath(dirRoot, absFile, prefix) {
  const rel = path.relative(dirRoot, absFile);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) {
    throw new Error(`文件不在源目录下: ${absFile}`);
  }
  const parts = rel.split(path.sep).filter(Boolean);
  if (!parts.length) throw new Error(`空相对路径: ${absFile}`);
  const docName = parts[parts.length - 1];
  const localDir = parts.slice(0, -1).join('/');
  const docDir = [prefix, localDir].filter(Boolean).join('/');
  return { doc_dir: docDir, doc_name: docName, rel: parts.join('/') };
}

async function pushOne({ host, pat, slug, docDir, docName, content }) {
  const enc = encodeURIComponent(slug);
  const pushURL = apiURL(host, `/projects/${enc}/dev-docs/push`);
  const pushRes = await postJSON(pushURL, {
    token: pat,
    body: {
      doc_dir: docDir,
      doc_name: docName,
      content,
    },
  });
  if (!pushRes.ok) {
    return {
      ok: false,
      status: pushRes.status,
      error: formatPushError(pushRes),
    };
  }
  return { ok: true, status: pushRes.status };
}

async function main() {
  const opts = parseArgs(process.argv);
  const envInfo = loadEnvFile(opts.envFile);

  const pat = (process.env.PAT || '').trim();
  let host;
  try {
    host = normalizeHost(process.env.BEDROCK_HOST || '');
  } catch {
    host = '';
  }

  const slug =
    opts.slug ||
    (process.env.BEDROCK_PROJECT || '').trim() ||
    (process.env.PROJECT_SLUG || '').trim() ||
    '';
  const dir = opts.dir || (process.env.DEV_DOCS_DIR || '').trim() || '';
  let prefix;
  try {
    prefix = normalizePrefix(
      opts.prefix != null && opts.prefix !== ''
        ? opts.prefix
        : process.env.DEV_DOCS_PREFIX || '',
    );
  } catch (err) {
    console.error(err.message || err);
    process.exit(1);
  }

  if (!slug) {
    console.error('缺少 --slug（或环境变量 BEDROCK_PROJECT / PROJECT_SLUG）');
    usage();
  }
  if (!dir) {
    console.error('缺少 --dir（或环境变量 DEV_DOCS_DIR）');
    usage();
  }

  if (!pat || !host) {
    const missing = [];
    if (!pat) missing.push('PAT');
    if (!host) missing.push('BEDROCK_HOST');
    console.error(
      `缺少环境变量: ${missing.join(', ')}` +
        (envInfo.loaded ? `（已加载 ${envInfo.path}）` : '（未找到可用的 .env）'),
    );
    if (!envInfo.loaded) {
      console.error('候选路径:', envInfo.candidates.join(' | '));
    }
    process.exit(1);
  }

  const dirRoot = path.resolve(dir);
  const cwd = process.cwd();

  let files;
  try {
    files = collectMarkdownFiles(dirRoot);
  } catch (err) {
    console.error(err.message || err);
    process.exit(1);
  }

  const items = files.map((abs) => {
    const mapped = toDocPath(dirRoot, abs, prefix);
    return {
      file: displayPath(cwd, abs),
      abs,
      doc_dir: mapped.doc_dir,
      doc_name: mapped.doc_name,
      rel: mapped.rel,
    };
  });

  const pushed = [];
  const failed = [];

  if (opts.dryRun) {
    for (const it of items) {
      pushed.push({
        file: it.file,
        doc_dir: it.doc_dir,
        doc_name: it.doc_name,
        dryRun: true,
      });
    }
    process.stdout.write(
      `${JSON.stringify(
        {
          dryRun: true,
          slug,
          dirRoot: displayPath(cwd, dirRoot),
          prefix: prefix || null,
          envFile: envInfo.loaded ? displayPath(cwd, envInfo.path) : null,
          host,
          pushed,
          failed,
          summary: {
            total: items.length,
            ok: items.length,
            failed: 0,
          },
        },
        null,
        2,
      )}\n`,
    );
    return;
  }

  for (const it of items) {
    let content;
    try {
      content = fs.readFileSync(it.abs, 'utf8');
    } catch (err) {
      failed.push({
        file: it.file,
        doc_dir: it.doc_dir,
        doc_name: it.doc_name,
        error: `读文件失败: ${err.message}`,
      });
      continue;
    }
    if (content === '') {
      failed.push({
        file: it.file,
        doc_dir: it.doc_dir,
        doc_name: it.doc_name,
        error: 'content 不能为空',
      });
      continue;
    }

    try {
      const result = await pushOne({
        host,
        pat,
        slug,
        docDir: it.doc_dir,
        docName: it.doc_name,
        content,
      });
      if (result.ok) {
        pushed.push({
          file: it.file,
          doc_dir: it.doc_dir,
          doc_name: it.doc_name,
          status: result.status,
        });
      } else {
        failed.push({
          file: it.file,
          doc_dir: it.doc_dir,
          doc_name: it.doc_name,
          status: result.status,
          error: result.error,
        });
      }
    } catch (err) {
      failed.push({
        file: it.file,
        doc_dir: it.doc_dir,
        doc_name: it.doc_name,
        error: err.message || String(err),
      });
    }
  }

  process.stdout.write(
    `${JSON.stringify(
      {
        dryRun: false,
        slug,
        dirRoot: displayPath(cwd, dirRoot),
        prefix: prefix || null,
        envFile: envInfo.loaded ? displayPath(cwd, envInfo.path) : null,
        host,
        pushed,
        failed,
        summary: {
          total: items.length,
          ok: pushed.length,
          failed: failed.length,
        },
      },
      null,
      2,
    )}\n`,
  );

  if (failed.length) process.exit(1);
}

main().catch((err) => {
  console.error(err.message || err);
  process.exit(1);
});
