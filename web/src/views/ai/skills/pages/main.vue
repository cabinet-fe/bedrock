<script setup lang="ts">
defineOptions({ name: "AiSkills" });

import { reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { saveBlob } from "@cat-kit/fe";
import { message } from "@veltra/desktop";

import { deleteSkill, downloadSkill, overwriteSkill, uploadSkill } from "@/api/ai";
import type { SkillPackage } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const table = useTemplateRef("table");
const dialogOpen = ref(false);
const overwriteID = ref<number | null>(null);
const file = ref<File | null>(null);
const form = reactive({
  name: "",
  description: "",
  visibility: "private" as "public" | "private",
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "visibility", name: "可见性", width: 100, align: "center" },
  { key: "package_digest", name: "Digest" },
  { key: "size_bytes", name: "大小" },
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
]);

function openUpload() {
  overwriteID.value = null;
  file.value = null;
  dialogOpen.value = true;
}

function openOverwrite(row: SkillPackage) {
  overwriteID.value = row.id;
  o(form).extend(row);
  file.value = null;
  dialogOpen.value = true;
}

function onFilePick(files: File[]) {
  file.value = files[0] ?? null;
}

async function save() {
  if (!file.value) {
    message.error("请选择 ZIP（需含 SKILL.md）");
    return;
  }
  const body = new FormData();
  body.append("file", file.value);
  body.append("name", form.name);
  body.append("description", form.description);
  body.append("visibility", form.visibility);
  try {
    if (overwriteID.value) {
      await overwriteSkill(overwriteID.value, body);
    } else {
      await uploadSkill(body);
    }
    dialogOpen.value = false;
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "上传失败");
  }
}

const onDownload = bind(async (row: SkillPackage) => {
  try {
    saveBlob(await downloadSkill(row.id), `${row.name}.zip`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "下载失败");
  }
});

const remove = bind(async (row: SkillPackage) => {
  try {
    await deleteSkill(row.id);
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="table" url="/skills" pagination :columns="columns">
      <template #filters>
        <u-button
          v-if="hasPermission('ai_skills:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openUpload"
        >
          上传
        </u-button>
      </template>
      <template #column:visibility="{ rowData }">
        <u-tag
          size="small"
          :type="(rowData as SkillPackage).visibility === 'public' ? 'success' : undefined"
        >
          {{ (rowData as SkillPackage).visibility === "public" ? "公开" : "私有" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as SkillPackage).id">
          <u-action
            v-if="hasPermission('ai_skills:download')"
            @run="onDownload(rowData as SkillPackage)"
          >
            下载
          </u-action>
          <u-action
            v-if="hasPermission('ai_skills:update')"
            @run="openOverwrite(rowData as SkillPackage)"
          >
            覆盖
          </u-action>
          <u-action
            v-if="hasPermission('ai_skills:delete')"
            type="danger"
            @run="remove(rowData as SkillPackage)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="overwriteID ? '覆盖 Skill' : '上传 Skill'"
      :model="form"
      label-width="90px"
      style="width: 480px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="描述" field="description" />
      <u-select
        label="可见性"
        field="visibility"
        :options="[
          { label: 'public', value: 'public' },
          { label: 'private', value: 'private' },
        ]"
        :rules="{ required: '必填' }"
      />
      <u-form-item label="ZIP">
        <div class="zip-field">
          <u-file-picker accept=".zip,application/zip" @pick="onFilePick">
            <u-button>{{ file ? "重新选择" : "选择 ZIP" }}</u-button>
          </u-file-picker>
          <span v-if="file" class="zip-name">{{ file.name }}</span>
        </div>
      </u-form-item>
    </FormDialog>
  </div>
</template>

<style scoped lang="scss">
.zip-field {
  display: flex;
  align-items: center;
  gap: 8px;
}
.zip-name {
  font-size: 13px;
  color: var(--u-color-text-secondary, #666);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
