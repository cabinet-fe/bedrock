<script setup lang="ts">
defineOptions({ name: "ProjectSelect", inheritAttrs: false });

import { http } from "@/api/http";
import type { PageResult, ProductProject } from "@/api/types";

/** 空串拉首页；有关键词走远程搜索（USelect options 函数会强制 filterable） */
async function searchProjects(qs: string): Promise<ProductProject[]> {
  const keyword = qs.trim();
  const { body } = await http.get<PageResult<ProductProject>>("/projects", {
    query: {
      page: 1,
      page_size: 100,
      ...(keyword ? { keyword } : {}),
    },
  });
  return body?.items ?? [];
}
</script>

<template>
  <u-select
    v-bind="$attrs"
    :options="searchProjects"
    value-key="id"
    label-key="name"
    clearable
    placeholder="所属项目"
  />
</template>
