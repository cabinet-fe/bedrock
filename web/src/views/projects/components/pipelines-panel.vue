<script setup lang="ts">
import { onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { enqueuePipelineRun, listPipelineRuns } from "@/api/cicd";
import type { BuildPipeline, ProductProject } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { JOB_STATUS_TAG, tagType } from "@/lib/tag";

const props = defineProps<{ project: ProductProject }>();

const router = useRouter();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const tableRef = useTemplateRef("table");
const query = reactive({ project_id: props.project.id });
const recentStatus = ref(new Map<number, string>());

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "recent_status", name: "最近运行", width: 110, align: "center" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

function canRun(row: BuildPipeline) {
  return row.enabled && row.trigger_manual;
}

function runDisabledTip(row: BuildPipeline) {
  if (!row.enabled) return "流水线已停用";
  if (!row.trigger_manual) return "未启用手动触发";
  return "";
}

function goCreate() {
  void router.push({
    path: "/cicd/pipelines",
    query: { project_id: String(props.project.id), create: "1" },
  });
}

function openEditor(row: BuildPipeline) {
  void router.push({ name: "cicd-pipeline-editor", params: { id: String(row.id) } });
}

const trigger = bind(async (row: BuildPipeline) => {
  try {
    const run = await enqueuePipelineRun(row.id, { trigger_type: "manual" });
    message.success(`已触发 #${run.run_number}`);
    await loadRecentStatus();
    void tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "触发失败");
  }
});

async function loadRecentStatus() {
  try {
    const res = await listPipelineRuns({
      project_id: props.project.id,
      page: 1,
      page_size: 50,
    });
    const map = new Map<number, string>();
    for (const run of res.items ?? []) {
      if (!map.has(run.build_pipeline_id)) map.set(run.build_pipeline_id, run.status);
    }
    recentStatus.value = map;
  } catch {
    recentStatus.value = new Map();
  }
}

onMounted(() => {
  void loadRecentStatus();
});
</script>

<template>
  <div class="resource-panel">
    <ProTable
      ref="table"
      url="/build-pipelines"
      :query="query"
      :columns="columns"
      pagination
      height="100%"
    >
      <template #toolbar>
        <u-button
          v-if="hasPermission('cicd_pipelines:create')"
          type="primary"
          @click.prevent="goCreate"
        >
          新建
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as BuildPipeline).enabled ? 'success' : undefined">
          {{ (rowData as BuildPipeline).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:recent_status="{ rowData }">
        <u-tag
          v-if="recentStatus.has((rowData as BuildPipeline).id)"
          size="small"
          :type="tagType(recentStatus.get((rowData as BuildPipeline).id), JOB_STATUS_TAG)"
        >
          {{ recentStatus.get((rowData as BuildPipeline).id) }}
        </u-tag>
        <template v-else>—</template>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="2" :loading="busyKey === (rowData as BuildPipeline).id">
          <u-action
            v-if="hasPermission('cicd_pipelines:execute')"
            :disabled="!canRun(rowData as BuildPipeline)"
            :title="runDisabledTip(rowData as BuildPipeline)"
            @run="trigger(rowData as BuildPipeline)"
          >
            触发
          </u-action>
          <u-action
            v-if="hasPermission('cicd_pipelines:update')"
            @run="openEditor(rowData as BuildPipeline)"
          >
            编排
          </u-action>
        </u-action-group>
      </template>
    </ProTable>
  </div>
</template>

<style scoped>
.resource-panel {
  height: 100%;
  min-height: 0;
}
</style>
