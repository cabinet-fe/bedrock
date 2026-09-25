<script setup lang="ts">
defineOptions({ name: "SystemBackup" });

import { computed, reactive, ref, useTemplateRef } from "vue";
import { saveBlob } from "@cat-kit/fe";
import { message } from "@veltra/desktop";
import { CirclePlus, Download, Upload } from "@veltra/icons/normal";

import {
  createBackup,
  deleteBackup,
  downloadBackup,
  inspectBackupFile,
  restoreBackup,
} from "@/api/system";
import type { BackupInspectResult, BackupModule, BackupStatus, SystemBackup } from "@/api/types";
import ProTable, { defineProTableColumns, type ProTableQuery } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import type { TagType } from "@/lib/tag";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();

const tableRef = useTemplateRef<{ reload: () => Promise<void> }>("table");
const fileInputRef = useTemplateRef<HTMLInputElement>("fileInput");

const query = reactive<ProTableQuery>({
  sort: "created_at@desc",
});

const MODULE_CONFIG: Record<string, { label: string; type: TagType }> = {
  database: { label: "数据库", type: "primary" },
  config: { label: "配置", type: "info" },
  storage: { label: "附件", type: "warning" },
  artifacts: { label: "制品", type: "info" },
  logs: { label: "日志", type: undefined },
};

const STATUS_CONFIG: Record<BackupStatus, { label: string; type: TagType }> = {
  success: { label: "成功", type: "success" },
  processing: { label: "处理中", type: "primary" },
  failed: { label: "失败", type: "danger" },
};

function formatBytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

const columns = defineProTableColumns([
  { key: "filename", name: "文件名 / 标识", minWidth: 240 },
  { key: "note", name: "备份备注", minWidth: 160 },
  {
    key: "file_size",
    name: "文件大小",
    width: 120,
    align: "right",
    render: ({ val }) => formatBytes(Number(val) || 0),
  },
  { key: "modules", name: "包含模块", minWidth: 220 },
  { key: "status", name: "状态", width: 100, align: "center" },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 240, align: "center", fixed: "right" },
]);

// --------------------------- One-click backup ---------------------------
const createDialogOpen = ref(false);
const creating = ref(false);
const createForm = reactive({
  modules: {
    database: true,
    config: true,
    storage: false,
    artifacts: false,
    logs: false,
  } as Record<BackupModule, boolean>,
  note: "",
});

const selectedModulesCount = computed(
  () => Object.values(createForm.modules).filter(Boolean).length,
);

function openCreateDialog() {
  createForm.modules.database = true;
  createForm.modules.config = true;
  createForm.modules.storage = false;
  createForm.modules.artifacts = false;
  createForm.modules.logs = false;
  createForm.note = "";
  createDialogOpen.value = true;
}

async function handleCreateSubmit() {
  if (selectedModulesCount.value === 0) {
    message.error("请至少选择一项备份模块");
    return;
  }
  creating.value = true;
  try {
    const modules = (Object.keys(createForm.modules) as BackupModule[]).filter(
      (k) => createForm.modules[k],
    );
    await createBackup({
      modules,
      note: createForm.note.trim() || undefined,
    });
    message.success("备份任务已提交并生成");
    createDialogOpen.value = false;
    await tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "创建备份失败");
  } finally {
    creating.value = false;
  }
}

// --------------------------- Online restore ---------------------------
const restoreDialogOpen = ref(false);
const restoreSuccessOpen = ref(false);
const restoreMode = ref<"record" | "upload">("record");
const selectedRecord = ref<SystemBackup | null>(null);
const inspectResult = ref<BackupInspectResult | null>(null);
const inspecting = ref(false);
const restoring = ref(false);
const adminPassword = ref("");
const autoSnapshot = ref(true);

function openRestoreRecord(row: SystemBackup) {
  restoreMode.value = "record";
  selectedRecord.value = row;
  inspectResult.value = null;
  adminPassword.value = "";
  autoSnapshot.value = true;
  restoreDialogOpen.value = true;
}

function triggerUploadRestore() {
  fileInputRef.value?.click();
}

async function handleFileChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const file = target.files?.[0];
  if (!file) return;

  inspecting.value = true;
  try {
    const res = await inspectBackupFile(file);
    inspectResult.value = res;
    restoreMode.value = "upload";
    selectedRecord.value = null;
    adminPassword.value = "";
    autoSnapshot.value = true;
    restoreDialogOpen.value = true;
    if (!res.compatible) {
      message.warning(res.message || "该备份包与当前系统环境不匹配，无法直接恢复");
    }
  } catch (err) {
    message.error(err instanceof Error ? err.message : "备份文件解析预检失败");
  } finally {
    inspecting.value = false;
    target.value = "";
  }
}

async function handleRestoreSubmit() {
  if (!adminPassword.value.trim()) {
    message.error("请输入当前管理员密码进行核验");
    return;
  }
  if (restoreMode.value === "upload" && !inspectResult.value?.compatible) {
    message.error(inspectResult.value?.message || "备份包与当前系统不兼容，禁止恢复");
    return;
  }

  restoring.value = true;
  try {
    await restoreBackup({
      backup_id: restoreMode.value === "record" ? selectedRecord.value?.id : undefined,
      upload_token: restoreMode.value === "upload" ? inspectResult.value?.upload_token : undefined,
      admin_password: adminPassword.value,
      auto_snapshot: autoSnapshot.value,
    });
    restoreDialogOpen.value = false;
    restoreSuccessOpen.value = true;
    message.success("系统恢复成功完成");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "系统恢复失败");
  } finally {
    restoring.value = false;
  }
}

function handleReloadPage() {
  window.location.reload();
}

// --------------------------- Download & delete ---------------------------
const onDownload = bind(async (row: SystemBackup) => {
  try {
    const blob = await downloadBackup(row.id);
    saveBlob(blob, row.filename);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "下载备份包失败");
  }
});

const remove = bind(async (row: SystemBackup) => {
  try {
    await deleteBackup(row.id);
    message.success("备份记录与物理文件已删除");
    await tableRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="table" url="/system/backups" :columns="columns" :query="query" pagination>
      <template #toolbar>
        <u-button
          v-if="hasPermission('system_backup:create')"
          type="primary"
          :icon="CirclePlus"
          @click.prevent="openCreateDialog"
        >
          一键备份
        </u-button>
        <u-button
          v-if="hasPermission('system_backup:restore')"
          type="warning"
          :icon="Upload"
          :loading="inspecting"
          @click.prevent="triggerUploadRestore"
        >
          上传恢复
        </u-button>
        <input
          ref="fileInput"
          type="file"
          accept=".zip"
          style="display: none"
          @change="handleFileChange"
        />
      </template>

      <template #column:modules="{ rowData }">
        <div class="module-tags">
          <u-tag
            v-for="mod in (rowData as SystemBackup).modules ?? []"
            :key="mod"
            size="small"
            :type="MODULE_CONFIG[mod]?.type"
          >
            {{ MODULE_CONFIG[mod]?.label ?? mod }}
          </u-tag>
        </div>
      </template>

      <template #column:status="{ rowData }">
        <u-tag size="small" :type="STATUS_CONFIG[(rowData as SystemBackup).status]?.type">
          {{
            STATUS_CONFIG[(rowData as SystemBackup).status]?.label ??
            (rowData as SystemBackup).status
          }}
        </u-tag>
      </template>

      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as SystemBackup).id">
          <u-action
            v-if="hasPermission('system_backup:download')"
            :disabled="(rowData as SystemBackup).status !== 'success'"
            :icon="Download"
            @run="onDownload(rowData as SystemBackup)"
          >
            下载
          </u-action>
          <u-action
            v-if="hasPermission('system_backup:restore')"
            :disabled="(rowData as SystemBackup).status !== 'success'"
            type="warning"
            @run="openRestoreRecord(rowData as SystemBackup)"
          >
            恢复
          </u-action>
          <u-action
            v-if="hasPermission('system_backup:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as SystemBackup)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <!-- 一键备份对话框 -->
    <u-dialog v-model="createDialogOpen" title="一键创建系统备份" style="width: 520px">
      <div class="dialog-body">
        <div class="section-title">选择备份模块（至少勾选 1 项）：</div>
        <div class="checkbox-list">
          <u-checkbox v-model="createForm.modules.database">
            数据库（核心业务表结构与数据）
          </u-checkbox>
          <u-checkbox v-model="createForm.modules.config">
            系统配置（config.yaml 配置文件）
          </u-checkbox>
          <u-checkbox v-model="createForm.modules.storage">
            存储附件（文件附件与业务上传资源）
          </u-checkbox>
          <u-checkbox v-model="createForm.modules.artifacts">
            构建制品（CI/CD 构建物产物目录）
          </u-checkbox>
          <u-checkbox v-model="createForm.modules.logs">
            运行日志（历史构建与执行日志）
          </u-checkbox>
        </div>

        <div class="section-title" style="margin-top: 16px">备份备注（可选）：</div>
        <u-input
          v-model="createForm.note"
          placeholder="如：升级前系统快照、日常定期归档"
          maxlength="200"
        />
      </div>

      <template #footer="{ close }">
        <u-button text :disabled="creating" @click="close">取消</u-button>
        <u-button
          type="primary"
          :loading="creating"
          :disabled="selectedModulesCount === 0"
          @click="handleCreateSubmit"
        >
          开始备份
        </u-button>
      </template>
    </u-dialog>

    <!-- 在线恢复高危确认对话框 -->
    <u-dialog v-model="restoreDialogOpen" title="系统在线恢复确认" style="width: 560px">
      <div class="dialog-body">
        <div class="danger-banner">
          <div class="danger-title">⚠️ 高危破坏性操作警告</div>
          <div class="danger-text">
            系统恢复将使用备份包中的数据完整覆盖现有数据库与对应文件模块！恢复后当前未备份的数据将不可逆丢失，请务必谨慎确认。
          </div>
        </div>

        <div class="meta-card">
          <div class="meta-row">
            <span class="meta-label">恢复模式：</span>
            <span class="meta-val font-semibold">
              {{ restoreMode === "record" ? "从历史备份记录恢复" : "上传本地备份包恢复" }}
            </span>
          </div>

          <template v-if="restoreMode === 'record' && selectedRecord">
            <div class="meta-row">
              <span class="meta-label">备份包文件：</span>
              <span class="meta-val font-mono">{{ selectedRecord.filename }}</span>
            </div>
            <div class="meta-row">
              <span class="meta-label">文件大小：</span>
              <span class="meta-val">{{ formatBytes(selectedRecord.file_size) }}</span>
            </div>
            <div class="meta-row">
              <span class="meta-label">生成时间：</span>
              <span class="meta-val">{{ formatDateTime(selectedRecord.created_at) }}</span>
            </div>
            <div class="meta-row">
              <span class="meta-label">包含模块：</span>
              <div class="meta-val module-tags">
                <u-tag
                  v-for="mod in selectedRecord.modules"
                  :key="mod"
                  size="small"
                  :type="MODULE_CONFIG[mod]?.type"
                >
                  {{ MODULE_CONFIG[mod]?.label ?? mod }}
                </u-tag>
              </div>
            </div>
            <div v-if="selectedRecord.note" class="meta-row">
              <span class="meta-label">备份备注：</span>
              <span class="meta-val">{{ selectedRecord.note }}</span>
            </div>
          </template>

          <template v-else-if="restoreMode === 'upload' && inspectResult">
            <div class="meta-row">
              <span class="meta-label">清单版本：</span>
              <span class="meta-val">{{ inspectResult.manifest.version || "1.0" }}</span>
            </div>
            <div class="meta-row">
              <span class="meta-label">数据库引擎：</span>
              <span class="meta-val font-mono">{{ inspectResult.manifest.db_driver }}</span>
            </div>
            <div class="meta-row">
              <span class="meta-label">兼容性预检：</span>
              <span class="meta-val">
                <u-tag size="small" :type="inspectResult.compatible ? 'success' : 'danger'">
                  {{ inspectResult.compatible ? "环境匹配，可恢复" : "环境不兼容" }}
                </u-tag>
              </span>
            </div>
            <div v-if="!inspectResult.compatible && inspectResult.message" class="error-text">
              {{ inspectResult.message }}
            </div>
            <div class="meta-row">
              <span class="meta-label">包含模块：</span>
              <div class="meta-val module-tags">
                <u-tag
                  v-for="mod in inspectResult.manifest.modules ?? []"
                  :key="mod"
                  size="small"
                  :type="MODULE_CONFIG[mod]?.type"
                >
                  {{ MODULE_CONFIG[mod]?.label ?? mod }}
                </u-tag>
              </div>
            </div>
            <div v-if="inspectResult.manifest.note" class="meta-row">
              <span class="meta-label">备份备注：</span>
              <span class="meta-val">{{ inspectResult.manifest.note }}</span>
            </div>
          </template>
        </div>

        <div class="field-item" style="margin-top: 16px">
          <div class="field-label"><span class="required">*</span> 当前管理员登录密码核验：</div>
          <u-input
            v-model="adminPassword"
            type="password"
            placeholder="请输入当前登录管理员密码进行二次核验"
            autocomplete="current-password"
          />
        </div>

        <div class="field-item" style="margin-top: 14px">
          <u-checkbox v-model="autoSnapshot">
            还原前自动创建当前数据快照（推荐勾选，提供安全回滚兜底）
          </u-checkbox>
        </div>
      </div>

      <template #footer="{ close }">
        <u-button text :disabled="restoring" @click="close">取消</u-button>
        <u-button
          type="danger"
          :loading="restoring"
          :disabled="
            !adminPassword.trim() || (restoreMode === 'upload' && !inspectResult?.compatible)
          "
          @click="handleRestoreSubmit"
        >
          确认全量还原
        </u-button>
      </template>
    </u-dialog>

    <!-- 恢复成功引导刷新对话框 -->
    <u-dialog
      v-model="restoreSuccessOpen"
      title="系统数据恢复成功"
      style="width: 440px"
      :closable="false"
      :mask-closable="false"
    >
      <div class="success-body">
        <div class="success-icon">✓</div>
        <div class="success-title">数据恢复已完成</div>
        <div class="success-desc">
          系统数据已成功覆盖还原，数据库连接池与服务状态已重置。为了加载最新系统数据，请立即刷新页面。
        </div>
      </div>
      <template #footer>
        <u-button type="primary" style="width: 100%" @click="handleReloadPage">
          立即刷新页面
        </u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
.module-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.dialog-body {
  padding: 8px 0;
}

.section-title {
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 8px;
  color: var(--u-text-primary, #1e293b);
}

.checkbox-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.danger-banner {
  background-color: var(--u-danger-light, #fef2f2);
  border: 1px solid var(--u-danger-border, #fecaca);
  border-radius: 6px;
  padding: 12px;
  margin-bottom: 16px;

  .danger-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--u-danger, #ef4444);
    margin-bottom: 4px;
  }

  .danger-text {
    font-size: 13px;
    line-height: 1.5;
    color: var(--u-danger-dark, #b91c1c);
  }
}

.meta-card {
  background-color: var(--u-fill-2, #f8fafc);
  border-radius: 6px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 13px;

  .meta-row {
    display: flex;
    align-items: center;

    .meta-label {
      width: 100px;
      flex-shrink: 0;
      color: var(--u-text-secondary, #64748b);
    }

    .meta-val {
      color: var(--u-text-primary, #0f172a);
      word-break: break-all;
    }
  }

  .error-text {
    color: var(--u-danger, #ef4444);
    font-size: 12px;
    padding-left: 100px;
  }
}

.field-item {
  .field-label {
    font-size: 13px;
    font-weight: 500;
    margin-bottom: 6px;
    color: var(--u-text-primary, #1e293b);

    .required {
      color: var(--u-danger, #ef4444);
    }
  }
}

.font-mono {
  font-family: monospace;
}

.font-semibold {
  font-weight: 600;
}

.success-body {
  text-align: center;
  padding: 16px 8px;

  .success-icon {
    width: 48px;
    height: 48px;
    line-height: 48px;
    border-radius: 50%;
    background-color: var(--u-success-light, #ecfdf5);
    color: var(--u-success, #10b981);
    font-size: 24px;
    font-weight: bold;
    margin: 0 auto 12px;
  }

  .success-title {
    font-size: 16px;
    font-weight: 600;
    color: var(--u-text-primary, #0f172a);
    margin-bottom: 8px;
  }

  .success-desc {
    font-size: 13px;
    line-height: 1.5;
    color: var(--u-text-secondary, #64748b);
  }
}
</style>
