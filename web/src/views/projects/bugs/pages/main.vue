<script setup lang="ts">
defineOptions({ name: "ProjectBugsWorkbench" });

import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { listProjects } from "@/api/projects";
import type { ProductProject, ProjectBug } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import {
  BUG_PRIORITY_OPTIONS,
  BUG_PRIORITY_TAG,
  BUG_SEVERITY_OPTIONS,
  BUG_SEVERITY_TAG,
  BUG_STATUS_OPTIONS,
  BUG_STATUS_TAG,
  bugPriorityLabel,
  bugSeverityLabel,
  bugStatusLabel,
  tagType,
} from "@/lib/tag";
import BugDetailDialog from "@/views/projects/bugs/components/bug-detail-dialog.vue";
import BugFormDialog from "@/views/projects/bugs/components/bug-form-dialog.vue";

const router = useRouter();
const { hasPermission } = usePermission();
const tableRef = useTemplateRef("table");

const canCreateBug = computed(() => hasPermission("project_bugs:create"));
const canUpdateBug = computed(() => hasPermission("project_bugs:update"));

const formDialogOpen = ref(false);
const detailDialogOpen = ref(false);
const currentBug = ref<ProjectBug | null>(null);
const detailProjectId = ref<number | undefined>(undefined);
const detailBugId = ref<number | undefined>(undefined);

const projectOptions = ref<{ label: string; value: number }[]>([]);
const query = reactive({
  keyword: "",
  project_id: undefined as number | undefined,
  status: undefined as string | undefined,
  severity: undefined as string | undefined,
  priority: undefined as string | undefined,
  assignee_id: undefined as number | undefined,
});

const columns = defineProTableColumns([
  { key: "title", name: "缺陷标题", minWidth: 200, sortable: true },
  { key: "project_name", name: "所属项目", width: 140 },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "severity", name: "严重程度", width: 100, align: "center" },
  { key: "priority", name: "优先级", width: 100, align: "center", sortable: true },
  { key: "assignee", name: "经办人", width: 120, align: "center" },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 140, align: "center", fixed: "right" },
]);

onMounted(async () => {
  try {
    const res = await listProjects({ page: 1, page_size: 100 });
    projectOptions.value = (res.items ?? []).map((p: ProductProject) => ({
      label: p.name,
      value: p.id,
    }));
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载项目选项失败");
  }
});

function handleCreate() {
  currentBug.value = null;
  formDialogOpen.value = true;
}

function handleViewBug(row: ProjectBug) {
  detailProjectId.value = row.project_id;
  detailBugId.value = row.id;
  detailDialogOpen.value = true;
}

function handleEditBug(row: ProjectBug) {
  currentBug.value = row;
  formDialogOpen.value = true;
}

function onDetailEdit(bug: ProjectBug) {
  currentBug.value = bug;
  formDialogOpen.value = true;
}

function onSaved() {
  void tableRef.value?.reload();
}

function onRefresh() {
  void tableRef.value?.reload();
}

function goToProject(projectId: number) {
  void router.push({
    path: `/project/projects/${projectId}`,
    query: { tab: "bugs" },
  });
}
</script>

<template>
  <div class="project-bugs-main">
    <ProTable
      ref="table"
      url="/projects/bugs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['project_id', 'status', 'severity', 'priority']"
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.project_id"
          :options="projectOptions"
          placeholder="全部项目"
          clearable
          style="width: 160px"
        />
        <u-select
          v-model="query.status"
          :options="BUG_STATUS_OPTIONS"
          placeholder="全部状态"
          clearable
          style="width: 120px"
        />
        <u-select
          v-model="query.severity"
          :options="BUG_SEVERITY_OPTIONS"
          placeholder="全部严重度"
          clearable
          style="width: 120px"
        />
        <u-select
          v-model="query.priority"
          :options="BUG_PRIORITY_OPTIONS"
          placeholder="全部优先级"
          clearable
          style="width: 120px"
        />
        <u-input v-model="query.keyword" placeholder="搜索标题/描述" style="width: 200px" />
      </template>

      <template #toolbar>
        <u-button v-if="canCreateBug" type="primary" @click.prevent="handleCreate">
          新建缺陷
        </u-button>
      </template>

      <template #column:title="{ rowData }">
        <a class="bug-title-link" href="#" @click.prevent="handleViewBug(rowData as ProjectBug)">
          {{ (rowData as ProjectBug).title }}
        </a>
      </template>

      <template #column:project_name="{ rowData }">
        <a
          v-if="(rowData as ProjectBug).project_id"
          class="project-link"
          href="#"
          @click.prevent="goToProject((rowData as ProjectBug).project_id)"
        >
          {{ (rowData as ProjectBug).project_name || `项目#${(rowData as ProjectBug).project_id}` }}
        </a>
        <span v-else>—</span>
      </template>

      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ProjectBug).status, BUG_STATUS_TAG)">
          {{ bugStatusLabel((rowData as ProjectBug).status) }}
        </u-tag>
      </template>

      <template #column:severity="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ProjectBug).severity, BUG_SEVERITY_TAG)">
          {{ bugSeverityLabel((rowData as ProjectBug).severity) }}
        </u-tag>
      </template>

      <template #column:priority="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ProjectBug).priority, BUG_PRIORITY_TAG)">
          {{ bugPriorityLabel((rowData as ProjectBug).priority) }}
        </u-tag>
      </template>

      <template #column:assignee="{ rowData }">
        {{
          (rowData as ProjectBug).assignee_name || (rowData as ProjectBug).assignee_username || "—"
        }}
      </template>

      <template #column:action="{ rowData }">
        <u-action-group :max="2">
          <u-action @run="handleViewBug(rowData as ProjectBug)">查看</u-action>
          <u-action v-if="canUpdateBug" @run="handleEditBug(rowData as ProjectBug)">编辑</u-action>
        </u-action-group>
      </template>
    </ProTable>

    <BugFormDialog
      v-model="formDialogOpen"
      :project-id="query.project_id"
      :bug="currentBug"
      :project-options="projectOptions"
      @saved="onSaved"
    />

    <BugDetailDialog
      v-model="detailDialogOpen"
      :project-id="detailProjectId"
      :bug-id="detailBugId"
      @edit="onDetailEdit"
      @refresh="onRefresh"
    />
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.project-bugs-main {
  height: 100%;
  min-height: 0;
}

.bug-title-link,
.project-link {
  color: fn.use-var(color, primary);
  text-decoration: none;
  cursor: pointer;

  &:hover {
    text-decoration: underline;
  }
}
</style>
