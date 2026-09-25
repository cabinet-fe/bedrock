import { darkTheme, type UITheme } from "@veltra/styles/theme";

/**
 * Dark-night theme — dark ancient style sharing Bedrock's roots: ink-pool bg / rice-paper silver text / pine smoke green accent.
 * Component-level tokens for dark are injected by loadTheme per family; global tokens are customized here.
 */
export const bedrockDarkTheme: UITheme = darkTheme.new({
  color: {
    primary: "#5f9b82", // pine smoke green one step brighter, readable on dark
    success: "#4d9e6f",
    warning: "#c99a4b",
    danger: "#c96a52",
    info: "#5a9aa8",
    disabled: "#26261f",
    default: "#26261f",
  },
  bg: {
    color: {
      bottom: "#12140f", // ink-pool base
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
    title: "#ece8db", // rice-paper white
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
