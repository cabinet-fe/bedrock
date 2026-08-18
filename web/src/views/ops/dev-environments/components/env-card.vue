<script setup lang="ts">
defineOptions({ name: "DevEnvCard" });

import type { DevEnvJob, DevEnvironment } from "@/api/types";
import defaultIcon from "@/assets/dev-env/default.svg";
import goIcon from "@/assets/dev-env/go.svg";
import javaIcon from "@/assets/dev-env/java.svg";
import nodeIcon from "@/assets/dev-env/nodejs.svg";
import pythonIcon from "@/assets/dev-env/python.svg";

export type DetectState = {
  status: "loading" | "detected" | "missing" | "error";
  version?: string;
  output?: string;
};

const ENV_ICONS: Record<string, string> = {
  go: goIcon,
  node: nodeIcon,
  java: javaIcon,
  python: pythonIcon,
  python3: pythonIcon,
};

const { item, detect, job } = defineProps<{
  item: DevEnvironment;
  detect?: DetectState;
  job?: DevEnvJob;
}>();

const emit = defineEmits<{
  sources: [];
  detect: [];
  install: [];
  upgrade: [];
  uninstall: [];
  switch: [];
  scripts: [];
  edit: [];
  remove: [];
  log: [];
  retry: [];
}>();

function envIcon(target: DevEnvironment): string {
  const exe = target.executable.toLowerCase();
  if (ENV_ICONS[exe]) return ENV_ICONS[exe];
  const name = target.name.toLowerCase();
  if (name.includes("node")) return nodeIcon;
  if (name.includes("python")) return pythonIcon;
  if (name.includes("java")) return javaIcon;
  if (name === "go" || name.includes("golang")) return goIcon;
  return defaultIcon;
}

function versionTagType(state?: DetectState) {
  if (state?.status === "detected") return "success";
  if (state?.status === "missing") return "warning";
  if (state?.status === "error") return "danger";
  return "info";
}

function versionTagLabel(state?: DetectState) {
  if (!state || state.status === "loading") return "检测中…";
  if (state.status === "detected") return state.version || "已安装";
  if (state.status === "missing") return "未安装";
  return "检测失败";
}
</script>

<template>
  <u-card class="env-card">
    <u-card-content>
      <div class="env-card-body">
        <header class="card-head">
          <div class="title-row">
            <img class="lang-icon" :src="envIcon(item)" :alt="item.name" width="28" height="28" />
            <h3>{{ item.name }}</h3>
            <u-tag size="small" :type="versionTagType(detect)">
              {{ versionTagLabel(detect) }}
            </u-tag>
          </div>
          <p class="meta">
            <span>{{ item.kind === "builtin" ? "内置" : "自定义" }}</span>
            <span>{{ item.executable }}</span>
            <span v-if="item.default_version">默认 {{ item.default_version }}</span>
          </p>
          <p class="desc">{{ item.description }}</p>
          <div class="actions">
            <u-action-group :max="5">
              <u-action @run="emit('sources')">设置</u-action>
              <u-action @run="emit('detect')">检测</u-action>
              <u-action @run="emit('install')">安装</u-action>
              <u-action @run="emit('upgrade')">升级</u-action>
              <u-action @run="emit('uninstall')">卸载</u-action>
              <u-action @run="emit('switch')">切版本</u-action>
              <u-action @run="emit('scripts')">脚本</u-action>
              <u-action v-if="item.kind === 'custom'" @run="emit('edit')">编辑</u-action>
              <u-action v-if="item.kind === 'custom'" type="danger" @run="emit('remove')"
                >删除</u-action
              >
            </u-action-group>
          </div>
        </header>

        <section class="block">
          <div class="block-head">
            <h4>最近任务</h4>
            <div class="actions">
              <span class="job-status">{{ job?.status || "暂无任务" }}</span>
              <u-action-group v-if="job" :max="2">
                <u-action @run="emit('log')">日志</u-action>
                <u-action v-if="['failed', 'interrupted'].includes(job.status)" @run="emit('retry')"
                  >重试</u-action
                >
              </u-action-group>
            </div>
          </div>
          <p class="job-summary">
            <template v-if="job">
              {{ job.operation }}
              <template v-if="job.requested_version"> · {{ job.requested_version }}</template>
            </template>
            <template v-else>安装、升级或切版本后将显示在这里</template>
          </p>
        </section>
      </div>
    </u-card-content>
  </u-card>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.env-card {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-width: 0;

  :deep(.u-card__content) {
    display: flex;
    flex: 1;
    flex-direction: column;
    height: 100%;
  }
}
.env-card-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-width: 0;
}
.card-head {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.lang-icon {
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  object-fit: contain;
}
.card-head h3 {
  margin: 0;
  font-size: 16px;
  line-height: 1.4;
}
.actions {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.meta,
.desc,
.job-summary {
  margin: 4px 0 0;
  color: fn.use-var(text-color, second);
  font-size: 13px;
  line-height: 1.4;
}
.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.desc {
  display: -webkit-box;
  min-height: calc(13px * 1.4 * 2);
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.block {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: auto;
  padding-top: 12px;
  border-top: fn.use-var(border, muted);
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.block-head h4 {
  margin: 0;
  font-size: 14px;
}
.job-status {
  font-size: 12px;
  color: fn.use-var(text-color, main);
}
</style>
