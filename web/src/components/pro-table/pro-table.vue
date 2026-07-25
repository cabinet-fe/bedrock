<script setup lang="ts" generic="T extends Record<string, any>">
import { sleep } from "@cat-kit/core";
import { computed, h, onMounted, ref, useSlots, watch } from "vue";
import {
  message,
  UButton,
  type TableColumn,
  type TableColumnNode,
  vLoading,
} from "@veltra/desktop";
import { ArrowDown, ArrowUp, ArrowUpdown } from "@veltra/icons/normal";

import { http } from "@/api/http";

import type { ProTableColumn, ProTableQuery } from "./helper";

type SortOrder = "asc" | "desc";

/** loading 最短展示，避免请求过快时闪烁 */
const MIN_LOADING_MS = 250;

const props = withDefaults(
  defineProps<{
    /** API path relative to `/api/v1` (e.g. `/users`) */
    url: string;
    columns: ProTableColumn[];
    /**
     * Filter / sort query object owned by the parent.
     * One-way prop: ProTable reads and mutates fields in place (no `update:query`).
     * Sort is written as `sort: "<field>@asc" | "<field>@desc"` and omitted when cleared.
     */
    query?: ProTableQuery;
    /** Fields that auto-trigger search when changed (selects, etc.) */
    autoQueryFields?: string[];
    /** Enable pagination (mutually exclusive with `tree`) */
    pagination?: boolean;
    /** Enable tree table (mutually exclusive with `pagination`). */
    tree?: boolean;
    rowKey?: string;
    /** Table area height; default fills remaining space */
    height?: string;
    /** Load on mount */
    immediate?: boolean;
    /** Expand all tree nodes by default */
    defaultExpandAll?: boolean;
    /** Enable row checkboxes (UTable checkable) */
    checkable?: boolean;
  }>(),
  {
    query: () => ({}),
    autoQueryFields: () => [],
    pagination: false,
    tree: false,
    rowKey: "id",
    height: "100%",
    immediate: true,
    defaultExpandAll: false,
    checkable: false,
  },
);

const checked = defineModel<T[]>("checked", { default: () => [] });

const emit = defineEmits<{
  loaded: [items: T[]];
}>();

const slots = useSlots();
const loading = ref(false);
const items = ref<T[]>([]);
const page = ref(1);
const pageSize = ref(100);
const total = ref(0);
/** 与后端 ParsePage maxPageSize=100 对齐，避免选项超出后被回写打回 */
const pageSizeOptions = [20, 50, 100];

const mode = computed(() => {
  if (props.pagination) return "pagination" as const;
  if (props.tree) return "tree" as const;
  return "list" as const;
});

function parseSort(value: unknown): { field: string; order: SortOrder | null } {
  if (typeof value !== "string" || !value.includes("@")) {
    return { field: "", order: null };
  }
  const at = value.lastIndexOf("@");
  const field = value.slice(0, at);
  const order = value.slice(at + 1);
  if (!field || (order !== "asc" && order !== "desc")) {
    return { field: "", order: null };
  }
  return { field, order };
}

function cycleSort(field: string) {
  const current = parseSort(props.query?.sort);
  let next: string | undefined;
  if (current.field !== field || !current.order) {
    next = `${field}@desc`;
  } else if (current.order === "desc") {
    next = `${field}@asc`;
  } else {
    next = undefined;
  }

  if (next) {
    props.query.sort = next;
  } else {
    delete props.query.sort;
  }
  void search();
}

function mapColumns(cols: ProTableColumn[]): TableColumn[] {
  const { field: sortField, order: sortOrder } = parseSort(props.query?.sort);

  return cols.map((col) => {
    const children = col.children?.length
      ? mapColumns(col.children as ProTableColumn[])
      : undefined;

    if (!col.sortable) {
      return children ? { ...col, children } : col;
    }

    const originalNameRender = col.nameRender;
    return {
      ...col,
      children,
      nameRender: (ctx: { column: TableColumnNode }) => {
        const label = originalNameRender?.(ctx) ?? col.name;
        const active = sortField === col.key && !!sortOrder;
        const Icon =
          active && sortOrder === "asc"
            ? ArrowUp
            : active && sortOrder === "desc"
              ? ArrowDown
              : ArrowUpdown;

        return h(
          "span",
          {
            class: ["pro-table__th", active && "is-sorted"],
            onClick: (e: MouseEvent) => {
              e.stopPropagation();
              cycleSort(col.key);
            },
          },
          [
            h("span", { class: "pro-table__th-label" }, label as any),
            h(UButton, {
              text: true,
              circle: true,
              icon: Icon,
              iconSize: 14,
              class: ["pro-table__sort-btn", active && "is-active"],
              // Visual indicator only; parent span owns the click cycle.
              tabindex: -1,
              style: { pointerEvents: "none" },
            }),
          ],
        );
      },
    };
  });
}

const resolvedColumns = computed(() => mapColumns(props.columns));

function cleanQuery(params: Record<string, unknown>): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "") continue;
    if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
      out[k] = v;
    }
  }
  return out;
}

function extractItems(body: Record<string, unknown>): T[] {
  return Array.isArray(body.items) ? (body.items as T[]) : [];
}

function applyPaginationMeta(body: Record<string, unknown>) {
  if (typeof body.total === "number") total.value = body.total;
  if (typeof body.page === "number") page.value = body.page;
  if (typeof body.page_size === "number") pageSize.value = body.page_size;
}

async function load() {
  loading.value = true;
  const started = Date.now();
  try {
    const params: Record<string, unknown> = { ...props.query };
    if (mode.value === "pagination") {
      params.page = page.value;
      params.page_size = pageSize.value;
    }

    // Envelope plugin unwraps `{ code, message, data }` → body is `data`.
    const { body: raw } = await http.get(props.url, { query: cleanQuery(params) });
    const body = (raw ?? {}) as Record<string, unknown>;

    const list = extractItems(body);
    items.value = list;

    if (mode.value === "pagination") {
      applyPaginationMeta(body);
    } else {
      total.value = list.length;
    }

    emit("loaded", list);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "加载失败");
    items.value = [];
    total.value = 0;
  } finally {
    const remain = MIN_LOADING_MS - (Date.now() - started);
    if (remain > 0) await sleep(remain);
    loading.value = false;
  }
}

/** Reset to page 1 and fetch (manual search / Enter / submit / sort). */
function search() {
  if (loading.value) return Promise.resolve();
  page.value = 1;
  return load();
}

/** Re-fetch current page (after create/update/delete). */
function reload() {
  return load();
}

function onPageSizeChange(size: number) {
  if (typeof size === "number" && size > 0) {
    pageSize.value = size;
  }
  page.value = 1;
  void load();
}

watch(
  () =>
    props.autoQueryFields
      .map((key) => `${key}:${JSON.stringify(props.query?.[key] ?? null)}`)
      .join("|"),
  () => {
    if (!props.autoQueryFields.length) return;
    page.value = 1;
    void load();
  },
);

watch(
  () => props.url,
  () => {
    page.value = 1;
    void load();
  },
);

onMounted(() => {
  if (props.immediate) void load();
});

defineExpose({ search, reload });
</script>

<template>
  <div class="pro-table">
    <form class="pro-table__toolbar" @submit.prevent="search">
      <div class="pro-table__filters">
        <slot name="filters" :search="search" :reload="reload" :query="query" />
      </div>
      <u-button type="primary" class="pro-table__search" :loading="loading" @click="search">
        查询
      </u-button>
    </form>

    <div class="pro-table__panel" :style="height !== '100%' ? { height, flex: 'none' } : undefined">
      <div v-loading="loading" class="pro-table__body">
        <u-table
          v-model:checked="checked"
          :columns="resolvedColumns"
          :data="items"
          :border="false"
          :row-key="rowKey"
          :tree="tree"
          :default-expand-all="defaultExpandAll"
          :stripe="mode !== 'tree'"
          :checkable="checkable"
        >
          <template v-for="(_, name) in slots" :key="name" #[name]="slotData">
            <slot
              v-if="name !== 'filters' && name !== 'empty'"
              :name="name"
              v-bind="slotData || {}"
            />
          </template>
        </u-table>
      </div>

      <div v-if="mode === 'pagination'" class="pro-table__footer">
        <u-paginator
          v-model:page-number="page"
          v-model:page-size="pageSize"
          :page-size-options="pageSizeOptions"
          :total="total"
          @change:page-number="load"
          @change:page-size="onPageSizeChange"
        />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.pro-table {
  display: flex;
  flex-direction: column;
  gap: fn.use-var(gap, default);
  height: 100%;
  flex: 1;
  min-height: 0;
}

.pro-table__toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: flex-end;
  gap: fn.use-var(gap, default);
  padding: 0 0 fn.use-var(gap, small);
}

.pro-table__filters {
  display: flex;
  flex-wrap: wrap;
  gap: fn.use-var(gap, small);
  align-items: flex-end;
  flex: 1;
  min-width: 0;
}

.pro-table__search {
  flex-shrink: 0;
}

.pro-table__panel {
  flex: 1;
  min-height: 280px;
  display: flex;
  flex-direction: column;
  min-width: 0;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
  overflow: hidden;
}

.pro-table__body {
  flex: 1;
  min-height: 0;
  position: relative;
}

.pro-table__body :deep(.u-table) {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100% !important;
}

.pro-table__footer {
  flex-shrink: 0;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  padding: fn.use-var(gap, small) 0 0;
  border-top: fn.use-var(border, muted);
}

.pro-table__th {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  max-width: 100%;
  cursor: pointer;
  user-select: none;
}

.pro-table__th-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.pro-table__th.is-sorted .pro-table__th-label {
  color: fn.use-var(color, primary);
}

.pro-table__sort-btn {
  flex-shrink: 0;
  color: fn.use-var(text-color, assist);
  opacity: 0.7;
}

.pro-table__sort-btn.is-active {
  color: fn.use-var(color, primary);
  opacity: 1;
}

.pro-table__th:hover .pro-table__sort-btn {
  opacity: 1;
}
</style>
