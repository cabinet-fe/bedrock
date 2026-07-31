import { computed, ref } from "vue";
import { storage, storageKey } from "@cat-kit/fe";
import { defineStore } from "pinia";

export type WorkspaceTab = {
  /** Stable identity = route.path. Sub-tab query changes update this tab. */
  key: string;
  /** Latest fullPath; layout tab navigation always uses this. */
  fullPath: string;
  title: string;
  /** Vue component name for keep-alive include */
  name: string;
  closable: boolean;
};

type TabsCache = {
  tabs: WorkspaceTab[];
  activeKey: string;
};

const TABS_KEY = storageKey<TabsCache>("workspace_tabs");

const HOME_TAB: WorkspaceTab = {
  key: "/",
  fullPath: "/",
  title: "首页",
  name: "HomePage",
  closable: false,
};

function isWorkspaceTab(value: unknown): value is WorkspaceTab {
  if (!value || typeof value !== "object") return false;
  const t = value as Record<string, unknown>;
  return (
    typeof t.key === "string" &&
    typeof t.fullPath === "string" &&
    typeof t.title === "string" &&
    typeof t.name === "string" &&
    typeof t.closable === "boolean"
  );
}

function loadCache(): TabsCache | null {
  const cached = storage.session.get(TABS_KEY);
  if (!cached || !Array.isArray(cached.tabs) || !cached.tabs.length) return null;
  if (!cached.tabs.every(isWorkspaceTab)) return null;
  if (typeof cached.activeKey !== "string") return null;

  const tabs = cached.tabs.map((t) => ({ ...t }));
  if (!tabs.some((t) => t.key === HOME_TAB.key)) {
    tabs.unshift({ ...HOME_TAB });
  }
  const activeKey = tabs.some((t) => t.key === cached.activeKey) ? cached.activeKey : HOME_TAB.key;
  return { tabs, activeKey };
}

function persist(tabs: WorkspaceTab[], activeKey: string) {
  storage.session.set(TABS_KEY, { tabs, activeKey });
}

function clearCache() {
  storage.session.remove(TABS_KEY);
}

function keepAliveNameFromRoute(route: {
  name?: string | symbol | null;
  meta: Record<string, unknown>;
}): string {
  const metaName = route.meta.keepAliveName;
  if (typeof metaName === "string" && metaName) return metaName;
  const name = route.name;
  if (typeof name === "string" && name) {
    return name
      .split("-")
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join("");
  }
  return "AnonymousPage";
}

export const useTabsStore = defineStore("tabs", () => {
  const cached = loadCache();
  const tabs = ref<WorkspaceTab[]>(cached?.tabs ?? [{ ...HOME_TAB }]);
  const activeKey = ref(cached?.activeKey ?? HOME_TAB.key);

  const cachedNames = computed(() => [...new Set(tabs.value.map((t) => t.name))]);

  const tabItems = computed(() =>
    tabs.value.map((t) => ({
      key: t.key,
      name: t.title,
      closable: t.closable,
    })),
  );

  function findByKey(key: string) {
    return tabs.value.find((t) => t.key === key);
  }

  function open(tab: Omit<WorkspaceTab, "closable"> & { closable?: boolean }) {
    const existing = findByKey(tab.key);
    if (existing) {
      existing.title = tab.title;
      existing.fullPath = tab.fullPath;
      existing.name = tab.name;
      activeKey.value = existing.key;
      persist(tabs.value, activeKey.value);
      return;
    }
    tabs.value.push({
      key: tab.key,
      fullPath: tab.fullPath,
      title: tab.title,
      name: tab.name,
      closable: tab.closable ?? tab.key !== "/",
    });
    activeKey.value = tab.key;
    persist(tabs.value, activeKey.value);
  }

  function close(key: string) {
    const idx = tabs.value.findIndex((t) => t.key === key);
    if (idx < 0) return;
    const tab = tabs.value[idx]!;
    if (!tab.closable) return;

    const wasActive = activeKey.value === key;
    tabs.value.splice(idx, 1);

    if (!tabs.value.length) {
      tabs.value.push({ ...HOME_TAB });
      activeKey.value = HOME_TAB.key;
      persist(tabs.value, activeKey.value);
      return;
    }

    if (wasActive) {
      const next = tabs.value[Math.min(idx, tabs.value.length - 1)]!;
      activeKey.value = next.key;
    }
    persist(tabs.value, activeKey.value);
  }

  /** Keep home + the specified tab; drop every other closable tab. */
  function closeOthers(keepKey: string) {
    tabs.value = tabs.value.filter((t) => t.key === HOME_TAB.key || t.key === keepKey);
    if (!tabs.value.some((t) => t.key === HOME_TAB.key)) {
      tabs.value.unshift({ ...HOME_TAB });
    }
    if (!findByKey(activeKey.value)) {
      activeKey.value = findByKey(keepKey)?.key ?? HOME_TAB.key;
    }
    persist(tabs.value, activeKey.value);
  }

  /** Leave only the home tab. */
  function closeAll() {
    tabs.value = [{ ...HOME_TAB }];
    activeKey.value = HOME_TAB.key;
    persist(tabs.value, activeKey.value);
  }

  function updateTitle(key: string, title: string) {
    const tab = findByKey(key);
    if (!tab || !title) return;
    if (tab.title === title) return;
    tab.title = title;
    persist(tabs.value, activeKey.value);
  }

  function syncFromRoute(
    route: {
      fullPath: string;
      path: string;
      name?: string | symbol | null;
      meta: Record<string, unknown>;
    },
    title: string,
  ) {
    if (route.path === "/login") return;
    const existing = findByKey(route.path);
    if (existing) {
      // Preserve custom titles set via updateTitle; only refresh navigation fields.
      existing.fullPath = route.fullPath;
      existing.name = keepAliveNameFromRoute(route);
      activeKey.value = existing.key;
      persist(tabs.value, activeKey.value);
      return;
    }
    open({
      key: route.path,
      fullPath: route.fullPath,
      title,
      name: keepAliveNameFromRoute(route),
      closable: route.path !== "/",
    });
  }

  function reset() {
    tabs.value = [{ ...HOME_TAB }];
    activeKey.value = HOME_TAB.key;
    clearCache();
  }

  return {
    tabs,
    activeKey,
    cachedNames,
    tabItems,
    findByKey,
    open,
    close,
    closeOthers,
    closeAll,
    updateTitle,
    syncFromRoute,
    reset,
  };
});
