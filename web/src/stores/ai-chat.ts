import { defineStore } from "pinia";
import { ref, shallowRef } from "vue";

import {
  createChatSession,
  deleteChatSession,
  listAvailableModels,
  listChatSessions,
  updateChatSession,
} from "@/api/ai";
import type { AiModel, ChatSession } from "@/api/types";

export const useAiChatStore = defineStore("ai-chat", () => {
  const aiModeActive = ref(false);
  const sessions = ref<ChatSession[]>([]);
  const currentSessionId = ref<number | null>(null);
  const availableModels = shallowRef<AiModel[]>([]);
  const currentModelId = ref<string>("");
  const currentReasoningLevel = ref<string | undefined>(undefined);
  const loadingSessions = ref(false);
  const loadingModels = ref(false);

  function syncReasoningForModel(modelId: string) {
    const target = availableModels.value.find((m) => m.model_id === modelId);
    if (!target || !target.reasoning_efforts || target.reasoning_efforts.length === 0) {
      currentReasoningLevel.value = undefined;
      return;
    }
    const currentValid = target.reasoning_efforts.some(
      (r) => r.value === currentReasoningLevel.value,
    );
    if (!currentValid) {
      currentReasoningLevel.value = target.reasoning_efforts[0]?.value;
    }
  }

  async function fetchAvailableModels(): Promise<AiModel[]> {
    loadingModels.value = true;
    try {
      const list = await listAvailableModels();
      availableModels.value = list;
      if (list.length > 0) {
        const hasCurrent = list.some((m) => m.model_id === currentModelId.value);
        if (!hasCurrent) {
          currentModelId.value = list[0]!.model_id;
        }
        syncReasoningForModel(currentModelId.value);
      } else {
        currentModelId.value = "";
        currentReasoningLevel.value = undefined;
      }
      return list;
    } finally {
      loadingModels.value = false;
    }
  }

  async function fetchSessions(): Promise<ChatSession[]> {
    loadingSessions.value = true;
    try {
      const res = await listChatSessions({ page: 1, page_size: 100 });
      const items = res.items ?? [];
      sessions.value = items;

      if (currentSessionId.value !== null) {
        const exists = items.some((s) => s.id === currentSessionId.value);
        if (!exists) {
          currentSessionId.value = items.length > 0 ? items[0]!.id : null;
        }
      } else if (items.length > 0) {
        currentSessionId.value = items[0]!.id;
      }

      return items;
    } finally {
      loadingSessions.value = false;
    }
  }

  async function toggleAiMode(active?: boolean): Promise<void> {
    const next = active ?? !aiModeActive.value;
    aiModeActive.value = next;
    if (next) {
      await Promise.all([fetchAvailableModels(), fetchSessions()]);
    }
  }

  async function createSession(title = "新对话", modelId?: string): Promise<ChatSession> {
    const model = modelId || currentModelId.value;
    const session = await createChatSession({
      title,
      model_id: model || undefined,
    });
    sessions.value.unshift(session);
    currentSessionId.value = session.id;
    if (session.model_id) {
      const exists = availableModels.value.some((m) => m.model_id === session.model_id);
      if (exists) {
        currentModelId.value = session.model_id;
        syncReasoningForModel(session.model_id);
      }
    }
    return session;
  }

  function selectSession(id: number): void {
    currentSessionId.value = id;
    const session = sessions.value.find((s) => s.id === id);
    if (session?.model_id) {
      const exists = availableModels.value.some((m) => m.model_id === session.model_id);
      if (exists) {
        currentModelId.value = session.model_id;
        syncReasoningForModel(session.model_id);
      }
    }
  }

  async function renameSession(id: number, title: string): Promise<void> {
    const trimmed = title.trim();
    if (!trimmed) return;
    const updated = await updateChatSession(id, { title: trimmed });
    const idx = sessions.value.findIndex((s) => s.id === id);
    if (idx !== -1) {
      sessions.value[idx] = {
        ...sessions.value[idx]!,
        title: updated.title,
        updated_at: updated.updated_at,
      };
    }
  }

  async function deleteSession(id: number): Promise<void> {
    await deleteChatSession(id);
    const idx = sessions.value.findIndex((s) => s.id === id);
    if (idx !== -1) {
      sessions.value.splice(idx, 1);
    }
    if (currentSessionId.value === id) {
      currentSessionId.value = sessions.value.length > 0 ? sessions.value[0]!.id : null;
    }
  }

  function setModel(modelId: string) {
    currentModelId.value = modelId;
    syncReasoningForModel(modelId);
  }

  function setReasoningLevel(level: string | undefined) {
    currentReasoningLevel.value = level;
  }

  return {
    aiModeActive,
    sessions,
    currentSessionId,
    availableModels,
    currentModelId,
    currentReasoningLevel,
    loadingSessions,
    loadingModels,
    toggleAiMode,
    fetchAvailableModels,
    fetchSessions,
    createSession,
    selectSession,
    renameSession,
    deleteSession,
    setModel,
    setReasoningLevel,
  };
});
