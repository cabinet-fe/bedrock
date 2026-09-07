import { defineStore } from "pinia";
import { computed, ref, shallowRef, watch } from "vue";

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
const AI_CACHED_REASONING_MAP_KEY = "bedrock_ai_cached_reasoning_map";

function getInitialAiMode(): boolean {
  try {
    return localStorage.getItem(AI_MODE_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function getCachedModelId(): string {
  try {
    const val = localStorage.getItem(AI_CACHED_MODEL_KEY);
    return val && val !== "fallback-model" ? val : "";
  } catch {
    return "";
  }
}

function setCachedModelId(id: string): void {
  try {
    if (id && id !== "fallback-model") {
      localStorage.setItem(AI_CACHED_MODEL_KEY, id);
    } else if (!id) {
      localStorage.removeItem(AI_CACHED_MODEL_KEY);
    }
  } catch {
    // ignore
  }
}

function getCachedReasoningMap(): Record<string, string> {
  try {
    const raw = localStorage.getItem(AI_CACHED_REASONING_MAP_KEY);
    return raw ? (JSON.parse(raw) as Record<string, string>) : {};
  } catch {
    return {};
  }
}

export function getCachedReasoningLevel(modelId?: string): string | undefined {
  try {
    if (modelId) {
      const map = getCachedReasoningMap();
      if (map[modelId]) {
        return map[modelId];
      }
    }
    const val = localStorage.getItem(AI_CACHED_REASONING_KEY);
    return val || undefined;
  } catch {
    return undefined;
  }
}

function setCachedReasoningLevel(level: string | undefined, modelId?: string): void {
  try {
    if (level) {
      localStorage.setItem(AI_CACHED_REASONING_KEY, level);
      if (modelId && modelId !== "fallback-model") {
        const map = getCachedReasoningMap();
        map[modelId] = level;
        localStorage.setItem(AI_CACHED_REASONING_MAP_KEY, JSON.stringify(map));
      }
    } else {
      localStorage.removeItem(AI_CACHED_REASONING_KEY);
      if (modelId) {
        const map = getCachedReasoningMap();
        delete map[modelId];
        localStorage.setItem(AI_CACHED_REASONING_MAP_KEY, JSON.stringify(map));
      }
    }
  } catch {
    // ignore
  }
}

export interface ActiveRightPanel {
  type: "build" | "pipeline" | "doc" | "project";
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
  const isTemporary = ref(false);
  const isDraft = computed(() => !isTemporary.value && currentSessionId.value === null);
  const availableModels = shallowRef<AiModel[]>([]);
  const currentModelId = ref<string>(getCachedModelId());
  const currentReasoningLevel = ref<string | undefined>(
    getCachedReasoningLevel(getCachedModelId()),
  );
  const loadingSessions = ref(false);
  const loadingModels = ref(false);
  const modelsLoaded = ref(false);
  let fetchModelsPromise: Promise<AiModel[]> | null = null;

  function openRightPanel(panel: ActiveRightPanel) {
    activeRightPanel.value = panel;
  }

  function closeRightPanel() {
    activeRightPanel.value = null;
  }

  function syncReasoningForModel(modelId: string) {
    if (!modelsLoaded.value || availableModels.value.length === 0) {
      return;
    }
    const target = availableModels.value.find((m) => m.model_id === modelId);
    if (!target) {
      return;
    }
    if (!target.reasoning_efforts || target.reasoning_efforts.length === 0) {
      currentReasoningLevel.value = undefined;
      setCachedReasoningLevel(undefined, modelId);
      return;
    }

    const cached = currentReasoningLevel.value ?? getCachedReasoningLevel(modelId);
    const matched = cached && target.reasoning_efforts.some((r) => r.value === cached);
    if (matched) {
      currentReasoningLevel.value = cached;
      setCachedReasoningLevel(cached, modelId);
    } else {
      const fallback = target.reasoning_efforts[0]?.value;
      currentReasoningLevel.value = fallback;
      setCachedReasoningLevel(fallback, modelId);
    }
  }

  watch(currentModelId, (val) => {
    if (!val || val === "fallback-model") return;
    if (modelsLoaded.value) {
      const isValid = availableModels.value.some((m) => m.model_id === val);
      if (isValid) {
        setCachedModelId(val);
        syncReasoningForModel(val);
      }
    }
  });

  watch(currentReasoningLevel, (val) => {
    if (!modelsLoaded.value) return;
    setCachedReasoningLevel(val, currentModelId.value);
  });

  async function fetchAvailableModels(): Promise<AiModel[]> {
    if (fetchModelsPromise) {
      return fetchModelsPromise;
    }
    loadingModels.value = true;
    fetchModelsPromise = (async () => {
      try {
        const list = await listAvailableModels();
        availableModels.value = list;
        modelsLoaded.value = true;
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
        fetchModelsPromise = null;
      }
    })();
    return fetchModelsPromise;
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
          currentSessionId.value = null;
        }
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
    isTemporary.value = false;
    currentSessionId.value = session.id;
    return session;
  }

  function selectSession(id: number): void {
    isTemporary.value = false;
    currentSessionId.value = id;
    closeRightPanel();
  }

  function startDraft(): void {
    isTemporary.value = false;
    currentSessionId.value = null;
    closeRightPanel();
  }

  function startTemporary(): void {
    isTemporary.value = true;
    currentSessionId.value = null;
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
      currentSessionId.value = null;
      isTemporary.value = false;
    }
  }

  function setModel(modelId: string) {
    if (!modelId || modelId === "fallback-model") return;
    currentModelId.value = modelId;
    if (modelsLoaded.value) {
      const isValid = availableModels.value.some((m) => m.model_id === modelId);
      if (isValid) {
        setCachedModelId(modelId);
        syncReasoningForModel(modelId);
      }
    }
  }

  function setReasoningLevel(level: string | undefined) {
    currentReasoningLevel.value = level;
    if (modelsLoaded.value) {
      setCachedReasoningLevel(level, currentModelId.value);
    }
  }

  return {
    aiModeActive,
    activeRightPanel,
    sessions,
    currentSessionId,
    isTemporary,
    isDraft,
    availableModels,
    currentModelId,
    currentReasoningLevel,
    loadingSessions,
    loadingModels,
    modelsLoaded,
    openRightPanel,
    closeRightPanel,
    toggleAiMode,
    fetchAvailableModels,
    fetchSessions,
    createSession,
    selectSession,
    startDraft,
    startTemporary,
    renameSession,
    deleteSession,
    setModel,
    setReasoningLevel,
    syncReasoningForModel,
    getCachedReasoningLevel,
  };
});
