<script setup lang="ts">
defineOptions({ name: "ResourceRepositories" });

import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
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
import { loadDictOptions, type DictOption } from "@/lib/dict";
import { splitCommaTags, tagType, type TagType } from "@/lib/tag";
import { useRepositoryStore } from "@/stores/repositories";

const AUTH_TYPE_TAG: Record<string, TagType> = {
  none: undefined,
  credential: "info",
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: batchBusy, run: runBatch } = useBusy();
const repoStore = useRepositoryStore();
const listRef = useTemplateRef("list");
const query = reactive({ keyword: "", tag: undefined as string | undefined });
const checked = ref<Repository[]>([]);
const dialogOpen = ref(false);
const editing = ref<Repository | null>(null);
const credOptions = ref<{ label: string; value: number }[]>([]);
const repoTypeOptions = ref<DictOption[]>([]);
const form = reactive({
  name: "",
  repo_url: "",
  description: "",
  tags: [] as string[],
  auth_type: "none",
  credential_id: undefined as number | undefined,
});

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "auth", title: "认证" },
];

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "repo_url", name: "URL" },
  { key: "tags", name: "类型", width: 180 },
  { key: "auth_type", name: "认证", width: 100, align: "center" },
  { key: "branches", name: "分支数" },
  { key: "branches_synced_at", name: "分支同步", width: 170, align: "center" },
  { key: "action", name: "操作", width: 360, align: "center", fixed: "right" },
]);

const repoTypeLabelMap = computed(() => {
  const map = new Map<string, string>();
  for (const opt of repoTypeOptions.value) map.set(opt.value, opt.label);
  return map;
});

function tagLabel(value: string): string {
  return repoTypeLabelMap.value.get(value) ?? value;
}

onMounted(async () => {
  void loadDictOptions("repo_type")
    .then((opts) => {
      repoTypeOptions.value = opts;
    })
    .catch(() => {
      /* 标签选项降级为空 */
    });
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
  o(form).extend(o(row).omit(["tags"]));
  form.tags = splitCommaTags(row.tags);
  dialogOpen.value = true;
}

async function save() {
  try {
    const body: Record<string, unknown> = { ...form, tags: form.tags.join(",") };
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
    await Promise.all([listRef.value?.reload(), repoStore.refresh()]);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Repository) => {
  try {
    await deleteRepository(row.id);
    message.success("已删除");
    await Promise.all([listRef.value?.reload(), repoStore.refresh()]);
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
    message.warn("请先勾选要同步的仓库");
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
        message.warn(`同步完成：成功 ${ok}，失败 ${fail}`);
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
      :auto-query-fields="['tag']"
      checkable
      pagination
    >
      <template #filters>
        <u-select
          v-model="query.tag"
          :options="repoTypeOptions"
          placeholder="全部类型"
          clearable
          style="width: 140px"
        />
        <u-input v-model="query.keyword" placeholder="名称/URL" style="width: 200px" />
      </template>
      <template #toolbar>
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
          @click.prevent="openCreate"
        >
          新建仓库
        </u-button>
      </template>
      <template #column:tags="{ rowData }">
        <span class="tag-cell">
          <u-tag
            v-for="tag in splitCommaTags((rowData as Repository).tags)"
            :key="tag"
            size="small"
            type="info"
          >
            {{ tagLabel(tag) }}
          </u-tag>
          <template v-if="!splitCommaTags((rowData as Repository).tags).length">—</template>
        </span>
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
      :groups="formGroups"
      label-width="110px"
      style="width: 560px"
      @submit="save"
    >
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-input label="Git URL" field="repo_url" :rules="{ required: '必填' }" />
        <u-multi-select
          label="标签"
          field="tags"
          :options="repoTypeOptions"
          placeholder="选择类型标签"
          filterable
          tips="选项来自数据字典 repo_type"
        />
        <u-input label="描述" field="description" />
      </template>
      <template #group:auth>
        <u-select
          label="认证"
          field="auth_type"
          :options="[
            { label: '无', value: 'none' },
            { label: '凭证', value: 'credential' },
          ]"
          tips="公开仓库选无；私有仓库绑定下方凭证"
        />
        <u-select
          v-if="form.auth_type === 'credential'"
          label="凭证"
          field="credential_id"
          :options="credOptions"
          tips="用于 git clone/fetch 的凭证；引用时需 credentials:use"
        />
      </template>
    </FormDialog>
  </div>
</template>

<style scoped>
.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
