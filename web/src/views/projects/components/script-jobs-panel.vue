<script setup lang="ts">
import { onMounted, ref } from "vue";

import { enqueueScriptRun, getScriptRun, listScriptJobs, listScriptRuns } from "@/api/cicd";
import type { ProductProject, ScriptJob } from "@/api/types";
import { usePermission } from "@/composables/use-permission";

import { isRunTerminal, useRunPoll } from "../composables/use-run-poll";
import RunCard from "./run-card.vue";

const props = defineProps<{ project: ProductProject }>();

const { hasPermission } = usePermission();
const canExecute = hasPermission("cicd_script_jobs:execute");

const jobs = ref<ScriptJob[]>([]);
const loading = ref(true);
const loadError = ref("");

const { statusMap, errorMap, isBusy, enqueue, loadRecent } = useRunPoll({
  fetch: (id) => getScriptRun(id),
  isTerminal: isRunTerminal,
});

function canRun(job: ScriptJob) {
  return job.enabled && job.trigger_manual;
}

function disabledTip(job: ScriptJob) {
  if (!job.enabled) return "任务已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
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
        :status="statusMap.get(job.id)"
        :error="errorMap.get(job.id)"
        :runnable="canRun(job)"
        :disabled-tip="disabledTip(job)"
        :busy="isBusy(job.id)"
        :can-execute="canExecute"
        @run="enqueue(job.id, () => enqueueScriptRun(job.id))"
      >
        <span>{{ job.script_type }}</span>
        <u-tag size="small" :type="job.enabled ? 'success' : undefined">
          {{ job.enabled ? "启用" : "停用" }}
        </u-tag>
      </RunCard>
    </div>
  </div>
</template>

<style scoped>
.run-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: auto;
}

.run-panel__empty {
  flex: 1;
  display: grid;
  place-items: center;
}

.run-panel__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 12px;
  align-content: start;
  padding: 2px 2px 16px;
}
</style>
