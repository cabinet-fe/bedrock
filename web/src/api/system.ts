import { http } from "./http";
import type {
  Dictionary,
  MenuGroup,
  NotificationItem,
  PageResult,
  PermissionCatalogGroup,
  RbacResource,
  Role,
  User,
} from "./types";

export type ListQuery = Record<string, string | number | boolean | undefined | null>;

function toQuery(params?: ListQuery): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  if (!params) return out;
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "") continue;
    out[k] = v;
  }
  return out;
}

export async function createUser(body: Record<string, unknown>): Promise<User> {
  const { body: data } = await http.post<User>("/users", body);
  return data;
}

export async function updateUser(id: number, body: Record<string, unknown>): Promise<User> {
  const { body: data } = await http.put<User>(`/users/${id}`, body);
  return data;
}

export async function deleteUser(id: number): Promise<void> {
  await http.delete(`/users/${id}`);
}

export async function listRoles(params?: ListQuery): Promise<PageResult<Role>> {
  const { body } = await http.get<PageResult<Role>>("/roles", { query: toQuery(params) });
  return body;
}

export async function createRole(body: Record<string, unknown>): Promise<Role> {
  const { body: data } = await http.post<Role>("/roles", body);
  return data;
}

export async function updateRole(id: number, body: Record<string, unknown>): Promise<Role> {
  const { body: data } = await http.put<Role>(`/roles/${id}`, body);
  return data;
}

export async function setRolePermissions(id: number, permissions: string[]): Promise<Role> {
  const { body } = await http.put<Role>(`/roles/${id}/permissions`, { permissions });
  return body;
}

export async function deleteRole(id: number): Promise<void> {
  await http.delete(`/roles/${id}`);
}

/** Three-level catalog for role permission editor. */
export async function getPermissionCatalog(): Promise<{ items: PermissionCatalogGroup[] }> {
  const { body } = await http.get<{ items: PermissionCatalogGroup[] }>("/roles/permission-catalog");
  return body;
}

export async function listMenuGroups(): Promise<{ items: MenuGroup[] }> {
  const { body } = await http.get<{ items: MenuGroup[] }>("/menu-groups");
  return body;
}

export async function createMenuGroup(body: Record<string, unknown>): Promise<MenuGroup> {
  const { body: data } = await http.post<MenuGroup>("/menu-groups", body);
  return data;
}

export async function updateMenuGroup(
  id: number,
  body: Record<string, unknown>,
): Promise<MenuGroup> {
  const { body: data } = await http.put<MenuGroup>(`/menu-groups/${id}`, body);
  return data;
}

export async function deleteMenuGroup(id: number): Promise<void> {
  await http.delete(`/menu-groups/${id}`);
}

export async function listResources(query?: {
  keyword?: string;
  type?: string;
  enabled?: string | boolean;
  group_id?: number | string;
}): Promise<{ items: RbacResource[] }> {
  const { body } = await http.get<{ items: RbacResource[] }>("/rbac/resources", {
    query: toQuery(query),
  });
  return body;
}

export async function createResource(body: Record<string, unknown>): Promise<RbacResource> {
  const { body: data } = await http.post<RbacResource>("/rbac/resources", body);
  return data;
}

export async function updateResource(
  id: number,
  body: Record<string, unknown>,
): Promise<RbacResource> {
  const { body: data } = await http.put<RbacResource>(`/rbac/resources/${id}`, body);
  return data;
}

export async function deleteResource(id: number): Promise<void> {
  await http.delete(`/rbac/resources/${id}`);
}

/** Upload/replace menu icon (raw ≤32KB). */
export async function updateResourceIcon(
  id: number,
  iconBase64: string,
  iconMime?: string,
): Promise<RbacResource> {
  const { body } = await http.put<RbacResource>(`/rbac/resources/${id}/icon`, {
    icon_base64: iconBase64,
    icon_mime: iconMime,
  });
  return body;
}

export async function getDictionary(id: number): Promise<Dictionary> {
  const { body } = await http.get<Dictionary>(`/dictionaries/${id}`);
  return body;
}

export async function getDictionaryByCode(code: string): Promise<Dictionary> {
  const { body } = await http.get<Dictionary>(`/dictionaries/code/${encodeURIComponent(code)}`);
  return body;
}

export async function createDictionary(body: Record<string, unknown>): Promise<Dictionary> {
  const { body: data } = await http.post<Dictionary>("/dictionaries", body);
  return data;
}

export async function updateDictionary(
  id: number,
  body: Record<string, unknown>,
): Promise<Dictionary> {
  const { body: data } = await http.put<Dictionary>(`/dictionaries/${id}`, body);
  return data;
}

export async function deleteDictionary(id: number): Promise<void> {
  await http.delete(`/dictionaries/${id}`);
}

export async function listNotifications(params?: ListQuery): Promise<PageResult<NotificationItem>> {
  const { body } = await http.get<PageResult<NotificationItem>>("/notifications", {
    query: toQuery(params),
  });
  return body;
}

export async function markNotificationRead(id: number): Promise<void> {
  await http.put(`/notifications/${id}/read`);
}

export async function markAllNotificationsRead(): Promise<void> {
  await http.put("/notifications/read-all");
}

export function notificationWsUrl(token: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/ws/notifications?token=${encodeURIComponent(token)}`;
}

export async function clearOperationLogs(): Promise<void> {
  await http.delete("/operation-logs");
}
