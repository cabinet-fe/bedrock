<script setup lang="ts">
defineOptions({ name: "SystemUsers" });

import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import { createUser, deleteUser, listRoles, updateUser } from "@/api/system";
import type { Role, User } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { useAuthStore } from "@/stores/auth";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const auth = useAuthStore();
const listRef = useTemplateRef("list");
const query = reactive({ keyword: "" });
const dialogOpen = ref(false);
const editing = ref<User | null>(null);
const roles = ref<Role[]>([]);
const form = reactive({
  username: "",
  password: "",
  display_name: "",
  email: "",
  is_active: true,
  role_ids: [] as number[],
});

const roleOptions = computed(() =>
  roles.value
    .filter((r) => r.type !== "builtin" && r.code !== "super_admin")
    .map((r) => ({ label: `${r.name} (${r.code})`, value: r.id })),
);

const roleNameById = computed(() => {
  const map = new Map<number, string>();
  for (const r of roles.value) map.set(r.id, r.name);
  return map;
});

const columns = defineProTableColumns([
  { key: "username", name: "用户名" },
  { key: "display_name", name: "显示名" },
  { key: "role_ids", name: "角色" },
  { key: "email", name: "邮箱" },
  { key: "is_active", name: "状态", width: 90, align: "center" },
  { key: "is_super_admin", name: "超管", width: 80, align: "center" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

onMounted(async () => {
  try {
    const res = await listRoles({ page: 1, page_size: 200 });
    roles.value = res.items ?? [];
  } catch {
    /* ignore */
  }
});

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(row: User) {
  editing.value = row;
  o(form).extend(row);
  form.password = "";
  form.role_ids = [...(row.role_ids ?? [])];
  dialogOpen.value = true;
}

async function save() {
  try {
    if (editing.value) {
      await updateUser(editing.value.id, {
        display_name: form.display_name,
        email: form.email,
        is_active: form.is_active,
        role_ids: form.role_ids,
        ...(form.password ? { password: form.password } : {}),
      });
      message.success("已更新");
      if (editing.value.id === auth.user?.id) {
        await auth.refreshMe(true);
      }
    } else {
      await createUser({
        username: form.username,
        password: form.password,
        display_name: form.display_name,
        email: form.email,
        is_active: form.is_active,
        role_ids: form.role_ids,
      });
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: User) => {
  try {
    await deleteUser(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/users" :query="query" :columns="columns" pagination>
      <template #filters>
        <u-input v-model="query.keyword" placeholder="用户名关键词" style="width: 200px" />
        <u-button
          v-if="hasPermission('system_users:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建用户
        </u-button>
      </template>
      <template #column:role_ids="{ rowData }">
        <span class="tag-cell">
          <u-tag v-for="id in (rowData as User).role_ids || []" :key="id" size="small" type="info">
            {{ roleNameById.get(id) ?? `#${id}` }}
          </u-tag>
          <template v-if="!(rowData as User).role_ids?.length">—</template>
        </span>
      </template>
      <template #column:is_active="{ rowData }">
        <u-tag size="small" :type="(rowData as User).is_active ? 'success' : 'warning'">
          {{ (rowData as User).is_active ? "启用" : "禁用" }}
        </u-tag>
      </template>
      <template #column:is_super_admin="{ rowData }">
        <u-tag v-if="(rowData as User).is_super_admin" size="small" type="warning"> 超管 </u-tag>
        <span v-else>—</span>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as User).id">
          <u-action v-if="hasPermission('system_users:update')" @run="openEdit(rowData as User)">
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('system_users:delete') && !(rowData as User).is_super_admin"
            need-confirm
            type="danger"
            @run="remove(rowData as User)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑用户' : '新建用户'"
      :model="form"
      label-width="88px"
      style="width: 520px"
      @submit="save"
    >
      <u-input v-if="!editing" label="用户名" field="username" :rules="{ required: '必填' }" />
      <u-password-input
        label="密码"
        field="password"
        :placeholder="editing ? '留空则不修改' : '密码'"
        :rules="editing ? undefined : { required: '必填' }"
      />
      <u-input label="显示名" field="display_name" />
      <u-input label="邮箱" field="email" />
      <u-multi-select
        label="角色"
        field="role_ids"
        :options="roleOptions"
        :disabled="!!editing?.is_super_admin"
        placeholder="选择角色"
        filterable
        clearable
      />
      <u-switch label="启用" field="is_active" />
    </FormDialog>
  </div>
</template>

<style scoped>
.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
