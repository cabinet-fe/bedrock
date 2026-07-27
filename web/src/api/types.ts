export interface ApiEnvelope<T = unknown> {
  code: number;
  message: string;
  data?: T;
  request_id?: string;
}

export interface User {
  id: number;
  username: string;
  display_name: string;
  email: string;
  avatar: string;
  is_active: boolean;
  is_super_admin: boolean;
  role_ids?: number[];
}

/** Two-level nav from login /auth/me — aligns with UGroupNav GroupNavGroup. */
export interface MenuGroupNode {
  title: string;
  children: MenuItemNode[];
}

export interface MenuItemNode {
  title: string;
  path: string;
  icon?: string;
}

export interface LoginResponse {
  access_token: string;
  user: User;
  permissions?: string[];
  menus?: MenuGroupNode[];
}

export interface MeResponse {
  user: User;
  permissions: string[];
  menus: MenuGroupNode[];
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

interface RolePermission {
  id: number;
  role_id: number;
  permission: string;
}

type RoleType = "builtin" | "custom";

export interface Role {
  id: number;
  name: string;
  code: string;
  description: string;
  type?: RoleType;
  data_scope?: "self" | "all";
  permissions?: RolePermission[];
}

export interface MenuGroup {
  id: number;
  name: string;
  code: string;
  route_prefix: string;
  sort_key: number;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

type RbacResourceType = "menu" | "action" | "card";

export interface RbacResource {
  id: number;
  code: string;
  full_code: string;
  type: RbacResourceType;
  group_id?: number | null;
  parent_id?: number | null;
  super_admin_only: boolean;
  hidden: boolean;
  enabled: boolean;
  sort_key: number;
  title: string;
  route: string;
  icon_base64?: string;
  icon_mime?: string;
  children?: RbacResource[];
}

export interface PermissionCatalogFeature {
  id: number;
  code: string;
  full_code: string;
  type: string;
  title?: string;
  super_admin_only: boolean;
  enabled: boolean;
}

export interface PermissionCatalogMenu {
  id: number;
  code: string;
  full_code: string;
  title: string;
  super_admin_only: boolean;
  hidden: boolean;
  enabled: boolean;
  features: PermissionCatalogFeature[];
}

export interface PermissionCatalogGroup {
  id: number;
  name: string;
  code: string;
  menus: PermissionCatalogMenu[];
}

export interface Dictionary {
  id: number;
  name: string;
  code: string;
  description: string;
  items?: DictItem[];
}

export interface DictItem {
  id?: number;
  dictionary_id?: number;
  label: string;
  value: string;
  sort_order?: number;
  enabled?: boolean;
}

export interface OperationLog {
  id: number;
  user_id: number;
  username: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details: string;
  ip_address: string;
  created_at: string;
}

export interface NotificationItem {
  id: number;
  user_id: number;
  type: string;
  title: string;
  message: string;
  build_run_id?: number | null;
  agent_run_id?: number | null;
  is_read: boolean;
  created_at: string;
}

export interface Credential {
  id: number;
  name: string;
  type: string;
  username: string;
  description: string;
  has_secret: boolean;
  has_passphrase?: boolean;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface Repository {
  id: number;
  name: string;
  description: string;
  tags: string;
  repo_url: string;
  auth_type: string;
  credential_id?: number | null;
  branches?: string[];
  branches_synced_at?: string | null;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface Server {
  id: number;
  name: string;
  host: string;
  port: number;
  os_type: string;
  username: string;
  auth_type: string;
  credential_id?: number | null;
  agent_url?: string;
  agent_credential_id?: number | null;
  description: string;
  tags: string;
  status: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface DeployTarget {
  id?: number;
  build_job_id?: number;
  server_id?: number | null;
  remote_path: string;
  method: string;
  post_deploy_script?: string;
  sort_order: number;
}

export interface BuildJob {
  id: number;
  repository_id: number;
  name: string;
  description: string;
  enabled: boolean;
  branch: string;
  shallow_clone: boolean;
  build_script_type: string;
  build_script: string;
  work_dir: string;
  output_dir: string;
  cache_paths: string;
  env_var_names?: string[];
  trigger_manual: boolean;
  trigger_webhook: boolean;
  trigger_cron: boolean;
  webhook_type?: string;
  webhook_ref_path?: string;
  webhook_commit_path?: string;
  webhook_message_path?: string;
  cron_expression: string;
  cron_timezone: string;
  max_artifacts: number;
  artifact_format: string;
  agent_trigger_event: string;
  agent_ids?: number[];
  is_public: boolean;
  deploy_targets?: DeployTarget[];
  created_by: number;
  created_at: string;
  updated_at: string;
}

interface BuildDeployAttempt {
  id: number;
  build_run_id: number;
  batch_no: number;
  deploy_target_id?: number | null;
  status: string;
  error_message?: string;
  created_at: string;
}

export interface BuildRun {
  id: number;
  build_job_id: number;
  build_number: number;
  status: string;
  stage: string;
  trigger_type: string;
  triggered_by: number;
  branch: string;
  commit_hash: string;
  commit_message: string;
  log_path?: string;
  artifact_path?: string;
  distribution_summary: string;
  snapshot_json?: string;
  error_message?: string;
  created_at: string;
  deploy_attempts?: BuildDeployAttempt[];
}

export type DashboardCardID =
  | "build_summary"
  | "agent_run_summary"
  | "system_info"
  | "system_status";

export interface DashboardCardLayout {
  id: DashboardCardID;
  visible: boolean;
  /** Normalized as y * 12 + x by the server. */
  order: number;
  /** Grid column start (0-based, 12-column grid). */
  x: number;
  /** Grid row start (0-based). */
  y: number;
  /** Width in columns (2–12). */
  w: number;
  /** Height in rows (≥2). */
  h: number;
}

export interface DashboardLayout {
  cards: DashboardCardLayout[];
}

export interface DashboardRecentBuildRun {
  id: number;
  build_job_id: number;
  build_number: number;
  status: string;
  branch: string;
  created_at: string;
}

export interface BuildSummary {
  running: number;
  queued: number;
  success_rate: number;
  recent: DashboardRecentBuildRun[];
}

export interface DashboardRecentAgentRun {
  id: number;
  agent_id: number;
  agent_name: string;
  trigger_type: string;
  status: string;
  created_at: string;
}

export interface AgentRunSummary {
  running: number;
  queued: number;
  success_rate: number;
  recent: DashboardRecentAgentRun[];
}

export interface SystemInfo {
  version: string;
  os: string;
  arch: string;
  runtime: string;
  hostname: string;
  start_time: string;
}

export interface DirectoryUsage {
  path: string;
  used_bytes: number;
}

export interface SystemStatus {
  cpu_usage_percent: number;
  memory_used_bytes: number;
  memory_total_bytes: number;
  memory_usage_percent: number;
  disk_used_bytes: number;
  disk_total_bytes: number;
  disk_free_bytes: number;
  disk_usage_percent: number;
  health: string;
  directories: DirectoryUsage[];
  collected_at: string;
}

export interface ProcessInfo {
  pid: number;
  name: string;
  cpu_percent: number;
  memory_bytes: number;
  username: string;
  num_threads: number;
  status: string;
  start_time: number;
  cmdline: string;
  ports: number[];
}

export interface DevEnvironment {
  id: number;
  name: string;
  kind: "builtin" | "custom";
  executable: string;
  description: string;
  detect_script: string;
  install_script: string;
  upgrade_script: string;
  uninstall_script: string;
  versions_script: string;
  switch_script: string;
  default_version: string;
  sources?: DevEnvInstallSource[];
}

export interface DevEnvInstallSource {
  id: number;
  environment_id: number;
  name: string;
  base_url: string;
  priority: number;
  enabled: boolean;
}

export interface DevEnvJob {
  id: number;
  environment_id: number;
  operation: "install" | "upgrade" | "uninstall" | "switch";
  requested_version: string;
  status: string;
  source_id?: number | null;
  command_snapshot: string;
  error_message: string;
  created_at: string;
  environment?: DevEnvironment;
  source?: DevEnvInstallSource;
}

export type ProjectRole = "owner" | "admin" | "member" | "readonly";

interface ProjectCapabilities {
  update: boolean;
  archive: boolean;
  delete: boolean;
  manage_members: boolean;
  transfer_owner: boolean;
}

export interface ProductProject {
  id: number;
  name: string;
  slug: string;
  description: string;
  status: "active" | "archived";
  owner_id: number;
  repository_id?: number | null;
  tags: string;
  is_public: boolean;
  created_by: number;
  created_at: string;
  updated_at: string;
  my_role?: ProjectRole;
  permissions?: ProjectCapabilities;
}

export interface ProjectMember {
  id: number;
  project_id: number;
  user_id: number;
  role: ProjectRole;
  username?: string;
  display_name?: string;
  created_at: string;
  updated_at: string;
}

export interface UserOption {
  id: number;
  username: string;
  display_name: string;
}

export interface Requirement {
  id: number;
  project_id: number;
  title: string;
  description: string;
  status: string;
  priority: "low" | "normal" | "high" | "urgent";
  assignee_id?: number | null;
  repository_id?: number | null;
  tags: string;
  created_by: number;
  updated_by: number;
  created_at: string;
  updated_at: string;
}

export interface RequirementStatusOption {
  label: string;
  value: string;
  sort_order: number;
  enabled: boolean;
}

export interface RequirementComment {
  id: number;
  requirement_id: number;
  content: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface RequirementAttachment {
  id: number;
  requirement_id: number;
  storage_object_id: number;
  filename: string;
  created_by: number;
  created_at: string;
}

export interface ApiDocNode {
  id: number;
  project_id: number;
  parent_id?: number | null;
  kind: "dir" | "doc";
  name: string;
  sort_order: number;
  repository_id?: number | null;
  published_content: string;
  draft_content: string;
  content_version: number;
  draft_base_version: number;
  draft_updated_at?: string | null;
  draft_source_run_id?: number | null;
  children?: ApiDocNode[];
}

export interface ApiDocDiff {
  node_id: number;
  content_version: number;
  has_draft: boolean;
  published_lines: number;
  draft_lines: number;
  added_lines: number;
  removed_lines: number;
}

export interface CliRuntimeDefinition {
  id: number;
  key: string;
  name: string;
  binary_name: string;
  description: string;
  install_status: string;
  installed_path: string;
  installed_version: string;
  healthy: boolean;
  risk_notice?: string;
}

export interface CliInstallSource {
  id: number;
  cli_key: string;
  name: string;
  base_url: string;
  priority: number;
  enabled: boolean;
}

export interface CliExecuteResult {
  success: boolean;
  output: string;
  error?: string;
}

export interface CliCheckUpdateResult {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  package: string;
  registry?: string;
  output?: string;
  error?: string;
}

export interface AiAgentRepoBinding {
  repository_id: number;
  branch: string;
}

interface AiAgentEnvVar {
  key: string;
  has_value: boolean;
}

export interface AiAgentEnvVarInput {
  key: string;
  value?: string;
}

export interface AiAgent {
  id: number;
  name: string;
  description: string;
  enabled: boolean;
  cli_key: string;
  system_prompt: string;
  skill_ids: number[];
  repo_bindings: AiAgentRepoBinding[];
  env_vars: AiAgentEnvVar[];
  output_dir: string;
  stream_output: boolean;
  timeout_sec: number;
  workspace_status: "pending" | "ready" | "failed";
  workspace_error?: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface AgentTrigger {
  id: number;
  agent_id: number;
  type: string;
  enabled: boolean;
  cron_expression?: string;
  cron_timezone?: string;
  build_job_id?: number | null;
  build_event?: string;
}

export interface AgentRun {
  id: number;
  agent_id: number;
  trigger_type: string;
  status: string;
  work_dir?: string;
  build_run_id?: number | null;
  project_id?: number | null;
  doc_node_id?: number | null;
  error_message?: string;
  output_text?: string;
  duration_ms?: number;
  started_at?: string | null;
  finished_at?: string | null;
  created_at: string;
}

export interface SkillPackage {
  id: number;
  name: string;
  description: string;
  visibility: "public" | "private";
  package_digest: string;
  size_bytes: number;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface PersonalAccessToken {
  id: number;
  user_id: number;
  name: string;
  token_prefix: string;
  scopes: string[];
  expires_at?: string | null;
  revoked_at?: string | null;
  last_used_at?: string | null;
  created_at: string;
}
