<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { Layers, Link, Time } from "@veltra/icons/normal";

import {
  enqueuePipelineRun,
  getPipelineRun,
  listBuildPipelines,
  listPipelineRuns,
} from "@/api/cicd";
import type { BuildPipeline, ProductProject } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";

import { isRunTerminal, useRunPoll } from "../composables/use-run-poll";
import RunCard from "./run-card.vue";
import RunHistoryDialog from "./run-history-dialog.vue";

const props = defineProps<{ project: ProductProject }>();

const { hasPermission } = usePermission();
const canExecute = hasPermission("cicd_pipelines:execute");
const canViewHistory = hasPermission("cicd_pipelines:view");

const pipelines = ref<BuildPipeline[]>([]);
const loading = ref(true);
const loadError = ref("");

const historyOpen = ref(false);
const historyPipeline = ref<BuildPipeline | null>(null);

const { statusMap, errorMap, isBusy, enqueue, loadRecent } = useRunPoll({
  fetch: (id) => getPipelineRun(id),
  isTerminal: isRunTerminal,
});

function canRun(row: BuildPipeline) {
  return row.enabled && row.trigger_manual;
}

function nodeCount(graphJson?: string): number {
  if (!graphJson) return 0;
  try {
    const graph = JSON.parse(graphJson) as { nodes?: { id?: string; type?: string }[] };
    const nodes = Array.isArray(graph.nodes) ? graph.nodes : [];
    return nodes.filter(
      (n) => n.type !== "start" && n.type !== "end" && n.id !== "__start__" && n.id !== "__end__",
    ).length;
  } catch {
    return 0;
  }
}

function disabledTip(row: BuildPipeline) {
  if (!row.enabled) return "已停用";
  if (!row.trigger_manual) return "未启用手动触发";
  return "";
}

function openHistory(row: BuildPipeline) {
  historyPipeline.value = row;
  historyOpen.value = true;
}

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await listBuildPipelines({ project_id: props.project.id, page: 1, page_size: 100 });
    pipelines.value = res.items ?? [];
  } catch (error) {
    pipelines.value = [];
    loadError.value = error instanceof Error ? error.message : "加载流水线失败";
  } finally {
    loading.value = false;
  }
}

async function loadRecentStatus() {
  try {
    const res = await listPipelineRuns({ project_id: props.project.id, page: 1, page_size: 50 });
    loadRecent(
      (res.items ?? []).map((r) => ({
        entityId: r.build_pipeline_id,
        runId: r.id,
        status: r.status,
      })),
    );
  } catch {
    /* 状态降级为无 */
  }
}

const historyEntityId = computed(() => historyPipeline.value?.id ?? 0);
const historyEntityName = computed(() => historyPipeline.value?.name ?? "");

onMounted(() => {
  void load();
  void loadRecentStatus();
});
</script>

<template>
  <div v-loading="loading" class="run-panel">
    <div v-if="!loading && (loadError || !pipelines.length)" class="run-panel__empty">
      <u-empty :text="loadError || '暂无流水线，请到「CI/CD · 流水线」创建并归属本项目'" />
    </div>
    <div v-else-if="pipelines.length" class="run-panel__grid">
      <RunCard
        v-for="row in pipelines"
        :key="row.id"
        :name="row.name"
        :description="row.description"
        :status="statusMap.get(row.id)"
        :error="errorMap.get(row.id)"
        :runnable="canRun(row)"
        :disabled-tip="disabledTip(row)"
        :busy="isBusy(row.id)"
        :can-execute="canExecute"
        :can-view-history="canViewHistory"
        @history="openHistory(row)"
        @run="enqueue(row.id, () => enqueuePipelineRun(row.id, { trigger_type: 'manual' }))"
      >
        <template #tags>
          <u-tag size="small" type="primary">流水线</u-tag>
        </template>

        <span
          v-if="nodeCount(row.graph_json)"
          class="run-card__meta-item"
          :title="`包含 ${nodeCount(row.graph_json)} 个执行节点`"
        >
          <u-icon :size="13"><Layers /></u-icon>
          <span>{{ nodeCount(row.graph_json) }} 个节点</span>
        </span>
        <span
          v-if="row.trigger_cron && row.cron_expression"
          class="run-card__meta-item run-card__mono"
          :title="'定时执行: ' + row.cron_expression"
        >
          <u-icon :size="13"><Time /></u-icon>
          <span>{{ row.cron_expression }}</span>
        </span>
        <span v-if="row.trigger_webhook" class="run-card__meta-item" title="支持 Webhook 自动触发">
          <u-icon :size="13"><Link /></u-icon>
          <span>Webhook</span>
        </span>

        <template #footer>
          <span>更新于 {{ formatDateTime(row.updated_at) || "—" }}</span>
        </template>
      </RunCard>
    </div>

    <RunHistoryDialog
      v-model="historyOpen"
      kind="pipeline"
      :entity-id="historyEntityId"
      :entity-name="historyEntityName"
      :project-id="project.id"
    />
  </div>
</template>

<style scoped lang="scss">
@use "@/lib/empty-center.scss" as empty;

.run-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: auto;
}

.run-panel__empty {
  @include empty.center;
}

.run-panel__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
  align-content: start;
  padding: 2px 2px 16px;
}
</style>
