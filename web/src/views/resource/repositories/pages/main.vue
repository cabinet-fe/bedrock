<script setup lang="ts">
defineOptions({ name: "ResourceRepositories" });

import { onMounted, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  createRepository,
  deleteRepository,
  listCredentials,
  syncRepositoryBranches,
  syncRepositoryBranchesBatch,
  testRepository,
  updateRepository,
} from "@/api/resource";
import type { Credential, Repository } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import { tagType, type TagType } from "@/lib/tag";

const AUTH_TYPE_TAG: Record<string, TagType> = {
  none: undefined,
  credential: "info",
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: batchBusy, run: runBatch } = useBusy();
const listRef = useTemplateRef("list");
const query = reactive({ keyword: "" });
const checked = ref<Repository[]>([]);
const dialogOpen = ref(false);
const editing = ref<Repository | null>(null);
const credOptions = ref<{ label: string; value: number }[]>([]);
const form = reactive({
  name: "",
  repo_url: "",
  description: "",
  tags: "",
  auth_type: "none",
  credential_id: undefined as number | undefined,
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "repo_url", name: "URL" },
  { key: "auth_type", name: "认证", width: 100, align: "center" },
  { key: "branches", name: "分支数" },
  { key: "branches_synced_at", name: "分支同步", width: 170, align: "center" },
  { key: "action", name: "操作", width: 360, align: "center", fixed: "right" },
]);

onMounted(async () => {
  if (hasPermission("resource_credentials:view") || hasPermission("resource_credentials:use")) {
    try {
      const res = await listCredentials({ page: 1, page_size: 100 });
      credOptions.value = (res.items ?? []).map((c: Credential) => ({
        label: `${c.name} (${c.type})`,
        value: c.id,
      }));
    } catch {
      /* ignore */
    }
  }
});

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(row: Repository) {
  editing.value = row;
  o(form).extend(row);
  dialogOpen.value = true;
}

async function save() {
  try {
    const body: Record<string, unknown> = { ...form };
    if (form.auth_type !== "credential" || !form.credential_id) {
      delete body.credential_id;
    }
    if (editing.value) {
      if (form.auth_type === "none") body.clear_credential = true;
      await updateRepository(editing.value.id, body);
      message.success("已更新");
    } else {
      await createRepository(body);
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Repository) => {
  try {
    await deleteRepository(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});

const onTest = bind(async (row: Repository) => {
  try {
    const res = await testRepository(row.id);
    message.success(`拉取成功，分支 ${res.branches?.length ?? 0} 个`);
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "测试失败");
  }
});

const onSyncBranches = bind(async (row: Repository) => {
  try {
    const res = await syncRepositoryBranches(row.id);
    message.success(`已同步 ${res.items.length} 个分支`);
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "同步失败");
  }
});

async function onBatchSyncBranches() {
  if (!checked.value.length) {
    message.warning("请先勾选要同步的仓库");
    return;
  }
  await runBatch(async () => {
    try {
      const results = await syncRepositoryBranchesBatch(checked.value.map((r) => r.id));
      const ok = results.filter((r) => r.ok).length;
      const fail = results.length - ok;
      if (fail === 0) {
        message.success(`已同步 ${ok} 个仓库`);
      } else {
        message.warning(`同步完成：成功 ${ok}，失败 ${fail}`);
      }
      checked.value = [];
      await listRef.value?.reload();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "批量同步失败");
    }
  });
}
</script>

<template>
  <div>
    <ProTable
      ref="list"
      v-model:checked="checked"
      url="/resource/repositories"
      :query="query"
      :columns="columns"
      checkable
      pagination
    >
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称/URL" style="width: 200px" />
        <u-button
          v-if="hasPermission('resource_repositories:update')"
          :loading="batchBusy"
          @click.prevent="onBatchSyncBranches"
        >
          批量同步分支
        </u-button>
        <u-button
          v-if="hasPermission('resource_repositories:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建仓库
        </u-button>
      </template>
      <template #column:auth_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Repository).auth_type, AUTH_TYPE_TAG)">
          {{ (rowData as Repository).auth_type === "credential" ? "凭证" : "无" }}
        </u-tag>
      </template>
      <template #column:branches="{ rowData }">
        {{ (rowData as Repository).branches?.length ?? 0 }}
      </template>
      <template #column:branches_synced_at="{ rowData }">
        {{ formatDateTime((rowData as Repository).branches_synced_at) || "—" }}
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="4" :loading="busyKey === (rowData as Repository).id">
          <u-action
            v-if="hasPermission('resource_repositories:update')"
            @run="openEdit(rowData as Repository)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('resource_repositories:update')"
            @run="onSyncBranches(rowData as Repository)"
          >
            同步分支
          </u-action>
          <u-action
            v-if="hasPermission('resource_repositories:view')"
            @run="onTest(rowData as Repository)"
          >
            测试
          </u-action>
          <u-action
            v-if="hasPermission('resource_repositories:delete')"
            @run="remove(rowData as Repository)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑仓库' : '新建仓库'"
      :model="form"
      label-width="110px"
      style="width: 560px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="Git URL" field="repo_url" :rules="{ required: '必填' }" />
      <u-select
        label="认证"
        field="auth_type"
        :options="[
          { label: '无', value: 'none' },
          { label: '凭证', value: 'credential' },
        ]"
      />
      <u-select
        v-if="form.auth_type === 'credential'"
        label="凭证"
        field="credential_id"
        :options="credOptions"
      />
      <u-input label="标签" field="tags" />
      <u-input label="描述" field="description" />
    </FormDialog>
  </div>
</template>
