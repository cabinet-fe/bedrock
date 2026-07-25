<script setup lang="ts">
defineOptions({ name: "AiAgents" });

import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  createAgent,
  createTrigger,
  deleteAgent,
  deleteTrigger,
  listSkills,
  listTriggers,
  manualRunAgent,
  updateAgent,
} from "@/api/ai";
import { listBuildJobs } from "@/api/cicd";
import { listRepositories, listRepositoryBranches, syncRepositoryBranches } from "@/api/resource";
import type {
  AiAgent,
  AiAgentEnvVarInput,
  AiAgentRepoBinding,
  BuildJob,
  SkillPackage,
} from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { tagType, type TagType } from "@/lib/tag";
import RunHistoryDialog from "../components/run-history-dialog.vue";

const CLI_KEY_TAG: Record<string, TagType> = {
  claude_code: "primary",
  opencode: "info",
  reasonix: "success",
  codex: "warning",
};

const WORKSPACE_STATUS_TAG: Record<string, TagType> = {
  ready: "success",
  pending: "warning",
  failed: "danger",
};

const WORKSPACE_STATUS_LABEL: Record<string, string> = {
  ready: "就绪",
  pending: "初始化中",
  failed: "失败",
};

const TRIGGER_TYPE_LABEL: Record<string, string> = {
  manual: "手动",
  api: "API",
  cron: "Cron",
  build_event: "构建事件",
};

type TriggerDraft = {
  /** Existing server id; undefined = newly added locally */
  id?: number;
  type: string;
  cron_expression: string;
  cron_timezone: string;
  build_job_id?: number;
  build_event: string;
};

type RepoBindingDraft = {
  repository_id?: number;
  branch: string;
};

type EnvVarDraft = {
  key: string;
  value: string;
  has_value?: boolean;
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const router = useRouter();
const table = useTemplateRef("table");
const dialogOpen = ref(false);
const historyOpen = ref(false);
const historyAgent = ref<AiAgent | null>(null);
const editing = ref<AiAgent | null>(null);
const skills = ref<SkillPackage[]>([]);
const buildJobs = ref<BuildJob[]>([]);
const repoOptions = ref<{ label: string; value: number }[]>([]);
const branchOptionsByRepo = ref<Record<number, { label: string; value: string }[]>>({});
const branchesLoadingByRepo = ref<Record<number, boolean>>({});
const branchesSyncingByRepo = ref<Record<number, boolean>>({});
/** Triggers shown in the agent form (existing + newly added drafts). */
const formTriggers = ref<TriggerDraft[]>([]);
/** Snapshot of server trigger ids when the edit dialog opened. */
const initialTriggerIDs = ref<number[]>([]);

const form = reactive({
  name: "",
  description: "",
  enabled: true,
  cli_key: "claude_code",
  system_prompt: "",
  skill_ids: [] as number[],
  repo_bindings: [] as RepoBindingDraft[],
  env_vars: [] as EnvVarDraft[],
  output_dir: "output",
  stream_output: false,
  timeout_sec: 600,
});

const TRIGGER_DRAFT_DEFAULTS = {
  type: "manual",
  cron_expression: "0 * * * *",
  cron_timezone: "Asia/Shanghai",
  build_job_id: undefined as number | undefined,
  build_event: "artifact_ready",
};
const triggerDraft = reactive({ ...TRIGGER_DRAFT_DEFAULTS });

const skillOptions = computed(() =>
  skills.value.map((s) => ({
    label: `${s.name}${s.visibility === "private" ? " (私有)" : ""}`,
    value: s.id,
  })),
);

const buildJobOptions = computed(() =>
  buildJobs.value.map((j) => ({
    label: `${j.name} (job-${j.id})`,
    value: j.id,
  })),
);

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "cli_key", name: "CLI", width: 120, align: "center" },
  { key: "workspace_status", name: "工作区", width: 110, align: "center" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "action", name: "操作", width: 320, align: "center", fixed: "right" },
]);

function openRunHistory(row: AiAgent) {
  historyAgent.value = row;
  historyOpen.value = true;
}

onMounted(async () => {
  const tasks: Promise<void>[] = [];
  if (hasPermission("ai_skills:view")) {
    tasks.push(
      listSkills({ page: 1, page_size: 200 })
        .then((res) => {
          skills.value = res.items ?? [];
        })
        .catch(() => {
          skills.value = [];
        }),
    );
  }
  if (hasPermission("cicd_build_jobs:view")) {
    tasks.push(
      listBuildJobs({ page: 1, page_size: 200 })
        .then((res) => {
          buildJobs.value = res.items ?? [];
        })
        .catch(() => {
          buildJobs.value = [];
        }),
    );
  }
  if (hasPermission("resource_repositories:view")) {
    tasks.push(
      listRepositories({ page: 1, page_size: 200 })
        .then((res) => {
          repoOptions.value = (res.items ?? []).map((r) => ({ label: r.name, value: r.id }));
        })
        .catch(() => {
          repoOptions.value = [];
        }),
    );
  }
  await Promise.all(tasks);
});

function resetTriggerDraft() {
  Object.assign(triggerDraft, TRIGGER_DRAFT_DEFAULTS);
}

async function loadBranches(repositoryId?: number, force = false) {
  if (!repositoryId) return;
  if (!force && branchOptionsByRepo.value[repositoryId]) return;
  branchesLoadingByRepo.value = { ...branchesLoadingByRepo.value, [repositoryId]: true };
  try {
    const { items } = await listRepositoryBranches(repositoryId);
    branchOptionsByRepo.value = {
      ...branchOptionsByRepo.value,
      [repositoryId]: items.map((b) => ({ label: b, value: b })),
    };
  } catch {
    branchOptionsByRepo.value = { ...branchOptionsByRepo.value, [repositoryId]: [] };
  } finally {
    branchesLoadingByRepo.value = { ...branchesLoadingByRepo.value, [repositoryId]: false };
  }
}

async function refreshBranches(repositoryId?: number) {
  if (!repositoryId) return;
  if (!hasPermission("resource_repositories:update")) {
    message.error("需要仓库更新权限才能同步分支");
    return;
  }
  branchesSyncingByRepo.value = { ...branchesSyncingByRepo.value, [repositoryId]: true };
  try {
    const { items } = await syncRepositoryBranches(repositoryId);
    branchOptionsByRepo.value = {
      ...branchOptionsByRepo.value,
      [repositoryId]: items.map((b) => ({ label: b, value: b })),
    };
    if (!items.length) {
      message.warning("未获取到分支，请检查仓库 URL / 凭证");
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : "同步分支失败");
  } finally {
    branchesSyncingByRepo.value = { ...branchesSyncingByRepo.value, [repositoryId]: false };
  }
}

function branchOptionsFor(repositoryId?: number) {
  if (!repositoryId) return [];
  return branchOptionsByRepo.value[repositoryId] ?? [];
}

function branchPlaceholder(repositoryId?: number) {
  if (!repositoryId) return "先选择仓库";
  if (branchesLoadingByRepo.value[repositoryId] || branchesSyncingByRepo.value[repositoryId]) {
    return "加载分支…";
  }
  const opts = branchOptionsByRepo.value[repositoryId];
  if (opts && opts.length === 0) return "无缓存，可点同步";
  return "选择或输入分支";
}

function onRepoChange(item: RepoBindingDraft) {
  item.branch = "main";
  void loadBranches(item.repository_id, true);
}

function canRun(row: AiAgent) {
  return row.enabled && row.workspace_status === "ready";
}

function runDisabledTip(row: AiAgent) {
  if (!row.enabled) return "智能体未启用";
  if (row.workspace_status === "pending") return "工作区初始化中";
  if (row.workspace_status === "failed") {
    return row.workspace_error || "工作区初始化失败";
  }
  return "";
}

function openCreate() {
  editing.value = null;
  form.skill_ids = [];
  form.repo_bindings = [];
  form.env_vars = [];
  formTriggers.value = [];
  initialTriggerIDs.value = [];
  resetTriggerDraft();
  dialogOpen.value = true;
}

async function openEdit(row: AiAgent) {
  editing.value = row;
  o(form).extend(row);
  form.skill_ids = [...(row.skill_ids ?? [])];
  form.repo_bindings = (row.repo_bindings ?? []).map((b: AiAgentRepoBinding) => ({
    repository_id: b.repository_id,
    branch: b.branch || "main",
  }));
  form.env_vars = (row.env_vars ?? []).map((e) => ({
    key: e.key,
    value: "",
    has_value: e.has_value,
  }));
  form.output_dir = row.output_dir || "output";
  resetTriggerDraft();
  for (const b of form.repo_bindings) {
    void loadBranches(b.repository_id);
  }
  try {
    const items = await listTriggers(row.id);
    formTriggers.value = items.map((t) => ({
      id: t.id,
      type: t.type,
      cron_expression: t.cron_expression ?? "",
      cron_timezone: t.cron_timezone ?? "UTC",
      build_job_id: t.build_job_id ?? undefined,
      build_event: t.build_event ?? "artifact_ready",
    }));
    initialTriggerIDs.value = items.map((t) => t.id);
  } catch {
    formTriggers.value = [];
    initialTriggerIDs.value = [];
    message.error("加载触发器失败");
  }
  dialogOpen.value = true;
}

function buildJobLabel(jobID?: number) {
  if (!jobID) return "";
  const job = buildJobs.value.find((j) => j.id === jobID);
  return job ? `${job.name} (job-${job.id})` : `job-${jobID}`;
}

function triggerSummary(t: TriggerDraft): string {
  const typeLabel = TRIGGER_TYPE_LABEL[t.type] ?? t.type;
  if (t.type === "cron") {
    return `${typeLabel} · ${t.cron_expression} (${t.cron_timezone})`;
  }
  if (t.type === "build_event") {
    return `${typeLabel} · ${buildJobLabel(t.build_job_id)} · ${t.build_event}`;
  }
  return typeLabel;
}

function addTriggerDraft() {
  if (triggerDraft.type === "cron") {
    if (!triggerDraft.cron_expression.trim() || !triggerDraft.cron_timezone.trim()) {
      message.error("请填写 Cron 表达式与时区");
      return;
    }
  }
  if (triggerDraft.type === "build_event") {
    if (!triggerDraft.build_job_id) {
      message.error("请选择构建任务");
      return;
    }
  }
  formTriggers.value.push({
    type: triggerDraft.type,
    cron_expression: triggerDraft.cron_expression,
    cron_timezone: triggerDraft.cron_timezone,
    build_job_id: triggerDraft.build_job_id,
    build_event: triggerDraft.build_event,
  });
  resetTriggerDraft();
}

function removeFormTrigger(index: number) {
  formTriggers.value.splice(index, 1);
}

function triggerPayload(t: TriggerDraft) {
  return {
    type: t.type,
    enabled: true,
    cron_expression: t.type === "cron" ? t.cron_expression : "",
    cron_timezone: t.type === "cron" ? t.cron_timezone : "UTC",
    build_job_id: t.type === "build_event" ? t.build_job_id : undefined,
    build_event: t.type === "build_event" ? t.build_event : "",
  };
}

async function syncTriggers(agentID: number) {
  const keptIDs = new Set(
    formTriggers.value.map((t) => t.id).filter((id): id is number => id != null),
  );
  const toDelete = initialTriggerIDs.value.filter((id) => !keptIDs.has(id));
  for (const tid of toDelete) {
    await deleteTrigger(agentID, tid);
  }
  const toCreate = formTriggers.value.filter((t) => t.id == null);
  for (const draft of toCreate) {
    await createTrigger(agentID, triggerPayload(draft));
  }
}

async function save() {
  const bindings: AiAgentRepoBinding[] = [];
  const seen = new Set<number>();
  for (const b of form.repo_bindings) {
    if (!b.repository_id) {
      message.error("请为每条绑定选择仓库");
      return;
    }
    if (seen.has(b.repository_id)) {
      message.error("同一智能体内仓库不能重复绑定");
      return;
    }
    seen.add(b.repository_id);
    bindings.push({
      repository_id: b.repository_id,
      branch: (b.branch || "main").trim() || "main",
    });
  }
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
  const body = {
    ...form,
    output_dir: form.output_dir || "output",
    repo_bindings: bindings,
    env_vars: envVars,
  };
  try {
    let agentID: number;
    if (editing.value) {
      await updateAgent(editing.value.id, body);
      agentID = editing.value.id;
    } else {
      const created = await createAgent(body);
      agentID = created.id;
    }
    await syncTriggers(agentID);
    dialogOpen.value = false;
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

const run = bind(async (row: AiAgent) => {
  if (!canRun(row)) {
    message.error(runDisabledTip(row) || "无法运行");
    return;
  }
  try {
    const agentRun = await manualRunAgent(row.id);
    message.success(`已创建运行 #${agentRun.id}`);
    await router.push(`/ai/runs/${agentRun.id}`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "触发失败");
  }
});

const remove = bind(async (row: AiAgent) => {
  try {
    await deleteAgent(row.id);
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="table" url="/ai/agents" pagination :columns="columns">
      <template #filters>
        <u-button
          v-if="hasPermission('ai_agents:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建
        </u-button>
      </template>
      <template #column:cli_key="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as AiAgent).cli_key, CLI_KEY_TAG)">
          {{ (rowData as AiAgent).cli_key }}
        </u-tag>
      </template>
      <template #column:workspace_status="{ rowData }">
        <u-tag
          size="small"
          :type="tagType((rowData as AiAgent).workspace_status, WORKSPACE_STATUS_TAG)"
          :title="
            (rowData as AiAgent).workspace_status === 'failed'
              ? (rowData as AiAgent).workspace_error || '工作区初始化失败'
              : undefined
          "
        >
          {{ WORKSPACE_STATUS_LABEL[(rowData as AiAgent).workspace_status] ?? "未知" }}
        </u-tag>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as AiAgent).enabled ? 'success' : undefined">
          {{ (rowData as AiAgent).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as AiAgent).id">
          <u-action v-if="hasPermission('ai_agents:update')" @run="openEdit(rowData as AiAgent)">
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('ai_agents:execute')"
            :disabled="!canRun(rowData as AiAgent)"
            :title="runDisabledTip(rowData as AiAgent)"
            @run="run(rowData as AiAgent)"
          >
            运行
          </u-action>
          <u-action v-if="hasPermission('ai_runs:view')" @run="openRunHistory(rowData as AiAgent)">
            运行历史
          </u-action>
          <u-action
            v-if="hasPermission('ai_agents:delete')"
            type="danger"
            @run="remove(rowData as AiAgent)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑智能体' : '新建智能体'"
      :model="form"
      label-width="110px"
      style="width: 1200px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="描述" field="description" />
      <u-select
        label="CLI"
        field="cli_key"
        :options="[
          { label: 'Claude Code', value: 'claude_code' },
          { label: 'OpenCode', value: 'opencode' },
          { label: 'Reasonix', value: 'reasonix' },
          { label: 'Codex', value: 'codex' },
        ]"
        :rules="{ required: '必填' }"
      />
      <u-textarea
        label="系统提示词"
        field="system_prompt"
        span="full"
        :rows="6"
        placeholder="描述任务目标；若需访问绑定仓库，请写相对路径，如 ./repo-12"
      />
      <u-multi-select
        label="技能"
        field="skill_ids"
        :options="skillOptions"
        placeholder="选择可访问的技能"
        filterable
        clearable
      />
      <u-group-input
        field="repo_bindings"
        label="仓库绑定"
        span="full"
        :item-default="{ repository_id: undefined, branch: 'main' }"
        :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
      >
        <template #default="{ item }">
          <u-select
            v-model="item.repository_id"
            :options="repoOptions"
            filterable
            clearable
            placeholder="选择仓库"
            @change="onRepoChange(item)"
          />
          <u-select
            v-model="item.branch"
            :options="branchOptionsFor(item.repository_id)"
            filterable
            creatable
            :disabled="!item.repository_id"
            :placeholder="branchPlaceholder(item.repository_id)"
            @focus="loadBranches(item.repository_id)"
          />
          <u-button
            text
            size="small"
            :disabled="!item.repository_id || branchesSyncingByRepo[item.repository_id]"
            @click="refreshBranches(item.repository_id)"
          >
            同步
          </u-button>
        </template>
      </u-group-input>
      <u-group-input
        field="env_vars"
        label="环境变量"
        span="full"
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
      <u-input label="产出目录名" field="output_dir" placeholder="默认 output" />
      <u-number-input label="超时(秒)" field="timeout_sec" :min="30" />
      <u-switch label="流式输出" field="stream_output" />

      <u-switch label="启用" field="enabled" />

      <u-form-item label="触发器" span="full">
        <div class="trigger-section">
          <ul v-if="formTriggers.length" class="trigger-list">
            <li
              v-for="(t, index) in formTriggers"
              :key="t.id ?? `new-${index}`"
              class="trigger-row"
            >
              <span class="trigger-summary">{{ triggerSummary(t) }}</span>
              <u-button text type="danger" size="small" @click="removeFormTrigger(index)">
                移除
              </u-button>
            </li>
          </ul>
          <p v-else class="trigger-empty">暂无触发器，可在下方添加</p>

          <div class="trigger-draft">
            <div class="trigger-draft-row">
              <span class="trigger-draft-label">类型</span>
              <u-select
                v-model="triggerDraft.type"
                :options="[
                  { label: '手动', value: 'manual' },
                  { label: 'API', value: 'api' },
                  { label: 'Cron', value: 'cron' },
                  { label: '构建事件', value: 'build_event' },
                ]"
              />
            </div>
            <template v-if="triggerDraft.type === 'cron'">
              <div class="trigger-draft-row">
                <span class="trigger-draft-label">表达式</span>
                <u-input v-model="triggerDraft.cron_expression" placeholder="如 0 * * * *" />
              </div>
              <div class="trigger-draft-row">
                <span class="trigger-draft-label">时区</span>
                <u-input
                  v-model="triggerDraft.cron_timezone"
                  placeholder="IANA，如 Asia/Shanghai"
                />
              </div>
            </template>
            <template v-if="triggerDraft.type === 'build_event'">
              <div class="trigger-draft-row">
                <span class="trigger-draft-label">构建任务</span>
                <u-select
                  v-model="triggerDraft.build_job_id"
                  :options="buildJobOptions"
                  filterable
                  clearable
                  placeholder="选择构建任务"
                />
              </div>
              <div class="trigger-draft-row">
                <span class="trigger-draft-label">事件</span>
                <u-select
                  v-model="triggerDraft.build_event"
                  :options="[
                    { label: 'artifact_ready（默认）', value: 'artifact_ready' },
                    { label: 'distribution_finished', value: 'distribution_finished' },
                  ]"
                />
              </div>
            </template>
            <u-button size="small" @click="addTriggerDraft">添加到列表</u-button>
          </div>
        </div>
      </u-form-item>
    </FormDialog>

    <RunHistoryDialog v-model="historyOpen" :agent="historyAgent" />
  </div>
</template>

<style scoped lang="scss">
:deep(.u-group-input__item > .u-select),
:deep(.u-group-input__item > .u-input),
:deep(.u-group-input__item > .u-password-input) {
  flex: 1;
  min-width: 0;
}

.trigger-section {
  width: 100%;
}

.trigger-list {
  margin: 0 0 12px;
  padding: 0;
  list-style: none;
}

.trigger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
  font-size: 13px;
}

.trigger-summary {
  flex: 1;
  min-width: 0;
}

.trigger-empty {
  margin: 0 0 12px;
  font-size: 13px;
  opacity: 0.65;
}

.trigger-draft {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 4px;
}

.trigger-draft-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.trigger-draft-label {
  flex: 0 0 64px;
  font-size: 13px;
  opacity: 0.8;
}

.trigger-draft-row > :deep(.u-select),
.trigger-draft-row > :deep(.u-input) {
  flex: 1;
  min-width: 0;
}
</style>
