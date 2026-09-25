<script setup lang="ts">
defineOptions({ name: "SystemResources" });

import { ref } from "vue";

import MenuGroupsPanel from "../components/menu-groups-panel.vue";
import ResourcesPanel from "../components/resources-panel.vue";

const tab = ref("resources");
const tabs = [
  { key: "resources", name: "菜单与功能" },
  { key: "groups", name: "菜单分组" },
];
</script>

<template>
  <u-tabs v-model="tab" :items="tabs" class="resources-page">
    <template #resources>
      <ResourcesPanel class="resources-page__panel" />
    </template>

    <template #groups>
      <MenuGroupsPanel class="resources-page__panel" />
    </template>
  </u-tabs>
</template>

<style scoped lang="scss">
.resources-page {
  height: 100%;
  min-height: 0;

  /* UTabs' single root slot is not wrapped in .u-tabs__content, so fill the remaining height ourselves */
  :deep(.u-scroll__content) {
    height: 100%;
  }
}

.resources-page__panel {
  height: 100%;
}
</style>
