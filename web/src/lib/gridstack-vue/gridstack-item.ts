import {
  defineComponent,
  h,
  inject,
  onBeforeUnmount,
  onMounted,
  provide,
  ref,
  Teleport,
  watch,
  type PropType,
} from "vue";
import { Utils } from "gridstack";
import { GS_CONTEXT_KEY, GS_ITEM_CONTEXT_KEY } from "./gridstack-context";
import type { GridStackWidget } from "./types";

/**
 * Teleport anchor for one grid item.
 * Owns the Vue subtree rendered inside GridStack-managed DOM.
 *
 * The slot content is teleported into the GS-owned `.grid-stack-item-content` element,
 * so widget components never need to know about GS DOM internals.
 */
export const GridStackItem = defineComponent({
  name: "GridStackItem",

  props: {
    id: {
      type: String,
      required: true,
    },
    options: {
      type: Object as PropType<Partial<GridStackWidget>>,
      required: true,
    },
  },

  setup(props, { slots }) {
    const ctx = inject(GS_CONTEXT_KEY);
    if (!ctx) throw new Error("<GridStackItem> must be used inside <GridStack>");

    /** The `.grid-stack-item-content` element to teleport into — null until GS creates it. */
    const containerEl = ref<HTMLElement | null>(null);

    /** True once we are fully mounted and have not yet started unmounting. */
    let alive = false;

    function syncContainer() {
      const g = ctx!.grid;
      if (!g) return;
      const node = Utils.findInGrid(g, props.id, true);
      const cont =
        (node?.el?.querySelector(".grid-stack-item-content") as HTMLElement | null) ?? null;
      containerEl.value = cont;
    }

    onMounted(() => {
      alive = true;
      const g = ctx!.grid;
      if (!g) return;

      const node = Utils.findInGrid(g, props.id, true);
      if (!node?.el) {
        g.addWidget({ ...props.options, id: props.id } as GridStackWidget);
      } else {
        g.update(node.el, { ...props.options, id: props.id } as GridStackWidget);
      }

      syncContainer();

      // After teleport content renders, refresh drag handles to pick up
      // any custom handle selectors the slot content may have introduced.
      void Promise.resolve().then(() => {
        if (!alive) return;
        const n = Utils.findInGrid(g, props.id, true);
        if (n?.el) g.refreshDragHandles(n.el);
      });
    });

    // Re-sync the container whenever layoutVersion changes (GS rearranged the DOM).
    watch(() => ctx!.layoutVersion.value, syncContainer);

    onBeforeUnmount(() => {
      alive = false;

      const g = ctx!.grid;
      if (!g?.engine) return;
      const node = Utils.findInGrid(g, props.id, true);
      if (!node?.el) return;
      try {
        g.removeWidget(node.el, false);
      } catch {
        // already removed
      }
    });

    provide(GS_ITEM_CONTEXT_KEY, { id: props.id });

    return () => {
      const target = containerEl.value;
      if (!target || !slots.default) return null;
      return h(Teleport, { to: target }, slots.default());
    };
  },
});
