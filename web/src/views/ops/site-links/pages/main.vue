<script setup lang="ts">
defineOptions({ name: "OpsSiteLinks" });

import { o } from "@cat-kit/core";
import { clipboard } from "@cat-kit/fe";
import { defineTableColumns, message } from "@veltra/desktop";
import { PictureRounded } from "@veltra/icons/normal";
import { onMounted, reactive, ref, shallowRef } from "vue";

import { createSiteLink, deleteSiteLink, listSiteLinks, updateSiteLink } from "@/api/ops";
import type { SiteLink, SiteLinkWrite } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

const ICON_LIMIT = 32 * 1024;

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();

const loading = ref(false);
const items = shallowRef<SiteLink[]>([]);
const dialogOpen = ref(false);
const editingId = ref<number | null>(null);
const form = reactive<SiteLinkWrite>({
  name: "",
  url: "",
  icon_base64: "",
  icon_mime: "",
  account: "",
  password: "",
  remark: "",
  enabled: true,
});

const columns = defineTableColumns([
  { key: "icon", name: "图标", width: 72, align: "center" },
  { key: "name", name: "名称", minWidth: 140 },
  { key: "url", name: "地址", minWidth: 200 },
  { key: "account", name: "账号", minWidth: 160 },
  { key: "password", name: "密码", minWidth: 160 },
  { key: "remark", name: "备注", minWidth: 120 },
  { key: "enabled", name: "状态", width: 90, align: "center" },
  { key: "action", name: "操作", width: 140, align: "center", fixed: "right" },
]);

function iconSrc(iconBase64: string, iconMime: string): string {
  const raw = iconBase64.trim();
  if (!raw) return "";
  return `data:${iconMime || "image/png"};base64,${raw}`;
}

async function load() {
  loading.value = true;
  try {
    items.value = await listSiteLinks();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

async function copyValue(value: string) {
  try {
    await clipboard.copy(value);
    message.success("已复制");
  } catch {
    message.error("复制失败");
  }
}

function openCreate() {
  editingId.value = null;
  dialogOpen.value = true;
}

function openEdit(row: SiteLink) {
  editingId.value = row.id;
  o(form).extend(row);
  dialogOpen.value = true;
}

async function onIconPick(files: File[]) {
  const file = files[0];
  if (!file) return;
  if (file.size > ICON_LIMIT) {
    message.error("图标原始体积不得超过 32KB");
    return;
  }
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  form.icon_base64 = btoa(binary);
  form.icon_mime = file.type || "image/png";
}

function clearIcon() {
  form.icon_base64 = "";
  form.icon_mime = "";
}

async function save() {
  try {
    // Empty account/password are stored as empty strings; do not omit them.
    const body = { ...form };
    if (editingId.value == null) {
      await createSiteLink(body);
      message.success("已创建");
    } else {
      await updateSiteLink(editingId.value, body);
      message.success("已更新");
    }
    dialogOpen.value = false;
    await load();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存失败");
  }
}

const remove = bind(async (row: SiteLink) => {
  try {
    await deleteSiteLink(row.id);
    message.success("已删除");
    await load();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
});

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="site-links">
    <div class="site-links__bar">
      <u-button v-if="hasPermission('ops_site_links:create')" type="primary" @click="openCreate">
        新建网站
      </u-button>
    </div>
    <div v-loading="loading" class="site-links__panel">
      <u-table :columns="columns" :data="items" row-key="id">
        <template #column:icon="{ rowData }">
          <img
            v-if="iconSrc((rowData as SiteLink).icon_base64, (rowData as SiteLink).icon_mime)"
            class="site-icon"
            :src="iconSrc((rowData as SiteLink).icon_base64, (rowData as SiteLink).icon_mime)"
            alt=""
          />
          <span v-else class="site-icon site-icon--placeholder" aria-hidden="true">
            <u-icon :size="16"><PictureRounded /></u-icon>
          </span>
        </template>
        <template #column:url="{ rowData }">
          <a
            v-if="(rowData as SiteLink).enabled && (rowData as SiteLink).url"
            class="site-url"
            :href="(rowData as SiteLink).url"
            target="_blank"
            rel="noopener noreferrer"
          >
            {{ (rowData as SiteLink).url }}
          </a>
          <span v-else class="site-url is-disabled">{{ (rowData as SiteLink).url || "—" }}</span>
        </template>
        <template #column:account="{ rowData }">
          <span class="copy-cell">
            <span class="copy-cell__text">{{ (rowData as SiteLink).account || "—" }}</span>
            <u-button text size="small" @click="copyValue((rowData as SiteLink).account)">
              复制
            </u-button>
          </span>
        </template>
        <template #column:password="{ rowData }">
          <span class="copy-cell">
            <span class="copy-cell__text">{{ (rowData as SiteLink).password || "—" }}</span>
            <u-button text size="small" @click="copyValue((rowData as SiteLink).password)">
              复制
            </u-button>
          </span>
        </template>
        <template #column:remark="{ rowData }">
          {{ (rowData as SiteLink).remark || "—" }}
        </template>
        <template #column:enabled="{ rowData }">
          <u-tag size="small" :type="(rowData as SiteLink).enabled ? 'success' : 'warning'">
            {{ (rowData as SiteLink).enabled ? "启用" : "停用" }}
          </u-tag>
        </template>
        <template #column:action="{ rowData }">
          <u-action-group :max="2" :loading="busyKey === (rowData as SiteLink).id">
            <u-action
              v-if="hasPermission('ops_site_links:update')"
              @run="openEdit(rowData as SiteLink)"
            >
              编辑
            </u-action>
            <u-action
              v-if="hasPermission('ops_site_links:delete')"
              need-confirm
              type="danger"
              @run="remove(rowData as SiteLink)"
            >
              删除
            </u-action>
          </u-action-group>
        </template>
      </u-table>
    </div>

    <FormDialog
      v-model="dialogOpen"
      :title="editingId == null ? '新建网站' : '编辑网站'"
      :model="form"
      label-width="72px"
      style="width: 520px"
      @submit="save"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input label="地址" field="url" :rules="{ required: '必填' }" placeholder="https://" />
      <u-form-item label="图标">
        <div class="icon-field">
          <img
            v-if="iconSrc(form.icon_base64, form.icon_mime)"
            class="site-icon"
            :src="iconSrc(form.icon_base64, form.icon_mime)"
            alt=""
          />
          <span v-else class="site-icon site-icon--placeholder" aria-hidden="true">
            <u-icon :size="16"><PictureRounded /></u-icon>
          </span>
          <div class="icon-field__body">
            <u-file-picker accept="image/*" @pick="onIconPick">
              <u-button>{{ form.icon_base64 ? "更换图标" : "上传图标" }}</u-button>
            </u-file-picker>
            <u-button v-if="form.icon_base64" text size="small" @click="clearIcon">清除</u-button>
            <p class="icon-hint">原始体积 ≤ 32KB</p>
          </div>
        </div>
      </u-form-item>
      <u-input label="账号" field="account" />
      <u-input label="密码" field="password" />
      <u-input label="备注" field="remark" />
      <u-switch label="启用" field="enabled" />
    </FormDialog>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.site-links {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, default);
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}

.site-links__bar {
  display: flex;
  justify-content: flex-end;
  flex-shrink: 0;
}

.site-links__panel {
  flex: 1;
  min-height: 0;
  overflow: auto;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.site-icon {
  width: 28px;
  height: 28px;
  object-fit: contain;
  border-radius: fn.use-var(radius, small);
  vertical-align: middle;
  flex-shrink: 0;
}

.site-icon--placeholder {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: fn.use-var(bg-color, middle);
  color: fn.use-var(text-color, assist);
}

.site-url {
  color: fn.use-var(color, primary);
  word-break: break-all;
}

.site-url.is-disabled {
  color: fn.use-var(text-color, main);
  cursor: default;
}

.copy-cell {
  display: inline-flex;
  align-items: center;
  gap: fn.use-var(gap, small);
  max-width: 100%;
}

.copy-cell__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.icon-field {
  display: flex;
  align-items: flex-start;
  gap: fn.use-var(gap, default);
}

.icon-field__body {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: fn.use-var(gap, small);
}

.icon-hint {
  margin: 0;
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, assist);
}
</style>
