<script setup lang="ts">
defineOptions({ name: "ProjectCard" });

import { computed } from "vue";

import type { ProductProject, ProjectRole } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import { type TagType } from "@/lib/tag";

const props = defineProps<{
  project: ProductProject;
  loading?: boolean;
}>();

const emit = defineEmits<{
  enter: [];
  members: [];
  edit: [];
  archive: [];
  remove: [];
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

const tags = computed(() => {
  const raw = props.project.tags;
  if (!raw?.trim()) return [];
  return raw
    .split(/[,，]/)
    .map((t) => t.trim())
    .filter(Boolean);
});

const role = computed(() => props.project.my_role);
const canEdit = computed(() => !!props.project.permissions?.update);
const canArchive = computed(
  () => !!props.project.permissions?.archive && props.project.status === "active",
);
const canDelete = computed(() => !!props.project.permissions?.delete);

function onBodyClick() {
  emit("enter");
}
</script>

<template>
  <u-card class="project-card" @click="onBodyClick">
    <u-card-content class="project-card__body">
      <header class="project-card__head">
        <div class="project-card__title-row">
          <h3 class="project-card__name" :title="project.name">{{ project.name }}</h3>
          <u-tag size="small" :type="project.status === 'archived' ? 'warning' : 'success'">
            {{ project.status === "archived" ? "已归档" : "活跃" }}
          </u-tag>
          <u-tag v-if="role" size="small" :type="ROLE_TAG[role]">
            {{ ROLE_LABEL[role] }}
          </u-tag>
        </div>
        <div class="project-card__menu" @click.stop>
          <u-action-group :loading="loading">
            <u-action in-dropdown @run="emit('enter')">进入</u-action>
            <u-action in-dropdown @run="emit('members')">成员</u-action>
            <u-action v-if="canEdit" in-dropdown @run="emit('edit')">编辑</u-action>
            <u-action v-if="canArchive" in-dropdown need-confirm @run="emit('archive')">
              归档
            </u-action>
            <u-action v-if="canDelete" in-dropdown need-confirm type="danger" @run="emit('remove')">
              解散
            </u-action>
          </u-action-group>
        </div>
      </header>

      <p class="project-card__desc">
        {{ project.description?.trim() || "暂无描述" }}
      </p>

      <div v-if="tags.length" class="project-card__tags">
        <u-tag v-for="tag in tags" :key="tag" size="small">{{ tag }}</u-tag>
      </div>

      <footer class="project-card__foot">
        <span class="project-card__slug" :title="project.slug">{{ project.slug }}</span>
        <span class="project-card__time">{{ formatDateTime(project.updated_at) }}</span>
      </footer>
    </u-card-content>
  </u-card>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.project-card {
  height: 100%;
  min-width: 0;
  cursor: pointer;
  transition: box-shadow 0.15s ease;

  &:hover {
    box-shadow: fn.use-var(shadow);
  }
}

.project-card__body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 160px;
}

.project-card__head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.project-card__title-row {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.project-card__name {
  margin: 0;
  max-width: 100%;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-card__menu {
  flex-shrink: 0;
}

.project-card__desc {
  margin: 0;
  min-height: 2.8em;
  font-size: 13px;
  line-height: 1.4;
  color: fn.use-var(text-color, second);
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  word-break: break-word;
}

.project-card__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.project-card__foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}

.project-card__slug {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.project-card__time {
  flex-shrink: 0;
}
</style>
