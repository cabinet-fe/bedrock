import { defineTableColumns, type TableColumn } from "@veltra/desktop";

export type ProTableQuery = Record<string, unknown>;

/** Column config; `sortable` adds a header sort control (UTable has no built-in sort). */
export type ProTableColumn = TableColumn & {
  sortable?: boolean;
  children?: ProTableColumn[];
};

export function defineProTableColumns(
  columns: ProTableColumn[],
  commonProps?: Partial<Pick<TableColumn, "align" | "minWidth">>,
): ProTableColumn[] {
  return defineTableColumns(columns as TableColumn[], commonProps) as ProTableColumn[];
}

/** 浅拷贝查询对象；保留 `undefined`，数组另拷一份。 */
export function snapshotQuery(query: ProTableQuery): ProTableQuery {
  const out: ProTableQuery = {};
  for (const key of Object.keys(query)) {
    const value = query[key];
    out[key] = Array.isArray(value) ? [...value] : value;
  }
  return out;
}

/** 将 `target` 原地恢复为快照（删除快照中不存在的键）。 */
export function restoreQuery(target: ProTableQuery, snapshot: ProTableQuery) {
  for (const key of Object.keys(target)) {
    if (!(key in snapshot)) delete target[key];
  }
  for (const key of Object.keys(snapshot)) {
    const value = snapshot[key];
    target[key] = Array.isArray(value) ? [...value] : value;
  }
}
