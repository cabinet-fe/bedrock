import { saveBlob } from "@cat-kit/fe";

import { http } from "./http";
import type {
  ApiDocNode,
  DevDocNode,
  PageResult,
  ProductProject,
  ProjectMember,
  ProjectRole,
  Requirement,
  RequirementAttachment,
  RequirementComment,
  RequirementStatusOption,
  UserOption,
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

export async function listProjects(params?: ListQuery): Promise<PageResult<ProductProject>> {
  const { body } = await http.get<PageResult<ProductProject>>("/projects", {
    query: toQuery(params),
  });
  return body;
}

export async function getProject(id: number): Promise<ProductProject> {
  const { body } = await http.get<ProductProject>(`/projects/${id}`);
  return body;
}

export async function createProject(input: Record<string, unknown>): Promise<ProductProject> {
  const { body } = await http.post<ProductProject>("/projects", input);
  return body;
}

export async function updateProject(
  id: number,
  input: Record<string, unknown>,
): Promise<ProductProject> {
  const { body } = await http.put<ProductProject>(`/projects/${id}`, input);
  return body;
}

export async function archiveProject(id: number): Promise<ProductProject> {
  const { body } = await http.post<ProductProject>(`/projects/${id}/archive`, {});
  return body;
}

export async function deleteProject(id: number): Promise<void> {
  await http.delete(`/projects/${id}`);
}

export async function listRequirementStatuses(): Promise<RequirementStatusOption[]> {
  const { body } = await http.get<{ items: RequirementStatusOption[] }>(
    "/projects/meta/requirement-statuses",
  );
  return body.items;
}

export async function listUserOptions(keyword?: string): Promise<UserOption[]> {
  const { body } = await http.get<{ items: UserOption[] }>("/projects/meta/user-options", {
    query: keyword ? { keyword } : undefined,
  });
  return body.items ?? [];
}

export async function listMembers(projectID: number): Promise<ProjectMember[]> {
  const { body } = await http.get<{ items: ProjectMember[] }>(`/projects/${projectID}/members`);
  return body.items ?? [];
}

export async function addProjectMember(
  projectID: number,
  userID: number,
  role: Exclude<ProjectRole, "owner">,
): Promise<ProjectMember> {
  const { body } = await http.post<ProjectMember>(`/projects/${projectID}/members`, {
    user_id: userID,
    role,
  });
  return body;
}

export async function updateProjectMember(
  projectID: number,
  userID: number,
  role: Exclude<ProjectRole, "owner">,
): Promise<ProjectMember> {
  const { body } = await http.put<ProjectMember>(`/projects/${projectID}/members/${userID}`, {
    role,
  });
  return body;
}

export async function removeProjectMember(projectID: number, userID: number): Promise<void> {
  await http.delete(`/projects/${projectID}/members/${userID}`);
}

export async function transferProjectOwner(
  projectID: number,
  userID: number,
): Promise<ProductProject> {
  const { body } = await http.post<ProductProject>(
    `/projects/${projectID}/members/transfer-owner`,
    {
      user_id: userID,
    },
  );
  return body;
}

export async function listRequirements(
  projectID: number,
  query?: Record<string, string | number | boolean | undefined>,
): Promise<PageResult<Requirement>> {
  const q = Object.fromEntries(
    Object.entries(query ?? {}).filter(([, v]) => v !== undefined && v !== ""),
  ) as Record<string, string | number | boolean>;
  const { body } = await http.get<PageResult<Requirement>>(`/projects/${projectID}/requirements`, {
    query: q,
  });
  return body;
}

export async function getRequirement(projectID: number, id: number): Promise<Requirement> {
  const { body } = await http.get<Requirement>(`/projects/${projectID}/requirements/${id}`);
  return body;
}

export async function createRequirement(
  projectID: number,
  input: Record<string, unknown>,
): Promise<Requirement> {
  const { body } = await http.post<Requirement>(`/projects/${projectID}/requirements`, input);
  return body;
}

export async function updateRequirement(
  projectID: number,
  id: number,
  input: Record<string, unknown>,
): Promise<Requirement> {
  const { body } = await http.put<Requirement>(`/projects/${projectID}/requirements/${id}`, input);
  return body;
}

export async function deleteRequirement(projectID: number, id: number): Promise<void> {
  await http.delete(`/projects/${projectID}/requirements/${id}`);
}

export async function listRequirementComments(
  projectID: number,
  requirementID: number,
): Promise<RequirementComment[]> {
  const { body } = await http.get<{ items: RequirementComment[] }>(
    `/projects/${projectID}/requirements/${requirementID}/comments`,
  );
  return body.items;
}

export async function createRequirementComment(
  projectID: number,
  requirementID: number,
  content: string,
): Promise<RequirementComment> {
  const { body } = await http.post<RequirementComment>(
    `/projects/${projectID}/requirements/${requirementID}/comments`,
    { content },
  );
  return body;
}

export async function updateRequirementComment(
  projectID: number,
  requirementID: number,
  commentID: number,
  content: string,
): Promise<RequirementComment> {
  const { body } = await http.put<RequirementComment>(
    `/projects/${projectID}/requirements/${requirementID}/comments/${commentID}`,
    { content },
  );
  return body;
}

export async function deleteRequirementComment(
  projectID: number,
  requirementID: number,
  commentID: number,
): Promise<void> {
  await http.delete(`/projects/${projectID}/requirements/${requirementID}/comments/${commentID}`);
}

export async function listRequirementAttachments(
  projectID: number,
  requirementID: number,
): Promise<RequirementAttachment[]> {
  const { body } = await http.get<{ items: RequirementAttachment[] }>(
    `/projects/${projectID}/requirements/${requirementID}/attachments`,
  );
  return body.items;
}

export async function uploadRequirementAttachment(
  projectID: number,
  requirementID: number,
  file: File,
): Promise<RequirementAttachment> {
  const form = new FormData();
  form.append("file", file);
  const { body } = await http.post<RequirementAttachment>(
    `/projects/${projectID}/requirements/${requirementID}/attachments`,
    form,
  );
  return body;
}

export async function deleteRequirementAttachment(
  projectID: number,
  requirementID: number,
  attachmentID: number,
): Promise<void> {
  await http.delete(
    `/projects/${projectID}/requirements/${requirementID}/attachments/${attachmentID}`,
  );
}

export async function downloadRequirementAttachment(
  projectID: number,
  requirementID: number,
  attachmentID: number,
  filename: string,
): Promise<void> {
  const { body } = await http.get<Blob>(
    `/projects/${projectID}/requirements/${requirementID}/attachments/${attachmentID}/download`,
    { responseType: "blob" },
  );
  saveBlob(body, filename);
}

export async function listDocTree(projectID: number): Promise<ApiDocNode[]> {
  const { body } = await http.get<{ items: ApiDocNode[] }>(`/projects/${projectID}/docs`);
  return body.items;
}

export async function getDocNode(projectID: number, nodeID: number): Promise<ApiDocNode> {
  const { body } = await http.get<ApiDocNode>(`/projects/${projectID}/docs/${nodeID}`);
  return body;
}

export async function createDocNode(
  projectID: number,
  input: Record<string, unknown>,
): Promise<ApiDocNode> {
  const { body } = await http.post<ApiDocNode>(`/projects/${projectID}/docs`, input);
  return body;
}

export async function updateDocNode(
  projectID: number,
  nodeID: number,
  input: Record<string, unknown>,
): Promise<ApiDocNode> {
  const { body } = await http.put<ApiDocNode>(`/projects/${projectID}/docs/${nodeID}`, input);
  return body;
}

export async function moveDocNode(
  projectID: number,
  nodeID: number,
  input: { parent_id?: number | null; sort_order?: number },
): Promise<ApiDocNode> {
  const { body } = await http.post<ApiDocNode>(`/projects/${projectID}/docs/${nodeID}/move`, input);
  return body;
}

export async function deleteDocNode(projectID: number, nodeID: number): Promise<void> {
  await http.delete(`/projects/${projectID}/docs/${nodeID}`);
}

async function uploadDocFile(
  endpoint: string,
  parentID: number | null,
  file: File,
): Promise<{ items?: ApiDocNode[] } | ApiDocNode> {
  const form = new FormData();
  form.append("file", file);
  if (parentID !== null) form.append("parent_id", String(parentID));
  const { body } = await http.post<{ items?: ApiDocNode[] } | ApiDocNode>(endpoint, form);
  return body;
}

export async function uploadMarkdown(
  projectID: number,
  parentID: number | null,
  file: File,
): Promise<ApiDocNode> {
  return (await uploadDocFile(`/projects/${projectID}/docs/upload`, parentID, file)) as ApiDocNode;
}

export async function importDocsZIP(
  projectID: number,
  parentID: number | null,
  file: File,
): Promise<ApiDocNode[]> {
  const body = await uploadDocFile(`/projects/${projectID}/docs/import-zip`, parentID, file);
  return "items" in body ? (body.items ?? []) : [];
}

export async function listDevDocTree(projectID: number): Promise<DevDocNode[]> {
  const { body } = await http.get<{ items: DevDocNode[] }>(`/projects/${projectID}/dev-docs`);
  return body.items;
}

export async function getDevDocNode(projectID: number, nodeID: number): Promise<DevDocNode> {
  const { body } = await http.get<DevDocNode>(`/projects/${projectID}/dev-docs/${nodeID}`);
  return body;
}

export async function createDevDocNode(
  projectID: number,
  input: Record<string, unknown>,
): Promise<DevDocNode> {
  const { body } = await http.post<DevDocNode>(`/projects/${projectID}/dev-docs`, input);
  return body;
}

export async function updateDevDocNode(
  projectID: number,
  nodeID: number,
  input: Record<string, unknown>,
): Promise<DevDocNode> {
  const { body } = await http.put<DevDocNode>(`/projects/${projectID}/dev-docs/${nodeID}`, input);
  return body;
}

export async function moveDevDocNode(
  projectID: number,
  nodeID: number,
  input: { parent_id?: number | null; sort_order?: number },
): Promise<DevDocNode> {
  const { body } = await http.post<DevDocNode>(
    `/projects/${projectID}/dev-docs/${nodeID}/move`,
    input,
  );
  return body;
}

export async function deleteDevDocNode(projectID: number, nodeID: number): Promise<void> {
  await http.delete(`/projects/${projectID}/dev-docs/${nodeID}`);
}

export async function uploadDevMarkdown(
  projectID: number,
  parentID: number | null,
  file: File,
): Promise<DevDocNode> {
  return (await uploadDocFile(
    `/projects/${projectID}/dev-docs/upload`,
    parentID,
    file,
  )) as DevDocNode;
}

export async function importDevDocsZIP(
  projectID: number,
  parentID: number | null,
  file: File,
): Promise<DevDocNode[]> {
  const body = await uploadDocFile(`/projects/${projectID}/dev-docs/import-zip`, parentID, file);
  return "items" in body ? ((body.items ?? []) as DevDocNode[]) : [];
}
