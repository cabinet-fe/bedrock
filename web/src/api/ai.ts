import { http } from "./http";
import type {
  AgentRun,
  AgentTrigger,
  AiAgent,
  PageResult,
  SkillFileContent,
  SkillFileNode,
  SkillPackage,
} from "./types";

type Query = Record<string, string | number | boolean | undefined>;

function compactQuery(query?: Query): Record<string, string | number | boolean> {
  return Object.fromEntries(
    Object.entries(query ?? {}).filter(([, value]) => value !== undefined && value !== ""),
  ) as Record<string, string | number | boolean>;
}

export async function listAgents(query?: Query): Promise<PageResult<AiAgent>> {
  const { body } = await http.get<PageResult<AiAgent>>("/ai/agents", {
    query: compactQuery(query),
  });
  return body;
}

export async function createAgent(input: Record<string, unknown>): Promise<AiAgent> {
  const { body } = await http.post<AiAgent>("/ai/agents", input);
  return body;
}

export async function getAgent(id: number): Promise<AiAgent> {
  const { body } = await http.get<AiAgent>(`/ai/agents/${id}`);
  return body;
}

export async function updateAgent(id: number, input: Record<string, unknown>): Promise<AiAgent> {
  const { body } = await http.put<AiAgent>(`/ai/agents/${id}`, input);
  return body;
}

export async function deleteAgent(id: number): Promise<void> {
  await http.delete(`/ai/agents/${id}`);
}

export async function listTriggers(agentID: number): Promise<AgentTrigger[]> {
  const { body } = await http.get<{ items: AgentTrigger[] }>(`/ai/agents/${agentID}/triggers`);
  return body.items;
}

export async function createTrigger(
  agentID: number,
  input: Record<string, unknown>,
): Promise<AgentTrigger> {
  const { body } = await http.post<AgentTrigger>(`/ai/agents/${agentID}/triggers`, input);
  return body;
}

export async function deleteTrigger(agentID: number, triggerID: number): Promise<void> {
  await http.delete(`/ai/agents/${agentID}/triggers/${triggerID}`);
}

export async function manualRunAgent(
  agentID: number,
  input?: { user_prompt?: string },
): Promise<AgentRun> {
  const { body } = await http.post<AgentRun>(`/ai/agents/${agentID}/runs`, {
    user_prompt: input?.user_prompt ?? "",
  });
  return body;
}

export async function listRuns(query?: Query): Promise<PageResult<AgentRun>> {
  const { body } = await http.get<PageResult<AgentRun>>("/ai/runs", {
    query: compactQuery(query),
  });
  return body;
}

export async function getRun(id: number): Promise<AgentRun> {
  const { body } = await http.get<AgentRun>(`/ai/runs/${id}`);
  return body;
}

export async function cancelRun(id: number): Promise<void> {
  await http.post(`/ai/runs/${id}/cancel`, {});
}

export function agentRunArtifactURL(id: number): string {
  return `/api/v1/ai/runs/${id}/artifact`;
}

/** Agent run log WebSocket URL (Bearer via query token). */
export function agentRunLogsWSURL(id: number, token: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/ws/ai/runs/${id}/logs?token=${encodeURIComponent(token)}`;
}

export async function listSkills(query?: Query): Promise<PageResult<SkillPackage>> {
  const { body } = await http.get<PageResult<SkillPackage>>("/skills", {
    query: compactQuery(query),
  });
  return body;
}

export async function getSkill(id: number): Promise<SkillPackage> {
  const { body } = await http.get<SkillPackage>(`/skills/${id}`);
  return body;
}

export async function uploadSkill(form: FormData): Promise<SkillPackage> {
  const { body } = await http.post<SkillPackage>("/skills", form);
  return body;
}

export async function overwriteSkill(id: number, form: FormData): Promise<SkillPackage> {
  const { body } = await http.put<SkillPackage>(`/skills/${id}`, form);
  return body;
}

export async function deleteSkill(id: number): Promise<void> {
  await http.delete(`/skills/${id}`);
}

export async function downloadSkill(id: number): Promise<Blob> {
  const { body } = await http.get<Blob>(`/skills/${id}/package`, { responseType: "blob" });
  return body;
}

export async function listSkillFiles(id: number): Promise<SkillFileNode[]> {
  const { body } = await http.get<SkillFileNode[]>(`/skills/${id}/files`);
  return body;
}

export async function readSkillFile(id: number, path: string): Promise<SkillFileContent> {
  const { body } = await http.get<SkillFileContent>(`/skills/${id}/files/content`, {
    query: { path },
  });
  return body;
}

export async function writeSkillFile(
  id: number,
  path: string,
  content: string,
): Promise<SkillFileContent> {
  const { body } = await http.put<SkillFileContent>(`/skills/${id}/files/content`, {
    path,
    content,
  });
  return body;
}

export async function createSkillEntry(
  id: number,
  input: { path: string; kind: "file" | "dir"; content?: string },
): Promise<SkillFileNode> {
  const { body } = await http.post<SkillFileNode>(`/skills/${id}/files`, input);
  return body;
}

export async function deleteSkillEntry(id: number, path: string): Promise<void> {
  await http.delete(`/skills/${id}/files`, { query: { path } });
}

export async function renameSkillEntry(
  id: number,
  fromPath: string,
  toPath: string,
): Promise<SkillFileNode> {
  const { body } = await http.post<SkillFileNode>(`/skills/${id}/files/rename`, {
    from_path: fromPath,
    to_path: toPath,
  });
  return body;
}
