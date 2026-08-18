<script setup lang="ts">
defineOptions({ name: "OpsDevEnvironments" });

import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { o } from "@cat-kit/core";
import { message, messageConfirm } from "@veltra/desktop";

import {
  createDevEnvSource,
  createDevEnvironment,
  deleteDevEnvSource,
  deleteDevEnvironment,
  detectDevEnvironment,
  enqueueDevEnvironmentOperation,
  getDevEnvJob,
  getDevEnvJobLogs,
  listDevEnvJobs,
  listDevEnvironments,
  listDevEnvVersions,
  pingDevEnvSource,
  retryDevEnvJob,
  updateDevEnvSource,
  updateDevEnvironment,
} from "@/api/ops";
import type { DevEnvInstallSource, DevEnvJob, DevEnvironment, VersionCatalog } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { usePermission } from "@/composables/use-permission";

import AgentCliSection from "../components/agent-cli-section.vue";
import EnvCard, { type DetectState } from "../components/env-card.vue";
import VersionPickDialog from "../components/version-pick-dialog.vue";

const { hasPermission } = usePermission();

const loading = ref(false);
const environments = ref<DevEnvironment[]>([]);
const latestJobs = ref<Record<number, DevEnvJob | undefined>>({});
const detectStates = ref<Record<number, DetectState>>({});

const envDialogOpen = ref(false);
const editingEnv = ref<DevEnvironment | null>(null);
const scriptsDialogOpen = ref(false);
const scriptsEnv = ref<DevEnvironment | null>(null);
const sourcesDialogOpen = ref(false);
const sourcesEnvId = ref<number | null>(null);
const sourceDialogOpen = ref(false);
const editingSource = ref<DevEnvInstallSource | null>(null);
const sourceEnvID = ref<number | null>(null);

const logViewerOpen = ref(false);
const jobLog = ref("");
const jobLogTitle = ref("");
const viewedJob = ref<{ envId: number; jobId: number } | null>(null);

const pendingJobs = new Map<number, number>();
let jobPollTimer: ReturnType<typeof setInterval> | undefined;

const envForm = reactive({
  name: "",
  executable: "",
  description: "",
  detect_script: "",
  install_script: "",
  upgrade_script: "",
  uninstall_script: "",
  versions_script: "",
  switch_script: "",
  default_version: "",
});

const envFormGroups = [
  { key: "basic", title: "基本信息" },
  { key: "scripts", title: "命令行脚本" },
];

const sourceForm = reactive({
  name: "",
  base_url: "",
  priority: 10,
  enabled: true,
});

type VersionOperation = "install" | "upgrade" | "switch";

const versionDialogOpen = ref(false);
const versionInitial = ref("");
const pendingVersionOp = ref<{ item: DevEnvironment; operation: VersionOperation } | null>(null);

const scriptsReadOnly = computed(() => scriptsEnv.value?.kind === "builtin");
const sourcesEnv = computed(
  () => environments.value.find((item) => item.id === sourcesEnvId.value) ?? null,
);
const versionDialogTitle = computed(() => {
  const pending = pendingVersionOp.value;
  if (!pending) return "目标版本";
  const labels: Record<VersionOperation, string> = {
    install: "安装",
    upgrade: "升级",
    switch: "切换版本",
  };
  return `${labels[pending.operation]} ${pending.item.name}`;
});

function showError(error: unknown) {
  message.error(error instanceof Error ? error.message : "操作失败");
}

function parseDetectedVersion(output: string): string {
  const text = output.trim();
  if (!text) return "已安装";
  const firstLine =
    text
      .split(/\r?\n/)
      .find((line) => line.trim())
      ?.trim() ?? text;

  const go = firstLine.match(/\bgo(\d+\.\d+(?:\.\d+)?)\b/i);
  if (go) return go[1];
  const python = firstLine.match(/\bPython\s+(\d+\.\d+(?:\.\d+)?)\b/i);
  if (python) return python[1];
  const java = text.match(/version\s+"([^"]+)"/i);
  if (java) return java[1];
  const node = firstLine.match(/\bv?(\d+\.\d+\.\d+)\b/);
  if (node) return node[0].startsWith("v") ? node[0] : `v${node[1]}`;
  const generic = firstLine.match(/\b(\d+\.\d+(?:\.\d+)?)\b/);
  if (generic) return generic[1];
  return firstLine.length > 48 ? `${firstLine.slice(0, 48)}…` : firstLine;
}

async function reload() {
  loading.value = true;
  try {
    const items = await listDevEnvironments();
    environments.value = items;
    await Promise.all(items.map((item) => refreshLatestJob(item.id)));
    void Promise.all(items.map((item) => runDetect(item, { silent: true })));
  } catch (error) {
    showError(error);
  } finally {
    loading.value = false;
  }
}

async function refreshLatestJob(envId: number) {
  try {
    const page = await listDevEnvJobs(envId, { page: 1, page_size: 1 });
    const job = page.items[0];
    latestJobs.value[envId] = job;
    if (job && ["queued", "running"].includes(job.status)) {
      trackJob(envId, job.id);
    }
  } catch {
    // 卡片在没有任务时仍渲染占位
  }
}

function openCreateEnv() {
  editingEnv.value = null;
  envDialogOpen.value = true;
}

function openEditEnv(item: DevEnvironment) {
  editingEnv.value = item;
  o(envForm).extend(item);
  envDialogOpen.value = true;
}

function openScripts(item: DevEnvironment) {
  scriptsEnv.value = item;
  o(envForm).extend(item);
  scriptsDialogOpen.value = true;
}

function openSourcesManager(item: DevEnvironment) {
  sourcesEnvId.value = item.id;
  sourcesDialogOpen.value = true;
}

async function saveEnv() {
  try {
    if (editingEnv.value) {
      await updateDevEnvironment(editingEnv.value.id, envForm);
      message.success("自定义开发环境已更新");
    } else {
      await createDevEnvironment(envForm);
      message.success("自定义开发环境已创建");
    }
    envDialogOpen.value = false;
    await reload();
  } catch (error) {
    showError(error);
  }
}

async function saveScripts() {
  if (!scriptsEnv.value || scriptsEnv.value.kind !== "custom") {
    scriptsDialogOpen.value = false;
    return;
  }
  try {
    await updateDevEnvironment(scriptsEnv.value.id, envForm);
    message.success("命令行脚本已更新");
    scriptsDialogOpen.value = false;
    await reload();
  } catch (error) {
    showError(error);
  }
}

async function removeEnv(item: DevEnvironment) {
  const action = await messageConfirm.danger(`删除自定义开发环境 ${item.name}？`, {
    cancelButtonText: "取消",
  }).onClosed;
  if (action !== "confirm") return;
  try {
    await deleteDevEnvironment(item.id);
    message.success("已删除");
    await reload();
  } catch (error) {
    showError(error);
  }
}

async function runDetect(item: DevEnvironment, options?: { silent?: boolean }) {
  detectStates.value[item.id] = {
    status: "loading",
    version: detectStates.value[item.id]?.version,
  };
  try {
    const result = await detectDevEnvironment(item.id);
    detectStates.value[item.id] = result.detected
      ? {
          status: "detected",
          version: parseDetectedVersion(result.output),
          output: result.output,
        }
      : { status: "missing", output: result.output };
    if (!options?.silent) {
      message[result.detected ? "success" : "warning"](
        result.detected ? `已检测到 ${item.name}` : `${item.name} 未检测到`,
      );
    }
  } catch (error) {
    detectStates.value[item.id] = { status: "error" };
    if (!options?.silent) showError(error);
  }
}

async function runOperation(
  item: DevEnvironment,
  operation: "install" | "upgrade" | "uninstall" | "switch",
) {
  if (operation === "uninstall") {
    const action = await messageConfirm.danger(`确认卸载 ${item.name}？`, {
      cancelButtonText: "取消",
    }).onClosed;
    if (action !== "confirm") return;
    await enqueueOperation(item, operation, "");
    return;
  }

  pendingVersionOp.value = { item, operation };
  versionInitial.value = item.default_version ?? "";
  versionDialogOpen.value = true;
}

async function loadPendingVersions(): Promise<VersionCatalog> {
  const pending = pendingVersionOp.value;
  if (!pending) return { items: [], catalog_url: "" };
  return listDevEnvVersions(pending.item.id);
}

function submitVersion(version: string) {
  const pending = pendingVersionOp.value;
  pendingVersionOp.value = null;
  if (!pending) return;
  void enqueueOperation(pending.item, pending.operation, version);
}

async function enqueueOperation(
  item: DevEnvironment,
  operation: "install" | "upgrade" | "uninstall" | "switch",
  version: string,
) {
  try {
    const job = await enqueueDevEnvironmentOperation(item.id, operation, version);
    message.success("任务已排队");
    trackJob(item.id, job.id);
    latestJobs.value[item.id] = job;
  } catch (error) {
    showError(error);
  }
}

function openCreateSource(envId: number) {
  sourceEnvID.value = envId;
  editingSource.value = null;
  sourceDialogOpen.value = true;
}

function openEditSource(envId: number, source: DevEnvInstallSource) {
  sourceEnvID.value = envId;
  editingSource.value = source;
  o(sourceForm).extend(source);
  sourceDialogOpen.value = true;
}

async function saveSource() {
  if (sourceEnvID.value == null) return;
  try {
    if (editingSource.value) {
      await updateDevEnvSource(sourceEnvID.value, editingSource.value.id, sourceForm);
      message.success("安装源已更新");
    } else {
      await createDevEnvSource(sourceEnvID.value, sourceForm);
      message.success("安装源已创建");
    }
    sourceDialogOpen.value = false;
    await reload();
  } catch (error) {
    showError(error);
  }
}

async function removeSource(envId: number, source: DevEnvInstallSource) {
  const action = await messageConfirm.danger(`删除安装源 ${source.name}？`, {
    cancelButtonText: "取消",
  }).onClosed;
  if (action !== "confirm") return;
  try {
    await deleteDevEnvSource(envId, source.id);
    await reload();
  } catch (error) {
    showError(error);
  }
}

async function pingSource(envId: number, source: DevEnvInstallSource) {
  try {
    const result = await pingDevEnvSource(envId, source.id);
    message[result.ok ? "success" : "warning"](result.ok ? "连通性正常" : result.detail);
  } catch (error) {
    showError(error);
  }
}

async function showJobLog(envId: number, job: DevEnvJob) {
  try {
    jobLog.value = await getDevEnvJobLogs(envId, job.id);
    jobLogTitle.value = `${job.operation} #${job.id}`;
    viewedJob.value = { envId, jobId: job.id };
    logViewerOpen.value = true;
    if (["queued", "running"].includes(job.status)) trackJob(envId, job.id);
  } catch (error) {
    showError(error);
  }
}

async function retryJob(envId: number, job: DevEnvJob) {
  try {
    const next = await retryDevEnvJob(envId, job.id);
    message.success("已创建重试任务");
    trackJob(envId, next.id);
    latestJobs.value[envId] = next;
  } catch (error) {
    showError(error);
  }
}

function trackJob(envId: number, jobId: number) {
  pendingJobs.set(jobId, envId);
  ensureJobPolling();
}

function ensureJobPolling() {
  if (jobPollTimer || pendingJobs.size === 0) return;
  jobPollTimer = setInterval(() => {
    void pollPendingJobs();
  }, 2000);
  void pollPendingJobs();
}

async function pollPendingJobs() {
  if (pendingJobs.size === 0) {
    stopJobPolling();
    return;
  }
  try {
    const entries = [...pendingJobs.entries()];
    const jobs = await Promise.all(entries.map(([jobId, envId]) => getDevEnvJob(envId, jobId)));
    for (const job of jobs) {
      latestJobs.value[job.environment_id] = job;
      if (!["queued", "running"].includes(job.status)) {
        pendingJobs.delete(job.id);
        const env = environments.value.find((item) => item.id === job.environment_id);
        if (env && job.status === "success") {
          void runDetect(env, { silent: true });
        }
      }
      if (viewedJob.value?.jobId === job.id) {
        jobLog.value = await getDevEnvJobLogs(job.environment_id, job.id);
      }
    }
  } catch {
    // 轮询失败不中断监控
  }
  if (pendingJobs.size === 0) stopJobPolling();
}

function stopJobPolling() {
  if (jobPollTimer) {
    clearInterval(jobPollTimer);
    jobPollTimer = undefined;
  }
}

onMounted(() => {
  void reload();
});
onUnmounted(stopJobPolling);
</script>

<template>
  <div class="dev-env-page">
    <u-scroll class="dev-env-page__scroll">
      <section v-loading="loading" class="page-section">
        <div class="section-head">
          <h2 class="section-title">开发语言</h2>
          <u-button
            v-if="hasPermission('ops_dev_environments:create')"
            type="primary"
            @click="openCreateEnv"
          >
            新建自定义环境
          </u-button>
        </div>

        <div class="cards">
          <EnvCard
            v-for="item in environments"
            :key="item.id"
            :item
            :detect="detectStates[item.id]"
            :job="latestJobs[item.id]"
            @sources="openSourcesManager(item)"
            @detect="runDetect(item)"
            @install="runOperation(item, 'install')"
            @upgrade="runOperation(item, 'upgrade')"
            @uninstall="runOperation(item, 'uninstall')"
            @switch="runOperation(item, 'switch')"
            @scripts="openScripts(item)"
            @edit="openEditEnv(item)"
            @remove="removeEnv(item)"
            @log="latestJobs[item.id] && showJobLog(item.id, latestJobs[item.id]!)"
            @retry="latestJobs[item.id] && retryJob(item.id, latestJobs[item.id]!)"
          />
        </div>
      </section>

      <section class="page-section">
        <div class="section-head">
          <h2 class="section-title">智能体 CLI</h2>
        </div>
        <AgentCliSection />
      </section>
    </u-scroll>

    <FormDialog
      v-model="envDialogOpen"
      :title="editingEnv ? '编辑自定义开发环境' : '新建自定义开发环境'"
      :model="envForm"
      :groups="envFormGroups"
      label-width="120px"
      style="width: 720px"
      @submit="saveEnv"
    >
      <template #prepend>
        <div class="risk-warning">
          高风险：自定义命令会以 Bedrock 进程 UID
          直接执行，不是沙箱隔离。仅在完全理解命令及其权限影响时保存和执行。
        </div>
      </template>
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="可执行文件" field="executable" :rules="{ required: '必填' }" />
        <u-input label="描述" field="description" />
        <u-input label="默认版本" field="default_version" />
      </template>
      <template #group:scripts>
        <u-textarea label="检测脚本" field="detect_script" />
        <u-textarea label="安装脚本" field="install_script" />
        <u-textarea label="升级脚本" field="upgrade_script" />
        <u-textarea label="卸载脚本" field="uninstall_script" />
        <u-textarea label="切版本脚本" field="switch_script" />
        <u-textarea label="列版本脚本" field="versions_script" />
      </template>
    </FormDialog>

    <FormDialog
      v-model="scriptsDialogOpen"
      :title="scriptsReadOnly ? `${scriptsEnv?.name} 命令行脚本（只读）` : '编辑命令行脚本'"
      :model="envForm"
      label-width="120px"
      style="width: 760px"
      :confirm-text="scriptsReadOnly ? '关闭' : '保存'"
      @submit="saveScripts"
    >
      <template v-if="!scriptsReadOnly" #prepend>
        <div class="risk-warning">
          占位符：<span v-pre
            >{{ name }} / {{ executable }} / {{ version }} / {{ source_url }}</span
          >
        </div>
      </template>
      <u-textarea label="检测脚本" field="detect_script" :readonly="scriptsReadOnly" />
      <u-textarea label="安装脚本" field="install_script" :readonly="scriptsReadOnly" />
      <u-textarea label="升级脚本" field="upgrade_script" :readonly="scriptsReadOnly" />
      <u-textarea label="卸载脚本" field="uninstall_script" :readonly="scriptsReadOnly" />
      <u-textarea label="切版本脚本" field="switch_script" :readonly="scriptsReadOnly" />
      <u-textarea label="列版本脚本" field="versions_script" :readonly="scriptsReadOnly" />
    </FormDialog>

    <u-dialog
      v-model="sourcesDialogOpen"
      :title="sourcesEnv ? `${sourcesEnv.name} · 安装源管理` : '安装源管理'"
      style="width: 640px"
    >
      <div class="sources-dialog">
        <div class="block-head">
          <h4>安装源列表</h4>
          <u-button
            v-if="sourcesEnv"
            size="small"
            text
            type="primary"
            @click="openCreateSource(sourcesEnv.id)"
          >
            添加
          </u-button>
        </div>
        <ul v-if="sourcesEnv?.sources?.length" class="source-list">
          <li v-for="source in sourcesEnv.sources" :key="source.id">
            <div class="source-info">
              <strong>{{ source.name }}</strong>
              <span class="source-url">{{ source.base_url }}</span>
              <span class="source-meta">
                优先级 {{ source.priority }} · {{ source.enabled ? "启用" : "停用" }}
              </span>
            </div>
            <u-action-group :max="3">
              <u-action @run="pingSource(sourcesEnv.id, source)">Ping</u-action>
              <u-action @run="openEditSource(sourcesEnv.id, source)">编辑</u-action>
              <u-action type="danger" @run="removeSource(sourcesEnv.id, source)">删除</u-action>
            </u-action-group>
          </li>
        </ul>
        <p v-else class="empty">尚未配置安装源</p>
      </div>
      <template #footer="{ close }">
        <u-button type="primary" @click="close()">关闭</u-button>
      </template>
    </u-dialog>

    <FormDialog
      v-model="sourceDialogOpen"
      :title="editingSource ? '编辑安装源' : '添加安装源'"
      :model="sourceForm"
      label-width="100px"
      style="width: 560px"
      @submit="saveSource"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="地址" field="base_url" :rules="{ required: '必填' }" />
      <u-input label="优先级" field="priority" type="number" />
      <u-select
        label="启用"
        field="enabled"
        :options="[
          { label: '启用', value: true },
          { label: '停用', value: false },
        ]"
      />
    </FormDialog>

    <VersionPickDialog
      v-model="versionDialogOpen"
      :title="versionDialogTitle"
      :initial-version="versionInitial"
      :load-versions="loadPendingVersions"
      @submit="submitVersion"
    />

    <u-dialog v-model="logViewerOpen" :title="jobLogTitle" style="width: 760px">
      <u-scroll height="55vh" class="job-log-scroll">
        <pre class="job-log">{{ jobLog || "暂无日志" }}</pre>
      </u-scroll>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.dev-env-page {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}
.dev-env-page__scroll {
  flex: 1;
  min-height: 0;
  min-width: 0;

  :deep(.u-scroll__content) {
    display: flex;
    flex-direction: column;
    gap: 28px;
    min-width: 0;
  }
}
.page-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}
.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.section-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.4;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(420px, 100%), 1fr));
  gap: 16px;
  align-items: stretch;
  min-width: 0;
}
.empty,
.source-url,
.source-meta {
  margin: 4px 0 0;
  color: fn.use-var(text-color, second);
  font-size: 13px;
  line-height: 1.4;
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.block-head h4 {
  margin: 0;
  font-size: 14px;
}
.sources-dialog {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 120px;
}
.source-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.source-list li {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  padding: 10px 0;
  border-bottom: fn.use-var(border, muted);
}
.source-list li:last-child {
  border-bottom: none;
}
.source-info {
  min-width: 0;
  flex: 1;
}
.source-info strong {
  display: block;
  font-size: 13px;
}
.source-url {
  display: block;
  word-break: break-all;
}
.risk-warning {
  margin-bottom: 12px;
  padding: 10px;
  border-radius: fn.use-var(radius, small);
  color: fn.use-var(color, warning);
  background: fn.use-var(color, warning, light, 9);
  line-height: 1.6;
}
.job-log-scroll {
  border-radius: fn.use-var(radius, small);
  background: fn.use-var(bg-color, bottom);
}
.job-log {
  margin: 0;
  padding: 12px;
  color: fn.use-var(text-color, main);
  white-space: pre-wrap;
}
</style>
