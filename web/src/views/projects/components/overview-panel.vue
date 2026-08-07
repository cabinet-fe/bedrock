<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { Agent, Build, Books, Layers, Terminal } from "@veltra/icons/normal";

import { listRuns } from "@/api/ai";
import {
  listBuildJobs,
  listBuildPipelines,
  listBuildRuns,
  listPipelineRuns,
  listScriptJobs,
  listScriptRuns,
} from "@/api/cicd";
import { listMembers, listRequirements } from "@/api/projects";
import type { ProductProject, ProjectMember } from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationBetween, formatDurationMs } from "@/lib/datetime";
import { JOB_STATUS_TAG, tagType } from "@/lib/tag";

type StatKey = "build" | "script" | "pipeline" | "requirements";

type StatCard = {
  key: StatKey;
  label: string;
  total: number | null;
  error: boolean;
  href?: string;
};

type TimelineItem = {
  key: string;
  type: "build" | "script" | "pipeline" | "ai";
  name: string;
  status: string;
  duration: string;
  triggeredBy: string;
  createdAt: string;
  href: string;
};

const TYPE_LABEL: Record<TimelineItem["type"], string> = {
  build: "构建",
  script: "脚本",
  pipeline: "流水线",
  ai: "智能体",
};

const TYPE_ICON = { build: Build, script: Terminal, pipeline: Layers, ai: Agent };

const STAT_ICON: Record<StatKey, unknown> = {
  build: Build,
  script: Terminal,
  pipeline: Layers,
  requirements: Books,
};

const VISIBILITY_LABEL: Record<ProductProject["status"] | string, string> = {
  active: "运行中",
  archived: "已归档",
};

const props = defineProps<{ project: ProductProject }>();

const router = useRouter();
const { hasPermission } = usePermission();

const loading = ref(true);
const stats = ref<StatCard[]>([]);
const timeline = ref<TimelineItem[]>([]);
const timelineError = ref(false);
const memberCount = ref<number | null>(null);

const projectTags = computed(() =>
  props.project.tags
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean),
);

const headerMeta = computed(() => {
  const items: string[] = [];
  if (props.project.slug) items.push(props.project.slug);
  items.push(props.project.is_public ? "公开" : "私有");
  items.push(VISIBILITY_LABEL[props.project.status] ?? props.project.status);
  if (memberCount.value != null) items.push(`${memberCount.value} 名成员`);
  items.push(`创建于 ${formatDateTime(props.project.created_at) || "—"}`);
  return items;
});

function userLabel(u: { username?: string; display_name?: string }): string {
  if (u.display_name && u.username) return `${u.display_name} (${u.username})`;
  return u.display_name || u.username || "";
}

async function settleTotal(
  key: StatKey,
  label: string,
  href: string,
  loader: () => Promise<{ total: number }>,
): Promise<StatCard> {
  try {
    const res = await loader();
    return { key, label, href, total: res.total, error: false };
  } catch {
    return { key, label, href, total: null, error: true };
  }
}

async function loadStats() {
  const tasks: Promise<StatCard>[] = [];
  if (hasPermission("cicd_build_jobs:view")) {
    tasks.push(
      settleTotal("build", "构建任务", "/cicd/build-jobs", () =>
        listBuildJobs({ project_id: props.project.id, page: 1, page_size: 1 }),
      ),
    );
  }
  if (hasPermission("cicd_script_jobs:view")) {
    tasks.push(
      settleTotal("script", "脚本任务", "/cicd/script-jobs", () =>
        listScriptJobs({ project_id: props.project.id, page: 1, page_size: 1 }),
      ),
    );
  }
  if (hasPermission("cicd_pipelines:view")) {
    tasks.push(
      settleTotal("pipeline", "流水线", "/cicd/pipelines", () =>
        listBuildPipelines({ project_id: props.project.id, page: 1, page_size: 1 }),
      ),
    );
  }
  if (hasPermission("project_requirements:view")) {
    tasks.push(
      settleTotal("requirements", "需求", "/project/requirements", () =>
        listRequirements(props.project.id, { page: 1, page_size: 1 }),
      ),
    );
  }
  stats.value = await Promise.all(tasks);
}

async function loadTimeline() {
  const pid = props.project.id;
  const chunks: TimelineItem[][] = [];
  let anyOk = false;
  let anyAttempt = false;

  // 触发者 id → 用户名：成员列表带 username/display_name，失败则回退为 #id
  const users = new Map<number, string>();
  try {
    const members: ProjectMember[] = await listMembers(pid);
    for (const m of members) users.set(m.user_id, userLabel(m));
  } catch {
    /* 成员加载失败时触发者回退为 #id */
  }
  const who = (id?: number) => (id ? (users.get(id) ?? `#${id}`) : "—");

  async function settleBlock(block: () => Promise<TimelineItem[]>) {
    anyAttempt = true;
    try {
      chunks.push(await block());
      anyOk = true;
    } catch {
      /* 单块降级 */
    }
  }

  const blocks: Promise<void>[] = [];

  if (hasPermission("cicd_build_jobs:view")) {
    blocks.push(
      settleBlock(async () => {
        const [runs, jobs] = await Promise.all([
          listBuildRuns({ project_id: pid, page: 1, page_size: 5 }),
          listBuildJobs({ project_id: pid, page: 1, page_size: 100 }),
        ]);
        const names = new Map((jobs.items ?? []).map((j) => [j.id, j.name]));
        return (runs.items ?? []).map((r) => ({
          key: `build-${r.id}`,
          type: "build" as const,
          name: names.get(r.build_job_id) ?? `任务#${r.build_job_id}`,
          status: r.status,
          duration: formatDurationMs(r.duration_ms) || "—",
          triggeredBy: who(r.triggered_by),
          createdAt: r.created_at,
          href: `/cicd/build-runs/${r.id}`,
        }));
      }),
    );
  }

  if (hasPermission("cicd_script_jobs:view")) {
    blocks.push(
      settleBlock(async () => {
        const [runs, jobs] = await Promise.all([
          listScriptRuns({ project_id: pid, page: 1, page_size: 5 }),
          listScriptJobs({ project_id: pid, page: 1, page_size: 100 }),
        ]);
        const names = new Map((jobs.items ?? []).map((j) => [j.id, j.name]));
        return (runs.items ?? []).map((r) => ({
          key: `script-${r.id}`,
          type: "script" as const,
          name: names.get(r.script_job_id) ?? `任务#${r.script_job_id}`,
          status: r.status,
          duration: formatDurationMs(r.duration_ms) || "—",
          triggeredBy: who(r.triggered_by),
          createdAt: r.created_at,
          href: `/cicd/script-runs/${r.id}`,
        }));
      }),
    );
  }

  if (hasPermission("cicd_pipelines:view")) {
    blocks.push(
      settleBlock(async () => {
        const [runs, pipes] = await Promise.all([
          listPipelineRuns({ project_id: pid, page: 1, page_size: 5 }),
          listBuildPipelines({ project_id: pid, page: 1, page_size: 100 }),
        ]);
        const names = new Map((pipes.items ?? []).map((p) => [p.id, p.name]));
        return (runs.items ?? []).map((r) => ({
          key: `pipeline-${r.id}`,
          type: "pipeline" as const,
          name: names.get(r.build_pipeline_id) ?? `流水线#${r.build_pipeline_id}`,
          status: r.status,
          duration: formatDurationBetween(r.started_at, r.finished_at) || "—",
          triggeredBy: who(r.triggered_by),
          createdAt: r.created_at,
          href: `/cicd/pipeline-runs/${r.id}`,
        }));
      }),
    );
  }

  if (hasPermission("ai_runs:view")) {
    blocks.push(
      settleBlock(async () => {
        // AgentRun.project_id 仅 docs_generate 等显式场景写入；智能体本身不归属项目
        const runs = await listRuns({ project_id: pid, page: 1, page_size: 5 });
        return (runs.items ?? []).map((r) => ({
          key: `ai-${r.id}`,
          type: "ai" as const,
          name: `智能体#${r.agent_id}`,
          status: r.status,
          duration: formatDurationMs(r.duration_ms) || "—",
          triggeredBy: "—",
          createdAt: r.created_at,
          href: `/ai/runs/${r.id}`,
        }));
      }),
    );
  }

  await Promise.all(blocks);
  timeline.value = chunks
    .flat()
    .sort((a, b) => (a.createdAt < b.createdAt ? 1 : a.createdAt > b.createdAt ? -1 : 0));
  timelineError.value = anyAttempt && !anyOk;
}

async function loadMembers() {
  if (!hasPermission("project_projects:view")) return;
  try {
    const members = await listMembers(props.project.id);
    memberCount.value = members.length;
  } catch {
    memberCount.value = null;
  }
}

onMounted(async () => {
  loading.value = true;
  await Promise.all([loadStats(), loadTimeline(), loadMembers()]);
  loading.value = false;
});

function openItem(item: TimelineItem) {
  void router.push(item.href);
}

function openStat(card: StatCard) {
  if (!card.href) return;
  void router.push({ path: card.href, query: { project_id: String(props.project.id) } });
}
</script>

<template>
  <div v-loading="loading" class="overview-panel">
    <!-- 项目头部：名称 / 描述 / 关键元信息 -->
    <header class="overview-panel__header">
      <div class="overview-panel__title-row">
        <h2 class="overview-panel__title">{{ project.name }}</h2>
        <u-tag v-if="project.status === 'archived'" size="small" type="warning">已归档</u-tag>
      </div>
      <p v-if="project.description" class="overview-panel__desc">{{ project.description }}</p>
      <div class="overview-panel__meta">
        <span v-for="(m, i) in headerMeta" :key="i" class="overview-panel__meta-item">{{ m }}</span>
        <template v-if="projectTags.length">
          <u-tag v-for="t in projectTags" :key="t" size="small" type="info">{{ t }}</u-tag>
        </template>
      </div>
    </header>

    <!-- 资源摘要 -->
    <section v-if="stats.length" class="overview-panel__section">
      <h3 class="overview-panel__section-title">资源</h3>
      <div class="overview-panel__stats">
        <button
          v-for="card in stats"
          :key="card.key"
          type="button"
          class="overview-panel__stat"
          @click="openStat(card)"
        >
          <span class="overview-panel__stat-icon" aria-hidden="true">
            <u-icon :size="16"><component :is="STAT_ICON[card.key]" /></u-icon>
          </span>
          <span class="overview-panel__stat-body">
            <span class="overview-panel__stat-label">{{ card.label }}</span>
            <span class="overview-panel__stat-value">
              <template v-if="card.error">—</template>
              <template v-else>{{ card.total ?? 0 }}</template>
            </span>
          </span>
        </button>
      </div>
    </section>

    <!-- 最近运行 -->
    <section class="overview-panel__section overview-panel__timeline">
      <h3 class="overview-panel__section-title">最近运行</h3>
      <u-empty v-if="timelineError" text="最近运行加载失败" />
      <u-empty v-else-if="!loading && !timeline.length" text="暂无运行记录" />
      <button
        v-for="item in timeline"
        :key="item.key"
        type="button"
        class="overview-panel__row"
        @click="openItem(item)"
      >
        <span class="overview-panel__type" aria-hidden="true">
          <u-icon :size="14"><component :is="TYPE_ICON[item.type]" /></u-icon>
        </span>
        <span class="overview-panel__name">{{ item.name }}</span>
        <u-tag size="small" :type="tagType(item.status, JOB_STATUS_TAG)">{{ item.status }}</u-tag>
        <span class="overview-panel__meta">{{ item.duration }}</span>
        <span class="overview-panel__meta overview-panel__who">{{ item.triggeredBy }}</span>
        <span class="overview-panel__meta overview-panel__time">{{
          formatDateTime(item.createdAt)
        }}</span>
      </button>
    </section>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.overview-panel {
  display: flex;
  flex-direction: column;
  gap: 24px;
  min-height: 0;
  height: 100%;
  overflow: auto;
  padding: 4px 4px 16px;
}

.overview-panel__header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.overview-panel__title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.overview-panel__title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-panel__desc {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: fn.use-var(text-color, second);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.overview-panel__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 14px;
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}

.overview-panel__meta-item {
  white-space: nowrap;
}

.overview-panel__section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

.overview-panel__section-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.overview-panel__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
}

.overview-panel__stat {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding: 12px 14px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
  }
}

.overview-panel__stat-icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(color, primary) 14%, transparent);
  color: fn.use-var(color, primary);
}

.overview-panel__stat-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.overview-panel__stat-label {
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}

.overview-panel__stat-value {
  font-size: 22px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.15;
  color: fn.use-var(text-color, title);
}

.overview-panel__timeline {
  flex: 1;
  min-height: 0;
}

.overview-panel__row {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto auto auto auto;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  border-bottom: fn.use-var(border, muted);
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
  }

  &:last-child {
    border-bottom: 0;
  }
}

.overview-panel__type {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(color, primary) 12%, transparent);
  color: fn.use-var(color, primary);
}

.overview-panel__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  color: fn.use-var(text-color, main);
}

.overview-panel__meta {
  flex-shrink: 0;
  font-size: 12px;
  color: fn.use-var(text-color, assist);
  font-variant-numeric: tabular-nums;
}

.overview-panel__who {
  min-width: 0;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-panel__time {
  white-space: nowrap;
}
</style>
