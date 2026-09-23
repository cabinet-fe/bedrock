<script setup lang="ts">
import type { KanbanBoard, ProjectIssue } from "@/api/types";
import {
  BUG_PRIORITY_LABEL,
  BUG_PRIORITY_TAG,
  BUG_SEVERITY_LABEL,
  BUG_SEVERITY_TAG,
  tagType,
} from "@/lib/tag";

function bugSeverityLabel(value: string | undefined): string {
  if (!value) return "";
  return BUG_SEVERITY_LABEL[value] ?? value;
}

defineOptions({ name: "KanbanBoard" });

const props = withDefaults(
  defineProps<{
    board: KanbanBoard | null;
    /** 是否允许拖拽流转(编辑权限) */
    draggable?: boolean;
    /** 卡片是否展示严重程度(缺陷) */
    showSeverity?: boolean;
    /** 卡片是否展示所属项目(跨项目看板) */
    showProject?: boolean;
    loading?: boolean;
  }>(),
  { draggable: false, showSeverity: false, showProject: false, loading: false },
);

const emit = defineEmits<{
  (e: "card-click", issue: ProjectIssue): void;
  (e: "transition", issue: ProjectIssue, status: string): void;
}>();

let draggingIssue: ProjectIssue | null = null;

function onDragStart(issue: ProjectIssue, event: DragEvent) {
  draggingIssue = issue;
  event.dataTransfer?.setData("text/plain", String(issue.id));
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
  }
}

function onDragEnd() {
  draggingIssue = null;
}

function onDrop(status: string) {
  const issue = draggingIssue;
  draggingIssue = null;
  if (issue && issue.status !== status) {
    emit("transition", issue, status);
  }
}

function assigneeLabel(issue: ProjectIssue): string {
  return issue.assignee_name || issue.assignee_username || "未指派";
}

function cardTag(issue: string): string | undefined {
  const parts = (issue ?? "").split(/[,，\s]+/).filter(Boolean);
  return parts.slice(0, 3).join(" · ");
}
</script>

<template>
  <div class="kanban-board" :class="{ 'is-loading': loading }">
    <div v-if="!board || board.columns.length === 0" class="kanban-empty">
      {{ loading ? "看板加载中…" : "暂无看板数据" }}
    </div>
    <div v-else class="kanban-columns">
      <div
        v-for="column in board.columns"
        :key="column.status"
        class="kanban-column"
        :class="{ 'is-terminal': column.terminal }"
        @dragover.prevent
        @drop.prevent="onDrop(column.status)"
      >
        <div class="kanban-column-header">
          <span class="kanban-column-title">
            {{ column.label }}
            <span v-if="column.terminal" class="kanban-terminal-mark">终态</span>
          </span>
          <span class="kanban-column-count">{{ column.cards.length }}</span>
        </div>
        <div class="kanban-column-body">
          <div
            v-for="card in column.cards"
            :key="card.id"
            class="kanban-card"
            :draggable="draggable"
            @dragstart="onDragStart(card, $event)"
            @dragend="onDragEnd"
            @click="emit('card-click', card)"
          >
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
              <span class="kanban-card-comments" v-if="card.comment_count">
                💬 {{ card.comment_count }}
              </span>
            </div>
            <div class="kanban-card-footer">
              <span class="kanban-card-project" v-if="showProject && card.project_name">
                {{ card.project_name }}
              </span>
              <span v-if="cardTag(card.tags ?? '')" class="kanban-card-tags">
                {{ cardTag(card.tags ?? "") }}
              </span>
              <span class="kanban-card-assignee" :title="assigneeLabel(card)">
                {{ assigneeLabel(card) }}
              </span>
            </div>
          </div>
          <div v-if="column.cards.length === 0" class="kanban-column-placeholder">
            {{ draggable ? "拖拽卡片到这里" : "暂无工作项" }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.kanban-board {
  height: 100%;
  min-height: 0;
  overflow: auto;
  padding: fn.use-var(spacing, 3);
}

.kanban-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: fn.use-var(color, text-secondary);
}

.kanban-columns {
  display: flex;
  align-items: flex-start;
  gap: fn.use-var(spacing, 3);
  min-height: 100%;
}

.kanban-column {
  display: flex;
  flex-direction: column;
  width: 280px;
  min-width: 280px;
  max-height: 100%;
  border-radius: fn.use-var(radius, md);
  background: fn.use-var(color, fill-light);
  padding: fn.use-var(spacing, 2);
}

.kanban-column.is-terminal {
  opacity: 0.85;
}

.kanban-column-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: fn.use-var(spacing, 1) fn.use-var(spacing, 2);
  font-weight: 600;
}

.kanban-terminal-mark {
  margin-left: 4px;
  font-size: 11px;
  font-weight: 400;
  color: fn.use-var(color, text-secondary);
}

.kanban-column-count {
  min-width: 20px;
  text-align: center;
  border-radius: 10px;
  background: fn.use-var(color, fill);
  color: fn.use-var(color, text-secondary);
  font-size: 12px;
  padding: 0 6px;
}

.kanban-column-body {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(spacing, 2);
  overflow-y: auto;
  padding: fn.use-var(spacing, 2);
  min-height: 60px;
}

.kanban-column-placeholder {
  border: 1px dashed fn.use-var(color, border);
  border-radius: fn.use-var(radius, sm);
  color: fn.use-var(color, text-secondary);
  font-size: 12px;
  text-align: center;
  padding: fn.use-var(spacing, 4) fn.use-var(spacing, 2);
}

.kanban-card {
  background: fn.use-var(color, bg);
  border: 1px solid fn.use-var(color, border-light);
  border-radius: fn.use-var(radius, sm);
  padding: fn.use-var(spacing, 2);
  cursor: pointer;
  transition: box-shadow 0.15s ease;

  &:hover {
    box-shadow: fn.use-var(shadow, sm);
  }
}

.kanban-card[draggable="true"] {
  cursor: grab;
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
