<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { UButton, UIcon, UInput, URadio, URadioGroup } from "@veltra/desktop";
import { Books, External, Folder, FolderOpened, Refresh, Search } from "@veltra/icons/normal";
import type { ChatToolCall } from "@veltra/ai";

import { getDevDocNode, getDocNode, listDevDocTree, listDocTree } from "@/api/projects";
import type { ProjectDocNode } from "@/api/types";
import { MarkdownScrollPane } from "@/components/markdown-viewer";

const props = defineProps<{
  toolCall: ChatToolCall;
}>();

const parsedArguments = computed<Record<string, any>>(() => {
  if (!props.toolCall.arguments) return {};
  try {
    return JSON.parse(props.toolCall.arguments);
  } catch {
    return {};
  }
});

const parsedResult = computed<Record<string, any>>(() => {
  if (!props.toolCall.result) return {};
  try {
    return JSON.parse(props.toolCall.result);
  } catch {
    return {};
  }
});

const projectId = computed<number>(() => {
  const p = parsedResult.value.project_id ?? parsedArguments.value.project_id;
  return Number(p) || 0;
});

const docKind = ref<"api" | "dev">("api");
const tree = ref<ProjectDocNode[]>([]);
const selectedNode = ref<ProjectDocNode | null>(null);
const content = ref("");
const loadingTree = ref(false);
const loadingContent = ref(false);
const searchKeyword = ref("");
const expandedDirIds = ref<Set<number>>(new Set());

function findNode(nodes: ProjectDocNode[], id: number): ProjectDocNode | undefined {
  for (const n of nodes) {
    if (n.id === id) return n;
    const found = findNode(n.children ?? [], id);
    if (found) return found;
  }
}

function findFirstDoc(nodes: ProjectDocNode[]): ProjectDocNode | undefined {
  for (const n of nodes) {
    if (n.kind === "doc") return n;
    const found = findFirstDoc(n.children ?? []);
    if (found) return found;
  }
}

function expandAncestors(nodes: ProjectDocNode[], targetId: number, acc: number[] = []): boolean {
  for (const n of nodes) {
    if (n.id === targetId) {
      for (const id of acc) expandedDirIds.value.add(id);
      return true;
    }
    if (n.children?.length) {
      if (expandAncestors(n.children, targetId, [...acc, n.id])) return true;
    }
  }
  return false;
}

function filterTree(nodes: ProjectDocNode[], kw: string): ProjectDocNode[] {
  if (!kw.trim()) return nodes;
  const lower = kw.toLowerCase().trim();
  const out: ProjectDocNode[] = [];
  for (const n of nodes) {
    const matched = n.name.toLowerCase().includes(lower);
    const filteredChildren = n.children ? filterTree(n.children, kw) : [];
    if (matched || filteredChildren.length > 0) {
      out.push({
        ...n,
        children: filteredChildren,
      });
      if (n.kind === "dir") {
        expandedDirIds.value.add(n.id);
      }
    }
  }
  return out;
}

const displayTree = computed(() => {
  return filterTree(tree.value, searchKeyword.value);
});

async function selectNode(node: ProjectDocNode) {
  selectedNode.value = node;
  if (node.kind === "dir") {
    toggleDir(node.id);
    content.value = "";
    return;
  }
  if (!projectId.value) return;
  loadingContent.value = true;
  try {
    const detail =
      docKind.value === "dev"
        ? await getDevDocNode(projectId.value, node.id)
        : await getDocNode(projectId.value, node.id);
    content.value = detail.content || "";
  } catch (err) {
    content.value = "加载文档内容失败: " + (err instanceof Error ? err.message : String(err));
  } finally {
    loadingContent.value = false;
  }
}

function toggleDir(id: number) {
  if (expandedDirIds.value.has(id)) {
    expandedDirIds.value.delete(id);
  } else {
    expandedDirIds.value.add(id);
  }
}

async function loadTree() {
  if (!projectId.value) return;
  loadingTree.value = true;
  try {
    const list =
      docKind.value === "dev"
        ? await listDevDocTree(projectId.value)
        : await listDocTree(projectId.value);
    tree.value = list;

    // Automatically expand all root-level directories
    for (const item of list) {
      if (item.kind === "dir") expandedDirIds.value.add(item.id);
    }

    const targetNodeId = parsedResult.value.node_id ?? parsedArguments.value.node_id;
    if (targetNodeId) {
      const found = findNode(tree.value, Number(targetNodeId));
      if (found) {
        expandAncestors(tree.value, found.id);
        await selectNode(found);
        return;
      }
    }

    const first = findFirstDoc(tree.value);
    if (first) {
      expandAncestors(tree.value, first.id);
      await selectNode(first);
    }
  } catch (err) {
    content.value = "加载文档树失败: " + (err instanceof Error ? err.message : String(err));
  } finally {
    loadingTree.value = false;
  }
}

watch(
  () => parsedArguments.value.doc_type ?? parsedResult.value.doc_type,
  (val) => {
    if (val === "dev" || val === "api") {
      docKind.value = val;
    }
  },
  { immediate: true },
);

watch(
  [projectId, docKind],
  () => {
    if (projectId.value) {
      void loadTree();
    }
  },
  { immediate: true },
);

function openExternalDocs() {
  if (!projectId.value) return;
  const sub = docKind.value === "dev" ? "dev-docs" : "docs";
  window.open(`/project/projects/${projectId.value}/${sub}`, "_blank");
}
</script>

<template>
  <div class="doc-viewer-panel">
    <!-- 头部工具栏 -->
    <header class="doc-viewer-panel__header">
      <div class="doc-viewer-panel__header-left">
        <u-radio-group v-model="docKind" size="small">
          <u-radio value="api">接口文档</u-radio>
          <u-radio value="dev">开发文档</u-radio>
        </u-radio-group>
      </div>

      <div class="doc-viewer-panel__header-right">
        <u-button size="small" text @click="loadTree">
          <u-icon :size="13">
            <Refresh />
          </u-icon>
          刷新
        </u-button>
        <u-button size="small" plain type="primary" @click="openExternalDocs">
          <u-icon :size="13">
            <External />
          </u-icon>
          管理文档
        </u-button>
      </div>
    </header>

    <!-- 主体区域：左侧文档树，右侧正文 -->
    <div class="doc-viewer-panel__body">
      <aside class="doc-viewer-panel__sidebar">
        <div class="doc-viewer-panel__search">
          <u-input v-model="searchKeyword" placeholder="过滤文档..." size="small" clearable>
            <template #prefix>
              <u-icon :size="13">
                <Search />
              </u-icon>
            </template>
          </u-input>
        </div>

        <div v-if="loadingTree" class="doc-viewer-panel__tree-empty">加载文档树中...</div>
        <div v-else-if="displayTree.length === 0" class="doc-viewer-panel__tree-empty">
          暂无文档
        </div>

        <div v-else class="doc-viewer-panel__tree-list">
          <!-- 递归渲染树节点组件内联 -->
          <template v-for="node in displayTree" :key="node.id">
            <!-- 目录节点 -->
            <template v-if="node.kind === 'dir'">
              <div class="doc-viewer-panel__tree-item is-dir" @click="toggleDir(node.id)">
                <u-icon :size="14" class="icon-dir">
                  <FolderOpened v-if="expandedDirIds.has(node.id)" />
                  <Folder v-else />
                </u-icon>
                <span class="name">{{ node.name }}</span>
              </div>

              <!-- 子节点展开 -->
              <div
                v-if="expandedDirIds.has(node.id) && node.children?.length"
                class="doc-viewer-panel__tree-children"
              >
                <template v-for="child in node.children" :key="child.id">
                  <div
                    v-if="child.kind === 'dir'"
                    class="doc-viewer-panel__tree-item is-dir"
                    @click="toggleDir(child.id)"
                  >
                    <u-icon :size="14" class="icon-dir">
                      <FolderOpened v-if="expandedDirIds.has(child.id)" />
                      <Folder v-else />
                    </u-icon>
                    <span class="name">{{ child.name }}</span>
                  </div>
                  <div
                    v-else
                    class="doc-viewer-panel__tree-item is-doc"
                    :class="{ 'is-selected': selectedNode?.id === child.id }"
                    @click="selectNode(child)"
                  >
                    <u-icon :size="13" class="icon-doc">
                      <Books />
                    </u-icon>
                    <span class="name">{{ child.name }}</span>
                  </div>
                </template>
              </div>
            </template>

            <!-- 顶层文档节点 -->
            <template v-else>
              <div
                class="doc-viewer-panel__tree-item is-doc"
                :class="{ 'is-selected': selectedNode?.id === node.id }"
                @click="selectNode(node)"
              >
                <u-icon :size="13" class="icon-doc">
                  <Books />
                </u-icon>
                <span class="name">{{ node.name }}</span>
              </div>
            </template>
          </template>
        </div>
      </aside>

      <!-- 右侧正文展示区 -->
      <main class="doc-viewer-panel__content">
        <div v-if="selectedNode" class="doc-viewer-panel__content-inner">
          <div class="doc-viewer-panel__content-header">
            <h3 class="doc-viewer-panel__doc-title">{{ selectedNode.name }}</h3>
            <span class="doc-viewer-panel__doc-type">
              {{ docKind === "dev" ? "开发文档" : "接口文档" }}
            </span>
          </div>

          <div v-if="loadingContent" class="doc-viewer-panel__loading">正文加载中...</div>
          <div v-else-if="selectedNode.kind === 'dir'" class="doc-viewer-panel__dir-hint">
            该节点为目录，请在左侧选择具体的 Markdown 文档。
          </div>
          <div v-else-if="!content.trim()" class="doc-viewer-panel__empty-content">
            文档内容为空
          </div>
          <div v-else class="doc-viewer-panel__markdown-container">
            <MarkdownScrollPane :content="content" />
          </div>
        </div>

        <div v-else class="doc-viewer-panel__empty-select">请在左侧选择要查看的文档</div>
      </main>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.doc-viewer-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 520px;
  background: fn.use-var(bg-color, top);
  color: fn.use-var(text-color, main);
  font-size: 13px;
}

.doc-viewer-panel__header {
  height: 44px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px;
  border-bottom: 1px solid fn.use-var(border, muted-color);
  background: fn.use-var(bg-color, middle);
}

.doc-viewer-panel__header-left,
.doc-viewer-panel__header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.doc-viewer-panel__body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.doc-viewer-panel__sidebar {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid fn.use-var(border, muted-color);
  display: flex;
  flex-direction: column;
  background: fn.use-var(bg-color, middle);
}

.doc-viewer-panel__search {
  padding: 8px 10px;
  border-bottom: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 50%, transparent);
}

.doc-viewer-panel__tree-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px 0;
}

.doc-viewer-panel__tree-empty {
  padding: 24px 12px;
  text-align: center;
  color: fn.use-var(text-color, placeholder);
  font-size: 12px;
}

.doc-viewer-panel__tree-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  cursor: pointer;
  user-select: none;
  font-size: 12px;
  color: fn.use-var(text-color, main);
  transition: background 0.15s ease;

  &:hover {
    background: color-mix(in srgb, fn.use-var(bg-color, bottom) 60%, transparent);
  }

  &.is-selected {
    background: color-mix(in srgb, fn.use-var(color, primary) 14%, transparent);
    color: fn.use-var(color, primary);
    font-weight: 500;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .icon-dir {
    color: fn.use-var(color, warning);
    flex-shrink: 0;
  }

  .icon-doc {
    color: fn.use-var(color, primary);
    flex-shrink: 0;
  }
}

.doc-viewer-panel__tree-children {
  padding-left: 14px;
}

.doc-viewer-panel__content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: fn.use-var(bg-color, top);
}

.doc-viewer-panel__content-inner {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.doc-viewer-panel__content-header {
  height: 40px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  border-bottom: 1px solid fn.use-var(border, muted-color);
}

.doc-viewer-panel__doc-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.doc-viewer-panel__doc-type {
  font-size: 11px;
  color: fn.use-var(text-color, description);
}

.doc-viewer-panel__markdown-container {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 12px 16px;
}

.doc-viewer-panel__loading,
.doc-viewer-panel__dir-hint,
.doc-viewer-panel__empty-content,
.doc-viewer-panel__empty-select {
  padding: 40px 16px;
  text-align: center;
  color: fn.use-var(text-color, placeholder);
  font-size: 13px;
}
</style>
