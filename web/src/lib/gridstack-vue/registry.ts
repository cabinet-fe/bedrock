/**
 * Static GridStack callbacks for Vue — same pattern as React gsCreateReactComponents.
 * Dispatches via DOM `_gridComp` / `_gridItemRef` back-refs (no closure over per-grid state).
 */
import { GridStack, Utils } from "gridstack";
import type { GridStackWidget as CoreGridStackWidget } from "gridstack";
import type { GridHTMLElement, GridItemHTMLElement, GridStackWidget } from "./types";

export function installGridStackVueCallbacks(): void {
  if (!GridStack.addRemoveCB) {
    GridStack.addRemoveCB = gsCreateVueComponents;
  }
}

function gsCreateVueComponents(
  parent: HTMLElement,
  w: CoreGridStackWidget,
  add: boolean,
  _isGrid: boolean,
): HTMLElement | undefined {
  if (add) {
    const gridHost = (parent as GridHTMLElement)._gridComp;
    if (!gridHost) return undefined;

    const opt = w as GridStackWidget;
    const el = Utils.createDiv(["grid-stack-item"]) as GridItemHTMLElement;
    Utils.createDiv(["grid-stack-item-content"], el);

    const id = opt.id != null ? String(opt.id) : undefined;
    if (id) {
      el._gridItemRef = { id, gridComp: gridHost };
      if (opt.component) gridHost.registerSyntheticItemId(id);
    }
    return el;
  }

  const el = (w as GridStackWidget).el as GridItemHTMLElement | undefined;
  if (el?._gridItemRef) {
    const { id, gridComp } = el._gridItemRef;
    gridComp.unregisterSyntheticItemId(id);
    delete el._gridItemRef;
  }
  el?.remove();
  return undefined;
}
