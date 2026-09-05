<script setup lang="ts">
defineOptions({ name: "DashboardSystemStatusCard" });

import { computed } from "vue";
import type { ColorType } from "@veltra/utils";
import { Folder, Monitor } from "@veltra/icons/normal";

import type { SystemStatus } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";

const props = defineProps<{
  data: SystemStatus | null;
}>();

const HEALTH_META: Record<string, { label: string; type: ColorType }> = {
  ok: { label: "正常", type: "success" },
  degraded: { label: "降级", type: "warning" },
};

const DIR_LABELS: Record<string, string> = {
  workspaces: "工作空间",
  artifacts: "构建制品",
  logs: "运行日志",
  caches: "构建缓存",
};

const healthMeta = computed(() => {
  const key = props.data?.health ?? "";
  return HEALTH_META[key] ?? { label: key || "—", type: "info" as ColorType };
});

const cpuPercent = computed(() => props.data?.cpu_usage_percent ?? 0);
const memPercent = computed(() => props.data?.memory_usage_percent ?? 0);
const diskPercent = computed(() => props.data?.disk_usage_percent ?? 0);

const totalDirsBytes = computed(() => {
  if (!props.data?.directories) return 0;
  return props.data.directories.reduce((acc, dir) => acc + (dir.used_bytes || 0), 0);
});

function loadType(percentage: number): ColorType {
  if (percentage >= 90) return "danger";
  if (percentage >= 70) return "warning";
  return "primary";
}

function formatBytes(value: number | undefined): string {
  if (value == null || !Number.isFinite(value)) return "—";
  if (value === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

function cleanDataDirName(fullPath: string): string {
  const match = fullPath.match(/[/\\]data[/\\]([^/\\]+)$/);
  if (match) return match[1];
  const relMatch = fullPath.match(/(?:^|[/\\])data[/\\]([^/\\]+)$/);
  if (relMatch) return relMatch[1];
  const dataIdx = fullPath.lastIndexOf("data/");
  if (dataIdx !== -1) return fullPath.slice(dataIdx + 5);
  return fullPath.split(/[/\\]/).filter(Boolean).pop() || fullPath;
}

function getDirLabel(name: string): string {
  return DIR_LABELS[name] || "数据目录";
}

function dirPercent(usedBytes: number | undefined): number {
  if (!usedBytes || !totalDirsBytes.value) return 0;
  return Math.min(100, Math.round((usedBytes * 100) / totalDirsBytes.value));
}
</script>

<template>
  <u-card class="tile">
    <u-card-header class="tile__header">
      <div class="tile__title-row">
        <span class="tile__icon" aria-hidden="true">
          <u-icon :size="16" color="primary"><Monitor /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">系统状态</h3>
          <span class="tile__subtitle">
            {{
              data?.collected_at ? `采集于 ${formatDateTime(data.collected_at)}` : "实时资源占用"
            }}
          </span>
        </div>
        <u-tag class="tile__health" size="small" :type="healthMeta.type">
          {{ healthMeta.label }}
        </u-tag>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div class="gauges">
        <div class="gauge">
          <u-progress circle :size="80" :percentage="cpuPercent" :type="loadType">
            <template #default="{ percentage }">
              <span class="gauge__pct">{{ percentage.toFixed(0) }}%</span>
            </template>
          </u-progress>
          <span class="gauge__label">CPU</span>
          <span class="gauge__hint">
            {{ cpuPercent >= 80 ? "高负载" : cpuPercent >= 50 ? "中等负载" : "运行良好" }}
          </span>
        </div>
        <div class="gauge">
          <u-progress circle :size="80" :percentage="memPercent" :type="loadType">
            <template #default="{ percentage }">
              <span class="gauge__pct">{{ percentage.toFixed(0) }}%</span>
            </template>
          </u-progress>
          <span class="gauge__label">内存</span>
          <span class="gauge__hint">
            {{ formatBytes(data?.memory_used_bytes) }} / {{ formatBytes(data?.memory_total_bytes) }}
          </span>
        </div>
        <div class="gauge">
          <u-progress circle :size="80" :percentage="diskPercent" :type="loadType">
            <template #default="{ percentage }">
              <span class="gauge__pct">{{ percentage.toFixed(0) }}%</span>
            </template>
          </u-progress>
          <span class="gauge__label">磁盘</span>
          <span class="gauge__hint">
            {{ formatBytes(data?.disk_used_bytes) }} / {{ formatBytes(data?.disk_total_bytes) }}
          </span>
        </div>
      </div>

      <div class="dir-container">
        <div class="dir-container__head">
          <div class="dir-container__head-left">
            <u-icon :size="14"><Folder /></u-icon>
            <span class="dir-container__title">数据目录占用 (data/)</span>
          </div>
          <span class="dir-container__total">总计: {{ formatBytes(totalDirsBytes) }}</span>
        </div>
        <div v-if="data?.directories?.length" class="dir-grid">
          <div v-for="dir in data.directories" :key="dir.path" class="dir-block" :title="dir.path">
            <div class="dir-block__header">
              <span class="dir-block__name">{{ cleanDataDirName(dir.path) }}</span>
              <span class="dir-block__label">{{ getDirLabel(cleanDataDirName(dir.path)) }}</span>
            </div>
            <div class="dir-block__size-row">
              <span class="dir-block__size">{{ formatBytes(dir.used_bytes) }}</span>
              <span v-if="totalDirsBytes > 0" class="dir-block__pct"
                >{{ dirPercent(dir.used_bytes) }}%</span
              >
            </div>
            <div class="dir-block__bar">
              <div
                class="dir-block__bar-fill"
                :style="{ width: `${dirPercent(dir.used_bytes)}%` }"
              />
            </div>
          </div>
        </div>
        <p v-else class="dir-container__empty">暂无目录采样</p>
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
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.tile__title {
  margin: 0;
  color: fn.use-var(text-color, title);
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.tile__subtitle {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
}

.tile__health {
  flex-shrink: 0;
}

.tile__body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 14px 12px;
  min-height: 0;
  overflow-y: auto;
}

.gauges {
  display: flex;
  align-items: center;
  justify-content: space-around;
  gap: 10px;
  padding: 2px 0 4px;
}

.gauge {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.gauge__pct {
  color: fn.use-var(text-color, title);
  font-size: 16px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}

.gauge__label {
  color: fn.use-var(text-color, second);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.gauge__hint {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-align: center;
}

.dir-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border-radius: fn.use-var(radius, default);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 60%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 70%, transparent);
}

.dir-container__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dir-container__head-left {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: fn.use-var(text-color, second);
}

.dir-container__title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.dir-container__total {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.dir-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

.dir-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 7px 8px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, top) 60%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 40%, transparent);
  transition:
    border-color 0.15s ease,
    background 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 40%, transparent);
  }
}

.dir-block__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 4px;
  min-width: 0;
}

.dir-block__name {
  color: fn.use-var(text-color, title);
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-block__label {
  color: fn.use-var(text-color, assist);
  font-size: 10px;
  flex-shrink: 0;
}

.dir-block__size-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 4px;
}

.dir-block__size {
  color: fn.use-var(text-color, second);
  font-size: 13px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.dir-block__pct {
  color: fn.use-var(text-color, assist);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}

.dir-block__bar {
  width: 100%;
  height: 3px;
  border-radius: 2px;
  background: color-mix(in srgb, fn.use-var(border, muted) 60%, transparent);
  overflow: hidden;
  margin-top: 1px;
}

.dir-block__bar-fill {
  height: 100%;
  border-radius: 2px;
  background: fn.use-var(color, primary);
  transition: width 0.3s ease;
}

.dir-container__empty {
  margin: 0;
  padding: 8px 0;
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  text-align: center;
}

@container (max-width: 440px) {
  .dir-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
