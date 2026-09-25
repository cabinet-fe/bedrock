import { onScopeDispose, ref } from "vue";

export interface TrackedRun {
  entityId: number;
  runId: number;
  status: string;
}

export interface UseRunPollOptions {
  fetch: (runId: number) => Promise<{ status: string }>;
  isTerminal: (status: string) => boolean;
  interval?: number;
}

/** Generic CI run terminal-state check: anything but queued / pending / running is terminal */
export const isRunTerminal = (status: string): boolean =>
  !["queued", "pending", "running"].includes(status);

/**
 * Run status management for "run only" cards: reflects the returned run state immediately after enqueue,
 * polls by entityId until a terminal state; repeated triggers of the same entity use the latest one.
 */
export function useRunPoll(options: UseRunPollOptions) {
  const interval = options.interval ?? 2000;
  /** entityId → latest run status */
  const statusMap = ref(new Map<number, string>());
  /** entityId → enqueue failure (feedback inside the card) */
  const errorMap = ref(new Map<number, string>());
  /** Entities with an enqueue request in flight */
  const pendingSet = ref(new Set<number>());
  const timers = new Map<number, ReturnType<typeof setInterval>>();

  function stopPoll(entityId: number) {
    const timer = timers.get(entityId);
    if (timer) {
      clearInterval(timer);
      timers.delete(entityId);
    }
  }

  function setStatus(entityId: number, status: string) {
    const next = new Map(statusMap.value);
    next.set(entityId, status);
    statusMap.value = next;
  }

  function track(entityId: number, runId: number, status: string) {
    setStatus(entityId, status);
    stopPoll(entityId);
    if (options.isTerminal(status)) return;
    const timer = setInterval(() => {
      void (async () => {
        try {
          const run = await options.fetch(runId);
          setStatus(entityId, run.status);
          if (options.isTerminal(run.status)) stopPoll(entityId);
        } catch {
          /* A single fetch failure is left for the next retry round */
        }
      })();
    }, interval);
    timers.set(entityId, timer);
  }

  /** Queued, or the latest run is still queued/running */
  function isBusy(entityId: number): boolean {
    if (pendingSet.value.has(entityId)) return true;
    const status = statusMap.value.get(entityId);
    return !!status && !options.isTerminal(status);
  }

  /** Trigger a run: on successful enqueue update status and poll; failures write card errors without a toast */
  async function enqueue(entityId: number, run: () => Promise<{ id: number; status: string }>) {
    if (isBusy(entityId)) return;
    pendingSet.value = new Set(pendingSet.value).add(entityId);
    const nextErr = new Map(errorMap.value);
    nextErr.delete(entityId);
    errorMap.value = nextErr;
    try {
      const created = await run();
      track(entityId, created.id, created.status);
    } catch (error) {
      const failed = new Map(errorMap.value);
      failed.set(entityId, error instanceof Error ? error.message : "触发失败");
      errorMap.value = failed;
    } finally {
      const next = new Set(pendingSet.value);
      next.delete(entityId);
      pendingSet.value = next;
    }
  }

  /** Initial latest status: first (newest) run per entityId; in-flight runs keep polling */
  function loadRecent(runs: TrackedRun[]) {
    for (const run of runs) {
      if (statusMap.value.has(run.entityId)) continue;
      track(run.entityId, run.runId, run.status);
    }
  }

  onScopeDispose(() => {
    for (const entityId of timers.keys()) stopPoll(entityId);
  });

  return { statusMap, errorMap, pendingSet, isBusy, enqueue, loadRecent };
}
