import { http } from "./http";
import type {
  DevEnvInstallSource,
  DevEnvJob,
  DevEnvironment,
  PageResult,
  VersionCatalog,
} from "./types";

type Query = Record<string, string | number | boolean | undefined>;

function compactQuery(query?: Query): Record<string, string | number | boolean> {
  return Object.fromEntries(
    Object.entries(query ?? {}).filter(([, value]) => value !== undefined && value !== ""),
  ) as Record<string, string | number | boolean>;
}

export async function killProcess(pid: number): Promise<void> {
  await http.post(`/ops/processes/${pid}/kill`, {});
}

export async function listDevEnvironments(): Promise<DevEnvironment[]> {
  const { body } = await http.get<{ items: DevEnvironment[] }>("/ops/dev-environments");
  return body.items;
}

export async function createDevEnvironment(
  input: Record<string, unknown>,
): Promise<DevEnvironment> {
  const { body } = await http.post<DevEnvironment>("/ops/dev-environments", input);
  return body;
}

export async function updateDevEnvironment(
  id: number,
  input: Record<string, unknown>,
): Promise<DevEnvironment> {
  const { body } = await http.put<DevEnvironment>(`/ops/dev-environments/${id}`, input);
  return body;
}

export async function deleteDevEnvironment(id: number): Promise<void> {
  await http.delete(`/ops/dev-environments/${id}`);
}

export async function detectDevEnvironment(
  id: number,
): Promise<{ detected: boolean; output: string }> {
  const { body } = await http.post<{ detected: boolean; output: string }>(
    `/ops/dev-environments/${id}/detect`,
    {},
  );
  return body;
}

export async function listDevEnvVersions(id: number): Promise<VersionCatalog> {
  const { body } = await http.get<VersionCatalog>(`/ops/dev-environments/${id}/versions`, {
    timeout: 60_000,
  });
  return body;
}

export async function enqueueDevEnvironmentOperation(
  id: number,
  operation: "install" | "upgrade" | "uninstall" | "switch",
  version = "",
): Promise<DevEnvJob> {
  const { body } = await http.post<DevEnvJob>(`/ops/dev-environments/${id}/${operation}`, {
    version,
  });
  return body;
}

export async function createDevEnvSource(
  envId: number,
  input: Record<string, unknown>,
): Promise<DevEnvInstallSource> {
  const { body } = await http.post<DevEnvInstallSource>(
    `/ops/dev-environments/${envId}/sources`,
    input,
  );
  return body;
}

export async function updateDevEnvSource(
  envId: number,
  sourceId: number,
  input: Record<string, unknown>,
): Promise<DevEnvInstallSource> {
  const { body } = await http.put<DevEnvInstallSource>(
    `/ops/dev-environments/${envId}/sources/${sourceId}`,
    input,
  );
  return body;
}

export async function deleteDevEnvSource(envId: number, sourceId: number): Promise<void> {
  await http.delete(`/ops/dev-environments/${envId}/sources/${sourceId}`);
}

export async function pingDevEnvSource(
  envId: number,
  sourceId: number,
): Promise<{ ok: boolean; detail: string }> {
  const { body } = await http.post<{ ok: boolean; detail: string }>(
    `/ops/dev-environments/${envId}/sources/${sourceId}/ping`,
    {},
  );
  return body;
}

export async function listDevEnvJobs(envId: number, query?: Query): Promise<PageResult<DevEnvJob>> {
  const { body } = await http.get<PageResult<DevEnvJob>>(`/ops/dev-environments/${envId}/jobs`, {
    query: compactQuery(query),
  });
  return body;
}

export async function getDevEnvJob(envId: number, jobId: number): Promise<DevEnvJob> {
  const { body } = await http.get<DevEnvJob>(`/ops/dev-environments/${envId}/jobs/${jobId}`);
  return body;
}

export async function getDevEnvJobLogs(envId: number, jobId: number): Promise<string> {
  const { body } = await http.get<string>(`/ops/dev-environments/${envId}/jobs/${jobId}/logs`);
  return body;
}

export async function retryDevEnvJob(envId: number, jobId: number): Promise<DevEnvJob> {
  const { body } = await http.post<DevEnvJob>(
    `/ops/dev-environments/${envId}/jobs/${jobId}/retry`,
    {},
  );
  return body;
}
