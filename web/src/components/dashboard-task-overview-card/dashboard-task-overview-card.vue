<script setup lang="ts">
defineOptions({ name: "DashboardTaskOverviewCard" });

import { Build, Layers, Terminal } from "@veltra/icons/normal";

import type { TaskOverview } from "@/api/types";

const props = defineProps<{
  data: TaskOverview | null;
}>();

const emit = defineEmits<{
  openBuildJobs: [];
  openScriptJobs: [];
  openPipelines: [];
}>();

const ITEMS = [
  {
    key: "build_jobs" as const,
    label: "构建任务",
    icon: Build,
    event: "openBuildJobs" as const,
  },
  {
    key: "script_jobs" as const,
    label: "脚本任务",
    icon: Terminal,
    event: "openScriptJobs" as const,
  },
  {
    key: "pipelines" as const,
    label: "流水线",
    icon: Layers,
    event: "openPipelines" as const,
  },
];

function count(key: "build_jobs" | "script_jobs" | "pipelines"): string {
  if (!props.data) return "—";
  const value = props.data[key];
  return value == null ? "—" : String(value);
}

function isVisible(key: "build_jobs" | "script_jobs" | "pipelines"): boolean {
  return props.data?.[key] != null;
}

function onClick(event: "openBuildJobs" | "openScriptJobs" | "openPipelines") {
  emit(event);
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
          <h3 class="tile__title">任务概览</h3>
          <p class="tile__subtitle">CI/CD 任务总量一览</p>
        </div>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div class="stats">
        <button
          v-for="item in ITEMS"
          :key="item.key"
          type="button"
          class="stat"
          :class="{ 'stat--disabled': !isVisible(item.key) }"
          :disabled="!isVisible(item.key)"
          @click="onClick(item.event)"
        >
          <span class="stat__icon" aria-hidden="true">
            <u-icon :size="16"><component :is="item.icon" /></u-icon>
          </span>
          <span class="stat__label">{{ item.label }}</span>
          <strong class="stat__value">{{ count(item.key) }}</strong>
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

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 10px;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding: 16px 14px;
  border: 0;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 70%, transparent);
  color: inherit;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease;

  &:hover:not(:disabled) {
    background: fn.use-var(bg-color, hover);
  }

  &--disabled {
    cursor: default;
    opacity: 0.55;
  }
}

.stat__icon {
  color: fn.use-var(text-color, assist);
}

.stat__label {
  color: fn.use-var(text-color, second);
  font-size: 12px;
  letter-spacing: 0.04em;
}

.stat__value {
  color: fn.use-var(text-color, title);
  font-size: clamp(28px, 8cqw, 40px);
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
  letter-spacing: -0.02em;
}
</style>
