import { ancientTheme, type UITheme } from "@veltra/styles/theme";

/**
 * 磐石主题 — 宣纸 / 黛墨 / 松烟绿的古风亮色系，与登录页同源。
 * 基于官方 ancientTheme 派生，补齐 1.7 主题模型新增 token：
 * shadow.sm / shadow.lg 按古风赭墨阴影定制，shadow.color 用 hex+alpha
 * （旧 rgba 写法无法生成 --u-shadow-color-a-* 系列 token）。
 * 全局颜色均经 Veltra token 由 loadTheme 注入；业务侧勿为配色硬编码色值。
 */
export const bedrockTheme: UITheme = ancientTheme.new({
  nav: {
    // 侧栏由 nav.variant 决定前景色系：light = 浅底深字（默认 dark 为深底浅字）。
    // 本应用布局把 group-nav 侧栏压平成宣纸浅底（layout.vue .app-nav 透明 + 侧栏底色），
    // 因此必须同时声明 light variant 与匹配的浅底，否则前景仍是深色侧栏的白字（不可读）。
    variant: "light",
    "bg-color": "#f1ede0", // 宣纸底，与 .app-sidebar 底色一致
  },
  shadow: {
    color: "#40362024",
    sm: "0 1px 2px rgba(64, 54, 32, 0.10)",
    lg: "0 8px 24px rgba(64, 54, 32, 0.16), 0 2px 6px rgba(64, 54, 32, 0.08)",
  },
});
