<script setup lang="ts">
import { computed, ref } from "vue";
import { Check, Setting } from "@veltra/icons/normal";
// USegment 不在 @veltra/vite 的组件表（components.gen.ts 为旧版桌面生成），显式导入并带样式
import { USegment } from "@veltra/desktop";
import "@veltra/desktop/components/segment/style.js";

import {
  THEME_OPTIONS,
  currentNavVariant,
  currentRadiusMode,
  currentSize,
  currentThemeID,
  resetThemeSettings,
  type ThemeOption,
} from "@/composables/use-theme";

const showDrawer = ref(false);

const themeGroups = computed(() => [
  {
    label: "浅色系",
    items: THEME_OPTIONS.filter((o) => o.theme.series === "light"),
  },
  {
    label: "深色系",
    items: THEME_OPTIONS.filter((o) => o.theme.series === "dark"),
  },
]);

const currentName = computed(
  () => THEME_OPTIONS.find((o) => o.id === currentThemeID.value)?.name ?? "",
);

const sizeOptions = [
  { label: "小", value: "small" },
  { label: "中", value: "default" },
  { label: "大", value: "large" },
];

const radiusOptions = [
  { label: "直角", value: "sharp" },
  { label: "默认", value: "default" },
  { label: "圆润", value: "soft" },
];

const navVariantOptions = [
  { label: "跟随主题", value: "follow" },
  { label: "深色", value: "dark" },
  { label: "浅色", value: "light" },
];

/** 迷你页卡预览：从主题 token 取色，与目标主题同源 */
function previewVars(item: ThemeOption) {
  const t = item.theme.theme;
  return {
    "--tp-bg": t.bg.color.bottom,
    "--tp-surface": t.bg.color.top,
    "--tp-primary": t.color.primary,
    "--tp-border": t.border.mutedColor,
    "--tp-text": t["text-color"].second,
    "--tp-title": t["text-color"].title,
    "--tp-radius": Math.min(t.radius.large, 12) + "px",
  };
}
</script>

<template>
  <u-button
    text
    type="primary"
    class="theme-trigger"
    :aria-label="`主题设置（当前：${currentName}）`"
    @click="showDrawer = true"
  >
    <u-icon :size="18">
      <Setting />
    </u-icon>
  </u-button>

  <u-drawer v-model="showDrawer" title="主题" direction="right" :show-close="true">
    <div class="drawer-content">
      <section class="drawer-section">
        <div v-for="group in themeGroups" :key="group.label" class="theme-group">
          <div class="theme-group-label">{{ group.label }}</div>
          <div class="theme-grid">
            <button
              v-for="item in group.items"
              :key="item.id"
              type="button"
              class="theme-card"
              :class="{ 'is-active': item.id === currentThemeID }"
              :style="previewVars(item)"
              @click="currentThemeID = item.id"
            >
              <span class="theme-preview" aria-hidden="true">
                <span class="theme-preview__side"></span>
                <span class="theme-preview__body">
                  <span class="theme-preview__line theme-preview__line--title"></span>
                  <span class="theme-preview__pill"></span>
                  <span class="theme-preview__line"></span>
                </span>
              </span>
              <span class="theme-card__meta">
                <span class="theme-card__name">{{ item.name }}</span>
                <u-icon v-if="item.id === currentThemeID" class="theme-card__check" :size="14">
                  <Check />
                </u-icon>
              </span>
            </button>
          </div>
        </div>
      </section>

      <section class="drawer-section">
        <h3 class="drawer-section-title">组件尺寸</h3>
        <USegment v-model="currentSize" :items="sizeOptions" block />
      </section>

      <section class="drawer-section">
        <h3 class="drawer-section-title">圆角</h3>
        <u-segment v-model="currentRadiusMode" :items="radiusOptions" block />
      </section>

      <section class="drawer-section">
        <h3 class="drawer-section-title">侧栏</h3>
        <u-segment v-model="currentNavVariant" :items="navVariantOptions" block />
      </section>

      <footer class="drawer-footer">
        <u-button plain size="small" @click="resetThemeSettings">恢复默认</u-button>
      </footer>
    </div>
  </u-drawer>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.theme-trigger {
  min-width: 32px;
  min-height: 32px;
  padding: 0 6px;
}

.drawer-content {
  display: flex;
  flex-direction: column;
  gap: 24px;
  padding: 20px;
}

.drawer-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.drawer-section-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.theme-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.theme-group-label {
  font-size: 11px;
  color: fn.use-var(text-color, second);
  letter-spacing: 0.05em;
}

.theme-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.theme-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 7px;
  background: fn.use-var(bg-color, middle);
  border: 1px solid fn.use-var(border, color);
  border-radius: 10px;
  cursor: pointer;
  text-align: left;
  transition:
    border-color var(--u-transition-fast) ease,
    box-shadow var(--u-transition-fast) ease,
    transform var(--u-transition-fast) ease;

  &:hover {
    transform: translateY(-1px);
    box-shadow: var(--u-shadow-sm);
    border-color: fn.use-var(border, muted-color);
  }

  &.is-active {
    border-color: fn.use-var(color, primary);
    box-shadow: 0 0 0 1px fn.use-var(color, primary);

    .theme-card__name {
      color: fn.use-var(color, primary);
      font-weight: 600;
    }
  }
}

.theme-preview {
  display: flex;
  gap: 6px;
  height: 64px;
  padding: 7px;
  background: var(--tp-bg);
  border: 1px solid var(--tp-border);
  border-radius: var(--tp-radius);
  overflow: hidden;
}

.theme-preview__side {
  width: 26%;
  background: var(--tp-surface);
  border-right: 1px solid var(--tp-border);
}

.theme-preview__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 6px;
}

.theme-preview__line {
  height: 5px;
  width: 86%;
  border-radius: 3px;
  background: var(--tp-text);
  opacity: 0.35;

  &--title {
    width: 58%;
    background: var(--tp-title);
    opacity: 0.8;
  }
}

.theme-preview__pill {
  width: 52%;
  height: 13px;
  border-radius: 999px;
  background: var(--tp-primary);
}

.theme-card__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2px 2px;
}

.theme-card__name {
  font-size: 12px;
  font-weight: 500;
  color: fn.use-var(text-color, main);
}

.theme-card__check {
  font-size: 14px;
  color: fn.use-var(color, primary);
}

.drawer-footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 16px;
  border-top: 1px solid fn.use-var(border, color);
}
</style>
