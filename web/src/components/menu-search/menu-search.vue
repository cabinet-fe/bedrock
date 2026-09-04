<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Component } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Clear, Enter, House, Search } from "@veltra/icons/normal";

import type { MenuGroupNode } from "@/api/types";
import { resolveMenuIcon } from "@/lib/menu-nav";
import { useAuthStore } from "@/stores/auth";

export interface SearchMenuItem {
  id: string;
  title: string;
  path: string;
  groupTitle: string;
  icon: Component | string;
  keywords: string[];
}

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const isOpen = ref(false);
const searchQuery = ref("");
const activeIndex = ref(0);
const inputRef = ref<HTMLInputElement | null>(null);
const listContainerRef = ref<HTMLElement | null>(null);

/** Shortcut indicator based on operating system */
const isMac = typeof navigator !== "undefined" && /macintosh|mac os x/i.test(navigator.userAgent);
const shortcutLabel = isMac ? "⌘ K" : "Ctrl K";

/** Keywords map for fast and intelligent fuzzy lookup across languages & pinyin initials */
const MENU_KEYWORDS: Record<string, string[]> = {
  "/": ["home", "shouye", "sy", "index", "dashboard", "概览", "工作台", "主页"],
  "/handbook": ["handbook", "manual", "shouce", "sc", "help", "docs", "帮助", "指南"],
  "/ops/processes": ["process", "jincheng", "jc", "proc", "pm2", "ops", "运维", "进程"],
  "/ops/dev-environments": ["dev", "env", "environment", "huanjing", "hj", "容器", "环境"],
  "/resource/repositories": ["repo", "repository", "git", "cangku", "ck", "code", "代码仓库"],
  "/resource/servers": ["server", "fuwuqi", "fwq", "host", "node", "服务器", "主机"],
  "/resource/credentials": [
    "credential",
    "pingzheng",
    "pz",
    "secret",
    "key",
    "password",
    "凭证",
    "密钥",
  ],
  "/resource/tokens": ["token", "lingpai", "lp", "auth", "api key", "访问令牌"],
  "/cicd/build-jobs": ["build", "job", "task", "goujian", "gj", "rw", "构建", "编译"],
  "/cicd/build-runs": ["build", "run", "history", "jilu", "jl", "构建记录", "历史"],
  "/cicd/script-jobs": ["script", "shell", "job", "task", "jiaoben", "jb", "脚本任务"],
  "/cicd/script-runs": ["script", "run", "history", "脚本记录"],
  "/cicd/pipelines": ["pipeline", "flow", "liushuixian", "lsx", "ci", "cd", "流水线", "编排"],
  "/cicd/pipeline-runs": ["pipeline", "run", "history", "流水线运行", "执行记录"],
  "/project/projects": ["project", "xiangmu", "xm", "app", "项目列表", "工程"],
  "/project/requirements": ["requirement", "xuqiu", "xq", "issue", "demand", "需求", "迭代"],
  "/project/docs": ["docs", "api", "swagger", "jiekou", "jk", "wendang", "wd", "接口文档"],
  "/project/dev-docs": ["docs", "dev", "kaifa", "kf", "wendang", "开发文档", "架构"],
  "/ai/agents": ["agent", "ai", "bot", "zhinengti", "znt", "智能体"],
  "/ai/runs": ["ai", "run", "history", "record", "运行记录", "智能体执行"],
  "/ai/skills": ["skill", "jineng", "jn", "tool", "ai", "技能", "插件"],
  "/system/users": ["user", "member", "account", "yonghu", "yh", "用户", "成员", "账号"],
  "/system/roles": ["role", "permission", "juese", "js", "角色", "权限组"],
  "/system/resources": ["resource", "perm", "auth", "quanxian", "qx", "zy", "权限资源", "菜单"],
  "/system/dictionaries": ["dictionary", "dict", "zidian", "zd", "数据字典", "配置"],
  "/system/operation-logs": ["log", "audit", "rizhi", "rz", "caozuo", "cz", "操作日志", "审计"],
};

/** All searchable items collected from auth.menus and top-level pages */
const allMenuItems = computed<SearchMenuItem[]>(() => {
  const items: SearchMenuItem[] = [];
  const registeredPaths = new Set<string>();

  // Add Home
  items.push({
    id: "home",
    title: "首页",
    path: "/",
    groupTitle: "快捷导航",
    icon: House,
    keywords: MENU_KEYWORDS["/"] ?? [],
  });
  registeredPaths.add("/");

  const groups: MenuGroupNode[] = auth.menus ?? [];
  for (const group of groups) {
    for (const child of group.children ?? []) {
      if (!child.path || registeredPaths.has(child.path)) continue;
      registeredPaths.add(child.path);

      items.push({
        id: child.path,
        title: child.title,
        path: child.path,
        groupTitle: group.title,
        icon: resolveMenuIcon(child.path, child.icon),
        keywords: MENU_KEYWORDS[child.path] ?? [],
      });
    }
  }

  return items;
});

/** Fuzzy score calculation for ranking search results */
function calculateScore(item: SearchMenuItem, query: string): number {
  if (!query) return 1;

  const q = query.toLowerCase();
  const title = item.title.toLowerCase();
  const path = item.path.toLowerCase();
  const group = item.groupTitle.toLowerCase();

  if (title === q) return 100;
  if (title.startsWith(q)) return 80;
  if (title.includes(q)) return 60;

  for (const kw of item.keywords) {
    const k = kw.toLowerCase();
    if (k === q) return 55;
    if (k.startsWith(q)) return 45;
    if (k.includes(q)) return 35;
  }

  if (path.includes(q)) return 30;
  if (group.includes(q)) return 20;

  return 0;
}

/** Grouped results for visual rendering */
const groupedResults = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) {
    const groups: { title: string; items: SearchMenuItem[] }[] = [];
    const map = new Map<string, SearchMenuItem[]>();

    for (const item of allMenuItems.value) {
      let list = map.get(item.groupTitle);
      if (!list) {
        list = [];
        map.set(item.groupTitle, list);
        groups.push({ title: item.groupTitle, items: list });
      }
      list.push(item);
    }
    return groups;
  }

  const scored = allMenuItems.value
    .map((item) => ({ item, score: calculateScore(item, q) }))
    .filter((entry) => entry.score > 0);

  const groupMap = new Map<
    string,
    { maxScore: number; items: { item: SearchMenuItem; score: number }[] }
  >();

  for (const entry of scored) {
    let group = groupMap.get(entry.item.groupTitle);
    if (!group) {
      group = { maxScore: entry.score, items: [] };
      groupMap.set(entry.item.groupTitle, group);
    } else if (entry.score > group.maxScore) {
      group.maxScore = entry.score;
    }
    group.items.push(entry);
  }

  return Array.from(groupMap.entries())
    .map(([title, data]) => ({
      title,
      maxScore: data.maxScore,
      items: data.items.sort((a, b) => b.score - a.score).map((e) => e.item),
    }))
    .sort((a, b) => b.maxScore - a.maxScore);
});

/** Flattened items in the exact visual display order (top-to-bottom) */
const visibleItems = computed<SearchMenuItem[]>(() => {
  return groupedResults.value.flatMap((g) => g.items);
});

const activeItem = computed<SearchMenuItem | undefined>(
  () => visibleItems.value[activeIndex.value],
);

// Reset active index when query changes
watch(searchQuery, () => {
  activeIndex.value = 0;
});

function scrollToActive() {
  void nextTick(() => {
    const activeEl = listContainerRef.value?.querySelector(".docsearch-item.is-active");
    if (activeEl && typeof activeEl.scrollIntoView === "function") {
      activeEl.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  });
}

function openModal() {
  isOpen.value = true;
  searchQuery.value = "";
  activeIndex.value = 0;
  void nextTick(() => {
    inputRef.value?.focus();
  });
}

function closeModal() {
  isOpen.value = false;
  searchQuery.value = "";
  activeIndex.value = 0;
}

function toggleModal() {
  if (isOpen.value) {
    closeModal();
  } else {
    openModal();
  }
}

function handleSelect(item: SearchMenuItem) {
  closeModal();
  if (item.path && item.path !== route.path) {
    void router.push(item.path);
  }
}

function onArrowDown() {
  const count = visibleItems.value.length;
  if (count === 0) return;
  activeIndex.value = (activeIndex.value + 1) % count;
  scrollToActive();
}

function onArrowUp() {
  const count = visibleItems.value.length;
  if (count === 0) return;
  activeIndex.value = (activeIndex.value - 1 + count) % count;
  scrollToActive();
}

function onEnterKey() {
  const item = activeItem.value;
  if (item) {
    handleSelect(item);
  }
}

function setActiveIndexByItem(item: SearchMenuItem) {
  const idx = visibleItems.value.findIndex((it) => it.id === item.id);
  if (idx !== -1) {
    activeIndex.value = idx;
  }
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

/** Highlight matched substring in text */
function highlightParts(text: string, query: string): { text: string; highlight: boolean }[] {
  const q = query.trim().toLowerCase();
  if (!q || !text) return [{ text, highlight: false }];

  const lower = text.toLowerCase();
  const idx = lower.indexOf(q);
  if (idx === -1) return [{ text, highlight: false }];

  const parts: { text: string; highlight: boolean }[] = [];
  let lastIndex = 0;
  let curr = lower.indexOf(q, lastIndex);
  while (curr !== -1) {
    if (curr > lastIndex) {
      parts.push({ text: text.slice(lastIndex, curr), highlight: false });
    }
    parts.push({ text: text.slice(curr, curr + q.length), highlight: true });
    lastIndex = curr + q.length;
    curr = lower.indexOf(q, lastIndex);
  }
  if (lastIndex < text.length) {
    parts.push({ text: text.slice(lastIndex), highlight: false });
  }
  return parts;
}
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

  <!-- DocSearch Modal Overlay -->
  <Teleport to="body">
    <Transition name="docsearch">
      <div v-if="isOpen" class="docsearch-backdrop" @click.self="closeModal">
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
              @keydown.down.prevent="onArrowDown"
              @keydown.up.prevent="onArrowUp"
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
          <div ref="listContainerRef" class="docsearch-body">
            <!-- Empty State -->
            <div v-if="visibleItems.length === 0" class="docsearch-empty">
              <div class="docsearch-empty__icon">
                <u-icon :size="24">
                  <Search />
                </u-icon>
              </div>
              <div class="docsearch-empty__title">未找到与「{{ searchQuery }}」相关的菜单</div>
              <div class="docsearch-empty__desc">请尝试搜索菜单名称、所属分类、路径或拼音简写</div>
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
                        <span class="docsearch-item__divider">/</span>
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
  max-height: min(600px, 80vh);
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
  display: flex;
  align-items: center;
  gap: 12px;
  height: 52px;
  padding: 0 16px;
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 45%, transparent);
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
  font-size: 15px;
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
  overflow-y: auto;
  padding: 8px 10px 12px;
}

/* ── Groups & Items ── */
.docsearch-groups {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.docsearch-group {
  display: flex;
  flex-direction: column;
}

.docsearch-group__title {
  padding: 6px 10px 4px;
  font-size: 11px;
  font-weight: 600;
  color: fn.use-var(text-color, second);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.docsearch-group__items {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.docsearch-item {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 48px;
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
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 40px;
  padding: 0 16px;
  border-top: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 45%, transparent);
  background: color-mix(in srgb, fn.use-var(bg-color, middle) 60%, fn.use-var(bg-color, top));
  font-size: 11.5px;
  color: fn.use-var(text-color, second);
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
