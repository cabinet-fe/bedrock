<script setup lang="ts">
defineOptions({ name: "HelpHandbook" });

import { ref } from "vue";

import { MarkdownScrollPane } from "@/components/markdown-viewer";
import { handbookSections } from "@/content/handbook/manifest";

const activeKey = ref(handbookSections[0]?.key ?? "");

const navItems = handbookSections.map((s) => ({
  key: s.key,
  name: s.title,
}));
</script>

<template>
  <u-tabs v-model="activeKey" class="handbook" :items="navItems" position="left">
    <template v-for="section in handbookSections" :key="section.key" #[section.key]>
      <MarkdownScrollPane class="handbook__pane" :content="section.content" />
    </template>
  </u-tabs>
</template>

<style scoped lang="scss">
.handbook {
  height: 100%;
  min-height: 0;

  /* UTabs 单根插槽不会包 .u-tabs__content，需自行占满剩余高度 */
  :deep(.u-scroll__content) {
    height: 100%;
  }
}

.handbook__pane {
  height: 100%;
  min-height: 0;
}
</style>
