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

/** CI 运行通用终态判定：queued / pending / running 之外均为终态 */
export const isRunTerminal = (status: string): boolean =>
  !["queued", "pending", "running"].includes(status);

/**
 * 「只运行」卡片的运行状态管理：入队后立即反映返回的 run 状态，
 * 非终态按 entityId 轮询至终态；同一实体重复触发时以最新一次为准。
 */
export function useRunPoll(options: UseRunPollOptions) {
  const interval = options.interval ?? 2000;
  /** entityId → 最近运行状态 */
  const statusMap = ref(new Map<number, string>());
  /** entityId → 入队失败信息（卡内反馈） */
  const errorMap = ref(new Map<number, string>());
  /** 入队请求进行中的实体 */
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
          /* 单次拉取失败留下一轮重试 */
        }
      })();
    }, interval);
    timers.set(entityId, timer);
  }

  /** 入队中，或最近 run 仍在排队/运行 */
  function isBusy(entityId: number): boolean {
    if (pendingSet.value.has(entityId)) return true;
    const status = statusMap.value.get(entityId);
    return !!status && !options.isTerminal(status);
  }

  /** 触发运行：入队成功即更新状态并轮询；失败写入卡内错误，不弹 toast */
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

  /** 初始最近状态：按 entityId 取首个（最新）run；进行中的 run 继续轮询 */
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
