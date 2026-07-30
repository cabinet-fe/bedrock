#!/usr/bin/env node

/**
 * 校验已写 Markdown 中的 METHOD path 是否与 list_endpoints 一致。
 * 不一致时非 0 退出（供写文档后硬门禁）。
 */

import path from 'path';
import { listEndpoints } from './lib/endpoints.mjs';
import { resolveProjectPaths, displayPath, DEFAULT_OUT } from './lib/out-paths.mjs';
import { verifyDocsAgainstEndpoints } from './lib/verify-docs.mjs';

function usage() {
  console.error(`用法: node verify_docs.mjs <srcRoot> [选项]

比对 <out>/<project>/*.md 中的 \`METHOD /path\` 与 list_endpoints 结果。
有 missingInDocs / extraInDocs / missingDocs 时退出码 1。

选项:
  --out <dir>             产出根（默认自动发现 / ${DEFAULT_OUT}）
  --project <name>        项目子目录名（强烈建议；并参与网关匹配）
  --service <name>        显式网关服务名
  --repo-root <dir>       模块/仓根（读 application.yml；多模块仓请指到具体模块）
  --docs a.md,b.md        只校验这些文档（默认：脚本扫到的全部 docFile）
  --files a.java,b.java   只扫这些 Controller（与 list_endpoints 相同）
  --gateway-json <path>   网关 JSON
  --allow-gateway-miss    网关未匹配时仍用 path 比对（默认：未匹配则用 servicePath）
  -h, --help

示例:
  node verify_docs.mjs repo-4/ic-upms/ic-upms-biz/src/main/java \\
    --project ic-upms-biz --repo-root repo-4/ic-upms/ic-upms-biz --out output`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (!args.length || args[0] === '-h' || args[0] === '--help') usage();

  const positional = [];
  const opts = {
    out: null,
    project: null,
    service: null,
    repoRoot: null,
    docs: null,
    files: null,
    gatewayJson: null,
    allowGatewayMiss: false,
  };

  for (let i = 0; i < args.length; i += 1) {
    const a = args[i];
    if (a === '--out') opts.out = args[++i];
    else if (a.startsWith('--out=')) opts.out = a.slice('--out='.length);
    else if (a === '--project') opts.project = args[++i];
    else if (a.startsWith('--project=')) opts.project = a.slice('--project='.length);
    else if (a === '--service') opts.service = args[++i];
    else if (a.startsWith('--service=')) opts.service = a.slice('--service='.length);
    else if (a === '--repo-root' || a === '--repoRoot') opts.repoRoot = args[++i];
    else if (a.startsWith('--repo-root=')) opts.repoRoot = a.slice('--repo-root='.length);
    else if (a.startsWith('--repoRoot=')) opts.repoRoot = a.slice('--repoRoot='.length);
    else if (a === '--docs') {
      opts.docs = (args[++i] || '')
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    } else if (a.startsWith('--docs=')) {
      opts.docs = a
        .slice('--docs='.length)
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    } else if (a === '--files') {
      opts.files = (args[++i] || '')
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    } else if (a.startsWith('--files=')) {
      opts.files = a
        .slice('--files='.length)
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    } else if (a === '--gateway-json' || a === '--gatewayJson' || a === '--gateway') {
      opts.gatewayJson = args[++i];
    } else if (a.startsWith('--gateway-json=')) {
      opts.gatewayJson = a.slice('--gateway-json='.length);
    } else if (a === '--allow-gateway-miss') opts.allowGatewayMiss = true;
    else if (a === '-h' || a === '--help') usage();
    else if (a.startsWith('-')) {
      console.error(`未知参数: ${a}`);
      usage();
    } else positional.push(a);
  }

  if (!positional.length) usage();
  return { srcRoot: positional[0], ...opts };
}

function main() {
  const opts = parseArgs(process.argv);
  const srcRoot = path.resolve(opts.srcRoot);
  const repoRoot = opts.repoRoot ? path.resolve(opts.repoRoot) : srcRoot;
  const workspace = process.cwd();

  const paths = resolveProjectPaths(repoRoot, {
    out: opts.out,
    project: opts.project,
  });

  const listed = listEndpoints(srcRoot, {
    files: opts.files,
    project: opts.project || paths.project,
    service: opts.service,
    repoRoot,
    gatewayJson: opts.gatewayJson,
  });

  if (listed.errors && listed.errors.length) {
    process.stdout.write(
      `${JSON.stringify(
        {
          ok: false,
          reason: 'list_endpoints_errors',
          errors: listed.errors,
          gateway: listed.gateway,
        },
        null,
        2,
      )}\n`,
    );
    process.exit(1);
  }

  if (!listed.count) {
    process.stdout.write(
      `${JSON.stringify(
        {
          ok: false,
          reason: 'no_endpoints',
          gateway: listed.gateway,
          hint: 'list_endpoints 返回 0 条；检查 srcRoot / --files / Controller 注解',
        },
        null,
        2,
      )}\n`,
    );
    process.exit(1);
  }

  const gatewayMatched = listed.gateway && listed.gateway.matched;
  const result = verifyDocsAgainstEndpoints({
    projectRoot: paths.projectRoot,
    endpoints: listed.endpoints,
    docs: opts.docs,
    gatewayMatched: opts.allowGatewayMiss ? true : gatewayMatched,
  });

  const payload = {
    ok: result.ok,
    project: paths.project,
    outRoot: displayPath(workspace, paths.outRoot),
    projectRoot: displayPath(workspace, paths.projectRoot),
    gateway: {
      matched: gatewayMatched,
      gatewayPrefix: listed.gateway?.gatewayPrefix ?? null,
      serviceName: listed.gateway?.serviceName ?? null,
      warning: listed.gateway?.warning ?? null,
    },
    endpointCount: listed.count,
    ...result,
    agentHint: result.ok
      ? '文档 path 与 list_endpoints 一致，可 stamp / push。'
      : '文档 path 与脚本不一致：按 missingInDocs / extraInDocs 修正 Markdown（禁止按英语习惯改单复数）；修好后再跑本脚本。',
  };

  process.stdout.write(`${JSON.stringify(payload, null, 2)}\n`);
  process.exit(result.ok ? 0 : 1);
}

main();
