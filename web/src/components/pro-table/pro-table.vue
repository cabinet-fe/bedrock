<script setup lang="ts" generic="T extends Record<string, any>">
import { sleep } from "@cat-kit/core";
import { computed, h, onMounted, onScopeDispose, ref, shallowRef, useSlots, watch } from "vue";
import {
  message,
  UButton,
  type TableColumn,
  type TableColumnNode,
  vLoading,
} from "@veltra/desktop";
import { ArrowDown, ArrowUp, ArrowUpdown, Refresh, Search } from "@veltra/icons/normal";

import { http } from "@/api/http";

import { restoreQuery, snapshotQuery, type ProTableColumn, type ProTableQuery } from "./helper";

type SortOrder = "asc" | "desc";

/** 短请求不闪 loading；超过后再展示 */
const LOADING_DELAY_MS = 160;
/** loading 一旦展示，最短可见时间，避免一闪而过 */
const MIN_VISIBLE_MS = 200;

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
const showLoading = ref(false);
const items = shallowRef<T[]>([]);
const page = ref(1);
const pageSize = ref(100);
const total = ref(0);
/** 与后端 ParsePage maxPageSize=100 对齐，避免选项超出后被回写打回 */
const pageSizeOptions = [20, 50, 100];

let loadGen = 0;
let loadingDelayTimer: ReturnType<typeof setTimeout> | undefined;
/** 重置时跳过 autoQueryFields watch，避免与 search 重复请求 */
let suppressAutoQuery = false;
const initialQuery = snapshotQuery(props.query);

onScopeDispose(() => {
  if (loadingDelayTimer) clearTimeout(loadingDelayTimer);
});

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
  const gen = ++loadGen;
  if (loadingDelayTimer) clearTimeout(loadingDelayTimer);

  let becameVisible = showLoading.value;
  if (!becameVisible) {
    loadingDelayTimer = setTimeout(() => {
      if (gen !== loadGen) return;
      showLoading.value = true;
      becameVisible = true;
    }, LOADING_DELAY_MS);
  }

  const started = Date.now();
  try {
    const params: Record<string, unknown> = { ...props.query };
    if (mode.value === "pagination") {
      params.page = page.value;
      params.page_size = pageSize.value;
    }

    // Envelope plugin unwraps `{ code, message, data }` → body is `data`.
    const { body: raw } = await http.get(props.url, { query: cleanQuery(params) });
    if (gen !== loadGen) return;

    const body = (raw ?? {}) as Record<string, unknown>;
    const list = extractItems(body);
    // 直接替换，不清空旧 rows，避免闪空白
    items.value = list;

    if (mode.value === "pagination") {
      applyPaginationMeta(body);
    } else {
      total.value = list.length;
    }

    emit("loaded", list);
  } catch (err) {
    if (gen !== loadGen) return;
    message.error(err instanceof Error ? err.message : "加载失败");
    items.value = [];
    total.value = 0;
  } finally {
    if (loadingDelayTimer) {
      clearTimeout(loadingDelayTimer);
      loadingDelayTimer = undefined;
    }
  }

  if (gen !== loadGen) return;

  if (becameVisible || showLoading.value) {
    const visibleFor = Date.now() - started - LOADING_DELAY_MS;
    const remain = MIN_VISIBLE_MS - Math.max(0, visibleFor);
    if (remain > 0) await sleep(remain);
    if (gen !== loadGen) return;
  }
  showLoading.value = false;
}

/** Reset to page 1 and fetch (manual search / Enter / submit / sort). */
function search() {
  page.value = 1;
  return load();
}

/** 恢复查询条件为初始值并重新请求 */
function reset() {
  suppressAutoQuery = true;
  restoreQuery(props.query, initialQuery);
  return search().finally(() => {
    suppressAutoQuery = false;
  });
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
    if (suppressAutoQuery || !props.autoQueryFields.length) return;
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

defineExpose({ search, reload, reset });
</script>

<template>
  <div class="pro-table">
    <form class="pro-table__toolbar" @submit.prevent="search">
      <!-- UButton 固定 type=button，隐式提交按钮承接输入框回车 -->
      <button class="pro-table__implicit-submit" type="submit" tabindex="-1" aria-hidden="true">
        查询
      </button>
      <div class="pro-table__search-group">
        <div class="pro-table__filters">
          <slot name="filters" :search="search" :reload="reload" :reset="reset" :query="query" />
        </div>
        <div class="pro-table__query-actions">
          <u-button type="primary" :icon="Search" :loading="showLoading" @click="search">
            查询
          </u-button>
          <u-button :icon="Refresh" :disabled="showLoading" @click="reset"> 重置 </u-button>
        </div>
      </div>
      <div class="pro-table__actions">
        <slot name="toolbar" :search="search" :reload="reload" :reset="reset" :query="query" />
      </div>
    </form>

    <div class="pro-table__panel" :style="height !== '100%' ? { height, flex: 'none' } : undefined">
      <div v-loading="showLoading" class="pro-table__body">
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
              v-if="name !== 'filters' && name !== 'toolbar' && name !== 'empty'"
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

<style scoped lang="scss" src="./pro-table.scss"></style>
