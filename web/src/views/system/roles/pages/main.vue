<script setup lang="ts">
defineOptions({ name: "SystemRoles" });

import { computed, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import { createRole, deleteRole, updateRole } from "@/api/system";
import type { Role } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const listRef = useTemplateRef("list");
const dialogOpen = ref(false);
const permsOpen = ref(false);
const editing = ref<Role | null>(null);
const form = reactive({
  name: "",
  code: "",
  description: "",
  data_scope: "self" as "self" | "all",
});

const DATA_SCOPE_OPTIONS = [
  { label: "仅自己", value: "self" },
  { label: "全部", value: "all" },
];

const DATA_SCOPE_LABEL: Record<string, string> = {
  self: "仅自己",
  all: "全部",
};

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "code", name: "编码" },
  { key: "data_scope", name: "数据权限", width: 100, align: "center" },
  { key: "type", name: "类型", width: 90, align: "center" },
  { key: "description", name: "描述" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

const isBuiltin = computed(() => editing.value?.type === "builtin");

function isBuiltinRole(row: Role) {
  return row.type === "builtin" || row.code === "super_admin";
}

function openCreate() {
  editing.value = null;
  o(form).extend({ name: "", code: "", description: "", data_scope: "self" });
  dialogOpen.value = true;
}

function openEdit(row: Role) {
  if (isBuiltinRole(row)) return;
  editing.value = row;
  o(form).extend(row);
  dialogOpen.value = true;
}

function openPerms(row: Role) {
  editing.value = row;
  permsOpen.value = true;
}

async function save() {
  try {
    if (editing.value) {
      await updateRole(editing.value.id, {
        name: form.name,
        description: form.description,
        data_scope: form.data_scope,
      });
      message.success("已更新");
    } else {
      await createRole({ ...form });
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Role) => {
  if (isBuiltinRole(row)) return;
  try {
    await deleteRole(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/roles" :columns="columns" pagination>
      <template #toolbar>
        <u-button
          v-if="hasPermission('system_roles:create')"
          type="primary"
          @click.prevent="openCreate"
        >
          新建角色
        </u-button>
      </template>
      <template #column:data_scope="{ rowData }">
        {{ DATA_SCOPE_LABEL[(rowData as Role).data_scope || "self"] || "仅自己" }}
      </template>
      <template #column:type="{ rowData }">
        <u-tag size="small" :type="isBuiltinRole(rowData as Role) ? 'warning' : 'info'">
          {{ isBuiltinRole(rowData as Role) ? "内置" : "自定义" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="4" :loading="busyKey === (rowData as Role).id">
          <u-action
            v-if="hasPermission('system_roles:update') && !isBuiltinRole(rowData as Role)"
            @run="openEdit(rowData as Role)"
          >
            编辑
          </u-action>
          <u-action v-if="hasPermission('system_roles:view')" @run="openPerms(rowData as Role)">
            权限
          </u-action>
          <u-action
            v-if="hasPermission('system_roles:delete') && !isBuiltinRole(rowData as Role)"
            need-confirm
            type="danger"
            @run="remove(rowData as Role)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑角色' : '新建角色'"
      :model="form"
      label-width="72px"
      style="width: 480px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input
        label="编码"
        field="code"
        :disabled="!!editing"
        :rules="{ required: '必填' }"
        tips="唯一标识，创建后不可修改；用于程序引用，不含空格"
      />
      <u-select
        label="数据权限"
        field="data_scope"
        :options="DATA_SCOPE_OPTIONS"
        :rules="{ required: '必填' }"
        tips="仅自己：项目可见成员所在项目，CI/CD 仅自己创建的任务；全部：可读全部项目/任务（写权限仍靠成员角色或 manage_all）。与 view_all 并存，多角色取最宽"
      />
      <u-input label="描述" field="description" />
    </FormDialog>

    <u-dialog v-model="permsOpen" title="角色权限" style="width: 560px">
      <p class="perm-hint">
        所有角色默认拥有除「系统信息」「系统状态」外的全部功能权限，无需也无法按角色分配。
      </p>
      <p class="perm-hint">
        仅超级管理员可访问系统信息与系统状态；角色仅用于区分数据可见范围（见上表「数据权限」）。
      </p>
      <p v-if="isBuiltin" class="perm-hint">内置超级管理员拥有全部权限。</p>
      <template #footer="{ close }">
        <u-button type="primary" @click="close()">知道了</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.perm-hint {
  margin: 0 0 fn.use-var(gap, small);
  color: fn.use-var(text-color, second);
}
</style>
