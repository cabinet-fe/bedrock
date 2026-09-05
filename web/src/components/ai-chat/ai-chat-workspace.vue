<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { createOpenAITransport, UAiChat, type ChatMessage } from "@veltra/ai";
import "@veltra/ai/style";
import { UButton, UIcon } from "@veltra/desktop";
import { Books, Close, VideoPlay } from "@veltra/icons/normal";

import { listChatMessages } from "@/api/ai";
import { getAccessToken } from "@/api/http";
import { useAiChatStore } from "@/stores/ai-chat";
import AiChatSidebar from "./ai-chat-sidebar.vue";
import BuildDetailPanel from "./panels/build-detail-panel.vue";
import DocViewerPanel from "./panels/doc-viewer-panel.vue";
import { aiChatTools } from "./tools";

const chatStore = useAiChatStore();
const currentMessages = ref<ChatMessage[]>([]);
const loadingMessages = ref(false);

function handleChatClick(event: MouseEvent) {
  const target = (event.target as HTMLElement)?.closest("a");
  if (!target) return;
  const href = target.getAttribute("href");
  if (!href) return;

  const buildMatch = href.match(/^\/cicd\/build-runs\/(\d+)/);
  if (buildMatch && buildMatch[1]) {
    event.preventDefault();
    event.stopPropagation();
    chatStore.openRightPanel({
      type: "build",
      id: Number(buildMatch[1]),
      title: `构建运行 #${buildMatch[1]}`,
    });
    return;
  }

  const pipelineMatch = href.match(/^\/cicd\/pipeline-runs\/(\d+)/);
  if (pipelineMatch && pipelineMatch[1]) {
    event.preventDefault();
    event.stopPropagation();
    chatStore.openRightPanel({
      type: "pipeline",
      id: Number(pipelineMatch[1]),
      title: `流水线运行 #${pipelineMatch[1]}`,
    });
    return;
  }
}

const transport = computed(() => {
  const token = getAccessToken();
  const rawModels = chatStore.availableModels;
  const models = rawModels.map((m) => ({
    id: m.model_id,
    label: m.name,
    description: m.notes || undefined,
    reasoningLevels:
      m.reasoning_efforts && m.reasoning_efforts.length > 0
        ? m.reasoning_efforts.map((r) => ({ value: r.value, label: r.label }))
        : undefined,
    defaultReasoningLevel: m.reasoning_efforts?.[0]?.value,
  }));

  const effectiveModels =
    models.length > 0 ? models : [{ id: "fallback-model", label: "暂无可用模型" }];

  const headers: Record<string, string> = {};
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  if (chatStore.currentSessionId) {
    headers["X-Session-ID"] = String(chatStore.currentSessionId);
  }

  return createOpenAITransport({
    headers,
    providers: [
      {
        id: "bedrock-ai",
        label: "Bedrock AI",
        endpoint: "/api/v1/ai/chat/completions",
        models: effectiveModels,
      },
    ],
  });
});

async function loadSessionMessages(sessionId: number) {
  loadingMessages.value = true;
  try {
    const list = await listChatMessages(sessionId);
    currentMessages.value = list.map((item) => ({
      id: String(item.id),
      role: (item.role as "user" | "assistant" | "tool") || "user",
      content: item.content,
      reasoning: item.reasoning_content,
      status: "done" as const,
    }));
  } catch (err) {
    console.error("加载会话历史消息失败:", err);
    currentMessages.value = [];
  } finally {
    loadingMessages.value = false;
  }
}

watch(
  () => chatStore.currentSessionId,
  (sessionId) => {
    if (sessionId) {
      void loadSessionMessages(sessionId);
    } else {
      currentMessages.value = [];
    }
  },
  { immediate: true },
);

async function onFinish(_msg: ChatMessage) {
  await chatStore.fetchSessions();
  if (chatStore.currentSessionId) {
    const session = chatStore.sessions.find((s) => s.id === chatStore.currentSessionId);
    if (session && session.title === "新对话") {
      const firstUser = currentMessages.value.find((m) => m.role === "user");
      if (firstUser && firstUser.content.trim()) {
        const title = firstUser.content.trim().slice(0, 24);
        await chatStore.renameSession(chatStore.currentSessionId, title);
      }
    }
  }
}

function onError(error: Error) {
  console.error("AI 对话出错:", error);
}

onMounted(async () => {
  if (chatStore.availableModels.length === 0) {
    await chatStore.fetchAvailableModels();
  }
  if (chatStore.sessions.length === 0) {
    await chatStore.fetchSessions();
  }
  if (chatStore.sessions.length === 0 && chatStore.availableModels.length > 0) {
    await chatStore.createSession("新对话");
  }
});
</script>

<template>
  <div class="ai-chat-workspace">
    <AiChatSidebar @select-session="loadSessionMessages" />

    <section class="ai-chat-workspace__main">
      <div
        class="ai-chat-workspace__chat-container"
        :class="{ 'is-loading': loadingMessages }"
        @click="handleChatClick"
      >
        <div
          v-if="chatStore.availableModels.length === 0 && !chatStore.loadingModels"
          class="ai-chat-workspace__empty-model"
        >
          <p>当前平台暂无可用的 AI 模型配置。</p>
          <p class="sub">请管理员在「AI - 服务商」中配置并启用服务商与模型。</p>
        </div>

        <u-ai-chat
          v-else
          v-model:messages="currentMessages"
          v-model:model="chatStore.currentModelId"
          v-model:reasoning-level="chatStore.currentReasoningLevel"
          :tools="aiChatTools"
          class="ai-chat-workspace__chat"
          :transport="transport"
          :models="transport.models"
          placeholder="输入问题，Enter 发送，Shift+Enter 换行..."
          token-usage-detail
          @finish="onFinish"
          @error="onError"
        />
      </div>
    </section>

    <!-- 右侧详情面板（构建/流水线/文档等） -->
    <aside v-if="chatStore.activeRightPanel" class="ai-chat-workspace__right-panel">
      <div class="ai-chat-workspace__panel-header">
        <div class="ai-chat-workspace__panel-title">
          <u-icon :size="16" class="panel-icon">
            <Books v-if="chatStore.activeRightPanel.type === 'doc'" />
            <VideoPlay v-else />
          </u-icon>
          <span>{{ chatStore.activeRightPanel.title || "运行详情" }}</span>
        </div>
        <u-button text circle size="small" title="关闭面板" @click="chatStore.closeRightPanel()">
          <u-icon :size="14">
            <Close />
          </u-icon>
        </u-button>
      </div>

      <div class="ai-chat-workspace__panel-body">
        <BuildDetailPanel
          v-if="
            chatStore.activeRightPanel.type === 'build' ||
            chatStore.activeRightPanel.type === 'pipeline'
          "
          :run-id="chatStore.activeRightPanel.id"
          :run-type="chatStore.activeRightPanel.type"
        />
        <DocViewerPanel
          v-else-if="chatStore.activeRightPanel.type === 'doc'"
          :node-id="chatStore.activeRightPanel.id"
          :project-id="chatStore.activeRightPanel.projectId"
          :doc-type="chatStore.activeRightPanel.docType"
        />
      </div>
    </aside>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.ai-chat-workspace {
  display: flex;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background: fn.use-var(bg-color, bottom);
  color: fn.use-var(text-color, main);
}

.ai-chat-workspace__main {
  flex: 1;
  min-width: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: fn.use-var(bg-color, middle);
}

.ai-chat-workspace__chat-container {
  flex: 1;
  min-height: 0;
  height: 100%;
  position: relative;
  overflow: hidden;

  &.is-loading {
    opacity: 0.6;
    pointer-events: none;
  }
}

.ai-chat-workspace__chat {
  height: 100%;
  width: 100%;

  /* 关闭图片与文件附件上传入口，仅支持纯文本交互 */
  :deep(.u-ai-chat__input-attach),
  :deep(.u-file-picker) {
    display: none !important;
  }
}

.ai-chat-workspace__empty-model {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: fn.use-var(text-color, description);
  font-size: 14px;
  gap: 8px;

  .sub {
    font-size: 12px;
    color: fn.use-var(text-color, placeholder);
  }
}

.ai-chat-workspace__right-panel {
  width: 680px;
  max-width: 50vw;
  min-width: 360px;
  height: 100%;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-left: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 70%, transparent);
  background: fn.use-var(bg-color, top);
  z-index: 5;
  animation: slideInRight 0.2s ease-out;
}

@keyframes slideInRight {
  from {
    opacity: 0;
    transform: translateX(20px);
  }
  to {
    opacity: 1;
    transform: translateX(0);
  }
}

.ai-chat-workspace__panel-header {
  height: 44px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 50%, transparent);
  background: fn.use-var(bg-color, middle);
}

.ai-chat-workspace__panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: fn.use-var(text-color, title);

  .panel-icon {
    color: fn.use-var(color, primary);
  }
}

.ai-chat-workspace__panel-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}
</style>
