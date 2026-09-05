<script setup lang="ts">
defineOptions({ name: "AiProviders" });

import { ref, useTemplateRef } from "vue";
import { message } from "@veltra/desktop";

import { deleteModel, deleteProvider } from "@/api/ai";
import type { AiModel, AiProvider } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import ModelFormDialog from "../components/model-form-dialog.vue";
import ProviderFormDialog from "../components/provider-form-dialog.vue";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busyKey: modelBusyKey, bind: bindModel } = useBusyKey();

const providerTable = useTemplateRef("providerTable");
const modelsTable = useTemplateRef("modelsTable");

const providerDialogOpen = ref(false);
const editingProvider = ref<AiProvider | null>(null);

const drawerOpen = ref(false);
const activeProvider = ref<AiProvider | null>(null);

const modelDialogOpen = ref(false);
const editingModel = ref<AiModel | null>(null);

const providerColumns = defineProTableColumns([
  { key: "name", name: "服务商名称", width: 160 },
  { key: "api_url", name: "API 地址" },
  { key: "has_api_key", name: "API Key", width: 100, align: "center" },
  { key: "enabled", name: "状态", width: 90, align: "center" },
  { key: "notes", name: "备注" },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 240, align: "center", fixed: "right" },
]);

const modelColumns = defineProTableColumns([
  { key: "name", name: "显示名称", width: 150 },
  { key: "model_id", name: "模型标识", width: 160 },
  { key: "reasoning_efforts", name: "推理档位", minWidth: 150 },
  { key: "sort_order", name: "排序", width: 80, align: "center" },
  { key: "enabled", name: "状态", width: 80, align: "center" },
  { key: "notes", name: "备注" },
  { key: "action", name: "操作", width: 150, align: "center", fixed: "right" },
]);

function openCreateProvider() {
  editingProvider.value = null;
  providerDialogOpen.value = true;
}

function openEditProvider(row: AiProvider) {
  editingProvider.value = row;
  providerDialogOpen.value = true;
}

const removeProvider = bind(async (row: AiProvider) => {
  try {
    await deleteProvider(row.id);
    message.success("服务商已删除");
    if (activeProvider.value?.id === row.id) {
      drawerOpen.value = false;
      activeProvider.value = null;
    }
    await providerTable.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除服务商失败");
  }
});

function onProviderSaved() {
  void providerTable.value?.reload();
}

function openModelsDrawer(row: AiProvider) {
  activeProvider.value = row;
  drawerOpen.value = true;
}

function openCreateModel() {
  editingModel.value = null;
  modelDialogOpen.value = true;
}

function openEditModel(row: AiModel) {
  editingModel.value = row;
  modelDialogOpen.value = true;
}

const removeModel = bindModel(async (row: AiModel) => {
  if (!activeProvider.value) return;
  try {
    await deleteModel(activeProvider.value.id, row.id);
    message.success("模型已删除");
    await modelsTable.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除模型失败");
  }
});

function onModelSaved() {
  void modelsTable.value?.reload();
}
</script>

<template>
  <div class="ai-providers-page">
    <ProTable ref="providerTable" url="/ai/providers" pagination :columns="providerColumns">
      <template #toolbar>
        <u-button
          v-if="hasPermission('ai_providers:create')"
          type="primary"
          @click.prevent="openCreateProvider"
        >
          新建服务商
        </u-button>
      </template>

      <template #column:has_api_key="{ rowData }">
        <u-tag size="small" :type="(rowData as AiProvider).has_api_key ? 'success' : 'warning'">
          {{ (rowData as AiProvider).has_api_key ? "已配置" : "未配置" }}
        </u-tag>
      </template>

      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as AiProvider).enabled ? 'success' : undefined">
          {{ (rowData as AiProvider).enabled ? "启用" : "停用" }}
        </u-tag>
      </template>

      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as AiProvider).id">
          <u-action @run="openModelsDrawer(rowData as AiProvider)"> 模型管理 </u-action>
          <u-action
            v-if="hasPermission('ai_providers:update')"
            @run="openEditProvider(rowData as AiProvider)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('ai_providers:delete')"
            need-confirm
            type="danger"
            @run="removeProvider(rowData as AiProvider)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <!-- 服务商编辑/新建弹窗 -->
    <ProviderFormDialog
      v-model="providerDialogOpen"
      :provider="editingProvider"
      @saved="onProviderSaved"
    />

    <!-- 模型抽屉 -->
    <u-drawer
      v-model="drawerOpen"
      :title="`模型管理 · ${activeProvider?.name ?? ''}`"
      show-close
      style="width: 860px"
    >
      <div v-if="activeProvider" class="models-drawer-content">
        <div class="models-drawer-header">
          <div class="provider-meta">
            <span class="meta-label">API 地址：</span>
            <code class="meta-url">{{ activeProvider.api_url }}</code>
          </div>
        </div>

        <div class="models-drawer-table">
          <ProTable
            ref="modelsTable"
            :url="`/ai/providers/${activeProvider.id}/models`"
            pagination
            :columns="modelColumns"
            height="100%"
          >
            <template #toolbar>
              <u-button
                v-if="hasPermission('ai_providers:create')"
                type="primary"
                size="small"
                @click.prevent="openCreateModel"
              >
                添加模型
              </u-button>
            </template>

            <template #column:reasoning_efforts="{ rowData }">
              <div class="tags-wrapper">
                <u-tag
                  v-for="rf in (rowData as AiModel).reasoning_efforts || []"
                  :key="rf.value"
                  size="small"
                  type="info"
                >
                  {{ rf.label || rf.value }}
                </u-tag>
                <span v-if="!(rowData as AiModel).reasoning_efforts?.length" class="empty-cell">
                  —
                </span>
              </div>
            </template>

            <template #column:enabled="{ rowData }">
              <u-tag size="small" :type="(rowData as AiModel).enabled ? 'success' : undefined">
                {{ (rowData as AiModel).enabled ? "启用" : "停用" }}
              </u-tag>
            </template>

            <template #column:action="{ rowData }">
              <u-action-group :max="2" :loading="modelBusyKey === (rowData as AiModel).id">
                <u-action
                  v-if="hasPermission('ai_providers:update')"
                  @run="openEditModel(rowData as AiModel)"
                >
                  编辑
                </u-action>
                <u-action
                  v-if="hasPermission('ai_providers:delete')"
                  need-confirm
                  type="danger"
                  @run="removeModel(rowData as AiModel)"
                >
                  删除
                </u-action>
              </u-action-group>
            </template>
          </ProTable>
        </div>
      </div>
    </u-drawer>

    <!-- 模型新建/编辑弹窗 -->
    <ModelFormDialog
      v-if="activeProvider"
      v-model="modelDialogOpen"
      :provider-id="activeProvider.id"
      :model="editingModel"
      @saved="onModelSaved"
    />
  </div>
</template>

<style scoped>
.ai-providers-page {
  height: 100%;
}

.models-drawer-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 12px;
}

.models-drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background-color: var(--u-bg-subtle, #f6f8fa);
  border-radius: 6px;
}

.provider-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.meta-label {
  color: var(--u-text-secondary, #656d76);
}

.meta-url {
  font-family: monospace;
  font-size: 12px;
}

.models-drawer-table {
  flex: 1;
  min-height: 0;
}

.tags-wrapper {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.empty-cell {
  color: var(--u-text-muted, #8c959f);
}
</style>
