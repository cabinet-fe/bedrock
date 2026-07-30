<script setup lang="ts">
defineOptions({ name: "AiSkills" });

import { reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { o } from "@cat-kit/core";
import { saveBlob } from "@cat-kit/fe";
import { message } from "@veltra/desktop";

import { deleteSkill, downloadSkill, overwriteSkill, uploadSkill } from "@/api/ai";
import type { SkillPackage } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const router = useRouter();
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
  { key: "source", name: "来源", width: 100, align: "center" },
  { key: "visibility", name: "可见性", width: 100, align: "center" },
  { key: "package_digest", name: "Digest" },
  { key: "size_bytes", name: "大小" },
  { key: "action", name: "操作", width: 320, align: "center", fixed: "right" },
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

function openDetail(row: SkillPackage) {
  void router.push({ name: "ai-skill-detail", params: { id: String(row.id) } });
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
      <template #toolbar>
        <u-button
          v-if="hasPermission('ai_skills:create')"
          type="primary"
          @click.prevent="openUpload"
        >
          上传
        </u-button>
      </template>
      <template #column:source="{ rowData }">
        <u-tag
          size="small"
          :type="(rowData as SkillPackage).source === 'builtin' ? undefined : 'success'"
        >
          {{ (rowData as SkillPackage).source === "builtin" ? "内置" : "上传" }}
        </u-tag>
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
        <u-action-group :max="4" :loading="busyKey === (rowData as SkillPackage).id">
          <u-action @run="openDetail(rowData as SkillPackage)">
            {{ (rowData as SkillPackage).editable ? "编辑" : "查看" }}
          </u-action>
          <u-action
            v-if="hasPermission('ai_skills:download')"
            @run="onDownload(rowData as SkillPackage)"
          >
            下载
          </u-action>
          <u-action
            v-if="hasPermission('ai_skills:update') && (rowData as SkillPackage).editable"
            @run="openOverwrite(rowData as SkillPackage)"
          >
            覆盖
          </u-action>
          <u-action
            v-if="
              hasPermission('ai_skills:delete') && (rowData as SkillPackage).source !== 'builtin'
            "
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
      <template #prepend>
        <p class="upload-tip">请上传符合 Skill 包规范的 ZIP；覆盖时将替换原有包内容。</p>
      </template>
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="描述" field="description" />
      <u-select
        label="可见性"
        field="visibility"
        :options="[
          { label: '公开', value: 'public' },
          { label: '私有', value: 'private' },
        ]"
        tips="公开：有技能查看权限即可读；私有：仅创建者，或角色数据权限为全部时可读"
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
.upload-tip {
  margin: 0 0 4px;
  font-size: 13px;
  color: var(--u-color-text-secondary, #666);
  line-height: 1.5;
}
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
