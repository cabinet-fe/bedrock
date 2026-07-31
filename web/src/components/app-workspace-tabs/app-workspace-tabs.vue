<script setup lang="ts">
import type { ContextmenuItem, TabItem } from "@veltra/desktop";
import { useRouter } from "vue-router";
import { ref, shallowRef } from "vue";

import { useTabsStore } from "@/stores/tabs";

const tabsStore = useTabsStore();
const router = useRouter();

const menuVisible = ref(false);
const menuPos = shallowRef({ x: 0, y: 0 });
const menuItems = shallowRef<ContextmenuItem[]>([]);

async function go(fullPath: string) {
  if (router.currentRoute.value.fullPath === fullPath) return;
  await router.push(fullPath);
}

async function activate(key: string) {
  if (key === tabsStore.activeKey) return;
  const tab = tabsStore.findByKey(key);
  if (!tab) return;
  await go(tab.fullPath);
}

async function handleClose(item: TabItem) {
  await closeTab(item.key);
}

/** Navigate away first when needed so syncFromRoute cannot reopen a just-closed tab. */
async function closeTab(key: string) {
  const closingActive = tabsStore.activeKey === key;
  if (closingActive) {
    const idx = tabsStore.tabs.findIndex((t) => t.key === key);
    const fallback =
      tabsStore.tabs[idx + 1] ??
      tabsStore.tabs[idx - 1] ??
      tabsStore.tabs.find((t) => t.key !== key);
    if (fallback) await go(fallback.fullPath);
  }
  tabsStore.close(key);
}

async function closeOthers(keepKey: string) {
  const keep = tabsStore.findByKey(keepKey);
  if (keep) await go(keep.fullPath);
  tabsStore.closeOthers(keepKey);
}

async function closeAll() {
  await go("/");
  tabsStore.closeAll();
}

function tabFromEvent(e: MouseEvent) {
  const el = (e.target as HTMLElement | null)?.closest?.(".u-tabs-bar__item");
  if (!(el instanceof HTMLElement)) return null;
  const list = el.parentElement;
  if (!list) return null;
  const index = [...list.querySelectorAll(":scope > .u-tabs-bar__item")].indexOf(el);
  return tabsStore.tabs[index] ?? null;
}

function onContextMenu(e: MouseEvent) {
  const tab = tabFromEvent(e);
  if (!tab) return;

  e.preventDefault();

  const hasOthers = tabsStore.tabs.some((t) => t.closable && t.key !== tab.key);
  const hasClosable = tabsStore.tabs.some((t) => t.closable);

  const menus: ContextmenuItem[] = [];
  if (tab.closable) {
    menus.push({
      label: "关闭",
      callback: () => closeTab(tab.key),
    });
  }
  menus.push(
    {
      label: "关闭其它",
      disabled: !hasOthers,
      callback: () => closeOthers(tab.key),
    },
    {
      label: "关闭全部",
      disabled: !hasClosable,
      callback: () => closeAll(),
    },
  );

  menuPos.value = { x: e.clientX, y: e.clientY };
  menuItems.value = menus;
  menuVisible.value = true;
}
</script>

<template>
  <div class="workspace-tabs" @contextmenu="onContextMenu">
    <u-tabs-horizontal
      :model-value="tabsStore.activeKey"
      :items="tabsStore.tabItems"
      closable
      block
      @update:model-value="activate"
      @close="handleClose"
    />

    <u-contextmenu
      v-if="menuVisible"
      :mouse-position="menuPos"
      :menus="menuItems"
      :width="140"
      @destroy="menuVisible = false"
    />
  </div>
</template>

<style scoped lang="scss">
.workspace-tabs {
  /* Whisper ledge under the thin rail — flush, no own bar chrome */
  flex-shrink: 0;
  min-width: 0;
  padding: 0 0 4px;
  background: transparent;

  :deep(.u-tabs-horizontal) {
    --u-tabs-header-bg: transparent;
  }
}
</style>
