<script setup lang="ts">
defineOptions({ name: "ResourceServers" });

import { onMounted, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  createServer,
  deleteServer,
  listCredentials,
  testServer,
  updateServer,
} from "@/api/resource";
import type { Credential, Server } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { tagType, type TagType } from "@/lib/tag";

const AUTH_TYPE_TAG: Record<string, TagType> = {
  password: "warning",
  key: "info",
  ssh_agent: "info",
  agent: "primary",
};

const SERVER_STATUS_TAG: Record<string, TagType> = {
  online: "success",
  offline: "danger",
  unknown: undefined,
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const listRef = useTemplateRef("list");
const query = reactive({ keyword: "" });
const dialogOpen = ref(false);
const editing = ref<Server | null>(null);
const credOptions = ref<{ label: string; value: number }[]>([]);
const form = reactive({
  name: "",
  host: "",
  port: 22,
  os_type: "linux",
  username: "",
  auth_type: "password",
  credential_id: undefined as number | undefined,
  agent_url: "",
  agent_credential_id: undefined as number | undefined,
  description: "",
  tags: "",
});

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "host", name: "主机" },
  { key: "port", name: "端口" },
  { key: "auth_type", name: "认证", width: 100, align: "center" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
]);

onMounted(async () => {
  try {
    const res = await listCredentials({ page: 1, page_size: 100 });
    credOptions.value = (res.items ?? []).map((c: Credential) => ({
      label: `${c.name} (${c.type})`,
      value: c.id,
    }));
  } catch {
    /* ignore */
  }
});

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(row: Server) {
  editing.value = row;
  o(form).extend(row);
  dialogOpen.value = true;
}

async function save() {
  try {
    const body: Record<string, unknown> = { ...form };
    if (!form.credential_id) {
      delete body.credential_id;
      if (editing.value) body.clear_credential = true;
    }
    if (!form.agent_credential_id) {
      delete body.agent_credential_id;
      if (editing.value) body.clear_agent_credential = true;
    }
    if (editing.value) {
      await updateServer(editing.value.id, body);
      message.success("已更新");
    } else {
      await createServer(body);
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Server) => {
  try {
    await deleteServer(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});

const onTest = bind(async (row: Server) => {
  try {
    const res = await testServer(row.id);
    message.success(res.output?.slice(0, 120) || "连接成功");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "连接失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/resource/servers" :query="query" :columns="columns" pagination>
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称/主机" style="width: 200px" />
        <u-button
          v-if="hasPermission('resource_servers:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建服务器
        </u-button>
      </template>
      <template #column:auth_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Server).auth_type, AUTH_TYPE_TAG)">
          {{ (rowData as Server).auth_type }}
        </u-tag>
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Server).status, SERVER_STATUS_TAG)">
          {{ (rowData as Server).status || "—" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="4" :loading="busyKey === (rowData as Server).id">
          <u-action
            v-if="hasPermission('resource_servers:update')"
            @run="openEdit(rowData as Server)"
          >
            编辑
          </u-action>
          <u-action v-if="hasPermission('resource_servers:view')" @run="onTest(rowData as Server)">
            测试
          </u-action>
          <u-action
            v-if="hasPermission('resource_servers:delete')"
            @run="remove(rowData as Server)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑服务器' : '新建服务器'"
      :model="form"
      label-width="110px"
      style="width: 560px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="主机" field="host" :rules="{ required: '必填' }" />
      <u-number-input label="端口" field="port" />
      <u-select
        label="OS"
        field="os_type"
        :options="[
          { label: 'linux', value: 'linux' },
          { label: 'windows', value: 'windows' },
        ]"
      />
      <u-input label="用户名" field="username" />
      <u-select
        label="认证方式"
        field="auth_type"
        :options="[
          { label: 'password', value: 'password' },
          { label: 'key', value: 'key' },
          { label: 'ssh_agent', value: 'ssh_agent' },
          { label: 'agent', value: 'agent' },
        ]"
      />
      <u-select label="凭证" field="credential_id" :options="credOptions" clearable />
      <u-input v-if="form.auth_type === 'agent'" label="Agent URL" field="agent_url" />
      <u-select
        v-if="form.auth_type === 'agent'"
        label="Agent 凭证"
        field="agent_credential_id"
        :options="credOptions"
        clearable
      />
      <u-input label="描述" field="description" />
    </FormDialog>
  </div>
</template>
