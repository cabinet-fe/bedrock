<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { enqueueBuildRun, getBuildRun, listBuildJobs, listBuildRuns } from "@/api/cicd";
import type { BuildJob, ProductProject } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { loadDictOptions } from "@/lib/dict";
import { splitCommaTags } from "@/lib/tag";
import { useRepositoryStore } from "@/stores/repositories";

import { isRunTerminal, useRunPoll } from "../composables/use-run-poll";
import RunCard from "./run-card.vue";
import RunHistoryDialog from "./run-history-dialog.vue";

const props = defineProps<{ project: ProductProject }>();

const { hasPermission } = usePermission();
const canExecute = hasPermission("cicd_build_jobs:execute");
const canViewHistory = hasPermission("cicd_build_jobs:view");
const repoStore = useRepositoryStore();

const jobs = ref<BuildJob[]>([]);
const loading = ref(true);
const loadError = ref("");
const repoTypeLabelMap = ref(new Map<string, string>());

const historyOpen = ref(false);
const historyJob = ref<BuildJob | null>(null);

const { statusMap, errorMap, isBusy, enqueue, loadRecent } = useRunPoll({
  fetch: (id) => getBuildRun(id),
  isTerminal: isRunTerminal,
});

function repoName(id: number): string {
  return repoStore.nameMap.get(id) ?? `#${id}`;
}

function tagLabel(value: string): string {
  return repoTypeLabelMap.value.get(value) ?? value;
}

function canRun(job: BuildJob) {
  return job.enabled && job.trigger_manual;
}

function disabledTip(job: BuildJob) {
  if (!job.enabled) return "任务已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function openHistory(job: BuildJob) {
  historyJob.value = job;
  historyOpen.value = true;
}

function buildMetaParts(job: BuildJob): string[] {
  const parts = [
    ...splitCommaTags(job.tags).map(tagLabel),
    repoName(job.repository_id),
    job.branch,
  ];
  if (!job.enabled) parts.push("已停用");
  return parts;
}

async function load() {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await listBuildJobs({ project_id: props.project.id, page: 1, page_size: 100 });
    jobs.value = res.items ?? [];
  } catch (error) {
    jobs.value = [];
    loadError.value = error instanceof Error ? error.message : "加载构建任务失败";
  } finally {
    loading.value = false;
  }
}

async function loadRecentStatus() {
  try {
    const res = await listBuildRuns({ project_id: props.project.id, page: 1, page_size: 50 });
    loadRecent(
      (res.items ?? []).map((r) => ({ entityId: r.build_job_id, runId: r.id, status: r.status })),
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
  if (hasPermission("resource_repositories:view")) {
    void repoStore.load();
  }
  void loadDictOptions("repo_type")
    .then((opts) => {
      const map = new Map<string, string>();
      for (const opt of opts) map.set(opt.value, opt.label);
      repoTypeLabelMap.value = map;
    })
    .catch(() => {
      /* 标签降级为原始值 */
    });
});
</script>

<template>
  <div v-loading="loading" class="run-panel">
    <div v-if="!loading && (loadError || !jobs.length)" class="run-panel__empty">
      <u-empty :text="loadError || '暂无构建任务，请到「CI/CD · 构建任务」创建并归属本项目'" />
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
        :can-view-history="canViewHistory"
        @history="openHistory(job)"
        @run="enqueue(job.id, () => enqueueBuildRun(job.id, { trigger_type: 'manual' }))"
      >
        <template v-for="(part, index) in buildMetaParts(job)" :key="`${job.id}-${index}`">
          <span v-if="index > 0" class="run-card__sep" aria-hidden="true">·</span>
          <span
            :class="[
              part === job.branch && 'run-card__mono',
              part === '已停用' && 'run-card__warn',
            ]"
          >
            {{ part }}
          </span>
        </template>
      </RunCard>
    </div>

    <RunHistoryDialog
      v-model="historyOpen"
      kind="build"
      :entity-id="historyEntityId"
      :entity-name="historyEntityName"
      :project-id="project.id"
    />
  </div>
</template>

<style scoped>
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
