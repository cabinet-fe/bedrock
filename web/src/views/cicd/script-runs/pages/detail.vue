<script setup lang="ts">
defineOptions({ name: "CicdScriptRunDetail" });

import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import {
  cancelScriptRun,
  getScriptJob,
  getScriptRun,
  retryScriptRun,
  scriptRunLogsWSURL,
} from "@/api/cicd";
import { getAccessToken } from "@/api/http";
import type { ScriptRun } from "@/api/types";
import BuildLogViewer, { resolveBuildLogStatus } from "@/components/build-log-viewer";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { BUILD_STAGE_TAG, JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";
import { useTabsStore } from "@/stores/tabs";

const route = useRoute();
const router = useRouter();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const run = ref<ScriptRun | null>(null);
const loading = ref(true);
const acting = ref(false);
const logViewerRef = ref<InstanceType<typeof BuildLogViewer> | null>(null);

function parseRouteId(raw: unknown): number | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

const canExecute = computed(() => hasPermission("cicd_script_jobs:execute"));
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

const canRetry = computed(
  () =>
    canExecute.value &&
    !!run.value &&
    ["failed", "cancelled", "interrupted", "success"].includes(run.value.status),
);

const wsUrl = computed(() => {
  const token = getAccessToken();
  if (!run.value || !token) return undefined;
  return scriptRunLogsWSURL(run.value.id, token);
});

async function syncTabTitle(r: ScriptRun) {
  let title = `脚本 #${r.run_number}`;
  try {
    const job = await getScriptJob(r.script_job_id);
    if (job.name) title = `${job.name} #${r.run_number}`;
  } catch {
    /* keep fallback */
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
    run.value = await getScriptRun(runId);
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
    run.value = await getScriptRun(runId);
  } catch {
    /* ignore */
  }
}

async function onCancel() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    run.value = await cancelScriptRun(run.value.id);
    message.success("已取消");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "取消失败");
  } finally {
    acting.value = false;
  }
}

async function onRetry() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    const next = await retryScriptRun(run.value.id);
    message.success(`已创建重试 #${next.run_number}`);
    await router.push({ name: "cicd-script-run-detail", params: { id: String(next.id) } });
  } catch (err) {
    message.error(err instanceof Error ? err.message : "重试失败");
  } finally {
    acting.value = false;
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
            <h2>脚本 #{{ run.run_number }}</h2>
            <u-tag size="small" :type="tagType(run.status, JOB_STATUS_TAG)">{{ run.status }}</u-tag>
          </div>
        </div>
        <div v-if="run" class="page-header__actions">
          <u-button v-if="canCancel" plain type="danger" :disabled="acting" @click="onCancel">
            取消
          </u-button>
          <u-button v-if="canRetry" type="primary" :disabled="acting" @click="onRetry"
            >重试</u-button
          >
        </div>
      </header>

      <div v-if="loading" class="state">加载中…</div>
      <template v-else-if="run">
        <section class="panel meta-panel">
          <div class="meta-grid">
            <div class="meta-item">
              <span class="meta-label">阶段</span>
              <u-tag size="small" :type="tagType(run.stage, BUILD_STAGE_TAG)">
                {{ run.stage || "—" }}
              </u-tag>
            </div>
            <div class="meta-item">
              <span class="meta-label">触发</span>
              <u-tag size="small" :type="tagType(run.trigger_type, TRIGGER_TYPE_TAG)">
                {{ run.trigger_type }}
              </u-tag>
            </div>
            <div class="meta-item">
              <span class="meta-label">任务 ID</span>
              <span class="meta-value">{{ run.script_job_id }}</span>
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
          <h3 class="section__title">脚本日志</h3>
          <BuildLogViewer
            ref="logViewerRef"
            :run-id="run.id"
            :live="isLive"
            :status="logViewerStatus"
            :ws-url="wsUrl"
            :hydrate-http="false"
            @refresh="onLogRefresh"
          />
        </section>
      </template>
      <div v-else class="page-empty">
        <u-empty text="执行记录不存在或无权访问" />
      </div>
    </div>
  </u-scroll>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;
@use "@/lib/empty-center.scss" as empty;

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
}

.panel {
  padding: 16px;
  border: 1px solid fn.use-var(border-color, light);
  border-radius: 8px;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 12px 20px;
}

.meta-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.meta-label {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.meta-value {
  font-size: 13px;
}

.error-msg {
  margin: 12px 0 0;
  color: fn.use-var(color, danger);
  font-size: 13px;
}

.state {
  padding: 24px;
  color: fn.use-var(text-color, secondary);
}

.page-empty {
  @include empty.center(320px);
}
</style>
