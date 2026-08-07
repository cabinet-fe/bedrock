<script setup lang="ts">
import { onMounted, onUnmounted, useTemplateRef } from "vue";
import MarkdownRender, { enableMermaid, type MarkdownIt } from "markstream-vue";
import "markstream-vue/index.css";

enableMermaid();

defineProps<{
  content: string;
}>();

const root = useTemplateRef<HTMLElement>("root");

type MarkdownItWithHeadingIds = MarkdownIt & { __bedrockHeadingIds?: boolean };

/** GitHub 风格 slug：去标点、空白→`-`、保留中文等字母数字 */
function toGithubSlug(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}\s-]/gu, "")
    .replace(/\s+/g, "-");
}

/** 经 customMarkdownIt 给 heading_open 写入 id；attrs 经 attrsRecord 落到 HeadingNode */
function withHeadingIds(md: MarkdownIt): MarkdownIt {
  const inst = md as MarkdownItWithHeadingIds;
  if (inst.__bedrockHeadingIds) return md;
  inst.__bedrockHeadingIds = true;

  md.core.ruler.push("heading_ids", (state) => {
    const seen = new Map<string, number>();
    const { tokens } = state;
    for (let i = 0; i < tokens.length; i++) {
      const token = tokens[i];
      if (!token || token.type !== "heading_open") continue;
      const text = tokens[i + 1]?.content ?? "";
      let id = toGithubSlug(text);
      if (!id) continue;
      const count = seen.get(id) ?? 0;
      seen.set(id, count + 1);
      if (count > 0) id = `${id}-${count}`;
      token.attrSet("id", id);
    }
  });
  return md;
}

function scrollToHash(hash: string) {
  const raw = hash.startsWith("#") ? hash.slice(1) : hash;
  if (!raw || !root.value) return;
  let id: string;
  try {
    id = decodeURIComponent(raw);
  } catch {
    id = raw;
  }
  root.value.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView({
    block: "start",
  });
}

function onClick(e: MouseEvent) {
  const target = e.target;
  if (!(target instanceof Element) || !root.value?.contains(target)) return;
  const anchor = target.closest("a[href^='#']");
  if (!(anchor instanceof HTMLAnchorElement)) return;
  const href = anchor.getAttribute("href");
  // 跳过空锚与路由 hash（如 #/path）
  if (!href || href === "#" || href.startsWith("#/")) return;
  e.preventDefault();
  // 同 hash 不会触发 hashchange，需手动滚；否则改 location.hash 由 hashchange 统一处理
  if (location.hash === href) {
    scrollToHash(href);
  } else {
    location.hash = href;
  }
}

function onHashChange() {
  scrollToHash(location.hash);
}

onMounted(() => {
  root.value?.addEventListener("click", onClick);
  window.addEventListener("hashchange", onHashChange);
  if (location.hash) {
    requestAnimationFrame(() => scrollToHash(location.hash));
  }
});

onUnmounted(() => {
  root.value?.removeEventListener("click", onClick);
  window.removeEventListener("hashchange", onHashChange);
});
</script>

<template>
  <div ref="root" class="markdown-viewer">
    <MarkdownRender
      :content="content"
      :enable-mermaid="true"
      :custom-markdown-it="withHeadingIds"
    />
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
  :deep(h3),
  :deep(h4),
  :deep(h5),
  :deep(h6) {
    margin-top: 1.25em;
    margin-bottom: 0.5em;
    scroll-margin-top: 12px;
  }

  :deep(h1:first-child),
  :deep(h2:first-child),
  :deep(h3:first-child),
  :deep(h4:first-child),
  :deep(h5:first-child),
  :deep(h6:first-child) {
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
