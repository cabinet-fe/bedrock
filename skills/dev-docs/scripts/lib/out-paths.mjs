import fs from 'fs';
import path from 'path';

/** 默认产出根（相对 workspace / cwd） */
const DEFAULT_OUT = 'dev-docs';

/** 未显式传 --out/--dir 时，按顺序探测已有同步记录的目录名 */
const OUT_CANDIDATES = ['output', 'dev-docs', 'docs'];

/**
 * 解析产出根。
 * 优先级：显式 --out/--dir → $BEDROCK_AGENT_OUTPUT → 发现已有 `.sync.json` → DEFAULT_OUT
 * @param {string|null|undefined} outOpt
 * @param {{ discover?: boolean, repoKey?: string|null, workspace?: string|null, env?: NodeJS.ProcessEnv }} [opts]
 */
function resolveOutRoot(outOpt, opts = {}) {
  const env = opts.env || process.env;
  const explicit = outOpt != null && String(outOpt).trim() ? String(outOpt).trim() : null;
  const workspace = opts.workspace ? path.resolve(opts.workspace) : null;
  const cwd = process.cwd();
  const base = workspace || cwd;

  if (explicit) {
    if (path.isAbsolute(explicit)) return path.resolve(explicit);
    const fromCwd = path.resolve(cwd, explicit);
    if (workspace && path.resolve(workspace) !== path.resolve(cwd)) {
      const fromWs = path.resolve(workspace, explicit);
      if (fs.existsSync(fromWs) && !fs.existsSync(fromCwd)) return fromWs;
    }
    return fromCwd;
  }

  const fromEnv =
    env.BEDROCK_AGENT_OUTPUT && String(env.BEDROCK_AGENT_OUTPUT).trim()
      ? String(env.BEDROCK_AGENT_OUTPUT).trim()
      : null;
  if (fromEnv) {
    if (path.isAbsolute(fromEnv)) return path.resolve(fromEnv);
    return path.resolve(base, fromEnv);
  }

  const discover = opts.discover !== false;
  if (discover) {
    for (const root of [workspace, cwd].filter(Boolean)) {
      const found = discoverExistingOutRoot(root, opts.repoKey);
      if (found) return found;
    }
  }
  return path.resolve(base, DEFAULT_OUT);
}

/**
 * @param {string} workspaceRoot
 * @param {string|null|undefined} repoKeyOpt
 * @returns {string|null}
 */
function discoverExistingOutRoot(workspaceRoot, repoKeyOpt) {
  const root = path.resolve(workspaceRoot || process.cwd());
  const repoKey =
    repoKeyOpt != null && String(repoKeyOpt).trim() ? String(repoKeyOpt).trim() : null;

  const hasSyncUnder = (outAbs) => {
    if (!fs.existsSync(outAbs) || !fs.statSync(outAbs).isDirectory()) return false;
    if (repoKey) {
      return fs.existsSync(path.join(outAbs, repoKey, '.sync.json'));
    }
    let entries;
    try {
      entries = fs.readdirSync(outAbs);
    } catch {
      return false;
    }
    return entries.some((name) => {
      if (name.startsWith('.')) return false;
      try {
        return fs.existsSync(path.join(outAbs, name, '.sync.json'));
      } catch {
        return false;
      }
    });
  };

  for (const name of OUT_CANDIDATES) {
    const abs = path.join(root, name);
    if (hasSyncUnder(abs)) return abs;
  }
  return null;
}

/**
 * 解析仓产出子目录名（repoKey）。优先级：`--repo-key` → 仓库目录名。
 * @param {string} repoRoot
 * @param {string|null|undefined} repoKeyOpt
 */
function resolveRepoKey(repoRoot, repoKeyOpt) {
  if (repoKeyOpt != null && String(repoKeyOpt).trim()) {
    return String(repoKeyOpt).trim();
  }
  return path.basename(path.resolve(repoRoot));
}

/**
 * 列出产出根下所有已有 `.sync.json` 的 repoKey。
 * @param {string} outRoot
 * @returns {string[]}
 */
function listSyncedRepoKeys(outRoot) {
  const abs = path.resolve(outRoot);
  if (!fs.existsSync(abs)) return [];
  let entries;
  try {
    entries = fs.readdirSync(abs);
  } catch {
    return [];
  }
  return entries
    .filter((name) => {
      if (name.startsWith('.')) return false;
      return fs.existsSync(path.join(abs, name, '.sync.json'));
    })
    .sort();
}

/**
 * 统一目录约定：
 *   <out>/<repoKey>/.sync.json
 *   <out>/<repoKey>/…/*.md（含子路径）
 * @param {string} repoRoot
 * @param {{ out?: string|null, repoKey?: string|null, discoverOut?: boolean, workspace?: string|null }} [opts]
 */
function resolveRepoPaths(repoRoot, opts = {}) {
  const repoKey = resolveRepoKey(repoRoot, opts.repoKey);
  const outRoot = resolveOutRoot(opts.out, {
    discover: opts.discoverOut !== false,
    repoKey,
    workspace: opts.workspace,
  });
  const repoOutDir = path.join(outRoot, repoKey);
  return {
    repoKey,
    outRoot,
    repoOutDir,
    syncJsonPath: path.join(repoOutDir, '.sync.json'),
  };
}

/**
 * 规范化 docs[] 条目：相对仓产出子目录，可含子路径；拒绝 `..` / 绝对路径。
 * @param {string} raw
 * @returns {string}
 */
function normalizeDocRel(raw) {
  const s = String(raw || '').trim().replace(/\\/g, '/');
  if (!s) throw new Error('空文档路径');
  if (path.isAbsolute(s) || s.startsWith('/')) {
    throw new Error(`文档路径不能是绝对路径: ${raw}`);
  }
  const parts = s.split('/').filter((p) => p.length > 0);
  if (!parts.length) throw new Error(`空文档路径: ${raw}`);
  for (const seg of parts) {
    if (seg === '.' || seg === '..') {
      throw new Error(`文档路径含非法段: ${raw}`);
    }
  }
  return parts.join('/');
}

/**
 * 检查 sync.docs[] 在仓产出目录下是否齐全。
 * @param {string} repoOutDir
 * @param {unknown} docs
 * @returns {{ missing: string[], checked: string[] }}
 */
function findMissingLocalDocs(repoOutDir, docs) {
  if (!Array.isArray(docs) || !docs.length) {
    return { missing: [], checked: [] };
  }
  const checked = [];
  const missing = [];
  for (const raw of docs) {
    let rel;
    try {
      rel = normalizeDocRel(raw);
    } catch {
      missing.push(String(raw));
      continue;
    }
    checked.push(rel);
    const abs = path.join(repoOutDir, ...rel.split('/'));
    if (!fs.existsSync(abs) || !fs.statSync(abs).isFile()) {
      missing.push(rel);
    }
  }
  return { missing, checked };
}

/**
 * @param {string} baseDir
 * @param {string} absPath
 */
function displayPath(baseDir, absPath) {
  const rel = path.relative(baseDir, absPath);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) return absPath;
  return rel;
}

export {
  DEFAULT_OUT,
  OUT_CANDIDATES,
  resolveOutRoot,
  discoverExistingOutRoot,
  resolveRepoKey,
  listSyncedRepoKeys,
  resolveRepoPaths,
  normalizeDocRel,
  findMissingLocalDocs,
  displayPath,
};
