<script setup lang="ts">
import { UKanban } from "@veltra/desktop";
import "@veltra/desktop/components/kanban/style.js";
import type { KanbanColumnItem } from "@veltra/desktop";

import { Plus } from "@veltra/icons/normal";

import { ref, watch } from "vue";

import type { KanbanBoard, ProjectIssue } from "@/api/types";
import {
  BUG_PRIORITY_LABEL,
  BUG_PRIORITY_TAG,
  BUG_SEVERITY_LABEL,
  BUG_SEVERITY_TAG,
  tagType,
} from "@/lib/tag";

defineOptions({ name: "KanbanBoard" });

const props = defineProps<{
  board: KanbanBoard | null;
  /** Whether cards can be dragged to change status (edit permission). */
  draggable?: boolean;
  /** Show a quick-create button on each column header (create permission). */
  quickCreate?: boolean;
  /** Show severity tag on cards (bug boards). */
  showSeverity?: boolean;
  /** Show project name on cards (cross-project board). */
  showProject?: boolean;
  loading?: boolean;
}>();

const emit = defineEmits<{
  (e: "card-click", issue: ProjectIssue): void;
  (e: "transition", issue: ProjectIssue, status: string): void;
  (e: "quick-create", status: string): void;
}>();

// The board prop is the single source of truth: in-column order has no
// backend persistence, so local reorders are kept until the next reload.
const columns = ref<KanbanColumnItem[]>([]);

watch(
  () => props.board,
  (board) => {
    columns.value = toColumns(board);
  },
  { immediate: true },
);

function toColumns(board: KanbanBoard | null): KanbanColumnItem[] {
  return (board?.columns ?? []).map((column) => ({
    key: column.status,
    title: column.label,
    terminal: column.terminal,
    items: column.cards,
  }));
}

function onCardClick(card: Record<string, any>) {
  emit("card-click", card as ProjectIssue);
}

function onQuickCreate(column: KanbanColumnItem) {
  emit("quick-create", String(column.key));
}

function onChange(next: KanbanColumnItem[]) {
  const truth = new Map<string, string>();
  for (const column of props.board?.columns ?? []) {
    for (const card of column.cards) truth.set(String(card.id), column.status);
  }

  let moved = false;
  for (const column of next) {
    for (const card of column.items) {
      const from = truth.get(String(card.id));
      if (from !== undefined && from !== column.key) {
        moved = true;
        emit("transition", card as ProjectIssue, column.key);
      }
    }
  }

  // Cross-column drops roll back to server state: the parent reloads the
  // board after a successful transition, a failure keeps the original column.
  if (moved) columns.value = toColumns(props.board);
}

function bugSeverityLabel(value: string | undefined): string {
  if (!value) return "";
  return BUG_SEVERITY_LABEL[value] ?? value;
}

function assigneeLabel(card: Record<string, any>): string {
  return card.assignee_name || card.assignee_username || "未指派";
}

function cardTags(tags: string): string | undefined {
  const parts = (tags ?? "").split(/[,，\s]+/).filter(Boolean);
  return parts.slice(0, 3).join(" · ");
}
</script>

<template>
  <div class="kanban-board">
    <div v-if="!board || board.columns.length === 0" class="kanban-empty">
      {{ loading ? "看板加载中…" : "暂无看板数据" }}
    </div>
    <UKanban
      v-else
      v-model:columns="columns"
      :disabled="!draggable"
      :placeholder="draggable ? '拖拽卡片到这里' : '暂无工作项'"
      @change="onChange"
    >
      <template #header="{ column, count }">
        <span class="kanban-column-title">
          {{ column.title }}<span v-if="column.terminal" class="kanban-terminal-mark">终态</span>
        </span>
        <span class="kanban-column-count">{{ count }}</span>
        <button
          v-if="quickCreate"
          type="button"
          class="kanban-column-add"
          :title="`在此列快捷新建（状态：${column.title}）`"
          @click="onQuickCreate(column)"
        >
          <u-icon :size="14"><Plus /></u-icon>
        </button>
      </template>
      <template #card="{ card }">
        <div class="kanban-card-content" @click="onCardClick(card)">
          <div class="kanban-card-title">{{ card.title }}</div>
          <div class="kanban-card-meta">
            <u-tag
              v-if="showSeverity && card.severity"
              size="small"
              :type="tagType(card.severity, BUG_SEVERITY_TAG)"
            >
              {{ bugSeverityLabel(card.severity) }}
            </u-tag>
            <u-tag size="small" :type="tagType(card.priority, BUG_PRIORITY_TAG)">
              {{ BUG_PRIORITY_LABEL[card.priority] ?? card.priority }}
            </u-tag>
            <span v-if="card.comment_count" class="kanban-card-comments">
              💬 {{ card.comment_count }}
            </span>
          </div>
          <div class="kanban-card-footer">
            <span v-if="showProject && card.project_name" class="kanban-card-project">
              {{ card.project_name }}
            </span>
            <span v-if="cardTags(card.tags ?? '')" class="kanban-card-tags">
              {{ cardTags(card.tags ?? "") }}
            </span>
            <span class="kanban-card-assignee" :title="assigneeLabel(card)">
              {{ assigneeLabel(card) }}
            </span>
          </div>
        </div>
      </template>
    </UKanban>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.kanban-board {
  height: 100%;
  min-height: 0;
}

.kanban-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: fn.use-var(color, text-secondary);
}

// Each column scrolls independently; column headers stay visible
:deep(.u-kanban) {
  height: 100%;
}

:deep(.u-kanban__column) {
  max-height: 100%;
}

:deep(.u-kanban__cards) {
  overflow-y: auto;
}

.kanban-column-title {
  flex: 1 1 0%;
  min-width: 0;
  font-weight: 600;
}

.kanban-terminal-mark {
  margin-left: 4px;
  font-size: 11px;
  font-weight: 400;
  color: fn.use-var(color, text-secondary);
}

.kanban-column-count {
  flex: none;
  min-width: 20px;
  text-align: center;
  border-radius: 10px;
  background: fn.use-var(color, fill);
  color: fn.use-var(color, text-secondary);
  font-size: 12px;
  padding: 0 6px;
}

.kanban-column-add {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  margin: 0 -2px 0 0;
  border: none;
  border-radius: fn.use-var(radius, default);
  background: transparent;
  color: fn.use-var(color, text-secondary);
  cursor: pointer;

  &:hover {
    background: fn.use-var(color, primary);
    color: fn.use-var(color, white);
  }
}

.kanban-card-content {
  cursor: pointer;
}

.kanban-card-title {
  font-size: 13px;
  line-height: 1.5;
  word-break: break-all;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.kanban-card-meta {
  display: flex;
  align-items: center;
  gap: fn.use-var(spacing, 1);
  margin-top: fn.use-var(spacing, 2);
}

.kanban-card-comments {
  font-size: 12px;
  color: fn.use-var(color, text-secondary);
}

.kanban-card-footer {
  display: flex;
  align-items: center;
  gap: fn.use-var(spacing, 2);
  margin-top: fn.use-var(spacing, 2);
  font-size: 12px;
  color: fn.use-var(color, text-secondary);
}

.kanban-card-project {
  max-width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kanban-card-tags {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kanban-card-assignee {
  margin-left: auto;
  flex-shrink: 0;
  max-width: 96px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
