<script setup lang="ts">
defineOptions({ name: "ProjectIterationsPanel" });

import { computed, onMounted, reactive, ref } from "vue";
import { message } from "@veltra/desktop";

import {
  createProjectIteration,
  deleteProjectIteration,
  getIterationBurndown,
  listProjectIterations,
  updateProjectIteration,
} from "@/api/projects";
import type { BurndownChart, ProductProject, ProjectIteration, ProjectRole } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { usePermission } from "@/composables/use-permission";
import { tagType, type TagType } from "@/lib/tag";
import BurndownChartView from "@/views/projects/components/burndown-chart.vue";

const ITERATION_STATUS_TAG: Record<string, TagType> = {
  planned: "info",
  active: "primary",
  closed: undefined,
};

const ITERATION_STATUS_LABEL: Record<string, string> = {
  planned: "未开始",
  active: "进行中",
  closed: "已关闭",
};

const props = defineProps<{
  project: ProductProject;
  projectRole?: ProjectRole;
  manageAll: boolean;
}>();

const { hasPermission } = usePermission();
const iterations = ref<ProjectIteration[]>([]);
const loading = ref(false);
const dialogOpen = ref(false);
const editing = ref<ProjectIteration | null>(null);
const burndown = ref<BurndownChart | null>(null);
const burndownIterationID = ref<number | undefined>(undefined);

const form = reactive({
  name: "",
  goal: "",
  start_date: "",
  end_date: "",
});

const canManage = computed(
  () =>
    props.manageAll ||
    props.projectRole === "owner" ||
    props.projectRole === "admin" ||
    hasPermission("project_projects:manage_all"),
);

const formGroups = [{ key: "meta", title: "迭代信息" }];

async function load() {
  loading.value = true;
  try {
    iterations.value = await listProjectIterations(props.project.id);
    const active = iterations.value.find((item) => item.status === "active");
    if (active) {
      await showBurndown(active);
    } else {
      burndown.value = null;
      burndownIterationID.value = undefined;
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载迭代失败");
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  form.name = "";
  form.goal = "";
  form.start_date = "";
  form.end_date = "";
  dialogOpen.value = true;
}

function openEdit(iteration: ProjectIteration) {
  editing.value = iteration;
  form.name = iteration.name;
  form.goal = iteration.goal ?? "";
  form.start_date = iteration.start_date ?? "";
  form.end_date = iteration.end_date ?? "";
  dialogOpen.value = true;
}

async function save() {
  try {
    if (editing.value) {
      await updateProjectIteration(props.project.id, editing.value.id, { ...form });
      message.success("迭代已更新");
    } else {
      await createProjectIteration(props.project.id, { ...form });
      message.success("迭代已创建");
    }
    dialogOpen.value = false;
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存迭代失败");
  }
}

async function changeStatus(iteration: ProjectIteration, status: ProjectIteration["status"]) {
  const tips: Record<string, string> = {
    active: "启动该迭代?",
    closed: "关闭迭代将把其中工作项移回 Backlog,确认关闭?",
  };
  if (tips[status] && !window.confirm(tips[status])) return;
  try {
    await updateProjectIteration(props.project.id, iteration.id, { status });
    message.success("迭代状态已更新");
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "迭代状态更新失败");
  }
}

async function removeIteration(iteration: ProjectIteration) {
  if (!window.confirm(`确认删除迭代「${iteration.name}」?`)) return;
  try {
    await deleteProjectIteration(props.project.id, iteration.id);
    message.success("迭代已删除");
    await load();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除迭代失败");
  }
}

async function showBurndown(iteration: ProjectIteration) {
  try {
    burndown.value = await getIterationBurndown(props.project.id, iteration.id);
    burndownIterationID.value = iteration.id;
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载燃尽图失败");
  }
}

function iterationRange(iteration: ProjectIteration): string {
  const start = iteration.start_date || "—";
  const end = iteration.end_date || "—";
  return `${start} ~ ${end}`;
}

function issueTotal(iteration: ProjectIteration): number {
  return Object.values(iteration.issue_counts ?? {}).reduce((sum, count) => sum + count, 0);
}

onMounted(() => void load());
</script>

<template>
  <section class="iterations-panel">
    <div class="iterations-toolbar">
      <span class="iterations-hint">
        迭代内工作项在需求/缺陷看板或详情中分配;未分配迭代的工作项位于 Backlog。
      </span>
      <div class="iterations-spacer" />
      <u-button v-if="canManage" type="primary" @click.prevent="openCreate">新建迭代</u-button>
    </div>

    <div v-if="loading" class="iterations-empty">加载中…</div>
    <div v-else-if="iterations.length === 0" class="iterations-empty">
      暂无迭代,点击「新建迭代」开始排期
    </div>
    <template v-else>
      <div class="iterations-list">
        <div
          v-for="iteration in iterations"
          :key="iteration.id"
          class="iteration-card"
          :class="{ 'is-active-chart': burndownIterationID === iteration.id }"
        >
          <div class="iteration-head">
            <strong class="iteration-name">{{ iteration.name }}</strong>
            <u-tag size="small" :type="tagType(iteration.status, ITERATION_STATUS_TAG)">
              {{ ITERATION_STATUS_LABEL[iteration.status] ?? iteration.status }}
            </u-tag>
          </div>
          <p class="iteration-goal">{{ iteration.goal || "暂无目标" }}</p>
          <div class="iteration-meta">
            <span>{{ iterationRange(iteration) }}</span>
            <span>工作项 {{ issueTotal(iteration) }}</span>
          </div>
          <div class="iteration-actions">
            <u-action v-if="iteration.status !== 'closed'" @run="showBurndown(iteration)">
              燃尽图
            </u-action>
            <u-action
              v-if="canManage && iteration.status === 'planned'"
              @run="changeStatus(iteration, 'active')"
            >
              启动
            </u-action>
            <u-action
              v-if="canManage && iteration.status === 'active'"
              @run="changeStatus(iteration, 'closed')"
            >
              关闭
            </u-action>
            <u-action v-if="canManage" @run="openEdit(iteration)">编辑</u-action>
            <u-action
              v-if="canManage && iteration.status !== 'active'"
              type="danger"
              need-confirm
              @run="removeIteration(iteration)"
            >
              删除
            </u-action>
          </div>
        </div>
      </div>

      <div v-if="burndown" class="iteration-burndown">
        <strong>燃尽图</strong>
        <BurndownChartView :chart="burndown" />
      </div>
    </template>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑迭代' : '新建迭代'"
      :model="form"
      :groups="formGroups"
      label-width="90px"
      style="width: 520px"
      @submit="save"
    >
      <template #group:meta>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="开始日期" field="start_date" placeholder="YYYY-MM-DD" />
        <u-input label="结束日期" field="end_date" placeholder="YYYY-MM-DD" />
        <u-textarea label="目标" field="goal" :rows="3" span="full" />
      </template>
    </FormDialog>
  </section>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.iterations-panel {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  gap: fn.use-var(spacing, 3);
}

.iterations-toolbar {
  display: flex;
  align-items: center;
  gap: fn.use-var(spacing, 3);
}

.iterations-hint {
  font-size: 12px;
  color: fn.use-var(color, text-secondary);
}

.iterations-spacer {
  flex: 1;
}

.iterations-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  color: fn.use-var(color, text-secondary);
}

.iterations-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: fn.use-var(spacing, 3);
}

.iteration-card {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(spacing, 2);
  border: 1px solid fn.use-var(color, border-light);
  border-radius: fn.use-var(radius, md);
  padding: fn.use-var(spacing, 3);

  &.is-active-chart {
    border-color: fn.use-var(color, primary);
  }
}

.iteration-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.iteration-goal {
  margin: 0;
  font-size: 13px;
  color: fn.use-var(color, text-secondary);
}

.iteration-meta {
  display: flex;
  gap: fn.use-var(spacing, 3);
  font-size: 12px;
  color: fn.use-var(color, text-secondary);
}

.iteration-actions {
  display: flex;
  gap: fn.use-var(spacing, 2);
  margin-top: auto;
}

.iteration-burndown {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(spacing, 2);
  border-top: 1px solid fn.use-var(color, border-light);
  padding-top: fn.use-var(spacing, 3);
}
</style>
