import { computed, inject } from "vue";
import { Utils } from "gridstack";
import type { GridStackNode } from "gridstack";
import { GS_CONTEXT_KEY, GS_ITEM_CONTEXT_KEY } from "./gridstack-context";

interface UseGridStackItemResult {
  id: string;
  node: GridStackNode | undefined;
}

/**
 * Returns the widget `id` and the live `GridStackNode` for the widget that
 * contains the calling component. Must be called inside `<GridStackItem>` slot content.
 *
 * Recomputes whenever `layoutVersion` changes.
 * Uses recursive grid search so items dragged to sub-grids are still found.
 */
export function useGridStackItem(): UseGridStackItemResult {
  const itemCtx = inject(GS_ITEM_CONTEXT_KEY);
  if (!itemCtx) throw new Error("useGridStackItem must be used inside <GridStackItem> content");

  const gsCtx = inject(GS_CONTEXT_KEY);
  if (!gsCtx) throw new Error("useGridStackItem must be used within <GridStack>");

  const node = computed<GridStackNode | undefined>(() => {
    // Access layoutVersion to make this reactive.
    void gsCtx.layoutVersion.value;
    const g = gsCtx.grid;
    return g ? Utils.findInGrid(g, String(itemCtx.id), true) : undefined;
  });

  return {
    id: itemCtx.id,
    get node() {
      return node.value;
    },
  };
}
