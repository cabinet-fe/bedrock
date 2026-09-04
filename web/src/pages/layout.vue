<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { NavItem } from "@veltra/desktop";
import { Logout } from "@veltra/icons/normal";

import AppBreadcrumb from "@/components/app-breadcrumb";
import AppWorkspaceTabs from "@/components/app-workspace-tabs";
import BrandLogo from "@/components/brand-logo";
import MenuSearch from "@/components/menu-search";
import NotificationBell from "@/components/notification-bell";
import ThemeSwitcher from "@/components/theme-switcher";
import { resolveRouteTitle } from "@/composables/use-breadcrumb";
import { menuGroupsToGroupNav } from "@/lib/menu-nav";
import { useAuthStore } from "@/stores/auth";
import { useTabsStore } from "@/stores/tabs";

const auth = useAuthStore();
const tabsStore = useTabsStore();
const router = useRouter();
const route = useRoute();

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
  <div class="app-shell">
    <aside class="app-sidebar">
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
          <AppBreadcrumb />
          <div class="app-rail__utils" role="group" aria-label="操作区">
            <MenuSearch />
            <ThemeSwitcher />
            <NotificationBell />
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
        <div class="app-rail__tabs">
          <AppWorkspaceTabs />
        </div>
      </header>

      <main class="app-main">
        <router-view v-slot="{ Component, route: viewRoute }">
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

.app-rail__tabs {
  min-width: 0;
  padding: 0 var(--rail-pad-x) 0;
}

.app-main {
  /* 不能用 height: 100%：那会占满 .app-body 全高并顶出 rail 的高度，
     导致页面底部被 overflow:hidden 裁掉一截 */
  flex: 1;
  min-height: 0;
}

.app-page {
  gap: fn.use-var(gap, large);
  padding: fn.use-var(gap, large);
  height: 100%;
}
</style>
