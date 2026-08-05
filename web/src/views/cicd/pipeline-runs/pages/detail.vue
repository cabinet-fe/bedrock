<script setup lang="ts">
defineOptions({ name: "CicdPipelineRunDetail" });

import type { Edge, Node } from "@vue-flow/core";
import { defineTableColumns, message } from "@veltra/desktop";
import { computed, onMounted, onUnmounted, provide, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { cancelPipelineRun, getBuildPipeline, getPipelineRun, listBuildJobs } from "@/api/cicd";
import type { PipelineRun, PipelineStageRun } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationBetween } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType, type TagType } from "@/lib/tag";
import { useTabsStore } from "@/stores/tabs";

import PipelineCanvas from "../../pipelines/components/pipeline-canvas.vue";
import {
  NODE_TYPE_LABEL,
  PIPELINE_TARGET_NAMES,
  orderStagesByGraph,
  parseGraphJson,
  type PipelineNodeData,
} from "../../pipelines/graph";

const route = useRoute();
const router = useRouter();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const detailPath = route.path;
const runId = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id);

const loading = ref(true);
const acting = ref(false);
const run = ref<PipelineRun | null>(null);
const pipelineName = ref("");
const jobNames = ref(new Map<number, string>());
const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);
/** snapshot 中 node_id → 节点名称 */
const nodeLabels = ref(new Map<string, string>());
let pollTimer: ReturnType<typeof setInterval> | null = null;

const canExecute = computed(() => hasPermission("cicd_pipelines:execute"));

const isLive = computed(() => {
  const s = run.value?.status;
  return s === "queued" || s === "running";
});

const canCancel = computed(() => {
  if (!canExecute.value || !run.value) return false;
  return run.value.status === "queued" || run.value.status === "running";
});

const orderedStages = computed(() => {
  const r = run.value;
  if (!r) return [];
  return orderStagesByGraph(r.stages ?? [], r.snapshot_json || "");
});

/** 节点副标题解析（构建任务名；脚本/智能体回退为类型名） */
const targetNames = computed(() => {
  const map: Record<string, string> = {};
  for (const [id, name] of jobNames.value) map[`buildJob:${id}`] = name;
  return map;
});
provide(PIPELINE_TARGET_NAMES, targetNames);

const stageColumns = defineTableColumns([
  { key: "node_type", name: "类型", width: 90, align: "center" },
  { key: "node_id", name: "名称", minWidth: 140 },
  { key: "build_job_id", name: "构建任务", minWidth: 120 },
  { key: "status", name: "状态", width: 110 },
  {
    key: "duration",
    name: "运行时间",
    width: 110,
    align: "center",
    render: ({ rowData }) =>
      formatDurationBetween(
        (rowData as PipelineStageRun).started_at,
        (rowData as PipelineStageRun).finished_at,
      ) || "—",
  },
  { key: "build_run_id", name: "构建运行", width: 100 },
  { key: "script_run_id", name: "脚本运行", width: 100 },
  { key: "agent_run_id", name: "智能体运行", width: 100 },
]);

const STAGE_TYPE_TAG: Record<string, TagType> = {
  start: "success",
  end: undefined,
  buildJob: "primary",
  scriptJob: "warning",
  agent: "info",
};

function stageNodeType(stage: PipelineStageRun): string {
  return stage.node_type || "buildJob";
}

function stageByNode(): Map<string, PipelineStageRun> {
  const m = new Map<string, PipelineStageRun>();
  for (const st of run.value?.stages ?? []) {
    m.set(st.node_id, st);
  }
  return m;
}

function applyGraph(snapshot: string) {
  const g = parseGraphJson(snapshot);
  const stages = stageByNode();
  const labels = new Map<string, string>();
  nodes.value = g.nodes.map((n) => {
    const st = stages.get(n.id);
    const d = (n.data ?? {}) as PipelineNodeData;
    const jobId = Number(d.build_job_id ?? 0);
    const label =
      d.label ||
      (jobId ? jobNames.value.get(jobId) : undefined) ||
      NODE_TYPE_LABEL[n.type ?? "buildJob"] ||
      n.id;
    labels.set(n.id, label);
    return { ...n, data: { ...d, label, status: st?.status } };
  });
  edges.value = g.edges;
  nodeLabels.value = labels;
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

function openScriptRun(stage: PipelineStageRun) {
  if (!stage.script_run_id) return;
  void router.push({
    name: "cicd-script-run-detail",
    params: { id: String(stage.script_run_id) },
  });
}

function openAgentRun(stage: PipelineStageRun) {
  if (!stage.agent_run_id) return;
  void router.push({ name: "ai-run-detail", params: { id: String(stage.agent_run_id) } });
}

async function onCancel() {
  if (!run.value || acting.value) return;
  acting.value = true;
  try {
    run.value = await cancelPipelineRun(run.value.id);
    applyGraph(run.value.snapshot_json || "");
    message.success("已取消");
  } catch (e) {
    message.error(e instanceof Error ? e.message : "取消失败");
  } finally {
    acting.value = false;
  }
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
        <strong
          >{{ pipelineName || `流水线 #${run.build_pipeline_id}` }} #{{ run.run_number }}</strong
        >
      </div>
      <div class="pipeline-run-detail__tags">
        <u-tag :type="tagType(run.status, JOB_STATUS_TAG)">{{ run.status }}</u-tag>
        <u-tag :type="tagType(run.trigger_type, TRIGGER_TYPE_TAG)">{{ run.trigger_type }}</u-tag>
        <u-button v-if="canCancel" plain type="danger" :disabled="acting" @click="onCancel">
          取消
        </u-button>
      </div>
    </header>

    <section class="meta-grid">
      <div class="meta-item">
        <span class="meta-label">运行时间</span>
        <span>{{ formatDurationBetween(run.started_at, run.finished_at) || "—" }}</span>
      </div>
      <div class="meta-item">
        <span class="meta-label">创建</span>
        <span>{{ formatDateTime(run.created_at) || "—" }}</span>
      </div>
      <div class="meta-item">
        <span class="meta-label">开始</span>
        <span>{{ formatDateTime(run.started_at) || "—" }}</span>
      </div>
      <div class="meta-item">
        <span class="meta-label">结束</span>
        <span>{{ formatDateTime(run.finished_at) || "—" }}</span>
      </div>
    </section>
    <p v-if="run.error_message" class="error-msg">{{ run.error_message }}</p>

    <h3>节点状态</h3>
    <div class="pipeline-run-detail__canvas">
      <PipelineCanvas v-model:nodes="nodes" v-model:edges="edges" readonly />
    </div>

    <h3>Stage 列表</h3>
    <u-table :columns="stageColumns" :data="orderedStages" row-key="id" border>
      <template #column:node_type="{ rowData }">
        <u-tag size="small" :type="STAGE_TYPE_TAG[stageNodeType(rowData as PipelineStageRun)]">
          {{ NODE_TYPE_LABEL[stageNodeType(rowData as PipelineStageRun)] }}
        </u-tag>
      </template>
      <template #column:node_id="{ rowData }">
        {{
          nodeLabels.get((rowData as PipelineStageRun).node_id) ||
          (rowData as PipelineStageRun).node_id
        }}
      </template>
      <template #column:build_job_id="{ rowData }">
        <template v-if="(rowData as PipelineStageRun).build_job_id">
          {{
            jobNames.get((rowData as PipelineStageRun).build_job_id) ||
            (rowData as PipelineStageRun).build_job_id
          }}
        </template>
        <span v-else>—</span>
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
      <template #column:script_run_id="{ rowData }">
        <u-action
          v-if="(rowData as PipelineStageRun).script_run_id"
          @run="openScriptRun(rowData as PipelineStageRun)"
        >
          #{{ (rowData as PipelineStageRun).script_run_id }}
        </u-action>
        <span v-else>—</span>
      </template>
      <template #column:agent_run_id="{ rowData }">
        <u-action
          v-if="(rowData as PipelineStageRun).agent_run_id"
          @run="openAgentRun(rowData as PipelineStageRun)"
        >
          #{{ (rowData as PipelineStageRun).agent_run_id }}
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
