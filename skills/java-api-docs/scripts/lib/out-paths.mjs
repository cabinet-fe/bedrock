import fs from 'fs';
import path from 'path';

/** 默认输出根目录（相对 process.cwd()，即工作区根） */
const DEFAULT_OUT = 'api-docs';

/** 未显式传 --out 时，按顺序探测已有同步记录的目录名 */
const OUT_CANDIDATES = ['output', 'api-docs', 'docs'];

/** 产出根下唯一的约定页文件名 */
const CONVENTIONS_FILE = '_conventions.md';

/**
 * 读仓库根 pom.xml 自己的 artifactId（跳过 &lt;parent&gt; 里的）。
 * @param {string} repoRoot
 * @returns {string|null}
 */
function readPomArtifactId(repoRoot) {
  const pomPath = path.join(repoRoot, 'pom.xml');
  if (!fs.existsSync(pomPath)) return null;
  let text;
  try {
    text = fs.readFileSync(pomPath, 'utf8');
  } catch {
    return null;
  }
  const withoutParent = text.replace(/<parent>[\s\S]*?<\/parent>/i, '');
  const m = withoutParent.match(/<artifactId>\s*([^<]+?)\s*<\/artifactId>/i);
  return m ? m[1].trim() : null;
}

/**
 * 解析项目名。优先级：`--project` → 根 pom artifactId → 仓库目录名。
 * @param {string} repoRoot
 * @param {string|null|undefined} projectOpt
 */
function resolveProjectName(repoRoot, projectOpt) {
  if (projectOpt != null && String(projectOpt).trim()) {
    return String(projectOpt).trim();
  }
  const fromPom = readPomArtifactId(repoRoot);
  if (fromPom) return fromPom;
  return path.basename(path.resolve(repoRoot));
}

/**
 * 工作区根：显式 `--workspace` → `$BEDROCK_AGENT_WORKDIR` → cwd。
 * @param {string|null|undefined} workspaceOpt
 * @param {NodeJS.ProcessEnv} [env]
 */
function resolveWorkspace(workspaceOpt, env = process.env) {
  if (workspaceOpt != null && String(workspaceOpt).trim()) {
    return path.resolve(String(workspaceOpt).trim());
  }
  const fromEnv = env.BEDROCK_AGENT_WORKDIR && String(env.BEDROCK_AGENT_WORKDIR).trim();
  if (fromEnv) return path.resolve(fromEnv);
  return process.cwd();
}

/**
 * 解析输出根目录。
 * 优先级：显式 `--out` → `$BEDROCK_AGENT_OUTPUT` → 发现已有 `.sync.json` → DEFAULT_OUT
 * @param {string|null|undefined} outOpt
 * @param {{
 *   discover?: boolean,
 *   project?: string|null,
 *   workspace?: string|null,
 *   env?: NodeJS.ProcessEnv,
 * }} [opts]
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
      const found = discoverExistingOutRoot(root, opts.project);
      if (found) return found;
    }
  }
  return path.resolve(base, DEFAULT_OUT);
}

/**
 * 在工作区根下寻找已有文档产出根（含任意 `<project>/.sync.json`）。
 * @param {string} workspaceRoot
 * @param {string|null|undefined} projectOpt  若给定，优先匹配该 project 的 sync
 * @returns {string|null} 绝对路径
 */
function discoverExistingOutRoot(workspaceRoot, projectOpt) {
  const root = path.resolve(workspaceRoot || process.cwd());
  const project =
    projectOpt != null && String(projectOpt).trim() ? String(projectOpt).trim() : null;

  const hasSyncUnder = (outAbs) => {
    if (!fs.existsSync(outAbs) || !fs.statSync(outAbs).isDirectory()) return false;
    if (project) {
      return fs.existsSync(path.join(outAbs, project, '.sync.json'));
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
 * 列出产出根下所有已有 `.sync.json` 的项目名。
 * @param {string} outRoot
 * @returns {string[]}
 */
function listSyncedProjects(outRoot) {
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
      if (name.startsWith('.') || name === CONVENTIONS_FILE) return false;
      return fs.existsSync(path.join(abs, name, '.sync.json'));
    })
    .sort();
}

/**
 * 统一目录约定：
 *   <out>/_conventions.md          # 唯一约定页（全仓共享）
 *   <out>/<project>/.sync.json
 *   <out>/<project>/<kebab>.md     # 每个 Controller 一个文件
 * @param {string} repoRoot
 * @param {{ out?: string|null, project?: string|null, discoverOut?: boolean }} [opts]
 */
function resolveProjectPaths(repoRoot, opts = {}) {
  const project = resolveProjectName(repoRoot, opts.project);
  const outRoot = resolveOutRoot(opts.out, {
    discover: opts.discoverOut !== false,
    project,
    workspace: opts.workspace,
  });
  const projectRoot = path.join(outRoot, project);
  return {
    project,
    outRoot,
    projectRoot,
    syncJsonPath: path.join(projectRoot, '.sync.json'),
    conventionsPath: path.join(outRoot, CONVENTIONS_FILE),
    /** 从 `<out>/<project>/*.md` 指向唯一约定页的相对链接 */
    conventionsLink: `../${CONVENTIONS_FILE}`,
    /**
     * @deprecated 旧单文件布局；新布局为按 Controller 的 kebab.md
     * 保留字段以免破坏调用方，勿再当作唯一文档路径。
     */
    defaultDocRel: path.join(project, `${project}.md`),
  };
}

/**
 * 控制器文档在项目目录下的绝对路径。
 * @param {string} projectRoot
 * @param {string} docFile  如 `sys-user.md`
 */
function resolveControllerDocPath(projectRoot, docFile) {
  const raw = String(docFile || '').trim();
  const base = path.basename(raw);
  if (!base || base !== raw || base.includes('..')) {
    throw new Error(`非法文档文件名: ${docFile}`);
  }
  if (!/\.md$/i.test(base)) {
    throw new Error(`文档文件名须以 .md 结尾: ${docFile}`);
  }
  return path.join(projectRoot, base);
}

/**
 * 把绝对路径尽量显示成相对 base；跨盘/外置时退回绝对路径。
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
  CONVENTIONS_FILE,
  readPomArtifactId,
  resolveProjectName,
  resolveWorkspace,
  resolveOutRoot,
  discoverExistingOutRoot,
  listSyncedProjects,
  resolveProjectPaths,
  resolveControllerDocPath,
  displayPath,
};
