<script setup lang="ts">
defineOptions({ name: "DashboardMyProjectsCard" });

import { ArrowRight, CirclePlus, Folder } from "@veltra/icons/normal";

import type { MyProject, ProjectRole } from "@/api/types";
import { type TagType } from "@/lib/tag";

const props = defineProps<{
  data: MyProject[] | null;
}>();

const emit = defineEmits<{
  openProject: [id: number];
  openProjects: [];
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
          <u-icon :size="16" color="primary"><Folder /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">我的项目</h3>
        </div>
        <span class="tile__count">{{ data?.length || 0 }} 个项目</span>
        <button
          type="button"
          class="tile__link-btn"
          title="查看全部项目"
          @click="emit('openProjects')"
        >
          <span>全部项目</span>
          <u-icon :size="12"><ArrowRight /></u-icon>
        </button>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div v-if="data?.length" class="project-grid">
        <button
          v-for="project in data"
          :key="project.id"
          type="button"
          class="project-card"
          @click="openProject(project)"
        >
          <div class="project-card__header">
            <span class="project-card__name" :title="project.name">{{ project.name }}</span>
            <span
              class="project-card__dot"
              :class="{ 'project-card__dot--archived': project.status === 'archived' }"
              :title="project.status === 'archived' ? '已归档' : '活跃'"
            />
          </div>
          <div class="project-card__meta">
            <u-tag
              v-if="project.my_role"
              size="small"
              :type="ROLE_TAG[project.my_role]"
              class="project-card__role"
            >
              {{ ROLE_LABEL[project.my_role] }}
            </u-tag>
            <span class="project-card__slug" :title="project.slug">{{ project.slug }}</span>
          </div>
        </button>
        <button
          v-if="data.length < 4"
          type="button"
          class="project-card project-card--add"
          @click="emit('openProjects')"
        >
          <u-icon :size="16"><CirclePlus /></u-icon>
          <span>新建/管理项目</span>
        </button>
      </div>
      <div v-else class="tile__empty">
        <p>暂无关联项目</p>
        <button type="button" class="tile__empty-btn" @click="emit('openProjects')">
          <u-icon :size="14"><CirclePlus /></u-icon>
          <span>前往项目中心</span>
        </button>
      </div>
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
  padding: 10px 14px 0;
}

.tile__title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tile__icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(color, primary) 18%, transparent);
}

.tile__titles {
  flex: 1;
  min-width: 0;
}

.tile__title {
  margin: 0;
  color: fn.use-var(text-color, title);
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.tile__count {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.tile__link-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border: 0;
  border-radius: fn.use-var(radius, small);
  background: transparent;
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  cursor: pointer;
  transition:
    color 0.15s ease,
    background 0.15s ease;

  &:hover {
    color: fn.use-var(color, primary);
    background: color-mix(in srgb, fn.use-var(color, primary) 10%, transparent);
  }
}

.tile__body {
  flex: 1;
  padding: 8px 14px 10px;
  min-height: 0;
  overflow-y: auto;
}

.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 8px;
}

.project-card {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 6px;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 70%, transparent);
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 60%, transparent);
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition:
    background 0.15s ease,
    border-color 0.15s ease,
    transform 0.12s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 45%, transparent);
    transform: translateY(-1px);
  }

  &--add {
    align-items: center;
    justify-content: center;
    gap: 6px;
    border-style: dashed;
    border-color: color-mix(in srgb, fn.use-var(border, muted) 90%, transparent);
    color: fn.use-var(text-color, assist);
    font-size: 11px;
    min-height: 52px;

    &:hover {
      color: fn.use-var(color, primary);
      border-color: color-mix(in srgb, fn.use-var(color, primary) 50%, transparent);
      background: color-mix(in srgb, fn.use-var(color, primary) 6%, transparent);
    }
  }
}

.project-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.project-card__name {
  color: fn.use-var(text-color, title);
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-card__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: fn.use-var(color, success);

  &--archived {
    background: fn.use-var(color, warning);
  }
}

.project-card__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  min-width: 0;
}

.project-card__role {
  font-size: 10px;
  line-height: 1.2;
}

.project-card__slug {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, assist);
  font-size: 10px;
  text-align: right;
}

.tile__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 100%;
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  margin: 0;

  p {
    margin: 0;
  }
}

.tile__empty-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 80%, transparent);
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, top) 70%, transparent);
  color: fn.use-var(color, primary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 45%, transparent);
  }
}
</style>
