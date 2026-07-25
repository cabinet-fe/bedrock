import {
  defineComponent,
  h,
  onBeforeUnmount,
  onMounted,
  provide,
  ref,
  type Component,
  type PropType,
} from "vue";
import { GridStack, Utils } from "gridstack";
import type { GridStackNodesHandler } from "gridstack";
import { GS_CONTEXT_KEY } from "./gridstack-context";
import { GridStackItem } from "./gridstack-item";
import { installGridStackVueCallbacks } from "./registry";
import type {
  GridHTMLElement,
  GridItemHTMLElement,
  GridStackHostApi,
  GridStackOptions,
  GridStackWidget,
} from "./types";

/** Maps `component` JSON keys to Vue components. */
export type ComponentMap = Record<string, Component>;

/**
 * `<GridStack>` — root component.
 *
 * - Pass `options` (with `children`) to seed the initial layout.
 * - Pass `components` to map `component` strings in widget JSON to Vue components.
 * - Emits layout changes and exposes `getGrid()` for imperative access.
 */
export const GridStackComponent = defineComponent({
  name: "GridStack",

  props: {
    options: {
      type: Object as PropType<GridStackOptions>,
      required: true,
    },
    components: {
      type: Object as PropType<ComponentMap>,
      required: true,
    },
  },

  emits: ["change"],

  setup(props, { emit, expose }) {
    installGridStackVueCallbacks();

    // Raw (non-reactive) reference to the GridStack instance.
    // IMPORTANT: never wrap in Vue ref() — Vue proxies break GS internals.
    let grid: GridStack | null = null;

    /**
     * Reactive flag that flips to true once the grid is initialized.
     * Used so the render function reacts to grid availability without proxying the GS instance.
     */
    const gridReady = ref(false);

    /** Bumped after GS-driven structural changes (added/removed) so descendant composables re-compute. */
    const layoutVersion = ref(0);

    /** IDs of widgets added by `addRemoveCB` that need a teleport anchor. */
    const syntheticIds = ref<Set<string>>(new Set());

    function registerSyntheticItemId(id: string) {
      const next = new Set(syntheticIds.value);
      next.add(id);
      syntheticIds.value = next;
    }

    function unregisterSyntheticItemId(id: string) {
      const next = new Set(syntheticIds.value);
      next.delete(id);
      syntheticIds.value = next;
    }

    const hostApi: GridStackHostApi = {
      registerSyntheticItemId,
      unregisterSyntheticItemId,
    };

    /** Reference to the `.grid-stack` root div — set via `onVnodeMounted`. */
    let rootEl: GridHTMLElement | null = null;

    onMounted(() => {
      if (!rootEl) return;

      // Stamp _gridComp BEFORE calling init() so that addRemoveCB fires during
      // init() can already reach the host API to register synthetic item IDs.
      rootEl._gridComp = hostApi;

      // Remove orphaned .grid-stack-item children left by prior hot-reload cycles
      // before calling init() so GS doesn't auto-register stale elements.
      const itemClass =
        ((props.options as Record<string, unknown>).itemClass as string | undefined) ??
        "grid-stack-item";
      Array.from(rootEl.children).forEach((child) => {
        if (child.classList.contains(itemClass) && !(child as GridItemHTMLElement).gridstackNode) {
          child.remove();
        }
      });

      const instance = GridStack.init(props.options, rootEl);
      if (!instance) return;
      grid = instance;
      gridReady.value = true;
      layoutVersion.value++;

      hookEvents();
    });

    onBeforeUnmount(() => {
      unhookEvents();
      if (rootEl) delete rootEl._gridComp;
      grid?.destroy(false);
      grid = null;
      gridReady.value = false;
    });

    function hookEvents() {
      if (!grid) return;

      grid.on("added", (() => {
        layoutVersion.value++;
      }) as GridStackNodesHandler);

      // change = position/resize; portal targets don't move so no layoutVersion bump.
      grid.on("change", ((e: Event, nodes: Parameters<GridStackNodesHandler>[1]) => {
        emit("change", e, nodes);
      }) as GridStackNodesHandler);

      grid.on("removed", (() => {
        layoutVersion.value++;
      }) as GridStackNodesHandler);
    }

    function unhookEvents() {
      if (!grid) return;
      ["added", "change", "removed"].forEach((ev) => grid!.off(ev));
    }

    expose({
      getGrid: () => grid,
    });

    // Provide context so GridStackItem and composables can reach the grid.
    provide(GS_CONTEXT_KEY, {
      get grid() {
        return grid;
      },
      layoutVersion,
    });

    return () => {
      // Build teleport anchors for GS-driven (synthetic) widget additions.
      const synItems: ReturnType<typeof h>[] = [];
      if (gridReady.value && grid && syntheticIds.value.size > 0) {
        for (const synId of syntheticIds.value) {
          const node = Utils.findInGrid(grid, synId, true) as GridStackWidget | undefined;
          if (!node?.component) continue;
          const Comp = props.components[node.component];
          if (!Comp) continue;
          synItems.push(
            h(
              GridStackItem,
              { key: `__gs_syn__${synId}`, id: synId, options: node as Partial<GridStackWidget> },
              { default: () => h(Comp) },
            ),
          );
        }
      }

      return h("div", { class: "gs-wrapper" }, [
        // The `.grid-stack` div — onVnodeMounted captures the DOM reference.
        h(
          "div",
          {
            class: "grid-stack",
            onVnodeMounted: (vnode) => {
              rootEl = vnode.el as GridHTMLElement | null;
            },
          },
          synItems,
        ),
      ]);
    };
  },
});
