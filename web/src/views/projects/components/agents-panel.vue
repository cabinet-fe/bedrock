<script setup lang="ts">
import { onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { listRuns, manualRunAgent } from "@/api/ai";
import type { AiAgent, ProductProject } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { JOB_STATUS_TAG, tagType } from "@/lib/tag";

import RunHistoryDialog from "@/views/ai/agents/components/run-history-dialog.vue";

const props = defineProps<{ project: ProductProject }>();

const router = useRouter();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const tableRef = useTemplateRef("table");
const query = reactive({ project_id: props.project.id });
const recentStatus = ref(new Map<number, string>());
const historyOpen = ref(false);
const historyAgent = ref<AiAgent | null>(null);

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "cli_key", name: "CLI", width: 120, align: "center" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "recent_status", name: "最近运行", width: 110, align: "center" },
  { key: "action", name: "操作", width: 260, align: "center", fixed: "right" },
]);

function canRun(row: AiAgent) {
  return row.enabled && row.workspace_status === "ready";
}

function runDisabledTip(row: AiAgent) {
  if (!row.enabled) return "智能体未启用";
  if (row.workspace_status === "pending") return "工作区初始化中";
  if (row.workspace_status === "failed") {
    return row.workspace_error || "工作区初始化失败";
  }
  return "";
}

function goCreate() {
  void router.push({
    path: "/ai/agents",
    query: { project_id: String(props.project.id), create: "1" },
  });
}

function goEdit(row: AiAgent) {
  void router.push({
    path: "/ai/agents",
    query: { project_id: String(props.project.id), id: String(row.id) },
  });
}

function openHistory(row: AiAgent) {
  historyAgent.value = row;
  historyOpen.value = true;
}

const trigger = bind(async (row: AiAgent) => {
  if (!canRun(row)) {
    message.error(runDisabledTip(row) || "无法运行");
    return;
  }
  try {
    const run = await manualRunAgent(row.id);
    message.success(`已创建运行 #${run.id}`);
    await loadRecentStatus();
    void tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "触发失败");
  }
});

async function loadRecentStatus() {
  try {
    const res = await listRuns({
      project_id: props.project.id,
      page: 1,
      page_size: 50,
    });
    const map = new Map<number, string>();
    for (const run of res.items ?? []) {
      if (!map.has(run.agent_id)) map.set(run.agent_id, run.status);
    }
    recentStatus.value = map;
  } catch {
    recentStatus.value = new Map();
  }
}

onMounted(() => {
  if (hasPermission("ai_runs:view")) void loadRecentStatus();
});
</script>

<template>
  <div class="resource-panel">
    <ProTable
      ref="table"
      url="/ai/agents"
      :query="query"
      :columns="columns"
      pagination
      height="100%"
    >
      <template #toolbar>
        <u-button v-if="hasPermission('ai_agents:create')" type="primary" @click.prevent="goCreate">
          新建
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as AiAgent).enabled ? 'success' : undefined">
          {{ (rowData as AiAgent).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:recent_status="{ rowData }">
        <u-tag
          v-if="recentStatus.has((rowData as AiAgent).id)"
          size="small"
          :type="tagType(recentStatus.get((rowData as AiAgent).id), JOB_STATUS_TAG)"
        >
          {{ recentStatus.get((rowData as AiAgent).id) }}
        </u-tag>
        <template v-else>—</template>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as AiAgent).id">
          <u-action
            v-if="hasPermission('ai_agents:execute')"
            :disabled="!canRun(rowData as AiAgent)"
            :title="runDisabledTip(rowData as AiAgent)"
            @run="trigger(rowData as AiAgent)"
          >
            运行
          </u-action>
          <u-action v-if="hasPermission('ai_runs:view')" @run="openHistory(rowData as AiAgent)">
            历史
          </u-action>
          <u-action v-if="hasPermission('ai_agents:update')" @run="goEdit(rowData as AiAgent)">
            编辑
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <RunHistoryDialog v-model="historyOpen" :agent="historyAgent" />
  </div>
</template>

<style scoped>
.resource-panel {
  height: 100%;
  min-height: 0;
}
</style>
