import { getAccessToken, http } from "./http";
import type {
  BuildJob,
  BuildPipeline,
  BuildRun,
  PageResult,
  PipelineRun,
  ScriptJob,
  ScriptRun,
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

// —— Build jobs ——
export async function listBuildJobs(params?: ListQuery): Promise<PageResult<BuildJob>> {
  const { body } = await http.get<PageResult<BuildJob>>("/build-jobs", { query: toQuery(params) });
  return body;
}

export async function getBuildJob(id: number): Promise<BuildJob> {
  const { body } = await http.get<BuildJob>(`/build-jobs/${id}`);
  return body;
}

export async function createBuildJob(body: Record<string, unknown>): Promise<BuildJob> {
  const { body: data } = await http.post<BuildJob>("/build-jobs", body);
  return data;
}

export async function updateBuildJob(id: number, body: Record<string, unknown>): Promise<BuildJob> {
  const { body: data } = await http.put<BuildJob>(`/build-jobs/${id}`, body);
  return data;
}

export async function deleteBuildJob(id: number): Promise<void> {
  await http.delete(`/build-jobs/${id}`);
}

export async function getBuildJobWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.get<{ webhook_secret: string; webhook_url: string }>(
    `/build-jobs/${id}/webhook-secret`,
  );
  return body;
}

export async function rotateBuildJobWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.post<{ webhook_secret: string; webhook_url: string }>(
    `/build-jobs/${id}/webhook-secret/rotate`,
    {},
  );
  return body;
}

export async function enqueueBuildRun(
  jobId: number,
  body?: Record<string, unknown>,
): Promise<BuildRun> {
  const { body: data } = await http.post<BuildRun>(`/build-jobs/${jobId}/runs`, body ?? {});
  return data;
}

// —— Build runs ——
export async function getBuildRun(id: number): Promise<BuildRun> {
  const { body } = await http.get<BuildRun>(`/build-runs/${id}`);
  return body;
}

export async function cancelBuildRun(id: number): Promise<BuildRun> {
  const { body } = await http.post<BuildRun>(`/build-runs/${id}/cancel`, {});
  return body;
}

export async function retryBuildRun(id: number): Promise<BuildRun> {
  const { body } = await http.post<BuildRun>(`/build-runs/${id}/retry`, {});
  return body;
}

export async function redeployBuildRun(
  id: number,
  body?: { target_ids?: number[] },
): Promise<BuildRun> {
  const { body: data } = await http.post<BuildRun>(`/build-runs/${id}/redeploy`, body ?? {});
  return data;
}

/** Artifact download URL (Bearer via browser navigation with token query is not used; open with fetch blob). */
export function buildRunArtifactURL(id: number): string {
  return `/api/v1/build-runs/${id}/artifact`;
}

export async function getBuildRunLog(id: number): Promise<string> {
  const token = getAccessToken();
  const res = await fetch(`/api/v1/build-runs/${id}/log`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  return res.text();
}

export function buildRunLogsWSURL(id: number, token: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/ws/build-runs/${id}/logs?token=${encodeURIComponent(token)}`;
}

// —— Script jobs ——
export async function listScriptJobs(params?: ListQuery): Promise<PageResult<ScriptJob>> {
  const { body } = await http.get<PageResult<ScriptJob>>("/script-jobs", {
    query: toQuery(params),
  });
  return body;
}

export async function getScriptJob(id: number): Promise<ScriptJob> {
  const { body } = await http.get<ScriptJob>(`/script-jobs/${id}`);
  return body;
}

export async function createScriptJob(body: Record<string, unknown>): Promise<ScriptJob> {
  const { body: data } = await http.post<ScriptJob>("/script-jobs", body);
  return data;
}

export async function updateScriptJob(
  id: number,
  body: Record<string, unknown>,
): Promise<ScriptJob> {
  const { body: data } = await http.put<ScriptJob>(`/script-jobs/${id}`, body);
  return data;
}

export async function deleteScriptJob(id: number): Promise<void> {
  await http.delete(`/script-jobs/${id}`);
}

export async function getScriptJobWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.get<{ webhook_secret: string; webhook_url: string }>(
    `/script-jobs/${id}/webhook-secret`,
  );
  return body;
}

export async function rotateScriptJobWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.post<{ webhook_secret: string; webhook_url: string }>(
    `/script-jobs/${id}/webhook-secret/rotate`,
    {},
  );
  return body;
}

export async function enqueueScriptRun(jobId: number): Promise<ScriptRun> {
  const { body: data } = await http.post<ScriptRun>(`/script-jobs/${jobId}/runs`, {});
  return data;
}

// —— Script runs ——
export async function getScriptRun(id: number): Promise<ScriptRun> {
  const { body } = await http.get<ScriptRun>(`/script-runs/${id}`);
  return body;
}

export async function cancelScriptRun(id: number): Promise<ScriptRun> {
  const { body } = await http.post<ScriptRun>(`/script-runs/${id}/cancel`, {});
  return body;
}

export async function retryScriptRun(id: number): Promise<ScriptRun> {
  const { body } = await http.post<ScriptRun>(`/script-runs/${id}/retry`, {});
  return body;
}

export function scriptRunLogsWSURL(id: number, token: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/ws/script-runs/${id}/logs?token=${encodeURIComponent(token)}`;
}

// —— Build pipelines ——
export async function listBuildPipelines(params?: ListQuery): Promise<PageResult<BuildPipeline>> {
  const { body } = await http.get<PageResult<BuildPipeline>>("/build-pipelines", {
    query: toQuery(params),
  });
  return body;
}

export async function getBuildPipeline(id: number): Promise<BuildPipeline> {
  const { body } = await http.get<BuildPipeline>(`/build-pipelines/${id}`);
  return body;
}

export async function createBuildPipeline(body: Record<string, unknown>): Promise<BuildPipeline> {
  const { body: data } = await http.post<BuildPipeline>("/build-pipelines", body);
  return data;
}

export async function updateBuildPipeline(
  id: number,
  body: Record<string, unknown>,
): Promise<BuildPipeline> {
  const { body: data } = await http.put<BuildPipeline>(`/build-pipelines/${id}`, body);
  return data;
}

export async function deleteBuildPipeline(id: number): Promise<void> {
  await http.delete(`/build-pipelines/${id}`);
}

export async function getBuildPipelineWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.get<{ webhook_secret: string; webhook_url: string }>(
    `/build-pipelines/${id}/webhook-secret`,
  );
  return body;
}

export async function rotateBuildPipelineWebhookSecret(
  id: number,
): Promise<{ webhook_secret: string; webhook_url: string }> {
  const { body } = await http.post<{ webhook_secret: string; webhook_url: string }>(
    `/build-pipelines/${id}/webhook-secret/rotate`,
    {},
  );
  return body;
}

export async function enqueuePipelineRun(
  pipelineId: number,
  body?: Record<string, unknown>,
): Promise<PipelineRun> {
  const { body: data } = await http.post<PipelineRun>(
    `/build-pipelines/${pipelineId}/runs`,
    body ?? {},
  );
  return data;
}

// —— Pipeline runs ——
export async function getPipelineRun(id: number): Promise<PipelineRun> {
  const { body } = await http.get<PipelineRun>(`/pipeline-runs/${id}`);
  return body;
}

export async function cancelPipelineRun(id: number): Promise<PipelineRun> {
  const { body } = await http.post<PipelineRun>(`/pipeline-runs/${id}/cancel`, {});
  return body;
}
