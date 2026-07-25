<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import { useRouter } from "vue-router";
import { Bell } from "@veltra/icons/normal";

import { getAccessToken } from "@/api/http";
import { notificationWsUrl } from "@/api/system";
import type { NotificationItem } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import { useAuthStore } from "@/stores/auth";
import { useNotificationStore } from "@/stores/notification";

const auth = useAuthStore();
const store = useNotificationStore();
const router = useRouter();

const unreadHidden = computed(() => store.unreadCount === 0);

let socket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

function clearReconnect(): void {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function disconnectWs(): void {
  clearReconnect();
  if (socket) {
    socket.onopen = null;
    socket.onmessage = null;
    socket.onerror = null;
    socket.onclose = null;
    socket.close();
    socket = null;
  }
}

function connectWs(): void {
  disconnectWs();
  const token = getAccessToken();
  if (!token || !auth.isAuthenticated) return;

  const ws = new WebSocket(notificationWsUrl(token));
  socket = ws;

  ws.onmessage = (ev) => {
    try {
      const raw = JSON.parse(String(ev.data)) as NotificationItem;
      if (!raw?.id || !raw.type || !raw.title) return;
      store.addNotification({
        ...raw,
        is_read: raw.is_read ?? false,
        created_at: raw.created_at || new Date().toISOString(),
      });
    } catch {
      // ignore malformed payloads
    }
  };

  ws.onclose = () => {
    if (socket !== ws) return;
    socket = null;
    if (!auth.isAuthenticated) return;
    clearReconnect();
    reconnectTimer = setTimeout(() => connectWs(), 3000);
  };
}

async function onItemClick(n: NotificationItem): Promise<void> {
  if (!n.is_read) {
    try {
      await store.markRead(n.id);
    } catch {
      // keep UI usable if mark-read fails
    }
  }
  if (n.build_run_id) {
    await router.push({ name: "cicd-build-run-detail", params: { id: String(n.build_run_id) } });
    return;
  }
  if (n.agent_run_id) {
    await router.push({ name: "ai-run-detail", params: { id: String(n.agent_run_id) } });
  }
}

async function onMarkAll(): Promise<void> {
  try {
    await store.markAllRead();
  } catch {
    // ignore
  }
}

onMounted(() => {
  if (auth.isAuthenticated) {
    void store.fetchNotifications();
    connectWs();
  }
});

watch(
  () => auth.isAuthenticated,
  (ok) => {
    if (ok) {
      void store.fetchNotifications();
      connectWs();
    } else {
      disconnectWs();
      store.reset();
    }
  },
);

onUnmounted(() => {
  disconnectWs();
});
</script>

<template>
  <u-dropdown trigger="click" width="360px">
    <template #trigger>
      <u-badge
        class="notif-badge"
        :value="store.unreadCount"
        size="small"
        :max="99"
        :hidden="unreadHidden"
      >
        <u-button text type="primary" class="notif-trigger" aria-label="通知">
          <Bell class="notif-bell-icon" />
        </u-button>
      </u-badge>
    </template>
    <template #content>
      <div class="notif-panel">
        <div class="notif-panel__head">
          <span class="notif-panel__title">站内通知</span>
          <u-button
            v-if="store.unreadCount > 0"
            text
            type="primary"
            size="small"
            @click="onMarkAll"
          >
            全部已读
          </u-button>
        </div>
        <div v-if="store.items.length === 0" class="notif-panel__empty">
          <u-empty description="暂无通知" />
        </div>
        <ul v-else class="notif-panel__list">
          <li v-for="n in store.items" :key="n.id">
            <button
              type="button"
              class="notif-item"
              :class="{ 'is-unread': !n.is_read }"
              @click="onItemClick(n)"
            >
              <span class="notif-item__title">{{ n.title }}</span>
              <span v-if="n.message" class="notif-item__msg">{{ n.message }}</span>
              <span class="notif-item__time">{{ formatDateTime(n.created_at) }}</span>
            </button>
          </li>
        </ul>
      </div>
    </template>
  </u-dropdown>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

/* 默认 UBadge 无 top/right，会落在触发器后方并叠到头像上；钉到铃铛右上角 */
.notif-badge {
  display: inline-flex;
  vertical-align: middle;
  /* 给角标外溢留一点空隙，避免贴住头像 */
  margin-right: 4px;

  :deep(.u-badge__sup) {
    top: -2px;
    right: -2px;
    /* 覆盖组件内联 transform，避免半截漂到头像上 */
    transform: none !important;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    /* 与登录页朱砂印同色；勿用 type=danger 浅底字色 */
    background-color: #b3452e !important;
    color: #fff;
    border: 1.5px solid fn.use-var(bg-color, top);
    font-size: 10px;
    font-weight: 600;
    line-height: 1;
    box-sizing: border-box;
  }
}

.notif-trigger {
  min-width: 32px;
  min-height: 32px;
  padding: 0 6px;
}

.notif-bell-icon {
  width: 18px;
  height: 18px;
}

.notif-panel {
  display: flex;
  flex-direction: column;
  max-height: 360px;
}

.notif-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: fn.use-var(gap, small);
  padding: 10px 12px;
  border-bottom: fn.use-var(border);
}

.notif-panel__title {
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.notif-panel__empty {
  padding: 24px 12px;
}

.notif-panel__list {
  margin: 0;
  padding: 0;
  list-style: none;
  overflow: auto;
}

.notif-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  border-bottom: fn.use-var(border);
  background: transparent;
  text-align: left;
  cursor: pointer;
  color: inherit;

  &:hover {
    background: fn.use-var(bg-color, top);
  }

  &.is-unread {
    background: color-mix(in srgb, fn.use-var(color, primary) 8%, transparent);
  }
}

.notif-item__title {
  font-size: fn.use-var(font-size-main, default);
  font-weight: 500;
  color: fn.use-var(text-color, main);
}

.notif-item__msg {
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, second);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.notif-item__time {
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, tip);
}
</style>
