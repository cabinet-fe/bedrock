<script setup lang="ts">
defineOptions({ name: "SystemOperationLogs" });

import { reactive, ref } from "vue";
import { message } from "@veltra/desktop";
import { Delete } from "@veltra/icons/normal";

import { clearOperationLogs } from "@/api/system";
import type { OperationLog } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime } from "@/lib/datetime";
import { tagType, type TagType } from "@/lib/tag";
import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();
const clearing = ref(false);
const confirmOpen = ref(false);
let tableReload: (() => Promise<void>) | null = null;

const query = reactive({
  action: "",
  resource_type: "",
});

const ACTION_TAG: Record<string, TagType> = {
  GET: "info",
  POST: "primary",
  PUT: "warning",
  PATCH: "warning",
  DELETE: "danger",
};

function openClearDialog(reload: () => Promise<void>) {
  tableReload = reload;
  confirmOpen.value = true;
}

async function handleClearConfirm() {
  clearing.value = true;
  try {
    await clearOperationLogs();
    message.success("操作日志已清空");
    confirmOpen.value = false;
    if (tableReload) {
      await tableReload();
    }
  } catch (err) {
    message.error(err instanceof Error ? err.message : "清空失败");
  } finally {
    clearing.value = false;
  }
}

const columns = defineProTableColumns([
  { key: "username", name: "用户", width: 100, minWidth: 80 },
  { key: "action", name: "动作", width: 150, align: "center" },
  { key: "resource_type", name: "资源", minWidth: 280 },
  { key: "resource_id", name: "资源ID", width: 90, minWidth: 80 },
  { key: "ip_address", name: "IP", width: 140, minWidth: 120 },
  {
    key: "created_at",
    name: "时间",
    width: 170,
    minWidth: 160,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
]);
</script>

<template>
  <div>
    <ProTable url="/operation-logs" :query="query" :columns="columns" pagination>
      <template #filters>
        <u-input v-model="query.action" placeholder="动作 (POST/PUT…)" style="width: 160px" />
        <u-input v-model="query.resource_type" placeholder="资源路径" style="width: 220px" />
      </template>
      <template #toolbar="{ reload }">
        <u-button
          v-if="
            auth.hasPermission('system_operation_logs:clear') ||
            auth.hasPermission('system_operation_logs:delete')
          "
          type="danger"
          :icon="Delete"
          @click.prevent="openClearDialog(reload)"
        >
          清空日志
        </u-button>
      </template>
      <template #column:action="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as OperationLog).action, ACTION_TAG)">
          {{ (rowData as OperationLog).action }}
        </u-tag>
      </template>
    </ProTable>

    <u-dialog v-model="confirmOpen" title="清空确认" style="width: 420px">
      <div class="confirm-dialog-content">确认清空所有操作日志？此操作不可恢复。</div>
      <template #footer="{ close }">
        <u-button text :disabled="clearing" @click="close">取消</u-button>
        <u-button type="danger" :loading="clearing" @click="handleClearConfirm">
          确认清空
        </u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
.confirm-dialog-content {
  padding: 12px 0 16px;
  font-size: 14px;
}
</style>
