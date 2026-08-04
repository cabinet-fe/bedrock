<script setup lang="ts">
import MarkdownRender, { enableMermaid } from "markstream-vue";
import "markstream-vue/index.css";

enableMermaid();

defineProps<{
  content: string;
}>();
</script>

<template>
  <div class="markdown-viewer">
    <MarkdownRender :content="content" :enable-mermaid="true" />
  </div>
</template>

<style scoped lang="scss">
.markdown-viewer {
  min-width: 0;
  padding: 16px 20px;
  border-radius: 8px;
  background: var(--u-bg-color-top, #fff);
  color-scheme: light;
  line-height: 1.65;

  :deep(h1),
  :deep(h2),
  :deep(h3) {
    margin-top: 1.25em;
    margin-bottom: 0.5em;
  }

  :deep(h1:first-child),
  :deep(h2:first-child),
  :deep(h3:first-child) {
    margin-top: 0;
  }

  /* 只兜底无 class 的原生 pre；markstream 的代码块（带 class）自带
     行号 gutter 与 padding-left 计算，覆盖 padding 会导致行号与代码重叠 */
  :deep(pre:not([class])) {
    overflow: auto;
    padding: 12px;
    border-radius: 6px;
    background: var(--u-bg-color-middle, #f6f7f9);
  }

  /* markstream 当前版本的 fallback pre 作用域样式有缺陷：scope 属性落在 pre 自身，
     其 [data-v-xxx] pre.code-pre-fallback 选择器要求祖代携带，永远匹配不上，
     导致 padding-left 缺失、行号与代码重叠。在此按它自己的变量补回 gutter 留白 */
  :deep(pre.markstream-pre--line-numbers) {
    padding-left: var(--markstream-code-padding-left, 52px);
  }

  /* fallback pre 无容器包裹，自带背景为 transparent，补回代码块底色与圆角 */
  :deep(pre.code-pre-fallback) {
    border-radius: 6px;
    background: var(--u-bg-color-middle, #f6f7f9);
  }

  :deep(code) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.92em;
  }

  :deep(table) {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  :deep(th),
  :deep(td) {
    padding: 8px 10px;
    border: 1px solid var(--u-border-color, #e5e7eb);
    text-align: left;
  }

  :deep(th) {
    background: var(--u-bg-color-middle, #f6f7f9);
  }
}
</style>
