import { ancientTheme, type UITheme } from "@veltra/styles/theme";

/**
 * Bedrock theme — an ancient-style light palette of rice paper / ink / pine smoke green, same source as the login page.
 * Derived from the official ancientTheme, filling in the tokens added by the 1.7 theme model:
 * shadow.sm / shadow.lg tuned as ancient ochre-ink shadows; shadow.color uses hex+alpha
 * (the old rgba syntax cannot generate the --u-shadow-color-a-* token series).
 * All global colors are injected via Veltra tokens by loadTheme; do not hard-code palette values in business code.
 */
export const bedrockTheme: UITheme = ancientTheme.new({
  nav: {
    // The sidebar foreground scheme comes from nav.variant: light = light bg, dark text (default dark = dark bg, light text).
    // This app's layout flattens the group-nav sidebar onto a rice-paper light bg (layout.vue .app-nav transparent + sidebar bg),
    // so the light variant and a matching light bg must be declared together, or the foreground stays the dark sidebar's white text (unreadable).
    variant: "light",
    "bg-color": "#f1ede0", // rice-paper base, matching .app-sidebar bg
  },
  shadow: {
    color: "#40362024",
    sm: "0 1px 2px rgba(64, 54, 32, 0.10)",
    lg: "0 8px 24px rgba(64, 54, 32, 0.16), 0 2px 6px rgba(64, 54, 32, 0.08)",
  },
});
