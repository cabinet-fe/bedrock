<script setup lang="ts">
import type { BuildJob } from "@/api/types";

defineProps<{
  jobs: BuildJob[];
}>();

const emit = defineEmits<{
  pick: [job: BuildJob];
}>();

function onDragStart(e: DragEvent, job: BuildJob) {
  e.dataTransfer?.setData("application/bedrock-build-job", String(job.id));
  e.dataTransfer?.setData("text/plain", job.name);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = "copy";
}
</script>

<template>
  <aside class="job-palette">
    <div class="job-palette__title">构建任务</div>
    <div class="job-palette__hint">拖到画布或点击添加</div>
    <ul class="job-palette__list">
      <li
        v-for="job in jobs"
        :key="job.id"
        class="job-palette__item"
        draggable="true"
        @dragstart="onDragStart($event, job)"
        @click="emit('pick', job)"
      >
        <span class="job-palette__name">{{ job.name }}</span>
        <span class="job-palette__meta">#{{ job.id }} · {{ job.branch }}</span>
      </li>
    </ul>
  </aside>
</template>

<style scoped lang="scss">
.job-palette {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid var(--u-border-color, #e5e5e5);
  padding: 12px;
  overflow: auto;
  background: var(--u-bg-color-page, #fafafa);
}

.job-palette__title {
  font-weight: 600;
  margin-bottom: 4px;
}

.job-palette__hint {
  font-size: 12px;
  color: var(--u-text-color-secondary, #888);
  margin-bottom: 12px;
}

.job-palette__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.job-palette__item {
  padding: 8px 10px;
  border: 1px solid var(--u-border-color, #e5e5e5);
  border-radius: 6px;
  background: var(--u-bg-color, #fff);
  cursor: grab;
  display: flex;
  flex-direction: column;
  gap: 2px;

  &:hover {
    border-color: var(--u-color-primary, #3b82f6);
  }
}

.job-palette__name {
  font-size: 13px;
  font-weight: 500;
}

.job-palette__meta {
  font-size: 11px;
  color: var(--u-text-color-secondary, #888);
}
</style>
