/// <reference types="vite-plus/client" />

interface ImportMetaEnv {
  readonly VITE_BEDROCK_ENCRYPTION_KEY?: string;
  readonly VITE_APP_VERSION?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

interface Window {
  __BEDROCK_ENCRYPTION_KEY__?: string;
  /** Injected by the server (embedded build); undefined (dev) means register open. */
  __BEDROCK_ALLOW_REGISTER__?: boolean;
}

declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<{}, {}, unknown>;
  export default component;
}

declare module "*.md?raw" {
  const content: string;
  export default content;
}
