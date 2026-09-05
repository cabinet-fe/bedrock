<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { UButton, UIcon, UTag } from "@veltra/desktop";
import { Link, Refresh, VideoPlay } from "@veltra/icons/normal";
import type { ChatToolCall } from "@veltra/ai";

import { getBuildRun, getPipelineRun } from "@/api/cicd";
import type { BuildRun, PipelineRun } from "@/api/types";
import BuildLogViewer, { resolveBuildLogStatus } from "@/components/build-log-viewer";
import { formatDateTime, formatDurationBetween } from "@/lib/datetime";
import { JOB_STATUS_TAG, jobStatusLabel, tagType } from "@/lib/tag";

const props = withDefaults(
  defineProps<{
    toolCall?: ChatToolCall;
    runId?: number;
    runType?: "build" | "pipeline";
  }>(),
  {},
);

const buildRun = ref<BuildRun | null>(null);
const pipelineRun = ref<PipelineRun | null>(null);
const loading = ref(false);
const error = ref("");
let pollTimer: ReturnType<typeof setInterval> | null = null;

const parsedArguments = computed<Record<string, any>>(() => {
  if (!props.toolCall?.arguments) return {};
  try {
    return JSON.parse(props.toolCall.arguments);
  } catch {
    return {};
  }
});

const parsedResult = computed<Record<string, any>>(() => {
  if (!props.toolCall?.result) return {};
  try {
    return JSON.parse(props.toolCall.result);
  } catch {
    return {};
  }
});

const runType = computed<"build" | "pipeline">(() => {
  if (props.runType) return props.runType;
  if (parsedResult.value.run_type === "pipeline") return "pipeline";
  if (parsedArguments.value.type === "pipeline") return "pipeline";
  if (props.toolCall?.name === "trigger_pipeline") return "pipeline";
  if (parsedArguments.value.pipeline_id) return "pipeline";
  return "build";
});

const runId = computed<number | null>(() => {
  if (props.runId && Number(props.runId) > 0) {
    return Number(props.runId);
  }
  if (parsedResult.value.run_id && Number(parsedResult.value.run_id) > 0) {
    return Number(parsedResult.value.run_id);
  }
  if (parsedArguments.value.run_id && Number(parsedArguments.value.run_id) > 0) {
    return Number(parsedArguments.value.run_id);
  }
  if (typeof props.toolCall?.result === "string") {
    const match = props.toolCall.result.match(/(?:run_id|运行 ID|运行|#)\s*[:：#]?\s*(\d+)/i);
    if (match && match[1]) {
      return Number(match[1]);
    }
  }
  return null;
});

const isLive = computed(() => {
  if (runType.value === "pipeline") {
    const s = pipelineRun.value?.status;
    return s === "queued" || s === "running";
  }
  const s = buildRun.value?.status;
  return s === "queued" || s === "running" || buildRun.value?.distribution_summary === "running";
});

const logViewerStatus = computed(() => {
  return resolveBuildLogStatus(buildRun.value?.status, buildRun.value?.distribution_summary);
});

async function loadData() {
  const id = runId.value;
  if (!id) return;
  loading.value = true;
  error.value = "";
  try {
    if (runType.value === "pipeline") {
      pipelineRun.value = await getPipelineRun(id);
    } else {
      buildRun.value = await getBuildRun(id);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载运行详情失败";
  } finally {
    loading.value = false;
  }
}

function startPolling() {
  stopPolling();
  if (!isLive.value) return;
  pollTimer = setInterval(async () => {
    if (!runId.value) return;
    try {
      if (runType.value === "pipeline") {
        pipelineRun.value = await getPipelineRun(runId.value);
      } else {
        buildRun.value = await getBuildRun(runId.value);
      }
      if (!isLive.value) {
        stopPolling();
      }
    } catch {
      // Keep quiet during background polling
    }
  }, 2500);
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

watch(
  [runId, runType],
  async ([newId]) => {
    if (newId) {
      await loadData();
      startPolling();
    }
  },
  { immediate: true },
);

watch(isLive, (live) => {
  if (live) {
    startPolling();
  } else {
    stopPolling();
  }
});

onMounted(() => {
  if (isLive.value) {
    startPolling();
  }
});

onBeforeUnmount(() => {
  stopPolling();
});

function openDetail() {
  if (!runId.value) return;
  const path =
    runType.value === "pipeline"
      ? `/cicd/pipeline-runs/${runId.value}`
      : `/cicd/build-runs/${runId.value}`;
  window.open(path, "_blank");
}
</script>

<template>
  <div class="build-detail-panel">
    <!-- 待用户确认态 -->
    <div
      v-if="toolCall?.status === 'awaiting-confirm'"
      class="build-detail-panel__notice is-warning"
    >
      <div class="build-detail-panel__notice-title">⚠️ 等待确认</div>
      <p class="build-detail-panel__notice-desc">
        该操作涉及敏感工程触发，请在对话卡片中点击「允许」以继续执行。
      </p>
    </div>

    <!-- 被用户拒绝态 -->
    <div v-else-if="toolCall?.status === 'rejected'" class="build-detail-panel__notice is-danger">
      <div class="build-detail-panel__notice-title">❌ 操作已拒绝</div>
      <p class="build-detail-panel__notice-desc">您已拒绝执行本次运行触发请求。</p>
    </div>

    <!-- 运行中且未出 ID -->
    <div
      v-else-if="toolCall?.status === 'running' && !runId"
      class="build-detail-panel__notice is-info"
    >
      <div class="build-detail-panel__notice-title">正在触发...</div>
      <p class="build-detail-panel__notice-desc">正在向平台提交运行任务，请稍候。</p>
    </div>

    <!-- 调用错误 -->
    <div
      v-else-if="toolCall?.status === 'error' && !runId"
      class="build-detail-panel__notice is-danger"
    >
      <div class="build-detail-panel__notice-title">❌ 触发失败</div>
      <p class="build-detail-panel__notice-desc">{{ toolCall?.error || "执行出错" }}</p>
    </div>

    <!-- 成功获取到运行 ID -->
    <template v-else-if="runId">
      <!-- 构建运行详情 -->
      <template v-if="runType === 'build'">
        <section class="build-detail-panel__card">
          <header class="build-detail-panel__card-header">
            <div class="build-detail-panel__card-title">
              <u-icon :size="16" class="build-detail-panel__title-icon">
                <VideoPlay />
              </u-icon>
              <span>构建运行 #{{ runId }}</span>
              <u-tag
                v-if="buildRun?.status"
                :type="tagType(buildRun.status, JOB_STATUS_TAG)"
                size="small"
              >
                {{ jobStatusLabel(buildRun.status) }}
              </u-tag>
            </div>

            <div class="build-detail-panel__card-actions">
              <u-button size="small" text @click="loadData">
                <u-icon :size="13">
                  <Refresh />
                </u-icon>
                刷新
              </u-button>
              <u-button size="small" type="primary" plain @click="openDetail">
                <u-icon :size="13">
                  <Link />
                </u-icon>
                打开详情页
              </u-button>
            </div>
          </header>

          <div class="build-detail-panel__meta-grid">
            <div class="build-detail-panel__meta-item">
              <span class="label">任务 ID</span>
              <span class="value">#{{ buildRun?.build_job_id ?? "—" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">构建分支</span>
              <span class="value code">{{ buildRun?.branch || "—" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">构建阶段</span>
              <span class="value">{{ buildRun?.stage || "—" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">触发方式</span>
              <span class="value">{{ buildRun?.trigger_type || "manual" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">提交哈希</span>
              <span class="value code">
                {{ buildRun?.commit_hash ? buildRun.commit_hash.slice(0, 8) : "—" }}
              </span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">运行耗时</span>
              <span class="value">
                {{ formatDurationBetween(buildRun?.started_at, buildRun?.finished_at) }}
              </span>
            </div>
            <div class="build-detail-panel__meta-item build-detail-panel__meta-item--full">
              <span class="label">开始时间</span>
              <span class="value">{{ formatDateTime(buildRun?.started_at) }}</span>
            </div>
          </div>
        </section>

        <!-- 构建日志展示 -->
        <section class="build-detail-panel__logs-section">
          <div class="build-detail-panel__section-title">实时构建日志</div>
          <div class="build-detail-panel__logs-wrapper">
            <BuildLogViewer
              :run-id="runId"
              :live="isLive"
              :status="logViewerStatus"
              height="380px"
            />
          </div>
        </section>
      </template>

      <!-- 流水线运行详情 -->
      <template v-else>
        <section class="build-detail-panel__card">
          <header class="build-detail-panel__card-header">
            <div class="build-detail-panel__card-title">
              <u-icon :size="16" class="build-detail-panel__title-icon">
                <VideoPlay />
              </u-icon>
              <span>流水线运行 #{{ runId }}</span>
              <u-tag
                v-if="pipelineRun?.status"
                :type="tagType(pipelineRun.status, JOB_STATUS_TAG)"
                size="small"
              >
                {{ jobStatusLabel(pipelineRun.status) }}
              </u-tag>
            </div>

            <div class="build-detail-panel__card-actions">
              <u-button size="small" text @click="loadData">
                <u-icon :size="13">
                  <Refresh />
                </u-icon>
                刷新
              </u-button>
              <u-button size="small" type="primary" plain @click="openDetail">
                <u-icon :size="13">
                  <Link />
                </u-icon>
                打开详情页
              </u-button>
            </div>
          </header>

          <div class="build-detail-panel__meta-grid">
            <div class="build-detail-panel__meta-item">
              <span class="label">流水线 ID</span>
              <span class="value">#{{ pipelineRun?.build_pipeline_id ?? "—" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">触发方式</span>
              <span class="value">{{ pipelineRun?.trigger_type || "manual" }}</span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">运行耗时</span>
              <span class="value">
                {{ formatDurationBetween(pipelineRun?.started_at, pipelineRun?.finished_at) }}
              </span>
            </div>
            <div class="build-detail-panel__meta-item">
              <span class="label">开始时间</span>
              <span class="value">{{ formatDateTime(pipelineRun?.started_at) }}</span>
            </div>
          </div>
        </section>

        <!-- 阶段列表展示 -->
        <section class="build-detail-panel__stages-section">
          <div class="build-detail-panel__section-title">阶段节点列表</div>
          <div
            v-if="!pipelineRun?.stages || pipelineRun.stages.length === 0"
            class="build-detail-panel__empty-stages"
          >
            暂无阶段运行记录
          </div>
          <div v-else class="build-detail-panel__stages-list">
            <div
              v-for="stage in pipelineRun.stages"
              :key="stage.id"
              class="build-detail-panel__stage-item"
            >
              <div class="build-detail-panel__stage-header">
                <span class="build-detail-panel__stage-name">{{ stage.node_id }}</span>
                <u-tag :type="tagType(stage.status, JOB_STATUS_TAG)" size="small">
                  {{ jobStatusLabel(stage.status) }}
                </u-tag>
              </div>
              <div class="build-detail-panel__stage-meta">
                <span>类型: {{ stage.node_type }}</span>
                <span v-if="stage.build_job_id">构建任务: #{{ stage.build_job_id }}</span>
                <span>
                  耗时: {{ formatDurationBetween(stage.started_at, stage.finished_at) }}
                </span>
              </div>
              <div v-if="stage.error_message" class="build-detail-panel__stage-error">
                {{ stage.error_message }}
              </div>
            </div>
          </div>
        </section>
      </template>
    </template>

    <div v-else class="build-detail-panel__empty">
      <p>未解析到构建运行标识。</p>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.build-detail-panel {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  font-size: 13px;
  color: fn.use-var(text-color, main);
}

.build-detail-panel__notice {
  padding: 12px 14px;
  border-radius: fn.use-var(radius, medium);
  border: 1px solid transparent;

  &.is-warning {
    background: color-mix(in srgb, fn.use-var(color, warning) 12%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, warning) 35%, transparent);
    color: fn.use-var(color, warning);
  }

  &.is-danger {
    background: color-mix(in srgb, fn.use-var(color, danger) 12%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, danger) 35%, transparent);
    color: fn.use-var(color, danger);
  }

  &.is-info {
    background: color-mix(in srgb, fn.use-var(color, primary) 12%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 35%, transparent);
    color: fn.use-var(color, primary);
  }
}

.build-detail-panel__notice-title {
  font-weight: 600;
  margin-bottom: 4px;
}

.build-detail-panel__notice-desc {
  font-size: 12px;
  margin: 0;
  opacity: 0.9;
}

.build-detail-panel__card {
  background: fn.use-var(bg-color, top);
  border: 1px solid fn.use-var(border, muted-color);
  border-radius: fn.use-var(radius, medium);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.build-detail-panel__card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.build-detail-panel__card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
  color: fn.use-var(text-color, title);
}

.build-detail-panel__title-icon {
  color: fn.use-var(color, primary);
}

.build-detail-panel__card-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.build-detail-panel__meta-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px 12px;
}

.build-detail-panel__meta-item {
  display: flex;
  flex-direction: column;
  gap: 2px;

  &--full {
    grid-column: span 2;
  }

  .label {
    font-size: 11px;
    color: fn.use-var(text-color, description);
  }

  .value {
    font-size: 12px;
    color: fn.use-var(text-color, title);

    &.code {
      font-family: monospace;
    }
  }
}

.build-detail-panel__logs-section,
.build-detail-panel__stages-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.build-detail-panel__section-title {
  font-weight: 600;
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.build-detail-panel__logs-wrapper {
  border-radius: fn.use-var(radius, medium);
  overflow: hidden;
  border: 1px solid fn.use-var(border, muted-color);
}

.build-detail-panel__empty-stages,
.build-detail-panel__empty {
  padding: 24px;
  text-align: center;
  color: fn.use-var(text-color, placeholder);
  font-size: 13px;
}

.build-detail-panel__stages-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.build-detail-panel__stage-item {
  padding: 10px 12px;
  background: fn.use-var(bg-color, top);
  border: 1px solid fn.use-var(border, muted-color);
  border-radius: fn.use-var(radius, small);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.build-detail-panel__stage-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.build-detail-panel__stage-name {
  font-weight: 500;
  font-size: 13px;
}

.build-detail-panel__stage-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 11px;
  color: fn.use-var(text-color, description);
}

.build-detail-panel__stage-error {
  font-size: 11px;
  color: fn.use-var(color, danger);
  background: color-mix(in srgb, fn.use-var(color, danger) 10%, transparent);
  padding: 4px 8px;
  border-radius: fn.use-var(radius, small);
}
</style>
