import { ref, type Ref } from "vue";

/** Loading for a single async op (toolbar buttons, notification bell, etc.) */
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
 * Per-key mutually exclusive async operation loading (table row action columns, etc.).
 * Only one key may be busy at a time, preventing double clicks.
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

  /** Wraps row actions whose first arg contains `id`, using `row.id` as the busy key automatically */
  function bind<T extends { id: K }>(fn: (row: T) => Promise<void>): (row: T) => Promise<void> {
    return (row) => run(row.id, () => fn(row));
  }

  return { busyKey, isBusy, run, bind };
}
