import { http } from "./http";
import type {
  AgentRun,
  AgentTrigger,
  AiAgent,
  AiModel,
  AiModelInput,
  AiProvider,
  AiProviderInput,
  ChatSession,
  ChatSessionInput,
  ChatSessionMessage,
  ChatSessionMessageInput,
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

export async function listProviders(query?: Query): Promise<PageResult<AiProvider>> {
  const { body } = await http.get<PageResult<AiProvider>>("/ai/providers", {
    query: compactQuery(query),
  });
  return body;
}

export async function getProvider(id: number): Promise<AiProvider> {
  const { body } = await http.get<AiProvider>(`/ai/providers/${id}`);
  return body;
}

export async function createProvider(
  input: AiProviderInput | Record<string, unknown>,
): Promise<AiProvider> {
  const { body } = await http.post<AiProvider>("/ai/providers", input);
  return body;
}

export async function updateProvider(
  id: number,
  input: AiProviderInput | Record<string, unknown>,
): Promise<AiProvider> {
  const { body } = await http.put<AiProvider>(`/ai/providers/${id}`, input);
  return body;
}

export async function deleteProvider(id: number): Promise<void> {
  await http.delete(`/ai/providers/${id}`);
}

export async function listModels(providerID: number, query?: Query): Promise<PageResult<AiModel>> {
  const { body } = await http.get<PageResult<AiModel>>(`/ai/providers/${providerID}/models`, {
    query: compactQuery(query),
  });
  return body;
}

export async function getModel(providerID: number, modelID: number): Promise<AiModel> {
  const { body } = await http.get<AiModel>(`/ai/providers/${providerID}/models/${modelID}`);
  return body;
}

export async function createModel(
  providerID: number,
  input: AiModelInput | Record<string, unknown>,
): Promise<AiModel> {
  const { body } = await http.post<AiModel>(`/ai/providers/${providerID}/models`, input);
  return body;
}

export async function updateModel(
  providerID: number,
  modelID: number,
  input: AiModelInput | Record<string, unknown>,
): Promise<AiModel> {
  const { body } = await http.put<AiModel>(`/ai/providers/${providerID}/models/${modelID}`, input);
  return body;
}

export async function deleteModel(providerID: number, modelID: number): Promise<void> {
  await http.delete(`/ai/providers/${providerID}/models/${modelID}`);
}

export async function listChatSessions(query?: Query): Promise<PageResult<ChatSession>> {
  const { body } = await http.get<PageResult<ChatSession>>("/ai/chat/sessions", {
    query: compactQuery(query),
  });
  return body;
}

export async function createChatSession(input: ChatSessionInput): Promise<ChatSession> {
  const { body } = await http.post<ChatSession>("/ai/chat/sessions", input);
  return body;
}

export async function updateChatSession(id: number, input: ChatSessionInput): Promise<ChatSession> {
  const { body } = await http.put<ChatSession>(`/ai/chat/sessions/${id}`, input);
  return body;
}

export async function deleteChatSession(id: number): Promise<void> {
  await http.delete(`/ai/chat/sessions/${id}`);
}

export async function listChatMessages(sessionId: number): Promise<ChatSessionMessage[]> {
  const { body } = await http.get<ChatSessionMessage[]>(`/ai/chat/sessions/${sessionId}/messages`);
  return body;
}

export async function createChatMessage(
  sessionId: number,
  input: ChatSessionMessageInput,
): Promise<ChatSessionMessage> {
  const { body } = await http.post<ChatSessionMessage>(
    `/ai/chat/sessions/${sessionId}/messages`,
    input,
  );
  return body;
}

export async function listAvailableModels(): Promise<AiModel[]> {
  const { body } = await http.get<AiModel[]>("/ai/chat/models");
  return body;
}
