#!/usr/bin/env node

import fs from 'fs';
import path from 'path';
import { readJsonIfExists } from './lib/fs-utils.mjs';
import {
  isGitRepo,
  gitHead,
  commitExists,
  changedFilesSince,
  repoRelFromWorkspace,
  resolveRepoRel,
  listNearbyGitRepos,
  findRepoContainingCommit,
} from './lib/git-utils.mjs';
import { resolveProjectPaths, displayPath, DEFAULT_OUT } from './lib/out-paths.mjs';
import { isRelevantJavaChange } from './lib/endpoints.mjs';
import { controllerDocFileName } from './lib/names.mjs';

const HINT_NOOP =
  '已与 HEAD 同步、无本地相关改动且 sync.docs 本地齐全；立即结束本项目。禁止 list_endpoints / resolve_types / 写 md / stamp。';
const HINT_WRONG_REPO =
  'baseCommit 不属于当前 repoRoot。请改用 suggestedRepoRoot 再跑 changed_since；禁止在本仓库全量重生成。';
const HINT_UPDATE =
  '有相关 Java 变更：仅对 files/controllers/docFiles 做增量更新，勿全量扫描。';
const HINT_MISSING =
  '本地 sync.docs 中有文档文件缺失：须重生 missingDocs/docFiles，禁止当 noop；写完后 stamp。';
const HINT_FULL = '需要全量扫描：跑 list_endpoints（不带 --files），写齐文档后 stamp。';
const HINT_CONVENTIONS =
  '约定页 <out>/_conventions.md 不存在，写文档前请先跑 ensure_conventions。';

function usage() {
  console.error(`用法: node changed_since.mjs <repoRoot> [baseCommit] [选项]

列出自 baseCommit 以来有改动的相关 Java 文件。
默认从 <out>/<project>/.sync.json 读取上次同步的提交号。
也包括未暂存/已暂存的本地改动。结果以 JSON 打印到标准输出。

关键字段 action（Agent 必须先看这个）:
  noop          — 无相关变更且本地 docs[] 齐全，立即结束本项目
  update_docs   — 仅更新返回的 docFiles（含缺失的 missingDocs）
  full_scan     — 全量生成
  wrong_repo    — 用错了仓库，按 suggestedRepoRoot 重跑（禁止全量）

选项:
  --out <dir>         输出根目录（默认: 自动发现已有 output/api-docs，否则 ${DEFAULT_OUT}）
  --project <name>    项目名（默认: 根 pom artifactId，否则仓库目录名）
  --workspace <dir>   工作区根（默认 process.cwd()；用于解析 sync.repoRel / 邻仓探测）
  -h, --help          显示帮助

目录约定: <out>/<project>/.sync.json + <kebab>.md；约定页 <out>/_conventions.md`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (!args.length || args[0] === '-h' || args[0] === '--help') usage();

  const positional = [];
  const opts = { out: null, project: null, workspace: null };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--out') {
      opts.out = args[++i];
    } else if (a.startsWith('--out=')) {
      opts.out = a.slice('--out='.length);
    } else if (a === '--project') {
      opts.project = args[++i];
    } else if (a.startsWith('--project=')) {
      opts.project = a.slice('--project='.length);
    } else if (a === '--workspace') {
      opts.workspace = args[++i];
    } else if (a.startsWith('--workspace=')) {
      opts.workspace = a.slice('--workspace='.length);
    } else if (a === '-h' || a === '--help') {
      usage();
    } else if (a.startsWith('-')) {
      console.error(`未知参数: ${a}`);
      usage();
    } else {
      positional.push(a);
    }
  }
  if (!positional.length) usage();
  return {
    repoRoot: path.resolve(positional[0]),
    baseCommitArg: positional[1] || null,
    out: opts.out,
    project: opts.project,
    workspace: opts.workspace ? path.resolve(opts.workspace) : process.cwd(),
  };
}

function controllerClassFromPath(relPath) {
  const base = path.basename(relPath, '.java');
  if (!base) return null;
  return base;
}

function printJson(obj) {
  process.stdout.write(`${JSON.stringify(obj, null, 2)}\n`);
}

function buildDocFiles(controllers) {
  return [
    ...new Set(
      controllers
        .map((f) => {
          const cls = controllerClassFromPath(f);
          return cls ? controllerDocFileName(cls) : null;
        })
        .filter(Boolean),
    ),
  ].sort();
}

/**
 * sync.docs[] 里相对项目目录缺失的本地 md（无 docs 数组则跳过检查）。
 * @param {string} projectRoot
 * @param {unknown} docs
 * @returns {string[]}
 */
function findMissingLocalDocs(projectRoot, docs) {
  if (!Array.isArray(docs) || docs.length === 0) return [];
  const missing = [];
  for (const entry of docs) {
    const raw = String(entry || '').trim();
    if (!raw) continue;
    const base = path.basename(raw);
    // 仅接受 basename（与 stamp / resolveControllerDocPath 一致）
    if (!base || base !== raw || base.includes('..')) continue;
    const abs = path.join(projectRoot, base);
    if (!fs.existsSync(abs)) missing.push(base);
  }
  return [...new Set(missing)].sort();
}

function mergeDocFiles(fromControllers, missingDocs) {
  return [...new Set([...(fromControllers || []), ...(missingDocs || [])])].sort();
}

function buildAgentHint({ hasGitChanges, missingDocs, conventionsMissing }) {
  const parts = [];
  if (hasGitChanges) parts.push(HINT_UPDATE);
  if (missingDocs.length) parts.push(HINT_MISSING);
  if (!hasGitChanges && !missingDocs.length) parts.push(HINT_NOOP);
  if (conventionsMissing && (hasGitChanges || missingDocs.length)) {
    parts.push(HINT_CONVENTIONS);
  }
  return parts.join(' ');
}

function main() {
  const { repoRoot, baseCommitArg, out, project, workspace } = parseArgs(process.argv);
  const paths = resolveProjectPaths(repoRoot, { out, project, workspace });
  const syncPath = paths.syncJsonPath;
  const sync = readJsonIfExists(syncPath);
  const syncFile = displayPath(workspace, syncPath);
  const outRootShown = displayPath(workspace, paths.outRoot);
  const repoRel = repoRelFromWorkspace(workspace, repoRoot);

  const legacySingleDoc =
    sync &&
    !Array.isArray(sync.docs) &&
    sync.docFile &&
    String(sync.docFile).endsWith(`${paths.project}.md`);

  const base = {
    outRoot: outRootShown,
    project: paths.project,
    syncFile,
    repoRoot: displayPath(workspace, repoRoot),
    repoRel,
    layout: 'per-controller',
    legacySingleDoc: Boolean(legacySingleDoc),
  };

  if (!isGitRepo(repoRoot)) {
    printJson({
      ...base,
      mode: 'full',
      action: 'full_scan',
      upToDate: false,
      reason: 'not_a_git_repo',
      baseCommit: null,
      head: null,
      files: [],
      controllers: [],
      docFiles: [],
      agentHint: HINT_FULL,
    });
    return;
  }

  const head = gitHead(repoRoot);
  let baseCommit = baseCommitArg || (sync && sync.baseCommit) || null;

  // sync 记录了别的仓库：当前 repo 对不上时，引导换仓，绝不因此全量
  if (sync && sync.repoRel && !baseCommitArg) {
    const expectedAbs = resolveRepoRel(workspace, sync.repoRel);
    if (
      expectedAbs &&
      path.resolve(expectedAbs) !== path.resolve(repoRoot) &&
      isGitRepo(expectedAbs)
    ) {
      printJson({
        ...base,
        mode: 'skip',
        action: 'wrong_repo',
        upToDate: false,
        reason: 'repo_rel_mismatch',
        baseCommit: sync.baseCommit || null,
        head,
        files: [],
        controllers: [],
        docFiles: [],
        suggestedRepoRoot: displayPath(workspace, expectedAbs),
        syncRepoRel: sync.repoRel,
        agentHint: HINT_WRONG_REPO,
      });
      return;
    }
  }

  if (!baseCommit) {
    printJson({
      ...base,
      mode: 'full',
      action: 'full_scan',
      upToDate: false,
      reason: 'no_base_commit',
      baseCommit: null,
      head,
      files: [],
      controllers: [],
      docFiles: [],
      sync,
      agentHint: HINT_FULL,
      note: legacySingleDoc
        ? '检测到旧单文件 docFile；全量后请改为按 Controller 的 <kebab>.md，并用 stamp --docs 更新'
        : undefined,
    });
    return;
  }

  if (!commitExists(repoRoot, baseCommit)) {
    // 可能用错了多仓工作区里的某个 repo-*：在邻仓里找真正拥有该 commit 的仓库
    const nearby = listNearbyGitRepos(workspace).filter(
      (r) => path.resolve(r) !== path.resolve(repoRoot),
    );
    const other = findRepoContainingCommit(nearby, baseCommit);
    if (other) {
      printJson({
        ...base,
        mode: 'skip',
        action: 'wrong_repo',
        upToDate: false,
        reason: 'base_commit_in_other_repo',
        baseCommit,
        head,
        files: [],
        controllers: [],
        docFiles: [],
        suggestedRepoRoot: displayPath(workspace, other),
        sync,
        agentHint: HINT_WRONG_REPO,
      });
      return;
    }

    // 已有文档 + 未知 commit：仍标 full，但提示先核对仓库，避免盲目重跑
    printJson({
      ...base,
      mode: 'full',
      action: 'full_scan',
      upToDate: false,
      reason: 'base_commit_unknown',
      baseCommit,
      head,
      files: [],
      controllers: [],
      docFiles: [],
      sync,
      agentHint: HINT_FULL,
      note:
        'baseCommit 在当前仓库不存在；若这是多仓工作区，请确认 repoRoot 是否选对，勿对已有文档盲目全量重写',
    });
    return;
  }

  const allChanged = changedFilesSince(repoRoot, baseCommit);
  const files = allChanged.filter(isRelevantJavaChange);
  const controllers = files.filter((f) => {
    const baseName = path.basename(f);
    return baseName.endsWith('Controller.java') || /\/controller\//i.test(f);
  });
  const fromControllers = buildDocFiles(controllers);
  const missingDocs = findMissingLocalDocs(paths.projectRoot, sync && sync.docs);
  const hasGitChanges = files.length > 0;
  const upToDate = !hasGitChanges && missingDocs.length === 0;
  const docFiles = mergeDocFiles(fromControllers, missingDocs);
  const reason = upToDate
    ? 'ok'
    : missingDocs.length === 0
      ? 'ok'
      : hasGitChanges
        ? 'ok_with_missing'
        : 'missing_local_docs';
  const conventionsMissing =
    !upToDate && !fs.existsSync(paths.conventionsPath);

  const result = {
    ...base,
    mode: 'incremental',
    action: upToDate ? 'noop' : 'update_docs',
    upToDate,
    reason,
    baseCommit,
    head,
    files,
    controllers,
    docFiles,
    allChangedCount: allChanged.length,
    agentHint: buildAgentHint({ hasGitChanges, missingDocs, conventionsMissing }),
  };
  if (missingDocs.length) result.missingDocs = missingDocs;
  printJson(result);
}

main();
