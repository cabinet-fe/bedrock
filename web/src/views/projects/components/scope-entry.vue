<script setup lang="ts">
import { computed, reactive } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { ProductProject } from "@/api/types";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { formatDateTime } from "@/lib/datetime";

const route = useRoute();
const router = useRouter();
const query = reactive({ keyword: "", status: "active" });

const projectTab = computed(() => {
  const tab = route.meta.projectTab;
  if (tab === "docs" || tab === "dev-docs" || tab === "requirements") return tab;
  return "requirements";
});

const actionLabel = computed(() => {
  if (projectTab.value === "docs") return "进入文档";
  if (projectTab.value === "dev-docs") return "进入开发文档";
  return "进入需求";
});

const columns = defineProTableColumns([
  { key: "name", name: "项目", sortable: true },
  { key: "slug", name: "标识" },
  { key: "status", name: "状态", width: 100, align: "center" },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 140, align: "center", fixed: "right" },
]);

function openProject(project: ProductProject) {
  void router.push({
    name: "project-detail",
    params: { id: project.id },
    query: { tab: projectTab.value },
  });
}
</script>

<template>
  <div>
    <ProTable
      url="/projects"
      :query="query"
      :columns="columns"
      pagination
      :auto-query-fields="['status']"
    >
      <template #filters>
        <u-input v-model="query.keyword" placeholder="名称、标识或标签" style="width: 240px" />
        <u-select
          v-model="query.status"
          placeholder="全部状态"
          :options="[
            { label: '全部状态', value: '' },
            { label: '活跃', value: 'active' },
            { label: '已归档', value: 'archived' },
          ]"
          style="width: 130px"
        />
      </template>
      <template #column:name="{ rowData }">
        <u-action @run="openProject(rowData as ProductProject)">
          {{ (rowData as ProductProject).name }}
        </u-action>
      </template>
      <template #column:status="{ rowData }">
        <u-tag
          size="small"
          :type="(rowData as ProductProject).status === 'archived' ? 'warning' : 'success'"
        >
          {{ (rowData as ProductProject).status === "archived" ? "已归档" : "活跃" }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action @run="openProject(rowData as ProductProject)">{{ actionLabel }}</u-action>
      </template>
    </ProTable>
  </div>
</template>
