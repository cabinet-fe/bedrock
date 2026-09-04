import { computed, ref, watch } from "vue";
import { House } from "@veltra/icons/normal";

import type { MenuGroupNode } from "@/api/types";
import { resolveMenuIcon } from "@/lib/menu-nav";
import { useAuthStore } from "@/stores/auth";
import { MENU_KEYWORDS, calculateScore } from "./helper";
import type { SearchMenuGroup, SearchMenuItem } from "./types";

export function useMenuSearch() {
  const auth = useAuthStore();

  const searchQuery = ref("");
  const activeIndex = ref(0);

  /** All searchable items collected from auth.menus and top-level pages */
  const allMenuItems = computed<SearchMenuItem[]>(() => {
    const items: SearchMenuItem[] = [];
    const registeredPaths = new Set<string>();

    // Add Home
    items.push({
      id: "home",
      title: "首页",
      path: "/",
      groupTitle: "快捷导航",
      icon: House,
      keywords: MENU_KEYWORDS["/"] ?? [],
    });
    registeredPaths.add("/");

    const groups: MenuGroupNode[] = auth.menus ?? [];
    for (const group of groups) {
      for (const child of group.children ?? []) {
        if (!child.path || registeredPaths.has(child.path)) continue;
        registeredPaths.add(child.path);

        items.push({
          id: child.path,
          title: child.title,
          path: child.path,
          groupTitle: group.title,
          icon: resolveMenuIcon(child.path, child.icon),
          keywords: MENU_KEYWORDS[child.path] ?? [],
        });
      }
    }

    return items;
  });

  /** Grouped results for visual rendering */
  const groupedResults = computed<SearchMenuGroup[]>(() => {
    const q = searchQuery.value.trim().toLowerCase();
    if (!q) {
      const groups: SearchMenuGroup[] = [];
      const map = new Map<string, SearchMenuItem[]>();

      for (const item of allMenuItems.value) {
        let list = map.get(item.groupTitle);
        if (!list) {
          list = [];
          map.set(item.groupTitle, list);
          groups.push({ title: item.groupTitle, items: list });
        }
        list.push(item);
      }
      return groups;
    }

    const scored = allMenuItems.value
      .map((item) => ({ item, score: calculateScore(item, q) }))
      .filter((entry) => entry.score > 0);

    const groupMap = new Map<
      string,
      { maxScore: number; items: { item: SearchMenuItem; score: number }[] }
    >();

    for (const entry of scored) {
      let group = groupMap.get(entry.item.groupTitle);
      if (!group) {
        group = { maxScore: entry.score, items: [] };
        groupMap.set(entry.item.groupTitle, group);
      } else if (entry.score > group.maxScore) {
        group.maxScore = entry.score;
      }
      group.items.push(entry);
    }

    return Array.from(groupMap.entries())
      .map(([title, data]) => ({
        title,
        maxScore: data.maxScore,
        items: data.items.sort((a, b) => b.score - a.score).map((e) => e.item),
      }))
      .sort((a, b) => (b.maxScore ?? 0) - (a.maxScore ?? 0));
  });

  /** Flattened items in the exact visual display order (top-to-bottom) */
  const visibleItems = computed<SearchMenuItem[]>(() => {
    return groupedResults.value.flatMap((g) => g.items);
  });

  const activeItem = computed<SearchMenuItem | undefined>(
    () => visibleItems.value[activeIndex.value],
  );

  // Reset active index when query changes
  watch(searchQuery, () => {
    activeIndex.value = 0;
  });

  function onArrowDown(onScroll?: () => void) {
    const count = visibleItems.value.length;
    if (count === 0) return;
    activeIndex.value = (activeIndex.value + 1) % count;
    onScroll?.();
  }

  function onArrowUp(onScroll?: () => void) {
    const count = visibleItems.value.length;
    if (count === 0) return;
    activeIndex.value = (activeIndex.value - 1 + count) % count;
    onScroll?.();
  }

  function setActiveIndexByItem(item: SearchMenuItem) {
    const idx = visibleItems.value.findIndex((it) => it.id === item.id);
    if (idx !== -1) {
      activeIndex.value = idx;
    }
  }

  function resetSearch() {
    searchQuery.value = "";
    activeIndex.value = 0;
  }

  return {
    searchQuery,
    activeIndex,
    allMenuItems,
    groupedResults,
    visibleItems,
    activeItem,
    onArrowDown,
    onArrowUp,
    setActiveIndexByItem,
    resetSearch,
  };
}
