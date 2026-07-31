<script setup lang="ts">
defineOptions({ name: "CicdScriptRuns" });

import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";

import { listScriptJobs } from "@/api/cicd";
import type { ScriptRun } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime } from "@/lib/datetime";
import { BUILD_STAGE_TAG, JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";

const router = useRouter();
const query = reactive({
  script_job_id: undefined as number | undefined,
  status: "",
});
const jobNameMap = ref(new Map<number, string>());

const columns = defineProTableColumns([
  { key: "run_number", name: "#" },
  { key: "script_job_id", name: "任务" },
  { key: "status", name: "状态" },
  { key: "stage", name: "阶段" },
  { key: "trigger_type", name: "触发" },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 120, align: "center", fixed: "right" },
]);

const jobOptions = computed(() =>
  [...jobNameMap.value.entries()].map(([value, label]) => ({ label, value })),
);

function jobLabel(jobId: number): string {
  return jobNameMap.value.get(jobId) ?? String(jobId);
}

function openDetail(row: ScriptRun) {
  void router.push({ name: "cicd-script-run-detail", params: { id: String(row.id) } });
}

onMounted(async () => {
  try {
    const jobs = await listScriptJobs({ page: 1, page_size: 100 });
    const map = new Map<number, string>();
    for (const job of jobs.items ?? []) {
      map.set(job.id, job.name);
    }
    jobNameMap.value = map;
  } catch {
    /* ignore */
  }
});
</script>

<template>
  <div>
    <ProTable
      url="/script-runs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['status', 'script_job_id']"
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.script_job_id"
          clearable
          placeholder="任务"
          style="width: 180px"
          :options="jobOptions"
        />
        <u-select
          v-model="query.status"
          clearable
          placeholder="状态"
          style="width: 140px"
          :options="[
            { label: 'queued', value: 'queued' },
            { label: 'running', value: 'running' },
            { label: 'success', value: 'success' },
            { label: 'failed', value: 'failed' },
            { label: 'cancelled', value: 'cancelled' },
            { label: 'interrupted', value: 'interrupted' },
          ]"
        />
      </template>
      <template #column:script_job_id="{ rowData }">
        {{ jobLabel((rowData as ScriptRun).script_job_id) }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ScriptRun).status, JOB_STATUS_TAG)">
          {{ (rowData as ScriptRun).status }}
        </u-tag>
      </template>
      <template #column:stage="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ScriptRun).stage, BUILD_STAGE_TAG)">
          {{ (rowData as ScriptRun).stage || "—" }}
        </u-tag>
      </template>
      <template #column:trigger_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ScriptRun).trigger_type, TRIGGER_TYPE_TAG)">
          {{ (rowData as ScriptRun).trigger_type }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openDetail(rowData as ScriptRun)">详情</u-action>
      </template>
    </ProTable>
  </div>
</template>
