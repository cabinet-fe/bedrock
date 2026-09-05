<script setup lang="ts">
import { computed, nextTick, reactive, useTemplateRef, watch } from "vue";

import type { RouteTarget } from "@/components/dashboard-route-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import type { BuildRun, PipelineRun, ScriptRun } from "@/api/types";
import { formatDateTime, formatDurationBetween, formatDurationMs } from "@/lib/datetime";
import { BUILD_STAGE_TAG, JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";

export type RunningDialogKind = "build" | "script" | "pipeline";

const buildColumns = defineProTableColumns([
  { key: "build_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "stage", name: "阶段", width: 100, align: "center" },
  { key: "branch", name: "分支" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
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

const scriptColumns = defineProTableColumns([
  { key: "run_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
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

const pipelineColumns = defineProTableColumns([
  { key: "run_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
  {
    key: "duration",
    name: "运行时间",
    width: 110,
    align: "center",
    render: ({ rowData }) =>
      formatDurationBetween(
        (rowData as PipelineRun).started_at,
        (rowData as PipelineRun).finished_at,
      ) || "—",
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

const TITLE: Record<RunningDialogKind, string> = {
  build: "运行中的构建",
  script: "运行中的脚本",
  pipeline: "运行中的流水线",
};

const DETAIL_ROUTE: Record<RunningDialogKind, string> = {
  build: "cicd-build-run-detail",
  script: "cicd-script-run-detail",
  pipeline: "cicd-pipeline-run-detail",
};

const DETAIL_TITLE: Record<RunningDialogKind, string> = {
  build: "构建详情",
  script: "脚本详情",
  pipeline: "流水线运行详情",
};

const props = defineProps<{
  kind: RunningDialogKind;
}>();

const open = defineModel<boolean>({ default: false });

const emit = defineEmits<{
  openDetail: [title: string, route: RouteTarget];
}>();

const tableRef = useTemplateRef("table");

const query = reactive({
  status: "running",
});

const title = computed(() => TITLE[props.kind]);

const url = computed(() => {
  if (props.kind === "build") return "/build-runs";
  if (props.kind === "script") return "/script-runs";
  return "/pipeline-runs";
});

const columns = computed(() => {
  if (props.kind === "build") return buildColumns;
  if (props.kind === "script") return scriptColumns;
  return pipelineColumns;
});

function openDetail(row: BuildRun | ScriptRun | PipelineRun) {
  open.value = false;
  emit("openDetail", DETAIL_TITLE[props.kind], {
    name: DETAIL_ROUTE[props.kind],
    params: { id: String(row.id) },
  });
}

watch(open, async (visible) => {
  if (!visible) return;
  query.status = "running";
  await nextTick();
  void tableRef.value?.reload();
});
</script>

<template>
  <u-dialog v-model="open" :title="title" style="width: 960px">
    <ProTable
      ref="table"
      :url="url"
      :query="query"
      :columns="columns"
      :immediate="false"
      pagination
      height="420px"
    >
      <template v-if="kind === 'build'" #column:stage="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as BuildRun).stage, BUILD_STAGE_TAG)">
          {{ (rowData as BuildRun).stage || "—" }}
        </u-tag>
      </template>
      <template #column:status="{ rowData }">
        <u-tag
          size="small"
          :type="tagType((rowData as BuildRun | ScriptRun | PipelineRun).status, JOB_STATUS_TAG)"
        >
          {{ (rowData as BuildRun | ScriptRun | PipelineRun).status }}
        </u-tag>
      </template>
      <template #column:trigger_type="{ rowData }">
        <u-tag
          size="small"
          :type="
            tagType((rowData as BuildRun | ScriptRun | PipelineRun).trigger_type, TRIGGER_TYPE_TAG)
          "
        >
          {{ (rowData as BuildRun | ScriptRun | PipelineRun).trigger_type }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openDetail(rowData as BuildRun | ScriptRun | PipelineRun)"
          >查看详情</u-action
        >
      </template>
    </ProTable>
    <template #footer="{ close }">
      <u-button text @click="close()">关闭</u-button>
    </template>
  </u-dialog>
</template>
