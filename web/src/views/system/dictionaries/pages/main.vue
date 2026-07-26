<script setup lang="ts">
defineOptions({ name: "SystemDictionaries" });

import { reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import { createDictionary, deleteDictionary, getDictionary, updateDictionary } from "@/api/system";
import type { DictItem, Dictionary } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const listRef = useTemplateRef("list");
const dialogOpen = ref(false);
const editing = ref<Dictionary | null>(null);
const form = reactive({
  name: "",
  code: "",
  description: "",
  items: [] as DictItem[],
});

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "items", title: "字典项" },
];

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "code", name: "编码" },
  { key: "description", name: "描述" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

const openEdit = bind(async (row: Dictionary) => {
  try {
    const full = await getDictionary(row.id);
    editing.value = full;
    o(form).extend(full);
    form.items = (full.items ?? []).map((it) => ({
      label: it.label,
      value: it.value,
      sort_order: it.sort_order ?? 0,
      enabled: it.enabled !== false,
    }));
    dialogOpen.value = true;
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  }
});

function addItem() {
  form.items.push({
    label: "",
    value: "",
    sort_order: form.items.length,
    enabled: true,
  });
}

function removeItem(idx: number) {
  form.items.splice(idx, 1);
}

async function save() {
  try {
    const items = form.items
      .map((it, i) => ({
        label: it.label.trim(),
        value: it.value.trim(),
        sort_order: it.sort_order ?? i,
        enabled: it.enabled !== false,
      }))
      .filter((it) => it.label && it.value);

    if (editing.value) {
      await updateDictionary(editing.value.id, {
        name: form.name,
        description: form.description,
        items,
      });
      message.success("已更新");
    } else {
      await createDictionary({
        name: form.name,
        code: form.code,
        description: form.description,
        items,
      });
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Dictionary) => {
  try {
    await deleteDictionary(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/dictionaries" :columns="columns" pagination>
      <template #filters>
        <u-button
          v-if="hasPermission('system_dictionaries:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建字典
        </u-button>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as Dictionary).id">
          <u-action
            v-if="hasPermission('system_dictionaries:update')"
            @run="openEdit(rowData as Dictionary)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('system_dictionaries:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as Dictionary)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑字典' : '新建字典'"
      :model="form"
      :groups="formGroups"
      label-width="72px"
      style="width: 640px"
      @submit="save"
    >
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="编码" field="code" :disabled="!!editing" :rules="{ required: '必填' }" />
        <u-input label="描述" field="description" />
      </template>
      <template #group:items>
        <div class="items-toolbar">
          <u-button size="small" @click="addItem">添加项</u-button>
        </div>
        <div v-if="!form.items.length" class="items-empty">暂无字典项</div>
        <div v-for="(it, idx) in form.items" :key="idx" class="item-row">
          <u-input v-model="it.label" placeholder="标签" style="flex: 1" />
          <u-input v-model="it.value" placeholder="值" style="flex: 1" />
          <u-switch v-model="it.enabled" />
          <u-button size="small" @click="removeItem(idx)">删</u-button>
        </div>
      </template>
    </FormDialog>
  </div>
</template>

<style scoped>
.items-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 8px;
}
.items-empty {
  color: #6b7280;
  font-size: 13px;
  margin-bottom: 8px;
}
.item-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}
</style>
