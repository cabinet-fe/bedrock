/**
 * Platform API client for the Bedrock extension.
 *
 * All calls hit `{baseUrl}/api/v1` with a PAT bearer token and share the same
 * error attribution: 401 -> invalid token, 403 -> missing PAT scope.
 */

/** @typedef {import('./types.js').Settings} Settings */
/** @typedef {import('./types.js').ProjectSummary} ProjectSummary */
/** @typedef {import('./types.js').BugSummary} BugSummary */
/** @typedef {import('./types.js').AttachmentSummary} AttachmentSummary */

/** @typedef {'unauthorized'|'forbidden'|'network'|'server'} ApiErrorKind */

export const SETTINGS_KEY = 'bedrockSettings';
const API_PREFIX = '/api/v1';

/** Error with a stable `kind` so callers can react to 401/403 uniformly. */
export class ApiError extends Error {
  /**
   * @param {ApiErrorKind} kind
   * @param {number} status HTTP status, 0 for network failures.
   * @param {string} message User-facing Chinese message.
   */
  constructor(kind, status, message) {
    super(message);
    this.name = 'ApiError';
    this.kind = kind;
    this.status = status;
  }
}

/**
 * @param {unknown} e
 * @returns {string} User-facing message for any thrown value.
 */
export function describeApiError(e) {
  if (e instanceof ApiError) return e.message;
  return `未知错误：${e instanceof Error ? e.message : String(e)}`;
}

/**
 * @param {unknown} value
 * @returns {value is Settings}
 */
export function isConfigured(value) {
  return (
    !!value &&
    typeof value === 'object' &&
    typeof value.baseUrl === 'string' &&
    /^https?:\/\//.test(value.baseUrl) &&
    typeof value.token === 'string' &&
    value.token.startsWith('br_')
  );
}

/**
 * Normalize a user-typed platform address to a site origin.
 * @param {string} raw
 * @returns {string|null} Origin like `http://host:port`, or null when invalid.
 */
export function normalizeBaseUrl(raw) {
  let url;
  try {
    url = new URL(raw.trim());
  } catch {
    return null;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
  return url.origin;
}

/**
 * @returns {Promise<Settings|null>}
 */
export async function loadSettings() {
  const stored = await chrome.storage.local.get(SETTINGS_KEY);
  const settings = stored[SETTINGS_KEY];
  return isConfigured(settings) ? settings : null;
}

/**
 * @param {Settings} settings
 */
export async function saveSettings(settings) {
  await chrome.storage.local.set({ [SETTINGS_KEY]: settings });
}

/**
 * @param {number} status
 * @returns {ApiErrorKind}
 */
function kindForStatus(status) {
  if (status === 401) return 'unauthorized';
  if (status === 403) return 'forbidden';
  return 'server';
}

/**
 * @param {number} status
 * @param {string} detail Server-provided message, may be empty.
 * @returns {string}
 */
function messageForStatus(status, detail) {
  if (status === 401) return '令牌无效或已过期，请在设置页检查 br_ 令牌';
  if (status === 403) {
    return `PAT scope 不足，请在平台为令牌补充 bugs:read / bugs:write 权限${detail ? `（${detail}）` : ''}`;
  }
  return `平台返回错误 HTTP ${status}${detail ? `：${detail}` : ''}`;
}

/**
 * @param {Settings} settings
 * @param {string} path Path below `/api/v1`, starting with `/`.
 * @param {{method?: string, body?: unknown}} [options] JSON body or FormData instance.
 * @returns {Promise<any>} Response envelope `data` payload.
 */
async function request(settings, path, options = {}) {
  const { method = 'GET', body } = options;
  /** @type {RequestInit} */
  const init = { method, headers: { Authorization: `Bearer ${settings.token}` } };
  if (body instanceof FormData) {
    init.body = body;
  } else if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }

  let response;
  try {
    response = await fetch(settings.baseUrl + API_PREFIX + path, init);
  } catch {
    throw new ApiError('network', 0, '无法连接平台，请检查平台地址与网络');
  }

  if (!response.ok) {
    let detail = '';
    try {
      const payload = await response.json();
      detail = payload && typeof payload.message === 'string' ? payload.message : '';
    } catch {
      // Non-JSON error body, keep detail empty.
    }
    const status = response.status;
    throw new ApiError(kindForStatus(status), status, messageForStatus(status, detail));
  }

  const payload = await response.json();
  return payload && typeof payload === 'object' && 'data' in payload ? payload.data : payload;
}

/**
 * List projects for the dropdown (PAT with `bugs:read` returns trimmed items).
 * @param {Settings} settings
 * @returns {Promise<ProjectSummary[]>}
 */
export async function listProjects(settings) {
  const page = await request(settings, '/projects?page_size=100');
  return Array.isArray(page && page.items) ? page.items : [];
}

/**
 * Create a bug in a project (PAT scope `bugs:write`).
 * @param {Settings} settings
 * @param {number} projectId
 * @param {{title: string, description?: string}} payload
 * @returns {Promise<BugSummary>}
 */
export async function createBug(settings, projectId, payload) {
  return request(settings, `/projects/${projectId}/bugs`, { method: 'POST', body: payload });
}

/**
 * Upload a screenshot as a bug attachment (PAT scope `bugs:write`).
 * @param {Settings} settings
 * @param {number} projectId
 * @param {number} bugId
 * @param {Blob} blob
 * @param {string} filename
 * @returns {Promise<AttachmentSummary>}
 */
export async function uploadBugAttachment(settings, projectId, bugId, blob, filename) {
  const formData = new FormData();
  formData.append('file', blob, filename);
  return request(settings, `/projects/${projectId}/bugs/${bugId}/attachments`, {
    method: 'POST',
    body: formData,
  });
}
