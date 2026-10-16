<script setup lang="ts">
defineOptions({ name: "SiteLinks" });

import { o } from "@cat-kit/core";
import { clipboard } from "@cat-kit/fe";
import { defineTableColumns, message, messageConfirm, type ContextMenuItem } from "@veltra/desktop";
import { Copy, Delete, Edit, Key, PictureRounded, Plus } from "@veltra/icons/normal";
import { computed, onMounted, reactive, ref, shallowRef } from "vue";

import { createSiteLink, deleteSiteLink, listSiteLinks, updateSiteLink } from "@/api/ops";
import type { SiteLink, SiteLinkAccount, SiteLinkWrite } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { usePermission } from "@/composables/use-permission";

const ICON_LIMIT = 32 * 1024;

const { hasPermission } = usePermission();

const canCreate = computed(() => hasPermission("site_links:create"));
const canUpdate = computed(() => hasPermission("site_links:update"));
const canDelete = computed(() => hasPermission("site_links:delete"));

const loading = ref(false);
const items = shallowRef<SiteLink[]>([]);
const dialogOpen = ref(false);
const editingId = ref<number | null>(null);
const form = reactive<SiteLinkWrite>({
  name: "",
  url: "",
  icon_base64: "",
  icon_mime: "",
  accounts: [],
  remark: "",
  enabled: true,
});

const accountDialogOpen = ref(false);
const viewingItem = ref<SiteLink | null>(null);

const menuOpen = ref(false);
const menuPos = ref({ x: 0, y: 0 });
const menuItems = shallowRef<ContextMenuItem[]>([]);

const accountColumns = defineTableColumns([
  { key: "account", name: "账号", minWidth: 160 },
  { key: "password", name: "密码", minWidth: 160 },
  { key: "remark", name: "备注", minWidth: 140 },
]);

function iconSrc(iconBase64: string, iconMime: string): string {
  const raw = iconBase64.trim();
  if (!raw) return "";
  return `data:${iconMime || "image/png"};base64,${raw}`;
}

function firstChar(name: string): string {
  const ch = Array.from(name.trim())[0];
  return ch ? ch.toUpperCase() : "?";
}

async function load() {
  loading.value = true;
  try {
    items.value = await listSiteLinks();
    if (viewingItem.value) {
      viewingItem.value = items.value.find((it) => it.id === viewingItem.value?.id) ?? null;
    }
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
  } finally {
    loading.value = false;
  }
}

async function copyValue(value: string) {
  if (!value) return;
  try {
    await clipboard.copy(value);
    message.success("已复制");
  } catch {
    message.error("复制失败");
  }
}

function onDisabledClick() {
  message.warning("该网站已停用，无法跳转");
}

function openAccounts(row: SiteLink) {
  viewingItem.value = row;
  accountDialogOpen.value = true;
}

function openCreate() {
  editingId.value = null;
  dialogOpen.value = true;
}

function openEdit(row: SiteLink) {
  editingId.value = row.id;
  o(form).extend(row);
  form.accounts = (row.accounts ?? []).map((acc) => ({ ...acc }));
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
    const accounts = form.accounts
      .map((acc) => ({
        account: acc.account.trim(),
        password: acc.password,
        remark: acc.remark.trim(),
      }))
      .filter((acc) => acc.account || acc.password || acc.remark);
    const body: SiteLinkWrite = {
      ...form,
      accounts,
    };
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

async function confirmRemove(row: SiteLink) {
  const action = await messageConfirm.danger(`删除网站「${row.name}」？`, {
    cancelButtonText: "取消",
  }).onClosed;
  if (action !== "confirm") return;
  try {
    await deleteSiteLink(row.id);
    message.success("已删除");
    if (viewingItem.value?.id === row.id) {
      accountDialogOpen.value = false;
      viewingItem.value = null;
    }
    await load();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "删除失败");
  }
}

function openMenu(e: MouseEvent, list: ContextMenuItem[]) {
  menuPos.value = { x: e.clientX, y: e.clientY };
  menuItems.value = list;
  menuOpen.value = true;
}

function onTileContextMenu(e: MouseEvent, row: SiteLink) {
  const list: ContextMenuItem[] = [
    {
      label: `查看账号 (${row.accounts?.length ?? 0})`,
      icon: Key,
      callback: () => openAccounts(row),
    },
    {
      label: "复制链接",
      icon: Copy,
      disabled: !row.url,
      callback: () => void copyValue(row.url),
    },
  ];
  if (canUpdate.value) {
    list.push({
      label: "编辑",
      icon: Edit,
      callback: () => openEdit(row),
    });
  }
  if (canDelete.value) {
    list.push({
      label: "删除",
      icon: Delete,
      callback: () => {
        menuOpen.value = false;
        window.setTimeout(() => {
          void confirmRemove(row);
        }, 0);
      },
    });
  }
  openMenu(e, list);
}

function onBlankContextMenu(e: MouseEvent) {
  const el = e.target as HTMLElement | null;
  if (el?.closest(".site-tile")) return;
  if (!canCreate.value) return;
  e.preventDefault();
  openMenu(e, [
    {
      label: "新建网站",
      icon: Plus,
      callback: () => openCreate(),
    },
  ]);
}

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="site-links">
    <div v-if="canCreate" class="site-links__bar">
      <u-button type="primary" @click="openCreate">新建网站</u-button>
    </div>

    <div v-loading="loading" class="site-links__panel" @contextmenu="onBlankContextMenu">
      <u-empty v-if="!loading && !items.length" text="暂无网站链接" />
      <div v-else class="site-grid">
        <template v-for="item in items" :key="item.id">
          <a
            v-if="item.enabled && item.url"
            class="site-tile"
            :href="item.url"
            target="_blank"
            rel="noopener noreferrer"
            :title="item.remark ? `${item.name}：${item.remark}` : item.name"
            @contextmenu.prevent.stop="onTileContextMenu($event, item)"
          >
            <div class="site-tile__icon">
              <img
                v-if="iconSrc(item.icon_base64, item.icon_mime)"
                class="site-tile__img"
                :src="iconSrc(item.icon_base64, item.icon_mime)"
                alt=""
              />
              <span v-else class="site-tile__letter" aria-hidden="true">
                {{ firstChar(item.name) }}
              </span>
              <span
                v-if="item.accounts?.length"
                class="site-tile__badge"
                :title="`共 ${item.accounts.length} 个账号，右键查看`"
              >
                {{ item.accounts.length }}
              </span>
            </div>
            <span class="site-tile__name">{{ item.name }}</span>
          </a>

          <div
            v-else
            class="site-tile is-disabled"
            :title="`${item.name}（已停用）`"
            @click="onDisabledClick"
            @contextmenu.prevent.stop="onTileContextMenu($event, item)"
          >
            <div class="site-tile__icon">
              <img
                v-if="iconSrc(item.icon_base64, item.icon_mime)"
                class="site-tile__img"
                :src="iconSrc(item.icon_base64, item.icon_mime)"
                alt=""
              />
              <span v-else class="site-tile__letter" aria-hidden="true">
                {{ firstChar(item.name) }}
              </span>
              <span class="site-tile__badge site-tile__badge--off">停用</span>
            </div>
            <span class="site-tile__name">{{ item.name }}</span>
          </div>
        </template>
      </div>
    </div>

    <u-contextmenu
      v-if="menuOpen"
      :mouse-position="menuPos"
      :menus="menuItems"
      :width="160"
      @destroy="menuOpen = false"
    />

    <u-dialog
      v-model="accountDialogOpen"
      :title="viewingItem ? `${viewingItem.name} - 账号与详情` : '账号与详情'"
      style="width: 640px"
    >
      <div v-if="viewingItem" class="account-dialog">
        <div class="account-dialog__meta">
          <div class="account-dialog__row">
            <span class="account-dialog__label">地址</span>
            <div class="account-dialog__val">
              <a
                v-if="viewingItem.enabled && viewingItem.url"
                class="site-url"
                :href="viewingItem.url"
                target="_blank"
                rel="noopener noreferrer"
              >
                {{ viewingItem.url }}
              </a>
              <span v-else class="site-url is-disabled">{{ viewingItem.url || "—" }}</span>
              <u-button
                v-if="viewingItem.url"
                text
                size="small"
                @click="copyValue(viewingItem.url)"
              >
                复制
              </u-button>
            </div>
          </div>
          <div class="account-dialog__row">
            <span class="account-dialog__label">状态</span>
            <div class="account-dialog__val">
              <u-tag size="small" :type="viewingItem.enabled ? 'success' : 'warning'">
                {{ viewingItem.enabled ? "启用" : "停用" }}
              </u-tag>
            </div>
          </div>
          <div v-if="viewingItem.remark" class="account-dialog__row">
            <span class="account-dialog__label">备注</span>
            <div class="account-dialog__val">{{ viewingItem.remark }}</div>
          </div>
        </div>

        <u-table
          v-if="viewingItem.accounts?.length"
          :columns="accountColumns"
          :data="viewingItem.accounts"
        >
          <template #column:account="{ rowData }">
            <span class="copy-cell">
              <span class="copy-cell__text">{{ (rowData as SiteLinkAccount).account || "—" }}</span>
              <u-button
                v-if="(rowData as SiteLinkAccount).account"
                text
                size="small"
                @click="copyValue((rowData as SiteLinkAccount).account)"
              >
                复制
              </u-button>
            </span>
          </template>
          <template #column:password="{ rowData }">
            <span class="copy-cell">
              <span class="copy-cell__text">{{
                (rowData as SiteLinkAccount).password || "—"
              }}</span>
              <u-button
                v-if="(rowData as SiteLinkAccount).password"
                text
                size="small"
                @click="copyValue((rowData as SiteLinkAccount).password)"
              >
                复制
              </u-button>
            </span>
          </template>
          <template #column:remark="{ rowData }">
            {{ (rowData as SiteLinkAccount).remark || "—" }}
          </template>
        </u-table>
        <u-empty v-else text="暂未配置账号" />
      </div>
    </u-dialog>

    <FormDialog
      v-model="dialogOpen"
      :title="editingId == null ? '新建网站' : '编辑网站'"
      :model="form"
      label-width="72px"
      style="width: 640px"
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
          <span v-else-if="form.name.trim()" class="site-icon site-icon--letter" aria-hidden="true">
            {{ firstChar(form.name) }}
          </span>
          <span v-else class="site-icon site-icon--placeholder" aria-hidden="true">
            <u-icon :size="18"><PictureRounded /></u-icon>
          </span>
          <div class="icon-field__body">
            <div class="icon-field__actions">
              <u-file-picker accept="image/*" @pick="onIconPick">
                <u-button>{{ form.icon_base64 ? "更换图标" : "上传图标" }}</u-button>
              </u-file-picker>
              <u-button v-if="form.icon_base64" text size="small" @click="clearIcon">清除</u-button>
            </div>
            <p class="icon-hint">未上传图标时默认以网站名称首字作为图标；上传图片原始体积 ≤ 32KB</p>
          </div>
        </div>
      </u-form-item>
      <u-group-input
        field="accounts"
        label="账号"
        span="full"
        :item-default="{ account: '', password: '', remark: '' }"
        :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
      >
        <template #default="{ item }">
          <u-input v-model="item.account" placeholder="账号" />
          <u-input v-model="item.password" placeholder="密码" />
          <u-input v-model="item.remark" placeholder="备注（如管理员/测试员）" />
        </template>
      </u-group-input>
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
  padding: fn.use-var(gap, large);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.site-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
  gap: fn.use-var(gap, large) fn.use-var(gap, default);
  align-items: start;
}

.site-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: fn.use-var(gap, small);
  padding: fn.use-var(gap, small);
  border-radius: fn.use-var(radius, default);
  text-decoration: none;
  color: fn.use-var(text-color, main);
  cursor: pointer;
  user-select: none;
  transition:
    background-color 0.16s ease,
    transform 0.16s ease;

  &:hover {
    background: fn.use-var(bg-color, hover);

    .site-tile__icon {
      transform: translateY(-2px);
    }
  }

  &.is-disabled {
    opacity: 0.56;
    cursor: not-allowed;

    &:hover .site-tile__icon {
      transform: none;
    }
  }
}

.site-tile__icon {
  position: relative;
  width: 64px;
  height: 64px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: fn.use-var(bg-color, middle);
  border: fn.use-var(border);
  transition: transform 0.16s ease;
  flex-shrink: 0;
}

.site-tile__img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  border-radius: 15px;
  padding: 6px;
  box-sizing: border-box;
}

.site-tile__letter {
  font-size: 26px;
  font-weight: 600;
  line-height: 1;
  color: fn.use-var(color, primary);
}

.site-tile__badge {
  position: absolute;
  top: -5px;
  right: -5px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 500;
  line-height: 18px;
  text-align: center;
  background: fn.use-var(color, primary);
  color: #fff;
  box-sizing: border-box;
}

.site-tile__badge--off {
  background: fn.use-var(color, warning);
}

.site-tile__name {
  width: 100%;
  font-size: fn.use-var(font-size-main, small);
  line-height: 1.35;
  text-align: center;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.account-dialog {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, default);
}

.account-dialog__meta {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, small);
  padding: fn.use-var(gap, default);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, middle);
}

.account-dialog__row {
  display: flex;
  align-items: baseline;
  gap: fn.use-var(gap, default);
  font-size: fn.use-var(font-size-main, default);
}

.account-dialog__label {
  flex-shrink: 0;
  width: 44px;
  color: fn.use-var(text-color, assist);
}

.account-dialog__val {
  display: flex;
  align-items: center;
  gap: fn.use-var(gap, small);
  min-width: 0;
  word-break: break-all;
}

.site-icon {
  width: 40px;
  height: 40px;
  object-fit: contain;
  border-radius: 10px;
  vertical-align: middle;
  flex-shrink: 0;
}

.site-icon--placeholder,
.site-icon--letter {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: fn.use-var(bg-color, middle);
  border: fn.use-var(border);
  color: fn.use-var(text-color, assist);
}

.site-icon--letter {
  font-size: 18px;
  font-weight: 600;
  color: fn.use-var(color, primary);
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

.icon-field__actions {
  display: flex;
  align-items: center;
  gap: fn.use-var(gap, small);
}

.icon-hint {
  margin: 0;
  font-size: fn.use-var(font-size-assist, small);
  color: fn.use-var(text-color, assist);
}

:deep(.u-group-input__item > .u-input) {
  flex: 1;
  min-width: 0;
}
</style>
