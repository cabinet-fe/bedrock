<script setup lang="ts">
defineOptions({ name: "ResourceCredentials" });

import { reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import { createCredential, deleteCredential, updateCredential } from "@/api/resource";
import type { Credential } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { tagType, type TagType } from "@/lib/tag";

const CRED_TYPE_TAG: Record<string, TagType> = {
  token: "primary",
  api_key: "primary",
  password: "warning",
  ssh_key: "info",
};

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const listRef = useTemplateRef("list");
const query = reactive({ keyword: "" });
const dialogOpen = ref(false);
const editing = ref<Credential | null>(null);
const form = reactive({
  name: "",
  type: "token",
  username: "",
  secret: "",
  passphrase: "",
  description: "",
});

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "secret", title: "密文" },
];

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "type", name: "类型", width: 100, align: "center" },
  { key: "username", name: "用户名" },
  { key: "has_secret", name: "密文", width: 80, align: "center" },
  { key: "action", name: "操作", width: 200, align: "center", fixed: "right" },
]);

function openCreate() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(row: Credential) {
  editing.value = row;
  o(form).extend(row);
  form.secret = "";
  form.passphrase = "";
  dialogOpen.value = true;
}

async function save() {
  try {
    const body: Record<string, unknown> = { ...form };
    if (!form.passphrase) delete body.passphrase;
    if (editing.value) {
      if (!form.secret) delete body.secret;
      await updateCredential(editing.value.id, body);
      message.success("已更新");
    } else {
      await createCredential(body);
      message.success("已创建");
    }
    dialogOpen.value = false;
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: Credential) => {
  try {
    await deleteCredential(row.id);
    message.success("已删除");
    await listRef.value?.reload();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="list" url="/resource/credentials" :query="query" :columns="columns" pagination>
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称关键词" style="width: 200px" />
        <u-button
          v-if="hasPermission('resource_credentials:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          新建凭证
        </u-button>
      </template>
      <template #column:type="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Credential).type, CRED_TYPE_TAG)">
          {{ (rowData as Credential).type }}
        </u-tag>
      </template>
      <template #column:has_secret="{ rowData }">
        <u-tag size="small" :type="(rowData as Credential).has_secret ? 'success' : 'warning'">
          {{ (rowData as Credential).has_secret ? "已设置" : "无" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as Credential).id">
          <u-action
            v-if="hasPermission('resource_credentials:update')"
            @run="openEdit(rowData as Credential)"
          >
            编辑
          </u-action>
          <u-action
            v-if="hasPermission('resource_credentials:delete')"
            @run="remove(rowData as Credential)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑凭证' : '新建凭证'"
      :model="form"
      :groups="formGroups"
      label-width="110px"
      style="width: 520px"
      @submit="save"
    >
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-select
          label="类型"
          field="type"
          :options="[
            { label: 'password', value: 'password' },
            { label: 'token', value: 'token' },
            { label: 'ssh_key', value: 'ssh_key' },
            { label: 'api_key', value: 'api_key' },
          ]"
          :rules="{ required: '必填' }"
          tips="决定密文字段语义；被仓库/服务器引用时按类型校验"
        />
        <u-input label="用户名" field="username" />
        <u-input label="描述" field="description" />
      </template>
      <template #group:secret>
        <u-password-input
          :label="editing ? '密文（留空不改）' : '密文'"
          field="secret"
          autocomplete="new-password"
          :rules="editing ? undefined : { required: '必填' }"
          tips="AES-GCM 加密存储，API 永不回显明文"
        />
        <u-password-input
          v-if="form.type === 'ssh_key'"
          label="口令（留空不改）"
          field="passphrase"
          autocomplete="new-password"
          tips="私钥口令（passphrase），同样加密存储"
        />
      </template>
    </FormDialog>
  </div>
</template>
