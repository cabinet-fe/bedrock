<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { Folder, Link, Time } from "@veltra/icons/normal";

import { enqueueScriptRun, getScriptRun, listScriptJobs, listScriptRuns } from "@/api/cicd";
import type { ProductProject, ScriptJob } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";

const SCRIPT_TYPE_LABEL: Record<string, string> = {
  bash: "Bash / sh",
  node: "Node.js",
  python: "Python",
  pwsh: "PowerShell 7+",
  powershell: "Windows PowerShell",
  cmd: "CMD",
};

function scriptTypeLabel(type: string) {
  return SCRIPT_TYPE_LABEL[type] ?? type;
}

import { isRunTerminal, useRunPoll } from "../composables/use-run-poll";
import RunCard from "./run-card.vue";
import RunHistoryDialog from "./run-history-dialog.vue";

const props = defineProps<{ project: ProductProject }>();

const { hasPermission } = usePermission();
const canExecute = hasPermission("cicd_script_jobs:execute");
const canViewHistory = hasPermission("cicd_script_jobs:view");

const jobs = ref<ScriptJob[]>([]);
const loading = ref(true);
const loadError = ref("");

const historyOpen = ref(false);
const historyJob = ref<ScriptJob | null>(null);

const { statusMap, errorMap, isBusy, enqueue, loadRecent } = useRunPoll({
  fetch: (id) => getScriptRun(id),
  isTerminal: isRunTerminal,
});

function canRun(job: ScriptJob) {
  return job.enabled && job.trigger_manual;
}

function disabledTip(job: ScriptJob) {
  if (!job.enabled) return "已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function openHistory(job: ScriptJob) {
  historyJob.value = job;
  historyOpen.value = true;
}

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await listScriptJobs({ project_id: props.project.id, page: 1, page_size: 100 });
    jobs.value = res.items ?? [];
  } catch (error) {
    jobs.value = [];
    loadError.value = error instanceof Error ? error.message : "加载脚本任务失败";
  } finally {
    loading.value = false;
  }
}

async function loadRecentStatus() {
  try {
    const res = await listScriptRuns({ project_id: props.project.id, page: 1, page_size: 50 });
    loadRecent(
      (res.items ?? []).map((r) => ({ entityId: r.script_job_id, runId: r.id, status: r.status })),
    );
  } catch {
    /* 状态降级为无 */
  }
}

const historyEntityId = computed(() => historyJob.value?.id ?? 0);
const historyEntityName = computed(() => historyJob.value?.name ?? "");

onMounted(() => {
  void load();
  void loadRecentStatus();
});
</script>

<template>
  <div v-loading="loading" class="run-panel">
    <div v-if="!loading && (loadError || !jobs.length)" class="run-panel__empty">
      <u-empty :text="loadError || '暂无脚本任务，请到「CI/CD · 脚本任务」创建并归属本项目'" />
    </div>
    <div v-else-if="jobs.length" class="run-panel__grid">
      <RunCard
        v-for="job in jobs"
        :key="job.id"
        :name="job.name"
        :description="job.description"
        :status="statusMap.get(job.id)"
        :error="errorMap.get(job.id)"
        :runnable="canRun(job)"
        :disabled-tip="disabledTip(job)"
        :busy="isBusy(job.id)"
        :can-execute="canExecute"
        :can-view-history="canViewHistory"
        @history="openHistory(job)"
        @run="enqueue(job.id, () => enqueueScriptRun(job.id))"
      >
        <template #tags>
          <u-tag size="small" type="primary">
            {{ scriptTypeLabel(job.script_type) }}
          </u-tag>
        </template>

        <span
          v-if="job.work_dir"
          class="run-card__meta-item run-card__mono"
          :title="'工作目录: ' + job.work_dir"
        >
          <u-icon :size="13"><Folder /></u-icon>
          <span>{{ job.work_dir }}</span>
        </span>
        <span
          v-if="job.trigger_cron && job.cron_expression"
          class="run-card__meta-item run-card__mono"
          :title="'定时执行: ' + job.cron_expression"
        >
          <u-icon :size="13"><Time /></u-icon>
          <span>{{ job.cron_expression }}</span>
        </span>
        <span v-if="job.trigger_webhook" class="run-card__meta-item" title="支持 Webhook 自动触发">
          <u-icon :size="13"><Link /></u-icon>
          <span>Webhook</span>
        </span>

        <template #footer>
          <span>更新于 {{ formatDateTime(job.updated_at) || "—" }}</span>
        </template>
      </RunCard>
    </div>

    <RunHistoryDialog
      v-model="historyOpen"
      kind="script"
      :entity-id="historyEntityId"
      :entity-name="historyEntityName"
      :project-id="project.id"
    />
  </div>
</template>

<style scoped lang="scss">
@use "@/lib/empty-center.scss" as empty;

.run-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: auto;
}

.run-panel__empty {
  @include empty.center;
}

.run-panel__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
  align-content: start;
  padding: 2px 2px 16px;
}
</style>
