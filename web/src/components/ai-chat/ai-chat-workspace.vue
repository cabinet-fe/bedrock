<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { createOpenAITransport, UAiChat, type ChatMessage } from "@veltra/ai";
import { Rollback } from "@veltra/icons/normal";
import "@veltra/ai/style";

import { listChatMessages } from "@/api/ai";
import { getAccessToken } from "@/api/http";
import type { ChatSession } from "@/api/types";
import { useAiChatStore } from "@/stores/ai-chat";
import AiChatSidebar from "./ai-chat-sidebar.vue";

const emit = defineEmits<{
  (e: "exit"): void;
}>();

const chatStore = useAiChatStore();
const currentMessages = ref<ChatMessage[]>([]);
const loadingMessages = ref(false);

const activeSession = computed<ChatSession | undefined>(() => {
  return chatStore.sessions.find((s) => s.id === chatStore.currentSessionId);
});

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
      <header class="ai-chat-workspace__header">
        <div class="ai-chat-workspace__info">
          <span class="ai-chat-workspace__session-title">
            {{ activeSession?.title || "AI 对话" }}
          </span>
          <span v-if="chatStore.currentModelId" class="ai-chat-workspace__model-tag">
            {{ chatStore.currentModelId }}
          </span>
        </div>

        <div class="ai-chat-workspace__actions">
          <u-button class="ai-chat-workspace__exit-btn" @click="emit('exit')">
            <u-icon :size="14">
              <Rollback />
            </u-icon>
            退出 AI 模式
          </u-button>
        </div>
      </header>

      <div class="ai-chat-workspace__chat-container" :class="{ 'is-loading': loadingMessages }">
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

.ai-chat-workspace__header {
  height: 48px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 60%, transparent);
  background: fn.use-var(bg-color, top);
}

.ai-chat-workspace__info {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.ai-chat-workspace__session-title {
  font-size: 14px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ai-chat-workspace__model-tag {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: fn.use-var(radius, small);
  background: color-mix(in srgb, fn.use-var(color, primary) 14%, transparent);
  color: fn.use-var(color, primary);
  font-family: monospace;
}

.ai-chat-workspace__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ai-chat-workspace__exit-btn {
  gap: 6px;
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
</style>
