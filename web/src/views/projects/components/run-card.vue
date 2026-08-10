<script setup lang="ts">
import { computed } from "vue";
import { CaretRight, History } from "@veltra/icons/normal";

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
  canViewHistory?: boolean;
}>();

const emit = defineEmits<{ run: []; history: [] }>();

const blocked = computed(() => !props.runnable || props.busy);

const runTitle = computed(() => {
  if (props.busy) return "运行中";
  if (!props.runnable) return props.disabledTip;
  return "运行";
});

const statusClass = computed(() => {
  if (!props.status) return "";
  return `run-card--status-${props.status}`;
});
</script>

<template>
  <u-card class="run-card" :class="statusClass">
    <u-card-content class="run-card__body">
      <div class="run-card__head">
        <h3 class="run-card__name" :title="name">{{ name }}</h3>
        <div v-if="canViewHistory || canExecute" class="run-card__actions">
          <u-button
            v-if="canViewHistory"
            plain
            size="small"
            circle
            :icon="History"
            title="运行历史"
            @click.stop="emit('history')"
          />
          <u-button
            v-if="canExecute"
            type="primary"
            size="small"
            circle
            :icon="CaretRight"
            :disabled="blocked"
            :loading="busy"
            :title="runTitle"
            @click.stop="emit('run')"
          />
        </div>
      </div>

      <p v-if="$slots.default" class="run-card__meta">
        <slot />
      </p>

      <p v-if="error" class="run-card__error">{{ error }}</p>
    </u-card-content>
  </u-card>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.run-card {
  min-width: 0;
  height: 100%;
  border-left: 3px solid fn.use-var(border, muted);
  transition:
    box-shadow 0.15s ease,
    border-color 0.15s ease;

  &:hover {
    box-shadow: fn.use-var(shadow);
  }

  &--status-queued,
  &--status-pending {
    border-left-color: fn.use-var(color, info);
  }

  &--status-running {
    border-left-color: fn.use-var(color, primary);
  }

  &--status-success {
    border-left-color: fn.use-var(color, success);
  }

  &--status-failed {
    border-left-color: fn.use-var(color, danger);
  }

  &--status-cancelled,
  &--status-interrupted {
    border-left-color: fn.use-var(color, warning);
  }
}

.run-card__body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px !important;
}

.run-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 28px;
}

.run-card__name {
  flex: 1;
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  color: fn.use-var(text-color, title);
}

.run-card__meta {
  margin: 0;
  min-width: 0;
  font-size: 12px;
  line-height: 1.5;
  color: fn.use-var(text-color, assist);

  :slotted(.run-card__sep) {
    margin: 0 6px;
    color: fn.use-var(text-color, disabled);
  }

  :slotted(.run-card__mono) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }

  :slotted(.run-card__warn) {
    color: fn.use-var(color, warning);
  }
}

.run-card__error {
  margin: 0;
  font-size: 12px;
  line-height: 1.4;
  color: fn.use-var(color, danger);
}

.run-card__actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
}
</style>
