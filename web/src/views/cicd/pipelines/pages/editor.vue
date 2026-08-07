<script setup lang="ts">
defineOptions({ name: "CicdPipelineEditor" });

import type { Edge, Node } from "@vue-flow/core";
import { message } from "@veltra/desktop";
import { computed, onMounted, provide, ref } from "vue";
import { useRoute } from "vue-router";

import { listAgents } from "@/api/ai";
import {
  enqueuePipelineRun,
  getBuildPipeline,
  listBuildJobs,
  listScriptJobs,
  updateBuildPipeline,
} from "@/api/cicd";
import type { BuildPipeline } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { useTabsStore } from "@/stores/tabs";

import NodeConfigDrawer from "../components/node-config-drawer.vue";
import PipelineCanvas from "../components/pipeline-canvas.vue";
import {
  NODE_TYPE_LABEL,
  PIPELINE_TARGET_NAMES,
  parseGraphJson,
  seedGraph,
  serializeGraph,
  type PipelineNodeData,
} from "../graph";

const route = useRoute();
const tabsStore = useTabsStore();
const { hasPermission } = usePermission();

const detailPath = route.path;
const pipelineId = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id);

const loading = ref(true);
const saving = ref(false);
const running = ref(false);
const pipeline = ref<BuildPipeline | null>(null);
const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);

/** 节点副标题所需的任务/智能体名映射（key: `${type}:${id}`） */
const targetNames = ref<Record<string, string>>({});
provide(PIPELINE_TARGET_NAMES, targetNames);

const canUpdate = computed(() => hasPermission("cicd_pipelines:update"));
const canExecute = computed(() => hasPermission("cicd_pipelines:execute"));

async function loadTargetNames() {
  const map: Record<string, string> = {};
  await Promise.all([
    listBuildJobs({ page: 1, page_size: 200 })
      .then((r) => {
        for (const j of r.items ?? []) map[`buildJob:${j.id}`] = j.name;
      })
      .catch(() => {}),
    listScriptJobs({ page: 1, page_size: 200 })
      .then((r) => {
        for (const j of r.items ?? []) map[`scriptJob:${j.id}`] = j.name;
      })
      .catch(() => {}),
    listAgents({ page: 1, page_size: 200 })
      .then((r) => {
        for (const a of r.items ?? []) map[`agent:${a.id}`] = a.name;
      })
      .catch(() => {}),
  ]);
  targetNames.value = map;
}

async function load() {
  loading.value = true;
  try {
    const p = await getBuildPipeline(pipelineId);
    pipeline.value = p;
    const graph = parseGraphJson(p.graph_json);
    // 空图自动种子化 start + end
    if (graph.nodes.length) {
      nodes.value = graph.nodes;
      edges.value = graph.edges;
    } else {
      const seeded = seedGraph();
      nodes.value = seeded.nodes;
      edges.value = seeded.edges;
    }
    tabsStore.updateTitle(detailPath, p.name);
    void loadTargetNames();
  } catch (e) {
    message.error(e instanceof Error ? e.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

function validateGraph(): boolean {
  for (const n of nodes.value) {
    const d = (n.data ?? {}) as PipelineNodeData;
    const missing =
      (n.type === "buildJob" && !d.build_job_id) ||
      (n.type === "scriptJob" && !d.script_job_id) ||
      (n.type === "agent" && !d.agent_id);
    if (missing) {
      message.warning(
        `节点「${d.label || NODE_TYPE_LABEL[n.type ?? ""] || n.id}」尚未配置，请先完成配置`,
      );
      return false;
    }
  }
  return true;
}

async function save() {
  if (!canUpdate.value || !validateGraph()) return;
  saving.value = true;
  try {
    pipeline.value = await persist();
    message.success("已保存");
  } catch (e) {
    message.error(e instanceof Error ? e.message : "保存失败");
  } finally {
    saving.value = false;
  }
}

function persist() {
  return updateBuildPipeline(pipelineId, {
    graph_json: serializeGraph(nodes.value, edges.value),
  });
}

async function run() {
  running.value = true;
  try {
    // 运行以画布当前状态为准：有编辑权限时先落库（避免跑到旧图/空图）
    if (canUpdate.value) {
      if (!validateGraph()) {
        running.value = false;
        return;
      }
      pipeline.value = await persist();
    }
    const r = await enqueuePipelineRun(pipelineId, { trigger_type: "manual" });
    message.success(`已触发 #${r.run_number}`);
  } catch (e) {
    message.error(e instanceof Error ? e.message : "触发失败");
  } finally {
    running.value = false;
  }
}

// —— 节点配置抽屉 ——
const drawerOpen = ref(false);
const configNode = ref<Node | null>(null);

function openConfig(node: Node) {
  configNode.value = node;
  drawerOpen.value = true;
}

function onConfigSave(data: PipelineNodeData) {
  const id = configNode.value?.id;
  if (!id) return;
  nodes.value = nodes.value.map((n) => (n.id === id ? { ...n, data } : n));
}

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="pipeline-editor">
    <header class="pipeline-editor__bar">
      <div>
        <strong v-if="pipeline">{{ pipeline.name }}</strong>
      </div>
      <div class="pipeline-editor__actions">
        <u-button v-if="canExecute" :loading="running" :disabled="loading" @click="run">
          运行
        </u-button>
        <u-button
          v-if="canUpdate"
          type="primary"
          :loading="saving"
          :disabled="loading"
          @click="save"
        >
          保存编排
        </u-button>
      </div>
    </header>
    <div v-if="loading" class="pipeline-editor__loading">加载中…</div>
    <div v-else class="pipeline-editor__body">
      <PipelineCanvas
        v-model:nodes="nodes"
        v-model:edges="edges"
        :readonly="!canUpdate"
        @configure-node="openConfig"
        @node-added="openConfig"
      />
    </div>
    <NodeConfigDrawer v-model="drawerOpen" :node="configNode" @save="onConfigSave" />
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

.pipeline-editor__actions {
  display: flex;
  gap: 8px;
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
