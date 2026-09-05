<script setup lang="ts">
import { computed, ref } from "vue";
import { UButton, UIcon, UTag } from "@veltra/desktop";
import { Check, CircleCheck, CircleClose, Loading, VideoPlay, Warning } from "@veltra/icons/normal";
import type { ChatToolCall } from "@veltra/ai";

import { useAiChatStore } from "@/stores/ai-chat";

const props = defineProps<{
  toolCall: ChatToolCall;
}>();

const rootRef = ref<HTMLElement | null>(null);
const chatStore = useAiChatStore();

const isPipeline = computed(() => {
  return (
    props.toolCall.name === "trigger_pipeline" ||
    args.value.pipeline_id != null ||
    result.value.run_type === "pipeline"
  );
});

const isViewRun = computed(() => {
  return props.toolCall.name === "view_build_run";
});

const args = computed<Record<string, any>>(() => {
  if (!props.toolCall.arguments) return {};
  try {
    return JSON.parse(props.toolCall.arguments);
  } catch {
    return {};
  }
});

const result = computed<Record<string, any>>(() => {
  if (!props.toolCall.result) return {};
  try {
    return JSON.parse(props.toolCall.result);
  } catch {
    return {};
  }
});

const runId = computed<number | null>(() => {
  if (result.value.run_id && Number(result.value.run_id) > 0) {
    return Number(result.value.run_id);
  }
  if (args.value.run_id && Number(args.value.run_id) > 0) {
    return Number(args.value.run_id);
  }
  if (typeof props.toolCall.result === "string") {
    const match = props.toolCall.result.match(/(?:run_id|运行 ID|运行|#)\s*[:：#]?\s*(\d+)/i);
    if (match && match[1]) {
      return Number(match[1]);
    }
  }
  return null;
});

function confirmAction() {
  const toolCallEl = rootRef.value?.closest(".u-ai-chat__tool-call");
  const confirmBtn = toolCallEl?.querySelector(
    ".u-ai-chat__tool-call-confirm button:first-child",
  ) as HTMLElement | null;
  confirmBtn?.click();
}

function rejectAction() {
  const toolCallEl = rootRef.value?.closest(".u-ai-chat__tool-call");
  const rejectBtn = toolCallEl?.querySelector(
    ".u-ai-chat__tool-call-confirm button:last-child",
  ) as HTMLElement | null;
  rejectBtn?.click();
}

function openPanel() {
  const id = runId.value;
  if (!id) return;
  chatStore.openRightPanel({
    type: isPipeline.value ? "pipeline" : "build",
    id,
    title: isPipeline.value
      ? `流水线运行 #${id}`
      : `构建运行 #${id} · 任务 #${result.value.job_id || args.value.job_id || ""}`,
  });
}
</script>

<template>
  <div ref="rootRef" class="build-trigger-card">
    <!-- 1. 待确认敏感/危险操作 (awaiting-confirm) -->
    <div v-if="toolCall.status === 'awaiting-confirm'" class="build-confirm-box">
      <div class="build-confirm-box__banner">
        <u-icon :size="16" class="warning-icon">
          <Warning />
        </u-icon>
        <span class="warning-title">
          敏感操作确认 · 即将触发{{ isPipeline ? "流水线" : "构建任务" }}
        </span>
      </div>

      <p class="build-confirm-box__desc">
        该操作将在平台真实发起代码拉取与云端构建执行，可能消耗计算资源并产生制品变更。
      </p>

      <div class="build-confirm-box__meta">
        <div class="meta-row">
          <span class="meta-label">{{ isPipeline ? "流水线 ID" : "构建任务 ID" }}：</span>
          <span class="meta-value font-mono"
            >#{{ isPipeline ? args.pipeline_id : args.job_id }}</span
          >
        </div>
        <div v-if="args.branch" class="meta-row">
          <span class="meta-label">指定分支：</span>
          <span class="meta-value font-mono">{{ args.branch }}</span>
        </div>
        <div v-if="args.variables && Object.keys(args.variables).length" class="meta-row">
          <span class="meta-label">环境变量：</span>
          <span class="meta-value font-mono">{{ JSON.stringify(args.variables) }}</span>
        </div>
      </div>

      <div class="build-confirm-box__actions">
        <u-button type="danger" size="small" @click="confirmAction">
          <u-icon :size="13">
            <Check />
          </u-icon>
          确认触发{{ isPipeline ? "流水线" : "构建" }}
        </u-button>
        <u-button text size="small" @click="rejectAction"> 取消 </u-button>
      </div>
    </div>

    <!-- 2. 运行中 (running) -->
    <div v-else-if="toolCall.status === 'running'" class="build-status-box is-running">
      <u-icon :size="15" class="is-loading">
        <Loading />
      </u-icon>
      <span>正在向平台提交{{ isPipeline ? "流水线" : "构建任务" }}运行请求...</span>
    </div>

    <!-- 3. 用户拒绝 (rejected) -->
    <div v-else-if="toolCall.status === 'rejected'" class="build-status-box is-rejected">
      <u-icon :size="15" class="danger-icon">
        <CircleClose />
      </u-icon>
      <span>您已取消/拒绝触发本次{{ isPipeline ? "流水线" : "构建任务" }}。</span>
    </div>

    <!-- 4. 执行成功 (success) -->
    <div v-else-if="toolCall.status === 'success'" class="build-status-box is-success">
      <div class="build-success-info">
        <div class="build-success-title">
          <u-icon :size="15" class="success-icon">
            <CircleCheck />
          </u-icon>
          <span>
            {{
              isViewRun ? "运行详情加载完成" : (isPipeline ? "流水线" : "构建任务") + "已成功触发"
            }}！运行 ID:
            <strong class="font-mono">#{{ runId }}</strong>
          </span>
          <u-tag v-if="result.status" size="small" type="primary">
            {{ result.status }}
          </u-tag>
        </div>
        <div v-if="result.branch" class="build-success-sub">构建分支: {{ result.branch }}</div>
      </div>

      <div class="build-success-actions">
        <u-button type="primary" size="small" @click="openPanel">
          <u-icon :size="13">
            <VideoPlay />
          </u-icon>
          在右侧面板打开实时日志
        </u-button>
        <a
          v-if="result.link"
          :href="result.link"
          class="build-success-link"
          @click.prevent="openPanel"
        >
          查看运行详情 &rarr;
        </a>
      </div>
    </div>

    <!-- 5. 错误 (error) -->
    <div v-else-if="toolCall.status === 'error'" class="build-status-box is-error">
      <u-icon :size="15" class="danger-icon">
        <CircleClose />
      </u-icon>
      <span>触发失败：{{ toolCall.error || "未知错误" }}</span>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.build-trigger-card {
  padding: 6px 0;
  font-size: 13px;
  width: 100%;
}

.build-confirm-box {
  background: color-mix(in srgb, fn.use-var(color, danger) 6%, fn.use-var(bg-color, top));
  border: 1px solid color-mix(in srgb, fn.use-var(color, danger) 30%, transparent);
  border-radius: fn.use-var(radius, medium);
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.build-confirm-box__banner {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  color: fn.use-var(color, danger);

  .warning-icon {
    flex-shrink: 0;
  }
}

.build-confirm-box__desc {
  margin: 0;
  font-size: 12px;
  color: fn.use-var(text-color, second);
  line-height: 1.5;
}

.build-confirm-box__meta {
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 50%, transparent);
  border-radius: fn.use-var(radius, small);
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
}

.meta-row {
  display: flex;
  align-items: center;
  gap: 4px;

  .meta-label {
    color: fn.use-var(text-color, description);
  }

  .meta-value {
    color: fn.use-var(text-color, title);
    font-weight: 500;
  }
}

.build-confirm-box__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
}

.build-status-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border-radius: fn.use-var(radius, medium);
  border: 1px solid transparent;

  &.is-running {
    flex-direction: row;
    align-items: center;
    background: color-mix(in srgb, fn.use-var(color, primary) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 25%, transparent);
    color: fn.use-var(color, primary);
  }

  &.is-rejected {
    flex-direction: row;
    align-items: center;
    background: color-mix(in srgb, fn.use-var(color, danger) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, danger) 20%, transparent);
    color: fn.use-var(color, danger);
  }

  &.is-error {
    flex-direction: row;
    align-items: center;
    background: color-mix(in srgb, fn.use-var(color, danger) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, danger) 25%, transparent);
    color: fn.use-var(color, danger);
  }

  &.is-success {
    background: color-mix(in srgb, fn.use-var(color, success) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, success) 25%, transparent);
  }
}

.build-success-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.build-success-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  color: fn.use-var(text-color, title);

  .success-icon {
    color: fn.use-var(color, success);
  }
}

.build-success-sub {
  font-size: 11px;
  color: fn.use-var(text-color, description);
  padding-left: 21px;
}

.build-success-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 4px;
}

.build-success-link {
  font-size: 12px;
  color: fn.use-var(color, primary);
  text-decoration: none;
  cursor: pointer;

  &:hover {
    text-decoration: underline;
  }
}

.font-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
