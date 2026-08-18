import { decryptAESGCM } from "@/lib/login-crypto";
import { http } from "./http";
import type {
  CliCheckUpdateResult,
  CliExecuteResult,
  CliInstallSource,
  CliRuntimeDefinition,
  Credential,
  PageResult,
  PersonalAccessToken,
  Repository,
  Server,
  VersionCatalog,
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

// —— Credentials ——
export async function listCredentials(params?: ListQuery): Promise<PageResult<Credential>> {
  const { body } = await http.get<PageResult<Credential>>("/resource/credentials", {
    query: toQuery(params),
  });
  return body;
}

export async function createCredential(body: Record<string, unknown>): Promise<Credential> {
  const { body: data } = await http.post<Credential>("/resource/credentials", body);
  return data;
}

export async function updateCredential(
  id: number,
  body: Record<string, unknown>,
): Promise<Credential> {
  const { body: data } = await http.put<Credential>(`/resource/credentials/${id}`, body);
  return data;
}

export async function deleteCredential(id: number): Promise<void> {
  await http.delete(`/resource/credentials/${id}`);
}

// —— Repositories ——
export async function listRepositories(params?: ListQuery): Promise<PageResult<Repository>> {
  const { body } = await http.get<PageResult<Repository>>("/resource/repositories", {
    query: toQuery(params),
  });
  return body;
}

export async function listRepositoryBranches(
  id: number,
): Promise<{ items: string[]; synced_at?: string | null }> {
  const { body } = await http.get<{ items: string[]; synced_at?: string | null }>(
    `/resource/repositories/${id}/branches`,
  );
  return { items: body.items ?? [], synced_at: body.synced_at };
}

export async function syncRepositoryBranches(
  id: number,
): Promise<{ items: string[]; synced_at?: string | null }> {
  const { body } = await http.post<{ items: string[]; synced_at?: string | null }>(
    `/resource/repositories/${id}/sync-branches`,
    {},
  );
  return { items: body.items ?? [], synced_at: body.synced_at };
}

export async function syncRepositoryBranchesBatch(
  ids: number[],
): Promise<{ id: number; ok: boolean; branch_count?: number; error?: string }[]> {
  const { body } = await http.post<{
    items: { id: number; ok: boolean; branch_count?: number; error?: string }[];
  }>("/resource/repositories/sync-branches", { ids });
  return body.items ?? [];
}

export async function createRepository(body: Record<string, unknown>): Promise<Repository> {
  const { body: data } = await http.post<Repository>("/resource/repositories", body);
  return data;
}

export async function updateRepository(
  id: number,
  body: Record<string, unknown>,
): Promise<Repository> {
  const { body: data } = await http.put<Repository>(`/resource/repositories/${id}`, body);
  return data;
}

export async function deleteRepository(id: number): Promise<void> {
  await http.delete(`/resource/repositories/${id}`);
}

export async function testRepository(id: number): Promise<{ ok: boolean; branches?: string[] }> {
  const { body } = await http.post<{ ok: boolean; branches?: string[] }>(
    `/resource/repositories/${id}/test`,
    {},
  );
  return body;
}

// —— Servers ——
export async function listServers(params?: ListQuery): Promise<PageResult<Server>> {
  const { body } = await http.get<PageResult<Server>>("/resource/servers", {
    query: toQuery(params),
  });
  return body;
}

export async function createServer(body: Record<string, unknown>): Promise<Server> {
  const { body: data } = await http.post<Server>("/resource/servers", body);
  return data;
}

export async function updateServer(id: number, body: Record<string, unknown>): Promise<Server> {
  const { body: data } = await http.put<Server>(`/resource/servers/${id}`, body);
  return data;
}

export async function deleteServer(id: number): Promise<void> {
  await http.delete(`/resource/servers/${id}`);
}

export async function testServer(id: number): Promise<{ ok: boolean; output?: string }> {
  const { body } = await http.post<{ ok: boolean; output?: string }>(
    `/resource/servers/${id}/test`,
    {},
  );
  return body;
}

// —— AI CLIs ——
export async function listCLIs(): Promise<{ items: CliRuntimeDefinition[]; risk_notice: string }> {
  const { body } = await http.get<{ items: CliRuntimeDefinition[]; risk_notice: string }>(
    "/resource/clis",
  );
  return body;
}

export async function detectCLI(key: string) {
  const { body } = await http.post<{
    detected: boolean;
    output: string;
    path: string;
    version: string;
    healthy: boolean;
    risk_notice: string;
  }>(`/resource/clis/${key}/detect`, {});
  return body;
}

export async function checkCLIUpdate(key: string): Promise<CliCheckUpdateResult> {
  const { body } = await http.post<CliCheckUpdateResult>(
    `/resource/clis/${key}/check-update`,
    {},
    { timeout: 60_000 },
  );
  return body;
}

export async function listCLIVersions(key: string): Promise<VersionCatalog> {
  const { body } = await http.get<VersionCatalog>(`/resource/clis/${key}/versions`, {
    timeout: 60_000,
  });
  return body;
}

export async function executeCLI(
  key: string,
  operation: "install" | "upgrade" | "uninstall",
  version = "",
): Promise<CliExecuteResult> {
  const { body } = await http.post<CliExecuteResult>(
    `/resource/clis/${key}/${operation}`,
    { version },
    { timeout: 300_000 },
  );
  return body;
}

export async function listCLISources(cliKey?: string): Promise<CliInstallSource[]> {
  const { body } = await http.get<{ items: CliInstallSource[] }>("/resource/cli-sources", {
    query: toQuery({ cli_key: cliKey }),
  });
  return body.items;
}

export async function createCLISource(input: {
  cli_key: string;
  name: string;
  base_url: string;
  priority?: number;
  enabled?: boolean;
}): Promise<CliInstallSource> {
  const { body } = await http.post<CliInstallSource>("/resource/cli-sources", input);
  return body;
}

export async function updateCLISource(
  id: number,
  input: {
    cli_key?: string;
    name?: string;
    base_url?: string;
    priority?: number;
    enabled?: boolean;
  },
): Promise<CliInstallSource> {
  const { body } = await http.put<CliInstallSource>(`/resource/cli-sources/${id}`, input);
  return body;
}

export async function deleteCLISource(id: number): Promise<void> {
  await http.delete(`/resource/cli-sources/${id}`);
}

// —— Personal access tokens ——
export async function createToken(input: {
  name: string;
  scopes: string[];
  expires_at?: string;
  expires_in_days?: number;
}): Promise<{ token: string; metadata: PersonalAccessToken }> {
  const { body } = await http.post<{ token: string; metadata: PersonalAccessToken }>(
    "/resource/tokens",
    input,
  );
  return body;
}

export async function revealToken(id: number): Promise<string> {
  const { body } = await http.get<{ token_cipher: string }>(`/resource/tokens/${id}/reveal`);
  return decryptAESGCM(body.token_cipher);
}

export async function deleteToken(id: number): Promise<void> {
  await http.delete(`/resource/tokens/${id}`);
}
