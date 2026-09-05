import { computed, shallowRef } from "vue";
import { defineStore } from "pinia";

import { listRepositories } from "@/api/resource";
import type { Repository } from "@/api/types";

/** 与后端 ParsePage maxPageSize 对齐 */
const PAGE_SIZE = 100;

export const useRepositoryStore = defineStore("repositories", () => {
  const items = shallowRef<Repository[]>([]);
  let loaded = false;
  let inflight: Promise<void> | null = null;

  const nameMap = computed(() => {
    const map = new Map<number, string>();
    for (const repo of items.value) map.set(repo.id, repo.name);
    return map;
  });

  const repoMap = computed(() => {
    const map = new Map<number, Repository>();
    for (const repo of items.value) map.set(repo.id, repo);
    return map;
  });

  async function fetchAll(): Promise<Repository[]> {
    const all: Repository[] = [];
    let page = 1;
    for (;;) {
      const res = await listRepositories({ page, page_size: PAGE_SIZE });
      const batch = res.items ?? [];
      all.push(...batch);
      if (batch.length < PAGE_SIZE || all.length >= res.total) break;
      page++;
    }
    return all;
  }

  async function load(force = false): Promise<void> {
    if (!force && loaded) return;
    if (inflight) return inflight;
    inflight = (async () => {
      try {
        items.value = await fetchAll();
        loaded = true;
      } catch {
        if (!loaded) items.value = [];
      }
    })().finally(() => {
      inflight = null;
    });
    return inflight;
  }

  /** 增删改后刷新；从未加载过则跳过，避免无订阅方时白打列表 */
  function refresh(): Promise<void> {
    if (!loaded && !inflight) return Promise.resolve();
    loaded = false;
    inflight = null;
    return load(true);
  }

  return { items, nameMap, repoMap, load, refresh };
});
