import fs from 'fs';
import path from 'path';

const METHOD_PATH_RE =
  /^\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(\/[^\s`*]*)\s*$/gim;

/**
 * 规范化 path：去查询串、去尾斜杠（根 `/` 除外）、合并重复斜杠。
 * @param {string} raw
 */
function normalizePath(raw) {
  let p = String(raw || '').trim();
  const q = p.indexOf('?');
  if (q >= 0) p = p.slice(0, q);
  p = p.replace(/\/+/g, '/');
  if (p.length > 1 && p.endsWith('/')) p = p.slice(0, -1);
  return p || '/';
}

/**
 * @param {string} method
 * @param {string} p
 */
function endpointKey(method, p) {
  return `${String(method || '').toUpperCase()} ${normalizePath(p)}`;
}

/**
 * 从 Markdown 正文提取 `METHOD /path`（含代码块内独立行）。
 * @param {string} md
 * @returns {{ method: string, path: string, key: string, line: number }[]}
 */
function extractDocEndpoints(md) {
  const out = [];
  const seen = new Set();
  let m;
  const re = new RegExp(METHOD_PATH_RE.source, METHOD_PATH_RE.flags);
  while ((m = re.exec(md))) {
    const method = m[1].toUpperCase();
    const p = normalizePath(m[2]);
    const key = endpointKey(method, p);
    if (seen.has(key)) continue;
    seen.add(key);
    const line = md.slice(0, m.index).split(/\n/).length;
    out.push({ method, path: p, key, line });
  }
  return out;
}

/**
 * @param {Iterable<{ method?: string, path?: string, servicePath?: string, docFile?: string }>} endpoints
 * @param {{ useServicePath?: boolean }} [opts]
 */
function indexScriptEndpoints(endpoints, opts = {}) {
  /** @type {Map<string, { method: string, path: string, key: string, docFile: string|null }>} */
  const byKey = new Map();
  /** @type {Map<string, Set<string>>} */
  const byDoc = new Map();

  for (const ep of endpoints || []) {
    const method = String(ep.method || '').toUpperCase();
    const rawPath = opts.useServicePath ? ep.servicePath || ep.path : ep.path || ep.servicePath;
    if (!method || !rawPath) continue;
    const p = normalizePath(rawPath);
    const key = endpointKey(method, p);
    const docFile = ep.docFile || null;
    if (!byKey.has(key)) {
      byKey.set(key, { method, path: p, key, docFile });
    }
    if (docFile) {
      if (!byDoc.has(docFile)) byDoc.set(docFile, new Set());
      byDoc.get(docFile).add(key);
    }
  }
  return { byKey, byDoc };
}

/**
 * 比对文档与 list_endpoints 结果。
 *
 * @param {{
 *   projectRoot: string,
 *   endpoints: any[],
 *   docs?: string[]|null,
 *   gatewayMatched?: boolean,
 * }} opts
 * @returns {{
 *   ok: boolean,
 *   docsChecked: string[],
 *   expectedCount: number,
 *   foundCount: number,
 *   missingInDocs: { key: string, docFile: string|null }[],
 *   extraInDocs: { key: string, docFile: string, line: number }[],
 *   missingDocs: string[],
 * }}
 */
function verifyDocsAgainstEndpoints(opts) {
  const projectRoot = path.resolve(opts.projectRoot);
  const { byKey, byDoc } = indexScriptEndpoints(opts.endpoints, {
    useServicePath: opts.gatewayMatched === false,
  });

  let docNames = opts.docs && opts.docs.length ? [...opts.docs] : [...byDoc.keys()];
  docNames = [...new Set(docNames.map((d) => path.basename(String(d).trim())).filter(Boolean))].sort();

  const missingInDocs = [];
  const extraInDocs = [];
  const missingDocs = [];
  const docsChecked = [];
  let foundCount = 0;
  let expectedCount = 0;

  for (const docFile of docNames) {
    const abs = path.join(projectRoot, docFile);
    if (!fs.existsSync(abs)) {
      missingDocs.push(docFile);
      const expected = byDoc.get(docFile);
      if (expected) {
        expectedCount += expected.size;
        for (const key of expected) {
          missingInDocs.push({ key, docFile });
        }
      }
      continue;
    }
    docsChecked.push(docFile);
    const md = fs.readFileSync(abs, 'utf8');
    const found = extractDocEndpoints(md);
    foundCount += found.length;

    const expectedKeys = byDoc.get(docFile) || new Set();
    // 若按 docFile 关联不到（旧文档），退回：文档中的 key 必须出现在全局 byKey
    const strict = expectedKeys.size > 0;
    if (strict) expectedCount += expectedKeys.size;

    const foundKeys = new Set(found.map((f) => f.key));

    if (strict) {
      for (const key of expectedKeys) {
        if (!foundKeys.has(key)) missingInDocs.push({ key, docFile });
      }
      for (const f of found) {
        if (!expectedKeys.has(f.key)) {
          extraInDocs.push({ key: f.key, docFile, line: f.line });
        }
      }
    } else {
      for (const f of found) {
        if (!byKey.has(f.key)) {
          extraInDocs.push({ key: f.key, docFile, line: f.line });
        }
      }
    }
  }

  const ok =
    missingDocs.length === 0 && missingInDocs.length === 0 && extraInDocs.length === 0;

  return {
    ok,
    docsChecked,
    expectedCount,
    foundCount,
    missingInDocs,
    extraInDocs,
    missingDocs,
  };
}

export {
  METHOD_PATH_RE,
  normalizePath,
  endpointKey,
  extractDocEndpoints,
  indexScriptEndpoints,
  verifyDocsAgainstEndpoints,
};
