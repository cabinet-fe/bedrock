<script setup lang="ts">
defineOptions({ name: "DashboardAgentRunCard" });

import { ArrowRight, Books, CircleCheck, Queue, Refresh } from "@veltra/icons/normal";

import type { AgentRunSummary, DashboardRecentAgentRun } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import {
  JOB_STATUS_TAG,
  TRIGGER_TYPE_TAG,
  jobStatusLabel,
  tagType,
  triggerTypeLabel,
} from "@/lib/tag";

const props = defineProps<{
  data: AgentRunSummary | null;
}>();

const emit = defineEmits<{
  openRun: [id: number];
  openJobs: [];
}>();

function metric(value: number | undefined): string {
  return value == null ? "—" : String(value);
}

function successRate(): string {
  if (!props.data) return "—";
  return `${props.data.success_rate.toFixed(1)}%`;
}

function openRun(run: DashboardRecentAgentRun) {
  emit("openRun", run.id);
}
</script>

<template>
  <u-card class="tile">
    <u-card-header class="tile__header">
      <div class="tile__title-row">
        <span class="tile__icon" aria-hidden="true">
          <u-icon :size="16" color="primary"><Books /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">智能体运行摘要</h3>
        </div>
        <div v-if="data?.running" class="tile__active-badge">
          <span class="tile__pulse-dot" />
          <span>{{ data.running }} 运行中</span>
        </div>
        <button type="button" class="tile__link-btn" title="查看智能体" @click="emit('openJobs')">
          <span>管理智能体</span>
          <u-icon :size="12"><ArrowRight /></u-icon>
        </button>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div class="tile__stats">
        <div class="stat-pill" :class="{ 'stat-pill--active': (data?.running ?? 0) > 0 }">
          <span class="stat-pill__label">
            <u-icon :size="12"><Refresh /></u-icon>
            运行中
          </span>
          <strong class="stat-pill__val">{{ metric(data?.running) }}</strong>
        </div>
        <div class="stat-pill" :class="{ 'stat-pill--queued': (data?.queued ?? 0) > 0 }">
          <span class="stat-pill__label">
            <u-icon :size="12"><Queue /></u-icon>
            排队
          </span>
          <strong class="stat-pill__val">{{ metric(data?.queued) }}</strong>
        </div>
        <div class="stat-pill stat-pill--rate">
          <span class="stat-pill__label">
            <u-icon :size="12"><CircleCheck /></u-icon>
            成功率
          </span>
          <strong class="stat-pill__val">{{ successRate() }}</strong>
        </div>
      </div>

      <div class="tile__tasks">
        <div class="tile__tasks-head">
          <span class="tile__tasks-title">当前活跃运行</span>
          <span v-if="data?.recent?.length" class="tile__tasks-count"
            >{{ data.recent.length }} 项</span
          >
        </div>
        <ul v-if="data?.recent?.length" class="task-list">
          <li v-for="run in data.recent" :key="run.id">
            <button type="button" class="task-row" @click="openRun(run)">
              <span class="task-row__name" :title="run.agent_name || `#${run.id}`">
                {{ run.agent_name || `#${run.id}` }}
              </span>
              <u-tag size="small" :type="tagType(run.status, JOB_STATUS_TAG)">
                {{ jobStatusLabel(run.status) }}
              </u-tag>
              <u-tag size="small" :type="tagType(run.trigger_type, TRIGGER_TYPE_TAG)">
                {{ triggerTypeLabel(run.trigger_type) }}
              </u-tag>
              <span class="task-row__time">{{ formatDateTime(run.created_at) || "—" }}</span>
            </button>
          </li>
        </ul>
        <div v-else class="tile__idle-state">
          <div class="tile__idle-badge">
            <span class="tile__idle-dot" />
            <span>执行队列空闲 · 暂无活跃运行</span>
          </div>
          <p class="tile__idle-tip">任务触发后将在此实时展示执行进度与状态</p>
          <button type="button" class="tile__idle-action" @click="emit('openJobs')">
            <span>前往智能体中心</span>
            <u-icon :size="12"><ArrowRight /></u-icon>
          </button>
        </div>
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

.tile__active-badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(color, primary) 15%, transparent);
  color: fn.use-var(color, primary);
  font-size: 11px;
  font-weight: 600;
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

.tile__pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: fn.use-var(color, primary);
  animation: pulse 1.6s infinite ease-in-out;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.4;
    transform: scale(0.8);
  }
}

.tile__body {
  flex: 1;
  display: flex;
  gap: 12px;
  padding: 8px 14px 10px;
  min-height: 0;
  overflow: hidden;
}

.tile__stats {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 6px;
  width: 125px;
  flex-shrink: 0;
}

.stat-pill {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 8px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 65%, transparent);
  color: inherit;
  font-size: 11px;
  text-align: left;

  &--active {
    background: color-mix(in srgb, fn.use-var(color, primary) 12%, transparent);
    .stat-pill__val {
      color: fn.use-var(color, primary);
    }
  }

  &--queued {
    .stat-pill__val {
      color: fn.use-var(color, warning);
    }
  }

  &--rate {
    .stat-pill__val {
      color: fn.use-var(color, success);
    }
  }
}

.stat-pill__label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: fn.use-var(text-color, second);
}

.stat-pill__val {
  color: fn.use-var(text-color, title);
  font-size: 13px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.tile__tasks {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 6px 10px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 50%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 60%, transparent);
}

.tile__tasks-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.tile__tasks-title {
  color: fn.use-var(text-color, title);
  font-size: 12px;
  font-weight: 600;
}

.tile__tasks-count {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.task-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-y: auto;
  min-height: 0;
  flex: 1;
}

.task-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 4px 6px;
  border: 0;
  border-radius: fn.use-var(radius, small);
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
  }
}

.task-row__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, title);
  font-weight: 600;
  font-size: 12px;
}

.task-row__time {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.tile__idle-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 4px;
}

.tile__idle-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: fn.use-var(text-color, second);
  padding: 3px 8px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 70%, transparent);
}

.tile__idle-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: fn.use-var(color, success);
  box-shadow: 0 0 6px fn.use-var(color, success);
}

.tile__idle-tip {
  margin: 0;
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  text-align: center;
}

.tile__idle-action {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 60%, transparent);
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, top) 80%, transparent);
  color: fn.use-var(color, primary);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;

  &:hover {
    background: color-mix(in srgb, fn.use-var(color, primary) 10%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 40%, transparent);
  }
}

@container (max-width: 360px) {
  .tile__body {
    flex-direction: column;
    overflow-y: auto;
  }
  .tile__stats {
    width: 100%;
    flex-direction: row;
  }
  .stat-pill {
    flex: 1;
  }
}
</style>
