#!/usr/bin/env node

/**
 * 将 <out> 下 Markdown 镜像推送到 Bedrock 产品项目文档。
 *
 * 典型用法（工作区根，智能体已写入 .env）:
 *   node scripts/push_docs.mjs --slug my-project
 *   node scripts/push_docs.mjs --slug my-project --out output
 *   node scripts/push_docs.mjs --slug my-project --dry-run
 */

import fs from 'fs';
import path from 'path';
import { loadEnvFile } from './lib/env-file.mjs';
import { apiURL, normalizeHost, postJSON, errorMessage } from './lib/http.mjs';
import { resolveOutRoot, displayPath, DEFAULT_OUT } from './lib/out-paths.mjs';

function usage() {
  console.error(`用法: node push_docs.mjs --slug <项目标识> [选项]

把产出根下全部 .md 推送到 Bedrock 产品项目文档（POST /projects/{slug}/docs/push）。
相对 --out 的路径镜像为 api_dir + api_doc_name（如 ic-upms-biz/sys-user.md）。
不推送 .sync.json。

所需环境变量（来自 env 文件或 process.env）:
  PAT             访问令牌（需 docs:write）
  BEDROCK_HOST    服务根地址，无尾斜杠（请求 {host}/api/v1/...）

env 文件加载顺序（第一个存在的）:
  --env-file → $BEDROCK_AGENT_ENV_FILE → $BEDROCK_AGENT_WORKDIR/.env → ./.env
  （文件中的键不覆盖已存在的 process.env，便于本机调试）

选项:
  --slug <id>           产品项目标识（必需；路径参数，可为数字 ID 或 slug）
  --out <dir>           输出根目录（默认: 自动发现已有 output/api-docs，否则 ${DEFAULT_OUT}）
  --env-file <path>     显式指定 .env 路径（也可用 --env-file=path）
  --dry-run             只列出将推送的文件，不发请求
  -h, --help            显示帮助

注意:
  - PAT 还须满足目标项目的成员 ACL；token 勿写入技能目录
  - Node ≥20 内置同名选项：若路径可能不存在，请用
    node -- scripts/push_docs.mjs --env-file <path> ...`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = {
    slug: null,
    out: null,
    envFile: null,
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
    } else if (a === '--env-file' || a === '--envFile') {
      opts.envFile = args[++i];
    } else if (a.startsWith('--env-file=')) {
      opts.envFile = a.slice('--env-file='.length);
    } else if (a.startsWith('--envFile=')) {
      opts.envFile = a.slice('--envFile='.length);
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
  return {
    slug: String(opts.slug).trim(),
    out: opts.out,
    envFile: opts.envFile,
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

  const items = files.map((abs) => {
    const mapped = toApiPath(outRoot, abs);
    return {
      file: displayPath(cwd, abs),
      abs,
      api_dir: mapped.api_dir,
      api_doc_name: mapped.api_doc_name,
      rel: mapped.rel,
    };
  });

  const pushed = [];
  const failed = [];

  if (opts.dryRun) {
    for (const it of items) {
      pushed.push({
        file: it.file,
        api_dir: it.api_dir,
        api_doc_name: it.api_doc_name,
        dryRun: true,
      });
    }
    process.stdout.write(
      `${JSON.stringify(
        {
          dryRun: true,
          slug: opts.slug,
          outRoot: displayPath(cwd, outRoot),
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

    try {
      const result = await pushOne({
        host,
        pat,
        slug: opts.slug,
        apiDir: it.api_dir,
        apiDocName: it.api_doc_name,
        apiDoc: content,
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

  process.stdout.write(
    `${JSON.stringify(
      {
        dryRun: false,
        slug: opts.slug,
        outRoot: displayPath(cwd, outRoot),
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
