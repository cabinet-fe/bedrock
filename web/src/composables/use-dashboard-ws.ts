import { onMounted, onUnmounted, watch, type Ref } from "vue";

import { dashboardWsUrl } from "@/api/dashboard";
import { getAccessToken } from "@/api/http";
import type { SystemStatus } from "@/api/types";
import { useAuthStore } from "@/stores/auth";

export type DashboardRunType = "build" | "script" | "pipeline";

const RUN_DEBOUNCE_MS = 300;
const RECONNECT_MS = 3000;

interface DashboardWsMessage {
  type?: string;
  run_type?: DashboardRunType;
  data?: SystemStatus;
}

export function useDashboardWs(options: {
  systemStatus: Ref<SystemStatus | null>;
  onRunChanged: (runType: DashboardRunType) => void;
}) {
  const auth = useAuthStore();
  let socket: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let runDebounceTimers: Partial<Record<DashboardRunType, ReturnType<typeof setTimeout>>> = {};

  function clearReconnect(): void {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function clearRunDebounce(): void {
    for (const timer of Object.values(runDebounceTimers)) {
      if (timer) clearTimeout(timer);
    }
    runDebounceTimers = {};
  }

  function scheduleRunRefresh(runType: DashboardRunType): void {
    const existing = runDebounceTimers[runType];
    if (existing) clearTimeout(existing);
    runDebounceTimers[runType] = setTimeout(() => {
      delete runDebounceTimers[runType];
      options.onRunChanged(runType);
    }, RUN_DEBOUNCE_MS);
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

  function cleanup(): void {
    disconnectWs();
    clearRunDebounce();
  }

  function connectWs(): void {
    disconnectWs();
    const token = getAccessToken();
    if (!token || !auth.isAuthenticated) return;

    const ws = new WebSocket(dashboardWsUrl(token));
    socket = ws;

    ws.onmessage = (ev) => {
      try {
        const raw = JSON.parse(String(ev.data)) as DashboardWsMessage;
        if (!raw?.type) return;
        if (raw.type === "run_changed" && raw.run_type) {
          scheduleRunRefresh(raw.run_type);
          return;
        }
        if (raw.type === "system_status" && raw.data) {
          options.systemStatus.value = raw.data;
        }
      } catch {
        // ignore malformed payloads
      }
    };

    ws.onclose = () => {
      if (socket !== ws) return;
      socket = null;
      if (!auth.isAuthenticated) return;
      clearReconnect();
      reconnectTimer = setTimeout(() => connectWs(), RECONNECT_MS);
    };
  }

  onMounted(() => {
    connectWs();
  });

  onUnmounted(() => {
    cleanup();
  });

  watch(
    () => auth.isAuthenticated,
    (authenticated) => {
      if (authenticated) {
        connectWs();
      } else {
        cleanup();
      }
    },
  );
}
