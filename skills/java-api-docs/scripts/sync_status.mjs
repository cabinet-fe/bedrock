#!/usr/bin/env node

/**
 * 多仓 / 多项目增量总览：一次扫完 <out> 下所有 .sync.json，
 * 告诉 Agent 哪些项目 noop（跳过）、哪些要增量/全量。
 *
 * 典型用法（在工作区根）:
 *   node scripts/sync_status.mjs --out output
 *   node scripts/sync_status.mjs          # 自动发现 output/api-docs
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
  listSyncedProjects,
  displayPath,
  DEFAULT_OUT,
} from './lib/out-paths.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const CHANGED_SINCE = path.join(__dirname, 'changed_since.mjs');

function usage() {
  console.error(`用法: node sync_status.mjs [选项]

扫描产出根下全部项目的同步状态，输出汇总 JSON。
Agent 处理「更新所有仓库文档」时必须先跑本脚本：
  - summary.noop 里的项目：跳过
  - 其余按 action / suggestedRepoRoot 调用 changed_since 后的流程

选项:
  --out <dir>         输出根目录（默认: 自动发现已有 output/api-docs，否则 ${DEFAULT_OUT}）
  --workspace <dir>   工作区根（默认 process.cwd()）
  -h, --help          显示帮助`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = { out: null, workspace: null };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--out') {
      opts.out = args[++i];
    } else if (a.startsWith('--out=')) {
      opts.out = a.slice('--out='.length);
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
 * 为项目解析应用哪个 git 仓库。
 */
function resolveRepoForProject(workspace, sync, nearbyRepos) {
  if (sync && sync.repoRel) {
    const abs = resolveRepoRel(workspace, sync.repoRel);
    if (abs && isGitRepo(abs)) return abs;
  }
  if (sync && sync.baseCommit) {
    const hit = findRepoContainingCommit(nearbyRepos, sync.baseCommit);
    if (hit) return hit;
  }
  // 回退：工作区自身若是 git
  if (nearbyRepos.length === 1) return nearbyRepos[0];
  if (isGitRepo(workspace)) return workspace;
  return null;
}

function runChangedSince(repoRoot, { out, project, workspace }) {
  const args = [CHANGED_SINCE, repoRoot, '--project', project, '--workspace', workspace];
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
  const projects = listSyncedProjects(outRoot);
  const nearby = listNearbyGitRepos(workspace);

  const items = [];
  for (const project of projects) {
    const syncPath = path.join(outRoot, project, '.sync.json');
    const sync = readJsonIfExists(syncPath);
    const repoRoot = resolveRepoForProject(workspace, sync, nearby);

    if (!repoRoot) {
      items.push({
        project,
        action: 'full_scan',
        mode: 'full',
        upToDate: false,
        reason: 'no_repo_resolved',
        repoRoot: null,
        syncFile: displayPath(workspace, syncPath),
        agentHint: '无法解析该项目所属 git 仓库；请指定 repoRoot 后跑 changed_since',
        sync,
      });
      continue;
    }

    // 若 sync 无 baseCommit 但 commit 不在解析到的仓，再试邻仓
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
      project,
      workspace,
    });

    if (!result.ok) {
      items.push({
        project,
        action: 'full_scan',
        mode: 'full',
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
      project,
      action: d.action,
      mode: d.mode,
      upToDate: Boolean(d.upToDate),
      reason: d.reason,
      repoRoot: d.repoRoot || displayPath(workspace, effectiveRepo),
      repoRel: d.repoRel || repoRelFromWorkspace(workspace, effectiveRepo),
      suggestedRepoRoot: d.suggestedRepoRoot,
      baseCommit: d.baseCommit,
      head: d.head,
      files: d.files || [],
      controllers: d.controllers || [],
      docFiles: d.docFiles || [],
      agentHint: d.agentHint,
    });
  }

  const summary = {
    noop: items.filter((i) => i.action === 'noop').map((i) => i.project),
    update_docs: items.filter((i) => i.action === 'update_docs').map((i) => i.project),
    full_scan: items.filter((i) => i.action === 'full_scan').map((i) => i.project),
    wrong_repo: items.filter((i) => i.action === 'wrong_repo').map((i) => i.project),
  };

  const allNoop = projects.length > 0 && summary.noop.length === projects.length;

  process.stdout.write(
    `${JSON.stringify(
      {
        outRoot: displayPath(workspace, outRoot),
        workspace: displayPath(workspace, workspace) || '.',
        projectCount: projects.length,
        allUpToDate: allNoop,
        summary,
        agentHint: allNoop
          ? '全部项目已同步且无相关改动；立即结束整个任务，禁止再跑 list_endpoints / 写文档。'
          : '只处理 summary 中非 noop 的项目；noop 列表必须跳过。',
        projects: items,
      },
      null,
      2,
    )}\n`,
  );
}

main();
