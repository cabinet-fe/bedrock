<script setup lang="ts">
defineOptions({ name: "CicdBuildJobs" });

import { computed, onMounted, reactive, ref, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

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
import type { BuildJob, BuildRun, DeployTarget, Repository, Server } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import {
  BUILD_STAGE_TAG,
  JOB_STATUS_TAG,
  TRIGGER_TYPE_TAG,
  tagType,
  type TagType,
} from "@/lib/tag";

const METHOD_OPTIONS = [
  { label: "rsync", value: "rsync" },
  { label: "sftp", value: "sftp" },
  { label: "scp", value: "scp" },
  { label: "agent", value: "agent" },
  { label: "local", value: "local" },
];

const ARTIFACT_OPTIONS = [
  { label: "gzip", value: "gzip" },
  { label: "zip", value: "zip" },
];

const BUILD_SCRIPT_TYPE_OPTIONS = [
  { label: "Bash / sh", value: "bash" },
  { label: "Node.js", value: "node" },
  { label: "Python", value: "python" },
  { label: "PowerShell 7+ (pwsh)", value: "pwsh" },
  { label: "Windows PowerShell 5.x", value: "powershell" },
  { label: "CMD", value: "cmd" },
];

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: rotateBusy, run: runRotate } = useBusy();
const router = useRouter();
const listRef = useTemplateRef("list");
const historyRef = useTemplateRef("history");
const query = reactive({ keyword: "", repository_id: undefined as number | undefined });
const dialogOpen = ref(false);
const secretOpen = ref(false);
const historyOpen = ref(false);
const historyJob = ref<BuildJob | null>(null);
const historyQuery = reactive({ build_job_id: undefined as number | undefined });
const editing = ref<BuildJob | null>(null);
const webhookInfo = reactive({ secret: "", url: "" });
const repoOptions = ref<{ label: string; value: number }[]>([]);
const serverOptions = ref<{ label: string; value: number }[]>([]);
const branchOptions = ref<{ label: string; value: string }[]>([]);
const branchesLoading = ref(false);
const form = reactive({
  repository_id: undefined as number | undefined,
  name: "",
  description: "",
  enabled: true,
  branch: "main",
  shallow_clone: true,
  build_script_type: "bash",
  build_script: "",
  work_dir: "",
  output_dir: "",
  env_var_names: "",
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
  artifact_format: "gzip",
  agent_trigger_event: "artifact_ready",
  agent_id: undefined as number | undefined,
  deploy_targets: [] as DeployTarget[],
});

const branchPlaceholder = computed(() => (branchesLoading.value ? "加载分支…" : "选择或输入分支"));
const showPs5Tip = computed(() => form.build_script_type === "powershell");

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
  { key: "branch", name: "分支" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "triggers", name: "触发" },
  { key: "action", name: "操作", width: 400, align: "center", fixed: "right" },
]);

const historyColumns = defineProTableColumns([
  { key: "build_number", name: "#" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "stage", name: "阶段", width: 100, align: "center" },
  { key: "branch", name: "分支" },
  { key: "trigger_type", name: "触发", width: 100, align: "center" },
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
});

function repoName(repositoryId: number): string {
  return repoNameMap.value.get(repositoryId) ?? `#${repositoryId}`;
}

function openHistory(row: BuildJob) {
  historyJob.value = row;
  historyQuery.build_job_id = row.id;
  historyOpen.value = true;
}

function openRunDetail(row: BuildRun) {
  void router.push({ name: "cicd-build-run-detail", params: { id: String(row.id) } });
}

watch(historyOpen, (open) => {
  if (open) {
    void historyRef.value?.reload();
  }
});

function triggerParts(job: BuildJob): { label: string; type: TagType }[] {
  const parts: { label: string; type: TagType }[] = [];
  if (job.trigger_manual) parts.push({ label: "手动", type: undefined });
  if (job.trigger_webhook) parts.push({ label: "Webhook", type: "info" });
  if (job.trigger_cron) parts.push({ label: "Cron", type: "primary" });
  return parts;
}

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

async function openEdit(row: BuildJob) {
  try {
    const full = await getBuildJob(row.id);
    editing.value = full;
    o(form).extend(full);
    form.env_var_names = (full.env_var_names ?? []).join(",");
    form.deploy_targets = (full.deploy_targets ?? []).map((t) => ({ ...t }));
    dialogOpen.value = true;
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  }
}

function addTarget() {
  form.deploy_targets.push({
    server_id: undefined,
    remote_path: "",
    method: "rsync",
    post_deploy_script: "",
    sort_order: form.deploy_targets.length,
  });
}

function removeTarget(idx: number) {
  form.deploy_targets.splice(idx, 1);
}

function buildBody(): Record<string, unknown> {
  const { env_var_names, deploy_targets, agent_id, ...rest } = form;
  return {
    ...rest,
    env_var_names: env_var_names
      .split(/[,;\s]+/)
      .map((s) => s.trim())
      .filter(Boolean),
    agent_id: agent_id || null,
    deploy_targets: deploy_targets.map((t, i) => ({
      server_id: t.method === "local" ? null : t.server_id,
      remote_path: t.remote_path,
      method: t.method,
      post_deploy_script: t.post_deploy_script || "",
      sort_order: t.sort_order ?? i,
    })),
  };
}

async function save() {
  try {
    const body = buildBody();
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
    await router.push({ name: "cicd-build-run-detail", params: { id: run.id } });
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
      :auto-query-fields="['repository_id']"
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.repository_id"
          :options="repoOptions"
          placeholder="全部仓库"
          clearable
          style="width: 180px"
        />
        <u-input v-model="query.keyword" placeholder="名称" style="width: 160px" />
        <u-button
          v-if="hasPermission('cicd_build_jobs:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建任务
        </u-button>
      </template>
      <template #column:repository="{ rowData }">
        {{ repoName((rowData as BuildJob).repository_id) }}
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
      label-width="110px"
      style="width: 1180px"
      @submit="save"
    >
      <u-select
        label="仓库"
        field="repository_id"
        :options="repoOptions"
        :disabled="!!editing"
        :rules="{ required: '必填' }"
      />
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="描述" field="description" />
      <u-switch label="启用" field="enabled" />
      <u-select
        label="分支"
        field="branch"
        :options="branchOptions"
        filterable
        creatable
        :disabled="!form.repository_id"
        :placeholder="branchPlaceholder"
      />
      <u-switch label="浅克隆" field="shallow_clone" />
      <u-select label="脚本类型" field="build_script_type" :options="BUILD_SCRIPT_TYPE_OPTIONS" />
      <p v-if="showPs5Tip" class="script-tip">
        Windows PowerShell 5.x 不支持 <code>&&</code>，请改用多行、<code>pwsh</code> 或
        <code>cmd</code>
      </p>
      <u-code-editor
        label="构建脚本"
        field="build_script"
        :langs="['js']"
        :default-lines="12"
        tips="语法高亮为 JavaScript 模式，不影响 bash / python 等脚本执行"
      />
      <u-input label="工作目录" field="work_dir" placeholder="相对仓库根" />
      <u-input label="输出目录" field="output_dir" />
      <u-input label="环境变量名" field="env_var_names" placeholder="逗号分隔，仅名称" />

      <u-form-item label="触发方式">
        <div class="trigger-row">
          <u-checkbox v-model="form.trigger_manual">手动</u-checkbox>
          <u-checkbox v-model="form.trigger_webhook">Webhook</u-checkbox>
          <u-checkbox v-model="form.trigger_cron">Cron</u-checkbox>
        </div>
      </u-form-item>
      <template v-if="form.trigger_cron">
        <u-input label="Cron 表达式" field="cron_expression" placeholder="如 0 */6 * * *" />
        <u-input label="时区" field="cron_timezone" placeholder="IANA，如 Asia/Shanghai" />
      </template>
      <template v-if="form.trigger_webhook">
        <u-input label="Webhook 类型" field="webhook_type" placeholder="auto / github / generic" />
        <u-input label="Ref JSONPath" field="webhook_ref_path" placeholder="generic 平台可选" />
        <u-input label="Commit JSONPath" field="webhook_commit_path" />
        <u-input label="Message JSONPath" field="webhook_message_path" />
      </template>

      <u-number-input label="制品保留" field="max_artifacts" />
      <u-select label="制品格式" field="artifact_format" :options="ARTIFACT_OPTIONS" />
      <u-select
        label="Agent 事件"
        field="agent_trigger_event"
        :options="[
          { label: 'artifact_ready（默认）', value: 'artifact_ready' },
          { label: 'distribution_finished', value: 'distribution_finished' },
          { label: 'none（不触发）', value: 'none' },
        ]"
      />
      <u-number-input label="绑定 Agent ID" field="agent_id" placeholder="可选" />

      <div class="targets-head">
        <strong>部署目标（Job 私有）</strong>
        <u-button size="small" @click="addTarget">添加</u-button>
      </div>
      <div v-for="(t, idx) in form.deploy_targets" :key="idx" class="target-block">
        <div class="target-row">
          <u-select v-model="t.method" :options="METHOD_OPTIONS" style="width: 110px" />
          <u-select
            v-if="t.method !== 'local'"
            v-model="t.server_id"
            :options="serverOptions"
            placeholder="服务器"
            style="width: 200px"
          />
          <u-input v-model="t.remote_path" placeholder="远程路径" style="flex: 1" />
          <u-button size="small" @click="removeTarget(idx)">删</u-button>
        </div>
        <u-textarea
          v-model="t.post_deploy_script"
          :rows="2"
          placeholder="部署后脚本（可选）"
          class="post-script"
        />
      </div>
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

<style scoped>
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
.targets-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 12px 0 8px;
}
.target-block {
  margin-bottom: 12px;
  padding: 8px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 6px;
}
.target-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.post-script {
  margin-top: 8px;
  width: 100%;
}
.script-tip {
  margin: -4px 0 8px 110px;
  font-size: 12px;
  color: rgba(0, 0, 0, 0.55);
  line-height: 1.5;
}
.script-tip code {
  font-size: 11px;
}
</style>
