<script setup lang="ts">
defineOptions({ name: "ProjectDetail" });

import { computed, onMounted, ref, watch } from "vue";
import { message } from "@veltra/desktop";
import { useRoute, useRouter } from "vue-router";

import { getProject } from "@/api/projects";
import type { ProductProject } from "@/api/types";
import { usePermission } from "@/composables/use-permission";

import DocsPanel from "../../components/docs-panel.vue";
import RequirementsPanel from "../../components/requirements-panel.vue";

const route = useRoute();
const router = useRouter();
const { hasPermission } = usePermission();
const project = ref<ProductProject | null>(null);
const tab = ref("requirements");

function parseRouteId(raw: unknown): number | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

// Layout keys detail by path and keep-alive caches the instance. Freeze path/id at
// setup so deactivated instances do not re-read the global route (e.g. /ai/runs/:id)
// and call getProject with a foreign id → 「项目不存在」.
const detailPath = route.path;
const projectID = parseRouteId(route.params.id);

const projectRole = computed(() => project.value?.my_role);
const canManageAll = computed(() => hasPermission("project_projects:manage_all"));
const tabs = computed(
  () =>
    [
      hasPermission("project_requirements:view") ? { key: "requirements", name: "需求" } : null,
      hasPermission("project_docs:view") ? { key: "docs", name: "接口文档" } : null,
    ].filter(Boolean) as { key: string; name: string }[],
);

function resolveTab(preferred?: unknown): string {
  const key = typeof preferred === "string" ? preferred : "";
  if (key && tabs.value.some((item) => item.key === key)) return key;
  return tabs.value[0]?.key ?? "";
}

function isThisDetailActive() {
  return route.path === detailPath;
}

async function load() {
  if (projectID == null) {
    project.value = null;
    return;
  }
  try {
    project.value = await getProject(projectID);
    if (isThisDetailActive()) {
      tab.value = resolveTab(route.query.tab);
    }
  } catch (error) {
    project.value = null;
    if (isThisDetailActive()) {
      message.error(error instanceof Error ? error.message : "读取项目失败");
    }
  }
}

onMounted(() => {
  void load();
});

watch(
  () => route.query.tab,
  (next) => {
    if (!isThisDetailActive()) return;
    const resolved = resolveTab(next);
    if (tab.value !== resolved) tab.value = resolved;
  },
);

watch(tab, (next) => {
  if (!isThisDetailActive()) return;
  if (!next || route.query.tab === next) return;
  void router.replace({ query: { ...route.query, tab: next } });
});
</script>

<template>
  <div class="project-detail">
    <div class="page-toolbar">
      <u-button plain @click="router.push({ name: 'projects' })">返回项目列表</u-button>
    </div>

    <template v-if="project">
      <u-tabs v-model="tab" :items="tabs" />
      <RequirementsPanel
        v-if="tab === 'requirements' && hasPermission('project_requirements:view')"
        class="project-detail__panel"
        :project="project"
        :project-role="projectRole"
        :manage-all="canManageAll"
      />
      <DocsPanel
        v-else-if="tab === 'docs' && hasPermission('project_docs:view')"
        class="project-detail__panel"
        :project="project"
        :project-role="projectRole"
        :manage-all="canManageAll"
      />
    </template>
    <u-empty v-else text="项目不存在或无权访问" />
  </div>
</template>

<style scoped>
.project-detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  gap: 12px;
}

.page-toolbar {
  display: flex;
  flex-shrink: 0;
  align-items: center;
}

.project-detail__panel {
  flex: 1;
  min-height: 0;
}
</style>
