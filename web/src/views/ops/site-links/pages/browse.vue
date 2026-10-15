<script setup lang="ts">
defineOptions({ name: "SiteLinks" });

import { clipboard } from "@cat-kit/fe";
import { defineTableColumns, message } from "@veltra/desktop";
import { PictureRounded } from "@veltra/icons/normal";
import { onMounted, ref, shallowRef } from "vue";

import { listSiteLinks } from "@/api/ops";
import type { SiteLink } from "@/api/types";

const loading = ref(false);
const items = shallowRef<SiteLink[]>([]);

const columns = defineTableColumns([
  { key: "icon", name: "图标", width: 72, align: "center" },
  { key: "name", name: "名称", minWidth: 140 },
  { key: "url", name: "地址", minWidth: 220 },
  { key: "account", name: "账号", minWidth: 180 },
  { key: "password", name: "密码", minWidth: 180 },
  { key: "remark", name: "备注", minWidth: 140 },
  { key: "enabled", name: "状态", width: 90, align: "center" },
]);

function iconSrc(row: SiteLink): string {
  const raw = row.icon_base64.trim();
  if (!raw) return "";
  return `data:${row.icon_mime || "image/png"};base64,${raw}`;
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

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="site-links">
    <div v-loading="loading" class="site-links__panel">
      <u-table :columns="columns" :data="items" row-key="id">
        <template #column:icon="{ rowData }">
          <img
            v-if="iconSrc(rowData as SiteLink)"
            class="site-icon"
            :src="iconSrc(rowData as SiteLink)"
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
      </u-table>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.site-links {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
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
</style>
