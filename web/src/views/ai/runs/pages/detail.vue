<script setup lang="ts">
defineOptions({ name: "AiRunDetail" });

import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { saveBlob } from "@cat-kit/fe";
import { message } from "@veltra/desktop";

import { agentRunArtifactURL, agentRunLogsWSURL, cancelRun, getAgent, getRun } from "@/api/ai";
import { getAccessToken } from "@/api/http";
import type { AgentRun } from "@/api/types";
import BuildLogViewer, { resolveBuildLogStatus } from "@/components/build-log-viewer";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";
import { useTabsStore } from "@/stores/tabs";

const route = useRoute();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const run = ref<AgentRun | null>(null);
const loading = ref(true);
const acting = ref(false);

function parseRouteId(raw: unknown): number | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

const canExecute = computed(() => hasPermission("ai_agents:execute"));
// Layout keys detail by path and keep-alive caches the instance. Freeze the id at
// setup so deactivated instances do not re-read the global route (which loses :id).
const detailPath = route.path;
const runId = parseRouteId(route.params.id);

const isLive = computed(() => {
  const s = run.value?.status;
  return s === "queued" || s === "running";
});

const logViewerStatus = computed(() => resolveBuildLogStatus(run.value?.status));

const canCancel = computed(() => {
  if (!canExecute.value || !run.value) return false;
  return run.value.status === "queued" || run.value.status === "running";
});

const logsWsURL = computed(() => {
  if (runId == null) return undefined;
  const token = getAccessToken();
  if (!token) return undefined;
  return agentRunLogsWSURL(runId, token);
});

async function syncTabTitle(r: AgentRun) {
  let title = `运行 #${r.id}`;
  try {
    const agent = await getAgent(r.agent_id);
    if (agent.name) title = agent.name;
  } catch {
    /* keep fallback title */
  }
  tabsStore.updateTitle(detailPath, title);
}

async function load() {
  if (runId == null) {
    message.error("无效 ID");
    loading.value = false;
    return;
  }
  try {
    run.value = await getRun(runId);
    await syncTabTitle(run.value);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

async function onLogRefresh() {
  if (runId == null) return;
  try {
    run.value = await getRun(runId);
  } catch {
    /* ignore */
  }
}

async function onCancel() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    await cancelRun(run.value.id);
    run.value = await getRun(run.value.id);
    message.success("已取消");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "取消失败");
  } finally {
    acting.value = false;
  }
}

async function onDownloadArtifact() {
  const token = getAccessToken();
  if (!token || !run.value) return;
  try {
    const res = await fetch(agentRunArtifactURL(run.value.id), {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error((await res.text()) || `HTTP ${res.status}`);
    }
    const blob = await res.blob();
    const cd = res.headers.get("Content-Disposition") || "";
    const m = /filename="?([^"]+)"?/.exec(cd);
    const name = m?.[1] || `run-${run.value.id}.zip`;
    saveBlob(blob, name);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "下载失败");
  }
}

onMounted(async () => {
  await load();
});
</script>

<template>
  <u-scroll>
    <div class="page">
      <header class="page-header">
        <div class="page-header__lead">
          <div v-if="run" class="page-header__title">
            <h2>运行 #{{ run.id }}</h2>
            <u-tag size="small" :type="tagType(run.status, JOB_STATUS_TAG)">{{ run.status }}</u-tag>
          </div>
        </div>
        <div v-if="run" class="page-header__actions">
          <u-button v-if="canCancel" plain type="danger" :disabled="acting" @click="onCancel">
            取消
          </u-button>
          <u-button
            v-if="run.status === 'success' && run.artifact_path"
            plain
            type="primary"
            :disabled="acting"
            @click="onDownloadArtifact"
          >
            下载制品
          </u-button>
        </div>
      </header>

      <div v-if="loading" class="state">加载中…</div>
      <template v-else-if="run">
        <section class="panel meta-panel">
          <div class="meta-grid">
            <div class="meta-item">
              <span class="meta-label">Agent</span>
              <span class="meta-value">{{ run.agent_id }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">触发</span>
              <u-tag size="small" :type="tagType(run.trigger_type, TRIGGER_TYPE_TAG)">
                {{ run.trigger_type }}
              </u-tag>
            </div>
            <div class="meta-item">
              <span class="meta-label">构建运行</span>
              <span class="meta-value">{{ run.build_run_id ?? "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">项目</span>
              <span class="meta-value">{{ run.project_id ?? "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">文档节点</span>
              <span class="meta-value">{{ run.doc_node_id ?? "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">运行时间</span>
              <span class="meta-value">{{ formatDurationMs(run.duration_ms) || "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">开始时间</span>
              <span class="meta-value">{{ formatDateTime(run.started_at) || "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">结束时间</span>
              <span class="meta-value">{{ formatDateTime(run.finished_at) || "—" }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">创建时间</span>
              <span class="meta-value">{{ formatDateTime(run.created_at) || "—" }}</span>
            </div>
          </div>
          <p v-if="run.error_message" class="error-msg">{{ run.error_message }}</p>
        </section>

        <section class="section">
          <h3 class="section__title">运行日志</h3>
          <BuildLogViewer
            :run-id="run.id"
            :live="isLive"
            :status="logViewerStatus"
            :ws-url="logsWsURL"
            :hydrate-http="false"
            @refresh="onLogRefresh"
          />
        </section>
      </template>
      <u-empty v-else text="运行记录不存在或无权访问" />
    </div>
  </u-scroll>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.page {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, large);
  min-width: 0;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.page-header__lead {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
}

.page-header__title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.page-header__title h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  line-height: 1.3;
  color: fn.use-var(text-color, title);
}

.page-header__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-left: auto;
}

.section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

.section__title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.4;
  color: fn.use-var(text-color, title);
}

.panel {
  min-width: 0;
  padding: fn.use-var(gap, default);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.meta-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 14px 20px;
}

.meta-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  min-width: 0;
}

.meta-label {
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}

.meta-value {
  font-size: 13px;
  font-weight: 500;
  color: fn.use-var(text-color, main);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.error-msg {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: fn.use-var(color, danger);
  overflow-wrap: anywhere;
}

.state {
  opacity: 0.7;
}
</style>
