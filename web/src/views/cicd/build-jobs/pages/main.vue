<script setup lang="ts">
defineOptions({ name: "CicdBuildJobs" });

import { computed, nextTick, onMounted, reactive, ref, useTemplateRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { o } from "@cat-kit/core";
import { clipboard } from "@cat-kit/fe";
import { message } from "@veltra/desktop";
import type { CodeEditorLang } from "@veltra/desktop";

import { listAgents } from "@/api/ai";
import {
  createBuildJob,
  deleteBuildJob,
  enqueueBuildRun,
  getBuildJob,
  getBuildJobWebhookSecret,
  rotateBuildJobWebhookSecret,
  updateBuildJob,
} from "@/api/cicd";
import { listRepositories, listRepositoryBranches, listServers } from "@/api/resource";
import { getDictionaryByCode } from "@/api/system";
import type {
  AiAgent,
  AiAgentEnvVarInput,
  BuildJob,
  BuildRun,
  DeployTarget,
  Repository,
  Server,
} from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import ProjectSelect from "@/components/project-select";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import {
  BUILD_STAGE_TAG,
  JOB_STATUS_TAG,
  TRIGGER_TYPE_TAG,
  tagType,
  type TagType,
} from "@/lib/tag";

function parsePositiveInt(raw: unknown): number | undefined {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

function queryFlag(raw: unknown): boolean {
  const value = Array.isArray(raw) ? raw[0] : raw;
  return value === "1" || value === "true";
}

const METHOD_OPTIONS = [
  { label: "rsync", value: "rsync" },
  { label: "sftp", value: "sftp" },
  { label: "scp", value: "scp" },
  { label: "agent", value: "agent" },
  { label: "local", value: "local" },
];

const ARTIFACT_OPTIONS = [
  { label: "zip", value: "zip" },
  { label: "gzip", value: "gzip" },
];

/** 常用触发时区；filterable + creatable，仍可输入任意 IANA 时区 */
const TIMEZONE_OPTIONS = [
  { label: "Asia/Shanghai（北京）", value: "Asia/Shanghai" },
  { label: "Asia/Tokyo（东京）", value: "Asia/Tokyo" },
  { label: "Asia/Singapore（新加坡）", value: "Asia/Singapore" },
  { label: "Europe/London（伦敦）", value: "Europe/London" },
  { label: "Europe/Berlin（柏林）", value: "Europe/Berlin" },
  { label: "America/New_York（纽约）", value: "America/New_York" },
  { label: "America/Los_Angeles（洛杉矶）", value: "America/Los_Angeles" },
  { label: "UTC", value: "UTC" },
];

const WEBHOOK_TYPE_OPTIONS = [
  { label: "自动识别", value: "auto" },
  { label: "GitHub", value: "github" },
  { label: "自定义（generic）", value: "generic" },
];

const AGENT_EVENT_OPTIONS = [
  { label: "制品就绪（默认）", value: "artifact_ready" },
  { label: "分发完成", value: "distribution_finished" },
  { label: "不触发", value: "none" },
];

const AGENT_EVENT_TIPS =
  "制品就绪：构建产物打包成功后执行 Agent；分发完成：全部部署目标执行完后执行 Agent；不触发：不自动执行 Agent";

const JAVA_BUILD_TIPS =
  "Java 示例：SDKMAN 装 JDK/Maven；构建脚本 `mvn -B package`；artifact_paths 指向 JAR 或制品目录；post_build 可整理后再归档；部署目标可选 post_deploy";

type EnvNameDraft = { name: string };
type EnvVarDraft = { key: string; value: string; has_value?: boolean };
type CachePathDraft = { path: string };
type ArtifactPathDraft = { path: string };

/** API `cache_paths` 为 JSON 数组字符串；兼容换行分隔 */
function parseCachePaths(raw: string | undefined): string[] {
  if (!raw) return [];
  const trimmed = raw.trim();
  if (!trimmed) return [];
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (Array.isArray(parsed)) return parsed.map((s) => String(s).trim()).filter(Boolean);
  } catch {
    /* newline-separated fallback */
  }
  return trimmed
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
}

const BUILD_SCRIPT_TYPE_OPTIONS = [
  { label: "Bash / sh", value: "bash" },
  { label: "Node.js", value: "node" },
  { label: "Python", value: "python" },
  { label: "PowerShell 7+ (pwsh)", value: "pwsh" },
  { label: "Windows PowerShell 5.x", value: "powershell" },
  { label: "CMD", value: "cmd" },
];

/** 脚本类型 → 编辑器高亮语言；无对应语言则不指定 */
const SCRIPT_TYPE_LANG: Record<string, CodeEditorLang | undefined> = {
  bash: "bash",
  node: "js",
  pwsh: "powershell",
  powershell: "powershell",
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: rotateBusy, run: runRotate } = useBusy();
const route = useRoute();
const router = useRouter();
const listRef = useTemplateRef("list");
const historyRef = useTemplateRef("history");
const query = reactive({
  keyword: "",
  repository_id: undefined as number | undefined,
  tag: undefined as string | undefined,
  project_id: parsePositiveInt(route.query.project_id),
});
const dialogOpen = ref(false);
const secretOpen = ref(false);
const historyOpen = ref(false);
const historyJob = ref<BuildJob | null>(null);
const historyQuery = reactive({
  build_job_id: undefined as number | undefined,
  project_id: undefined as number | undefined,
});
const editing = ref<BuildJob | null>(null);
const webhookInfo = reactive({ secret: "", url: "" });
const repoOptions = ref<{ label: string; value: number }[]>([]);
const serverOptions = ref<{ label: string; value: number }[]>([]);
const agentOptions = ref<{ label: string; value: number }[]>([]);
const branchOptions = ref<{ label: string; value: string }[]>([]);
const repoTypeOptions = ref<{ label: string; value: string }[]>([]);
const branchesLoading = ref(false);
const form = reactive({
  repository_id: undefined as number | undefined,
  name: "",
  description: "",
  tags: [] as string[],
  enabled: true,
  branch: "main",
  shallow_clone: true,
  build_script_type: "bash",
  build_script: "",
  post_build_script: "",
  work_dir: "",
  artifact_paths: [] as ArtifactPathDraft[],
  cache_paths: [] as CachePathDraft[],
  env_var_names: [] as EnvNameDraft[],
  env_vars: [] as EnvVarDraft[],
  trigger_manual: true,
  trigger_webhook: false,
  trigger_cron: false,
  webhook_type: "auto",
  webhook_ref_path: "",
  webhook_commit_path: "",
  webhook_message_path: "",
  cron_expression: "",
  cron_timezone: "Asia/Shanghai",
  max_artifacts: 5,
  artifact_format: "zip",
  agent_trigger_event: "artifact_ready",
  agent_ids: [] as number[],
  is_public: false,
  project_id: undefined as number | undefined,
  deploy_targets: [] as DeployTarget[],
});

const branchPlaceholder = computed(() => (branchesLoading.value ? "加载分支…" : "选择或输入分支"));

/** 编辑器高亮语言跟随脚本类型；python / cmd 无对应语言则不指定 */
const editorLangs = computed(() => {
  const lang = SCRIPT_TYPE_LANG[form.build_script_type];
  return lang ? [lang] : [];
});
const ps5Tip = computed(() =>
  form.build_script_type === "powershell"
    ? "Windows PowerShell 5.x 不支持 &&，请改用多行、pwsh 或 cmd"
    : undefined,
);

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "build", title: "构建配置" },
  { key: "trigger", title: "触发方式" },
  { key: "artifact", title: "制品与 Agent" },
  { key: "deploy", title: "部署目标" },
];

const repoNameMap = computed(() => {
  const map = new Map<number, string>();
  for (const opt of repoOptions.value) {
    map.set(opt.value, opt.label);
  }
  return map;
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "repository", name: "仓库" },
  { key: "tags", name: "类型", width: 160 },
  { key: "branch", name: "分支" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "triggers", name: "触发" },
  { key: "action", name: "操作", width: 400, align: "center", fixed: "right" },
]);

const repoTypeLabelMap = computed(() => {
  const map = new Map<string, string>();
  for (const opt of repoTypeOptions.value) {
    map.set(opt.value, opt.label);
  }
  return map;
});

function splitTags(raw?: string | null): string[] {
  if (!raw) return [];
  return raw
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function tagLabel(value: string): string {
  return repoTypeLabelMap.value.get(value) ?? value;
}

const historyColumns = defineProTableColumns([
  { key: "build_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "stage", name: "阶段", width: 100, align: "center" },
  { key: "branch", name: "分支" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
  {
    key: "duration_ms",
    name: "运行时间",
    width: 110,
    align: "center",
    render: ({ val }) => formatDurationMs(val as number) || "—",
  },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 140, align: "center", fixed: "right" },
]);

async function loadBranches(repositoryId?: number) {
  if (!repositoryId) {
    branchOptions.value = [];
    return;
  }
  branchesLoading.value = true;
  try {
    const { items } = await listRepositoryBranches(repositoryId);
    branchOptions.value = items.map((b) => ({ label: b, value: b }));
  } catch {
    branchOptions.value = [];
  } finally {
    branchesLoading.value = false;
  }
}

watch(
  () => form.repository_id,
  (id) => {
    void loadBranches(id);
  },
);

watch(dialogOpen, (open) => {
  if (open && form.repository_id) {
    void loadBranches(form.repository_id);
  } else if (!open) {
    branchOptions.value = [];
  }
});

onMounted(async () => {
  try {
    const [repos, servers] = await Promise.all([
      listRepositories({ page: 1, page_size: 100 }),
      listServers({ page: 1, page_size: 100 }),
    ]);
    repoOptions.value = (repos.items ?? []).map((r: Repository) => ({
      label: r.name,
      value: r.id,
    }));
    serverOptions.value = (servers.items ?? []).map((s: Server) => ({
      label: `${s.name} (${s.host})`,
      value: s.id,
    }));
  } catch {
    /* ignore */
  }
  try {
    const dict = await getDictionaryByCode("repo_type");
    repoTypeOptions.value = (dict.items ?? [])
      .filter((it) => it.enabled !== false)
      .map((it) => ({ label: it.label, value: it.value }));
  } catch {
    /* ignore */
  }
  // 无 AI 模块权限时静默失败，Agent 选项留空
  try {
    const agents = await listAgents({ page: 1, page_size: 100 });
    agentOptions.value = (agents.items ?? []).map((a: AiAgent) => ({
      label: a.name,
      value: a.id,
    }));
  } catch {
    /* ignore */
  }

  const editID = parsePositiveInt(route.query.id);
  const prefillID = parsePositiveInt(route.query.project_id);
  if (editID != null && hasPermission("cicd_build_jobs:update")) {
    try {
      openEdit(await getBuildJob(editID));
    } catch (err) {
      message.error(err instanceof Error ? err.message : "加载任务失败");
    }
  } else if (queryFlag(route.query.create) && hasPermission("cicd_build_jobs:create")) {
    openCreate(prefillID);
  }
});

function repoName(repositoryId: number): string {
  return repoNameMap.value.get(repositoryId) ?? `#${repositoryId}`;
}

function openHistory(row: BuildJob) {
  historyJob.value = row;
  historyQuery.build_job_id = row.id;
  historyQuery.project_id = row.project_id ?? undefined;
  historyOpen.value = true;
}

watch(historyOpen, async (open) => {
  if (!open || !historyJob.value) return;
  historyQuery.build_job_id = historyJob.value.id;
  historyQuery.project_id = historyJob.value.project_id ?? undefined;
  await nextTick();
  void historyRef.value?.reload();
});

function openRunDetail(row: BuildRun) {
  historyOpen.value = false;
  void router.push({ name: "cicd-build-run-detail", params: { id: String(row.id) } });
}

function triggerParts(job: BuildJob): { label: string; type: TagType }[] {
  const parts: { label: string; type: TagType }[] = [];
  if (job.trigger_manual) parts.push({ label: "手动", type: undefined });
  if (job.trigger_webhook) parts.push({ label: "Webhook", type: "info" });
  if (job.trigger_cron) parts.push({ label: "Cron", type: "primary" });
  return parts;
}

function canBuild(job: BuildJob) {
  return job.enabled && job.trigger_manual;
}

function buildDisabledTip(job: BuildJob) {
  if (!job.enabled) return "任务已停用";
  if (!job.trigger_manual) return "未启用手动触发";
  return "";
}

function openCreate(projectID?: number) {
  editing.value = null;
  dialogOpen.value = true;
  form.project_id = typeof projectID === "number" ? projectID : undefined;
}

function parseArtifactPaths(job: BuildJob): string[] {
  if (job.artifact_paths?.length) {
    return job.artifact_paths.map((p) => String(p).trim()).filter(Boolean);
  }
  const legacy = job.output_dir?.trim();
  return legacy ? [legacy] : [];
}

function openEdit(row: BuildJob) {
  editing.value = row;
  o(form).extend(
    o(row).omit([
      "cache_paths",
      "artifact_paths",
      "env_var_names",
      "env_vars",
      "deploy_targets",
      "agent_ids",
      "post_build_script",
      "workspace_path",
      "tags",
    ]),
  );
  form.tags = splitTags(row.tags);
  form.env_var_names = (row.env_var_names ?? []).map((name) => ({ name }));
  form.env_vars = (row.env_vars ?? []).map((e) => ({
    key: e.key,
    value: "",
    has_value: e.has_value,
  }));
  form.cache_paths = parseCachePaths(row.cache_paths).map((path) => ({ path }));
  form.artifact_paths = parseArtifactPaths(row).map((path) => ({ path }));
  form.post_build_script = row.post_build_script ?? "";
  form.agent_ids = row.agent_ids ?? [];
  form.deploy_targets = (row.deploy_targets ?? []).map((t) => ({
    ...t,
    mirror: !!t.mirror,
  }));
  dialogOpen.value = true;
}

async function copyWorkspacePath() {
  const path = editing.value?.workspace_path;
  if (!path) return;
  try {
    await clipboard.copy(path);
    message.success("已复制工作区路径");
  } catch {
    message.error("复制失败");
  }
}

function addTarget() {
  form.deploy_targets.push({
    server_id: undefined,
    remote_path: "",
    method: "rsync",
    post_deploy_script: "",
    mirror: false,
    sort_order: form.deploy_targets.length,
  });
}

function removeTarget(idx: number) {
  form.deploy_targets.splice(idx, 1);
}

function buildBody(): Record<string, unknown> | undefined {
  const envVars: AiAgentEnvVarInput[] = [];
  const seenKeys = new Set<string>();
  for (const e of form.env_vars) {
    const key = e.key.trim();
    if (!key) {
      message.error("环境变量 key 不能为空");
      return;
    }
    if (key.includes("=") || key.includes("\n")) {
      message.error("环境变量 key 不能包含 = 或换行");
      return;
    }
    if (seenKeys.has(key)) {
      message.error(`环境变量 key 重复: ${key}`);
      return;
    }
    seenKeys.add(key);
    const row: AiAgentEnvVarInput = { key };
    if (e.value !== "") {
      row.value = e.value;
    } else if (!e.has_value) {
      message.error(`新建环境变量 ${key} 必须填写值`);
      return;
    }
    envVars.push(row);
  }
  return {
    ...o(form).omit([
      "env_var_names",
      "env_vars",
      "cache_paths",
      "artifact_paths",
      "deploy_targets",
      "tags",
    ]),
    tags: form.tags.join(","),
    env_var_names: form.env_var_names.map((e) => e.name.trim()).filter(Boolean),
    env_vars: envVars,
    artifact_paths: form.artifact_paths.map((a) => a.path.trim()).filter(Boolean),
    cache_paths: JSON.stringify(form.cache_paths.map((c) => c.path.trim()).filter(Boolean)),
    // 后端 Update 用 *uint：0 表示解除关联（省略字段则不改）
    project_id: form.project_id ?? 0,
    deploy_targets: form.deploy_targets.map((t, i) => ({
      server_id: t.method === "local" ? null : t.server_id,
      remote_path: t.remote_path,
      method: t.method,
      post_deploy_script: t.post_deploy_script || "",
      mirror: t.method === "rsync" ? !!t.mirror : false,
      sort_order: t.sort_order ?? i,
    })),
  };
}

async function save() {
  try {
    const body = buildBody();
    if (!body) return;
    if (editing.value) {
      await updateBuildJob(editing.value.id, body);
      message.success("已更新");
    } else {
      await createBuildJob(body);
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: BuildJob) => {
  try {
    await deleteBuildJob(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});

const trigger = bind(async (row: BuildJob) => {
  try {
    const run = await enqueueBuildRun(row.id, { trigger_type: "manual" });
    message.success(`已入队 #${run.build_number}`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "构建失败");
  }
});

const showWebhook = bind(async (row: BuildJob) => {
  try {
    const res = await getBuildJobWebhookSecret(row.id);
    webhookInfo.secret = res.webhook_secret;
    webhookInfo.url = res.webhook_url;
    editing.value = row;
    secretOpen.value = true;
  } catch (err) {
    message.error(err instanceof Error ? err.message : "获取 Webhook 失败");
  }
});

async function rotateWebhookSecret() {
  if (!editing.value) return;
  await runRotate(async () => {
    try {
      const res = await rotateBuildJobWebhookSecret(editing.value!.id);
      webhookInfo.secret = res.webhook_secret;
      webhookInfo.url = res.webhook_url;
      message.success("已轮换");
    } catch (err) {
      message.error(err instanceof Error ? err.message : "轮换失败");
    }
  });
}
</script>

<template>
  <div>
    <ProTable
      ref="list"
      url="/build-jobs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['repository_id', 'tag', 'project_id']"
      pagination
    >
      <template #filters>
        <ProjectSelect v-model="query.project_id" placeholder="全部项目" style="width: 180px" />
        <u-select
          v-model="query.repository_id"
          :options="repoOptions"
          placeholder="全部仓库"
          clearable
          style="width: 180px"
        />
        <u-select
          v-model="query.tag"
          :options="repoTypeOptions"
          placeholder="全部类型"
          clearable
          style="width: 140px"
        />
        <u-input v-model="query.keyword" placeholder="名称" style="width: 160px" />
      </template>
      <template #toolbar>
        <u-button
          v-if="hasPermission('cicd_build_jobs:create')"
          type="primary"
          @click.prevent="openCreate()"
        >
          新建任务
        </u-button>
      </template>
      <template #column:repository="{ rowData }">
        {{ repoName((rowData as BuildJob).repository_id) }}
      </template>
      <template #column:tags="{ rowData }">
        <span class="tag-cell">
          <template v-for="parts in [splitTags((rowData as BuildJob).tags)]" :key="0">
            <u-tag v-for="tag in parts" :key="tag" size="small" type="info">
              {{ tagLabel(tag) }}
            </u-tag>
            <template v-if="!parts.length">—</template>
          </template>
        </span>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as BuildJob).enabled ? 'success' : undefined">
          {{ (rowData as BuildJob).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:triggers="{ rowData }">
        <span class="tag-cell">
          <template v-for="parts in [triggerParts(rowData as BuildJob)]" :key="0">
            <u-tag v-for="part in parts" :key="part.label" size="small" :type="part.type">
              {{ part.label }}
            </u-tag>
            <template v-if="!parts.length">—</template>
          </template>
        </span>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="4" :loading="busyKey === (rowData as BuildJob).id">
          <u-action
            v-if="hasPermission('cicd_build_jobs:update')"
            @run="openEdit(rowData as BuildJob)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:execute')"
            :disabled="!canBuild(rowData as BuildJob)"
            :title="buildDisabledTip(rowData as BuildJob)"
            @run="trigger(rowData as BuildJob)"
          >
            构建
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:view')"
            @run="openHistory(rowData as BuildJob)"
          >
            构建历史
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:view') && (rowData as BuildJob).trigger_webhook"
            @run="showWebhook(rowData as BuildJob)"
          >
            Webhook
          </u-action>
          <u-action
            v-if="hasPermission('cicd_build_jobs:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as BuildJob)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑任务' : '新建任务'"
      :model="form"
      :groups="formGroups"
      label-width="110px"
      style="width: 1180px"
      @submit="save"
    >
      <template #group:basic>
        <u-select
          label="仓库"
          field="repository_id"
          :options="repoOptions"
          :disabled="!!editing"
          :rules="{ required: '必填' }"
        />
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="描述" field="description" />
        <ProjectSelect label="所属项目" field="project_id" />
        <u-multi-select
          label="标签"
          field="tags"
          :options="repoTypeOptions"
          placeholder="选择类型标签"
          filterable
          tips="选项来自数据字典 repo_type"
        />
        <u-switch label="启用" field="enabled" />
        <u-switch
          label="公开"
          field="is_public"
          tips="开启后，仅自己数据权限的用户也可查看该任务与执行记录；不授予改/删/触发"
        />
      </template>

      <template #group:build>
        <u-select
          label="分支"
          field="branch"
          :options="branchOptions"
          filterable
          creatable
          :disabled="!form.repository_id"
          :placeholder="branchPlaceholder"
        />
        <u-switch
          label="浅克隆"
          field="shallow_clone"
          tips="只拉取最近提交，加快克隆；需要完整历史时关闭"
        />
        <u-select label="脚本类型" field="build_script_type" :options="BUILD_SCRIPT_TYPE_OPTIONS" />
        <u-code-editor
          label="构建脚本"
          field="build_script"
          :langs="editorLangs"
          :default-lines="12"
          :tips="ps5Tip ?? JAVA_BUILD_TIPS"
          span="full"
        />
        <u-code-editor
          label="构建后脚本"
          field="post_build_script"
          :langs="editorLangs"
          :default-lines="6"
          tips="构建成功后、归档前执行"
          span="full"
        />
        <u-input
          label="工作目录"
          field="work_dir"
          placeholder="相对仓库根，可留空"
          tips="须存在；可配合 SDKMAN 等开发环境"
        />
        <u-form-item v-if="editing?.workspace_path" label="工作区路径" span="full">
          <div class="workspace-path-row">
            <code class="workspace-path">{{ editing.workspace_path }}</code>
            <u-button size="small" @click="copyWorkspacePath">复制</u-button>
          </div>
        </u-form-item>
        <u-group-input
          field="artifact_paths"
          label="制品路径"
          span="full"
          tips="相对仓库根；可为文件或目录；单文件不压缩，多路径打成一个包"
          :item-default="{ path: '' }"
          :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
        >
          <template #default="{ item }">
            <u-input v-model="item.path" placeholder="如 target/app.jar 或 dist" />
          </template>
        </u-group-input>
        <u-group-input
          field="cache_paths"
          label="缓存路径"
          span="full"
          tips="相对仓库根，如 .m2/repository；构建间复用"
          :item-default="{ path: '' }"
          :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
        >
          <template #default="{ item }">
            <u-input v-model="item.path" placeholder="路径" />
          </template>
        </u-group-input>
        <u-group-input
          field="env_var_names"
          label="环境变量名"
          span="full"
          tips="仅名称；运行时从宿主机 os.Environ 注入"
          :item-default="{ name: '' }"
          :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
        >
          <template #default="{ item }">
            <u-input v-model="item.name" placeholder="VAR_NAME" />
          </template>
        </u-group-input>
        <u-group-input
          field="env_vars"
          label="环境变量"
          span="full"
          tips="Key-Value 加密存储；同名时覆盖宿主机注入值；API 不回显明文"
          :item-default="{ key: '', value: '', has_value: false }"
          :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
        >
          <template #default="{ item }">
            <u-input v-model="item.key" placeholder="KEY" />
            <u-password-input
              v-model="item.value"
              :placeholder="item.has_value ? '已设置，留空不改' : '值'"
              autocomplete="new-password"
            />
          </template>
        </u-group-input>
      </template>

      <template #group:trigger>
        <u-form-item label="触发">
          <div class="trigger-row">
            <u-checkbox v-model="form.trigger_manual">手动</u-checkbox>
            <u-checkbox v-model="form.trigger_webhook">Webhook</u-checkbox>
            <u-checkbox v-model="form.trigger_cron">Cron</u-checkbox>
          </div>
        </u-form-item>
        <template v-if="form.trigger_cron">
          <u-input label="Cron 表达式" field="cron_expression" placeholder="如 0 */6 * * *" />
          <u-select
            label="时区"
            field="cron_timezone"
            :options="TIMEZONE_OPTIONS"
            filterable
            creatable
          />
        </template>
        <template v-if="form.trigger_webhook">
          <u-select label="Webhook 类型" field="webhook_type" :options="WEBHOOK_TYPE_OPTIONS" />
          <u-input
            label="分支 JSONPath"
            field="webhook_ref_path"
            placeholder="generic 平台可选"
            tips="generic Webhook 从 JSON 取分支的路径，如 $.ref"
          />
          <u-input
            label="提交 JSONPath"
            field="webhook_commit_path"
            tips="generic Webhook 取 commit hash 的 JSONPath"
          />
          <u-input
            label="消息 JSONPath"
            field="webhook_message_path"
            tips="generic Webhook 取提交说明的 JSONPath"
          />
        </template>
      </template>

      <template #group:artifact>
        <u-number-input
          label="制品保留"
          field="max_artifacts"
          tips="每个任务最多保留的历史制品数，超出后自动清理最旧的"
        />
        <u-select label="制品格式" field="artifact_format" :options="ARTIFACT_OPTIONS" />
        <u-select
          label="Agent 事件"
          field="agent_trigger_event"
          :options="AGENT_EVENT_OPTIONS"
          :tips="AGENT_EVENT_TIPS"
        />
        <u-multi-select
          label="执行 Agent"
          field="agent_ids"
          :options="agentOptions"
          placeholder="选择事件触发时执行的 Agent"
          filterable
          tips="构建事件触发时执行的 AI Agent；可多选，空则不触发"
        />
      </template>

      <template #group:deploy>
        <div class="deploy-targets">
          <div class="targets-toolbar">
            <span class="targets-hint">按顺序执行；Java 可配合 post_deploy 重启/切流量</span>
            <u-button size="small" type="primary" @click="addTarget">添加目标</u-button>
          </div>
          <div v-if="!form.deploy_targets.length" class="targets-empty">尚未配置部署目标</div>
          <div v-for="(t, idx) in form.deploy_targets" :key="idx" class="target-item">
            <span class="target-item__index">{{ idx + 1 }}</span>
            <div class="target-item__body">
              <div class="target-item__row">
                <u-select v-model="t.method" :options="METHOD_OPTIONS" style="width: 110px" />
                <u-select
                  v-if="t.method !== 'local'"
                  v-model="t.server_id"
                  :options="serverOptions"
                  placeholder="选择服务器"
                  style="width: 220px"
                />
                <u-input v-model="t.remote_path" placeholder="远程路径" style="flex: 1" />
              </div>
              <div v-if="t.method === 'rsync'" class="target-item__row target-item__mirror">
                <u-switch v-model="t.mirror" />
                <span class="target-item__script-caption" style="margin: 0">镜像同步</span>
                <span class="targets-hint">会删除目标目录中制品没有的文件</span>
              </div>
              <div class="target-item__script-caption">部署后脚本（可选）</div>
              <u-code-editor v-model="t.post_deploy_script" :default-lines="4" />
            </div>
            <u-button text type="danger" size="small" @click="removeTarget(idx)">删除</u-button>
          </div>
        </div>
      </template>
    </FormDialog>

    <u-dialog
      v-model="historyOpen"
      :title="historyJob ? `构建历史 · ${historyJob.name}` : '构建历史'"
      style="width: 960px"
    >
      <ProTable
        ref="history"
        url="/build-runs"
        :query="historyQuery"
        :columns="historyColumns"
        :auto-query-fields="['build_job_id']"
        :immediate="false"
        pagination
        height="420px"
      >
        <template #column:status="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).status, JOB_STATUS_TAG)">
            {{ (rowData as BuildRun).status }}
          </u-tag>
        </template>
        <template #column:stage="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).stage, BUILD_STAGE_TAG)">
            {{ (rowData as BuildRun).stage || "—" }}
          </u-tag>
        </template>
        <template #column:trigger_type="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as BuildRun).trigger_type, TRIGGER_TYPE_TAG)">
            {{ (rowData as BuildRun).trigger_type }}
          </u-tag>
        </template>
        <template #column:action="{ rowData }">
          <u-action @run="openRunDetail(rowData as BuildRun)">查看详情</u-action>
        </template>
      </ProTable>
      <template #footer="{ close }">
        <u-button text @click="close()">关闭</u-button>
      </template>
    </u-dialog>

    <u-dialog v-model="secretOpen" title="Webhook" style="width: 560px">
      <p class="mono">URL: {{ webhookInfo.url }}</p>
      <p class="mono">Secret: {{ webhookInfo.secret }}</p>
      <template #footer="{ close }">
        <u-button text @click="close()">关闭</u-button>
        <u-button
          v-if="hasPermission('cicd_build_jobs:update')"
          type="primary"
          :loading="rotateBusy"
          @click="rotateWebhookSecret"
        >
          轮换
        </u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.mono {
  font-family: ui-monospace, monospace;
  word-break: break-all;
}
.trigger-row {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
}
.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
// 自定义内容在 u-form 网格中默认只占一列，撑满整行
.deploy-targets {
  grid-column: 1 / -1;
}
.targets-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: fn.use-var(gap, default);
}
.targets-hint {
  font-size: fn.use-var(font-size-assist, default);
  color: fn.use-var(text-color, assist);
}
.targets-empty {
  padding: fn.use-var(gap, large) 0;
  text-align: center;
  font-size: fn.use-var(font-size-assist, default);
  color: fn.use-var(text-color, placeholder);
}
.target-item {
  display: flex;
  align-items: center;
  gap: fn.use-var(gap, default);
  padding: fn.use-var(gap, default);
  border-radius: fn.use-var(radius, default);
  background-color: fn.use-var(bg-color, middle);

  & + & {
    margin-top: fn.use-var(gap, default);
  }
}
.target-item__index {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background-color: fn.use-var(bg-color, top);
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  line-height: 18px;
  text-align: center;
}
.target-item__body {
  flex: 1;
  min-width: 0;
}
.target-item__row {
  display: flex;
  align-items: center;
  gap: fn.use-var(gap, default);
}
.target-item__mirror {
  margin-top: fn.use-var(gap, default);
}
.target-item__script-caption {
  margin: fn.use-var(gap, default) 0 4px;
  font-size: fn.use-var(font-size-assist, default);
  color: fn.use-var(text-color, assist);
}
:deep(.u-group-input__item > .u-input),
:deep(.u-group-input__item > .u-password-input) {
  flex: 1;
  min-width: 0;
}
.workspace-path-row {
  display: flex;
  align-items: center;
  gap: fn.use-var(gap, default);
  width: 100%;
  min-width: 0;
}
.workspace-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: fn.use-var(font-size-assist, default);
  color: fn.use-var(text-color, assist);
}
</style>
