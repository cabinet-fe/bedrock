<script setup lang="ts">
defineOptions({ name: "AiAgentRunHistoryDialog" });

import { nextTick, reactive, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";

import type { AgentRun, AiAgent } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType, triggerTypeLabel } from "@/lib/tag";

const open = defineModel<boolean>({ required: true });
const props = defineProps<{
  agent: AiAgent | null;
}>();

const router = useRouter();
const table = useTemplateRef("table");
const query = reactive({ agent_id: undefined as number | undefined });

const columns = defineProTableColumns([
  { key: "trigger_type", name: "触发", width: 110, align: "center" },
  { key: "status", name: "状态", width: 100, align: "center" },
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
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 120, align: "center", fixed: "right" },
]);

function openDetail(row: AgentRun) {
  open.value = false;
  void router.push(`/ai/runs/${row.id}`);
}

watch(open, async (visible) => {
  if (!visible || !props.agent) return;
  query.agent_id = props.agent.id;
  await nextTick();
  void table.value?.reload();
});
</script>

<template>
  <u-dialog
    v-model="open"
    :title="agent ? `运行历史 · ${agent.name}` : '运行历史'"
    style="width: 960px"
  >
    <ProTable
      ref="table"
      url="/ai/runs"
      :query="query"
      :columns="columns"
      :immediate="false"
      pagination
      height="420px"
    >
      <template #column:trigger_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as AgentRun).trigger_type, TRIGGER_TYPE_TAG)">
          {{ triggerTypeLabel((rowData as AgentRun).trigger_type) }}
        </u-tag>
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as AgentRun).status, JOB_STATUS_TAG)">
          {{ (rowData as AgentRun).status }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openDetail(rowData as AgentRun)">详情</u-action>
      </template>
    </ProTable>
    <template #footer="{ close }">
      <u-button text @click="close()">关闭</u-button>
    </template>
  </u-dialog>
</template>
