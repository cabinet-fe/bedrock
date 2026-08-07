<script setup lang="ts">
import { computed } from "vue";

import { JOB_STATUS_TAG, tagType } from "@/lib/tag";

const props = defineProps<{
  name: string;
  status?: string;
  error?: string;
  /** 业务上可运行（启用 + 已开手动触发） */
  runnable: boolean;
  disabledTip: string;
  /** 入队中或 run 未终态 */
  busy: boolean;
  canExecute: boolean;
}>();

const emit = defineEmits<{ run: [] }>();

const blocked = computed(() => !props.runnable || props.busy);

const tip = computed(() => {
  if (props.busy) return "任务运行中";
  return props.disabledTip;
});

const label = computed(() => (props.busy ? "运行中" : "运行"));
</script>

<template>
  <u-card integrate class="run-card">
    <u-card-content class="run-card__body">
      <div class="run-card__main">
        <div class="run-card__title">
          <h3 class="run-card__name" :title="name">{{ name }}</h3>
          <u-tag v-if="status" size="small" :type="tagType(status, JOB_STATUS_TAG)">
            {{ status }}
          </u-tag>
        </div>
        <div class="run-card__meta">
          <slot />
        </div>
        <p v-if="error" class="run-card__error">{{ error }}</p>
      </div>
      <u-button
        v-if="canExecute"
        class="run-card__run"
        type="primary"
        :disabled="blocked"
        :loading="busy"
        :title="tip"
        @click="emit('run')"
      >
        {{ label }}
      </u-button>
    </u-card-content>
  </u-card>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.run-card {
  min-width: 0;
}

.run-card__body {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 14px 16px !important;
}

.run-card__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.run-card__title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.run-card__name {
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.3;
  color: fn.use-var(text-color, title);
}

.run-card__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font-size: 12px;
  line-height: 1.4;
  color: fn.use-var(text-color, assist);
}

.run-card__error {
  margin: 0;
  font-size: 12px;
  color: fn.use-var(color, danger);
}

.run-card__run {
  flex-shrink: 0;
  min-width: 88px;
}
</style>
