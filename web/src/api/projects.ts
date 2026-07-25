import { saveBlob } from "@cat-kit/fe";

import { http } from "./http";
import type {
  ApiDocDiff,
  ApiDocNode,
  ProductProject,
  ProjectMember,
  ProjectRole,
  Requirement,
  RequirementAttachment,
  RequirementComment,
  RequirementStatusOption,
  UserOption,
} from "./types";

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

export async function publishDocNode(
  projectID: number,
  nodeID: number,
  expectedVersion: number,
): Promise<ApiDocNode> {
  const { body } = await http.post<ApiDocNode>(`/projects/${projectID}/docs/${nodeID}/publish`, {
    expected_version: expectedVersion,
  });
  return body;
}

export async function getDocDiff(projectID: number, nodeID: number): Promise<ApiDocDiff> {
  const { body } = await http.get<ApiDocDiff>(`/projects/${projectID}/docs/${nodeID}/diff`);
  return body;
}
