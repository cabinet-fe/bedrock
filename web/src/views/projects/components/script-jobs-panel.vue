<script setup lang="ts">
import { onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { enqueueScriptRun, listScriptRuns } from "@/api/cicd";
import type { ProductProject, ScriptJob } from "@/api/types";
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
  { key: "script_type", name: "类型", width: 120, align: "center" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "recent_status", name: "最近状态", width: 110, align: "center" },
  { key: "action", name: "操作", width: 180, align: "center", fixed: "right" },
]);

function canRun(job: ScriptJob) {
  return job.enabled && job.trigger_manual;
}

function runDisabledTip(job: ScriptJob) {
  if (!job.enabled) return "任务已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function goCreate() {
  void router.push({
    path: "/cicd/script-jobs",
    query: { project_id: String(props.project.id), create: "1" },
  });
}

function goEdit(row: ScriptJob) {
  void router.push({
    path: "/cicd/script-jobs",
    query: { project_id: String(props.project.id), id: String(row.id) },
  });
}

const trigger = bind(async (row: ScriptJob) => {
  try {
    const run = await enqueueScriptRun(row.id);
    message.success(`已入队 #${run.run_number}`);
    await loadRecentStatus();
    void tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "触发失败");
  }
});

async function loadRecentStatus() {
  try {
    const res = await listScriptRuns({
      project_id: props.project.id,
      page: 1,
      page_size: 50,
    });
    const map = new Map<number, string>();
    for (const run of res.items ?? []) {
      if (!map.has(run.script_job_id)) map.set(run.script_job_id, run.status);
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
      url="/script-jobs"
      :query="query"
      :columns="columns"
      pagination
      height="100%"
    >
      <template #toolbar>
        <u-button
          v-if="hasPermission('cicd_script_jobs:create')"
          type="primary"
          @click.prevent="goCreate"
        >
          新建
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as ScriptJob).enabled ? 'success' : undefined">
          {{ (rowData as ScriptJob).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:recent_status="{ rowData }">
        <u-tag
          v-if="recentStatus.has((rowData as ScriptJob).id)"
          size="small"
          :type="tagType(recentStatus.get((rowData as ScriptJob).id), JOB_STATUS_TAG)"
        >
          {{ recentStatus.get((rowData as ScriptJob).id) }}
        </u-tag>
        <template v-else>—</template>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="2" :loading="busyKey === (rowData as ScriptJob).id">
          <u-action
            v-if="hasPermission('cicd_script_jobs:execute')"
            :disabled="!canRun(rowData as ScriptJob)"
            :title="runDisabledTip(rowData as ScriptJob)"
            @run="trigger(rowData as ScriptJob)"
          >
            触发
          </u-action>
          <u-action
            v-if="hasPermission('cicd_script_jobs:update')"
            @run="goEdit(rowData as ScriptJob)"
          >
            编辑
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
