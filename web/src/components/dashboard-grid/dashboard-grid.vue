<script setup lang="ts">
defineOptions({ name: "DashboardGrid" });

import { nextTick, provide, reactive, useTemplateRef, watch } from "vue";
import "gridstack/dist/gridstack.min.css";

import type {
  AgentRunSummary,
  BuildSummary,
  DashboardCardLayout,
  MyProject,
  PipelineRunSummary,
  ScriptRunSummary,
  SystemInfo,
  SystemStatus,
  TaskOverview,
} from "@/api/types";
import {
  GridStackComponent,
  type ComponentMap,
  type GridStackNode,
  type GridStackOptions,
} from "@/lib/gridstack-vue";

import DashboardWidgetHost from "./dashboard-widget-host.vue";
import {
  DASHBOARD_GRID_COLUMNS,
  DASHBOARD_WIDGET_CTX,
  geometryFromWidgets,
  toGridWidgets,
  visibleCardsSignature,
  type DashboardWidgetHostContext,
} from "./helper";

const props = defineProps<{
  items: DashboardCardLayout[];
  editing: boolean;
  buildSummary: BuildSummary | null;
  agentRunSummary: AgentRunSummary | null;
  scriptRunSummary: ScriptRunSummary | null;
  pipelineRunSummary: PipelineRunSummary | null;
  taskOverview: TaskOverview | null;
  myProjects: MyProject[] | null;
  systemInfo: SystemInfo | null;
  systemStatus: SystemStatus | null;
}>();

const emit = defineEmits<{
  change: [cards: DashboardCardLayout[]];
  openBuildRun: [id: number];
  openAgentRun: [id: number];
  openScriptRun: [id: number];
  openPipelineRun: [id: number];
  openProject: [id: number];
  openProjects: [];
  openBuildJobs: [];
  openScriptJobs: [];
  openPipelines: [];
  openAgentJobs: [];
  showRunning: [kind: "build" | "script" | "pipeline"];
}>();

const gridRef = useTemplateRef("gridRef");
let syncingFromGrid = false;

/**
 * Fixed 12 columns: widths scale with the container; w=6 always takes 50%,
 * avoiding leftover space when columns are dropped.
 * Note: options must keep a static reference — the wrapper watches options
 * and calls updateOptions, and updateOptions reloads children as the full
 * layout. If options are rebuilt with editing, entering/exiting edit mode
 * would overwrite the current layout with stale children; editing uses
 * setStatic instead.
 */
const gridOptions: GridStackOptions = {
  column: DASHBOARD_GRID_COLUMNS,
  cellHeight: 80,
  margin: 10,
  animate: true,
  mode: "top",
  handle: ".dashboard-widget__drag",
  alwaysShowResizeHandle: true,
  minRow: 1,
  staticGrid: !props.editing,
  // children only take effect at init; later visibility/layout changes go through watch → load()
  children: toGridWidgets(props.items.filter((card) => card.visible)),
};

/** Each card id maps to one host component; the host dispatches to the concrete card by id. */
const components: ComponentMap = {
  build_summary: DashboardWidgetHost,
  agent_run_summary: DashboardWidgetHost,
  system_info: DashboardWidgetHost,
  system_status: DashboardWidgetHost,
  script_run_summary: DashboardWidgetHost,
  pipeline_run_summary: DashboardWidgetHost,
  cicd_task_overview: DashboardWidgetHost,
  my_projects: DashboardWidgetHost,
};

/** Shared via provide to Teleport-mounted card hosts (the injection chain survives Teleport). */
const hostCtx = reactive<DashboardWidgetHostContext>({
  editing: false,
  buildSummary: null,
  agentRunSummary: null,
  scriptRunSummary: null,
  pipelineRunSummary: null,
  taskOverview: null,
  myProjects: null,
  systemInfo: null,
  systemStatus: null,
  openBuildRun: (id: number) => emit("openBuildRun", id),
  openAgentRun: (id: number) => emit("openAgentRun", id),
  openScriptRun: (id: number) => emit("openScriptRun", id),
  openPipelineRun: (id: number) => emit("openPipelineRun", id),
  openProject: (id: number) => emit("openProject", id),
  openProjects: () => emit("openProjects"),
  openBuildJobs: () => emit("openBuildJobs"),
  openScriptJobs: () => emit("openScriptJobs"),
  openPipelines: () => emit("openPipelines"),
  openAgentJobs: () => emit("openAgentJobs"),
  showRunning: (kind) => emit("showRunning", kind),
});
provide(DASHBOARD_WIDGET_CTX, hostCtx);

function syncHostCtx() {
  hostCtx.editing = props.editing;
  hostCtx.buildSummary = props.buildSummary;
  hostCtx.agentRunSummary = props.agentRunSummary;
  hostCtx.scriptRunSummary = props.scriptRunSummary;
  hostCtx.pipelineRunSummary = props.pipelineRunSummary;
  hostCtx.taskOverview = props.taskOverview;
  hostCtx.myProjects = props.myProjects;
  hostCtx.systemInfo = props.systemInfo;
  hostCtx.systemStatus = props.systemStatus;
}

function onGridChange(_event: Event, nodes: GridStackNode[]) {
  if (!props.editing) return;
  syncingFromGrid = true;
  emit("change", geometryFromWidgets(nodes, props.items));
  void nextTick(() => {
    syncingFromGrid = false;
  });
}

watch(
  () =>
    [
      props.editing,
      props.buildSummary,
      props.agentRunSummary,
      props.scriptRunSummary,
      props.pipelineRunSummary,
      props.taskOverview,
      props.myProjects,
      props.systemInfo,
      props.systemStatus,
    ] as const,
  () => {
    syncHostCtx();
  },
  { immediate: true },
);

watch(
  () => props.editing,
  (editing) => {
    gridRef.value?.getGrid()?.setStatic(!editing);
  },
);

watch(
  () => visibleCardsSignature(props.items),
  () => {
    if (syncingFromGrid) return;
    gridRef.value?.getGrid()?.load(toGridWidgets(props.items.filter((card) => card.visible)));
  },
);
</script>

<template>
  <GridStackComponent
    ref="gridRef"
    class="dashboard-grid"
    :class="{ 'dashboard-grid--editing': editing }"
    :options="gridOptions"
    :components="components"
    @change="onGridChange"
  />
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.dashboard-grid {
  width: 100%;
  min-height: 160px;

  :deep(.grid-stack) {
    min-height: 160px;
  }

  :deep(.grid-stack-item) {
    transition:
      transform 0.18s ease,
      box-shadow 0.18s ease;
  }

  &:not(.dashboard-grid--editing) :deep(.grid-stack-item:hover) {
    transform: translateY(-2px);
  }

  &--editing :deep(.grid-stack-item-content) {
    outline: 1px dashed color-mix(in srgb, fn.use-var(color, primary) 45%, transparent);
    outline-offset: -1px;
    border-radius: fn.use-var(radius, default);
  }
}
</style>
