<script setup lang="ts">
import { reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import { createMenuGroup, deleteMenuGroup, updateMenuGroup } from "@/api/system";
import type { MenuGroup } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const listRef = useTemplateRef("list");
const dialogOpen = ref(false);
const editing = ref<MenuGroup | null>(null);
const form = reactive({
  name: "",
  code: "",
  route_prefix: "",
  sort_key: 0,
  enabled: true,
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "code", name: "编码" },
  { key: "route_prefix", name: "路由前缀" },
  { key: "sort_key", name: "排序" },
  { key: "enabled", name: "状态", width: 90, align: "center" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(row: MenuGroup) {
  editing.value = row;
  o(form).extend(row);
  dialogOpen.value = true;
}

async function save() {
  try {
    if (editing.value) {
      await updateMenuGroup(editing.value.id, form);
      message.success("已更新");
    } else {
      await createMenuGroup(form);
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: MenuGroup) => {
  try {
    await deleteMenuGroup(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/menu-groups" :columns="columns">
      <template #toolbar>
        <u-button
          v-if="hasPermission('system_resources:create')"
          type="primary"
          @click.prevent="openCreate"
        >
          新建分组
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as MenuGroup).enabled ? 'success' : 'warning'">
          {{ (rowData as MenuGroup).enabled ? "启用" : "禁用" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as MenuGroup).id">
          <u-action
            v-if="hasPermission('system_resources:update')"
            @run="openEdit(rowData as MenuGroup)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('system_resources:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as MenuGroup)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑分组' : '新建分组'"
      :model="form"
      label-width="96px"
      style="width: 480px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input
        label="编码"
        field="code"
        :disabled="!!editing"
        :rules="{ required: '必填' }"
        placeholder="如 system（不含 .）"
      />
      <u-input label="路由前缀" field="route_prefix" placeholder="/system" />
      <u-number-input label="排序" field="sort_key" />
      <u-switch label="启用" field="enabled" />
    </FormDialog>
  </div>
</template>
