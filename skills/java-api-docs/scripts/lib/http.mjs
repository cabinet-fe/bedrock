/**
 * 规范化 Bedrock host：去尾斜杠，拒绝空值。
 * @param {string} host
 * @returns {string}
 */
function normalizeHost(host) {
  const raw = String(host || '').trim();
  if (!raw) throw new Error('BEDROCK_HOST 不能为空');
  return raw.replace(/\/+$/, '');
}

/**
 * 拼开放 API 路径：`{host}/api/v1/...`
 * @param {string} host
 * @param {string} apiPath  以 / 开头，如 `/projects/foo/docs/push`
 */
function apiURL(host, apiPath) {
  const base = normalizeHost(host);
  const p = apiPath.startsWith('/') ? apiPath : `/${apiPath}`;
  return `${base}/api/v1${p}`;
}

/**
 * @param {Response} res
 * @returns {Promise<{ ok: boolean, status: number, data: unknown, text: string }>}
 */
async function readResponseJSON(res) {
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { raw: text };
    }
  }
  return { ok: res.ok, status: res.status, data, text };
}

function requireFetch(fetchImpl) {
  const impl = fetchImpl || globalThis.fetch;
  if (typeof impl !== 'function') {
    throw new Error('当前 Node 无 fetch；请使用 Node ≥ 18');
  }
  return impl;
}

function requirePAT(token) {
  const t = String(token || '').trim();
  if (!t) throw new Error('PAT 不能为空');
  return t;
}

/**
 * POST JSON，Bearer token。
 * @param {string} url
 * @param {{ token: string, body: unknown, fetchImpl?: typeof fetch }} opts
 * @returns {Promise<{ ok: boolean, status: number, data: unknown, text: string }>}
 */
async function postJSON(url, opts) {
  const token = requirePAT(opts.token);
  const fetchImpl = requireFetch(opts.fetchImpl);
  const res = await fetchImpl(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(opts.body ?? {}),
  });
  return readResponseJSON(res);
}

/**
 * GET JSON，Bearer token。
 * @param {string} url
 * @param {{ token: string, fetchImpl?: typeof fetch }} opts
 * @returns {Promise<{ ok: boolean, status: number, data: unknown, text: string }>}
 */
async function getJSON(url, opts) {
  const token = requirePAT(opts.token);
  const fetchImpl = requireFetch(opts.fetchImpl);
  const res = await fetchImpl(url, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/json',
    },
  });
  return readResponseJSON(res);
}

/**
 * 从 API 错误响应里尽量抽出可读消息。
 * @param {{ status: number, data: unknown, text: string }} res
 */
function errorMessage(res) {
  const d = res.data;
  if (d && typeof d === 'object') {
    const obj = /** @type {Record<string, unknown>} */ (d);
    if (typeof obj.message === 'string' && obj.message) return obj.message;
    if (typeof obj.error === 'string' && obj.error) return obj.error;
    if (obj.error && typeof obj.error === 'object') {
      const e = /** @type {Record<string, unknown>} */ (obj.error);
      if (typeof e.message === 'string' && e.message) return e.message;
    }
  }
  if (res.text) return res.text.slice(0, 200);
  return `HTTP ${res.status}`;
}

export { normalizeHost, apiURL, postJSON, getJSON, errorMessage };
