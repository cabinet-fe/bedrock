<script setup lang="ts">
defineOptions({ name: "DashboardMyProjectsCard" });

import { Folder } from "@veltra/icons/normal";

import type { MyProject, ProjectRole } from "@/api/types";
import { type TagType } from "@/lib/tag";

const props = defineProps<{
  data: MyProject[] | null;
}>();

const emit = defineEmits<{
  openProject: [id: number];
}>();

const ROLE_TAG: Record<ProjectRole, TagType> = {
  owner: "primary",
  admin: "info",
  member: undefined,
  readonly: "warning",
};

const ROLE_LABEL: Record<ProjectRole, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  readonly: "Readonly",
};

function openProject(project: MyProject) {
  emit("openProject", project.id);
}
</script>

<template>
  <u-card class="tile">
    <u-card-header class="tile__header">
      <div class="tile__title-row">
        <span class="tile__icon" aria-hidden="true">
          <u-icon :size="18" color="primary"><Folder /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">我的项目</h3>
          <p class="tile__subtitle">最近参与的产品项目</p>
        </div>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <ul v-if="data?.length" class="list">
        <li v-for="project in data" :key="project.id">
          <button type="button" class="list__row" @click="openProject(project)">
            <span class="list__name">{{ project.name }}</span>
            <u-tag size="small" :type="project.status === 'archived' ? 'warning' : 'success'">
              {{ project.status === "archived" ? "已归档" : "活跃" }}
            </u-tag>
            <u-tag v-if="project.my_role" size="small" :type="ROLE_TAG[project.my_role]">
              {{ ROLE_LABEL[project.my_role] }}
            </u-tag>
            <span class="list__slug">{{ project.slug }}</span>
          </button>
        </li>
      </ul>
      <p v-else class="list__empty">暂无项目</p>
    </u-card-content>
  </u-card>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.tile {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  container-type: inline-size;
  background: color-mix(in srgb, fn.use-var(bg-color, top) 88%, fn.use-var(color, primary) 4%);
}

.tile__header {
  padding-bottom: 0;
}

.tile__title-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.tile__icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(color, primary) 22%, transparent);
}

.tile__titles {
  min-width: 0;
}

.tile__title {
  margin: 0;
  color: fn.use-var(text-color, title);
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.tile__subtitle {
  margin: 4px 0 0;
  color: fn.use-var(text-color, assist);
  font-size: 12px;
}

.tile__body {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: auto;
}

.list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.list__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto minmax(0, 1fr);
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  border-radius: fn.use-var(radius, default);
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
  }
}

.list__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, title);
  font-weight: 600;
}

.list__slug {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  text-align: right;
}

.list__empty {
  margin: 0;
  padding: 16px 4px;
  color: fn.use-var(text-color, assist);
  font-size: 13px;
}
</style>
