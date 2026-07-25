<script setup lang="ts">
defineOptions({ name: "HelpHandbook" });

import { ref } from "vue";

import MarkdownViewer from "@/components/markdown-viewer";
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
      <MarkdownViewer :content="section.content" />
    </template>
  </u-tabs>
</template>

<style scoped lang="scss">
.handbook {
  height: 100%;
  min-height: 0;
}
</style>
