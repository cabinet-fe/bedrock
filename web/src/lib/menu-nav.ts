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
