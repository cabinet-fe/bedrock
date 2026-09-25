import type { GroupNavGroup, NavItem } from "@veltra/desktop";
import {
  Agent,
  Books,
  Build,
  Checklist,
  Folder,
  GitBranch,
  House,
  Key,
  List,
  Process,
  Role,
  Secured,
  Server,
  Share,
  Skill,
  Terminal,
  Token,
  TriangleAlert,
  User,
} from "@veltra/icons/normal";
import type { Component } from "vue";

import type { MenuGroupNode } from "@/api/types";

/** Falls back by path when the backend icon is empty; display-only preset, menus still follow the backend */
const MENU_DEFAULT_ICONS: Record<string, Component> = {
  "/": House,
  "/handbook": Books,
  "/ops/processes": Process,
  "/ops/dev-environments": Terminal,
  "/resource/repositories": GitBranch,
  "/resource/servers": Server,
  "/resource/credentials": Key,
  "/resource/tokens": Token,
  "/cicd/build-jobs": Build,
  "/cicd/script-jobs": Terminal,
  "/cicd/pipelines": Share,
  "/project/projects": Folder,
  "/project/requirements": Checklist,
  "/project/bugs": TriangleAlert,
  "/project/docs": Books,
  "/project/dev-docs": Books,
  "/ai/agents": Agent,
  "/ai/skills": Skill,
  "/system/users": User,
  "/system/roles": Role,
  "/system/resources": Secured,
  "/system/dictionaries": Books,
  "/system/operation-logs": Checklist,
};

export function resolveMenuIcon(path: string, icon?: string): NonNullable<NavItem["icon"]> {
  if (icon) return icon;
  return (MENU_DEFAULT_ICONS[path] ?? List) as NonNullable<NavItem["icon"]>;
}

/** Map /auth/me MenuGroupNode[] → @veltra/desktop GroupNavGroup. */
export function menuGroupsToGroupNav(groups: MenuGroupNode[] | undefined | null): GroupNavGroup[] {
  if (!groups?.length) return [];
  return groups.map((group) => ({
    title: group.title,
    children: (group.children ?? []).map((child): NavItem => ({
      title: child.title,
      path: child.path,
      icon: resolveMenuIcon(child.path, child.icon),
    })),
  }));
}

/**
 * Matches the menu icon for a route:
 * 1. Prefer the longest prefix match in the current menu tree (keeps detail pages like /project/projects/1 on the same icon as their entry /project/projects);
 * 2. Fall back to the built-in MENU_DEFAULT_ICONS map (also longest-prefix);
 * 3. Root paths fall back to House, others to List.
 */
export function resolveRouteIcon(path: string, menus?: MenuGroupNode[] | null): Component | string {
  if (menus?.length) {
    let bestChild: { path: string; icon?: string } | null = null;
    let bestLen = -1;

    for (const group of menus) {
      for (const child of group.children ?? []) {
        const route = child.path;
        if (!route) continue;
        if (path === route || path.startsWith(`${route}/`)) {
          if (route.length > bestLen) {
            bestChild = child;
            bestLen = route.length;
          }
        }
      }
    }

    if (bestChild) {
      return resolveMenuIcon(bestChild.path, bestChild.icon);
    }
  }

  // fallback to MENU_DEFAULT_ICONS
  if (MENU_DEFAULT_ICONS[path]) {
    return MENU_DEFAULT_ICONS[path]!;
  }

  let bestPrefix = "";
  for (const p of Object.keys(MENU_DEFAULT_ICONS)) {
    if (p !== "/" && (path === p || path.startsWith(`${p}/`))) {
      if (p.length > bestPrefix.length) {
        bestPrefix = p;
      }
    }
  }
  if (bestPrefix && MENU_DEFAULT_ICONS[bestPrefix]) {
    return MENU_DEFAULT_ICONS[bestPrefix]!;
  }

  if (path === "/" || path === "") return House;
  return List;
}
