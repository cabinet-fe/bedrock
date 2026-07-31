<script setup lang="ts">
defineOptions({ name: "CicdPipelineRuns" });

import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";

import { listBuildPipelines } from "@/api/cicd";
import type { PipelineRun } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";

const router = useRouter();
const query = reactive({
  build_pipeline_id: undefined as number | undefined,
  status: "",
});
const pipelineNameMap = ref(new Map<number, string>());

const columns = defineProTableColumns([
  { key: "run_number", name: "#" },
  { key: "build_pipeline_id", name: "流水线" },
  { key: "status", name: "状态" },
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

const pipelineOptions = computed(() =>
  [...pipelineNameMap.value.entries()].map(([value, label]) => ({ label, value })),
);

function pipelineLabel(id: number): string {
  return pipelineNameMap.value.get(id) ?? String(id);
}

function openDetail(row: PipelineRun) {
  void router.push({ name: "cicd-pipeline-run-detail", params: { id: String(row.id) } });
}

onMounted(async () => {
  try {
    const page = await listBuildPipelines({ page: 1, page_size: 100 });
    const map = new Map<number, string>();
    for (const p of page.items ?? []) {
      map.set(p.id, p.name);
    }
    pipelineNameMap.value = map;
  } catch {
    /* ignore */
  }
});
</script>

<template>
  <div>
    <ProTable
      url="/pipeline-runs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['status', 'build_pipeline_id']"
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.build_pipeline_id"
          clearable
          placeholder="流水线"
          style="width: 180px"
          :options="pipelineOptions"
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
          ]"
        />
      </template>
      <template #column:build_pipeline_id="{ rowData }">
        {{ pipelineLabel((rowData as PipelineRun).build_pipeline_id) }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as PipelineRun).status, JOB_STATUS_TAG)">
          {{ (rowData as PipelineRun).status }}
        </u-tag>
      </template>
      <template #column:trigger_type="{ rowData }">
        <u-tag
          size="small"
          :type="tagType((rowData as PipelineRun).trigger_type, TRIGGER_TYPE_TAG)"
        >
          {{ (rowData as PipelineRun).trigger_type }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openDetail(rowData as PipelineRun)">详情</u-action>
      </template>
    </ProTable>
  </div>
</template>
