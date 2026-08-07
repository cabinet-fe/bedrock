<script setup lang="ts">
defineOptions({ name: "CicdPipelines" });

import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import {
  createBuildPipeline,
  deleteBuildPipeline,
  enqueuePipelineRun,
  getBuildPipelineWebhookSecret,
  updateBuildPipeline,
} from "@/api/cicd";
import type { BuildPipeline } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";

const router = useRouter();
const { hasPermission } = usePermission();
const tableRef = ref<InstanceType<typeof ProTable> | null>(null);
const query = reactive({ keyword: "" });
const dialogOpen = ref(false);
const saving = ref(false);
const editingId = ref<number | null>(null);
const form = reactive({
  name: "",
  description: "",
  enabled: true,
  trigger_manual: true,
  trigger_webhook: false,
  trigger_cron: false,
  cron_expression: "",
  cron_timezone: "UTC",
  is_public: false,
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "enabled", name: "启用", width: 80, align: "center" },
  { key: "triggers", name: "触发" },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
]);

function triggerParts(row: BuildPipeline): string[] {
  const parts: string[] = [];
  if (row.trigger_manual) parts.push("manual");
  if (row.trigger_webhook) parts.push("webhook");
  if (row.trigger_cron) parts.push("cron");
  return parts;
}

function resetForm() {
  Object.assign(form, {
    name: "",
    description: "",
    enabled: true,
    trigger_manual: true,
    trigger_webhook: false,
    trigger_cron: false,
    cron_expression: "",
    cron_timezone: "UTC",
    is_public: false,
  });
  editingId.value = null;
}

function openCreate() {
  resetForm();
  dialogOpen.value = true;
}

function openEdit(row: BuildPipeline) {
  editingId.value = row.id;
  Object.assign(form, {
    name: row.name,
    description: row.description,
    enabled: row.enabled,
    trigger_manual: row.trigger_manual,
    trigger_webhook: row.trigger_webhook,
    trigger_cron: row.trigger_cron,
    cron_expression: row.cron_expression,
    cron_timezone: row.cron_timezone || "UTC",
    is_public: row.is_public,
  });
  dialogOpen.value = true;
}

async function save() {
  if (!form.name.trim()) {
    message.warning("请填写名称");
    return;
  }
  saving.value = true;
  try {
    const payload = { ...form };
    if (editingId.value == null) {
      const created = await createBuildPipeline({
        ...payload,
        graph_json: JSON.stringify({ nodes: [], edges: [] }),
      });
      message.success("已创建");
      dialogOpen.value = false;
      void router.push({ name: "cicd-pipeline-editor", params: { id: String(created.id) } });
    } else {
      await updateBuildPipeline(editingId.value, payload);
      message.success("已保存");
      dialogOpen.value = false;
      tableRef.value?.reload();
    }
  } catch (e) {
    message.error(e instanceof Error ? e.message : "保存失败");
  } finally {
    saving.value = false;
  }
}

async function runPipeline(row: BuildPipeline) {
  try {
    const run = await enqueuePipelineRun(row.id, { trigger_type: "manual" });
    message.success(`已触发 #${run.run_number}`);
  } catch (e) {
    message.error(e instanceof Error ? e.message : "触发失败");
  }
}

async function remove(row: BuildPipeline) {
  try {
    await deleteBuildPipeline(row.id);
    message.success("已删除");
    tableRef.value?.reload();
  } catch (e) {
    message.error(e instanceof Error ? e.message : "删除失败");
  }
}

async function showWebhook(row: BuildPipeline) {
  try {
    const secret = await getBuildPipelineWebhookSecret(row.id);
    message.info(secret.webhook_url);
  } catch (e) {
    message.error(e instanceof Error ? e.message : "获取失败");
  }
}

function openEditor(row: BuildPipeline) {
  void router.push({ name: "cicd-pipeline-editor", params: { id: String(row.id) } });
}
</script>

<template>
  <div>
    <ProTable
      ref="tableRef"
      url="/build-pipelines"
      :query="query"
      :columns="columns"
      :auto-query-fields="['keyword']"
      pagination
    >
      <template #filters>
        <u-input v-model="query.keyword" clearable placeholder="搜索" style="width: 200px" />
      </template>
      <template #toolbar>
        <u-button v-if="hasPermission('cicd_pipelines:create')" type="primary" @click="openCreate">
          新建流水线
        </u-button>
      </template>
      <template #column:enabled="{ rowData }">
        <u-tag size="small" :type="(rowData as BuildPipeline).enabled ? 'success' : undefined">
          {{ (rowData as BuildPipeline).enabled ? "是" : "否" }}
        </u-tag>
      </template>
      <template #column:triggers="{ rowData }">
        <span class="trigger-tags">
          <u-tag v-for="t in triggerParts(rowData as BuildPipeline)" :key="t" size="small">
            {{ t }}
          </u-tag>
        </span>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="4">
          <u-action
            v-if="hasPermission('cicd_pipelines:update')"
            @run="openEditor(rowData as BuildPipeline)"
          >
            编排
          </u-action>
          <u-action
            v-if="hasPermission('cicd_pipelines:update')"
            @run="openEdit(rowData as BuildPipeline)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('cicd_pipelines:execute')"
            @run="runPipeline(rowData as BuildPipeline)"
          >
            运行
          </u-action>
          <u-action
            v-if="
              hasPermission('cicd_pipelines:view') && (rowData as BuildPipeline).trigger_webhook
            "
            @run="showWebhook(rowData as BuildPipeline)"
          >
            Webhook
          </u-action>
          <u-action
            v-if="hasPermission('cicd_pipelines:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as BuildPipeline)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <u-dialog
      v-model="dialogOpen"
      :title="editingId ? '编辑流水线' : '新建流水线'"
      style="width: 720px"
    >
      <u-form label-width="100px">
        <u-form-item label="名称" required span="full">
          <u-input v-model="form.name" />
        </u-form-item>
        <u-form-item label="描述" span="full">
          <u-input v-model="form.description" type="textarea" :rows="2" />
        </u-form-item>
        <div class="switch-grid">
          <u-form-item label="启用">
            <u-switch v-model="form.enabled" />
          </u-form-item>
          <u-form-item label="公开只读">
            <u-switch v-model="form.is_public" />
          </u-form-item>
          <u-form-item label="手动触发">
            <u-switch v-model="form.trigger_manual" />
          </u-form-item>
          <u-form-item label="Webhook">
            <u-switch v-model="form.trigger_webhook" />
          </u-form-item>
          <u-form-item label="Cron">
            <u-switch v-model="form.trigger_cron" />
          </u-form-item>
        </div>
        <template v-if="form.trigger_cron">
          <u-form-item label="表达式">
            <u-input v-model="form.cron_expression" placeholder="0 2 * * *" />
          </u-form-item>
          <u-form-item label="时区">
            <u-input v-model="form.cron_timezone" placeholder="UTC" />
          </u-form-item>
        </template>
      </u-form>
      <template #footer>
        <u-button @click="dialogOpen = false">取消</u-button>
        <u-button type="primary" :loading="saving" @click="save">保存</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
.trigger-tags {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}

.switch-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 24px;
}
</style>
