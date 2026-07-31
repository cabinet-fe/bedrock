<script setup lang="ts">
import type { ContextmenuItem, TabItem } from "@veltra/desktop";
import { contextmenu } from "@veltra/desktop";
import { useRouter } from "vue-router";

import { useTabsStore } from "@/stores/tabs";

const tabsStore = useTabsStore();
const router = useRouter();

function navigateActive() {
  const next = tabsStore.findByKey(tabsStore.activeKey);
  if (next) void router.push(next.fullPath);
}

function activate(key: string) {
  if (key === tabsStore.activeKey) return;
  const tab = tabsStore.findByKey(key);
  if (!tab) return;
  void router.push(tab.fullPath);
}

function handleClose(item: TabItem) {
  const closingActive = tabsStore.activeKey === item.key;
  tabsStore.close(item.key);
  if (closingActive) navigateActive();
}

function closeTab(key: string) {
  const closingActive = tabsStore.activeKey === key;
  tabsStore.close(key);
  if (closingActive) navigateActive();
}

function closeOthers(keepKey: string) {
  tabsStore.closeOthers(keepKey);
  navigateActive();
}

function closeAll() {
  tabsStore.closeAll();
  void router.push("/");
}

function onContextMenu(e: MouseEvent) {
  const el = (e.target as HTMLElement | null)?.closest?.(".u-tabs-bar__item");
  if (!(el instanceof HTMLElement)) return;
  const list = el.parentElement;
  if (!list) return;
  const index = [...list.querySelectorAll(":scope > .u-tabs-bar__item")].indexOf(el);
  const tab = tabsStore.tabs[index];
  if (!tab) return;

  e.preventDefault();

  const hasOthers = tabsStore.tabs.some((t) => t.closable && t.key !== tab.key);
  const hasClosable = tabsStore.tabs.some((t) => t.closable);

  const menus: ContextmenuItem[] = [
    {
      label: "关闭",
      disabled: !tab.closable,
      callback: () => closeTab(tab.key),
    },
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
  ];

  contextmenu.pop({
    mousePosition: { x: e.clientX, y: e.clientY },
    menus,
    width: 140,
  });
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
