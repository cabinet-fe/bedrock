import { getDictionaryByCode } from "@/api/system";

export type DictOption = { label: string; value: string };

const memo = new Map<string, DictOption[]>();
const inflight = new Map<string, Promise<DictOption[]>>();

/** 拉取启用中的字典项为 select options；同 code 去重并缓存。 */
export function loadDictOptions(code: string): Promise<DictOption[]> {
  const cached = memo.get(code);
  if (cached) return Promise.resolve(cached);
  const pending = inflight.get(code);
  if (pending) return pending;

  const request = getDictionaryByCode(code)
    .then((dict) => {
      const opts = (dict.items ?? [])
        .filter((it) => it.enabled)
        .map((it) => ({ label: it.label, value: it.value }));
      memo.set(code, opts);
      return opts;
    })
    .finally(() => {
      inflight.delete(code);
    });
  inflight.set(code, request);
  return request;
}

/** 字典保存后清缓存，避免下拉仍用旧项。 */
export function clearDictOptionsCache(code?: string) {
  if (code) {
    memo.delete(code);
    inflight.delete(code);
    return;
  }
  memo.clear();
  inflight.clear();
}
