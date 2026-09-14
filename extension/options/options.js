import { loadSettings, saveSettings, normalizeBaseUrl } from '../shared/api.js';

const form = document.getElementById('options-form');
const baseUrlInput = document.getElementById('base-url');
const tokenInput = document.getElementById('pat-token');
const statusLine = document.getElementById('status-line');

/**
 * Show feedback under the form.
 * @param {string} text
 * @param {'ok'|'error'} tone
 */
function setStatus(text, tone) {
  statusLine.textContent = text;
  statusLine.className = `status ${tone}`;
}

// Prefill saved settings.
const settings = await loadSettings();
if (settings) {
  baseUrlInput.value = settings.baseUrl;
  tokenInput.value = settings.token;
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();

  const baseUrl = normalizeBaseUrl(baseUrlInput.value);
  if (!baseUrl) {
    setStatus('平台地址无效，需为 http(s) 站点地址', 'error');
    return;
  }
  const token = tokenInput.value.trim();
  if (!token.startsWith('br_')) {
    setStatus('令牌无效，需为 br_ 前缀的个人访问令牌', 'error');
    return;
  }

  // Request host access for the configured origin while still inside the
  // user gesture; the fetch from the popup needs this when CORS is locked down.
  let granted = true;
  try {
    granted = await chrome.permissions.request({ origins: [`${baseUrl}/*`] });
  } catch {
    granted = false;
  }

  await saveSettings({ baseUrl, token });
  baseUrlInput.value = baseUrl;
  setStatus(
    granted ? '已保存' : '已保存，但未授予平台访问权限，弹窗调用平台可能失败，可重新保存并在弹窗中授权',
    granted ? 'ok' : 'error'
  );
});
