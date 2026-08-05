#!/usr/bin/env node

import path from 'path';
import { resolveTypes } from './lib/types.mjs';

function usage() {
  console.error(`用法: node resolve_types.mjs <srcRoot> TypeA,TypeB

在 srcRoot 下解析 DTO/VO/Entity 字段树。
除字段声明外，会识别 public/protected 的 getXxx/setXxx/isXxx
（与字段名不同的属性会并入，如 argKeys 字段 + getArgKeyArray → argKeyArray）。
每字段含 required + requiredSource（validation|schema|platform|default）。
未解析类型标记为 needs_source（需读源码）。

说明: 本脚本只解析类型。文档输出目录请用 changed_since / stamp_commit 的
      --out <dir> 与 --project <name>（约定: <out>/<project>/<module>.md）。`);
  process.exit(2);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  if (args.length < 2 || args[0] === '-h' || args[0] === '--help') usage();
  return {
    srcRoot: path.resolve(args[0]),
    typeNames: args[1].split(',').map((s) => s.trim()).filter(Boolean),
  };
}

function main() {
  const { srcRoot, typeNames } = parseArgs(process.argv);
  if (!typeNames.length) usage();
  const result = resolveTypes(srcRoot, typeNames);
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

main();
