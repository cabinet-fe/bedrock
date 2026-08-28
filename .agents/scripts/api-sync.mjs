#!/usr/bin/env node
// API 契约先行检查：写接口前先写 api/ 契约文档。
// 硬性（exit 1，阻止提交）：
//   - internal/<域>/handler/*.go 变更时，对应 api/<域>.md 必须同步变更
//   - internal/pkg/response.go（响应信封）变更时，API-SPEC.md 必须同步变更
//   - handler 中新增的路由路径必须已出现在某个 api/*.md 中
// 软提醒（仅输出，不阻止）：
//   - service / model / ws / engine / cmd/server 变更，建议核对契约
// 用法：
//   node api-sync.mjs               # 检查已暂存变更（pre-commit 钩子调用）
//   node api-sync.mjs --worktree    # 包含未暂存变更（开发中手动检查）
//
// 安装：make install-hooks（git config core.hooksPath .githooks）
// 手动：make check-api-contracts

import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';

process.stdout.on('error', (err) => {
  if (err.code === 'EPIPE') process.exit(0);
});

const ROOT = execSync('git rev-parse --show-toplevel', { encoding: 'utf8' }).trim();
const WORKTREE = process.argv.includes('--worktree');
const DIFF_BASE = WORKTREE ? 'HEAD' : '--cached';

// 域 → 契约文档。handler 变更必须同步其中至少一个。
const DOMAIN_DOCS = {
  auth: ['api/auth.md'],
  system: ['api/system.md'],
  rbac: ['api/system.md'],
  resource: ['api/resource.md'],
  cicd: ['api/cicd.md', 'api/cicd-scripts.md'],
  engine: ['api/cicd.md'],
  ops: ['api/ops.md'],
  dashboard: ['api/ops.md'],
  project: ['api/project.md'],
  ai: ['api/ai.md'],
  dsh: ['api/dsh.md'],
};

// 特殊文件 → 必须同步的文档（响应信封）。
const HARD_FILES = {
  'internal/pkg/response.go': ['API-SPEC.md'],
};

// 软提醒前缀：改了建议核对契约，但不阻止。
const SOFT_PREFIXES = [
  'internal/ws/',
  'internal/engine/',
  'cmd/server/',
];

function git(args) {
  return execSync(`git ${args.join(' ')}`, { cwd: ROOT, encoding: 'utf8' });
}

function changedFiles() {
  return git(['diff', DIFF_BASE, '--name-only', '--diff-filter=ACMR'])
    .split('\n').map((s) => s.trim()).filter(Boolean);
}

// 解析 -U0 diff，返回「新增行在现文件中的行号」集合（1-based）。
function addedLineNumbers(file) {
  let out;
  try {
    out = git(['diff', DIFF_BASE, '-U0', '--', file]);
  } catch {
    return new Set();
  }
  const added = new Set();
  let newLine = 0;
  for (const line of out.split('\n')) {
    const hunk = line.match(/^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    if (hunk) {
      newLine = Number(hunk[1]);
      continue;
    }
    if (line.startsWith('+')) {
      if (!line.startsWith('+++')) added.add(newLine);
      newLine += 1;
    } else if (line.startsWith('-') || line.startsWith('\\')) {
      // 旧侧行/无行号标记，不推进新侧行号
    } else {
      newLine += 1;
    }
  }
  return added;
}

// 提取 handler 中「本次新增」的路由注册（跟随 Group 前缀还原完整路径）。
function extractNewRoutes(file) {
  let content;
  try {
    content = fs.readFileSync(path.join(ROOT, file), 'utf8');
  } catch {
    return [];
  }
  const added = addedLineNumbers(file);
  if (added.size === 0) return [];
  const lines = content.split('\n');
  const routes = [];
  let group = '';
  const GROUP_RE = /\.Group\(\s*["'`]([^"'`]*)["'`]/;
  const ROUTE_RE = /\.(GET|POST|PUT|DELETE|PATCH)\(\s*["'`]([^"'`]*)["'`]/;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const gm = line.match(GROUP_RE);
    if (gm) {
      group = gm[1];
      continue;
    }
    const rm = line.match(ROUTE_RE);
    if (rm && added.has(i + 1)) {
      const full = normalizePath(group + rm[2]);
      if (full) routes.push({ line: i + 1, method: rm[1], path: full });
    }
  }
  return routes;
}

function normalizePath(p) {
  let out = p
    .replace(/^\/api\/v1/, '')
    .replace(/:([A-Za-z_][A-Za-z0-9_]*)/g, '{$1}')
    .replace(/\/+$/, '');
  return out.startsWith('/') ? out : '';
}

function apiDocsContent() {
  const dir = path.join(ROOT, 'api');
  const docs = {};
  for (const name of fs.readdirSync(dir)) {
    if (name.endsWith('.md')) {
      try {
        docs[`api/${name}`] = fs.readFileSync(path.join(dir, name), 'utf8');
      } catch { /* 忽略读取失败 */ }
    }
  }
  return docs;
}

function main() {
  const files = changedFiles();
  if (files.length === 0) {
    process.stdout.write('OK: 无变更可检查\n');
    return;
  }
  const changed = new Set(files);
  const docs = apiDocsContent();
  const fails = [];
  const warns = [];

  // 硬性 1/2：handler 与响应信封必须带契约变更
  for (const file of files) {
    if (!file.endsWith('.go') || file.endsWith('_test.go')) continue;

    const special = HARD_FILES[file];
    if (special) {
      const missing = special.filter((d) => !changed.has(d));
      if (missing.length > 0) {
        fails.push(`${file} 变更了响应信封，但契约未同步：${missing.join('、')}（契约先行：先改契约再改代码）`);
      }
      continue;
    }

    const hm = file.match(/^internal\/([^/]+)\/handler\//);
    if (!hm) continue;
    const docsFor = DOMAIN_DOCS[hm[1]];
    if (!docsFor) continue;
    if (!docsFor.some((d) => changed.has(d))) {
      fails.push(`${file} 已变更，但契约文档未同步：${docsFor.join('、')}（契约先行：写接口前先写 api 文档）`);
    }
  }

  // 硬性 3：新增路由必须已写入契约
  for (const file of files) {
    if (!file.startsWith('internal/') || !file.includes('/handler/')) continue;
    if (!file.endsWith('.go') || file.endsWith('_test.go')) continue;
    for (const r of extractNewRoutes(file)) {
      const hay = Object.values(docs).join('\n');
      const braced = r.path;
      const coloned = r.path.replace(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g, ':$1');
      if (!hay.includes(braced) && !hay.includes(coloned)) {
        fails.push(`${file}:${r.line} 新增路由 ${r.method} ${r.path} 未在任何 api/*.md 中描述（契约先行：先写文档再写接口）`);
      }
    }
  }

  // 软提醒
  for (const file of files) {
    if (!file.endsWith('.go') || file.endsWith('_test.go')) continue;
    if (SOFT_PREFIXES.some((p) => file.startsWith(p))) {
      const sm = file.match(/^internal\/([^/]+)\//);
      const docsFor = sm ? DOMAIN_DOCS[sm[1]] : [];
      warns.push(`${file} 已变更，建议核对契约：${docsFor.length ? docsFor.join('、') : 'api/*.md'}`);
      continue;
    }
    const sm = file.match(/^internal\/([^/]+)\/(service|model)\//);
    if (sm && DOMAIN_DOCS[sm[1]]) {
      warns.push(`${file} 已变更，建议核对契约：${DOMAIN_DOCS[sm[1]].join('、')}`);
    }
  }

  for (const w of warns) process.stdout.write(`[WARN] ${w}\n`);
  for (const f of fails) process.stdout.write(`[FAIL] ${f}\n`);

  if (fails.length > 0) {
    process.stdout.write('\n契约先行：先更新 api/ 契约文档再提交（确定无误可用 git commit --no-verify 跳过）。\n');
    process.exit(1);
  }
  if (warns.length === 0) process.stdout.write('OK: API 契约与变更一致\n');
}

main();
