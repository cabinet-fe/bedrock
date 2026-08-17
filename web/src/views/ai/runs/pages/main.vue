<script setup lang="ts">
defineOptions({ name: "AiRuns" });

import { computed, onMounted, reactive, ref, useTemplateRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { listAgents } from "@/api/ai";
import type { AgentRun } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType, triggerTypeLabel } from "@/lib/tag";

const STATUS_OPTIONS = [
  { label: "queued", value: "queued" },
  { label: "running", value: "running" },
  { label: "success", value: "success" },
  { label: "failed", value: "failed" },
  { label: "cancelled", value: "cancelled" },
  { label: "interrupted", value: "interrupted" },
];

const route = useRoute();
const router = useRouter();
const table = useTemplateRef("table");
const agentNameMap = ref(new Map<number, string>());

function queryString(key: string): string {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

function parseAgentID(value: string): number | undefined {
  if (!value) return undefined;
  const id = Number(value);
  return Number.isFinite(id) && id > 0 ? id : undefined;
}

const query = reactive({
  agent_id: parseAgentID(queryString("agent_id")),
  status: queryString("status"),
});

const agentOptions = computed(() =>
  [...agentNameMap.value.entries()].map(([value, label]) => ({ label, value })),
);

const columns = defineProTableColumns([
  { key: "agent_id", name: "智能体" },
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

function agentLabel(agentID: number): string {
  return agentNameMap.value.get(agentID) ?? `#${agentID}`;
}

function openDetail(row: AgentRun) {
  void router.push(`/ai/runs/${row.id}`);
}

onMounted(async () => {
  try {
    const res = await listAgents({ page: 1, page_size: 200 });
    const map = new Map<number, string>();
    for (const agent of res.items ?? []) {
      map.set(agent.id, agent.name);
    }
    agentNameMap.value = map;
  } catch {
    /* ignore */
  }
});

watch(
  () => queryString("agent_id"),
  (agentID) => {
    const next = parseAgentID(agentID);
    if (query.agent_id === next) return;
    query.agent_id = next;
    void table.value?.search();
  },
);
</script>

<template>
  <div>
    <ProTable
      ref="table"
      url="/ai/runs"
      pagination
      :columns="columns"
      :query="query"
      :auto-query-fields="['agent_id', 'status']"
    >
      <template #filters>
        <u-select
          v-model="query.agent_id"
          clearable
          placeholder="智能体"
          style="width: 200px"
          :options="agentOptions"
        />
        <u-select
          v-model="query.status"
          clearable
          placeholder="状态"
          style="width: 140px"
          :options="STATUS_OPTIONS"
        />
      </template>
      <template #column:agent_id="{ rowData }">
        {{ agentLabel((rowData as AgentRun).agent_id) }}
      </template>
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
  </div>
</template>
