import { defineStore } from "pinia";
import { computed, ref } from "vue";

import { listNotifications, markAllNotificationsRead, markNotificationRead } from "@/api/system";
import type { NotificationItem } from "@/api/types";

const NOTIFICATION_LIMIT = 50;

export const useNotificationStore = defineStore("notification", () => {
  const items = ref<NotificationItem[]>([]);

  const unreadCount = computed(() => items.value.filter((n) => !n.is_read).length);

  async function fetchNotifications(): Promise<void> {
    // 铃铛面板是收件箱语义：只拉未读，已读不再回拉，避免列表随历史膨胀
    const page = await listNotifications({
      page: 1,
      page_size: NOTIFICATION_LIMIT,
      is_read: false,
    });
    items.value = page?.items ?? [];
  }

  function addNotification(n: NotificationItem): void {
    if (items.value.some((x) => x.id === n.id)) return;
    items.value = [n, ...items.value].slice(0, NOTIFICATION_LIMIT);
  }

  async function markRead(id: number): Promise<void> {
    await markNotificationRead(id);
    items.value = items.value.map((n) => (n.id === id ? { ...n, is_read: true } : n));
  }

  async function markAllRead(): Promise<void> {
    await markAllNotificationsRead();
    items.value = items.value.map((n) => ({ ...n, is_read: true }));
  }

  function reset(): void {
    items.value = [];
  }

  return {
    items,
    unreadCount,
    fetchNotifications,
    addNotification,
    markRead,
    markAllRead,
    reset,
  };
});
