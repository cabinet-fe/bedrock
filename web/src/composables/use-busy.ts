import { ref, type Ref } from "vue";

/** 单个异步操作的 loading（工具栏按钮、通知铃等） */
export function useBusy() {
  const busy = ref(false);

  async function run(fn: () => Promise<void>): Promise<void> {
    if (busy.value) return;
    busy.value = true;
    try {
      await fn();
    } finally {
      busy.value = false;
    }
  }

  return { busy, run };
}

/**
 * 按 key 互斥的异步操作 loading（表格行操作列等）。
 * 同一时刻只允许一个 key 处于 busy，避免连点。
 */
export function useBusyKey<K extends string | number = number>() {
  const busyKey = ref<K | null>(null) as Ref<K | null>;

  function isBusy(key: K): boolean {
    return busyKey.value === key;
  }

  async function run(key: K, fn: () => Promise<void>): Promise<void> {
    if (busyKey.value != null) return;
    busyKey.value = key;
    try {
      await fn();
    } finally {
      busyKey.value = null;
    }
  }

  /** 包装首参含 `id` 的行操作，自动以 `row.id` 作为 busy key */
  function bind<T extends { id: K }>(fn: (row: T) => Promise<void>): (row: T) => Promise<void> {
    return (row) => run(row.id, () => fn(row));
  }

  return { busyKey, isBusy, run, bind };
}
