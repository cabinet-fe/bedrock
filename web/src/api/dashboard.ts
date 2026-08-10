import { http } from "./http";
import type {
  AgentRunSummary,
  BuildSummary,
  DashboardLayout,
  MyProject,
  PipelineRunSummary,
  ScriptRunSummary,
  SystemInfo,
  SystemStatus,
  TaskOverview,
} from "./types";

export async function getDashboardLayout(): Promise<DashboardLayout> {
  const { body } = await http.get<DashboardLayout>("/dashboard/layout");
  return body;
}

export async function saveDashboardLayout(layout: DashboardLayout): Promise<DashboardLayout> {
  const { body } = await http.put<DashboardLayout>("/dashboard/layout", layout);
  return body;
}

export async function getBuildSummary(): Promise<BuildSummary> {
  const { body } = await http.get<BuildSummary>("/dashboard/build-summary");
  return body;
}

export async function getAgentRunSummary(): Promise<AgentRunSummary> {
  const { body } = await http.get<AgentRunSummary>("/dashboard/agent-run-summary");
  return body;
}

export async function getSystemInfo(): Promise<SystemInfo> {
  const { body } = await http.get<SystemInfo>("/dashboard/system-info");
  return body;
}

export async function getSystemStatus(): Promise<SystemStatus> {
  const { body } = await http.get<SystemStatus>("/dashboard/system-status");
  return body;
}

export async function getScriptRunSummary(): Promise<ScriptRunSummary> {
  const { body } = await http.get<ScriptRunSummary>("/dashboard/script-run-summary");
  return body;
}

export async function getPipelineRunSummary(): Promise<PipelineRunSummary> {
  const { body } = await http.get<PipelineRunSummary>("/dashboard/pipeline-run-summary");
  return body;
}

export async function getTaskOverview(): Promise<TaskOverview> {
  const { body } = await http.get<TaskOverview>("/dashboard/task-overview");
  return body;
}

export async function getMyProjects(): Promise<MyProject[]> {
  const { body } = await http.get<MyProject[]>("/dashboard/my-projects");
  return body;
}

export function dashboardWsUrl(token: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/ws/dashboard?token=${encodeURIComponent(token)}`;
}
