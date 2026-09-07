<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import { defineTableColumns, UIcon, UTable, UTag } from "@veltra/desktop";
import { CircleClose, Loading } from "@veltra/icons/normal";
import type { ChatToolCall } from "@veltra/ai";

import {
  BUILD_STAGE_TAG,
  buildStageLabel,
  JOB_STATUS_TAG,
  jobStatusLabel,
  type TagType,
  TRIGGER_TYPE_TAG,
  triggerTypeLabel,
} from "@/lib/tag";
import { useAiChatStore } from "@/stores/ai-chat";

const props = defineProps<{
  toolCall: ChatToolCall;
}>();

const chatStore = useAiChatStore();
const router = useRouter();

export interface ColumnDef {
  key: string;
  name: string;
  width?: number;
  minWidth?: number;
  align?: "left" | "center" | "right";
  type?: "text" | "tag" | "link";
  linkKey?: string;
}

export interface TableQueryResult {
  title?: string;
  total?: number;
  columns?: ColumnDef[];
  items?: Record<string, any>[];
  [key: string]: any;
}

const parsedData = computed<TableQueryResult | null>(() => {
  const res = props.toolCall.result;
  if (!res) return null;
  if (typeof res === "object") return res as TableQueryResult;
  if (typeof res === "string") {
    try {
      const parsed = JSON.parse(res);
      if (parsed && typeof parsed === "object") {
        return parsed as TableQueryResult;
      }
    } catch {
      // not json
    }
  }
  return null;
});

const totalCount = computed(() => {
  if (parsedData.value?.total != null) {
    return parsedData.value.total;
  }
  return parsedData.value?.items?.length ?? 0;
});

const tableItems = computed(() => {
  const raw = parsedData.value?.items ?? [];
  return raw.map((item, index) => ({
    id: item.id ?? `row_${index}`,
    ...item,
  }));
});

const activeColumns = computed<ColumnDef[]>(() => {
  return parsedData.value?.columns ?? [];
});

const tableColumns = computed(() => {
  const defs = activeColumns.value;
  if (defs.length > 0) {
    return defineTableColumns(
      defs.map((col) => ({
        key: col.key,
        name: col.name,
        width: col.width,
        minWidth: col.minWidth,
        align: col.align,
      })),
    );
  }
  const items = tableItems.value;
  if (items.length > 0) {
    const keys = Object.keys(items[0]!).filter((k) => k !== "link");
    return defineTableColumns(
      keys.map((k) => ({
        key: k,
        name: k,
      })),
    );
  }
  return [];
});

function isLinkCell(col: ColumnDef, val: unknown, rowData: Record<string, any>): boolean {
  if (col.type === "link") return true;
  if (col.linkKey && rowData[col.linkKey]) return true;
  if (rowData[`${col.key}_link`]) return true;
  if (
    rowData.link &&
    (col.key === "name" || col.key === "id" || col.key === "link" || col.key === "action")
  ) {
    return true;
  }
  if (
    typeof val === "string" &&
    (val.startsWith("/") || val.startsWith("http://") || val.startsWith("https://"))
  ) {
    return true;
  }
  return false;
}

function getLinkUrl(col: ColumnDef, val: unknown, rowData: Record<string, any>): string {
  if (col.linkKey && rowData[col.linkKey]) return String(rowData[col.linkKey]);
  if (rowData[`${col.key}_link`]) return String(rowData[`${col.key}_link`]);
  if (
    rowData.link &&
    (col.key === "name" || col.key === "id" || col.key === "link" || col.key === "action")
  ) {
    return String(rowData.link);
  }
  if (
    typeof val === "string" &&
    (val.startsWith("/") || val.startsWith("http://") || val.startsWith("https://"))
  ) {
    return val;
  }
  return "";
}

function getLinkLabel(col: ColumnDef, val: unknown): string {
  if (val != null && val !== "" && typeof val !== "object") {
    const s = String(val);
    if (s.startsWith("/")) {
      return "查看详情";
    }
    return s;
  }
  return "查看详情";
}

function isTagCell(col: ColumnDef, val: unknown): boolean {
  if (col.type === "tag") return true;
  if (typeof val === "boolean") return true;
  if (["status", "enabled", "stage", "trigger_type", "auth_type", "type"].includes(col.key)) {
    return true;
  }
  return false;
}

function resolveTagType(col: ColumnDef, val: unknown): TagType {
  if (val == null || val === "" || val === "—") return undefined;
  if (typeof val === "boolean") {
    return val ? "success" : undefined;
  }
  const str = String(val).toLowerCase().trim();

  if (JOB_STATUS_TAG[str]) return JOB_STATUS_TAG[str];
  if (TRIGGER_TYPE_TAG[str]) return TRIGGER_TYPE_TAG[str];
  if (BUILD_STAGE_TAG[str]) return BUILD_STAGE_TAG[str];

  if (["active", "success", "online", "enabled", "已启用", "正常", "激活"].includes(str)) {
    return "success";
  }
  if (["failed", "danger", "error", "offline", "离线", "失败"].includes(str)) {
    return "danger";
  }
  if (["disabled", "已禁用", "禁用"].includes(str)) {
    return undefined;
  }
  if (["running", "运行中", "构建中", "cloning", "building"].includes(str)) {
    return "primary";
  }
  if (["warning", "warn", "cancelled", "interrupted", "取消", "中断", "已取消"].includes(str)) {
    return "warning";
  }
  if (
    [
      "archived",
      "queued",
      "pending",
      "等待",
      "排队",
      "已归档",
      "token",
      "password",
      "ssh_key",
    ].includes(str)
  ) {
    return "info";
  }

  return undefined;
}

function resolveTagLabel(val: unknown): string {
  if (val == null || val === "") return "—";
  if (typeof val === "boolean") {
    return val ? "已启用" : "已禁用";
  }
  const str = String(val);
  const jl = jobStatusLabel(str);
  if (jl && jl !== str) return jl;
  const tl = triggerTypeLabel(str);
  if (tl && tl !== str) return tl;
  const bl = buildStageLabel(str);
  if (bl && bl !== str) return bl;

  if (str === "active") return "已激活";
  if (str === "archived") return "已归档";
  if (str === "online") return "在线";
  if (str === "offline") return "离线";
  return str;
}

function formatCellValue(val: unknown): string {
  if (val == null || val === "") return "—";
  if (typeof val === "object") {
    try {
      return JSON.stringify(val);
    } catch {
      return String(val);
    }
  }
  return String(val);
}

function onLinkClick(e: MouseEvent, url: string, rowData?: Record<string, any>) {
  if (e.metaKey || e.ctrlKey) {
    return;
  }
  e.preventDefault();
  handleLinkNavigation(url, rowData);
}

function handleLinkNavigation(url: string, rowData?: Record<string, any>) {
  if (!url) return;

  // 1. 构建运行链接：联动打开右侧抽屉面板
  const buildMatch = url.match(/\/cicd\/build-runs\/(\d+)/);
  if (buildMatch && buildMatch[1]) {
    const runId = Number(buildMatch[1]);
    const jobId = rowData?.build_job_id ?? rowData?.job_id;
    chatStore.openRightPanel({
      type: "build",
      id: runId,
      title: jobId ? `构建运行 #${runId} · 任务 #${jobId}` : `构建运行 #${runId}`,
    });
    return;
  }

  // 2. 流水线运行链接：联动打开右侧抽屉面板
  const pipelineMatch = url.match(/\/cicd\/pipeline-runs\/(\d+)/);
  if (pipelineMatch && pipelineMatch[1]) {
    const runId = Number(pipelineMatch[1]);
    const pipelineId = rowData?.pipeline_id ?? rowData?.build_pipeline_id;
    chatStore.openRightPanel({
      type: "pipeline",
      id: runId,
      title: pipelineId ? `流水线运行 #${runId} · 流水线 #${pipelineId}` : `流水线运行 #${runId}`,
    });
    return;
  }

  // 3. 外部链接在新窗口打开
  if (url.startsWith("http://") || url.startsWith("https://")) {
    window.open(url, "_blank");
    return;
  }

  // 4. 常规页面跳转：切回经典模式并在主工作区跳转对应路由
  void chatStore.toggleAiMode(false);
  void router.push(url);
}
</script>

<template>
  <div class="query-table-card">
    <!-- 1. 正在查询 (running / pending) -->
    <div
      v-if="toolCall.status === 'running' || toolCall.status === 'pending'"
      class="query-status-box is-running"
    >
      <u-icon :size="15" class="is-loading">
        <Loading />
      </u-icon>
      <span>正在查询数据...</span>
    </div>

    <!-- 2. 查询失败 (error) -->
    <div v-else-if="toolCall.status === 'error'" class="query-status-box is-error">
      <u-icon :size="15" class="danger-icon">
        <CircleClose />
      </u-icon>
      <span>查询失败：{{ toolCall.error || "未知错误" }}</span>
    </div>

    <!-- 3. 执行成功并解析到结构化数据 -->
    <div v-else-if="parsedData" class="query-table-container">
      <div class="query-table-header">
        <span class="table-title">{{ parsedData.title || "查询结果" }}</span>
        <span class="table-total">共 {{ totalCount }} 条数据</span>
      </div>

      <div class="query-table-body">
        <u-table :columns="tableColumns" :data="tableItems" row-key="id" size="small" border stripe>
          <template
            v-for="col of activeColumns"
            :key="col.key"
            #[`column:${col.key}`]="{ rowData, val }"
          >
            <!-- 链接字段 -->
            <a
              v-if="isLinkCell(col, val, rowData)"
              :href="getLinkUrl(col, val, rowData)"
              class="query-table-link"
              @click="onLinkClick($event, getLinkUrl(col, val, rowData), rowData)"
            >
              {{ getLinkLabel(col, val) }}
            </a>

            <!-- 状态 / Tag 字段 -->
            <u-tag v-else-if="isTagCell(col, val)" size="small" :type="resolveTagType(col, val)">
              {{ resolveTagLabel(val) }}
            </u-tag>

            <!-- 普通文本 -->
            <span v-else class="query-table-text">
              {{ formatCellValue(val) }}
            </span>
          </template>
        </u-table>
      </div>
    </div>

    <!-- 4. Fallback：非标准 JSON 结果降级展示 -->
    <div v-else-if="toolCall.result" class="query-fallback-box">
      <pre class="query-fallback-text">{{ toolCall.result }}</pre>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.query-table-card {
  padding: 4px 0;
  font-size: 13px;
  width: 100%;
}

.query-status-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-radius: fn.use-var(radius, medium);
  border: 1px solid transparent;

  &.is-running {
    background: color-mix(in srgb, fn.use-var(color, primary) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, primary) 25%, transparent);
    color: fn.use-var(color, primary);
  }

  &.is-error {
    background: color-mix(in srgb, fn.use-var(color, danger) 8%, transparent);
    border-color: color-mix(in srgb, fn.use-var(color, danger) 25%, transparent);
    color: fn.use-var(color, danger);
  }
}

.query-table-container {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}

.query-table-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2px;
}

.table-title {
  font-weight: 600;
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.table-total {
  font-size: 12px;
  color: fn.use-var(text-color, description);
}

.query-table-body {
  width: 100%;
  overflow: hidden;
  border-radius: fn.use-var(radius, medium);
}

.query-table-link {
  color: fn.use-var(color, primary);
  text-decoration: none;
  cursor: pointer;
  word-break: break-all;

  &:hover {
    text-decoration: underline;
  }
}

.query-table-text {
  word-break: break-all;
}

.query-fallback-box {
  background: color-mix(in srgb, fn.use-var(bg-color, bottom) 50%, transparent);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 40%, transparent);
  border-radius: fn.use-var(radius, medium);
  padding: 8px 12px;
}

.query-fallback-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
}
</style>
