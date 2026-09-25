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

/** Shallow-copies the query object; keeps `undefined` and re-copies arrays. */
export function snapshotQuery(query: ProTableQuery): ProTableQuery {
  const out: ProTableQuery = {};
  for (const key of Object.keys(query)) {
    const value = query[key];
    out[key] = Array.isArray(value) ? [...value] : value;
  }
  return out;
}

/** Restores `target` in place from a snapshot (removes keys absent from the snapshot). */
export function restoreQuery(target: ProTableQuery, snapshot: ProTableQuery) {
  for (const key of Object.keys(target)) {
    if (!(key in snapshot)) delete target[key];
  }
  for (const key of Object.keys(snapshot)) {
    const value = snapshot[key];
    target[key] = Array.isArray(value) ? [...value] : value;
  }
}
