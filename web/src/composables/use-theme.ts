import { ref, watch } from "vue";
import { useConfig } from "@veltra/compositions";
import type { ComponentSize } from "@veltra/utils";
import {
  glassTheme,
  loadTheme,
  midnightTheme,
  navSidebarTokens,
  oceanTheme,
  sakuraTheme,
  type UITheme,
} from "@veltra/styles/theme";

import { bedrockTheme } from "@/theme/bedrock";
import { bedrockDarkTheme } from "@/theme/bedrock-dark";

export interface ThemeOption {
  id: string;
  name: string;
  desc: string;
  theme: UITheme;
}

/**
 * 浅色系预设官方默认深色侧栏（nav.variant 默认 dark = 深底浅字），而本应用
 * 布局为扁平浅色侧栏（layout.vue .app-nav 透明 + 侧栏底色随 --u-nav-bg-color），
 * 因此浅色预设需切 nav.variant: 'light'（浅底深字），并补上匹配的浅底色。
 */
function withLightSidebar(theme: UITheme): UITheme {
  return theme.new({
    nav: { variant: "light", "bg-color": theme.theme.bg.color.bottom },
  });
}

/** 主题注册表：磐石（默认）为品牌主题，其余为 veltra 官方预设 */
export const THEME_OPTIONS: ThemeOption[] = [
  { id: "bedrock", name: "磐石", desc: "宣纸古风 · 亮", theme: bedrockTheme },
  { id: "bedrock-dark", name: "黛夜", desc: "墨池古风 · 暗", theme: bedrockDarkTheme },
  { id: "glass", name: "玻璃", desc: "拟态通透 · 暗", theme: glassTheme },
  { id: "midnight", name: "午夜", desc: "深空靛蓝 · 暗", theme: midnightTheme },
  { id: "sakura", name: "樱花", desc: "春雪柔粉 · 亮", theme: withLightSidebar(sakuraTheme) },
  { id: "ocean", name: "海盐", desc: "青瓷冷白 · 亮", theme: withLightSidebar(oceanTheme) },
];

export const DEFAULT_THEME_ID = THEME_OPTIONS[0]!.id;

const THEME_STORAGE_KEY = "bedrock.theme";
const SIZE_STORAGE_KEY = "bedrock.size";
const RADIUS_STORAGE_KEY = "bedrock.radius";
const NAV_VARIANT_STORAGE_KEY = "bedrock.nav-variant";

function readStored<T extends string>(key: string, valid: readonly T[], fallback: T): T {
  try {
    const stored = localStorage.getItem(key);
    if (stored && (valid as readonly string[]).includes(stored)) return stored as T;
  } catch {
    // ignore storage errors
  }
  return fallback;
}

/* ── 可调配置：主题 / 组件尺寸 / 圆角 / 侧栏（侧栏外观由 nav.variant 决定） ── */

export const CURRENT_SIZE_OPTIONS = ["small", "default", "large"] as const;
export type ThemeSize = (typeof CURRENT_SIZE_OPTIONS)[number];

export const CURRENT_RADIUS_OPTIONS = ["sharp", "default", "soft"] as const;
export type RadiusMode = (typeof CURRENT_RADIUS_OPTIONS)[number];

export const CURRENT_NAV_VARIANT_OPTIONS = ["follow", "dark", "light"] as const;
export type NavVariantMode = (typeof CURRENT_NAV_VARIANT_OPTIONS)[number];

export const currentThemeID = ref(readStored(THEME_STORAGE_KEY, THEME_OPTIONS.map((o) => o.id), DEFAULT_THEME_ID));
export const currentSize = ref<ComponentSize>(readStored(SIZE_STORAGE_KEY, CURRENT_SIZE_OPTIONS, "default"));
export const currentRadiusMode = ref<RadiusMode>(readStored(RADIUS_STORAGE_KEY, CURRENT_RADIUS_OPTIONS, "default"));
export const currentNavVariant = ref<NavVariantMode>(
  readStored(NAV_VARIANT_STORAGE_KEY, CURRENT_NAV_VARIANT_OPTIONS, "follow"),
);

/** 组合当前配置得到生效主题（与 veltra playground 同逻辑） */
export function buildEffectiveTheme(): UITheme {
  const option = THEME_OPTIONS.find((o) => o.id === currentThemeID.value) ?? THEME_OPTIONS[0]!;
  let base = option.theme;

  if (currentRadiusMode.value === "sharp") {
    base = base.new({ radius: { small: 0, default: 0, large: 0 } });
  } else if (currentRadiusMode.value === "soft") {
    const r = base.theme.radius;
    base = base.new({ radius: { small: r.small + 2, default: r.default + 6, large: r.large + 8 } });
  }

  if (currentNavVariant.value !== "follow") {
    // 强制侧栏变体：展开整套变体 token 覆盖预设侧栏个性（如樱花深酒红底），
    // 保证前景 / 底色 + 侧栏底（layout 用 --u-nav-bg-color）三处配套。
    const nav: Record<string, string> = { variant: currentNavVariant.value };
    for (const [name, value] of Object.entries(navSidebarTokens(base.series, currentNavVariant.value))) {
      nav[name.replace(/^--u-nav-/, "")] = value;
    }
    base = base.new({ nav });
  }
  return base;
}

/** 应用配置：注入 token + 组件尺寸 + 持久化 */
export function applySettings(): void {
  loadTheme(buildEffectiveTheme());
  setConfig({ size: currentSize.value });
  persistSettings();
}

function persistSettings(): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, currentThemeID.value);
    localStorage.setItem(SIZE_STORAGE_KEY, currentSize.value);
    localStorage.setItem(RADIUS_STORAGE_KEY, currentRadiusMode.value);
    localStorage.setItem(NAV_VARIANT_STORAGE_KEY, currentNavVariant.value);
  } catch {
    // ignore storage errors
  }
}

const { setConfig } = useConfig();

/** 任一配置变化时立即生效（含组件尺寸、主题、圆角、侧栏） */
watch([currentThemeID, currentSize, currentRadiusMode, currentNavVariant], applySettings);

/** 入口启动时恢复用户上次选择的配置 */
export function initTheme(): void {
  applySettings();
}

/** 全量恢复默认（主题=磐石，尺寸=中，圆角=默认，侧栏=跟随主题） */
export function resetThemeSettings(): void {
  currentThemeID.value = DEFAULT_THEME_ID;
  currentSize.value = "default";
  currentRadiusMode.value = "default";
  currentNavVariant.value = "follow";
  applySettings();
}
