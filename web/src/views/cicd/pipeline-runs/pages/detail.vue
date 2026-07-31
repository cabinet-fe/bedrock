<script setup lang="ts">
defineOptions({ name: "CicdPipelineRunDetail" });

import type { Edge, Node } from "@vue-flow/core";
import { defineTableColumns, message } from "@veltra/desktop";
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { getBuildPipeline, getPipelineRun, listBuildJobs } from "@/api/cicd";
import type { PipelineRun, PipelineStageRun } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";
import { useTabsStore } from "@/stores/tabs";

import PipelineCanvas from "../../pipelines/components/pipeline-canvas.vue";

const route = useRoute();
const router = useRouter();
const tabsStore = useTabsStore();

const detailPath = route.path;
const runId = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id);

const loading = ref(true);
const run = ref<PipelineRun | null>(null);
const pipelineName = ref("");
const jobNames = ref(new Map<number, string>());
const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);
let pollTimer: ReturnType<typeof setInterval> | null = null;

const isLive = computed(() => {
  const s = run.value?.status;
  return s === "queued" || s === "running";
});

const stageColumns = defineTableColumns([
  { key: "node_id", name: "节点", minWidth: 100 },
  { key: "build_job_id", name: "构建任务", minWidth: 120 },
  { key: "status", name: "状态", width: 120 },
  { key: "build_run_id", name: "构建运行", width: 120 },
]);

function stageByNode(): Map<string, PipelineStageRun> {
  const m = new Map<string, PipelineStageRun>();
  for (const st of run.value?.stages ?? []) {
    m.set(st.node_id, st);
  }
  return m;
}

function applyGraph(snapshot: string) {
  try {
    const g = JSON.parse(snapshot || '{"nodes":[],"edges":[]}') as {
      nodes?: Node[];
      edges?: Edge[];
    };
    const stages = stageByNode();
    nodes.value = (g.nodes ?? []).map((n) => {
      const st = stages.get(n.id);
      const jobId = Number((n.data as { build_job_id?: number })?.build_job_id ?? 0);
      return {
        ...n,
        type: "buildJob",
        data: {
          ...n.data,
          label:
            (n.data as { label?: string })?.label || jobNames.value.get(jobId) || `Job #${jobId}`,
          status: st?.status,
        },
      };
    });
    edges.value = g.edges ?? [];
  } catch {
    nodes.value = [];
    edges.value = [];
  }
}

async function load() {
  try {
    const data = await getPipelineRun(runId);
    run.value = data;
    applyGraph(data.snapshot_json || "");
    const titleBase = pipelineName.value || `流水线 #${data.build_pipeline_id}`;
    tabsStore.updateTitle(detailPath, `${titleBase} #${data.run_number}`);
  } catch (e) {
    message.error(e instanceof Error ? e.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

function openBuildRun(stage: PipelineStageRun) {
  if (!stage.build_run_id) return;
  void router.push({ name: "cicd-build-run-detail", params: { id: String(stage.build_run_id) } });
}

onMounted(async () => {
  try {
    const jobs = await listBuildJobs({ page: 1, page_size: 200 });
    const map = new Map<number, string>();
    for (const j of jobs.items ?? []) map.set(j.id, j.name);
    jobNames.value = map;
  } catch {
    /* ignore */
  }
  await load();
  if (run.value) {
    try {
      const p = await getBuildPipeline(run.value.build_pipeline_id);
      pipelineName.value = p.name;
      tabsStore.updateTitle(detailPath, `${p.name} #${run.value.run_number}`);
    } catch {
      /* ignore */
    }
  }
  pollTimer = setInterval(() => {
    if (isLive.value) void load();
  }, 3000);
});

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
});
</script>

<template>
  <div v-if="loading" class="muted">加载中…</div>
  <div v-else-if="run" class="pipeline-run-detail">
    <header class="pipeline-run-detail__bar">
      <div>
        <u-button text @click="router.push({ name: 'cicd-pipeline-runs' })">返回列表</u-button>
        <strong
          >{{ pipelineName || `流水线 #${run.build_pipeline_id}` }} #{{ run.run_number }}</strong
        >
      </div>
      <div class="pipeline-run-detail__tags">
        <u-tag :type="tagType(run.status, JOB_STATUS_TAG)">{{ run.status }}</u-tag>
        <u-tag :type="tagType(run.trigger_type, TRIGGER_TYPE_TAG)">{{ run.trigger_type }}</u-tag>
      </div>
    </header>

    <section class="meta-grid">
      <div class="meta-item">
        <span class="meta-label">创建</span>
        <span>{{ formatDateTime(run.created_at) }}</span>
      </div>
      <div class="meta-item">
        <span class="meta-label">开始</span>
        <span>{{ formatDateTime(run.started_at) }}</span>
      </div>
      <div class="meta-item">
        <span class="meta-label">结束</span>
        <span>{{ formatDateTime(run.finished_at) }}</span>
      </div>
    </section>
    <p v-if="run.error_message" class="error-msg">{{ run.error_message }}</p>

    <h3>节点状态</h3>
    <div class="pipeline-run-detail__canvas">
      <PipelineCanvas v-model:nodes="nodes" v-model:edges="edges" readonly />
    </div>

    <h3>Stage 列表</h3>
    <u-table :columns="stageColumns" :data="run.stages ?? []" row-key="id" border>
      <template #column:build_job_id="{ rowData }">
        {{
          jobNames.get((rowData as PipelineStageRun).build_job_id) ||
          (rowData as PipelineStageRun).build_job_id
        }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as PipelineStageRun).status, JOB_STATUS_TAG)">
          {{ (rowData as PipelineStageRun).status }}
        </u-tag>
      </template>
      <template #column:build_run_id="{ rowData }">
        <u-action
          v-if="(rowData as PipelineStageRun).build_run_id"
          @run="openBuildRun(rowData as PipelineStageRun)"
        >
          #{{ (rowData as PipelineStageRun).build_run_id }}
        </u-action>
        <span v-else>—</span>
      </template>
    </u-table>
  </div>
</template>

<style scoped lang="scss">
.muted {
  color: var(--u-text-color-secondary, #888);
  padding: 24px;
}

.pipeline-run-detail__bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;

  strong {
    margin-left: 8px;
  }
}

.pipeline-run-detail__tags {
  display: flex;
  gap: 8px;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.meta-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}

.meta-label {
  color: var(--u-text-color-secondary, #888);
  font-size: 12px;
}

.error-msg {
  color: var(--u-color-danger, #ef4444);
  margin: 0 0 16px;
}

.pipeline-run-detail__canvas {
  height: 360px;
  border: 1px solid var(--u-border-color, #e5e5e5);
  border-radius: 8px;
  margin-bottom: 20px;
  overflow: hidden;
}

h3 {
  margin: 16px 0 8px;
  font-size: 14px;
}
</style>
