import { darkTheme, type UITheme } from "@veltra/styles/theme";

/**
 * 黛夜主题 — 与磐石同源的深色古风：墨池底 / 宣纸银字 / 松烟绿主色。
 * 深色系下组件级 token 由 loadTheme 按系列注入，全局 token 在此定制。
 */
export const bedrockDarkTheme: UITheme = darkTheme.new({
  color: {
    primary: "#5f9b82", // 松烟绿提亮一档，暗底上保持可读
    success: "#4d9e6f",
    warning: "#c99a4b",
    danger: "#c96a52",
    info: "#5a9aa8",
    disabled: "#26261f",
    default: "#26261f",
  },
  bg: {
    color: {
      bottom: "#12140f", // 墨池底
      middle: "#1a1d16",
      top: "#22261d",
      hover: "#2b3026",
      black: "#000000",
    },
    filter: {
      blur: "none",
      saturate: "none",
    },
  },
  "text-color": {
    title: "#ece8db", // 宣纸白
    main: "#c9c4b4",
    second: "#98917f",
    assist: "#6b6555",
    placeholder: "#6b6555",
    disabled: "#4a463c",
    white: "#ffffff",
  },
  border: {
    color: "#33392e",
    mutedColor: "#3d4436",
  },
  shadow: {
    color: "#00000066",
    sm: "0 1px 2px rgba(0, 0, 0, 0.4)",
    lg: "0 8px 24px rgba(0, 0, 0, 0.5), 0 2px 6px rgba(0, 0, 0, 0.35)",
  },
});
