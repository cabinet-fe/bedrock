<script setup lang="ts">
import { computed } from "vue";
import { CaretRight, History } from "@veltra/icons/normal";
import { jobStatusLabel } from "@/lib/tag";

const props = defineProps<{
  name: string;
  description?: string;
  status?: string;
  error?: string;
  /** 业务上可运行（启用 + 已开手动触发） */
  runnable: boolean;
  disabledTip?: string;
  /** 入队中或 run 未终态 */
  busy: boolean;
  canExecute: boolean;
  canViewHistory?: boolean;
}>();

const emit = defineEmits<{ run: []; history: [] }>();

const blocked = computed(() => !props.runnable || props.busy);

const runTitle = computed(() => {
  if (props.busy) return "运行中";
  if (!props.runnable) return props.disabledTip || "不可运行";
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
            :icon="History"
            class="run-card__btn run-card__btn--history"
            title="运行历史"
            @click.stop="emit('history')"
          />
          <u-button
            v-if="canExecute"
            plain
            type="primary"
            size="small"
            :icon="CaretRight"
            class="run-card__btn run-card__btn--run"
            :disabled="blocked"
            :loading="busy"
            :title="runTitle"
            @click.stop="emit('run')"
          />
        </div>
      </div>

      <div v-if="$slots.tags || (!runnable && disabledTip)" class="run-card__tags">
        <slot name="tags" />
        <u-tag v-if="!runnable && disabledTip" size="small" type="warning">
          {{ disabledTip }}
        </u-tag>
      </div>

      <p v-if="description?.trim()" class="run-card__desc" :title="description">
        {{ description }}
      </p>

      <div v-if="$slots.default" class="run-card__meta">
        <slot />
      </div>

      <div v-if="$slots.footer || status" class="run-card__footer">
        <div class="run-card__footer-left">
          <slot name="footer" />
        </div>
        <div v-if="status" class="run-card__status" :class="`run-card__status--${status}`">
          <span class="run-card__status-dot" />
          <span class="run-card__status-text">{{ jobStatusLabel(status) }}</span>
        </div>
      </div>

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
  gap: 8px;
  padding: 12px 14px !important;
  height: 100%;
  box-sizing: border-box;
}

.run-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
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

.run-card__actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
}

.run-card__btn {
  width: 28px;
  height: 28px;
  min-width: 28px;
  padding: 0 !important;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;

  &--history {
    color: fn.use-var(text-color, regular);
    border: 1px solid fn.use-var(border, muted);
    background-color: transparent;

    &:hover {
      color: fn.use-var(text-color, main);
      border-color: fn.use-var(border);
      background-color: fn.use-var(fill-color, light);
    }
  }

  &--run {
    border: 1px solid transparent;

    &:not(:disabled):hover {
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.06);
    }
  }
}

.run-card__tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.run-card__desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: fn.use-var(text-color, secondary);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.run-card__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  line-height: 1.5;
  color: fn.use-var(text-color, assist);

  :slotted(.run-card__meta-item) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :slotted(.run-card__sep) {
    margin: 0 4px;
    color: fn.use-var(text-color, disabled);
  }

  :slotted(.run-card__mono) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }

  :slotted(.run-card__warn) {
    color: fn.use-var(color, warning);
  }
}

.run-card__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px dashed fn.use-var(border, light);
  font-size: 11px;
  color: fn.use-var(text-color, disabled);

  :slotted(.run-card__footer-item) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
}

.run-card__footer-left {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.run-card__status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 500;
  line-height: 1;
  flex-shrink: 0;

  &--success {
    color: fn.use-var(color, success);
    .run-card__status-dot {
      background-color: fn.use-var(color, success);
    }
  }

  &--failed {
    color: fn.use-var(color, danger);
    .run-card__status-dot {
      background-color: fn.use-var(color, danger);
    }
  }

  &--running {
    color: fn.use-var(color, primary);
    .run-card__status-dot {
      background-color: fn.use-var(color, primary);
      animation: run-card-pulse 1.4s ease-in-out infinite;
    }
  }

  &--queued,
  &--pending {
    color: fn.use-var(color, info);
    .run-card__status-dot {
      background-color: fn.use-var(color, info);
    }
  }

  &--cancelled,
  &--interrupted {
    color: fn.use-var(color, warning);
    .run-card__status-dot {
      background-color: fn.use-var(color, warning);
    }
  }
}

.run-card__status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

@keyframes run-card-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.35;
    transform: scale(0.8);
  }
}

.run-card__error {
  margin: 0;
  font-size: 12px;
  line-height: 1.4;
  color: fn.use-var(color, danger);
  background-color: fn.use-var(color, danger, light-9);
  padding: 4px 8px;
  border-radius: 4px;
}
</style>
