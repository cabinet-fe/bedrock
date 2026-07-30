#!/usr/bin/env node

import path from 'path';
import { readJsonIfExists, writeJson } from './lib/fs-utils.mjs';
import {
  isGitRepo,
  gitHead,
  todayYmd,
  repoRelFromWorkspace,
} from './lib/git-utils.mjs';
import { resolveProjectPaths, displayPath, DEFAULT_OUT } from './lib/out-paths.mjs';

function usage() {
  console.error(`用法: node stamp_commit.mjs <repoRoot> [选项]

把当前 HEAD 写入 <out>/<project>/.sync.json 的 baseCommit（上次同步时的提交号）。
--dry-run 只打印将要写入的 JSON 与路径，不写文件。

选项:
  --out <dir>           输出根目录（默认: 自动发现已有 output/api-docs，否则 ${DEFAULT_OUT}）
  --project <name>      项目名（默认: 根 pom artifactId，否则仓库目录名）
  --workspace <dir>     工作区根（默认 process.cwd()；用于写入相对 repoRel）
  --docs a.md,b.md      本次写入的控制器文档列表（写入 meta.docs；也可用多次 --doc）
  --gateway-prefix /x   写入 meta.gatewayPrefix（可选）
  --gateway-service n   写入 meta.gatewayService（可选）
  --module <name>       兼容旧字段 meta.module（默认=project）
  --doc-file <path>     兼容旧字段 meta.docFile（单文件时代遗留；优先用 --docs）
  --dry-run             只预览，不写文件
  -h, --help            显示帮助

目录约定: <out>/<project>/.sync.json、<out>/<project>/<kebab>.md；约定页 <out>/_conventions.md
会写入 repoRel（相对工作区的仓库路径），供下次 changed_since / sync_status 避免用错仓。`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (!args.length || args[0] === '-h' || args[0] === '--help') usage();

  const positional = [];
  const out = {
    out: null,
    project: null,
    workspace: null,
    module: null,
    docFile: null,
    docs: null,
    gatewayPrefix: null,
    gatewayService: null,
    dryRun: false,
  };
  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--dry-run') {
      out.dryRun = true;
    } else if (a === '--out') {
      out.out = args[++i];
    } else if (a.startsWith('--out=')) {
      out.out = a.slice('--out='.length);
    } else if (a === '--project') {
      out.project = args[++i];
    } else if (a.startsWith('--project=')) {
      out.project = a.slice('--project='.length);
    } else if (a === '--workspace') {
      out.workspace = args[++i];
    } else if (a.startsWith('--workspace=')) {
      out.workspace = a.slice('--workspace='.length);
    } else if (a === '--module') {
      out.module = args[++i];
    } else if (a.startsWith('--module=')) {
      out.module = a.slice('--module='.length);
    } else if (a === '--docs') {
      const val = args[++i] || '';
      out.docs = val
        .split(',')
        .map((s) => path.basename(s.trim()))
        .filter(Boolean);
    } else if (a.startsWith('--docs=')) {
      out.docs = a
        .slice('--docs='.length)
        .split(',')
        .map((s) => path.basename(s.trim()))
        .filter(Boolean);
    } else if (a === '--doc') {
      if (!out.docs) out.docs = [];
      out.docs.push(path.basename(args[++i] || ''));
    } else if (a === '--doc-file' || a === '--docFile') {
      out.docFile = args[++i];
    } else if (a.startsWith('--doc-file=')) {
      out.docFile = a.slice('--doc-file='.length);
    } else if (a.startsWith('--docFile=')) {
      out.docFile = a.slice('--docFile='.length);
    } else if (a === '--gateway-prefix' || a === '--gatewayPrefix') {
      out.gatewayPrefix = args[++i];
    } else if (a.startsWith('--gateway-prefix=')) {
      out.gatewayPrefix = a.slice('--gateway-prefix='.length);
    } else if (a.startsWith('--gatewayPrefix=')) {
      out.gatewayPrefix = a.slice('--gatewayPrefix='.length);
    } else if (a === '--gateway-service' || a === '--gatewayService') {
      out.gatewayService = args[++i];
    } else if (a.startsWith('--gateway-service=')) {
      out.gatewayService = a.slice('--gateway-service='.length);
    } else if (a.startsWith('--gatewayService=')) {
      out.gatewayService = a.slice('--gatewayService='.length);
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
  const paths = resolveProjectPaths(opts.repoRoot, {
    out: opts.out,
    project: opts.project,
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

  // docs：显式参数 → 合并上次 → 空数组（全量后由 agent 填）
  let docs = opts.docs;
  if (!docs) {
    if (Array.isArray(prev.docs) && prev.docs.length) {
      docs = [...prev.docs];
    } else if (prev.docFile) {
      // 兼容旧单文件：保留 basename，提示已迁移
      docs = [path.basename(String(prev.docFile))];
    } else {
      docs = [];
    }
  }

  const meta = {
    baseCommit: head,
    updatedAt: todayYmd(),
    project: paths.project,
    docs,
    layout: 'per-controller',
    repoRel,
  };

  // 可选网关元数据
  const gatewayPrefix =
    opts.gatewayPrefix != null ? opts.gatewayPrefix : prev.gatewayPrefix;
  const gatewayService =
    opts.gatewayService != null ? opts.gatewayService : prev.gatewayService;
  if (gatewayPrefix != null && gatewayPrefix !== '') {
    meta.gatewayPrefix = gatewayPrefix;
  }
  if (gatewayService != null && gatewayService !== '') {
    meta.gatewayService = gatewayService;
  }

  // 兼容旧字段（勿再作为唯一真相）
  meta.module = opts.module || prev.module || paths.project;
  if (opts.docFile) {
    meta.docFile = opts.docFile;
  } else if (docs.length === 1) {
    meta.docFile = displayPath(
      opts.repoRoot,
      path.join(paths.projectRoot, docs[0]),
    );
  } else if (prev.docFile && docs.length === 0) {
    meta.docFile = prev.docFile;
  }

  if (opts.dryRun) {
    process.stdout.write(
      `${JSON.stringify(
        {
          dryRun: true,
          path: shownPath,
          outRoot: displayPath(opts.workspace, paths.outRoot),
          project: paths.project,
          projectRoot: displayPath(opts.workspace, paths.projectRoot),
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
        project: paths.project,
        projectRoot: displayPath(opts.workspace, paths.projectRoot),
        repoRel,
        meta,
      },
      null,
      2,
    )}\n`,
  );
}

main();
