<script setup lang="ts">
defineOptions({ name: "ProjectBugsPanel" });

import { computed, onMounted, reactive, ref, useTemplateRef, watch } from "vue";
import { useRoute } from "vue-router";
import { message } from "@veltra/desktop";

import {
  deleteProjectBug,
  getProjectKanban,
  listProjectBugs,
  transitionProjectIssueStatus,
} from "@/api/projects";
import type {
  KanbanBoard,
  ProductProject,
  ProjectBug,
  ProjectIssue,
  ProjectRole,
} from "@/api/types";
import KanbanBoardView from "@/views/projects/components/kanban-board.vue";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
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
import { useRepositoryStore } from "@/stores/repositories";
import BugDetailDialog from "@/views/projects/bugs/components/bug-detail-dialog.vue";
import BugFormDialog from "@/views/projects/bugs/components/bug-form-dialog.vue";

const props = defineProps<{
  project: ProductProject;
  projectRole?: ProjectRole;
  manageAll: boolean;
}>();

const emit = defineEmits<{
  (e: "create"): void;
  (e: "view", bug: ProjectBug): void;
  (e: "edit", bug: ProjectBug): void;
}>();

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const repoStore = useRepositoryStore();
const route = useRoute();
const tableRef = useTemplateRef("table");

const formDialogOpen = ref(false);
const detailDialogOpen = ref(false);
const editingBug = ref<ProjectBug | null>(null);
const detailBugId = ref<number | undefined>(undefined);
const viewMode = ref<"kanban" | "table">("kanban");
const includeTerminal = ref(false);
const board = ref<KanbanBoard | null>(null);
const boardLoading = ref(false);

const stats = reactive({
  total: 0,
  open: 0,
  inProgress: 0,
  resolved: 0,
  criticalHigh: 0,
});

const query = reactive({
  keyword: "",
  status: undefined as string | undefined,
  severity: undefined as string | undefined,
  priority: undefined as string | undefined,
});

const canEditProjectContent = computed(
  () =>
    props.manageAll ||
    props.projectRole === "owner" ||
    props.projectRole === "admin" ||
    props.projectRole === "member",
);
const canAdminProjectContent = computed(
  () => props.manageAll || props.projectRole === "owner" || props.projectRole === "admin",
);
const canCreateBug = computed(
  () => hasPermission("project_bugs:create") && canEditProjectContent.value,
);
const canUpdateBug = computed(
  () => hasPermission("project_bugs:update") && canEditProjectContent.value,
);
const canDeleteBug = computed(
  () => hasPermission("project_bugs:delete") && canAdminProjectContent.value,
);

const columns = defineProTableColumns([
  { key: "title", name: "缺陷标题", minWidth: 200, sortable: true },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "severity", name: "严重程度", width: 100, align: "center" },
  { key: "priority", name: "优先级", width: 100, align: "center", sortable: true },
  { key: "assignee", name: "经办人", width: 120, align: "center" },
  { key: "repo_branch", name: "关联仓库/分支", width: 160 },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 160, align: "center", fixed: "right" },
]);

async function loadStats() {
  try {
    const res = await listProjectBugs(props.project.id, { page: 1, page_size: 100 });
    const items = res.items ?? [];
    stats.total = res.total ?? items.length;
    stats.open = items.filter((b) => b.status === "open").length;
    stats.inProgress = items.filter((b) => b.status === "in_progress").length;
    stats.resolved = items.filter((b) => b.status === "resolved").length;
    stats.criticalHigh = items.filter(
      (b) => b.severity === "high" || b.severity === "critical",
    ).length;
  } catch {
    /* ignore stats error */
  }
}

function toggleStatFilter(type: "all" | "open" | "in_progress" | "resolved" | "critical") {
  if (type === "all") {
    query.status = undefined;
    query.severity = undefined;
  } else if (type === "critical") {
    query.status = undefined;
    query.severity = query.severity === "critical" ? undefined : "critical";
  } else {
    query.severity = undefined;
    query.status = query.status === type ? undefined : type;
  }
}

const removeBug = bind(async (bug: ProjectBug) => {
  try {
    await deleteProjectBug(props.project.id, bug.id);
    message.success("缺陷已删除");
    refreshAll();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除缺陷失败");
  }
});

function handleCreate() {
  editingBug.value = null;
  formDialogOpen.value = true;
  emit("create");
}

function handleView(bug: ProjectBug) {
  detailBugId.value = bug.id;
  detailDialogOpen.value = true;
  emit("view", bug);
}

function handleEdit(bug: ProjectBug) {
  editingBug.value = bug;
  formDialogOpen.value = true;
  emit("edit", bug);
}

function onDetailEdit(bug: ProjectBug) {
  editingBug.value = bug;
  formDialogOpen.value = true;
}

async function onBugSaved() {
  refreshAll();
}

async function onDetailRefresh() {
  refreshAll();
}

function resolveRepoBranch(bug: ProjectBug): string {
  const repoName =
    bug.repository_name ||
    (bug.repository_id
      ? (repoStore.nameMap.get(bug.repository_id) ?? `#${bug.repository_id}`)
      : "");
  if (repoName && bug.branch) return `${repoName} (${bug.branch})`;
  if (repoName) return repoName;
  if (bug.branch) return bug.branch;
  return "—";
}

async function loadBoard() {
  boardLoading.value = true;
  try {
    board.value = await getProjectKanban({
      projectID: props.project.id,
      type: "bug",
      includeTerminal: includeTerminal.value,
      keyword: query.keyword || undefined,
    });
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载缺陷看板失败");
  } finally {
    boardLoading.value = false;
  }
}

function refreshAll() {
  if (viewMode.value === "kanban") {
    void loadBoard();
  } else {
    void tableRef.value?.reload();
  }
  void loadStats();
}

function handleIssueView(issue: ProjectIssue) {
  detailBugId.value = issue.id;
  detailDialogOpen.value = true;
}

async function onBoardTransition(issue: ProjectIssue, status: string) {
  try {
    await transitionProjectIssueStatus(props.project.id, issue.id, { status });
    message.success("缺陷状态已流转");
    await loadBoard();
    void loadStats();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "状态流转失败");
  }
}

watch(
  () => [includeTerminal.value, viewMode.value, query.keyword] as const,
  () => {
    if (viewMode.value === "kanban") {
      void loadBoard();
    }
  },
);

onMounted(() => {
  void loadBoard();
  void loadStats();
  void repoStore.load();
  if (route.query.bug_id) {
    const bId = Number(route.query.bug_id);
    if (Number.isFinite(bId) && bId > 0) {
      detailBugId.value = bId;
      detailDialogOpen.value = true;
    }
  }
});
</script>

<template>
  <div class="bugs-panel">
    <!-- 缺陷状态统计卡片 -->
    <div class="bugs-panel__stats">
      <button
        type="button"
        class="bugs-panel__stat"
        :class="{ 'bugs-panel__stat--active': !query.status && !query.severity }"
        @click="toggleStatFilter('all')"
      >
        <span class="bugs-panel__stat-label">全部缺陷</span>
        <span class="bugs-panel__stat-value">{{ stats.total }}</span>
      </button>
      <button
        type="button"
        class="bugs-panel__stat"
        :class="{ 'bugs-panel__stat--active': query.status === 'open' }"
        @click="toggleStatFilter('open')"
      >
        <span class="bugs-panel__stat-label">待处理</span>
        <span class="bugs-panel__stat-value text-primary">{{ stats.open }}</span>
      </button>
      <button
        type="button"
        class="bugs-panel__stat"
        :class="{ 'bugs-panel__stat--active': query.status === 'in_progress' }"
        @click="toggleStatFilter('in_progress')"
      >
        <span class="bugs-panel__stat-label">处理中</span>
        <span class="bugs-panel__stat-value text-warning">{{ stats.inProgress }}</span>
      </button>
      <button
        type="button"
        class="bugs-panel__stat"
        :class="{ 'bugs-panel__stat--active': query.status === 'resolved' }"
        @click="toggleStatFilter('resolved')"
      >
        <span class="bugs-panel__stat-label">已解决</span>
        <span class="bugs-panel__stat-value text-success">{{ stats.resolved }}</span>
      </button>
      <button
        type="button"
        class="bugs-panel__stat"
        :class="{ 'bugs-panel__stat--active': query.severity === 'critical' }"
        @click="toggleStatFilter('critical')"
      >
        <span class="bugs-panel__stat-label">严重/致命</span>
        <span class="bugs-panel__stat-value text-danger">{{ stats.criticalHigh }}</span>
      </button>
    </div>

    <div class="bugs-panel__toolbar">
      <u-radio-group
        v-model="viewMode"
        type="button"
        size="small"
        :items="[
          { label: '看板', value: 'kanban' },
          { label: '表格', value: 'table' },
        ]"
      />
      <u-checkbox v-if="viewMode === 'kanban'" v-model="includeTerminal">
        显示已关闭/已拒绝
      </u-checkbox>
    </div>

    <KanbanBoardView
      v-show="viewMode === 'kanban'"
      class="bugs-panel__kanban"
      :board="board"
      :draggable="canUpdateBug"
      :show-severity="true"
      :loading="boardLoading"
      @card-click="handleIssueView"
      @transition="onBoardTransition"
    />

    <!-- 缺陷列表表格 -->
    <ProTable
      v-if="viewMode === 'table'"
      ref="table"
      :url="`/projects/${project.id}/bugs`"
      :query="query"
      :columns="columns"
      :auto-query-fields="['status', 'severity', 'priority']"
      pagination
    >
      <template #filters>
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
        <a class="bug-title-link" href="#" @click.prevent="handleView(rowData as ProjectBug)">
          {{ (rowData as ProjectBug).title }}
        </a>
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

      <template #column:repo_branch="{ rowData }">
        {{ resolveRepoBranch(rowData as ProjectBug) }}
      </template>

      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as ProjectBug).id">
          <u-action @run="handleView(rowData as ProjectBug)">详情</u-action>
          <u-action v-if="canUpdateBug" @run="handleEdit(rowData as ProjectBug)">编辑</u-action>
          <u-action
            v-if="canDeleteBug"
            need-confirm
            type="danger"
            @run="removeBug(rowData as ProjectBug)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <BugFormDialog
      v-model="formDialogOpen"
      :project-id="project.id"
      :bug="editingBug"
      @saved="onBugSaved"
    />

    <BugDetailDialog
      v-model="detailDialogOpen"
      :project-id="project.id"
      :bug-id="detailBugId"
      :project-role="projectRole"
      :manage-all="manageAll"
      @edit="onDetailEdit"
      @refresh="onDetailRefresh"
    />
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.bugs-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.bugs-panel__toolbar {
  display: flex;
  align-items: center;
  gap: 16px;
}

.bugs-panel__kanban {
  flex: 1;
  min-height: 320px;
}

.bugs-panel__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}

.bugs-panel__stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 14px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: all 0.15s ease;

  &:hover,
  &--active {
    border-color: fn.use-var(color, primary);
    background: fn.use-var(bg-color, hover);
  }
}

.bugs-panel__stat-label {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.bugs-panel__stat-value {
  font-size: 20px;
  font-weight: 600;
  color: fn.use-var(text-color, title);

  &.text-primary {
    color: fn.use-var(color, primary);
  }

  &.text-warning {
    color: fn.use-var(color, warning);
  }

  &.text-success {
    color: fn.use-var(color, success);
  }

  &.text-danger {
    color: fn.use-var(color, danger);
  }
}

.bug-title-link {
  color: fn.use-var(color, primary);
  text-decoration: none;
  cursor: pointer;

  &:hover {
    text-decoration: underline;
  }
}
</style>
