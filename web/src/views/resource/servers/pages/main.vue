<script setup lang="ts">
defineOptions({ name: "ResourceServers" });

import { computed, reactive, ref, useTemplateRef, watch } from "vue";
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

const AUTH_TYPE_LABEL: Record<string, string> = {
  password: "密码",
  ssh_key: "SSH 密钥",
  key: "SSH 密钥",
  ssh_agent: "SSH 密钥",
  agent: "Deploy Agent",
};

const AUTH_TYPE_TAG: Record<string, TagType> = {
  password: "warning",
  ssh_key: "info",
  key: "info",
  ssh_agent: "info",
  agent: "primary",
};

const SERVER_STATUS_TAG: Record<string, TagType> = {
  online: "success",
  offline: "danger",
  unknown: undefined,
};

const AUTH_OPTIONS = [
  { label: "密码", value: "password" },
  { label: "SSH 密钥", value: "ssh_key" },
  { label: "Deploy Agent", value: "agent" },
];

const AUTH_TIPS: Record<string, string> = {
  password: "密码表单直填，AES-GCM 加密存于服务器记录",
  ssh_key:
    "请在运行 Bedrock 的主机配置私钥（~/.ssh 默认私钥或 ssh-agent），目标机已授权对应公钥；应用内不存储私钥",
  agent: "经远端 Deploy Agent；需填写 Agent URL，可选绑定 Agent 凭证",
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
  auth_type: "",
  password: "",
  agent_url: "",
  agent_credential_id: undefined as number | undefined,
  description: "",
  tags: "",
});

const formGroups = computed(() =>
  form.auth_type
    ? [
        { key: "connection", title: "基本信息" },
        { key: "auth", title: "认证" },
      ]
    : [{ key: "connection", title: "基本信息" }],
);

const showHostFields = computed(() => !!form.auth_type && form.auth_type !== "agent");

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "host", name: "主机" },
  { key: "port", name: "端口" },
  { key: "auth_type", name: "认证", width: 120, align: "center" },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
]);

async function loadAgentCredentials() {
  if (credOptions.value.length > 0) return;
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

watch(
  () => form.auth_type,
  (t) => {
    if (t === "agent") void loadAgentCredentials();
  },
);

function openCreate() {
  editing.value = null;
  o(form).extend({
    name: "",
    host: "",
    port: 22,
    os_type: "linux",
    username: "",
    auth_type: "",
    password: "",
    agent_url: "",
    agent_credential_id: undefined,
    description: "",
    tags: "",
  });
  dialogOpen.value = true;
}

function openEdit(row: Server) {
  editing.value = row;
  const auth = row.auth_type === "key" || row.auth_type === "ssh_agent" ? "ssh_key" : row.auth_type;
  o(form).extend({ ...row, password: "", auth_type: auth });
  if (auth === "agent") void loadAgentCredentials();
  dialogOpen.value = true;
}

async function save() {
  try {
    const body: Record<string, unknown> = {
      name: form.name,
      host: form.host,
      port: form.port,
      os_type: form.os_type,
      username: form.username,
      auth_type: form.auth_type,
      description: form.description,
      tags: form.tags,
    };
    if (form.auth_type === "password" && form.password) {
      body.password = form.password;
    }
    if (form.auth_type === "agent") {
      body.agent_url = form.agent_url;
      if (form.agent_credential_id) {
        body.agent_credential_id = form.agent_credential_id;
      } else if (editing.value) {
        body.clear_agent_credential = true;
      }
    } else if (editing.value) {
      body.agent_url = "";
      body.clear_agent_credential = true;
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

function authLabel(t: string) {
  return AUTH_TYPE_LABEL[t] ?? t;
}
</script>

<template>
  <div>
    <ProTable ref="list" url="/resource/servers" :query="query" :columns="columns" pagination>
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称/主机" style="width: 200px" />
      </template>
      <template #toolbar>
        <u-button
          v-if="hasPermission('resource_servers:create')"
          type="primary"
          @click.prevent="openCreate"
        >
          新建服务器
        </u-button>
      </template>
      <template #column:auth_type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Server).auth_type, AUTH_TYPE_TAG)">
          {{ authLabel((rowData as Server).auth_type) }}
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
      :groups="formGroups"
      label-width="110px"
      style="width: 560px"
      @submit="save"
    >
      <template #group:connection>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-select
          label="OS"
          field="os_type"
          :options="[
            { label: 'linux', value: 'linux' },
            { label: 'windows', value: 'windows' },
          ]"
        />
        <u-select
          label="认证方式"
          field="auth_type"
          :options="AUTH_OPTIONS"
          :rules="{ required: '必填' }"
          :tips="AUTH_TIPS[form.auth_type]"
        />
        <u-input v-if="showHostFields" label="主机" field="host" :rules="{ required: '必填' }" />
        <u-number-input v-if="showHostFields" label="端口" field="port" />
        <u-input label="描述" field="description" />
      </template>
      <template #group:auth>
        <u-input v-if="showHostFields" label="用户名" field="username" />
        <u-password-input
          v-if="form.auth_type === 'password'"
          :label="editing ? '密码（留空不改）' : '密码'"
          field="password"
          autocomplete="new-password"
          :tips="
            editing?.has_password ? '已保存密码；留空表示不修改' : 'AES-GCM 加密存储，API 永不回显'
          "
        />
        <u-input
          v-if="form.auth_type === 'agent'"
          label="Agent URL"
          field="agent_url"
          tips="远端 Deploy Agent 的 HTTP 地址"
        />
        <u-select
          v-if="form.auth_type === 'agent'"
          label="Agent 凭证"
          field="agent_credential_id"
          :options="credOptions"
          clearable
          tips="访问 Deploy Agent 时使用的凭证；绑定需 credentials:use"
        />
      </template>
    </FormDialog>
  </div>
</template>
