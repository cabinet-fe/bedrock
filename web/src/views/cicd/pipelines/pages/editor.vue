<script setup lang="ts">
defineOptions({ name: "CicdPipelineEditor" });

import type { Edge, Node } from "@vue-flow/core";
import { message } from "@veltra/desktop";
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { getBuildPipeline, listBuildJobs, updateBuildPipeline } from "@/api/cicd";
import type { BuildJob, BuildPipeline } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { useTabsStore } from "@/stores/tabs";

import JobPalette from "../components/job-palette.vue";
import PipelineCanvas from "../components/pipeline-canvas.vue";

const route = useRoute();
const router = useRouter();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const detailPath = route.path;
const pipelineId = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id);

const loading = ref(true);
const saving = ref(false);
const pipeline = ref<BuildPipeline | null>(null);
const jobs = ref<BuildJob[]>([]);
const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);

function parseGraph(raw: string) {
  try {
    const g = JSON.parse(raw || '{"nodes":[],"edges":[]}') as {
      nodes?: Node[];
      edges?: Edge[];
    };
    nodes.value = (g.nodes ?? []).map((n) => ({
      ...n,
      type: n.type || "buildJob",
    }));
    edges.value = g.edges ?? [];
  } catch {
    nodes.value = [];
    edges.value = [];
  }
}

function jobLabel(jobId: number): string {
  return jobs.value.find((j) => j.id === jobId)?.name ?? `Job #${jobId}`;
}

function addJob(job: BuildJob, position = { x: 80 + nodes.value.length * 40, y: 80 }) {
  const full = jobs.value.find((j) => j.id === job.id) ?? job;
  const id = `n-${full.id}-${Date.now()}`;
  nodes.value = [
    ...nodes.value,
    {
      id,
      type: "buildJob",
      position,
      data: { build_job_id: full.id, label: full.name || jobLabel(full.id) },
    },
  ];
}

async function load() {
  loading.value = true;
  try {
    const [p, jobPage] = await Promise.all([
      getBuildPipeline(pipelineId),
      listBuildJobs({ page: 1, page_size: 200 }),
    ]);
    pipeline.value = p;
    jobs.value = jobPage.items ?? [];
    parseGraph(p.graph_json);
    tabsStore.updateTitle(detailPath, `编排 · ${p.name}`);
  } catch (e) {
    message.error(e instanceof Error ? e.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!hasPermission("cicd_pipelines:update")) return;
  saving.value = true;
  try {
    const graph_json = JSON.stringify({
      nodes: nodes.value.map(({ id, type, position, data }) => ({ id, type, position, data })),
      edges: edges.value.map(({ id, source, target }) => ({ id, source, target })),
    });
    pipeline.value = await updateBuildPipeline(pipelineId, { graph_json });
    message.success("已保存");
  } catch (e) {
    message.error(e instanceof Error ? e.message : "保存失败");
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="pipeline-editor">
    <header class="pipeline-editor__bar">
      <div>
        <u-button text @click="router.push({ name: 'cicd-pipelines' })">返回列表</u-button>
        <strong v-if="pipeline">{{ pipeline.name }}</strong>
      </div>
      <u-button
        v-if="hasPermission('cicd_pipelines:update')"
        type="primary"
        :loading="saving"
        :disabled="loading"
        @click="save"
      >
        保存编排
      </u-button>
    </header>
    <div v-if="loading" class="pipeline-editor__loading">加载中…</div>
    <div v-else class="pipeline-editor__body">
      <JobPalette v-if="hasPermission('cicd_pipelines:update')" :jobs="jobs" @pick="addJob" />
      <PipelineCanvas
        v-model:nodes="nodes"
        v-model:edges="edges"
        :readonly="!hasPermission('cicd_pipelines:update')"
        @add-job="addJob"
      />
    </div>
  </div>
</template>

<style scoped lang="scss">
.pipeline-editor {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 120px);
  min-height: 480px;
}

.pipeline-editor__bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  gap: 12px;

  strong {
    margin-left: 8px;
  }
}

.pipeline-editor__body {
  display: flex;
  flex: 1;
  min-height: 0;
  border: 1px solid var(--u-border-color, #e5e5e5);
  border-radius: 8px;
  overflow: hidden;
}

.pipeline-editor__loading {
  padding: 40px;
  color: var(--u-text-color-secondary, #888);
}
</style>
