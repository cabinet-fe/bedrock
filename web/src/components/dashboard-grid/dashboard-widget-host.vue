<script setup lang="ts">
defineOptions({ name: "DashboardWidgetHost" });

import { inject } from "vue";
import { Move } from "@veltra/icons/normal";

import type { DashboardCardID } from "@/api/types";
import DashboardAgentRunCard from "@/components/dashboard-agent-run-card";
import DashboardBuildCard from "@/components/dashboard-build-card";
import DashboardMyProjectsCard from "@/components/dashboard-my-projects-card";
import DashboardPipelineRunCard from "@/components/dashboard-pipeline-run-card";
import DashboardScriptRunCard from "@/components/dashboard-script-run-card";
import DashboardSystemInfoCard from "@/components/dashboard-system-info-card";
import DashboardSystemStatusCard from "@/components/dashboard-system-status-card";
import DashboardTaskOverviewCard from "@/components/dashboard-task-overview-card";
import { useGridStackItem } from "@/lib/gridstack-vue";

import { DASHBOARD_WIDGET_CTX } from "./helper";

const { id } = useGridStackItem();
const ctx = inject(DASHBOARD_WIDGET_CTX);
if (!ctx) throw new Error("DashboardWidgetHost must be used inside DashboardGrid");

const cardId = id as DashboardCardID;
</script>

<template>
  <div class="dashboard-widget">
    <div v-if="ctx.editing" class="dashboard-widget__drag" title="拖拽移动">
      <u-icon :size="14"><Move /></u-icon>
      <span>拖拽</span>
    </div>
    <div class="dashboard-widget__body">
      <DashboardBuildCard
        v-if="cardId === 'build_summary'"
        :data="ctx.buildSummary"
        @open-run="ctx.openBuildRun"
        @show-running="ctx.showRunning('build')"
        @open-jobs="ctx.openBuildJobs"
      />
      <DashboardAgentRunCard
        v-else-if="cardId === 'agent_run_summary'"
        :data="ctx.agentRunSummary"
        @open-run="ctx.openAgentRun"
        @open-jobs="ctx.openAgentJobs"
      />
      <DashboardScriptRunCard
        v-else-if="cardId === 'script_run_summary'"
        :data="ctx.scriptRunSummary"
        @open-run="ctx.openScriptRun"
        @show-running="ctx.showRunning('script')"
        @open-jobs="ctx.openScriptJobs"
      />
      <DashboardPipelineRunCard
        v-else-if="cardId === 'pipeline_run_summary'"
        :data="ctx.pipelineRunSummary"
        @open-run="ctx.openPipelineRun"
        @show-running="ctx.showRunning('pipeline')"
        @open-jobs="ctx.openPipelines"
      />
      <DashboardTaskOverviewCard
        v-else-if="cardId === 'cicd_task_overview'"
        :data="ctx.taskOverview"
        @open-build-jobs="ctx.openBuildJobs"
        @open-script-jobs="ctx.openScriptJobs"
        @open-pipelines="ctx.openPipelines"
      />
      <DashboardMyProjectsCard
        v-else-if="cardId === 'my_projects'"
        :data="ctx.myProjects"
        @open-project="ctx.openProject"
        @open-projects="ctx.openProjects"
      />
      <DashboardSystemInfoCard v-else-if="cardId === 'system_info'" :data="ctx.systemInfo" />
      <DashboardSystemStatusCard v-else-if="cardId === 'system_status'" :data="ctx.systemStatus" />
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.dashboard-widget {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.dashboard-widget__drag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  padding: 6px 10px;
  margin-bottom: 4px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 75%, transparent);
  color: fn.use-var(text-color, second);
  font-size: 12px;
  cursor: move;
  user-select: none;
}

.dashboard-widget__body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
</style>
