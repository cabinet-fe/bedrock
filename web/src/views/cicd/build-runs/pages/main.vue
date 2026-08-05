<script setup lang="ts">
defineOptions({ name: "CicdBuildRuns" });

import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";

import { listBuildJobs } from "@/api/cicd";
import type { BuildRun } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import {
  BUILD_DISTRIBUTION_TAG,
  BUILD_STAGE_TAG,
  JOB_STATUS_TAG,
  TRIGGER_TYPE_TAG,
  tagType,
} from "@/lib/tag";

const router = useRouter();
const query = reactive({
  build_job_id: undefined as number | undefined,
  status: "",
});
const jobNameMap = ref(new Map<number, string>());

const columns = defineProTableColumns([
  { key: "build_number", name: "#" },
  { key: "build_job_id", name: "任务" },
  { key: "status", name: "状态" },
  { key: "stage", name: "阶段" },
  { key: "distribution_summary", name: "分发" },
  { key: "branch", name: "分支" },
  { key: "trigger_type", name: "触发" },
  {
    key: "duration_ms",
    name: "运行时间",
    width: 110,
    align: "center",
    render: ({ val }) => formatDurationMs(val as number) || "—",
  },
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

function openDetail(row: BuildRun) {
  void router.push({ name: "cicd-build-run-detail", params: { id: String(row.id) } });
}

onMounted(async () => {
  try {
    const jobs = await listBuildJobs({ page: 1, page_size: 100 });
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
      url="/build-runs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['status', 'build_job_id']"
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.build_job_id"
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
      <template #column:build_job_id="{ rowData }">
        {{ jobLabel((rowData as BuildRun).build_job_id) }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as BuildRun).status, JOB_STATUS_TAG)">
          {{ (rowData as BuildRun).status }}
        </u-tag>
      </template>
      <template #column:stage="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as BuildRun).stage, BUILD_STAGE_TAG)">
          {{ (rowData as BuildRun).stage || "—" }}
        </u-tag>
      </template>
      <template #column:distribution_summary="{ rowData }">
        <u-tag
          size="small"
          :type="tagType((rowData as BuildRun).distribution_summary, BUILD_DISTRIBUTION_TAG)"
        >
          {{ (rowData as BuildRun).distribution_summary || "—" }}
        </u-tag>
      </template>
      <template #column:trigger_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as BuildRun).trigger_type, TRIGGER_TYPE_TAG)">
          {{ (rowData as BuildRun).trigger_type }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openDetail(rowData as BuildRun)">详情</u-action>
      </template>
    </ProTable>
  </div>
</template>
