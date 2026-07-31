<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";
import { Agent, Build, CaretRight, Flag, Terminal } from "@veltra/icons/normal";
import { computed, inject, type Component } from "vue";

import { NODE_TYPE_LABEL, PIPELINE_TARGET_NAMES, type PipelineNodeData } from "../graph";

const props = defineProps<{
  type?: string;
  data: PipelineNodeData;
}>();

const targetNames = inject(PIPELINE_TARGET_NAMES, undefined);

const TYPE_ICON: Record<string, Component> = {
  start: CaretRight,
  end: Flag,
  buildJob: Build,
  scriptJob: Terminal,
  agent: Agent,
};

const TYPE_COLOR: Record<string, string> = {
  start: "var(--u-color-success, #22c55e)",
  end: "var(--u-text-color-secondary, #999)",
  buildJob: "var(--u-color-primary, #3b82f6)",
  scriptJob: "var(--u-color-warning, #f59e0b)",
  agent: "var(--u-color-info, #06b6d4)",
};

const nodeType = computed(() => props.type || "buildJob");
const isPill = computed(() => nodeType.value === "start" || nodeType.value === "end");
const icon = computed(() => TYPE_ICON[nodeType.value] ?? Build);
const typeColor = computed(() => TYPE_COLOR[nodeType.value] ?? TYPE_COLOR.buildJob);
const label = computed(() => props.data.label || NODE_TYPE_LABEL[nodeType.value] || nodeType.value);

const targetId = computed(() => {
  const d = props.data;
  if (nodeType.value === "buildJob") return d.build_job_id;
  if (nodeType.value === "scriptJob") return d.script_job_id;
  if (nodeType.value === "agent") return d.agent_id;
  return undefined;
});

const subtitle = computed(() => {
  const typeLabel = NODE_TYPE_LABEL[nodeType.value] ?? nodeType.value;
  const name = targetId.value
    ? targetNames?.value[`${nodeType.value}:${targetId.value}`]
    : undefined;
  return name ? `${typeLabel} · ${name}` : typeLabel;
});
</script>

<template>
  <div
    class="pipeline-node"
    :class="{ 'pipeline-node--pill': isPill }"
    :data-status="data.status || ''"
    :style="{ '--node-type-color': typeColor }"
  >
    <Handle v-if="nodeType !== 'start'" type="target" :position="Position.Left" />
    <template v-if="isPill">
      <u-icon :size="12" class="pipeline-node__icon"><component :is="icon" /></u-icon>
      <span class="pipeline-node__label">{{ label }}</span>
    </template>
    <template v-else>
      <span class="pipeline-node__bar" />
      <u-icon :size="16" class="pipeline-node__icon"><component :is="icon" /></u-icon>
      <div class="pipeline-node__text">
        <div class="pipeline-node__label">{{ label }}</div>
        <div class="pipeline-node__sub">{{ subtitle }}</div>
      </div>
    </template>
    <Handle v-if="nodeType !== 'end'" type="source" :position="Position.Right" />
  </div>
</template>

<style scoped lang="scss">
.pipeline-node {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 150px;
  padding: 8px 12px 8px 16px;
  border-radius: 8px;
  border: 1px solid var(--u-border-color, #d0d0d0);
  background: var(--u-bg-color, #fff);
  font-size: 12px;
  box-shadow: 0 1px 2px rgb(0 0 0 / 6%);

  &--pill {
    min-width: 0;
    padding: 4px 14px;
    border-radius: 999px;
    font-weight: 600;

    .pipeline-node__label {
      margin: 0;
    }
  }

  &[data-status="success"] {
    border-color: var(--u-color-success, #22c55e);
    background: color-mix(in srgb, var(--u-color-success, #22c55e) 12%, var(--u-bg-color, #fff));
  }
  &[data-status="failed"],
  &[data-status="cancelled"],
  &[data-status="interrupted"] {
    border-color: var(--u-color-danger, #ef4444);
    background: color-mix(in srgb, var(--u-color-danger, #ef4444) 10%, var(--u-bg-color, #fff));
  }
  &[data-status="running"],
  &[data-status="queued"] {
    border-color: var(--u-color-primary, #3b82f6);
    background: color-mix(in srgb, var(--u-color-primary, #3b82f6) 10%, var(--u-bg-color, #fff));
    animation: pipeline-node-breathe 1.6s ease-in-out infinite;
  }
  &[data-status="skipped"],
  &[data-status="pending"] {
    opacity: 0.65;
  }
}

@keyframes pipeline-node-breathe {
  0%,
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--u-color-primary, #3b82f6) 40%, transparent);
  }
  50% {
    box-shadow: 0 0 0 5px color-mix(in srgb, var(--u-color-primary, #3b82f6) 10%, transparent);
  }
}

.pipeline-node__bar {
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 3px;
  background: var(--node-type-color);
}

.pipeline-node__icon {
  color: var(--node-type-color);
  flex-shrink: 0;
}

.pipeline-node__text {
  min-width: 0;
}

.pipeline-node__label {
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pipeline-node__sub {
  margin-top: 1px;
  font-size: 11px;
  color: var(--u-text-color-secondary, #888);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
