import { defineStore } from "pinia";
import { ref, shallowRef, watch } from "vue";

import {
  createChatSession,
  deleteChatSession,
  listAvailableModels,
  listChatSessions,
  updateChatSession,
} from "@/api/ai";
import type { AiModel, ChatSession } from "@/api/types";

const AI_MODE_STORAGE_KEY = "bedrock_ai_mode_active";
const AI_CACHED_MODEL_KEY = "bedrock_ai_cached_model_id";
const AI_CACHED_REASONING_KEY = "bedrock_ai_cached_reasoning_level";

function getInitialAiMode(): boolean {
  try {
    return localStorage.getItem(AI_MODE_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function getCachedModelId(): string {
  try {
    return localStorage.getItem(AI_CACHED_MODEL_KEY) || "";
  } catch {
    return "";
  }
}

function setCachedModelId(id: string): void {
  try {
    if (id) {
      localStorage.setItem(AI_CACHED_MODEL_KEY, id);
    } else {
      localStorage.removeItem(AI_CACHED_MODEL_KEY);
    }
  } catch {
    // ignore
  }
}

function getCachedReasoningLevel(): string | undefined {
  try {
    const val = localStorage.getItem(AI_CACHED_REASONING_KEY);
    return val || undefined;
  } catch {
    return undefined;
  }
}

function setCachedReasoningLevel(level: string | undefined): void {
  try {
    if (level) {
      localStorage.setItem(AI_CACHED_REASONING_KEY, level);
    } else {
      localStorage.removeItem(AI_CACHED_REASONING_KEY);
    }
  } catch {
    // ignore
  }
}

export interface ActiveRightPanel {
  type: "build" | "pipeline" | "doc";
  id: number;
  title?: string;
  projectId?: number;
  docType?: "api" | "dev";
}

export const useAiChatStore = defineStore("ai-chat", () => {
  const aiModeActive = ref(getInitialAiMode());
  const activeRightPanel = ref<ActiveRightPanel | null>(null);
  const sessions = ref<ChatSession[]>([]);
  const currentSessionId = ref<number | null>(null);
  const availableModels = shallowRef<AiModel[]>([]);
  const currentModelId = ref<string>(getCachedModelId());
  const currentReasoningLevel = ref<string | undefined>(getCachedReasoningLevel());
  const loadingSessions = ref(false);
  const loadingModels = ref(false);

  function openRightPanel(panel: ActiveRightPanel) {
    activeRightPanel.value = panel;
  }

  function closeRightPanel() {
    activeRightPanel.value = null;
  }

  function syncReasoningForModel(modelId: string) {
    const target = availableModels.value.find((m) => m.model_id === modelId);
    if (!target || !target.reasoning_efforts || target.reasoning_efforts.length === 0) {
      currentReasoningLevel.value = undefined;
      setCachedReasoningLevel(undefined);
      return;
    }
    const cached = getCachedReasoningLevel();
    const currentValid = cached && target.reasoning_efforts.some((r) => r.value === cached);
    if (currentValid) {
      currentReasoningLevel.value = cached;
    } else {
      currentReasoningLevel.value = target.reasoning_efforts[0]?.value;
      setCachedReasoningLevel(currentReasoningLevel.value);
    }
  }

  watch(currentModelId, (val) => {
    if (val) {
      setCachedModelId(val);
      syncReasoningForModel(val);
    }
  });

  watch(currentReasoningLevel, (val) => {
    setCachedReasoningLevel(val);
  });

  async function fetchAvailableModels(): Promise<AiModel[]> {
    loadingModels.value = true;
    try {
      const list = await listAvailableModels();
      availableModels.value = list;
      if (list.length > 0) {
        const cachedModel = getCachedModelId();
        const hasCached = cachedModel && list.some((m) => m.model_id === cachedModel);
        if (hasCached) {
          currentModelId.value = cachedModel;
        } else {
          // 如果未设置或由于更改服务商导致模型 ID 失效，回退到默认模型（首个可用模型）
          currentModelId.value = list[0]!.model_id;
          setCachedModelId(currentModelId.value);
        }
        syncReasoningForModel(currentModelId.value);
      } else {
        currentModelId.value = "";
        currentReasoningLevel.value = undefined;
        setCachedModelId("");
        setCachedReasoningLevel(undefined);
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
    try {
      localStorage.setItem(AI_MODE_STORAGE_KEY, String(next));
    } catch {
      // ignore
    }
    if (next) {
      await Promise.all([fetchAvailableModels(), fetchSessions()]);
    } else {
      closeRightPanel();
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
    return session;
  }

  function selectSession(id: number): void {
    currentSessionId.value = id;
    closeRightPanel();
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
    activeRightPanel,
    sessions,
    currentSessionId,
    availableModels,
    currentModelId,
    currentReasoningLevel,
    loadingSessions,
    loadingModels,
    openRightPanel,
    closeRightPanel,
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
