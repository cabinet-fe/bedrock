import { createApp } from "vue";
import { createPinia } from "pinia";
import { vLoading } from "@veltra/desktop";
import "@veltra/styles/normalize";
import "@veltra/styles/transitions";
import "@veltra/desktop/components/message/style.js";
import "@veltra/desktop/components/message-confirm/style.js";
import "@veltra/desktop/components/loading/style.js";

import App from "./App.vue";
import router from "./router";
import { setOnAuthExpired } from "./api/http";
import { useAuthStore } from "./stores/auth";
import { initTheme } from "./composables/use-theme";

// loadTheme writes html[data-theme=light] per theme family; Veltra injects light component CSS vars from it.
// Restore the user's last theme from localStorage (default: ancient-style light).
initTheme();

const app = createApp(App);
const pinia = createPinia();

app.use(pinia);
app.use(router);

// Resolver covers U* components only; directives must be registered manually.
app.directive("loading", vLoading);

setOnAuthExpired(() => {
  const auth = useAuthStore();
  auth.clearSession();
  if (router.currentRoute.value.name !== "login") {
    void router.replace({ name: "login" });
  }
});

app.mount("#app");
