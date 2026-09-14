import {
  loadSettings,
  listProjects,
  createBug,
  uploadBugAttachment,
  describeApiError,
} from '../shared/api.js';

const setupView = document.getElementById('setup-view');
const reportForm = document.getElementById('report-form');
const openOptionsBtn = document.getElementById('open-options');
const titleInput = document.getElementById('bug-title');
const projectSelect = document.getElementById('project-select');
const descriptionInput = document.getElementById('bug-description');
const shotBox = document.getElementById('shot-box');
const reShotBtn = document.getElementById('re-shot');
const removeShotBtn = document.getElementById('remove-shot');
const submitBtn = document.getElementById('submit-btn');
const statusLine = document.getElementById('status-line');
const previewOverlay = document.getElementById('preview-overlay');
const previewImg = document.getElementById('preview-img');

/** @typedef {import('../shared/types.js').Settings} Settings */
/** @typedef {import('../shared/types.js').ProjectSummary} ProjectSummary */

/**
 * @typedef {object} PopupState
 * @property {Settings|null} settings
 * @property {ProjectSummary[]} projects
 * @property {string|null} screenshot PNG data URL of the visible area.
 * @property {string|null} shotError Capture failure message, shown instead of the thumbnail.
 * @property {boolean} submitting
 */

/** @type {PopupState} */
const state = { settings: null, projects: [], screenshot: null, shotError: null, submitting: false };

/**
 * @param {string} text
 * @param {'ok'|'error'} [tone]
 */
function setStatus(text, tone = 'ok') {
  statusLine.textContent = text;
  statusLine.className = `status ${tone}`;
}

function updateSubmitEnabled() {
  submitBtn.disabled = state.submitting || !projectSelect.value;
}

/**
 * Render the screenshot area: thumbnail (click to enlarge), capture failure
 * hint, or empty hint after deletion.
 */
function renderShot() {
  shotBox.textContent = '';
  if (state.screenshot) {
    const img = document.createElement('img');
    img.src = state.screenshot;
    img.alt = '当前页可见区域截图';
    img.addEventListener('click', () => {
      previewImg.src = state.screenshot;
      previewOverlay.classList.remove('hidden');
    });
    shotBox.appendChild(img);
  } else {
    shotBox.textContent = state.shotError || '未附截图';
  }
  removeShotBtn.disabled = !state.screenshot;
}

/**
 * Capture the visible area of the current tab as a PNG data URL.
 */
async function captureScreenshot() {
  try {
    const dataUrl = await chrome.tabs.captureVisibleTab(null, { format: 'png' });
    state.screenshot = dataUrl;
    state.shotError = null;
  } catch {
    state.screenshot = null;
    state.shotError = '无法截取此页面（浏览器内置页面等不支持），可点「重截」或直接提交';
  }
  renderShot();
}

/**
 * Load projects into the dropdown; failures surface 401/403 attribution.
 */
async function loadProjects() {
  projectSelect.disabled = true;
  projectSelect.textContent = '';
  const loading = document.createElement('option');
  loading.value = '';
  loading.textContent = '加载中…';
  projectSelect.appendChild(loading);
  updateSubmitEnabled();
  try {
    state.projects = await listProjects(state.settings);
    projectSelect.textContent = '';
    const placeholder = document.createElement('option');
    placeholder.value = '';
    placeholder.textContent = state.projects.length ? '请选择项目' : '无可用项目';
    projectSelect.appendChild(placeholder);
    for (const project of state.projects) {
      const option = document.createElement('option');
      option.value = String(project.id);
      option.textContent = project.name;
      projectSelect.appendChild(option);
    }
    projectSelect.disabled = false;
    if (!state.projects.length) setStatus('平台无可用项目（需令牌有 bugs:read 且为项目成员）', 'error');
  } catch (e) {
    projectSelect.textContent = '';
    const retry = document.createElement('option');
    retry.value = '';
    retry.textContent = '项目加载失败';
    projectSelect.appendChild(retry);
    setStatus(describeApiError(e), 'error');
  }
  updateSubmitEnabled();
}

/**
 * Convert the captured PNG data URL to a Blob for multipart upload.
 * @param {string} dataUrl
 * @returns {Promise<Blob>}
 */
async function dataUrlToBlob(dataUrl) {
  const response = await fetch(dataUrl);
  return response.blob();
}

/**
 * `screenshot-YYYYMMDD-HHmmss.png` attachment filename.
 * @returns {string}
 */
function screenshotFilename() {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  const stamp =
    `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}` +
    `-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `screenshot-${stamp}.png`;
}

/**
 * Create the bug first, then upload the screenshot as its attachment.
 */
async function submitReport() {
  state.submitting = true;
  updateSubmitEnabled();
  submitBtn.textContent = '提交中…';
  const projectId = Number(projectSelect.value);
  try {
    const bug = await createBug(state.settings, projectId, {
      title: titleInput.value.trim(),
      description: descriptionInput.value,
    });
    if (state.screenshot) {
      const blob = await dataUrlToBlob(state.screenshot);
      await uploadBugAttachment(state.settings, projectId, bug.id, blob, screenshotFilename());
      setStatus(`已创建缺陷 #${bug.id} 并上传截图`, 'ok');
    } else {
      setStatus(`已创建缺陷 #${bug.id}（未附截图）`, 'ok');
    }
  } catch (e) {
    setStatus(describeApiError(e), 'error');
  } finally {
    state.submitting = false;
    submitBtn.textContent = '提交缺陷';
    updateSubmitEnabled();
  }
}

openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());
reShotBtn.addEventListener('click', () => captureScreenshot());
removeShotBtn.addEventListener('click', () => {
  state.screenshot = null;
  state.shotError = null;
  renderShot();
});
projectSelect.addEventListener('change', () => {
  updateSubmitEnabled();
  statusLine.textContent = '';
});
previewOverlay.addEventListener('click', () => {
  previewOverlay.classList.add('hidden');
  previewImg.src = '';
});
reportForm.addEventListener('submit', (event) => {
  event.preventDefault();
  if (!state.submitting) submitReport();
});

// Boot: settings check, then screenshot + project list in parallel.
const settings = await loadSettings();
if (!settings) {
  setupView.classList.remove('hidden');
} else {
  state.settings = settings;
  reportForm.classList.remove('hidden');
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (tab && tab.url) descriptionInput.value = `来源：${tab.url}`;
  await Promise.all([captureScreenshot(), loadProjects()]);
}
