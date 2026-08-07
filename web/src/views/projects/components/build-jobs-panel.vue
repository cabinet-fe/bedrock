<script setup lang="ts">
import { nextTick, onMounted, reactive, ref, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { enqueueBuildRun, listBuildRuns } from "@/api/cicd";
import { listRepositories } from "@/api/resource";
import type { BuildJob, BuildRun, ProductProject } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { BUILD_STAGE_TAG, JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType } from "@/lib/tag";

const props = defineProps<{ project: ProductProject }>();

const router = useRouter();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const tableRef = useTemplateRef("table");
const historyRef = useTemplateRef("history");
const query = reactive({ project_id: props.project.id });
const repoNameMap = ref(new Map<number, string>());
const recentStatus = ref(new Map<number, string>());
const historyOpen = ref(false);
const historyJob = ref<BuildJob | null>(null);
const historyQuery = reactive({
  build_job_id: undefined as number | undefined,
  project_id: props.project.id,
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "repository", name: "仓库" },
  { key: "branch", name: "分支", width: 120 },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "tags", name: "标签", width: 160 },
  { key: "recent_status", name: "最近运行", width: 110, align: "center" },
  { key: "action", name: "操作", width: 240, align: "center", fixed: "right" },
]);

const historyColumns = defineProTableColumns([
  { key: "build_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "stage", name: "阶段", width: 100, align: "center" },
  { key: "branch", name: "分支" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
  {
    key: "duration_ms",
    name: "运行时间",
    width: 110,
    align: "center",
    render: ({ val }) => formatDurationMs(val as number) || "—",
  },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 100, align: "center", fixed: "right" },
]);

function splitTags(raw?: string | null): string[] {
  if (!raw) return [];
  return raw
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function repoName(id: number): string {
  return repoNameMap.value.get(id) ?? `#${id}`;
}

function canBuild(job: BuildJob) {
  return job.enabled && job.trigger_manual;
}

function buildDisabledTip(job: BuildJob) {
  if (!job.enabled) return "任务已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function goCreate() {
  void router.push({
    path: "/cicd/build-jobs",
    query: { project_id: String(props.project.id), create: "1" },
  });
}

function goEdit(row: BuildJob) {
  void router.push({
    path: "/cicd/build-jobs",
    query: { project_id: String(props.project.id), id: String(row.id) },
  });
}

function openHistory(row: BuildJob) {
  historyJob.value = row;
  historyQuery.build_job_id = row.id;
  historyQuery.project_id = props.project.id;
  historyOpen.value = true;
}

function openRunDetail(row: BuildRun) {
  historyOpen.value = false;
  void router.push({ name: "cicd-build-run-detail", params: { id: String(row.id) } });
}

const trigger = bind(async (row: BuildJob) => {
  try {
    const run = await enqueueBuildRun(row.id, { trigger_type: "manual" });
    message.success(`已入队 #${run.build_number}`);
    await loadRecentStatus();
    void tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "构建失败");
  }
});

async function loadRecentStatus() {
  try {
    const res = await listBuildRuns({
      project_id: props.project.id,
      page: 1,
      page_size: 50,
    });
    const map = new Map<number, string>();
    for (const run of res.items ?? []) {
      if (!map.has(run.build_job_id)) map.set(run.build_job_id, run.status);
    }
    recentStatus.value = map;
  } catch {
    recentStatus.value = new Map();
  }
}

watch(historyOpen, async (open) => {
  if (!open || !historyJob.value) return;
  historyQuery.build_job_id = historyJob.value.id;
  historyQuery.project_id = props.project.id;
  await nextTick();
  void historyRef.value?.reload();
});

onMounted(async () => {
  const tasks: Promise<void>[] = [loadRecentStatus()];
  if (hasPermission("resource_repositories:view")) {
    tasks.push(
      listRepositories({ page: 1, page_size: 100 })
        .then((res) => {
          const map = new Map<number, string>();
          for (const repo of res.items ?? []) map.set(repo.id, repo.name);
          repoNameMap.value = map;
        })
        .catch(() => {
          /* ignore */
        }),
    );
  }
  await Promise.all(tasks);
});
</script>

<template>
  <div class="resource-panel">
    <ProTable
      ref="table"
      url="/build-jobs"
      :query="query"
      :columns="columns"
      pagination
      height="100%"
    >
      <template #toolbar>
        <u-button
          v-if="hasPermission('cicd_build_jobs:create')"
          type="primary"
          @click.prevent="goCreate"
        >
          新建
        </u-button>
      </template>
      <template #column:repository="{ rowData }">
        {{ repoName((rowData as BuildJob).repository_id) }}
      </template>
      <template #column:tags="{ rowData }">
        <span class="tag-cell">
          <template v-for="parts in [splitTags((rowData as BuildJob).tags)]" :key="0">
            <u-tag v-for="tag in parts" :key="tag" size="small" type="info">{{ tag }}</u-tag>
            <template v-if="!parts.length">—</template>
          </template>
        </span>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as BuildJob).enabled ? 'success' : undefined">
          {{ (rowData as BuildJob).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:recent_status="{ rowData }">
        <u-tag
          v-if="recentStatus.has((rowData as BuildJob).id)"
          size="small"
          :type="tagType(recentStatus.get((rowData as BuildJob).id), JOB_STATUS_TAG)"
        >
          {{ recentStatus.get((rowData as BuildJob).id) }}
        </u-tag>
        <template v-else>—</template>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as BuildJob).id">
          <u-action
            v-if="hasPermission('cicd_build_jobs:execute')"
            :disabled="!canBuild(rowData as BuildJob)"
            :title="buildDisabledTip(rowData as BuildJob)"
            @run="trigger(rowData as BuildJob)"
          >
            构建
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:update')"
            @run="goEdit(rowData as BuildJob)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:view')"
            @run="openHistory(rowData as BuildJob)"
          >
            历史
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <u-dialog
      v-model="historyOpen"
      :title="historyJob ? `构建历史 · ${historyJob.name}` : '构建历史'"
      style="width: 960px"
    >
      <ProTable
        ref="history"
        url="/build-runs"
        :query="historyQuery"
        :columns="historyColumns"
        :immediate="false"
        pagination
        height="420px"
      >
        <template #column:status="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).status, JOB_STATUS_TAG)">
            {{ (rowData as BuildRun).status }}
          </u-tag>
        </template>
        <template #column:stage="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).stage, BUILD_STAGE_TAG)">
            {{ (rowData as BuildRun).stage || "—" }}
          </u-tag>
        </template>
        <template #column:trigger_type="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).trigger_type, TRIGGER_TYPE_TAG)">
            {{ (rowData as BuildRun).trigger_type }}
          </u-tag>
        </template>
        <template #column:action="{ rowData }">
          <u-action @run="openRunDetail(rowData as BuildRun)">详情</u-action>
        </template>
      </ProTable>
      <template #footer="{ close }">
        <u-button text @click="close()">关闭</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped>
.resource-panel {
  height: 100%;
  min-height: 0;
}

.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
