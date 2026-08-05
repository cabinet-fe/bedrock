#!/usr/bin/env node

/**
 * 把当前 HEAD 写入 <out>/<repoKey>/.sync.json 的 baseCommit，并合并 docs[] / repoRel。
 */

import path from 'path';
import { readJsonIfExists, writeJson } from './lib/fs-utils.mjs';
import {
  isGitRepo,
  gitHead,
  todayYmd,
  repoRelFromWorkspace,
} from './lib/git-utils.mjs';
import {
  resolveRepoPaths,
  normalizeDocRel,
  displayPath,
  DEFAULT_OUT,
} from './lib/out-paths.mjs';

function usage() {
  console.error(`用法: node stamp_commit.mjs <repoRoot> [选项]

把当前 HEAD 写入 <out>/<repoKey>/.sync.json 的 baseCommit。
--docs 与已有 docs[] 做并集合并（保留子路径）。--dry-run 只预览不写。

选项:
  --out / --dir <dir>   产出根（默认: $BEDROCK_AGENT_OUTPUT 或发现已有产出，否则 ${DEFAULT_OUT}）
  --repo-key <name>     仓产出子目录名（默认: 仓库目录名）
  --workspace <dir>     工作区根（默认 process.cwd()；用于写入相对 repoRel）
  --docs a.md,b/c.md    本次文档列表（相对仓产出子目录；与已有 docs 合并）
  --doc <path>          追加单个文档路径（可多次）
  --dry-run             只预览，不写文件
  -h, --help            显示帮助

目录约定: <out>/<repoKey>/.sync.json；docs[] 可含子路径。
会写入 repoRel（相对工作区的仓库路径），供下次 changed_since / sync_status 避免用错仓。`);
  process.exit(2);
}

function parseDocList(raw) {
  return String(raw || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => normalizeDocRel(s));
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (!args.length || args[0] === '-h' || args[0] === '--help') usage();

  const positional = [];
  const out = {
    out: null,
    repoKey: null,
    workspace: null,
    docs: null,
    dryRun: false,
  };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--dry-run') {
      out.dryRun = true;
    } else if (a === '--out' || a === '--dir') {
      out.out = args[++i];
    } else if (a.startsWith('--out=')) {
      out.out = a.slice('--out='.length);
    } else if (a.startsWith('--dir=')) {
      out.out = a.slice('--dir='.length);
    } else if (a === '--repo-key' || a === '--repoKey') {
      out.repoKey = args[++i];
    } else if (a.startsWith('--repo-key=')) {
      out.repoKey = a.slice('--repo-key='.length);
    } else if (a.startsWith('--repoKey=')) {
      out.repoKey = a.slice('--repoKey='.length);
    } else if (a === '--workspace') {
      out.workspace = args[++i];
    } else if (a.startsWith('--workspace=')) {
      out.workspace = a.slice('--workspace='.length);
    } else if (a === '--docs') {
      out.docs = parseDocList(args[++i] || '');
    } else if (a.startsWith('--docs=')) {
      out.docs = parseDocList(a.slice('--docs='.length));
    } else if (a === '--doc') {
      if (!out.docs) out.docs = [];
      out.docs.push(normalizeDocRel(args[++i] || ''));
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
  if (out.docs) out.docs = [...new Set(out.docs.filter(Boolean))].sort();
  const { workspace: workspaceOpt, ...rest } = out;
  return {
    ...rest,
    repoRoot: path.resolve(positional[0]),
    workspace: workspaceOpt ? path.resolve(workspaceOpt) : process.cwd(),
  };
}

function main() {
  const opts = parseArgs(process.argv);
  const paths = resolveRepoPaths(opts.repoRoot, {
    out: opts.out,
    repoKey: opts.repoKey,
    workspace: opts.workspace,
  });
  const syncPath = paths.syncJsonPath;
  const prev = readJsonIfExists(syncPath) || {};
  const shownPath = displayPath(opts.workspace, syncPath);
  const repoRel = repoRelFromWorkspace(opts.workspace, opts.repoRoot);

  if (!isGitRepo(opts.repoRoot)) {
    console.error('不是 git 仓库:', opts.repoRoot);
    process.exit(1);
  }

  const head = gitHead(opts.repoRoot);

  // docs：显式参数与上次并集 → 仅上次 → 空数组
  let docs;
  const prevDocs = Array.isArray(prev.docs)
    ? prev.docs
        .map((d) => {
          try {
            return normalizeDocRel(d);
          } catch {
            return null;
          }
        })
        .filter(Boolean)
    : [];

  if (opts.docs) {
    docs = [...new Set([...prevDocs, ...opts.docs])].sort();
  } else if (prevDocs.length) {
    docs = [...prevDocs].sort();
  } else {
    docs = [];
  }

  const meta = {
    baseCommit: head,
    updatedAt: todayYmd(),
    docs,
    repoRel,
  };

  if (opts.dryRun) {
    process.stdout.write(
      `${JSON.stringify(
        {
          dryRun: true,
          path: shownPath,
          outRoot: displayPath(opts.workspace, paths.outRoot),
          repoKey: paths.repoKey,
          repoOutDir: displayPath(opts.workspace, paths.repoOutDir),
          repoRel,
          meta,
        },
        null,
        2,
      )}\n`,
    );
    return;
  }

  writeJson(syncPath, meta);
  process.stdout.write(
    `${JSON.stringify(
      {
        written: true,
        path: shownPath,
        outRoot: displayPath(opts.workspace, paths.outRoot),
        repoKey: paths.repoKey,
        repoOutDir: displayPath(opts.workspace, paths.repoOutDir),
        repoRel,
        meta,
      },
      null,
      2,
    )}\n`,
  );
}

main();
