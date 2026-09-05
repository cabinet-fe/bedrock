<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { ScrollExposed } from "@veltra/desktop";
import { Clear, Enter, Search } from "@veltra/icons/normal";

import { highlightParts } from "./helper";
import type { SearchMenuItem } from "./types";
import { useMenuSearch } from "./use-menu-search";

const open = defineModel<boolean>("open", { required: true });

const router = useRouter();
const route = useRoute();

const inputRef = ref<HTMLInputElement | null>(null);
const scrollRef = ref<ScrollExposed | null>(null);
const listContainerRef = ref<HTMLElement | null>(null);

const {
  searchQuery,
  groupedResults,
  visibleItems,
  activeItem,
  onArrowDown,
  onArrowUp,
  setActiveIndexByItem,
  resetSearch,
} = useMenuSearch();

function scrollToActive() {
  void nextTick(() => {
    const activeEl = listContainerRef.value?.querySelector(".docsearch-item.is-active");
    if (activeEl && typeof activeEl.scrollIntoView === "function") {
      activeEl.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  });
}

function closeModal() {
  open.value = false;
  resetSearch();
}

function handleSelect(item: SearchMenuItem) {
  closeModal();
  if (item.path && item.path !== route.path) {
    void router.push(item.path);
  }
}

function onEnterKey() {
  const item = activeItem.value;
  if (item) {
    handleSelect(item);
  }
}

watch(open, (isOpen) => {
  if (isOpen) {
    resetSearch();
    void nextTick(() => {
      scrollRef.value?.scrollTo({ y: 0 });
      inputRef.value?.focus();
    });
  }
});

watch(searchQuery, () => {
  void nextTick(() => {
    scrollRef.value?.scrollTo({ y: 0 });
  });
});
</script>

<template>
  <Teleport to="body">
    <Transition name="docsearch">
      <div v-if="open" class="docsearch-backdrop" @click.self="closeModal">
        <div class="docsearch-modal" role="dialog" aria-modal="true" aria-label="搜索菜单">
          <!-- Search Input Header -->
          <header class="docsearch-header">
            <u-icon class="docsearch-header__icon" :size="18">
              <Search />
            </u-icon>
            <input
              ref="inputRef"
              v-model="searchQuery"
              class="docsearch-header__input"
              type="text"
              placeholder="搜索菜单、功能或路径 (支持拼音与关键词)..."
              autocomplete="off"
              spellcheck="false"
              @keydown.down.prevent="onArrowDown(scrollToActive)"
              @keydown.up.prevent="onArrowUp(scrollToActive)"
              @keydown.enter.prevent="onEnterKey"
              @keydown.esc.prevent="closeModal"
            />
            <button
              v-if="searchQuery"
              type="button"
              class="docsearch-header__clear"
              aria-label="清除搜索"
              @click="
                searchQuery = '';
                inputRef?.focus();
              "
            >
              <u-icon :size="14">
                <Clear />
              </u-icon>
            </button>
            <kbd class="docsearch-kbd docsearch-kbd--esc" @click="closeModal">ESC</kbd>
          </header>

          <!-- Search Results List -->
          <u-scroll ref="scrollRef" class="docsearch-body">
            <div ref="listContainerRef" class="docsearch-content">
              <!-- Empty State -->
              <div v-if="visibleItems.length === 0" class="docsearch-empty">
                <div class="docsearch-empty__icon">
                  <u-icon :size="24">
                    <Search />
                  </u-icon>
                </div>
                <div class="docsearch-empty__title">未找到与「{{ searchQuery }}」相关的菜单</div>
                <div class="docsearch-empty__desc">
                  请尝试搜索菜单名称、所属分类、路径或拼音简写
                </div>
              </div>

              <!-- Grouped Results -->
              <div v-else class="docsearch-groups">
                <section v-for="group in groupedResults" :key="group.title" class="docsearch-group">
                  <header class="docsearch-group__title">{{ group.title }}</header>
                  <div class="docsearch-group__items">
                    <div
                      v-for="item in group.items"
                      :key="item.id"
                      class="docsearch-item"
                      :class="{ 'is-active': activeItem?.id === item.id }"
                      role="option"
                      :aria-selected="activeItem?.id === item.id"
                      @mouseenter="setActiveIndexByItem(item)"
                      @click="handleSelect(item)"
                    >
                      <!-- Left Icon Box -->
                      <div class="docsearch-item__icon">
                        <u-icon :size="15">
                          <component :is="item.icon" />
                        </u-icon>
                      </div>

                      <!-- Middle Info -->
                      <div class="docsearch-item__content">
                        <div class="docsearch-item__title">
                          <template
                            v-for="(part, idx) in highlightParts(item.title, searchQuery)"
                            :key="idx"
                          >
                            <mark v-if="part.highlight" class="docsearch-highlight">{{
                              part.text
                            }}</mark>
                            <span v-else>{{ part.text }}</span>
                          </template>
                        </div>
                        <div class="docsearch-item__meta">
                          <span class="docsearch-item__group">{{ item.groupTitle }}</span>
                          <span class="docsearch-item__divider">·</span>
                          <span class="docsearch-item__path">
                            <template
                              v-for="(part, idx) in highlightParts(item.path, searchQuery)"
                              :key="idx"
                            >
                              <mark v-if="part.highlight" class="docsearch-highlight">{{
                                part.text
                              }}</mark>
                              <span v-else>{{ part.text }}</span>
                            </template>
                          </span>
                        </div>
                      </div>

                      <!-- Right Action Hint -->
                      <div class="docsearch-item__action">
                        <span class="docsearch-item__action-text">跳转</span>
                        <u-icon :size="12">
                          <Enter />
                        </u-icon>
                      </div>
                    </div>
                  </div>
                </section>
              </div>
            </div>
          </u-scroll>

          <!-- Modal Footer with Shortcuts Legend -->
          <footer class="docsearch-footer">
            <div class="docsearch-footer__commands">
              <span class="docsearch-footer__cmd">
                <kbd class="docsearch-kbd">↑</kbd>
                <kbd class="docsearch-kbd">↓</kbd>
                <span>切换</span>
              </span>
              <span class="docsearch-footer__cmd">
                <kbd class="docsearch-kbd">↵</kbd>
                <span>跳转</span>
              </span>
              <span class="docsearch-footer__cmd">
                <kbd class="docsearch-kbd">ESC</kbd>
                <span>关闭</span>
              </span>
            </div>
            <div class="docsearch-footer__count">{{ visibleItems.length }} 个菜单</div>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

/* ── DocSearch Backdrop Overlay ── */
.docsearch-backdrop {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: min(12vh, 90px) 16px 32px;
  background: rgba(12, 14, 20, 0.58);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}

/* ── DocSearch Modal Container ── */
.docsearch-modal {
  position: relative;
  width: 600px;
  max-width: 100%;
  height: 520px;
  max-height: min(520px, 80vh);
  display: flex;
  flex-direction: column;
  background: fn.use-var(bg-color, top);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 70%, transparent);
  border-radius: 14px;
  box-shadow:
    0 0 0 1px color-mix(in srgb, fn.use-var(border, muted-color) 20%, transparent),
    0 24px 64px -12px rgba(0, 0, 0, 0.45),
    0 8px 24px -4px rgba(0, 0, 0, 0.25);
  overflow: hidden;
}

/* ── DocSearch Header / Input ── */
.docsearch-header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  height: 54px;
  padding: 0 18px;
  background: fn.use-var(bg-color, top);
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 45%, transparent);
  box-sizing: border-box;
}

.docsearch-header__icon {
  flex-shrink: 0;
  color: fn.use-var(color, primary);
}

.docsearch-header__input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  outline: none;
  background: transparent;
  color: fn.use-var(text-color, title);
  font-size: 14.5px;
  font-family: inherit;
  caret-color: fn.use-var(color, primary);

  &::placeholder {
    color: fn.use-var(text-color, assist);
    font-size: 14px;
  }
}

.docsearch-header__clear {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: fn.use-var(text-color, second);
  cursor: pointer;
  transition:
    background-color var(--u-transition-fast) ease,
    color var(--u-transition-fast) ease;

  &:hover {
    background: fn.use-var(bg-color, hover);
    color: fn.use-var(text-color, title);
  }
}

/* ── Common Kbd Badge ── */
.docsearch-kbd {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  border-radius: 4px;
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 65%, transparent);
  background: color-mix(in srgb, fn.use-var(bg-color, middle) 75%, transparent);
  color: fn.use-var(text-color, second);
  font-family: inherit;
  font-size: 11px;
  font-weight: 500;
  line-height: 1;
  box-shadow: 0 1px 1px rgba(0, 0, 0, 0.08);

  &--esc {
    cursor: pointer;
    &:hover {
      background: fn.use-var(bg-color, hover);
      color: fn.use-var(text-color, title);
    }
  }
}

/* ── DocSearch Body / Results ── */
.docsearch-body {
  flex: 1;
  min-height: 0;
}

.docsearch-content {
  padding: 10px 14px 14px;
}

/* ── Groups & Items ── */
.docsearch-groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.docsearch-group {
  display: flex;
  flex-direction: column;
}

.docsearch-group__title {
  padding: 4px 8px 4px;
  font-size: 11px;
  font-weight: 600;
  color: fn.use-var(text-color, second);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.docsearch-group__items {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.docsearch-item {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 50px;
  padding: 8px 12px;
  border-radius: 8px;
  border: 1px solid transparent;
  background: transparent;
  cursor: pointer;
  user-select: none;
  transition:
    background-color 0.12s ease,
    border-color 0.12s ease;

  &:hover,
  &.is-active {
    background: color-mix(in srgb, fn.use-var(color, primary) 10%, fn.use-var(bg-color, hover));
    border-color: color-mix(in srgb, fn.use-var(color, primary) 25%, transparent);

    .docsearch-item__icon {
      background: color-mix(in srgb, fn.use-var(color, primary) 20%, transparent);
      color: fn.use-var(color, primary);
    }

    .docsearch-item__title {
      color: fn.use-var(color, primary);
    }

    .docsearch-item__action {
      opacity: 1;
      transform: translateX(0);
      pointer-events: auto;
    }
  }
}

.docsearch-item__icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 6px;
  background: color-mix(in srgb, fn.use-var(border, muted-color) 25%, transparent);
  color: fn.use-var(text-color, main);
  transition:
    background-color 0.12s ease,
    color 0.12s ease;
}

.docsearch-item__content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.docsearch-item__title {
  font-size: 13.5px;
  font-weight: 500;
  color: fn.use-var(text-color, title);
  line-height: 1.4;
  transition: color 0.12s ease;
}

.docsearch-item__meta {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  color: fn.use-var(text-color, second);
  line-height: 1.2;
}

.docsearch-item__group {
  opacity: 0.85;
}

.docsearch-item__divider {
  opacity: 0.45;
}

.docsearch-item__path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  opacity: 0.75;
}

.docsearch-highlight {
  background: transparent;
  color: fn.use-var(color, primary);
  text-decoration: underline;
  text-underline-offset: 2px;
  font-weight: 600;
}

.docsearch-item__action {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border-radius: 4px;
  background: color-mix(in srgb, fn.use-var(color, primary) 14%, transparent);
  color: fn.use-var(color, primary);
  font-size: 11.5px;
  font-weight: 500;
  opacity: 0;
  pointer-events: none;
  transform: translateX(-4px);
  transition:
    opacity 0.12s ease,
    transform 0.12s ease;
}

.docsearch-item__action-text {
  line-height: 1;
}

/* ── Empty State ── */
.docsearch-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 44px 16px;
  text-align: center;
}

.docsearch-empty__icon {
  display: grid;
  place-items: center;
  width: 46px;
  height: 46px;
  border-radius: 50%;
  background: color-mix(in srgb, fn.use-var(border, muted-color) 20%, transparent);
  color: fn.use-var(text-color, assist);
  margin-bottom: 12px;
}

.docsearch-empty__title {
  font-size: 14px;
  font-weight: 500;
  color: fn.use-var(text-color, title);
  margin-bottom: 6px;
}

.docsearch-empty__desc {
  font-size: 12px;
  color: fn.use-var(text-color, second);
}

/* ── Modal Footer ── */
.docsearch-footer {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 44px;
  padding: 0 18px;
  border-top: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 45%, transparent);
  background: color-mix(in srgb, fn.use-var(bg-color, middle) 60%, fn.use-var(bg-color, top));
  font-size: 11.5px;
  color: fn.use-var(text-color, second);
  box-sizing: border-box;
}

.docsearch-footer__commands {
  display: flex;
  align-items: center;
  gap: 16px;
}

.docsearch-footer__cmd {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.docsearch-footer__count {
  opacity: 0.85;
}

/* ── Transitions ── */
.docsearch-enter-active,
.docsearch-leave-active {
  transition: opacity 0.18s ease;

  .docsearch-modal {
    transition:
      transform 0.18s cubic-bezier(0.16, 1, 0.3, 1),
      opacity 0.18s ease;
  }
}

.docsearch-enter-from,
.docsearch-leave-to {
  opacity: 0;

  .docsearch-modal {
    opacity: 0;
    transform: scale(0.97) translateY(-8px);
  }
}
</style>
