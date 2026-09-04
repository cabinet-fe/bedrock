import type { GroupNavGroup, NavItem } from "@veltra/desktop";
import {
  Agent,
  Books,
  Build,
  Checklist,
  Folder,
  GitBranch,
  History,
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
  User,
} from "@veltra/icons/normal";
import type { Component } from "vue";

import type { MenuGroupNode } from "@/api/types";

/** 后端 icon 为空时按 path 回退；仅前端展示预设，菜单仍以后端下发为准 */
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
  "/cicd/build-runs": History,
  "/cicd/script-jobs": Terminal,
  "/cicd/script-runs": History,
  "/cicd/pipelines": Share,
  "/cicd/pipeline-runs": History,
  "/project/projects": Folder,
  "/project/requirements": Checklist,
  "/project/docs": Books,
  "/project/dev-docs": Books,
  "/ai/agents": Agent,
  "/ai/runs": History,
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
 * 匹配路由对应的菜单图标：
 * 1. 优先在当前菜单树中通过最长前缀匹配（让详情页如 /project/projects/1 与其入口 /project/projects 图标保持一致）；
 * 2. 降级匹配内置默认菜单图标映射表 MENU_DEFAULT_ICONS（同样支持最长前缀匹配）；
 * 3. 根路径回退 House，其它回退 List。
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
