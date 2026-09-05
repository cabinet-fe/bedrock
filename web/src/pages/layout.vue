<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { NavItem } from "@veltra/desktop";
import { AiChat, Logout } from "@veltra/icons/normal";

import AiChatWorkspace from "@/components/ai-chat/ai-chat-workspace.vue";
import AppBreadcrumb from "@/components/app-breadcrumb";
import AppWorkspaceTabs from "@/components/app-workspace-tabs";
import BrandLogo from "@/components/brand-logo";
import MenuSearch from "@/components/menu-search";
import NotificationBell from "@/components/notification-bell";
import ThemeSwitcher from "@/components/theme-switcher";
import { resolveRouteTitle } from "@/composables/use-breadcrumb";
import { menuGroupsToGroupNav } from "@/lib/menu-nav";
import { useAiChatStore } from "@/stores/ai-chat";
import { useAuthStore } from "@/stores/auth";
import { useTabsStore } from "@/stores/tabs";

const auth = useAuthStore();
const tabsStore = useTabsStore();
const aiChat = useAiChatStore();
const router = useRouter();
const route = useRoute();

/** Embed mode: strip shell UI for iframe embedding (dashboard dialogs). */
const embed = computed(() => route.query._embed === "1");

const displayName = computed(() => auth.user?.display_name || auth.user?.username || "");
const nameInitial = computed(() => {
  const name = displayName.value.trim();
  return name ? name.charAt(0).toUpperCase() : "?";
});

const navGroups = computed(() => menuGroupsToGroupNav(auth.menus));
const currentPath = computed(() => route.path);

watch(
  () => [route.fullPath, auth.menus] as const,
  () => {
    if (embed.value) return;
    tabsStore.syncFromRoute(route, resolveRouteTitle(route, auth.menus));
  },
  { immediate: true },
);

function onVisibility() {
  if (document.visibilityState === "visible") {
    void auth.refreshMe();
  }
}

onMounted(() => {
  document.addEventListener("visibilitychange", onVisibility);
});

onUnmounted(() => {
  document.removeEventListener("visibilitychange", onVisibility);
});

async function handleLogout() {
  await auth.logout();
  await router.replace({ name: "login" });
}

function onNavClick(item: NavItem) {
  if (item.path && !item.disabled) {
    void router.push(item.path);
  }
}
</script>

<template>
  <!-- Embed mode: bare page content for iframe embedding -->
  <main v-if="embed" class="app-embed">
    <router-view v-slot="{ Component, route: viewRoute }">
      <component :is="Component" :key="viewRoute.path" class="app-page" />
    </router-view>
  </main>

  <div v-else class="app-shell">
    <aside v-if="!aiChat.aiModeActive" class="app-sidebar">
      <div class="app-sidebar__brand">
        <BrandLogo />
      </div>
      <u-group-nav
        class="app-nav"
        :groups="navGroups"
        :current-path="currentPath"
        @item-click="onNavClick"
      />
    </aside>

    <div class="app-body">
      <!-- Thin continuous rail: crumb + quiet utilities on one height; tabs as whisper ledge -->
      <header class="app-rail">
        <div class="app-rail__bar">
          <AppBreadcrumb v-if="!aiChat.aiModeActive" />
          <div v-else class="app-rail__ai-title">
            <u-icon :size="16" class="ai-icon">
              <AiChat />
            </u-icon>
            <span>全局 AI 对话模式</span>
          </div>
          <div class="app-rail__utils" role="group" aria-label="操作区">
            <u-button
              :type="aiChat.aiModeActive ? 'primary' : 'text'"
              class="app-rail__ai-btn"
              title="切换全局 AI 模式"
              @click="aiChat.toggleAiMode()"
            >
              <u-icon :size="14">
                <AiChat />
              </u-icon>
              {{ aiChat.aiModeActive ? "退出 AI 模式" : "AI 模式" }}
            </u-button>
            <MenuSearch v-if="!aiChat.aiModeActive" />
            <ThemeSwitcher />
            <NotificationBell v-if="!aiChat.aiModeActive" />
            <span class="app-rail__identity">
              <span class="app-rail__avatar" aria-hidden="true">{{ nameInitial }}</span>
              <span class="user-name">{{ displayName }}</span>
            </span>
            <u-button text type="primary" class="app-rail__logout" @click="handleLogout">
              <u-icon :size="14">
                <Logout />
              </u-icon>
              退出
            </u-button>
          </div>
        </div>
        <div v-if="!aiChat.aiModeActive" class="app-rail__tabs">
          <AppWorkspaceTabs />
        </div>
      </header>

      <main class="app-main" :class="{ 'is-ai-mode': aiChat.aiModeActive }">
        <AiChatWorkspace v-if="aiChat.aiModeActive" @exit="aiChat.toggleAiMode(false)" />
        <router-view v-else v-slot="{ Component, route: viewRoute }">
          <Transition name="fade" mode="out-in">
            <component :is="Component" :key="viewRoute.path" class="app-page" />
          </Transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.app-embed {
  height: 100%;
  overflow: hidden;
  background: fn.use-var(bg-color, bottom);
  color: fn.use-var(text-color, main);
}

.app-shell {
  height: 100%;
  display: flex;
  overflow: hidden;
  background: fn.use-var(bg-color, bottom);
  color: fn.use-var(text-color, main);
}

.app-sidebar {
  --sidebar-width: 240px;

  flex-shrink: 0;
  width: var(--sidebar-width);
  min-width: var(--sidebar-width);
  max-width: var(--sidebar-width);
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: none;
  outline: none;
  /* 侧栏底色随主题 nav 配置（--u-nav-bg-color），"侧栏=深/浅"切换时整体可复现 */
  background: var(--u-nav-bg-color);
  box-shadow: 4px 0 24px rgb(0 0 0 / 28%);

  /* 品牌文字随侧栏前景色（--u-nav-*）走，与 group-nav 同源，深/浅侧栏均可读 */
  :deep(.brand-logo__name) {
    color: var(--u-nav-second-color);
  }

  :deep(.brand-logo__sub) {
    color: var(--u-nav-strong-color);
  }
}

.app-sidebar__brand {
  position: relative;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  min-height: 68px;
  padding: 0 16px;
  border: none;
  /* 品牌区铺一层极浅的绢面渐变，自上而淡，与导航拉开层次而不成卡片；
     以侧栏前景色淡调出，深浅侧栏下均有层次且不打架 */
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--u-nav-strong-color) 7%, transparent) 0%,
    transparent 100%
  );

  /* 与导航共用同一底色，仅靠一条柔和发丝线区分，避免割裂感 */
  &::after {
    content: "";
    position: absolute;
    right: 0;
    bottom: 0;
    left: 0;
    height: 1px;
    background: color-mix(in srgb, fn.use-var(border, muted-color) 70%, transparent);
  }
}

.app-nav {
  flex: 1;
  min-height: 0;
  width: 100%;
  overflow: hidden;
  border: none;
  /* 抵消 u-group-nav 自带的卡片样式，使其融入侧边栏底色 */
  border-radius: 0;
  background: transparent;
  box-shadow: none;

  :deep(*) {
    border: none;
  }
}

.app-body {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: fn.use-var(bg-color, middle);
}

/* ── Thin continuous rail ──
   Single-height bar: breadcrumb left, quiet utils right; tabs whisper below */
.app-rail {
  --rail-pad-x: #{fn.use-var(gap, large)};

  position: relative;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: fn.use-var(bg-color, top);
  z-index: 2;
}

.app-rail__bar {
  display: flex;
  align-items: center;
  gap: 16px;
  min-height: 44px;
  padding: 0 var(--rail-pad-x);
}

.app-rail__utils {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}

.app-rail__identity {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin: 0;
  padding: 0 2px;
}

.app-rail__avatar {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: color-mix(in srgb, fn.use-var(color, primary) 28%, fn.use-var(bg-color, bottom));
  color: fn.use-var(text-color, title);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  line-height: 1;
}

.user-name {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: fn.use-var(text-color, second);
  font-size: fn.use-var(font-size-main, default);
  font-weight: 400;
}

.app-rail__logout {
  gap: 4px;
}

.app-rail__ai-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: fn.use-var(text-color, title);

  .ai-icon {
    color: fn.use-var(color, primary);
  }
}

.app-rail__ai-btn {
  gap: 4px;
}

.app-rail__tabs {
  min-width: 0;
  padding: 0 var(--rail-pad-x) 0;
}

.app-main {
  /* 不能用 height: 100%：那会占满 .app-body 全高并顶出 rail 的高度，
     导致页面底部被 overflow:hidden 裁掉一截 */
  flex: 1;
  min-height: 0;

  &.is-ai-mode {
    display: flex;
    overflow: hidden;
  }
}

.app-page {
  gap: fn.use-var(gap, large);
  padding: fn.use-var(gap, large);
  height: 100%;
}
</style>
