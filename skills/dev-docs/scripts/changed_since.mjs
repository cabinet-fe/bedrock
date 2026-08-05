#!/usr/bin/env node

/**
 * 按代码仓库比对 .sync.json 的 baseCommit 与 HEAD，并检查本地 docs[] 是否齐全。
 * 不做 git diff / 文件级过滤：不一致或缺文件即整仓 run_agent。
 */

import path from 'path';
import { readJsonIfExists } from './lib/fs-utils.mjs';
import {
  isGitRepo,
  gitHead,
  commitExists,
  repoRelFromWorkspace,
  resolveRepoRel,
  listNearbyGitRepos,
  findRepoContainingCommit,
} from './lib/git-utils.mjs';
import {
  resolveRepoPaths,
  findMissingLocalDocs,
  displayPath,
  DEFAULT_OUT,
} from './lib/out-paths.mjs';

const HINT_NOOP =
  'baseCommit 已等于 HEAD 且本地 docs[] 齐全；立即跳过本仓。禁止写 md / stamp / push。';
const HINT_RUN =
  '需要为本仓跑 agent：读代码、写/补齐产出目录下的 md，然后 stamp_commit，再按需 push_docs。';
const HINT_WRONG_REPO =
  'baseCommit / repoRel 不属于当前 repoRoot。请改用 suggestedRepoRoot 再跑 changed_since；禁止在本仓库盲目重写。';
const HINT_MISSING =
  '本地 docs[] 有缺失：即使 commit 一致也须跑 agent 重生缺失文件，禁止当 noop。';

function usage() {
  console.error(`用法: node changed_since.mjs <repoRoot> [选项]

按代码仓库比对 <out>/<repoKey>/.sync.json 的 baseCommit 与当前 HEAD。
本地 docs[] 缺失时即使 commit 一致也返回 run_agent。
结果以 JSON 打印到标准输出。

关键字段 action（Agent 必须先看这个）:
  noop        — baseCommit===HEAD 且本地 docs 齐全，立即跳过本仓
  run_agent   — 需整仓生成/更新文档
  wrong_repo  — 用错了仓库，按 suggestedRepoRoot 重跑

选项:
  --out / --dir <dir>   产出根（默认: $BEDROCK_AGENT_OUTPUT 或发现已有产出，否则 ${DEFAULT_OUT}）
  --repo-key <name>     仓产出子目录名（默认: 仓库目录名）
  --workspace <dir>     工作区根（默认 process.cwd()；用于解析 sync.repoRel）
  -h, --help            显示帮助

目录约定: <out>/<repoKey>/.sync.json + 相对该子目录的 docs[]（可含子路径）`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (!args.length || args[0] === '-h' || args[0] === '--help') usage();

  const positional = [];
  const opts = { out: null, repoKey: null, workspace: null };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--out' || a === '--dir') {
      opts.out = args[++i];
    } else if (a.startsWith('--out=')) {
      opts.out = a.slice('--out='.length);
    } else if (a.startsWith('--dir=')) {
      opts.out = a.slice('--dir='.length);
    } else if (a === '--repo-key' || a === '--repoKey') {
      opts.repoKey = args[++i];
    } else if (a.startsWith('--repo-key=')) {
      opts.repoKey = a.slice('--repo-key='.length);
    } else if (a.startsWith('--repoKey=')) {
      opts.repoKey = a.slice('--repoKey='.length);
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
    out: opts.out,
    repoKey: opts.repoKey,
    workspace: opts.workspace ? path.resolve(opts.workspace) : process.cwd(),
  };
}

function printJson(obj) {
  process.stdout.write(`${JSON.stringify(obj, null, 2)}\n`);
}

function main() {
  const { repoRoot, out, repoKey, workspace } = parseArgs(process.argv);
  const paths = resolveRepoPaths(repoRoot, { out, repoKey, workspace });
  const syncPath = paths.syncJsonPath;
  const sync = readJsonIfExists(syncPath);
  const syncFile = displayPath(workspace, syncPath);
  const outRootShown = displayPath(workspace, paths.outRoot);
  const repoRel = repoRelFromWorkspace(workspace, repoRoot);

  const base = {
    outRoot: outRootShown,
    repoKey: paths.repoKey,
    syncFile,
    repoRoot: displayPath(workspace, repoRoot),
    repoRel,
    repoOutDir: displayPath(workspace, paths.repoOutDir),
  };

  if (!isGitRepo(repoRoot)) {
    printJson({
      ...base,
      action: 'run_agent',
      upToDate: false,
      reason: 'not_a_git_repo',
      baseCommit: null,
      head: null,
      missingDocs: [],
      agentHint: HINT_RUN,
    });
    return;
  }

  const head = gitHead(repoRoot);
  const baseCommit = (sync && sync.baseCommit) || null;

  // sync 记录了别的仓库：当前 repo 对不上时，引导换仓
  if (sync && sync.repoRel) {
    const expectedAbs = resolveRepoRel(workspace, sync.repoRel);
    if (
      expectedAbs &&
      path.resolve(expectedAbs) !== path.resolve(repoRoot) &&
      isGitRepo(expectedAbs)
    ) {
      printJson({
        ...base,
        action: 'wrong_repo',
        upToDate: false,
        reason: 'repo_rel_mismatch',
        baseCommit: sync.baseCommit || null,
        head,
        missingDocs: [],
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
      action: 'run_agent',
      upToDate: false,
      reason: 'no_base_commit',
      baseCommit: null,
      head,
      missingDocs: [],
      sync,
      agentHint: HINT_RUN,
    });
    return;
  }

  if (!commitExists(repoRoot, baseCommit)) {
    const nearby = listNearbyGitRepos(workspace).filter(
      (r) => path.resolve(r) !== path.resolve(repoRoot),
    );
    const other = findRepoContainingCommit(nearby, baseCommit);
    if (other) {
      printJson({
        ...base,
        action: 'wrong_repo',
        upToDate: false,
        reason: 'base_commit_in_other_repo',
        baseCommit,
        head,
        missingDocs: [],
        suggestedRepoRoot: displayPath(workspace, other),
        sync,
        agentHint: HINT_WRONG_REPO,
      });
      return;
    }

    printJson({
      ...base,
      action: 'run_agent',
      upToDate: false,
      reason: 'base_commit_unknown',
      baseCommit,
      head,
      missingDocs: [],
      sync,
      agentHint: HINT_RUN,
      note: 'baseCommit 在当前仓库不存在；若这是多仓工作区，请确认 repoRoot 是否选对',
    });
    return;
  }

  const { missing } = findMissingLocalDocs(paths.repoOutDir, sync.docs);
  const commitMatch = baseCommit === head;

  if (commitMatch && missing.length === 0) {
    printJson({
      ...base,
      action: 'noop',
      upToDate: true,
      reason: 'ok',
      baseCommit,
      head,
      missingDocs: [],
      docs: Array.isArray(sync.docs) ? sync.docs : [],
      agentHint: HINT_NOOP,
    });
    return;
  }

  if (commitMatch && missing.length > 0) {
    printJson({
      ...base,
      action: 'run_agent',
      upToDate: false,
      reason: 'missing_local_docs',
      baseCommit,
      head,
      missingDocs: missing,
      docs: Array.isArray(sync.docs) ? sync.docs : [],
      agentHint: HINT_MISSING,
    });
    return;
  }

  printJson({
    ...base,
    action: 'run_agent',
    upToDate: false,
    reason: missing.length ? 'commit_mismatch_with_missing' : 'commit_mismatch',
    baseCommit,
    head,
    missingDocs: missing,
    docs: Array.isArray(sync.docs) ? sync.docs : [],
    agentHint: HINT_RUN,
  });
}

main();
