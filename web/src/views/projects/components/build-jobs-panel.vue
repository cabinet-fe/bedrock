<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { Folder, GitBranch, Link, Terminal, Time } from "@veltra/icons/normal";

import { enqueueBuildRun, getBuildRun, listBuildJobs, listBuildRuns } from "@/api/cicd";
import type { BuildJob, ProductProject } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import { loadDictOptions } from "@/lib/dict";
import { repoTagType, splitCommaTags } from "@/lib/tag";
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

function parseRepoSlug(url?: string): string {
  if (!url) return "";
  const cleaned = url.replace(/\.git$/i, "").replace(/\/+$/, "");
  const parts = cleaned.split(/[/:]/);
  return parts[parts.length - 1] || "";
}

function resolveJobTags(job: BuildJob): string[] {
  const direct = splitCommaTags(job.tags);
  if (direct.length) return direct;

  const repo = repoStore.repoMap.get(job.repository_id);
  if (repo) {
    const repoTags = splitCommaTags(repo.tags);
    if (repoTags.length) return repoTags;

    const trimmed = repo.name?.trim() ?? "";
    const lower = trimmed.toLowerCase();
    if (["前端", "后端", "全栈", "frontend", "backend", "fullstack"].includes(lower)) {
      return [trimmed];
    }
  }

  return [];
}

function repoDisplayName(job: BuildJob): string {
  const repo = repoStore.repoMap.get(job.repository_id);
  const fallback = repoStore.nameMap.get(job.repository_id) ?? `#${job.repository_id}`;
  if (!repo) return fallback;

  const trimmed = repo.name?.trim() ?? "";
  const lower = trimmed.toLowerCase();
  if (
    ["前端", "后端", "全栈", "frontend", "backend", "fullstack"].includes(lower) &&
    repo.repo_url
  ) {
    const slug = parseRepoSlug(repo.repo_url);
    if (slug) return slug;
  }
  return trimmed || fallback;
}

function tagLabel(value: string): string {
  return repoTypeLabelMap.value.get(value) ?? value;
}

function canRun(job: BuildJob) {
  return job.enabled && job.trigger_manual;
}

function disabledTip(job: BuildJob) {
  if (!job.enabled) return "已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function openHistory(job: BuildJob) {
  historyJob.value = job;
  historyOpen.value = true;
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
        :description="job.description"
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
        <template #tags>
          <u-tag
            v-for="tag in resolveJobTags(job)"
            :key="tag"
            size="small"
            :type="repoTagType(tag)"
          >
            {{ tagLabel(tag) }}
          </u-tag>
        </template>

        <span class="run-card__meta-item" :title="'代码仓库: ' + repoDisplayName(job)">
          <u-icon :size="13"><Folder /></u-icon>
          <span>{{ repoDisplayName(job) }}</span>
        </span>
        <span class="run-card__meta-item run-card__mono" :title="'构建分支: ' + job.branch">
          <u-icon :size="13"><GitBranch /></u-icon>
          <span>{{ job.branch }}</span>
        </span>
        <span
          v-if="job.work_dir"
          class="run-card__meta-item run-card__mono"
          :title="'工作目录: ' + job.work_dir"
        >
          <u-icon :size="13"><Folder /></u-icon>
          <span>{{ job.work_dir }}</span>
        </span>
        <span
          v-if="job.build_script_type && job.build_script_type !== 'bash'"
          class="run-card__meta-item"
          :title="'脚本类型: ' + job.build_script_type"
        >
          <u-icon :size="13"><Terminal /></u-icon>
          <span>{{ job.build_script_type }}</span>
        </span>
        <span
          v-if="job.trigger_cron && job.cron_expression"
          class="run-card__meta-item run-card__mono"
          :title="'定时构建: ' + job.cron_expression"
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
          <span v-if="job.shallow_clone" class="run-card__footer-item">浅克隆</span>
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
