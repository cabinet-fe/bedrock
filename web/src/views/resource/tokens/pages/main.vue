<script setup lang="ts">
defineOptions({ name: "ResourceTokens" });

import { date, type Dater } from "@cat-kit/core";
import { message } from "@veltra/desktop";
import { reactive, ref, useTemplateRef } from "vue";

import { createToken, deleteToken } from "@/api/resource";
import type { PersonalAccessToken } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const table = useTemplateRef("table");
const dialogOpen = ref(false);
const plaintext = ref("");

type ExpireMode = "days" | "date" | "never";

const EXPIRE_MODE_OPTIONS = [
  { value: "days", label: "天数" },
  { value: "date", label: "自定义日期" },
  { value: "never", label: "永不过期" },
];

const EXPIRE_DAYS_OPTIONS = [
  { value: 30, label: "30 天" },
  { value: 90, label: "90 天" },
  { value: 180, label: "180 天" },
  { value: 365, label: "365 天" },
];

const form = reactive({
  name: "",
  scopeSkills: true,
  scopeAgents: false,
  scopeDocsWrite: false,
  scopeDocsPublish: false,
  expireMode: "days" as ExpireMode,
  expireDays: 30,
  expires_at: "",
});

const formGroups = [
  { key: "basic", title: "基本信息" },
  { key: "expire", title: "过期设置" },
];

type TokenStatus = "valid" | "expired" | "revoked";

const STATUS_LABEL: Record<TokenStatus, string> = {
  valid: "有效",
  expired: "已过期",
  revoked: "已吊销",
};

const STATUS_TAG: Record<TokenStatus, "success" | "warning" | "danger"> = {
  valid: "success",
  expired: "warning",
  revoked: "danger",
};

const columns = defineProTableColumns([
  { key: "name", name: "名称" },
  { key: "token_prefix", name: "前缀" },
  { key: "scopes", name: "Scope" },
  { key: "status", name: "状态", width: 100, align: "center" },
  {
    key: "expires_at",
    name: "过期时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val) || "永不过期",
  },
  {
    key: "last_used_at",
    name: "最近使用",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val) || "—",
  },
  {
    key: "created_at",
    name: "创建时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 120, align: "center", fixed: "right" },
]);

function tokenStatus(row: PersonalAccessToken): TokenStatus {
  if (row.revoked_at) return "revoked";
  if (row.expires_at && new Date(row.expires_at).getTime() <= Date.now()) return "expired";
  return "valid";
}

function disabledExpiresAt(d: Dater) {
  return d.startOf("day").timestamp < date().startOf("day").timestamp;
}

function openCreate() {
  plaintext.value = "";
  form.name = "";
  form.scopeSkills = true;
  form.scopeAgents = false;
  form.scopeDocsWrite = false;
  form.scopeDocsPublish = false;
  form.expireMode = "days";
  form.expireDays = 30;
  form.expires_at = "";
  dialogOpen.value = true;
}

async function save() {
  const scopes: string[] = [];
  if (form.scopeSkills) scopes.push("skills:read");
  if (form.scopeAgents) scopes.push("agents:run");
  if (form.scopeDocsWrite) scopes.push("docs:write");
  if (form.scopeDocsPublish) scopes.push("docs:publish");
  if (!scopes.length) {
    message.error("至少选择一个 scope");
    return;
  }
  if (form.expireMode === "date" && !form.expires_at) {
    message.error("请选择过期日期");
    return;
  }
  try {
    const payload: {
      name: string;
      scopes: string[];
      expires_at?: string;
      expires_in_days?: number;
    } = {
      name: form.name,
      scopes,
    };
    if (form.expireMode === "days") {
      payload.expires_in_days = form.expireDays;
    } else if (form.expireMode === "date") {
      payload.expires_at = date(form.expires_at).endOf("day").raw.toISOString();
    }
    const result = await createToken(payload);
    plaintext.value = result.token;
    message.success("令牌已创建，请立即复制明文（仅显示一次）");
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "创建失败");
  }
}

async function copyPlaintext() {
  if (!plaintext.value) return;
  try {
    await navigator.clipboard.writeText(plaintext.value);
    message.success("已复制");
  } catch {
    message.error("复制失败");
  }
}

const remove = bind(async (row: PersonalAccessToken) => {
  try {
    await deleteToken(row.id);
    message.success("已删除");
    table.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
});
</script>

<template>
  <div>
    <ProTable ref="table" url="/resource/tokens" pagination :columns="columns">
      <template #filters>
        <u-button
          v-if="hasPermission('resource_tokens:create')"
          type="primary"
          style="margin-left: auto"
          @click.prevent="openCreate"
        >
          创建令牌
        </u-button>
      </template>
      <template #column:scopes="{ rowData }">
        <span class="tag-cell">
          <u-tag
            v-for="scope in (rowData as PersonalAccessToken).scopes || []"
            :key="scope"
            size="small"
            type="info"
          >
            {{ scope }}
          </u-tag>
        </span>
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" :type="STATUS_TAG[tokenStatus(rowData as PersonalAccessToken)]">
          {{ STATUS_LABEL[tokenStatus(rowData as PersonalAccessToken)] }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="2" :loading="busyKey === (rowData as PersonalAccessToken).id">
          <u-action
            v-if="hasPermission('resource_tokens:delete')"
            need-confirm
            type="danger"
            @run="remove(rowData as PersonalAccessToken)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      title="创建 PAT"
      :model="form"
      :groups="formGroups"
      label-width="90px"
      style="width: 520px"
      @submit="save"
      @closed="plaintext = ''"
    >
      <template #prepend>
        <p class="create-tip">创建后明文仅显示一次，请立即复制并妥善保管。</p>
      </template>
      <template #group:basic>
        <u-input label="名称" field="name" :rules="{ required: '必填' }" />
        <u-form-item label="Scope">
          <div class="scope-row">
            <u-checkbox v-model="form.scopeSkills">skills:read</u-checkbox>
            <u-checkbox v-model="form.scopeAgents">agents:run</u-checkbox>
            <u-checkbox v-model="form.scopeDocsWrite">docs:write</u-checkbox>
            <u-checkbox v-model="form.scopeDocsPublish">docs:publish</u-checkbox>
          </div>
        </u-form-item>
      </template>
      <template #group:expire>
        <u-radio-group label="过期时间" field="expireMode" :items="EXPIRE_MODE_OPTIONS" />
        <u-select
          v-if="form.expireMode === 'days'"
          label="有效天数"
          field="expireDays"
          :options="EXPIRE_DAYS_OPTIONS"
          :rules="{ required: '必填' }"
        />
        <u-date-picker
          v-if="form.expireMode === 'date'"
          label="过期日期"
          field="expires_at"
          placeholder="选择日期"
          :disabled-date="disabledExpiresAt"
          :rules="{ required: '必填' }"
        />
      </template>
      <template v-if="plaintext" #append>
        <div class="once">
          <div class="once-head">
            <strong>明文（仅此一次）：</strong>
            <u-button size="small" @click="copyPlaintext">复制</u-button>
          </div>
          <code>{{ plaintext }}</code>
        </div>
      </template>
    </FormDialog>
  </div>
</template>

<style scoped lang="scss">
.create-tip {
  margin: 0 0 4px;
  font-size: 13px;
  color: var(--u-color-text-secondary, #666);
  line-height: 1.5;
}
.scope-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.tag-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
.once {
  margin-top: 12px;
  padding: 10px;
  background: var(--u-color-warning-bg, #fff7ed);
  border-radius: 6px;
  word-break: break-all;
}
.once-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;
}
</style>
