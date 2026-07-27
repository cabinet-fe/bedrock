<script setup lang="ts">
defineOptions({ name: "SystemRoles" });

import { computed, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  createRole,
  deleteRole,
  getPermissionCatalog,
  setRolePermissions,
  updateRole,
} from "@/api/system";
import type {
  PermissionCatalogFeature,
  PermissionCatalogGroup,
  PermissionCatalogMenu,
  Role,
} from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { useAuthStore } from "@/stores/auth";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: permBusy, run: runPerm } = useBusy();
const auth = useAuthStore();
const listRef = useTemplateRef("list");
const dialogOpen = ref(false);
const permOpen = ref(false);
const editing = ref<Role | null>(null);
const form = reactive({
  name: "",
  code: "",
  description: "",
  data_scope: "self" as "self" | "all",
});
const catalog = ref<PermissionCatalogGroup[]>([]);
const checked = ref<Set<string>>(new Set());

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
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
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

const openPerms = bind(async (row: Role) => {
  if (isBuiltinRole(row)) {
    message.info("内置超级管理员拥有全部权限，不可编辑");
    return;
  }
  editing.value = row;
  if (!catalog.value.length) {
    const res = await getPermissionCatalog();
    catalog.value = res.items ?? [];
  }
  checked.value = new Set((row.permissions ?? []).map((p) => p.permission));
  permOpen.value = true;
});

function isChecked(code: string) {
  return checked.value.has(code);
}

function isFeatureBindable(feat: PermissionCatalogFeature) {
  return !feat.super_admin_only && feat.enabled;
}

function bindableFeatures(menu: PermissionCatalogMenu) {
  return (menu.features ?? []).filter(isFeatureBindable);
}

function menuCheckState(menu: PermissionCatalogMenu): boolean | "indeterminate" {
  const feats = bindableFeatures(menu);
  if (!feats.length) return false;
  const n = feats.filter((f) => checked.value.has(f.full_code)).length;
  if (n === 0) return false;
  if (n === feats.length) return true;
  return "indeterminate";
}

function toggleFeature(feat: PermissionCatalogFeature, on: boolean) {
  if (!isFeatureBindable(feat)) return;
  const next = new Set(checked.value);
  if (on) next.add(feat.full_code);
  else next.delete(feat.full_code);
  checked.value = next;
}

function toggleMenu(menu: PermissionCatalogMenu, on: boolean) {
  const next = new Set(checked.value);
  for (const feat of bindableFeatures(menu)) {
    if (on) next.add(feat.full_code);
    else next.delete(feat.full_code);
  }
  checked.value = next;
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
      await createRole({ ...form, permissions: [] });
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

async function savePerms() {
  if (!editing.value || isBuiltin.value) return;
  await runPerm(async () => {
    try {
      await setRolePermissions(editing.value!.id, [...checked.value]);
      message.success("权限已保存");
      permOpen.value = false;
      await listRef.value?.reload();
      await auth.refreshMe(true);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "保存失败");
    }
  });
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
      <template #filters>
        <u-button
          v-if="hasPermission('system_roles:create')"
          type="primary"
          style="margin-left: auto"
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
          <u-action
            v-if="hasPermission('system_roles:update') && !isBuiltinRole(rowData as Role)"
            @run="openPerms(rowData as Role)"
          >
            权限
          </u-action>
          <u-action
            v-else-if="hasPermission('system_roles:update') && isBuiltinRole(rowData as Role)"
            @run="openPerms(rowData as Role)"
          >
            全部权限
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

    <u-dialog v-model="permOpen" title="角色权限" style="width: 780px">
      <p v-if="isBuiltin" class="perm-hint">内置超级管理员拥有全部权限，不可修改绑定。</p>
      <div v-else class="perm-catalog">
        <section v-for="group in catalog" :key="group.id" class="perm-group">
          <h3 class="perm-group__title">{{ group.name }}</h3>
          <div v-for="menu in group.menus" :key="menu.id" class="perm-menu">
            <div class="perm-menu__head">
              <u-checkbox
                :model-value="menuCheckState(menu) === true"
                :indeterminate="menuCheckState(menu) === 'indeterminate'"
                :disabled="!bindableFeatures(menu).length"
                @change="(on) => toggleMenu(menu, on)"
              >
                <span class="perm-menu__title">{{ menu.title }}</span>
                <code class="perm-menu__code">{{ menu.code }}</code>
                <u-tag v-if="menu.hidden" size="small" type="info">隐藏</u-tag>
                <u-tag v-if="menu.super_admin_only" size="small" type="warning">仅超管</u-tag>
              </u-checkbox>
            </div>
            <div class="perm-features">
              <u-checkbox
                v-for="feat in menu.features"
                :key="feat.id"
                class="perm-check"
                :model-value="isChecked(feat.full_code)"
                :disabled="!isFeatureBindable(feat)"
                @change="(on) => toggleFeature(feat, on)"
              >
                <span>{{ feat.title || feat.code }}</span>
                <u-tag v-if="feat.super_admin_only" size="small" type="warning">仅超管</u-tag>
              </u-checkbox>
            </div>
          </div>
        </section>
      </div>
      <template #footer="{ close }">
        <u-button text :disabled="permBusy" @click="close()">取消</u-button>
        <u-button v-if="!isBuiltin" type="primary" :loading="permBusy" @click="savePerms">
          保存权限
        </u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.perm-hint {
  margin: 0;
  color: fn.use-var(text-color, second);
}

.perm-catalog {
  max-height: 480px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, large);
}

.perm-group__title {
  margin: 0 0 fn.use-var(gap, small);
  font-size: fn.use-var(font-size-main, default);
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.perm-menu {
  padding: fn.use-var(gap, small) 0;
  border-bottom: fn.use-var(border);
}

.perm-menu__head {
  margin-bottom: 6px;
  font-weight: 500;
}

.perm-menu__code {
  margin-left: 6px;
  color: fn.use-var(text-color, assist);
  font-size: 12px;
}

.perm-features {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 14px;
  padding-left: 22px;
}

.perm-check {
  font-size: 13px;
}
</style>
