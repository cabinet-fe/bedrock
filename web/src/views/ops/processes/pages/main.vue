<script setup lang="ts">
defineOptions({ name: "OpsProcesses" });

import { reactive, useTemplateRef } from "vue";
import { message } from "@veltra/desktop";

import { killProcess } from "@/api/ops";
import type { ProcessInfo } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { formatDateTime } from "@/lib/datetime";
import { tagType, type TagType } from "@/lib/tag";

const PROCESS_STATUS_TAG: Record<string, TagType> = {
  R: "success",
  S: undefined,
  D: "warning",
  Z: "danger",
  T: "info",
  t: "info",
  X: "danger",
  I: undefined,
};

const { busyKey, run } = useBusyKey();
const listRef = useTemplateRef("list");
const query = reactive({
  keyword: "",
  pid: "",
  port: "",
  sort: "cpu_percent@desc",
});

const columns = defineProTableColumns([
  { key: "pid", name: "PID" },
  { key: "name", name: "名称", sortable: true },
  { key: "cpu_percent", name: "CPU", sortable: true },
  { key: "memory_bytes", name: "内存", sortable: true },
  { key: "username", name: "用户" },
  { key: "num_threads", name: "线程" },
  { key: "status", name: "状态", width: 80, align: "center" },
  {
    key: "start_time",
    name: "启动时间",
    width: 170,
    align: "center",
    render: ({ val }) => (val ? formatDateTime(val) : "—"),
  },
  { key: "ports", name: "监听端口" },
  { key: "cmdline", name: "命令行" },
  { key: "action", name: "操作", width: 120, align: "center", fixed: "right" },
]);

function formatBytes(value: number): string {
  if (!value) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

async function terminate(row: ProcessInfo) {
  await run(row.pid, async () => {
    try {
      await killProcess(row.pid);
      message.success("进程终止请求已发送");
      await listRef.value?.reload();
    } catch (error) {
      message.error(error instanceof Error ? error.message : "终止进程失败");
    }
  });
}
</script>

<template>
  <div>
    <ProTable ref="list" url="/ops/processes" :query="query" :columns="columns" row-key="pid">
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称 / 命令行" style="width: 180px" />
        <u-input v-model="query.pid" type="number" placeholder="PID" style="width: 110px" />
        <u-input v-model="query.port" type="number" placeholder="端口" style="width: 110px" />
      </template>
      <template #column:cpu_percent="{ rowData }">
        {{ (rowData as ProcessInfo).cpu_percent.toFixed(1) }}%
      </template>
      <template #column:memory_bytes="{ rowData }">
        {{ formatBytes((rowData as ProcessInfo).memory_bytes) }}
      </template>
      <template #column:num_threads="{ rowData }">
        {{ (rowData as ProcessInfo).num_threads || "—" }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag
          v-if="(rowData as ProcessInfo).status"
          size="small"
          :type="tagType((rowData as ProcessInfo).status, PROCESS_STATUS_TAG)"
        >
          {{ (rowData as ProcessInfo).status }}
        </u-tag>
        <span v-else>—</span>
      </template>
      <template #column:ports="{ rowData }">
        {{ (rowData as ProcessInfo).ports.join(", ") || "—" }}
      </template>
      <template #column:cmdline="{ rowData }">
        <span class="cmdline" :title="(rowData as ProcessInfo).cmdline || undefined">
          {{ (rowData as ProcessInfo).cmdline || "—" }}
        </span>
      </template>
      <template #column:action="{ rowData }">
        <u-action
          need-confirm
          type="danger"
          :loading="busyKey === (rowData as ProcessInfo).pid"
          @run="terminate(rowData as ProcessInfo)"
        >
          终止
        </u-action>
      </template>
    </ProTable>
  </div>
</template>

<style scoped>
.cmdline {
  display: block;
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
</style>
