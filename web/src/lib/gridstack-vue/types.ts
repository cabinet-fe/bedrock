/**
 * Vue wrapper type extensions — same identifiers as core `gridstack`, extended when imported from `gridstack/dist/vue`.
 * All three framework wrappers (Vue, React, Angular) use the same `component`/`props` field names for widget JSON.
 */
import type {
  GridStackOptions as CoreGridStackOptions,
  GridStackWidget as CoreGridStackWidget,
  GridStackNode as CoreGridStackNode,
  GridHTMLElement as CoreGridHTMLElement,
  GridItemHTMLElement as CoreGridItemHTMLElement,
} from "gridstack";

/** Host API stamped on `.grid-stack` element as `_gridComp` for `addRemoveCB` dispatch. */
export interface GridStackHostApi {
  registerSyntheticItemId(id: string): void;
  unregisterSyntheticItemId(id: string): void;
}

export interface GridStackWidget extends CoreGridStackWidget {
  /** Key in the `components` map passed to `<GridStack :components="...">` */
  component?: string;
  /** Runtime DOM node when removing via `addRemoveCB` (not serialized). */
  el?: HTMLElement;
}

export interface GridStackNode extends CoreGridStackNode {
  component?: string;
}

export interface GridStackOptions extends Omit<CoreGridStackOptions, "children"> {
  children?: GridStackWidget[];
}

export interface GridHTMLElement extends CoreGridHTMLElement {
  _gridComp?: GridStackHostApi;
}

export interface GridItemHTMLElement extends CoreGridItemHTMLElement {
  _gridItemRef?: { id: string; gridComp: GridStackHostApi };
}
