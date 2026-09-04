<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { Search } from "@veltra/icons/normal";

import { shortcutLabel } from "./helper";
import MenuSearchModal from "./menu-search-modal.vue";
import type { SearchMenuItem } from "./types";

export type { SearchMenuItem };

const isOpen = ref(false);

function openModal() {
  isOpen.value = true;
}

function closeModal() {
  isOpen.value = false;
}

function toggleModal() {
  isOpen.value = !isOpen.value;
}

/** Global shortcut listener */
function onGlobalKeydown(e: KeyboardEvent) {
  const isK = e.key.toLowerCase() === "k";
  if (isK && (e.metaKey || e.ctrlKey)) {
    e.preventDefault();
    toggleModal();
    return;
  }

  if (e.key === "/" && !isOpen.value) {
    const target = e.target as HTMLElement | null;
    const tagName = target?.tagName?.toLowerCase();
    const isEditable =
      target?.isContentEditable ||
      tagName === "input" ||
      tagName === "textarea" ||
      tagName === "select";

    if (!isEditable) {
      e.preventDefault();
      openModal();
      return;
    }
  }

  if (e.key === "Escape" && isOpen.value) {
    e.preventDefault();
    closeModal();
  }
}

onMounted(() => {
  window.addEventListener("keydown", onGlobalKeydown);
});

onUnmounted(() => {
  window.removeEventListener("keydown", onGlobalKeydown);
});
</script>

<template>
  <!-- Search Trigger Button in Header Rail -->
  <button
    type="button"
    class="menu-search-trigger"
    :aria-label="`搜索菜单 (${shortcutLabel})`"
    @click="openModal"
  >
    <u-icon class="menu-search-trigger__icon" :size="14">
      <Search />
    </u-icon>
    <span class="menu-search-trigger__text">搜索菜单...</span>
    <kbd class="menu-search-trigger__kbd">{{ shortcutLabel }}</kbd>
  </button>

  <MenuSearchModal v-model:open="isOpen" />
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

/* ── Search Trigger Button (Rail Header) ── */
.menu-search-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 28px;
  padding: 0 8px 0 10px;
  border-radius: 6px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 70%, transparent);
  background: color-mix(in srgb, fn.use-var(border, muted-color) 18%, transparent);
  color: fn.use-var(text-color, second);
  cursor: pointer;
  outline: none;
  user-select: none;
  transition:
    background-color var(--u-transition-fast) ease,
    border-color var(--u-transition-fast) ease,
    color var(--u-transition-fast) ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
    border-color: fn.use-var(color, primary);
    color: fn.use-var(color, primary);

    .menu-search-trigger__kbd {
      border-color: color-mix(in srgb, fn.use-var(color, primary) 45%, transparent);
      color: fn.use-var(color, primary);
    }
  }

  &:focus-visible {
    border-color: fn.use-var(color, primary);
    box-shadow: 0 0 0 2px color-mix(in srgb, fn.use-var(color, primary) 25%, transparent);
  }
}

.menu-search-trigger__icon {
  flex-shrink: 0;
  opacity: 0.85;
}

.menu-search-trigger__text {
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
}

.menu-search-trigger__kbd {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 18px;
  padding: 0 5px;
  border-radius: 4px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 65%, transparent);
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 80%, transparent);
  color: fn.use-var(text-color, second);
  font-family: inherit;
  font-size: 11px;
  font-weight: 500;
  line-height: 1;
  transition:
    border-color var(--u-transition-fast) ease,
    color var(--u-transition-fast) ease;
}

@media (max-width: 820px) {
  .menu-search-trigger__text,
  .menu-search-trigger__kbd {
    display: none;
  }

  .menu-search-trigger {
    padding: 0 6px;
    min-width: 28px;
  }
}
</style>
