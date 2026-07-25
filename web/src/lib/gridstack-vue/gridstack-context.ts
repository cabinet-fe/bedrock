import type { InjectionKey, Ref } from "vue";
import type { GridStack } from "gridstack";

interface GsContext {
  /** Raw GridStack instance — NOT a Vue ref (GS proxies break internals). */
  grid: GridStack | null;
  /** Bumped after GS-driven layout changes so descendants can re-sync. */
  layoutVersion: Ref<number>;
}

interface GsItemContext {
  id: string;
}

export const GS_CONTEXT_KEY: InjectionKey<GsContext> = Symbol("gs-context");
export const GS_ITEM_CONTEXT_KEY: InjectionKey<GsItemContext> = Symbol("gs-item-context");
