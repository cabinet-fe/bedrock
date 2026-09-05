<script setup lang="ts">
defineOptions({ name: "RepoSelect", inheritAttrs: false });

import { onMounted, ref } from "vue";

import { loadDictOptions } from "@/lib/dict";
import { repoTagType, splitCommaTags } from "@/lib/tag";
import { useRepositoryStore } from "@/stores/repositories";

const repoStore = useRepositoryStore();
const typeLabelMap = ref(new Map<string, string>());

function typeLabel(value: string) {
  return typeLabelMap.value.get(value) ?? value;
}

onMounted(() => {
  void repoStore.load();
  void loadDictOptions("repo_type")
    .then((opts) => {
      const map = new Map<string, string>();
      for (const opt of opts) map.set(opt.value, opt.label);
      typeLabelMap.value = map;
    })
    .catch(() => {
      /* 标签降级为原始值 */
    });
});
</script>

<template>
  <u-select
    placeholder="选择仓库"
    clearable
    min-width="280px"
    v-bind="$attrs"
    filterable
    :options="repoStore.items"
    value-key="id"
    label-key="name"
  >
    <template #default="{ option }">
      <span class="repo-select__option">
        <span class="repo-select__name">{{ option?.name }}</span>
        <span class="repo-select__tags">
          <u-tag
            v-for="tag in splitCommaTags(option?.tags)"
            :key="tag"
            size="small"
            :type="repoTagType(tag)"
          >
            {{ typeLabel(tag) }}
          </u-tag>
        </span>
      </span>
    </template>
  </u-select>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.repo-select__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: fn.use-var(gap, small);
  min-width: 0;
}

.repo-select__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.repo-select__tags {
  display: flex;
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 4px;
  justify-content: flex-end;
}
</style>
