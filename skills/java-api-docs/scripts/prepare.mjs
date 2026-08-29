#!/usr/bin/env node

/**
 * Agent 入口：一次探出模块路径 + git action，禁止 Agent 先逛工作区。
 *
 *   node prepare.mjs --project ic-upms-biz --force
 *   node prepare.mjs
 */

import fs from 'fs';
import path from 'path';
import { spawnSync } from 'child_process';
import { fileURLToPath } from 'url';
import {
  resolveWorkspace,
  resolveOutRoot,
  listSyncedProjects,
  displayPath,
} from './lib/out-paths.mjs';
import { discoverModules, matchModules, toSrcRel } from './lib/discover.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SKILL_ROOT = path.resolve(__dirname, '..');
const CHANGED_SINCE = path.join(__dirname, 'changed_since.mjs');

const HINT_NOW =
  '立刻执行 project.next.list_endpoints（或 projects[].next）。禁止再 Glob/Grep/列目录找源码根。写 md 时再读 references/writing.md。';
const HINT_NOOP = '已与 HEAD 同步且本地文档齐全；立即结束。禁止 list_endpoints / 写 md / stamp。';
const HINT_FORCE = '用户要求重生，已忽略 git noop。立刻跑 next.list_endpoints。';

function usage() {
  console.error(`用法: node prepare.mjs [query] [选项]

探出工作区 Java 模块、产出根、git 变更范围，打印下一步该跑的命令。
Agent 接到本技能后必须立刻跑本脚本，禁止先探索仓库。

选项:
  --project <name>    模块 / 项目 / Controller 名（与位置参数 query 相同）
  --docs a.md,b.md    只重生这些控制器文档
  --force             用户点名重生：忽略 git noop
  --out <dir>         产出根（默认 $BEDROCK_AGENT_OUTPUT 或发现已有目录）
  --workspace <dir>   工作区根（默认 $BEDROCK_AGENT_WORKDIR 或 cwd）
  -h, --help`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = {
    query: null,
    project: null,
    docs: [],
    force: false,
    out: null,
    workspace: null,
  };
  const positional = [];
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '-h' || a === '--help') usage();
    else if (a === '--force') opts.force = true;
    else if (a === '--project') opts.project = args[++i];
    else if (a.startsWith('--project=')) opts.project = a.slice('--project='.length);
    else if (a === '--docs') {
      opts.docs = splitDocs(args[++i] || '');
    } else if (a.startsWith('--docs=')) {
      opts.docs = splitDocs(a.slice('--docs='.length));
    } else if (a === '--out') opts.out = args[++i];
    else if (a.startsWith('--out=')) opts.out = a.slice('--out='.length);
    else if (a === '--workspace') opts.workspace = args[++i];
    else if (a.startsWith('--workspace=')) opts.workspace = a.slice('--workspace='.length);
    else if (a.startsWith('-')) {
      console.error(`未知参数: ${a}`);
      usage();
    } else {
      positional.push(a);
    }
  }
  opts.query = opts.project || positional[0] || null;
  return opts;
}

function splitDocs(val) {
  return String(val)
    .split(',')
    .map((s) => path.basename(s.trim()))
    .filter((s) => /\.md$/i.test(s));
}

function printJson(obj) {
  process.stdout.write(`${JSON.stringify(obj, null, 2)}\n`);
}

function runChangedSince(repoRoot, { out, project, workspace }) {
  const args = [CHANGED_SINCE, repoRoot, '--project', project, '--workspace', workspace];
  if (out) args.push('--out', out);
  const res = spawnSync(process.execPath, args, {
    encoding: 'utf8',
    cwd: workspace,
  });
  if (res.status !== 0) {
    return {
      ok: false,
      stderr: (res.stderr || res.error || '').toString().trim(),
      stdout: (res.stdout || '').toString().trim(),
    };
  }
  try {
    return { ok: true, data: JSON.parse(res.stdout) };
  } catch (err) {
    return { ok: false, stderr: `parse failed: ${err.message}`, stdout: res.stdout };
  }
}

function rel(workspace, abs) {
  return displayPath(workspace, abs);
}

function buildNext(workspace, scriptsDir, outShown, item, { conventionsMissing }) {
  const listArgs = [
    'node',
    rel(workspace, path.join(scriptsDir, 'list_endpoints.mjs')),
    item.bizSrcRoot,
    `--project ${item.project}`,
    `--repo-root ${item.moduleRoot}`,
  ];
  if (item.action === 'update_docs' && item.listFiles && item.listFiles.length) {
    listArgs.push(`--files ${item.listFiles.join(',')}`);
  }
  const docsFlag =
    item.docFiles && item.docFiles.length ? ` --docs ${item.docFiles.join(',')}` : '';
  return {
    ensure_conventions: conventionsMissing
      ? `node ${rel(workspace, path.join(scriptsDir, 'ensure_conventions.mjs'))} --out ${outShown}`
      : null,
    list_endpoints: listArgs.join(' '),
    resolve_types: `node ${rel(workspace, path.join(scriptsDir, 'resolve_types.mjs'))} ${item.apiSrcRoot} <TypeA,TypeB>`,
    verify_docs: `node ${rel(workspace, path.join(scriptsDir, 'verify_docs.mjs'))} ${item.bizSrcRoot} --project ${item.project} --repo-root ${item.moduleRoot} --out ${outShown}${docsFlag}`,
    stamp: `node ${rel(workspace, path.join(scriptsDir, 'stamp_commit.mjs'))} ${item.repoRoot} --project ${item.project} --workspace ${rel(workspace, workspace) || '.'} --out ${outShown}${docsFlag}`,
  };
}

function gitFilesToListFiles(mod, gitFiles) {
  const out = [];
  for (const f of gitFiles || []) {
    if (!/Controller\.java$/.test(f) && !/\/controller\//i.test(f)) continue;
    const srcRel = toSrcRel(mod.repoRoot, mod.bizSrcRoot, f);
    if (srcRel) out.push(srcRel);
  }
  return [...new Set(out)];
}

function applyDocsFilter(item, docs) {
  if (!docs.length) return item;
  const want = new Set(docs.map((d) => path.basename(d)));
  item.docFiles = (item.docFiles || []).filter((d) => want.has(d));
  const extra = docs.filter((d) => !item.docFiles.includes(d));
  item.docFiles = [...item.docFiles, ...extra];
  const byDoc = new Map(item.controllersMeta.map((c) => [c.docFile, c.rel]));
  item.listFiles = item.docFiles.map((d) => byDoc.get(d)).filter(Boolean);
  if (item.action === 'noop') item.action = 'update_docs';
  return item;
}

function enrich(mod, git, workspace, { force, docs }) {
  const repoRootShown = rel(workspace, mod.repoRoot);
  const item = {
    project: mod.project,
    action: git.action || 'full_scan',
    reason: git.reason || null,
    upToDate: Boolean(git.upToDate),
    repoRoot: git.repoRoot || repoRootShown,
    repoRel: git.repoRel,
    moduleRoot: rel(workspace, mod.moduleRoot),
    bizSrcRoot: rel(workspace, mod.bizSrcRoot),
    apiSrcRoot: rel(workspace, mod.apiSrcRoot),
    springName: mod.springName,
    suggestedRepoRoot: git.suggestedRepoRoot,
    baseCommit: git.baseCommit,
    head: git.head,
    files: git.files || [],
    controllers: git.controllers || [],
    docFiles: git.docFiles || [],
    listFiles: gitFilesToListFiles(mod, git.files || git.controllers || []),
    missingDocs: git.missingDocs,
    agentHint: git.agentHint,
    controllersMeta: mod.controllers,
  };

  if (force && item.action === 'noop') {
    item.action = docs.length ? 'update_docs' : 'full_scan';
    item.upToDate = false;
    item.reason = 'user_force';
    item.agentHint = HINT_FORCE;
    if (!docs.length) {
      item.docFiles = mod.controllers.map((c) => c.docFile);
      item.listFiles = [];
    }
  }

  if (force && item.action === 'update_docs' && !docs.length && !item.docFiles.length) {
    item.action = 'full_scan';
    item.listFiles = [];
  }

  applyDocsFilter(item, docs);

  if (item.action === 'full_scan') {
    item.listFiles = [];
    if (!item.docFiles.length) {
      item.docFiles = mod.controllers.map((c) => c.docFile);
    }
  }

  delete item.controllersMeta;
  return item;
}

function candidateBrief(mod, workspace) {
  return {
    project: mod.project,
    moduleName: mod.moduleName,
    springName: mod.springName,
    moduleRoot: rel(workspace, mod.moduleRoot),
    controllers: mod.controllers.map((c) => c.docFile),
  };
}

function main() {
  const opts = parseArgs(process.argv);
  if (opts.force && !opts.query && !opts.docs.length) {
    printJson({
      action: 'need_project',
      error: '未点名模块时禁止 --force 全仓重生。请加 --project <模块名>。',
      agentHint: '去掉 --force 跑增量，或补上 --project。',
    });
    process.exit(2);
  }

  const workspace = resolveWorkspace(opts.workspace);
  const outRoot = resolveOutRoot(opts.out, { discover: true, workspace });
  const outShown = rel(workspace, outRoot) || outRoot;
  const scriptsDir = __dirname;
  const conventionsMissing = !fs.existsSync(path.join(outRoot, '_conventions.md'));
  const modules = discoverModules(workspace, { outRoot });

  const base = {
    outRoot: outShown,
    workspace: rel(workspace, workspace) || '.',
    skillRoot: rel(workspace, SKILL_ROOT),
    scriptsDir: rel(workspace, scriptsDir),
    conventionsMissing,
    force: opts.force,
    query: opts.query,
    readWhenWriting: [
      'references/writing.md',
      'references/project-conventions.md',
      'references/template.md',
    ],
  };

  if (!modules.length) {
    printJson({
      ...base,
      action: 'not_found',
      allUpToDate: false,
      agentHint: '工作区未找到 src/main/java（含 *Controller.java 的 biz 模块）。核对仓库是否已 checkout。',
      projects: [],
      candidates: [],
    });
    return;
  }

  // 无点名：只处理已有 .sync.json 的项目（等同旧 sync_status）。尚无任何文档时才对发现的模块全量。
  const syncedNames = new Set(listSyncedProjects(outRoot));
  let selected = (
    opts.query
      ? []
      : syncedNames.size
        ? modules.filter((m) => syncedNames.has(m.project))
        : modules
  ).map((mod) => ({ mod, scope: 'project', docFile: null, listFile: null }));

  if (opts.query) {
    const matched = matchModules(modules, opts.query);
    if (matched.status === 'none') {
      printJson({
        ...base,
        action: 'not_found',
        agentHint: `未匹配「${opts.query}」。从 candidates 里挑一个再跑 prepare --project <名>。`,
        candidates: modules.map((m) => candidateBrief(m, workspace)),
        projects: [],
      });
      return;
    }
    if (matched.status === 'ambiguous') {
      printJson({
        ...base,
        action: 'need_project',
        agentHint: `「${opts.query}」匹配到多个模块，列出 candidates 后停止，不要自己全扫。`,
        candidates: matched.matches.map((m) => ({
          ...candidateBrief(m.mod, workspace),
          scope: m.scope,
          docFile: m.docFile,
        })),
        projects: [],
      });
      return;
    }
    selected = matched.matches;
  }

  const projects = [];
  for (const sel of selected) {
    const gitRes = runChangedSince(sel.mod.repoRoot, {
      out: outShown,
      project: sel.mod.project,
      workspace,
    });
    const git = gitRes.ok
      ? gitRes.data
      : {
          action: 'full_scan',
          reason: 'changed_since_failed',
          agentHint: gitRes.stderr || gitRes.stdout || 'changed_since 失败',
          files: [],
          controllers: [],
          docFiles: [],
        };

    let docs = opts.docs.slice();
    if (sel.scope === 'controller' && sel.docFile) {
      if (!docs.includes(sel.docFile)) docs.push(sel.docFile);
    }

    const item = enrich(sel.mod, git, workspace, { force: opts.force, docs });
    if (sel.scope === 'controller' && sel.docFile && opts.force) {
      item.action = 'update_docs';
      item.upToDate = false;
      item.reason = 'user_force';
      item.agentHint = HINT_FORCE;
      item.docFiles = [sel.docFile];
      item.listFiles = sel.listFile ? [sel.listFile] : item.listFiles;
    }
    item.next = buildNext(workspace, scriptsDir, outShown, item, { conventionsMissing });
    projects.push(item);
  }

  const work = projects.filter((p) => p.action !== 'noop');
  const allUpToDate = projects.length > 0 && work.length === 0;
  let action = 'noop';
  if (!projects.length) action = 'not_found';
  else if (projects.some((p) => p.action === 'wrong_repo')) action = 'wrong_repo';
  else if (allUpToDate) action = 'noop';
  else if (work.some((p) => p.action === 'full_scan')) action = 'full_scan';
  else action = 'update_docs';

  const agentHint = allUpToDate
    ? HINT_NOOP
    : action === 'wrong_repo'
      ? '用 suggestedRepoRoot 再跑 prepare，禁止因此全量。'
      : HINT_NOW;

  printJson({
    ...base,
    action,
    allUpToDate,
    agentHint,
    project: projects.length === 1 ? projects[0] : undefined,
    projects,
  });
}

main();
