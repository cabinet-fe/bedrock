<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { Delete, Edit, Message, Plus } from "@veltra/icons/normal";

import type { ChatSession } from "@/api/types";
import { useAiChatStore } from "@/stores/ai-chat";

const emit = defineEmits<{
  (e: "select-session", sessionId: number): void;
}>();

const chatStore = useAiChatStore();

const sortedSessions = computed(() => {
  return [...chatStore.sessions].sort((a, b) => {
    return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
  });
});

const editingId = ref<number | null>(null);
const editingTitle = ref("");

async function handleNewSession() {
  try {
    const session = await chatStore.createSession("新对话");
    emit("select-session", session.id);
  } catch (err) {
    console.error("创建会话失败:", err);
  }
}

function handleSelectSession(session: ChatSession) {
  if (editingId.value === session.id) return;
  chatStore.selectSession(session.id);
  emit("select-session", session.id);
}

function startRename(session: ChatSession) {
  editingId.value = session.id;
  editingTitle.value = session.title;
  void nextTick(() => {
    const el = document.getElementById(
      `session-edit-input-${session.id}`,
    ) as HTMLInputElement | null;
    el?.focus();
    el?.select();
  });
}

async function saveRename(session: ChatSession) {
  if (editingId.value !== session.id) return;
  const title = editingTitle.value.trim();
  editingId.value = null;
  if (title && title !== session.title) {
    try {
      await chatStore.renameSession(session.id, title);
    } catch (err) {
      console.error("重命名会话失败:", err);
    }
  }
}

function cancelRename() {
  editingId.value = null;
}

async function handleDelete(sessionId: number) {
  try {
    await chatStore.deleteSession(sessionId);
    if (chatStore.currentSessionId !== null) {
      emit("select-session", chatStore.currentSessionId);
    }
  } catch (err) {
    console.error("删除会话失败:", err);
  }
}

function formatSessionTime(dateStr: string): string {
  if (!dateStr) return "";
  const d = new Date(dateStr);
  if (Number.isNaN(d.getTime())) return "";
  const now = new Date();
  const isToday =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();

  const pad = (n: number) => (n < 10 ? `0${n}` : `${n}`);
  const hours = pad(d.getHours());
  const minutes = pad(d.getMinutes());

  if (isToday) {
    return `${hours}:${minutes}`;
  }
  const month = pad(d.getMonth() + 1);
  const day = pad(d.getDate());
  if (d.getFullYear() === now.getFullYear()) {
    return `${month}-${day}`;
  }
  return `${d.getFullYear()}-${month}-${day}`;
}
</script>

<template>
  <aside class="ai-chat-sidebar">
    <div class="ai-chat-sidebar__header">
      <u-button
        type="primary"
        class="ai-chat-sidebar__new-btn"
        :loading="chatStore.loadingSessions"
        @click="handleNewSession"
      >
        <u-icon :size="14">
          <Plus />
        </u-icon>
        新建会话
      </u-button>
    </div>

    <u-scroll class="ai-chat-sidebar__list">
      <div
        v-if="sortedSessions.length === 0 && !chatStore.loadingSessions"
        class="ai-chat-sidebar__empty"
      >
        暂无历史会话
      </div>

      <div
        v-for="session in sortedSessions"
        :key="session.id"
        class="ai-chat-sidebar__item"
        :class="{ 'is-active': chatStore.currentSessionId === session.id }"
        @click="handleSelectSession(session)"
      >
        <u-icon :size="14" class="ai-chat-sidebar__item-icon">
          <Message />
        </u-icon>

        <div class="ai-chat-sidebar__item-content">
          <input
            v-if="editingId === session.id"
            :id="`session-edit-input-${session.id}`"
            v-model="editingTitle"
            class="ai-chat-sidebar__item-input"
            @blur="saveRename(session)"
            @keydown.enter.prevent="saveRename(session)"
            @keydown.esc.prevent="cancelRename"
            @click.stop
          />
          <span
            v-else
            class="ai-chat-sidebar__item-title"
            :title="session.title"
            @dblclick.stop="startRename(session)"
          >
            {{ session.title || "未命名会话" }}
          </span>

          <span class="ai-chat-sidebar__item-time">
            {{ formatSessionTime(session.updated_at) }}
          </span>
        </div>

        <div class="ai-chat-sidebar__item-actions" @click.stop>
          <u-icon
            class="ai-chat-sidebar__action-btn"
            title="重命名会话"
            :size="13"
            @click="startRename(session)"
          >
            <Edit />
          </u-icon>

          <u-pop-confirm
            title="确认删除该会话及其所有历史消息？"
            confirm-text="删除"
            direction="top"
            @confirm="handleDelete(session.id)"
          >
            <template #reference>
              <u-icon class="ai-chat-sidebar__action-btn is-delete" title="删除会话" :size="13">
                <Delete />
              </u-icon>
            </template>
          </u-pop-confirm>
        </div>
      </div>
    </u-scroll>
  </aside>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.ai-chat-sidebar {
  display: flex;
  flex-direction: column;
  width: 260px;
  min-width: 260px;
  height: 100%;
  background: var(--u-nav-bg-color, fn.use-var(bg-color, top));
  border-right: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 70%, transparent);
  user-select: none;
}

.ai-chat-sidebar__header {
  padding: 12px 10px;
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 40%, transparent);
}

.ai-chat-sidebar__new-btn {
  width: 100%;
  justify-content: center;
  gap: 6px;
}

.ai-chat-sidebar__list {
  flex: 1;
  min-height: 0;

  :deep(.u-scroll__content) {
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
}

.ai-chat-sidebar__empty {
  padding: 32px 0;
  text-align: center;
  font-size: 13px;
  color: fn.use-var(text-color, description);
}

.ai-chat-sidebar__item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: fn.use-var(radius, default);
  cursor: pointer;
  transition:
    background 0.15s ease,
    color 0.15s ease;
  color: var(--u-nav-second-color, fn.use-var(text-color, main));

  &:hover {
    background: color-mix(in srgb, var(--u-nav-strong-color, #fff) 8%, transparent);

    .ai-chat-sidebar__item-actions {
      opacity: 1;
      visibility: visible;
    }
  }

  &.is-active {
    background: color-mix(in srgb, fn.use-var(color, primary) 16%, transparent);
    color: fn.use-var(color, primary);
    font-weight: 500;

    .ai-chat-sidebar__item-icon {
      color: fn.use-var(color, primary);
    }
  }
}

.ai-chat-sidebar__item-icon {
  flex-shrink: 0;
  color: fn.use-var(text-color, description);
}

.ai-chat-sidebar__item-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ai-chat-sidebar__item-title {
  font-size: 13px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ai-chat-sidebar__item-input {
  width: 100%;
  font-size: 13px;
  padding: 2px 4px;
  border: 1px solid fn.use-var(color, primary);
  border-radius: 3px;
  background: fn.use-var(bg-color, bottom);
  color: fn.use-var(text-color, main);
  outline: none;
}

.ai-chat-sidebar__item-time {
  font-size: 11px;
  color: fn.use-var(text-color, description);
}

.ai-chat-sidebar__item-actions {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.15s ease;
}

.ai-chat-sidebar__action-btn {
  padding: 2px;
  border-radius: 3px;
  color: fn.use-var(text-color, description);
  cursor: pointer;

  &:hover {
    color: var(--u-nav-strong-color, fn.use-var(text-color, title));
    background: color-mix(in srgb, var(--u-nav-strong-color, #fff) 12%, transparent);
  }

  &.is-delete:hover {
    color: fn.use-var(color, danger);
  }
}
</style>
