<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import type { Component } from "vue";
import { useRouter } from "vue-router";
import { Agent, Bell, Build } from "@veltra/icons/normal";

import { getAccessToken } from "@/api/http";
import { notificationWsUrl } from "@/api/system";
import type { NotificationItem } from "@/api/types";
import { useBusy } from "@/composables/use-busy";
import { formatDateTime } from "@/lib/datetime";
import { useAuthStore } from "@/stores/auth";
import { useNotificationStore } from "@/stores/notification";

const auth = useAuthStore();
const store = useNotificationStore();
const router = useRouter();
const { busy: markAllBusy, run: runMarkAll } = useBusy();

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
  await runMarkAll(async () => {
    try {
      await store.markAllRead();
    } catch {
      // ignore
    }
  });
}

/** 类型 → 图标：构建 / 智能体 / 其他 */
function iconOf(n: NotificationItem): Component {
  if (n.type.startsWith("build_run_")) return Build;
  if (n.type.startsWith("agent_run_")) return Agent;
  return Bell;
}

type NotifTone = "success" | "failed" | "muted";

/** 状态 → 色调：成功松烟绿，失败朱砂，其余黛墨灰 */
function toneOf(n: NotificationItem): NotifTone {
  if (n.type.endsWith("_success")) return "success";
  if (n.type.endsWith("_failed")) return "failed";
  return "muted";
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
          <div class="notif-panel__heading">
            <span class="notif-panel__title">站内通知</span>
            <span v-if="store.unreadCount > 0" class="notif-panel__unread">
              {{ store.unreadCount }} 条未读
            </span>
          </div>
          <u-button
            v-if="store.unreadCount > 0"
            text
            type="primary"
            size="small"
            :loading="markAllBusy"
            @click="onMarkAll"
          >
            全部已读
          </u-button>
        </div>
        <div v-if="store.items.length === 0" class="notif-panel__empty">
          <u-empty text="暂无未读通知" />
        </div>
        <u-scroll
          v-else
          tag="ul"
          class="notif-panel__scroll"
          container-class="notif-panel__list"
          content-class="notif-panel__items"
        >
          <li v-for="n in store.items" :key="n.id">
            <button
              type="button"
              class="notif-item"
              :class="{ 'is-unread': !n.is_read }"
              @click="onItemClick(n)"
            >
              <span class="notif-item__icon" :class="`is-${toneOf(n)}`" aria-hidden="true">
                <u-icon :size="15">
                  <component :is="iconOf(n)" />
                </u-icon>
              </span>
              <span class="notif-item__body">
                <span class="notif-item__title">{{ n.title }}</span>
                <span v-if="n.message" class="notif-item__msg">{{ n.message }}</span>
                <span class="notif-item__time">{{ formatDateTime(n.created_at) }}</span>
              </span>
              <span v-if="!n.is_read" class="notif-item__dot" aria-label="未读" />
            </button>
          </li>
        </u-scroll>
      </div>
    </template>
  </u-dropdown>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;
@use "@/lib/empty-center.scss" as empty;

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
  max-height: 420px;
}

.notif-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: fn.use-var(gap, small);
  padding: 12px 16px 10px;
  /* 发丝分隔线，比整根 border 轻 */
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 55%, transparent);
}

.notif-panel__heading {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}

.notif-panel__title {
  font-size: fn.use-var(font-size-main, default);
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.notif-panel__unread {
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, second);
  white-space: nowrap;
}

.notif-panel__empty {
  @include empty.center(120px);
}

.notif-panel__scroll {
  flex: 1;
  min-height: 0;

  :deep(.notif-panel__list) {
    padding: 6px;
  }

  :deep(.notif-panel__items) {
    margin: 0;
    padding: 0;
    list-style: none;
  }
}

.notif-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border: 0;
  border-radius: fn.use-var(radius, default);
  background: transparent;
  text-align: left;
  cursor: pointer;
  color: inherit;
  transition: background-color 0.15s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
  }

  /* 已读条目整体降半阶，未读保持墨色并缀朱砂点 */
  &:not(.is-unread) {
    .notif-item__title {
      color: fn.use-var(text-color, second);
      font-weight: 400;
    }

    .notif-item__icon {
      opacity: 0.72;
    }
  }
}

.notif-item__icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  margin-top: 1px;
  border-radius: fn.use-var(radius, default);

  &.is-success {
    color: fn.use-var(color, primary);
    background: color-mix(in srgb, fn.use-var(color, primary) 10%, transparent);
  }

  /* 与登录页朱砂印同色 */
  &.is-failed {
    color: #b3452e;
    background: color-mix(in srgb, #b3452e 10%, transparent);
  }

  &.is-muted {
    color: fn.use-var(text-color, assist);
    background: color-mix(in srgb, fn.use-var(text-color, assist) 14%, transparent);
  }
}

.notif-item__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.notif-item__title {
  font-size: fn.use-var(font-size-main, default);
  font-weight: 500;
  line-height: 1.4;
  color: fn.use-var(text-color, title);
}

.notif-item__msg {
  font-size: fn.use-var(font-size-assist, small);
  line-height: 1.45;
  color: fn.use-var(text-color, second);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  word-break: break-all;
}

.notif-item__time {
  margin-top: 1px;
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, tip);
}

.notif-item__dot {
  flex-shrink: 0;
  align-self: center;
  width: 6px;
  height: 6px;
  margin-left: 2px;
  border-radius: 50%;
  /* 与登录页朱砂印同色 */
  background: #b3452e;
}
</style>
