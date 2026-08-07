<script setup lang="ts">
import { useTemplateRef } from "vue";
import type { ScrollExposed } from "@veltra/desktop";
import { Backtop, Down } from "@veltra/icons/normal";

import MarkdownViewer from "./markdown-viewer.vue";

defineProps<{
  content: string;
}>();

const scroll = useTemplateRef<ScrollExposed>("scroll");

function scrollToEdge(edge: "top" | "bottom") {
  scroll.value?.scrollTo({
    y: edge === "top" ? 0 : Number.MAX_SAFE_INTEGER,
  });
}
</script>

<template>
  <div class="markdown-scroll-pane">
    <u-scroll ref="scroll" class="markdown-scroll-pane__scroll">
      <MarkdownViewer :content="content" />
    </u-scroll>
    <div class="markdown-scroll-pane__actions">
      <u-button size="small" circle :icon="Backtop" title="回顶部" @click="scrollToEdge('top')" />
      <u-button size="small" circle :icon="Down" title="去底部" @click="scrollToEdge('bottom')" />
    </div>
  </div>
</template>

<style scoped lang="scss">
.markdown-scroll-pane {
  position: relative;
  min-width: 0;
  min-height: 0;
  height: 100%;
}

.markdown-scroll-pane__scroll {
  width: 100%;
  height: 100%;
}

.markdown-scroll-pane__actions {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
