<script setup lang="ts">
defineOptions({ name: "DashboardSystemInfoCard" });

import { computed } from "vue";
import { clipboard } from "@cat-kit/fe";
import { message } from "@veltra/desktop";
import { Copy, Internet, Server, Time, Variable } from "@veltra/icons/normal";

import type { SystemInfo } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";

const props = defineProps<{
  data: SystemInfo | null;
}>();

const platform = computed(() => {
  if (!props.data) return "—";
  return `${props.data.os} / ${props.data.arch}`;
});

const uptime = computed(() => {
  const start = props.data?.start_time;
  if (!start) return "—";
  const ms = Date.now() - new Date(start).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "—";
  const totalHours = Math.floor(ms / 3_600_000);
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  const mins = Math.floor((ms % 3_600_000) / 60_000);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (totalHours > 0) return `${totalHours} 小时 ${mins} 分`;
  return `${mins} 分钟`;
});

async function copyHostname() {
  if (!props.data?.hostname) return;
  try {
    await clipboard.copy(props.data.hostname);
    message.success("已复制主机名");
  } catch {
    message.info(props.data.hostname);
  }
}
</script>

<template>
  <u-card class="tile">
    <u-card-header class="tile__header">
      <div class="tile__title-row">
        <span class="tile__icon" aria-hidden="true">
          <u-icon :size="16" color="primary"><Server /></u-icon>
        </span>
        <div class="tile__titles">
          <h3 class="tile__title">系统信息</h3>
        </div>
      </div>
    </u-card-header>

    <u-card-content class="tile__body">
      <div class="hero">
        <div class="hero__left">
          <span class="hero__label">系统版本</span>
          <span class="hero__version">{{ data?.version || "—" }}</span>
        </div>
        <button
          v-if="data?.hostname"
          type="button"
          class="hero__host"
          title="点击复制主机名"
          @click="copyHostname"
        >
          <u-icon :size="13"><Internet /></u-icon>
          <span>{{ data.hostname }}</span>
          <u-icon :size="11" class="hero__copy-icon"><Copy /></u-icon>
        </button>
      </div>

      <div class="facts">
        <div class="fact">
          <span class="fact__label">
            <u-icon :size="12"><Server /></u-icon>
            平台系统
          </span>
          <span class="fact__value">{{ platform }}</span>
        </div>
        <div class="fact">
          <span class="fact__label">
            <u-icon :size="12"><Variable /></u-icon>
            运行时
          </span>
          <span class="fact__value">{{ data?.runtime || "—" }}</span>
        </div>
        <div class="fact">
          <span class="fact__label">
            <u-icon :size="12"><Time /></u-icon>
            已运行
          </span>
          <span class="fact__value">{{ uptime }}</span>
        </div>
        <div class="fact">
          <span class="fact__label">
            <u-icon :size="12"><Time /></u-icon>
            启动时间
          </span>
          <span class="fact__value">{{ formatDateTime(data?.start_time) || "—" }}</span>
        </div>
      </div>

      <div class="env-badge">
        <span class="env-badge__dot" />
        <span class="env-badge__label">Bedrock Core Engine · 平台服务运行良好</span>
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
  min-width: 0;
}

.tile__title {
  margin: 0;
  color: fn.use-var(text-color, title);
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.tile__body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 8px 14px 12px;
  min-height: 0;
  overflow-y: auto;
}

.hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-radius: fn.use-var(radius, default);
  background: linear-gradient(
    145deg,
    color-mix(in srgb, fn.use-var(color, primary) 16%, transparent),
    color-mix(in srgb, fn.use-var(bg-color, bottom) 80%, transparent) 55%
  );
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 60%, transparent);
}

.hero__left {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.hero__label {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
}

.hero__version {
  color: fn.use-var(text-color, title);
  font-size: 18px;
  font-weight: 650;
  letter-spacing: -0.02em;
}

.hero__host {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 60%, transparent);
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, top) 60%, transparent);
  color: fn.use-var(text-color, second);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s ease;

  &:hover {
    color: fn.use-var(color, primary);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 45%, transparent);
    background: fn.use-var(bg-color, hover);
  }
}

.hero__copy-icon {
  color: fn.use-var(text-color, assist);
}

.facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.fact {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  padding: 8px 10px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 60%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 50%, transparent);
  transition: border-color 0.15s ease;

  &:hover {
    border-color: color-mix(in srgb, fn.use-var(color, primary) 35%, transparent);
  }
}

.fact__label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: fn.use-var(text-color, second);
  font-size: 11px;
  letter-spacing: 0.04em;
}

.fact__value {
  color: fn.use-var(text-color, title);
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.env-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 40%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted) 40%, transparent);
}

.env-badge__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: fn.use-var(color, success);
  box-shadow: 0 0 6px fn.use-var(color, success);
}

.env-badge__label {
  color: fn.use-var(text-color, assist);
  font-size: 11px;
}
</style>
