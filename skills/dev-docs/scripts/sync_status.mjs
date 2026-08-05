#!/usr/bin/env node

/**
 * 多仓增量总览：扫 <out> 下各 <repoKey>/.sync.json，汇总每仓 action。
 *
 * 典型用法（在工作区根）:
 *   node scripts/sync_status.mjs --out output
 *   node scripts/sync_status.mjs
 */

import path from 'path';
import { spawnSync } from 'child_process';
import { fileURLToPath } from 'url';
import { readJsonIfExists } from './lib/fs-utils.mjs';
import {
  isGitRepo,
  commitExists,
  resolveRepoRel,
  listNearbyGitRepos,
  findRepoContainingCommit,
  repoRelFromWorkspace,
} from './lib/git-utils.mjs';
import {
  resolveOutRoot,
  listSyncedRepoKeys,
  displayPath,
  DEFAULT_OUT,
} from './lib/out-paths.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const CHANGED_SINCE = path.join(__dirname, 'changed_since.mjs');

function usage() {
  console.error(`用法: node sync_status.mjs [选项]

扫描产出根下全部仓的同步状态，输出汇总 JSON。
Agent 处理「更新所有仓库开发文档」时必须先跑本脚本：
  - summary.noop 里的仓：跳过
  - summary.run_agent 里的仓：整仓跑 agent → stamp → 可选 push
  - summary.wrong_repo：按 suggestedRepoRoot 重跑

选项:
  --out / --dir <dir>   产出根（默认: $BEDROCK_AGENT_OUTPUT 或发现已有产出，否则 ${DEFAULT_OUT}）
  --workspace <dir>     工作区根（默认 process.cwd()）
  -h, --help            显示帮助`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = { out: null, workspace: null };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--out' || a === '--dir') {
      opts.out = args[++i];
    } else if (a.startsWith('--out=')) {
      opts.out = a.slice('--out='.length);
    } else if (a.startsWith('--dir=')) {
      opts.out = a.slice('--dir='.length);
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
      console.error(`未知参数: ${a}`);
      usage();
    }
  }
  return {
    out: opts.out,
    workspace: opts.workspace ? path.resolve(opts.workspace) : process.cwd(),
  };
}

/**
 * 为仓解析应用哪个 git 仓库。
 */
function resolveRepoForKey(workspace, sync, nearbyRepos) {
  if (sync && sync.repoRel) {
    const abs = resolveRepoRel(workspace, sync.repoRel);
    if (abs && isGitRepo(abs)) return abs;
  }
  if (sync && sync.baseCommit) {
    const hit = findRepoContainingCommit(nearbyRepos, sync.baseCommit);
    if (hit) return hit;
  }
  if (nearbyRepos.length === 1) return nearbyRepos[0];
  if (isGitRepo(workspace)) return workspace;
  return null;
}

function runChangedSince(repoRoot, { out, repoKey, workspace }) {
  const args = [CHANGED_SINCE, repoRoot, '--repo-key', repoKey, '--workspace', workspace];
  if (out) {
    args.push('--out', out);
  }
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

function main() {
  const { out, workspace } = parseArgs(process.argv);
  const outRoot = resolveOutRoot(out, { discover: true, workspace });
  const repoKeys = listSyncedRepoKeys(outRoot);
  const nearby = listNearbyGitRepos(workspace);

  const items = [];
  for (const repoKey of repoKeys) {
    const syncPath = path.join(outRoot, repoKey, '.sync.json');
    const sync = readJsonIfExists(syncPath);
    const repoRoot = resolveRepoForKey(workspace, sync, nearby);

    if (!repoRoot) {
      items.push({
        repoKey,
        action: 'run_agent',
        upToDate: false,
        reason: 'no_repo_resolved',
        repoRoot: null,
        syncFile: displayPath(workspace, syncPath),
        agentHint: '无法解析该仓所属 git 仓库；请指定 repoRoot 后跑 changed_since',
        sync,
      });
      continue;
    }

    let effectiveRepo = repoRoot;
    if (sync && sync.baseCommit && !commitExists(repoRoot, sync.baseCommit)) {
      const other = findRepoContainingCommit(
        nearby.filter((r) => path.resolve(r) !== path.resolve(repoRoot)),
        sync.baseCommit,
      );
      if (other) effectiveRepo = other;
    }

    const result = runChangedSince(effectiveRepo, {
      out: displayPath(workspace, outRoot),
      repoKey,
      workspace,
    });

    if (!result.ok) {
      items.push({
        repoKey,
        action: 'run_agent',
        upToDate: false,
        reason: 'changed_since_failed',
        repoRoot: displayPath(workspace, effectiveRepo),
        repoRel: repoRelFromWorkspace(workspace, effectiveRepo),
        error: result.stderr || result.stdout,
        agentHint: 'changed_since 执行失败；检查路径后重试',
      });
      continue;
    }

    const d = result.data;
    items.push({
      repoKey,
      action: d.action,
      upToDate: Boolean(d.upToDate),
      reason: d.reason,
      repoRoot: d.repoRoot || displayPath(workspace, effectiveRepo),
      repoRel: d.repoRel || repoRelFromWorkspace(workspace, effectiveRepo),
      suggestedRepoRoot: d.suggestedRepoRoot,
      baseCommit: d.baseCommit,
      head: d.head,
      missingDocs: d.missingDocs || [],
      agentHint: d.agentHint,
    });
  }

  const summary = {
    noop: items.filter((i) => i.action === 'noop').map((i) => i.repoKey),
    run_agent: items.filter((i) => i.action === 'run_agent').map((i) => i.repoKey),
    wrong_repo: items.filter((i) => i.action === 'wrong_repo').map((i) => i.repoKey),
  };

  const allNoop = repoKeys.length > 0 && summary.noop.length === repoKeys.length;

  process.stdout.write(
    `${JSON.stringify(
      {
        outRoot: displayPath(workspace, outRoot),
        workspace: displayPath(workspace, workspace) || '.',
        repoCount: repoKeys.length,
        allUpToDate: allNoop,
        summary,
        agentHint: allNoop
          ? '全部仓已同步且本地 docs 齐全；立即结束整个任务，禁止再写文档 / stamp / push。'
          : '只处理 summary 中非 noop 的仓；noop 列表必须跳过。',
        repos: items,
      },
      null,
      2,
    )}\n`,
  );
}

main();
