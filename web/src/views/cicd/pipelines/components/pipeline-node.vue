<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";

export interface PipelineNodeData {
  build_job_id: number;
  label?: string;
  status?: string;
}

defineProps<{
  data: PipelineNodeData;
}>();
</script>

<template>
  <div class="pipeline-node" :data-status="data.status || ''">
    <Handle type="target" :position="Position.Left" />
    <div class="pipeline-node__label">{{ data.label || `Job #${data.build_job_id}` }}</div>
    <div class="pipeline-node__meta">#{{ data.build_job_id }}</div>
    <div v-if="data.status" class="pipeline-node__status">{{ data.status }}</div>
    <Handle type="source" :position="Position.Right" />
  </div>
</template>

<style scoped lang="scss">
.pipeline-node {
  min-width: 140px;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--u-border-color, #d0d0d0);
  background: var(--u-bg-color, #fff);
  font-size: 12px;
  box-shadow: 0 1px 2px rgb(0 0 0 / 6%);

  &[data-status="success"] {
    border-color: var(--u-color-success, #22c55e);
    background: color-mix(in srgb, var(--u-color-success, #22c55e) 12%, white);
  }
  &[data-status="failed"],
  &[data-status="cancelled"],
  &[data-status="interrupted"] {
    border-color: var(--u-color-danger, #ef4444);
    background: color-mix(in srgb, var(--u-color-danger, #ef4444) 10%, white);
  }
  &[data-status="running"],
  &[data-status="queued"] {
    border-color: var(--u-color-primary, #3b82f6);
    background: color-mix(in srgb, var(--u-color-primary, #3b82f6) 10%, white);
  }
  &[data-status="skipped"],
  &[data-status="pending"] {
    opacity: 0.75;
  }
}

.pipeline-node__label {
  font-weight: 600;
  margin-bottom: 2px;
}

.pipeline-node__meta,
.pipeline-node__status {
  color: var(--u-text-color-secondary, #666);
}
</style>
