<script setup lang="ts">
defineOptions({ name: "AgentCliSection" });

import { computed, onMounted, reactive, ref } from "vue";
import { o } from "@cat-kit/core";
import { message, messageConfirm } from "@veltra/desktop";

import {
  checkCLIUpdate,
  createCLISource,
  deleteCLISource,
  detectCLI,
  executeCLI,
  listCLIs,
  listCLISources,
  updateCLISource,
} from "@/api/resource";
import type { CliInstallSource, CliRuntimeDefinition } from "@/api/types";
import FormDialog from "@/components/form-dialog";

type DetectState = {
  status: "loading" | "detected" | "missing" | "error";
  version?: string;
};

type Operation = "install" | "upgrade" | "uninstall" | "check";
type VersionOperation = "install" | "upgrade";

const loading = ref(false);
const items = ref<CliRuntimeDefinition[]>([]);
const riskNotice = ref("");
const detectStates = ref<Record<string, DetectState>>({});
const busy = ref<{ key: string; op: Operation } | null>(null);

const sourcesDialogOpen = ref(false);
const sourcesCli = ref<CliRuntimeDefinition | null>(null);
const sourcesList = ref<CliInstallSource[]>([]);
const sourcesLoading = ref(false);

const sourceDialogOpen = ref(false);
const editingSource = ref<CliInstallSource | null>(null);
const sourceForm = reactive({
  name: "",
  base_url: "",
  priority: 10,
  enabled: true,
});

const versionDialogOpen = ref(false);
const versionForm = reactive({ version: "" });
const pendingVersionOp = ref<{
  item: CliRuntimeDefinition;
  operation: VersionOperation;
} | null>(null);

const failureDialogOpen = ref(false);
const failureTitle = ref("");
const failureDetail = ref("");

const versionDialogTitle = computed(() => {
  const pending = pendingVersionOp.value;
  if (!pending) return "目标版本";
  return pending.operation === "install"
    ? `安装 ${pending.item.name}`
    : `升级 ${pending.item.name}`;
});

function showError(error: unknown) {
  message.error(error instanceof Error ? error.message : "操作失败");
}

function formatVersion(version?: string) {
  const trimmed = version?.trim();
  if (!trimmed || trimmed.includes("/") || trimmed.includes("\\")) return "";
  return trimmed;
}

function versionTagType(state?: DetectState) {
  if (state?.status === "detected") return "success";
  if (state?.status === "missing") return "warning";
  if (state?.status === "error") return "danger";
  return "info";
}

function versionTagLabel(state?: DetectState) {
  if (!state || state.status === "loading") return "检测中…";
  if (state.status === "detected") return formatVersion(state.version) || "已安装";
  if (state.status === "missing") return "未安装";
  return "检测失败";
}

function isBusy(key: string, op?: Operation) {
  if (!busy.value || busy.value.key !== key) return false;
  return op ? busy.value.op === op : true;
}

async function reload() {
  loading.value = true;
  try {
    const data = await listCLIs();
    items.value = data.items ?? [];
    riskNotice.value = data.risk_notice ?? "";
    void Promise.all(items.value.map((item) => runDetect(item)));
  } catch (error) {
    showError(error);
  } finally {
    loading.value = false;
  }
}

async function runDetect(item: CliRuntimeDefinition) {
  detectStates.value[item.key] = {
    status: "loading",
    version: detectStates.value[item.key]?.version,
  };
  try {
    const result = await detectCLI(item.key);
    detectStates.value[item.key] = result.detected
      ? { status: "detected", version: formatVersion(result.version) || undefined }
      : { status: "missing" };
  } catch {
    detectStates.value[item.key] = { status: "error" };
  }
}

async function runCheckUpdate(item: CliRuntimeDefinition) {
  if (isBusy(item.key)) return;
  busy.value = { key: item.key, op: "check" };
  try {
    const result = await checkCLIUpdate(item.key);
    const latest = formatVersion(result.latest_version);
    if (result.error) {
      message.error(result.error);
      return;
    }
    if (result.update_available && latest) {
      const action = await messageConfirm.warning(`${item.name} 有新版本 ${latest}，是否更新？`, {
        cancelButtonText: "取消",
      }).onClosed;
      if (action === "confirm") {
        busy.value = null;
        await runOperation(item, "upgrade", latest);
      }
    } else {
      message.success(`${item.name} 已是最新版本`);
    }
  } catch (error) {
    showError(error);
  } finally {
    if (busy.value?.op === "check") busy.value = null;
  }
}

async function runOperation(
  item: CliRuntimeDefinition,
  operation: "install" | "upgrade" | "uninstall",
  version = "",
) {
  if (isBusy(item.key)) return;

  if (operation === "uninstall") {
    const action = await messageConfirm.danger(`确认卸载 ${item.name}？`, {
      cancelButtonText: "取消",
    }).onClosed;
    if (action !== "confirm") return;
    await executeCliOperation(item, operation, "");
    return;
  }

  if (!version) {
    pendingVersionOp.value = { item, operation };
    versionForm.version = "";
    versionDialogOpen.value = true;
    return;
  }

  await executeCliOperation(item, operation, version);
}

async function submitVersion() {
  const pending = pendingVersionOp.value;
  if (!pending) return;
  const { item, operation } = pending;
  const targetVersion = versionForm.version;
  versionDialogOpen.value = false;
  pendingVersionOp.value = null;
  void executeCliOperation(item, operation, targetVersion);
}

async function executeCliOperation(
  item: CliRuntimeDefinition,
  operation: "install" | "upgrade" | "uninstall",
  targetVersion: string,
) {
  busy.value = { key: item.key, op: operation };
  try {
    const result = await executeCLI(item.key, operation, targetVersion);
    if (result.success) {
      message.success(
        operation === "upgrade" && targetVersion
          ? `${item.name} 已更新到 ${targetVersion}`
          : `${item.name} ${operation} 完成`,
      );
      await runDetect(item);
      return;
    }
    failureTitle.value = `${item.name} · ${operation} 失败`;
    const parts = [result.output?.trim(), result.error?.trim()].filter(Boolean);
    failureDetail.value = parts.join("\n\n") || "无输出";
    failureDialogOpen.value = true;
  } catch (error) {
    showError(error);
  } finally {
    busy.value = null;
  }
}

async function loadSources() {
  if (!sourcesCli.value) return;
  sourcesLoading.value = true;
  try {
    sourcesList.value = await listCLISources(sourcesCli.value.key);
  } catch (error) {
    showError(error);
    sourcesList.value = [];
  } finally {
    sourcesLoading.value = false;
  }
}

async function openSourcesManager(item: CliRuntimeDefinition) {
  sourcesCli.value = item;
  sourcesDialogOpen.value = true;
  await loadSources();
}

function openCreateSource() {
  editingSource.value = null;
  sourceDialogOpen.value = true;
}

function openEditSource(source: CliInstallSource) {
  editingSource.value = source;
  o(sourceForm).extend(source);
  sourceDialogOpen.value = true;
}

async function saveSource() {
  if (!sourcesCli.value) return;
  try {
    if (editingSource.value) {
      await updateCLISource(editingSource.value.id, sourceForm);
      message.success("安装源已更新");
    } else {
      await createCLISource({ ...sourceForm, cli_key: sourcesCli.value.key });
      message.success("安装源已创建");
    }
    sourceDialogOpen.value = false;
    await loadSources();
  } catch (error) {
    showError(error);
  }
}

async function removeSource(source: CliInstallSource) {
  const action = await messageConfirm.danger(`删除安装源 ${source.name}？`, {
    cancelButtonText: "取消",
  }).onClosed;
  if (action !== "confirm") return;
  try {
    await deleteCLISource(source.id);
    await loadSources();
  } catch (error) {
    showError(error);
  }
}

onMounted(() => {
  void reload();
});
</script>

<template>
  <div v-loading="loading" class="agent-cli-section">
    <p class="risk">
      {{ riskNotice || "AI CLI 以 Bedrock 同 UID 执行，无 OS/容器沙箱。" }}
    </p>

    <div class="cards">
      <u-card v-for="item in items" :key="item.key" class="cli-card">
        <u-card-content>
          <header class="card-head">
            <div class="title-row">
              <h3>{{ item.name }}</h3>
              <u-tag size="small" :type="versionTagType(detectStates[item.key])">
                {{ versionTagLabel(detectStates[item.key]) }}
              </u-tag>
            </div>
            <p class="meta">
              <span>{{ item.key }}</span>
              <span>{{ item.binary_name }}</span>
            </p>
            <p v-if="item.description" class="desc">{{ item.description }}</p>
            <div class="actions">
              <u-action-group :max="5">
                <u-action @run="openSourcesManager(item)">设置</u-action>
                <u-action
                  :disabled="isBusy(item.key)"
                  :loading="isBusy(item.key, 'install')"
                  @run="runOperation(item, 'install')"
                >
                  安装
                </u-action>
                <u-action
                  :disabled="isBusy(item.key)"
                  :loading="isBusy(item.key, 'check')"
                  @run="runCheckUpdate(item)"
                >
                  检查更新
                </u-action>
                <u-action
                  :disabled="isBusy(item.key)"
                  :loading="isBusy(item.key, 'upgrade')"
                  @run="runOperation(item, 'upgrade')"
                >
                  升级
                </u-action>
                <u-action
                  :disabled="isBusy(item.key)"
                  :loading="isBusy(item.key, 'uninstall')"
                  type="danger"
                  @run="runOperation(item, 'uninstall')"
                >
                  卸载
                </u-action>
              </u-action-group>
            </div>
          </header>
        </u-card-content>
      </u-card>
    </div>

    <u-dialog
      v-model="sourcesDialogOpen"
      :title="sourcesCli ? `${sourcesCli.name} · 安装源管理` : '安装源管理'"
      style="width: 640px"
    >
      <div v-loading="sourcesLoading" class="sources-dialog">
        <div class="block-head">
          <h4>安装源列表</h4>
          <u-button size="small" text type="primary" @click="openCreateSource">添加</u-button>
        </div>
        <ul v-if="sourcesList.length" class="source-list">
          <li v-for="source in sourcesList" :key="source.id">
            <div class="source-info">
              <strong>{{ source.name }}</strong>
              <span class="source-url">{{ source.base_url }}</span>
              <span class="source-meta">
                优先级 {{ source.priority }} · {{ source.enabled ? "启用" : "停用" }}
              </span>
            </div>
            <u-action-group :max="2">
              <u-action @run="openEditSource(source)">编辑</u-action>
              <u-action type="danger" @run="removeSource(source)">删除</u-action>
            </u-action-group>
          </li>
        </ul>
        <p v-else class="empty">尚未配置安装源，安装时将使用 npm 默认 Registry</p>
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
      <u-input
        label="Registry"
        field="base_url"
        placeholder="https://registry.npmjs.org"
        :rules="{ required: '必填' }"
      />
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

    <FormDialog
      v-model="versionDialogOpen"
      :title="versionDialogTitle"
      :model="versionForm"
      confirm-text="确认"
      label-width="100px"
      style="width: 480px"
      @submit="submitVersion"
    >
      <u-input label="目标版本" field="version" placeholder="可留空，使用安装源默认版本" />
    </FormDialog>

    <u-dialog v-model="failureDialogOpen" :title="failureTitle" style="width: 760px">
      <pre class="failure-log">{{ failureDetail }}</pre>
      <template #footer="{ close }">
        <u-button type="primary" @click="close()">关闭</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.agent-cli-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.risk {
  margin: 0;
  color: fn.use-var(color, warning);
  font-size: 13px;
  line-height: 1.5;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(420px, 1fr));
  gap: 16px;
  align-items: start;
}
.cli-card {
  min-width: 0;
}
.card-head {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.title-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.card-head h3 {
  margin: 0;
  font-size: 16px;
  line-height: 1.4;
}
.actions {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.meta,
.desc,
.empty,
.source-url,
.source-meta {
  margin: 4px 0 0;
  color: fn.use-var(text-color, second);
  font-size: 13px;
  line-height: 1.4;
}
.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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
.failure-log {
  max-height: 55vh;
  margin: 0;
  padding: 12px;
  overflow: auto;
  border-radius: fn.use-var(radius, small);
  color: fn.use-var(text-color, main);
  background: fn.use-var(bg-color, bottom);
  white-space: pre-wrap;
}
</style>
