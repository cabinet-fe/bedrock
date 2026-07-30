<script setup lang="ts">
defineOptions({ name: "Projects" });

import { reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";
import { useRouter } from "vue-router";

import { archiveProject, createProject, deleteProject, updateProject } from "@/api/projects";
import type { ProductProject } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";

import MembersPanel from "../../components/members-panel.vue";

const router = useRouter();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const tableRef = useTemplateRef("table");
const query = reactive({ keyword: "", status: "" });
const dialogOpen = ref(false);
const membersOpen = ref(false);
const membersProject = ref<ProductProject | null>(null);
const editing = ref<ProductProject | null>(null);
const form = reactive({
  name: "",
  slug: "",
  description: "",
  tags: "",
  is_public: false,
});

const columns = defineProTableColumns([
  { key: "name", name: "项目", sortable: true },
  { key: "slug", name: "标识" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "is_public", name: "公开", width: 80, align: "center" },
  { key: "tags", name: "标签" },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 380, align: "center", fixed: "right" },
]);

function openCreate() {
  editing.value = null;
  o(form).extend({ name: "", slug: "", description: "", tags: "", is_public: false });
  dialogOpen.value = true;
}

function openEdit(project: ProductProject) {
  editing.value = project;
  o(form).extend(project);
  dialogOpen.value = true;
}

function openMembers(project: ProductProject) {
  membersProject.value = project;
  membersOpen.value = true;
}

async function save() {
  try {
    const input = { ...form };
    if (editing.value) {
      await updateProject(editing.value.id, input);
      message.success("项目已更新");
    } else {
      await createProject(input);
      message.success("项目已创建");
    }
    dialogOpen.value = false;
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

const archive = bind(async (project: ProductProject) => {
  try {
    await archiveProject(project.id);
    message.success("项目已归档");
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "归档失败");
  }
});

const remove = bind(async (project: ProductProject) => {
  try {
    await deleteProject(project.id);
    message.success("项目已解散");
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "解散失败");
  }
});

function openProject(project: ProductProject) {
  void router.push({ name: "project-detail", params: { id: project.id } });
}

function splitTags(raw?: string | null): string[] {
  if (!raw?.trim()) return [];
  return raw
    .split(/[,，]/)
    .map((t) => t.trim())
    .filter(Boolean);
}

async function onOwnerTransferred() {
  await tableRef.value?.reload();
}
</script>

<template>
  <div>
    <ProTable
      ref="table"
      url="/projects"
      :query="query"
      :columns="columns"
      pagination
      :auto-query-fields="['status']"
    >
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称、标识或标签" style="width: 240px" />
        <u-select
          v-model="query.status"
          placeholder="全部状态"
          :options="[
            { label: '全部状态', value: '' },
            { label: '活跃', value: 'active' },
            { label: '已归档', value: 'archived' },
          ]"
          style="width: 130px"
        />
      </template>
      <template #toolbar>
        <u-button
          v-if="hasPermission('project_projects:create')"
          type="primary"
          @click.prevent="openCreate"
        >
          新建项目
        </u-button>
      </template>
      <template #column:name="{ rowData }">
        <u-action @run="openProject(rowData as ProductProject)">
          {{ (rowData as ProductProject).name }}
        </u-action>
      </template>
      <template #column:is_public="{ rowData }">
        {{ (rowData as ProductProject).is_public ? "是" : "否" }}
      </template>
      <template #column:status="{ rowData }">
        <u-tag
          size="small"
          :type="(rowData as ProductProject).status === 'archived' ? 'warning' : 'success'"
        >
          {{ (rowData as ProductProject).status === "archived" ? "已归档" : "活跃" }}
        </u-tag>
      </template>
      <template #column:tags="{ rowData }">
        <span class="tag-cell">
          <u-tag v-for="tag in splitTags((rowData as ProductProject).tags)" :key="tag" size="small">
            {{ tag }}
          </u-tag>
        </span>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="5" :loading="busyKey === (rowData as ProductProject).id">
          <u-action @run="openProject(rowData as ProductProject)">进入</u-action>
          <u-action @run="openMembers(rowData as ProductProject)">成员</u-action>
          <u-action
            v-if="(rowData as ProductProject).permissions?.update"
            @run="openEdit(rowData as ProductProject)"
          >
            编辑
          </u-action>
          <u-action
            v-if="
              (rowData as ProductProject).permissions?.archive &&
              (rowData as ProductProject).status === 'active'
            "
            need-confirm
            @run="archive(rowData as ProductProject)"
          >
            归档
          </u-action>
          <u-action
            v-if="(rowData as ProductProject).permissions?.delete"
            need-confirm
            type="danger"
            @run="remove(rowData as ProductProject)"
          >
            解散
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑项目' : '新建项目'"
      :model="form"
      label-width="100px"
      style="width: 560px"
      @submit="save"
    >
      <template #prepend>
        <p class="slug-tip">标识创建后通常作为稳定引用，请使用字母、数字与连字符。</p>
      </template>
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input
        label="标识"
        field="slug"
        placeholder="仅字母、数字、连字符，如 my-project"
        :pattern="/^[a-zA-Z0-9\- ]*$/"
        :rules="{ required: '必填' }"
      />
      <u-input label="标签" field="tags" placeholder="逗号分隔" />
      <u-switch
        label="公开"
        field="is_public"
        tips="开启后，仅自己数据权限的用户也可读取该项目（含需求/文档）；不授予写权限"
      />
      <u-textarea label="描述" field="description" :rows="4" />
    </FormDialog>

    <u-dialog
      v-model="membersOpen"
      :title="membersProject ? `项目成员 · ${membersProject.name}` : '项目成员'"
      style="width: 800px"
    >
      <MembersPanel
        v-if="membersOpen && membersProject"
        :project="membersProject"
        height="420px"
        @owner-transferred="onOwnerTransferred"
      />
      <template #footer="{ close }">
        <u-button text @click="close()">关闭</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped>
.slug-tip {
  margin: 0 0 4px;
  font-size: 13px;
  color: var(--u-color-text-secondary, #666);
  line-height: 1.5;
}
.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
