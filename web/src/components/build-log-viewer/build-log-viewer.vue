<script setup lang="ts">
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import {
  ArrowDown,
  ArrowUp,
  Check,
  Close,
  Copy,
  Maximum,
  Search,
  ZoomOut,
} from "@veltra/icons/normal";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  shallowRef,
  useTemplateRef,
  watch,
} from "vue";

import { buildRunLogsWSURL, getBuildRunLog } from "@/api/cicd";
import { getAccessToken } from "@/api/http";

import {
  BUILD_LOG_STATUS_LABEL,
  BUILD_LOG_STATUS_TAG,
  normalizeLogLines,
  SEARCH_OPTIONS,
  TERMINAL_OPTIONS,
  writeLinesToTerminal,
  type BuildLogStatus,
} from "./helper";

const props = withDefaults(
  defineProps<{
    runId: number;
    live?: boolean;
    status?: BuildLogStatus;
    initialLogs?: string;
    height?: string;
    /**
     * Override log WebSocket URL. When omitted, uses build-run logs WS.
     * Pass a full `ws(s)://…` URL (e.g. agent run logs).
     */
    wsUrl?: string;
    /** When false, skip HTTP log hydrate (WS / initialLogs only). Default true. */
    hydrateHttp?: boolean;
  }>(),
  {
    live: false,
    status: "pending",
    height: "480px",
    hydrateHttp: true,
  },
);

const emit = defineEmits<{
  refresh: [];
}>();

const terminalContainer = useTemplateRef<HTMLDivElement>("terminalContainer");

const autoScroll = shallowRef(true);
const isFullscreen = shallowRef(false);
const searchOpen = shallowRef(false);
const searchQuery = shallowRef("");
const matchCount = shallowRef(0);
const currentMatch = shallowRef(0);
const lineCount = shallowRef(0);
const copied = shallowRef(false);

const xterm = shallowRef<Terminal | null>(null);
const fitAddon = shallowRef<FitAddon | null>(null);
const searchAddon = shallowRef<SearchAddon | null>(null);

const logsBuffer = shallowRef<string[]>([]);
const initialized = shallowRef(false);
const userScrolling = shallowRef(false);

let ws: WebSocket | null = null;
let copiedTimer: ReturnType<typeof setTimeout> | null = null;

const statusLabel = computed(() => BUILD_LOG_STATUS_LABEL[props.status] ?? props.status);
const statusTagType = computed(() => BUILD_LOG_STATUS_TAG[props.status]);
const terminalHeight = computed(() => (isFullscreen.value ? "calc(100vh - 52px)" : props.height));

function fitTerminal() {
  if (!fitAddon.value || !xterm.value) return;
  try {
    fitAddon.value.fit();
  } catch {
    // ignore fit errors during transitions
  }
}

function scrollToBottom() {
  xterm.value?.scrollToBottom();
}

function appendToTerminal(data: string, withLeadingBreak = true) {
  const term = xterm.value;
  if (term) {
    term.write(withLeadingBreak ? `\r\n${data}` : data);
    lineCount.value += 1;
    if (autoScroll.value) {
      scrollToBottom();
    }
    return;
  }
  logsBuffer.value.push(data);
}

function initTerminal() {
  const el = terminalContainer.value;
  if (!el || initialized.value) return;

  const term = new Terminal(TERMINAL_OPTIONS);
  const fit = new FitAddon();
  const search = new SearchAddon();

  term.loadAddon(fit);
  term.loadAddon(search);

  search.onDidChangeResults((result) => {
    if (result) {
      matchCount.value = result.resultCount;
      currentMatch.value = result.resultIndex === -1 ? 0 : result.resultIndex + 1;
    } else {
      matchCount.value = 0;
      currentMatch.value = 0;
    }
  });

  term.open(el);

  try {
    const webglAddon = new WebglAddon();
    webglAddon.onContextLoss(() => {
      webglAddon.dispose();
    });
    term.loadAddon(webglAddon);
  } catch {
    // WebGL not supported, fall back to canvas renderer
  }

  fit.fit();

  term.onScroll(() => {
    const viewport = term.buffer.active;
    const isAtBottom = viewport.baseY <= viewport.viewportY;
    if (!isAtBottom && !userScrolling.value) {
      userScrolling.value = true;
      autoScroll.value = false;
    }
    if (isAtBottom && userScrolling.value) {
      userScrolling.value = false;
    }
  });

  xterm.value = term;
  fitAddon.value = fit;
  searchAddon.value = search;
  initialized.value = true;

  if (logsBuffer.value.length > 0) {
    term.write(logsBuffer.value.join("\r\n"));
    lineCount.value = logsBuffer.value.length;
    logsBuffer.value = [];
    if (autoScroll.value) {
      scrollToBottom();
    }
  }
}

function disposeTerminal() {
  initialized.value = false;
  xterm.value?.dispose();
  xterm.value = null;
  fitAddon.value = null;
  searchAddon.value = null;
}

function setLogs(text: string) {
  const lines = normalizeLogLines(text);
  const term = xterm.value;
  if (term) {
    writeLinesToTerminal(term, lines);
    lineCount.value = lines.length;
    if (autoScroll.value) {
      scrollToBottom();
    }
  } else {
    logsBuffer.value = lines;
    lineCount.value = lines.length;
  }
}

async function hydrateLogHTTP() {
  if (!props.hydrateHttp || !props.runId) return;
  try {
    const text = await getBuildRunLog(props.runId);
    if (text) {
      setLogs(text);
    }
  } catch {
    // no log yet
  }
}

function isControlMessage(text: string): boolean {
  return (
    text === "__REFRESH__" || text.startsWith("__REFRESH__") || text.startsWith("__TERMINAL__:")
  );
}

function disconnectWS() {
  if (ws) {
    ws.close();
    ws = null;
  }
}

function shouldConnectWS(): boolean {
  if (!props.runId) return false;
  if (props.live) return true;
  // Agent runs (and similar): no HTTP log API — open WS once for file replay.
  return !props.hydrateHttp && !!props.wsUrl;
}

function connectWS() {
  disconnectWS();
  if (!shouldConnectWS()) return;

  const token = getAccessToken();
  if (!token) return;

  const url = props.wsUrl || buildRunLogsWSURL(props.runId, token);
  ws = new WebSocket(url);
  ws.onmessage = (ev) => {
    const text = String(ev.data);
    if (isControlMessage(text)) {
      emit("refresh");
      return;
    }
    appendToTerminal(text);
  };
  ws.onerror = () => {
    void hydrateLogHTTP();
  };
}

function performSearch(direction: "next" | "prev" = "next") {
  const addon = searchAddon.value;
  const query = searchQuery.value;
  if (!addon || !query) {
    matchCount.value = 0;
    currentMatch.value = 0;
    return;
  }

  if (direction === "next") {
    addon.findNext(query, SEARCH_OPTIONS);
  } else {
    addon.findPrevious(query, SEARCH_OPTIONS);
  }
}

function onSearchInput(value: string) {
  searchQuery.value = value;
  matchCount.value = 0;
  currentMatch.value = 0;
  if (value) {
    performSearch("next");
  } else {
    searchAddon.value?.clearDecorations();
  }
}

function closeSearch() {
  searchOpen.value = false;
  searchQuery.value = "";
  matchCount.value = 0;
  currentMatch.value = 0;
  searchAddon.value?.clearDecorations();
}

async function copyAll() {
  const term = xterm.value;
  if (!term) return;
  term.selectAll();
  const text = term.getSelection();
  term.clearSelection();
  if (!text) return;
  await navigator.clipboard.writeText(text);
  copied.value = true;
  if (copiedTimer) clearTimeout(copiedTimer);
  copiedTimer = setTimeout(() => {
    copied.value = false;
  }, 2000);
}

function enableAutoScroll() {
  autoScroll.value = true;
  userScrolling.value = false;
  scrollToBottom();
}

function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value;
}

function appendLine(line: string) {
  appendToTerminal(line);
}

function reconnect() {
  connectWS();
}

function resetViewer() {
  logsBuffer.value = [];
  lineCount.value = 0;
  autoScroll.value = true;
  userScrolling.value = false;
  xterm.value?.clear();
  disconnectWS();
}

async function loadLogs() {
  if (props.initialLogs !== undefined) {
    setLogs(props.initialLogs);
    return;
  }
  await hydrateLogHTTP();
}

defineExpose({
  appendLine,
  reconnect,
});

watch(
  () => props.runId,
  async () => {
    resetViewer();
    await loadLogs();
    connectWS();
  },
);

watch(
  () => props.live,
  (live, wasLive) => {
    if (live) {
      connectWS();
      return;
    }
    // Was streaming → stop. Avoid reconnecting on live→false (would replay duplicates).
    if (wasLive) {
      disconnectWS();
    }
  },
);

watch(
  () => props.wsUrl,
  () => {
    if (shouldConnectWS()) {
      connectWS();
    }
  },
);

watch(
  () => props.initialLogs,
  (text) => {
    if (text !== undefined) {
      setLogs(text);
    }
  },
);

watch([autoScroll, lineCount], () => {
  if (autoScroll.value) {
    scrollToBottom();
  }
});

watch(isFullscreen, async () => {
  await nextTick();
  setTimeout(fitTerminal, 100);
});

onMounted(async () => {
  initTerminal();
  await loadLogs();
  connectWS();
  window.addEventListener("resize", fitTerminal);
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", fitTerminal);
  disconnectWS();
  if (copiedTimer) clearTimeout(copiedTimer);
  disposeTerminal();
});
</script>

<template>
  <div class="build-log-viewer" :class="{ 'build-log-viewer--fullscreen': isFullscreen }">
    <div class="build-log-viewer__toolbar">
      <div class="build-log-viewer__meta">
        <u-tag size="small" :type="statusTagType">{{ statusLabel }}</u-tag>
        <span class="build-log-viewer__lines">{{ lineCount }} 行</span>
      </div>
      <div class="build-log-viewer__actions">
        <div v-if="searchOpen" class="build-log-viewer__search">
          <u-icon :size="14" class="build-log-viewer__search-icon">
            <Search />
          </u-icon>
          <input
            class="build-log-viewer__search-input"
            :value="searchQuery"
            placeholder="搜索日志..."
            autofocus
            @input="onSearchInput(($event.target as HTMLInputElement).value)"
            @keydown.enter.exact="performSearch('next')"
            @keydown.enter.shift="performSearch('prev')"
            @keydown.esc="closeSearch"
          />
          <span v-if="searchQuery" class="build-log-viewer__search-count">
            {{ matchCount > 0 ? `${currentMatch}/${matchCount}` : "无匹配" }}
          </span>
          <u-button
            text
            circle
            size="small"
            :icon="ArrowUp"
            :icon-size="14"
            :disabled="matchCount === 0"
            @click="performSearch('prev')"
          />
          <u-button
            text
            circle
            size="small"
            :icon="ArrowDown"
            :icon-size="14"
            :disabled="matchCount === 0"
            @click="performSearch('next')"
          />
          <u-button text circle size="small" :icon="Close" :icon-size="14" @click="closeSearch" />
        </div>
        <template v-else>
          <u-button
            text
            circle
            size="small"
            :icon="Search"
            :icon-size="14"
            @click="searchOpen = true"
          />
        </template>
        <u-button v-if="!autoScroll" text size="small" @click="enableAutoScroll">跟随</u-button>
        <u-button
          text
          circle
          size="small"
          :icon="copied ? Check : Copy"
          :icon-size="14"
          @click="copyAll"
        />
        <u-button
          text
          circle
          size="small"
          :icon="isFullscreen ? ZoomOut : Maximum"
          :icon-size="14"
          @click="toggleFullscreen"
        />
      </div>
    </div>

    <div
      ref="terminalContainer"
      class="build-log-viewer__terminal"
      :style="{ height: terminalHeight }"
    />
  </div>
</template>

<style scoped>
.build-log-viewer {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid #27272a;
  border-radius: 8px;
  background: #09090b;
}

.build-log-viewer--fullscreen {
  position: fixed;
  inset: 0;
  z-index: 1000;
  border: 0;
  border-radius: 0;
}

.build-log-viewer__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid rgb(39 39 42 / 60%);
  background: rgb(24 24 27 / 80%);
}

.build-log-viewer__meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.build-log-viewer__lines {
  font-size: 12px;
  color: #71717a;
}

.build-log-viewer__actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.build-log-viewer__search {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border: 1px solid #3f3f46;
  border-radius: 6px;
  background: #27272a;
}

.build-log-viewer__search-icon {
  color: #71717a;
  flex-shrink: 0;
}

.build-log-viewer__search-input {
  width: 160px;
  border: 0;
  background: transparent;
  color: #e4e4e7;
  font-size: 13px;
  outline: none;
}

.build-log-viewer__search-input::placeholder {
  color: #71717a;
}

.build-log-viewer__search-count {
  font-size: 12px;
  color: #71717a;
  white-space: nowrap;
}

.build-log-viewer__terminal {
  padding: 8px;
  width: 100%;
  overflow: hidden;
}

.build-log-viewer__terminal :deep(.xterm) {
  height: 100%;
  width: 100%;
}

.build-log-viewer__terminal :deep(.xterm-viewport) {
  scrollbar-width: thin;
  scrollbar-color: #52525b transparent;
}

.build-log-viewer__terminal :deep(.xterm-viewport::-webkit-scrollbar) {
  width: 8px;
}

.build-log-viewer__terminal :deep(.xterm-viewport::-webkit-scrollbar-track) {
  background: transparent;
}

.build-log-viewer__terminal :deep(.xterm-viewport::-webkit-scrollbar-thumb) {
  background: #52525b;
  border-radius: 9999px;
}

.build-log-viewer__terminal :deep(.xterm-viewport::-webkit-scrollbar-thumb:hover) {
  background: #71717a;
}

.build-log-viewer :deep(.u-button) {
  color: #d4d4d8;
}

.build-log-viewer :deep(.u-button:hover) {
  background: #3f3f46;
  color: #fff;
}
</style>
