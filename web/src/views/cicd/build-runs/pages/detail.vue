<script setup lang="ts">
defineOptions({ name: "CicdBuildRunDetail" });

import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { saveBlob } from "@cat-kit/fe";
import { message } from "@veltra/desktop";
import type { ColorType } from "@veltra/utils";

import {
  buildRunArtifactURL,
  cancelBuildRun,
  getBuildJob,
  getBuildRun,
  redeployBuildRun,
  retryBuildRun,
} from "@/api/cicd";
import { getAccessToken } from "@/api/http";
import type { BuildRun } from "@/api/types";
import BuildLogViewer, { resolveBuildLogStatus } from "@/components/build-log-viewer";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import {
  BUILD_DISTRIBUTION_TAG,
  BUILD_STAGE_TAG,
  JOB_STATUS_TAG,
  TRIGGER_TYPE_TAG,
  tagType,
} from "@/lib/tag";
import { useTabsStore } from "@/stores/tabs";

/** 与 BuildRun.stage 对齐（不含终态 idle） */
const PIPELINE_STEPS: { key: string; label: string }[] = [
  { key: "pending", label: "排队" },
  { key: "cloning", label: "克隆" },
  { key: "building", label: "构建" },
  { key: "archiving", label: "归档" },
  { key: "distributing", label: "分发" },
];

const route = useRoute();
const router = useRouter();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const run = ref<BuildRun | null>(null);
const loading = ref(true);
const acting = ref(false);
const logViewerRef = ref<InstanceType<typeof BuildLogViewer> | null>(null);

function parseRouteId(raw: unknown): number | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

const canExecute = computed(() => hasPermission("cicd_build_jobs:execute"));
// Layout keys detail by path and keep-alive caches the instance. Freeze the id at
// setup so deactivated instances do not re-read the global route (which loses :id).
const detailPath = route.path;
const runId = parseRouteId(route.params.id);

const isLive = computed(() => {
  const s = run.value?.status;
  return s === "queued" || s === "running" || run.value?.distribution_summary === "running";
});

const logViewerStatus = computed(() =>
  resolveBuildLogStatus(run.value?.status, run.value?.distribution_summary),
);

const canCancel = computed(() => {
  if (!canExecute.value || !run.value) return false;
  return (
    run.value.status === "queued" ||
    run.value.status === "running" ||
    (run.value.status === "success" && run.value.distribution_summary === "running")
  );
});

const canRetry = computed(
  () =>
    canExecute.value &&
    !!run.value &&
    ["failed", "cancelled", "interrupted", "success"].includes(run.value.status),
);

const canRedeploy = computed(
  () => canExecute.value && run.value?.status === "success" && !!run.value.artifact_path,
);

const shortCommit = computed(() => {
  const hash = run.value?.commit_hash?.trim();
  if (!hash) return "—";
  return hash.length > 12 ? hash.slice(0, 12) : hash;
});

function stageStepIndex(stage: string): number {
  const idx = PIPELINE_STEPS.findIndex((s) => s.key === stage);
  return idx >= 0 ? idx : 0;
}

/** u-steps：undefined = 全部完成；否则为当前活动步索引 */
const stepCurrent = computed<number | undefined>(() => {
  const r = run.value;
  if (!r) return undefined;
  const { stage, status, distribution_summary: dist } = r;

  if (stage === "idle") {
    if (status === "success") {
      if (dist === "all_failed" || dist === "partial" || dist === "cancelled") {
        return PIPELINE_STEPS.length - 1;
      }
      return undefined;
    }
    // 排队中取消会直接落到 idle
    if (status === "cancelled" || status === "interrupted") return 0;
    return undefined;
  }

  return stageStepIndex(stage);
});

const currentStepType = computed<ColorType | undefined>(() => {
  const r = run.value;
  if (!r || stepCurrent.value === undefined) return undefined;
  const { status, distribution_summary: dist } = r;

  if (status === "failed") return "danger";
  if (status === "cancelled" || status === "interrupted") return "warning";
  if (dist === "all_failed") return "danger";
  if (dist === "partial" || dist === "cancelled") return "warning";
  if (status === "queued") return "info";
  return "primary";
});

async function syncTabTitle(r: BuildRun) {
  let title = `构建 #${r.build_number}`;
  try {
    const job = await getBuildJob(r.build_job_id);
    if (job.name) title = `${job.name} #${r.build_number}`;
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
    run.value = await getBuildRun(runId);
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
    run.value = await getBuildRun(runId);
  } catch {
    /* ignore */
  }
}

async function onCancel() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    run.value = await cancelBuildRun(run.value.id);
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
    const next = await retryBuildRun(run.value.id);
    message.success(`已创建重试 #${next.build_number}`);
    await router.push({ name: "cicd-build-run-detail", params: { id: String(next.id) } });
  } catch (err) {
    message.error(err instanceof Error ? err.message : "重试失败");
  } finally {
    acting.value = false;
  }
}

async function onRedeploy() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    run.value = await redeployBuildRun(run.value.id);
    message.success("已开始重新分发");
    logViewerRef.value?.appendLine("=== Redeploy requested ===");
    logViewerRef.value?.reconnect();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "重新分发失败");
  } finally {
    acting.value = false;
  }
}

async function onDownloadArtifact() {
  const token = getAccessToken();
  if (!token || !run.value) return;
  try {
    const res = await fetch(buildRunArtifactURL(run.value.id), {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error((await res.text()) || `HTTP ${res.status}`);
    }
    const blob = await res.blob();
    const cd = res.headers.get("Content-Disposition") || "";
    const m = /filename="?([^"]+)"?/.exec(cd);
    const name = m?.[1] || `build-${run.value.build_number}.tar.gz`;
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
            <h2>构建 #{{ run.build_number }}</h2>
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
          <u-button v-if="canRetry" type="primary" :disabled="acting" @click="onRetry"
            >重试</u-button
          >
          <u-button v-if="canRedeploy" type="primary" :disabled="acting" @click="onRedeploy">
            重新分发
          </u-button>
        </div>
      </header>

      <div v-if="loading" class="state">加载中…</div>
      <template v-else-if="run">
        <section class="panel steps-panel">
          <u-steps
            align-center
            :items="PIPELINE_STEPS"
            :current="stepCurrent"
            :current-step-type="currentStepType"
            finished-step-type="success"
          />
        </section>

        <section class="panel meta-panel">
          <div class="meta-grid">
            <div class="meta-item">
              <span class="meta-label">阶段</span>
              <u-tag size="small" :type="tagType(run.stage, BUILD_STAGE_TAG)">
                {{ run.stage || "—" }}
              </u-tag>
            </div>
            <div class="meta-item">
              <span class="meta-label">分发汇总</span>
              <u-tag size="small" :type="tagType(run.distribution_summary, BUILD_DISTRIBUTION_TAG)">
                {{ run.distribution_summary || "—" }}
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
              <span class="meta-value">{{ run.build_job_id }}</span>
            </div>
            <div class="meta-item">
              <span class="meta-label">分支</span>
              <span class="meta-value mono">{{ run.branch || "—" }}</span>
            </div>
            <div class="meta-item meta-item--wide">
              <span class="meta-label">Commit</span>
              <span class="meta-value mono" :title="run.commit_hash || undefined">
                {{ shortCommit }}
              </span>
            </div>
            <div class="meta-item">
              <span class="meta-label">创建时间</span>
              <span class="meta-value">{{ formatDateTime(run.created_at) || "—" }}</span>
            </div>
          </div>
          <p v-if="run.commit_message" class="commit-msg">{{ run.commit_message }}</p>
          <p v-if="run.error_message" class="error-msg">{{ run.error_message }}</p>
        </section>

        <section class="section">
          <h3 class="section__title">构建日志</h3>
          <BuildLogViewer
            ref="logViewerRef"
            :run-id="run.id"
            :live="isLive"
            :status="logViewerStatus"
            @refresh="onLogRefresh"
          />
        </section>

        <section class="section">
          <h3 class="section__title">部署尝试</h3>
          <div class="panel" :class="{ 'panel--empty': !run.deploy_attempts?.length }">
            <u-empty v-if="!run.deploy_attempts?.length" text="暂无部署尝试" />
            <ul v-else class="attempts">
              <li v-for="a in run.deploy_attempts" :key="a.id" class="attempt">
                <div class="attempt__main">
                  <span class="mono">batch {{ a.batch_no }}</span>
                  <span class="attempt__sep">·</span>
                  <span>target {{ a.deploy_target_id ?? "—" }}</span>
                  <u-tag size="small" :type="tagType(a.status, JOB_STATUS_TAG)">{{
                    a.status
                  }}</u-tag>
                </div>
                <p v-if="a.error_message" class="attempt__error">{{ a.error_message }}</p>
              </li>
            </ul>
          </div>
        </section>
      </template>
      <u-empty v-else text="执行记录不存在或无权访问" />
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

.steps-panel {
  overflow-x: auto;
}

.panel--empty {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 120px;
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

@media (min-width: 640px) {
  .meta-item--wide {
    grid-column: span 2;
  }
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

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.commit-msg {
  margin: 0;
  padding-top: 12px;
  border-top: fn.use-var(border, muted);
  font-size: 13px;
  line-height: 1.5;
  color: fn.use-var(text-color, second);
  overflow-wrap: anywhere;
}

.error-msg {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: fn.use-var(color, danger);
  overflow-wrap: anywhere;
}

.attempts {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.attempt {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.attempt__main {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.attempt__sep {
  color: fn.use-var(text-color, assist);
}

.attempt__error {
  margin: 0;
  font-size: 12px;
  color: fn.use-var(color, danger);
  overflow-wrap: anywhere;
}

.state {
  opacity: 0.7;
}
</style>
