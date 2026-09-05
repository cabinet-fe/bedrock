<script setup lang="ts">
defineOptions({ name: "HomePage" });

import { computed, ref } from "vue";
import { message } from "@veltra/desktop";
import { Edit, Setting } from "@veltra/icons/normal";
import { useRouter } from "vue-router";

import {
  getAgentRunSummary,
  getBuildSummary,
  getDashboardLayout,
  getMyProjects,
  getPipelineRunSummary,
  getScriptRunSummary,
  getSystemInfo,
  getSystemStatus,
  getTaskOverview,
  saveDashboardLayout,
} from "@/api/dashboard";
import type {
  AgentRunSummary,
  BuildSummary,
  DashboardCardID,
  DashboardCardLayout,
  MyProject,
  PipelineRunSummary,
  ScriptRunSummary,
  SystemInfo,
  SystemStatus,
  TaskOverview,
} from "@/api/types";
import DashboardGrid, { ensureCardGeometry, resetCardGeometry } from "@/components/dashboard-grid";
import DashboardRunningDialog, {
  type RunningDialogKind,
} from "@/components/dashboard-running-dialog";
import { useDashboardWs, type DashboardRunType } from "@/composables/use-dashboard-ws";

const router = useRouter();
const layout = ref<DashboardCardLayout[]>([]);
const editSnapshot = ref<DashboardCardLayout[]>([]);
const manageDraft = ref<DashboardCardLayout[]>([]);
const editing = ref(false);
const manageOpen = ref(false);
const saving = ref(false);
const loading = ref(true);
const buildSummary = ref<BuildSummary | null>(null);
const agentRunSummary = ref<AgentRunSummary | null>(null);
const scriptRunSummary = ref<ScriptRunSummary | null>(null);
const pipelineRunSummary = ref<PipelineRunSummary | null>(null);
const taskOverview = ref<TaskOverview | null>(null);
const myProjects = ref<MyProject[] | null>(null);
const systemInfo = ref<SystemInfo | null>(null);
const systemStatus = ref<SystemStatus | null>(null);
const runningDialogOpen = ref(false);
const runningDialogKind = ref<RunningDialogKind>("build");

const visibleCards = computed(() => layout.value.filter((card) => card.visible));

const cardTitles: Record<DashboardCardID, string> = {
  build_summary: "构建摘要",
  agent_run_summary: "智能体运行摘要",
  system_info: "系统信息",
  system_status: "系统状态",
  script_run_summary: "脚本运行摘要",
  pipeline_run_summary: "流水线运行摘要",
  cicd_task_overview: "任务概览",
  my_projects: "我的项目",
};

function cloneCards(cards: DashboardCardLayout[]): DashboardCardLayout[] {
  return cards.map((card) => ({ ...card }));
}

function isVisible(id: DashboardCardID): boolean {
  return layout.value.some((card) => card.id === id && card.visible);
}

function showLoadError(error: unknown) {
  message.error(error instanceof Error ? error.message : "加载失败");
}

async function refreshBuildSummary() {
  if (!isVisible("build_summary")) return;
  try {
    buildSummary.value = await getBuildSummary();
  } catch (error) {
    showLoadError(error);
  }
}

async function refreshScriptRunSummary() {
  if (!isVisible("script_run_summary")) return;
  try {
    scriptRunSummary.value = await getScriptRunSummary();
  } catch (error) {
    showLoadError(error);
  }
}

async function refreshPipelineRunSummary() {
  if (!isVisible("pipeline_run_summary")) return;
  try {
    pipelineRunSummary.value = await getPipelineRunSummary();
  } catch (error) {
    showLoadError(error);
  }
}

function onRunChanged(runType: DashboardRunType) {
  if (runType === "build") void refreshBuildSummary();
  if (runType === "script") void refreshScriptRunSummary();
  if (runType === "pipeline") void refreshPipelineRunSummary();
}

async function refreshStatus() {
  if (!isVisible("system_status")) return;
  try {
    systemStatus.value = await getSystemStatus();
  } catch (error) {
    showLoadError(error);
  }
}

async function loadCardData() {
  const requests: Promise<void>[] = [];
  if (isVisible("build_summary")) {
    requests.push(
      getBuildSummary()
        .then((result) => {
          buildSummary.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("agent_run_summary")) {
    requests.push(
      getAgentRunSummary()
        .then((result) => {
          agentRunSummary.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("script_run_summary")) {
    requests.push(
      getScriptRunSummary()
        .then((result) => {
          scriptRunSummary.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("pipeline_run_summary")) {
    requests.push(
      getPipelineRunSummary()
        .then((result) => {
          pipelineRunSummary.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("cicd_task_overview")) {
    requests.push(
      getTaskOverview()
        .then((result) => {
          taskOverview.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("my_projects")) {
    requests.push(
      getMyProjects()
        .then((result) => {
          myProjects.value = result;
        })
        .catch(showLoadError),
    );
  }
  if (isVisible("system_info")) {
    requests.push(
      getSystemInfo()
        .then((result) => {
          systemInfo.value = result;
        })
        .catch(showLoadError),
    );
  }
  await Promise.all(requests);
}

async function loadDashboard() {
  loading.value = true;
  try {
    const result = await getDashboardLayout();
    layout.value = ensureCardGeometry(result.cards);
    await loadCardData();
    window.setTimeout(() => void refreshStatus(), 0);
  } catch (error) {
    showLoadError(error);
  } finally {
    loading.value = false;
  }
}

function enterEdit() {
  editSnapshot.value = cloneCards(layout.value);
  editing.value = true;
}

function cancelEdit() {
  layout.value = cloneCards(editSnapshot.value);
  editing.value = false;
}

function resetToDefault() {
  layout.value = resetCardGeometry(layout.value);
  message.info("已重置为默认紧凑布局（点击保存后生效）");
}

async function saveEdit() {
  saving.value = true;
  try {
    const saved = await saveDashboardLayout({ cards: ensureCardGeometry(layout.value) });
    layout.value = ensureCardGeometry(saved.cards);
    editing.value = false;
    await loadCardData();
    void refreshStatus();
    message.success("仪表盘布局已保存");
  } catch (error) {
    showLoadError(error);
  } finally {
    saving.value = false;
  }
}

function openManage() {
  manageDraft.value = cloneCards(layout.value);
  manageOpen.value = true;
}

async function persistManage() {
  const next = ensureCardGeometry(
    layout.value.map((card) => {
      const draft = manageDraft.value.find((item) => item.id === card.id);
      return draft ? { ...card, visible: draft.visible } : card;
    }),
  );

  if (editing.value) {
    layout.value = next;
    manageOpen.value = false;
    await loadCardData();
    void refreshStatus();
    return;
  }

  saving.value = true;
  try {
    const saved = await saveDashboardLayout({ cards: next });
    layout.value = ensureCardGeometry(saved.cards);
    manageOpen.value = false;
    await loadCardData();
    void refreshStatus();
    message.success("卡片可见性已保存");
  } catch (error) {
    showLoadError(error);
  } finally {
    saving.value = false;
  }
}

function onGridChange(cards: DashboardCardLayout[]) {
  layout.value = ensureCardGeometry(cards);
}

function openBuildRun(id: number) {
  void router.push({ name: "cicd-build-run-detail", params: { id: String(id) } });
}

function openAgentRun(id: number) {
  void router.push({ name: "ai-run-detail", params: { id: String(id) } });
}

function openScriptRun(id: number) {
  void router.push({ name: "cicd-script-run-detail", params: { id: String(id) } });
}

function openPipelineRun(id: number) {
  void router.push({ name: "cicd-pipeline-run-detail", params: { id: String(id) } });
}

function openProject(id: number) {
  void router.push({ name: "project-detail", params: { id: String(id) } });
}

function openProjects() {
  void router.push({ name: "projects" });
}

function openBuildJobs() {
  void router.push({ name: "cicd-build-jobs" });
}

function openScriptJobs() {
  void router.push({ name: "cicd-script-jobs" });
}

function openPipelines() {
  void router.push({ name: "cicd-pipelines" });
}

function openAgentJobs() {
  void router.push({ name: "ai-agents" });
}

function showRunning(kind: RunningDialogKind) {
  runningDialogKind.value = kind;
  runningDialogOpen.value = true;
}

useDashboardWs({
  systemStatus,
  onRunChanged,
});

void loadDashboard();
</script>

<template>
  <div class="dashboard">
    <div class="dashboard__toolbar">
      <template v-if="editing">
        <u-button text @click="resetToDefault">恢复默认</u-button>
        <u-button text @click="cancelEdit">取消</u-button>
        <u-button type="primary" :loading="saving" @click="saveEdit">保存</u-button>
      </template>
      <template v-else>
        <u-button text type="primary" @click="enterEdit">
          <u-icon :size="14"><Edit /></u-icon>
          编辑布局
        </u-button>
      </template>
      <u-button text @click="openManage">
        <u-icon :size="14"><Setting /></u-icon>
        管理卡片
      </u-button>
    </div>

    <u-dialog v-model="manageOpen" title="管理卡片" style="width: 480px">
      <p class="dashboard__editor-hint">仅列出当前账号可用的卡片；关闭后可在此重新启用。</p>
      <div v-for="card in manageDraft" :key="card.id" class="dashboard__editor-row">
        <label class="dashboard__editor-label">
          <u-switch v-model="card.visible" />
          <span>{{ cardTitles[card.id] }}</span>
        </label>
      </div>
      <template #footer="{ close }">
        <u-button text @click="close()">取消</u-button>
        <u-button type="primary" :loading="saving" @click="persistManage">保存</u-button>
      </template>
    </u-dialog>

    <DashboardRunningDialog v-model="runningDialogOpen" :kind="runningDialogKind" />

    <div v-if="loading" v-loading="true" class="dashboard__loading" />
    <u-scroll v-else class="dashboard__content">
      <DashboardGrid
        v-if="visibleCards.length"
        :items="layout"
        :editing="editing"
        :build-summary="buildSummary"
        :agent-run-summary="agentRunSummary"
        :script-run-summary="scriptRunSummary"
        :pipeline-run-summary="pipelineRunSummary"
        :task-overview="taskOverview"
        :my-projects="myProjects"
        :system-info="systemInfo"
        :system-status="systemStatus"
        @change="onGridChange"
        @open-build-run="openBuildRun"
        @open-agent-run="openAgentRun"
        @open-script-run="openScriptRun"
        @open-pipeline-run="openPipelineRun"
        @open-project="openProject"
        @open-projects="openProjects"
        @open-build-jobs="openBuildJobs"
        @open-script-jobs="openScriptJobs"
        @open-pipelines="openPipelines"
        @open-agent-jobs="openAgentJobs"
        @show-running="showRunning"
      />
      <div v-else class="dashboard__empty">
        <u-empty text="当前没有可见卡片。请打开「管理卡片」启用。" />
      </div>
    </u-scroll>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;
@use "@/lib/empty-center.scss" as empty;

.dashboard {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.dashboard__toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-height: 32px;
}

.dashboard__content {
  flex: 1;
  min-height: 0;
}

.dashboard__editor-hint {
  margin: 0 0 12px;
  color: fn.use-var(text-color, second);
  font-size: 13px;
}

.dashboard__editor-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;

  & + & {
    border-top: fn.use-var(border, muted);
  }
}

.dashboard__editor-label {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: fn.use-var(text-color, main);
  cursor: pointer;
}

.dashboard__loading {
  flex: 1;
  min-height: 240px;
}

.dashboard__empty {
  @include empty.center(320px);
}
</style>
