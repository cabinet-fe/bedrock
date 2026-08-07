<script setup lang="ts">
defineOptions({ name: "CicdScriptJobs" });

import { computed, nextTick, onMounted, reactive, ref, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";
import { o } from "@cat-kit/core";
import { clipboard } from "@cat-kit/fe";
import { message } from "@veltra/desktop";
import type { CodeEditorLang } from "@veltra/desktop";

import {
  createScriptJob,
  deleteScriptJob,
  enqueueScriptRun,
  getScriptJob,
  getScriptJobWebhookSecret,
  rotateScriptJobWebhookSecret,
  updateScriptJob,
} from "@/api/cicd";
import type { AiAgentEnvVarInput, ScriptJob, ScriptRun } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import ProjectSelect from "@/components/project-select";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import { JOB_STATUS_TAG, TRIGGER_TYPE_TAG, tagType, type TagType } from "@/lib/tag";

function parsePositiveInt(raw: unknown): number | undefined {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

function queryFlag(raw: unknown): boolean {
  const value = Array.isArray(raw) ? raw[0] : raw;
  return value === "1" || value === "true";
}

const TIMEZONE_OPTIONS = [
  { label: "Asia/Shanghai（北京）", value: "Asia/Shanghai" },
  { label: "UTC", value: "UTC" },
  { label: "America/New_York（纽约）", value: "America/New_York" },
];

const SCRIPT_TYPE_OPTIONS = [
  { label: "Bash / sh", value: "bash" },
  { label: "Node.js", value: "node" },
  { label: "Python", value: "python" },
  { label: "PowerShell 7+ (pwsh)", value: "pwsh" },
  { label: "Windows PowerShell 5.x", value: "powershell" },
  { label: "CMD", value: "cmd" },
];

const SCRIPT_TYPE_LANG: Record<string, CodeEditorLang | undefined> = {
  bash: "bash",
  node: "js",
  pwsh: "powershell",
  powershell: "powershell",
};

type EnvNameDraft = { name: string };
type EnvVarDraft = { key: string; value: string; has_value?: boolean };

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: rotateBusy, run: runRotate } = useBusy();
const route = useRoute();
const router = useRouter();
const listRef = useTemplateRef("list");
const historyRef = useTemplateRef("history");
const query = reactive({
  keyword: "",
  project_id: parsePositiveInt(route.query.project_id),
});
const dialogOpen = ref(false);
const secretOpen = ref(false);
const historyOpen = ref(false);
const historyJob = ref<ScriptJob | null>(null);
const historyQuery = reactive({
  script_job_id: undefined as number | undefined,
  project_id: undefined as number | undefined,
});
const editing = ref<ScriptJob | null>(null);
const webhookInfo = reactive({ secret: "", url: "" });

const form = reactive({
  name: "",
  description: "",
  enabled: true,
  script_type: "bash",
  script: "",
  work_dir: "",
  env_var_names: [] as EnvNameDraft[],
  env_vars: [] as EnvVarDraft[],
  trigger_manual: true,
  trigger_webhook: false,
  trigger_cron: false,
  webhook_type: "generic",
  cron_expression: "",
  cron_timezone: "Asia/Shanghai",
  is_public: false,
  project_id: undefined as number | undefined,
});

const editorLangs = computed(() => {
  const lang = SCRIPT_TYPE_LANG[form.script_type];
  return lang ? [lang] : [];
});

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "script", title: "脚本配置" },
  { key: "trigger", title: "触发方式" },
];

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "triggers", name: "触发" },
  { key: "action", name: "操作", width: 360, align: "center", fixed: "right" },
]);

const historyColumns = defineProTableColumns([
  { key: "run_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
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
  { key: "action", name: "操作", width: 120, align: "center", fixed: "right" },
]);

function triggerParts(job: ScriptJob): { label: string; type: TagType }[] {
  const parts: { label: string; type: TagType }[] = [];
  if (job.trigger_manual) parts.push({ label: "手动", type: undefined });
  if (job.trigger_webhook) parts.push({ label: "Webhook", type: "info" });
  if (job.trigger_cron) parts.push({ label: "Cron", type: "primary" });
  return parts;
}

function canRun(job: ScriptJob) {
  return job.enabled && job.trigger_manual;
}

function openCreate(projectID?: number) {
  editing.value = null;
  o(form).extend({
    name: "",
    description: "",
    enabled: true,
    script_type: "bash",
    script: "",
    work_dir: "",
    env_var_names: [],
    env_vars: [],
    trigger_manual: true,
    trigger_webhook: false,
    trigger_cron: false,
    webhook_type: "generic",
    cron_expression: "",
    cron_timezone: "Asia/Shanghai",
    is_public: false,
    project_id: typeof projectID === "number" ? projectID : undefined,
  });
  dialogOpen.value = true;
}

function openEdit(row: ScriptJob) {
  editing.value = row;
  o(form).extend(o(row).omit(["env_var_names", "env_vars", "workspace_path"]));
  form.env_var_names = (row.env_var_names ?? []).map((name) => ({ name }));
  form.env_vars = (row.env_vars ?? []).map((e) => ({
    key: e.key,
    value: "",
    has_value: e.has_value,
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

function buildBody(): Record<string, unknown> | undefined {
  const envVars: AiAgentEnvVarInput[] = [];
  const seenKeys = new Set<string>();
  for (const e of form.env_vars) {
    const key = e.key.trim();
    if (!key) {
      message.error("环境变量 key 不能为空");
      return;
    }
    if (seenKeys.has(key)) {
      message.error(`环境变量 key 重复: ${key}`);
      return;
    }
    seenKeys.add(key);
    const row: AiAgentEnvVarInput = { key };
    if (e.value !== "") row.value = e.value;
    else if (!e.has_value) {
      message.error(`新建环境变量 ${key} 必须填写值`);
      return;
    }
    envVars.push(row);
  }
  return {
    ...o(form).omit(["env_var_names", "env_vars"]),
    env_var_names: form.env_var_names.map((e) => e.name.trim()).filter(Boolean),
    env_vars: envVars,
    project_id: form.project_id ?? 0,
  };
}

async function save() {
  const body = buildBody();
  if (!body) return;
  try {
    if (editing.value) {
      await updateScriptJob(editing.value.id, body);
      message.success("已更新");
    } else {
      await createScriptJob(body);
      message.success("已创建");
    }
    dialogOpen.value = false;
    void listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

async function onDelete(row: ScriptJob) {
  try {
    await deleteScriptJob(row.id);
    message.success("已删除");
    void listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
}

async function onRun(row: ScriptJob) {
  try {
    const run = await enqueueScriptRun(row.id);
    message.success(`已入队 #${run.run_number}`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "触发失败");
  }
}

async function openSecret(row: ScriptJob) {
  try {
    const info = await getScriptJobWebhookSecret(row.id);
    webhookInfo.secret = info.webhook_secret;
    webhookInfo.url = info.webhook_url;
    editing.value = row;
    secretOpen.value = true;
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  }
}

async function onRotateSecret() {
  if (!editing.value) return;
  await runRotate(async () => {
    const info = await rotateScriptJobWebhookSecret(editing.value!.id);
    webhookInfo.secret = info.webhook_secret;
    webhookInfo.url = info.webhook_url;
    message.success("已轮换");
  });
}

function openHistory(row: ScriptJob) {
  historyJob.value = row;
  historyQuery.script_job_id = row.id;
  historyQuery.project_id = row.project_id ?? undefined;
  historyOpen.value = true;
  void nextTick(() => historyRef.value?.reload());
}

function openRunDetail(row: ScriptRun) {
  historyOpen.value = false;
  void router.push({ name: "cicd-script-run-detail", params: { id: String(row.id) } });
}

onMounted(async () => {
  const editID = parsePositiveInt(route.query.id);
  const prefillID = parsePositiveInt(route.query.project_id);
  if (editID != null && hasPermission("cicd_script_jobs:update")) {
    try {
      openEdit(await getScriptJob(editID));
    } catch (err) {
      message.error(err instanceof Error ? err.message : "加载任务失败");
    }
  } else if (queryFlag(route.query.create) && hasPermission("cicd_script_jobs:create")) {
    openCreate(prefillID);
  }
});
</script>

<template>
  <div>
    <ProTable
      ref="list"
      url="/script-jobs"
      :query="query"
      :columns="columns"
      :auto-query-fields="['project_id']"
      pagination
    >
      <template #filters>
        <ProjectSelect v-model="query.project_id" placeholder="全部项目" style="width: 180px" />
        <u-input v-model="query.keyword" clearable placeholder="搜索名称" style="width: 200px" />
      </template>
      <template #toolbar>
        <u-button
          v-if="hasPermission('cicd_script_jobs:create')"
          type="primary"
          @click="openCreate()"
        >
          新建
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as ScriptJob).enabled ? 'success' : 'info'">
          {{ (rowData as ScriptJob).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>
      <template #column:triggers="{ rowData }">
        <span class="triggers">
          <u-tag
            v-for="p in triggerParts(rowData as ScriptJob)"
            :key="p.label"
            size="small"
            :type="p.type"
          >
            {{ p.label }}
          </u-tag>
        </span>
      </template>
      <template #column:action="{ rowData }">
        <u-action
          v-if="hasPermission('cicd_script_jobs:update')"
          @run="openEdit(rowData as ScriptJob)"
        >
          编辑
        </u-action>
        <u-action
          v-if="hasPermission('cicd_script_jobs:execute')"
          :disabled="!canRun(rowData as ScriptJob)"
          :busy="busyKey === `run-${(rowData as ScriptJob).id}`"
          @run="bind(`run-${(rowData as ScriptJob).id}`, () => onRun(rowData as ScriptJob))"
        >
          执行
        </u-action>
        <u-action
          v-if="hasPermission('cicd_script_jobs:view')"
          @run="openHistory(rowData as ScriptJob)"
        >
          记录
        </u-action>
        <u-action
          v-if="hasPermission('cicd_script_jobs:view') && (rowData as ScriptJob).trigger_webhook"
          @run="openSecret(rowData as ScriptJob)"
        >
          Webhook
        </u-action>
        <u-action
          v-if="hasPermission('cicd_script_jobs:delete')"
          type="danger"
          confirm="确认删除该脚本任务？"
          @run="onDelete(rowData as ScriptJob)"
        >
          删除
        </u-action>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑脚本任务' : '新建脚本任务'"
      :model="form"
      :groups="formGroups"
      label-width="110px"
      style="width: 960px"
      @submit="save"
    >
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="描述" field="description" />
        <ProjectSelect label="所属项目" field="project_id" />
        <u-switch label="启用" field="enabled" />
        <u-switch
          label="公开"
          field="is_public"
          tips="开启后，仅自己数据权限的用户也可查看该任务与执行记录"
        />
      </template>

      <template #group:script>
        <u-form-item v-if="editing?.workspace_path" label="工作区路径" span="full">
          <div class="workspace-path-row">
            <code class="workspace-path">{{ editing.workspace_path }}</code>
            <u-button size="small" @click="copyWorkspacePath">复制</u-button>
          </div>
        </u-form-item>
        <u-select label="脚本类型" field="script_type" :options="SCRIPT_TYPE_OPTIONS" />
        <u-code-editor
          label="脚本"
          field="script"
          :langs="editorLangs"
          :default-lines="12"
          tips="支持 ${{ job.id }} / ${{ workspace }} / ${{ env.KEY }} 模板"
          span="full"
        />
        <u-input label="工作目录" field="work_dir" placeholder="相对工作区，可留空" />
        <u-group-input
          field="env_var_names"
          label="环境变量名"
          span="full"
          tips="仅名称；运行时从宿主机注入"
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
          tips="Key-Value 加密存储；API 不回显明文"
          :item-default="{ key: '', value: '', has_value: false }"
          :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
        >
          <template #default="{ item }">
            <u-input v-model="item.key" placeholder="KEY" />
            <u-input
              v-model="item.value"
              :placeholder="item.has_value ? '留空保留原值' : 'value'"
            />
          </template>
        </u-group-input>
      </template>

      <template #group:trigger>
        <u-switch label="手动触发" field="trigger_manual" />
        <u-switch label="Webhook" field="trigger_webhook" />
        <u-switch label="Cron" field="trigger_cron" />
        <template v-if="form.trigger_cron">
          <u-input label="Cron 表达式" field="cron_expression" placeholder="0 2 * * *" />
          <u-select
            label="时区"
            field="cron_timezone"
            filterable
            creatable
            :options="TIMEZONE_OPTIONS"
          />
        </template>
      </template>
    </FormDialog>

    <u-dialog v-model="secretOpen" title="Webhook" width="560px">
      <div class="secret-box">
        <div>
          <div class="secret-label">URL</div>
          <code>{{ webhookInfo.url }}</code>
        </div>
        <div>
          <div class="secret-label">Secret</div>
          <code>{{ webhookInfo.secret }}</code>
        </div>
      </div>
      <template #footer>
        <u-button
          v-if="hasPermission('cicd_script_jobs:update')"
          :loading="rotateBusy"
          @click="onRotateSecret"
        >
          轮换密钥
        </u-button>
      </template>
    </u-dialog>

    <u-dialog v-model="historyOpen" :title="`执行记录 · ${historyJob?.name ?? ''}`" width="860px">
      <ProTable
        ref="history"
        url="/script-runs"
        :query="historyQuery"
        :columns="historyColumns"
        pagination
      >
        <template #column:status="{ rowData }">
          <u-tag size="small" :type="tagType((rowData as ScriptRun).status, JOB_STATUS_TAG)">
            {{ (rowData as ScriptRun).status }}
          </u-tag>
        </template>
        <template #column:trigger_type="{ rowData }">
          <u-tag
            size="small"
            :type="tagType((rowData as ScriptRun).trigger_type, TRIGGER_TYPE_TAG)"
          >
            {{ (rowData as ScriptRun).trigger_type }}
          </u-tag>
        </template>
        <template #column:action="{ rowData }">
          <u-action @run="openRunDetail(rowData as ScriptRun)">详情</u-action>
        </template>
      </ProTable>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
.triggers {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}

.workspace-path-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.workspace-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.secret-box {
  display: flex;
  flex-direction: column;
  gap: 16px;

  code {
    display: block;
    word-break: break-all;
    font-size: 12px;
  }
}

.secret-label {
  margin-bottom: 4px;
  font-size: 12px;
  opacity: 0.7;
}
</style>
