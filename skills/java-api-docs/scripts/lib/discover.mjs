import fs from 'fs';
import path from 'path';
import { walkJavaFiles } from './fs-utils.mjs';
import { listNearbyGitRepos } from './git-utils.mjs';
import { readPomArtifactId, listSyncedProjects } from './out-paths.mjs';
import { readSpringApplicationName } from './gateway.mjs';
import { controllerDocFileName, controllerToKebab } from './names.mjs';

const SKIP_DIRS = new Set([
  '.git',
  'target',
  'node_modules',
  'build',
  'dist',
  '.idea',
  '.agents',
  '__MACOSX',
  'data',
  'output',
  'api-docs',
  'web',
  'tmp',
  'skills',
  'coverage',
  'vendor',
]);

/**
 * @param {string} s
 */
function normalizeKey(s) {
  return String(s || '')
    .trim()
    .toLowerCase()
    .replace(/_/g, '-')
    .replace(/\.md$/i, '');
}

/**
 * 项目/服务名变体：去 ic- 前缀、去 -biz/-api 后缀。
 * @param {string} s
 * @returns {string[]}
 */
function nameVariants(s) {
  const n = normalizeKey(s);
  if (!n) return [];
  const out = [n];
  if (n.startsWith('ic-')) out.push(n.slice(3));
  if (n.endsWith('-biz')) out.push(n.slice(0, -4));
  if (n.endsWith('-api')) out.push(n.slice(0, -4));
  if (n.startsWith('ic-') && n.endsWith('-biz')) out.push(n.slice(3, -4));
  if (n.startsWith('ic-') && n.endsWith('-api')) out.push(n.slice(3, -4));
  return [...new Set(out.filter(Boolean))];
}

/**
 * 在 git 仓内找 `src/main/java`（只走目录名，不扫 .java）。
 * @param {string} repoRoot
 * @param {number} [maxDepth]
 * @returns {string[]}
 */
function findSrcMainJavaDirs(repoRoot, maxDepth = 8) {
  const hits = [];
  const walk = (dir, depth) => {
    if (depth > maxDepth) return;
    const base = path.basename(dir);
    const parent = path.basename(path.dirname(dir));
    if (base === 'main' && parent === 'src') {
      const javaDir = path.join(dir, 'java');
      try {
        if (fs.statSync(javaDir).isDirectory()) hits.push(javaDir);
      } catch {
        /* missing */
      }
      return;
    }
    let entries;
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const ent of entries) {
      if (!ent.isDirectory()) continue;
      if (SKIP_DIRS.has(ent.name) || ent.name.startsWith('.')) continue;
      const child = path.join(dir, ent.name);
      // 嵌套 git 仓交给 listNearbyGitRepos，禁止从父仓深扫进去
      if (depth > 0 && fs.existsSync(path.join(child, '.git'))) continue;
      walk(child, depth + 1);
    }
  };
  walk(path.resolve(repoRoot), 0);
  return hits;
}

/**
 * @param {string} srcRoot
 */
function listControllers(srcRoot) {
  if (!fs.existsSync(srcRoot)) return [];
  let files;
  try {
    files = walkJavaFiles(srcRoot, {
      filter: (f) => /Controller\.java$/.test(f),
    });
  } catch {
    return [];
  }
  return files.map((abs) => {
    const className = path.basename(abs, '.java');
    return {
      className,
      rel: path.relative(srcRoot, abs) || path.basename(abs),
      docFile: controllerDocFileName(className),
    };
  });
}

/**
 * 同级 `foo-biz` → `foo-api/src/main/java`；没有则退回 biz 自己。
 * @param {string} moduleRoot
 * @param {string} bizSrcRoot
 */
function pairApiSrcRoot(moduleRoot, bizSrcRoot) {
  const name = path.basename(moduleRoot);
  const parent = path.dirname(moduleRoot);
  const candidates = [];
  if (name.endsWith('-biz')) {
    candidates.push(path.join(parent, `${name.slice(0, -4)}-api`, 'src', 'main', 'java'));
  }
  candidates.push(path.join(parent, `${name}-api`, 'src', 'main', 'java'));
  for (const p of candidates) {
    if (fs.existsSync(p)) return p;
  }
  return bizSrcRoot;
}

/**
 * git 相对路径 → 相对 srcRoot（供 list_endpoints --files）。
 * @param {string} repoRoot
 * @param {string} srcRoot
 * @param {string} gitRel
 * @returns {string|null}
 */
function toSrcRel(repoRoot, srcRoot, gitRel) {
  const abs = path.isAbsolute(gitRel)
    ? gitRel
    : path.resolve(repoRoot, gitRel);
  const rel = path.relative(srcRoot, abs);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) return null;
  return rel;
}

/**
 * @param {string} workspace
 * @param {{ outRoot?: string|null }} [opts]
 */
function discoverModules(workspace, opts = {}) {
  const repos = listNearbyGitRepos(workspace);
  const nested = repos.filter((r) => path.resolve(r) !== path.resolve(workspace));
  const roots = nested.length ? nested : repos;
  const synced = opts.outRoot ? listSyncedProjects(opts.outRoot) : [];
  const modules = [];
  const seen = new Set();

  for (const repoRoot of roots) {
    for (const bizSrcRoot of findSrcMainJavaDirs(repoRoot)) {
      const absSrc = path.resolve(bizSrcRoot);
      if (seen.has(absSrc)) continue;
      seen.add(absSrc);
      const moduleRoot = path.resolve(bizSrcRoot, '../../..');
      const moduleName = path.basename(moduleRoot);
      if (moduleName.endsWith('-api')) continue;

      const controllers = listControllers(bizSrcRoot);
      if (!controllers.length) continue;

      const springName = readSpringApplicationName(moduleRoot);
      const pomId = readPomArtifactId(moduleRoot) || readPomArtifactId(repoRoot);
      const syncedHit = synced.find((p) => {
        const keys = nameVariants(p);
        const mine = [
          ...nameVariants(moduleName),
          ...nameVariants(pomId),
          ...nameVariants(springName),
        ];
        return keys.some((k) => mine.includes(k));
      });
      const project = syncedHit || pomId || moduleName;

      modules.push({
        repoRoot,
        moduleRoot,
        moduleName,
        project,
        springName,
        pomId,
        bizSrcRoot,
        apiSrcRoot: pairApiSrcRoot(moduleRoot, bizSrcRoot),
        controllers,
      });
    }
  }
  return modules;
}

/**
 * @param {object} mod
 * @param {string} query
 * @returns {{ score: number, scope: 'project'|'controller', docFile?: string, listFile?: string }}
 */
function scoreModule(mod, query) {
  const q = normalizeKey(query);
  if (!q) return { score: 0, scope: 'project' };

  const keys = [mod.project, mod.moduleName, mod.springName, mod.pomId].filter(Boolean);
  const qVars = nameVariants(query);

  for (const k of keys) {
    if (normalizeKey(k) === q) return { score: 100, scope: 'project' };
  }
  for (const k of keys) {
    const kVars = nameVariants(k);
    if (kVars.some((v) => qVars.includes(v) && v.length > 1)) {
      return { score: 90, scope: 'project' };
    }
  }
  for (const k of keys) {
    const kn = normalizeKey(k);
    if (kn.startsWith(`${q}-`) || q.startsWith(`${kn}-`)) {
      return { score: 60, scope: 'project' };
    }
  }

  for (const c of mod.controllers) {
    const kebab = controllerToKebab(c.className);
    const classN = normalizeKey(c.className);
    const classBare = normalizeKey(c.className.replace(/Controller$/i, ''));
    const docN = normalizeKey(c.docFile);
    if (docN === q || classN === q || classBare === q || kebab === q) {
      return {
        score: 70,
        scope: 'controller',
        docFile: c.docFile,
        listFile: c.rel,
      };
    }
  }
  return { score: 0, scope: 'project' };
}

/**
 * 按用户说的名字选模块。同分的 -api 让给 -biz；仍多份则 ambiguous。
 * @param {object[]} modules
 * @param {string} query
 */
function matchModules(modules, query) {
  const scored = modules
    .map((mod) => ({ mod, ...scoreModule(mod, query) }))
    .filter((s) => s.score > 0)
    .sort((a, b) => b.score - a.score);

  if (!scored.length) {
    return { status: 'none', matches: [] };
  }

  const best = scored[0].score;
  let top = scored.filter((s) => s.score === best);
  if (top.length > 1) {
    const bizOnly = top.filter((s) => !s.mod.moduleName.endsWith('-api'));
    if (bizOnly.length) top = bizOnly;
  }
  if (top.length > 1) {
    const sameProject = new Set(top.map((s) => s.mod.project));
    if (sameProject.size === 1) top = [top[0]];
  }
  if (top.length > 1) {
    return { status: 'ambiguous', matches: top };
  }
  return { status: 'one', matches: top };
}

export {
  normalizeKey,
  nameVariants,
  findSrcMainJavaDirs,
  listControllers,
  pairApiSrcRoot,
  toSrcRel,
  discoverModules,
  scoreModule,
  matchModules,
};
