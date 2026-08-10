<script setup lang="ts">
defineOptions({ name: "DashboardPipelineRunCard" });

import { Layers, Queue, Refresh, CircleCheck } from "@veltra/icons/normal";

import type { DashboardRecentPipelineRun, PipelineRunSummary } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import { JOB_STATUS_TAG, tagType } from "@/lib/tag";

const props = defineProps<{
  data: PipelineRunSummary | null;
}>();

const emit = defineEmits<{
  openRun: [id: number];
  showRunning: [];
}>();

const STATUS_LABEL: Record<string, string> = {
  queued: "排队",
  pending: "等待",
  running: "运行中",
  success: "成功",
  failed: "失败",
  cancelled: "已取消",
  interrupted: "中断",
};

function statusLabel(status: string): string {
  return STATUS_LABEL[status] ?? status;
}

function metric(value: number | undefined): string {
  return value == null ? "—" : String(value);
}

function successRate(): string {
  if (!props.data) return "—";
  return `${props.data.success_rate.toFixed(1)}%`;
}

function openRun(run: DashboardRecentPipelineRun) {
  emit("openRun", run.id);
}
</script>

<template>
  <u-card class="tile">
    <u-card-header class="tile__header">
      <div class="tile__title-row">
        <span class="tile__icon" aria-hidden="true">
          <u-icon :size="18" color="primary"><Layers /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">流水线运行摘要</h3>
          <p class="tile__subtitle">近期流水线编排执行吞吐与结果</p>
        </div>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div class="metrics">
        <button type="button" class="metric metric--clickable" @click="emit('showRunning')">
          <span class="metric__icon" aria-hidden="true">
            <u-icon :size="14"><Refresh /></u-icon>
          </span>
          <span class="metric__label">运行中</span>
          <strong class="metric__value">{{ metric(data?.running) }}</strong>
        </button>
        <div class="metric">
          <span class="metric__icon" aria-hidden="true">
            <u-icon :size="14"><Queue /></u-icon>
          </span>
          <span class="metric__label">排队</span>
          <strong class="metric__value">{{ metric(data?.queued) }}</strong>
        </div>
        <div class="metric metric--accent">
          <span class="metric__icon" aria-hidden="true">
            <u-icon :size="14"><CircleCheck /></u-icon>
          </span>
          <span class="metric__label">成功率</span>
          <strong class="metric__value">{{ successRate() }}</strong>
        </div>
      </div>

      <div class="recent">
        <div class="recent__head">
          <span class="recent__title">近期运行</span>
        </div>
        <ul v-if="data?.recent?.length" class="recent__list">
          <li v-for="run in data.recent" :key="run.id">
            <button type="button" class="recent__row" @click="openRun(run)">
              <span class="recent__name">{{ run.pipeline_name || `#${run.run_number}` }}</span>
              <u-tag size="small" :type="tagType(run.status, JOB_STATUS_TAG)">
                {{ statusLabel(run.status) }}
              </u-tag>
              <span class="recent__num">#{{ run.run_number }}</span>
              <span class="recent__time">{{ formatDateTime(run.created_at) || "—" }}</span>
            </button>
          </li>
        </ul>
        <p v-else class="recent__empty">暂无近期流水线运行记录</p>
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
  gap: 20px;
  min-height: 0;
  overflow: auto;
}

.metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
}

.metric {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  padding: 14px 12px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 70%, transparent);

  &--accent .metric__value {
    color: fn.use-var(color, primary);
  }

  &--clickable {
    border: 0;
    color: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 0.15s ease;

    &:hover {
      background: fn.use-var(bg-color, hover);
    }
  }
}

.metric__icon {
  color: fn.use-var(text-color, assist);
}

.metric__label {
  color: fn.use-var(text-color, second);
  font-size: 12px;
  letter-spacing: 0.04em;
}

.metric__value {
  color: fn.use-var(text-color, title);
  font-size: clamp(24px, 6cqw, 34px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
  letter-spacing: -0.02em;
}

.recent {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.recent__head {
  margin-bottom: 10px;
}

.recent__title {
  color: fn.use-var(text-color, second);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.recent__list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.recent__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto auto;
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

.recent__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, title);
  font-weight: 600;
}

.recent__num {
  color: fn.use-var(text-color, second);
  font-variant-numeric: tabular-nums;
}

.recent__time {
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.recent__empty {
  margin: 0;
  padding: 16px 4px;
  color: fn.use-var(text-color, assist);
  font-size: 13px;
}
</style>
