#!/usr/bin/env node

/**
 * 将 <out> 下 Markdown 增量推送到 Bedrock 产品项目文档。
 *
 * 默认：GET /docs/export 比对远程，只推新建或内容有变化的文件。
 *   node scripts/push_docs.mjs --slug my-project
 *   node scripts/push_docs.mjs --slug my-project --out output
 *   node scripts/push_docs.mjs --slug my-project --docs ic-upms-biz/sys-user.md --dry-run
 * 全量覆盖（跳过比对）:
 *   node scripts/push_docs.mjs --slug my-project --all
 */

import fs from 'fs';
import path from 'path';
import { loadEnvFile } from './lib/env-file.mjs';
import { apiURL, normalizeHost, postJSON, getJSON, errorMessage } from './lib/http.mjs';
import { resolveOutRoot, displayPath, DEFAULT_OUT } from './lib/out-paths.mjs';

function usage() {
  console.error(`用法: node push_docs.mjs --slug <项目标识> [选项]

把产出根下 .md 推送到 Bedrock 产品项目文档（POST /projects/{slug}/docs/push）。
默认先 GET /docs/export 比对远程，只推新建或内容有变化的文件；内容相同列入 skipped。
相对 --out 的路径镜像为 api_dir + api_doc_name（如 ic-upms-biz/sys-user.md）。
不推送 .sync.json。

所需环境变量（来自 env 文件或 process.env）:
  PAT             访问令牌（需 docs:write；默认比对还需要 docs:read）
  BEDROCK_HOST    服务根地址，无尾斜杠（请求 {host}/api/v1/...）

env 文件加载顺序（第一个存在的）:
  --env-file → $BEDROCK_AGENT_ENV_FILE → $BEDROCK_AGENT_WORKDIR/.env → ./.env
  （文件中的键不覆盖已存在的 process.env，便于本机调试）

选项:
  --slug <id>           产品项目标识（必需；路径参数，可为数字 ID 或 slug）
  --out <dir>           输出根目录（默认: 自动发现已有 output/api-docs，否则 ${DEFAULT_OUT}）
  --docs a.md,b.md      只考虑这些文件（相对 --out，或 basename）；再与远程比对
  --all                 跳过远程比对，推送候选全部（无变化也覆盖）
  --env-file <path>     显式指定 .env 路径（也可用 --env-file=path）
  --dry-run             只列出将推送 / 跳过的文件，不发 push 请求（仍会 export 比对，除非 --all）
  -h, --help            显示帮助

注意:
  - PAT 还须满足目标项目的成员 ACL；token 勿写入技能目录
  - Node ≥20 内置同名选项：若路径可能不存在，请用
    node -- scripts/push_docs.mjs --env-file <path> ...`);
  process.exit(2);
}

function parseDocsList(raw) {
  return String(raw || '')
    .split(',')
    .map((s) => s.trim().replace(/\\/g, '/').replace(/^\.\//, ''))
    .filter(Boolean);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = {
    slug: null,
    out: null,
    envFile: null,
    docs: null,
    all: false,
    dryRun: false,
  };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--slug') {
      opts.slug = args[++i];
    } else if (a.startsWith('--slug=')) {
      opts.slug = a.slice('--slug='.length);
    } else if (a === '--out') {
      opts.out = args[++i];
    } else if (a.startsWith('--out=')) {
      opts.out = a.slice('--out='.length);
    } else if (a === '--docs') {
      opts.docs = parseDocsList(args[++i] || '');
    } else if (a.startsWith('--docs=')) {
      opts.docs = parseDocsList(a.slice('--docs='.length));
    } else if (a === '--doc') {
      if (!opts.docs) opts.docs = [];
      opts.docs.push(...parseDocsList(args[++i] || ''));
    } else if (a === '--env-file' || a === '--envFile') {
      opts.envFile = args[++i];
    } else if (a.startsWith('--env-file=')) {
      opts.envFile = a.slice('--env-file='.length);
    } else if (a.startsWith('--envFile=')) {
      opts.envFile = a.slice('--envFile='.length);
    } else if (a === '--all') {
      opts.all = true;
    } else if (a === '--publish') {
      console.error('警告: --publish 已移除（文档不再有发布概念），已忽略');
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
  if (!opts.slug || !String(opts.slug).trim()) {
    console.error('缺少 --slug');
    usage();
  }
  if (opts.docs) {
    opts.docs = [...new Set(opts.docs)];
    if (!opts.docs.length) {
      console.error('--docs 不能为空');
      usage();
    }
  }
  return {
    slug: String(opts.slug).trim(),
    out: opts.out,
    envFile: opts.envFile,
    docs: opts.docs,
    all: opts.all,
    dryRun: opts.dryRun,
  };
}

/**
 * 递归收集 out 下全部 .md；跳过隐藏目录/文件（名以 . 开头）。
 * @param {string} outRoot
 * @returns {string[]} 绝对路径，已排序
 */
function collectMarkdownFiles(outRoot) {
  const abs = path.resolve(outRoot);
  if (!fs.existsSync(abs) || !fs.statSync(abs).isDirectory()) {
    throw new Error(`产出根不存在或不是目录: ${abs}`);
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
 * 相对 out 的路径 → { api_dir, api_doc_name }（正斜杠）。
 * `_conventions.md` → api_dir=""；`ic-upms-biz/sys-user.md` → api_dir="ic-upms-biz"。
 * @param {string} outRoot
 * @param {string} absFile
 */
function toApiPath(outRoot, absFile) {
  const rel = path.relative(outRoot, absFile);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) {
    throw new Error(`文件不在产出根下: ${absFile}`);
  }
  const parts = rel.split(path.sep).filter(Boolean);
  if (!parts.length) throw new Error(`空相对路径: ${absFile}`);
  const apiDocName = parts[parts.length - 1];
  const apiDir = parts.slice(0, -1).join('/');
  return { api_dir: apiDir, api_doc_name: apiDocName, rel: parts.join('/') };
}

function itemMatchesDocSpec(item, spec) {
  const s = String(spec || '').replace(/\\/g, '/').replace(/^\.\//, '');
  if (!s) return false;
  return item.rel === s || item.api_doc_name === s;
}

function filterItemsByDocs(items, docs) {
  if (!docs) return { items, missing: [] };
  const wanted = new Set();
  const missing = [];
  for (const spec of docs) {
    const hits = items.filter((it) => itemMatchesDocSpec(it, spec));
    if (!hits.length) missing.push(spec);
    for (const hit of hits) wanted.add(hit.rel);
  }
  return { items: items.filter((it) => wanted.has(it.rel)), missing };
}

/**
 * 从 docs/export 信封取出 path → content。
 * @param {{ data: unknown }} res
 * @returns {Map<string, string>}
 */
function remoteMapFromExport(res) {
  const map = new Map();
  const body = res.data && typeof res.data === 'object' ? res.data : null;
  const payload = body && body.data && typeof body.data === 'object' ? body.data : null;
  const list = payload && Array.isArray(payload.items) ? payload.items : [];
  for (const it of list) {
    if (!it || typeof it !== 'object') continue;
    const p = String(it.path || '').replace(/\\/g, '/');
    if (!p) continue;
    map.set(p, typeof it.content === 'string' ? it.content : '');
  }
  return map;
}

async function fetchRemoteMap({ host, pat, slug }) {
  const enc = encodeURIComponent(slug);
  const url = apiURL(host, `/projects/${enc}/docs/export`);
  const res = await getJSON(url, { token: pat });
  if (!res.ok) {
    const hint =
      res.status === 403
        ? '默认推送会先 GET /docs/export 比对远程。请给 PAT 加上 docs:read，或显式 --all 强制全量。'
        : '';
    throw new Error(
      `读取远程文档失败 (${res.status}): ${errorMessage(res)}${hint ? `。${hint}` : ''}`,
    );
  }
  return remoteMapFromExport(res);
}

async function pushOne({ host, pat, slug, apiDir, apiDocName, apiDoc }) {
  const enc = encodeURIComponent(slug);
  const pushURL = apiURL(host, `/projects/${enc}/docs/push`);
  const pushRes = await postJSON(pushURL, {
    token: pat,
    body: {
      api_dir: apiDir,
      api_doc_name: apiDocName,
      api_doc: apiDoc,
    },
  });
  if (!pushRes.ok) {
    return {
      ok: false,
      status: pushRes.status,
      error: errorMessage(pushRes),
    };
  }
  return { ok: true, status: pushRes.status };
}

function printResult(payload) {
  process.stdout.write(`${JSON.stringify(payload, null, 2)}\n`);
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

  const outRoot = resolveOutRoot(opts.out, { discover: true });
  const cwd = process.cwd();

  let files;
  try {
    files = collectMarkdownFiles(outRoot);
  } catch (err) {
    console.error(err.message || err);
    process.exit(1);
  }

  const allItems = files.map((abs) => {
    const mapped = toApiPath(outRoot, abs);
    return {
      file: displayPath(cwd, abs),
      abs,
      api_dir: mapped.api_dir,
      api_doc_name: mapped.api_doc_name,
      rel: mapped.rel,
    };
  });

  const filtered = filterItemsByDocs(allItems, opts.docs);
  if (filtered.missing.length) {
    console.error(`--docs 未匹配到本地文件: ${filtered.missing.join(', ')}`);
    process.exit(1);
  }
  const items = filtered.items;

  let remoteMap = null;
  if (!opts.all) {
    try {
      remoteMap = await fetchRemoteMap({ host, pat, slug: opts.slug });
    } catch (err) {
      console.error(err.message || err);
      process.exit(1);
    }
  }

  const skipped = [];
  const failed = [];
  const queued = [];

  for (const it of items) {
    if (opts.all && opts.dryRun) {
      queued.push(it);
      continue;
    }
    let content;
    try {
      content = fs.readFileSync(it.abs, 'utf8');
    } catch (err) {
      failed.push({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        error: `读文件失败: ${err.message}`,
      });
      continue;
    }
    if (content === '') {
      failed.push({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        error: 'api_doc 不能为空',
      });
      continue;
    }
    if (remoteMap && remoteMap.get(it.rel) === content) {
      skipped.push({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        reason: 'unchanged',
      });
      continue;
    }
    queued.push({ ...it, content });
  }

  const baseOut = {
    slug: opts.slug,
    outRoot: displayPath(cwd, outRoot),
    envFile: envInfo.loaded ? displayPath(cwd, envInfo.path) : null,
    host,
    incremental: !opts.all,
  };

  if (opts.dryRun) {
    printResult({
      ...baseOut,
      dryRun: true,
      pushed: queued.map((it) => ({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        dryRun: true,
      })),
      skipped,
      failed,
      summary: {
        total: items.length,
        ok: queued.length,
        skipped: skipped.length,
        failed: failed.length,
      },
    });
    if (failed.length) process.exit(1);
    return;
  }

  const pushed = [];
  for (const it of queued) {
    try {
      const result = await pushOne({
        host,
        pat,
        slug: opts.slug,
        apiDir: it.api_dir,
        apiDocName: it.api_doc_name,
        apiDoc: it.content,
      });
      if (result.ok) {
        pushed.push({
          file: it.file,
          api_dir: it.api_dir,
          api_doc_name: it.api_doc_name,
          status: result.status,
        });
      } else {
        failed.push({
          file: it.file,
          api_dir: it.api_dir,
          api_doc_name: it.api_doc_name,
          status: result.status,
          error: result.error,
        });
      }
    } catch (err) {
      failed.push({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        error: err.message || String(err),
      });
    }
  }

  printResult({
    ...baseOut,
    dryRun: false,
    pushed,
    skipped,
    failed,
    summary: {
      total: items.length,
      ok: pushed.length,
      skipped: skipped.length,
      failed: failed.length,
    },
  });

  if (failed.length) process.exit(1);
}

main().catch((err) => {
  console.error(err.message || err);
  process.exit(1);
});
