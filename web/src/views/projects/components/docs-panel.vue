<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { message } from "@veltra/desktop";
import { Books, Folder } from "@veltra/icons/normal";

import {
  createDocNode,
  deleteDocNode,
  getDocNode,
  importDocsZIP,
  listDocTree,
  moveDocNode,
  updateDocNode,
  uploadMarkdown,
} from "@/api/projects";
import type { ApiDocNode, ProductProject, ProjectRole } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import MarkdownViewer from "@/components/markdown-viewer";
import { usePermission } from "@/composables/use-permission";

const props = defineProps<{
  project: ProductProject;
  projectRole?: ProjectRole;
  manageAll: boolean;
}>();
const { hasPermission } = usePermission();

const tree = ref<ApiDocNode[]>([]);
const selectedID = ref<number>();
const selected = ref<ApiDocNode | null>(null);
const content = ref("");
const docPane = ref("preview");
const nodeDialogOpen = ref(false);
const moveDialogOpen = ref(false);
const creatingKind = ref<"dir" | "doc">("doc");
const createParentID = ref<number | null>(null);
/** 当前正在移动的节点（来自树节点操作，非右侧内容区） */
const movingNode = ref<ApiDocNode | null>(null);
const nodeForm = reactive({ name: "" });
const moveForm = reactive({ parent_id: undefined as number | undefined, sort_order: 0 });

const canEditProjectContent = computed(
  () =>
    props.manageAll ||
    props.projectRole === "owner" ||
    props.projectRole === "admin" ||
    props.projectRole === "member",
);
const canAdminProjectContent = computed(
  () => props.manageAll || props.projectRole === "owner" || props.projectRole === "admin",
);
const canCreate = computed(
  () => hasPermission("project_docs:create") && canEditProjectContent.value,
);
const canUpdate = computed(
  () => hasPermission("project_docs:update") && canEditProjectContent.value,
);
const canDelete = computed(
  () => hasPermission("project_docs:delete") && canAdminProjectContent.value,
);
const docPaneTabs = computed(() =>
  canUpdate.value
    ? [
        { key: "preview", name: "预览" },
        { key: "edit", name: "编辑" },
      ]
    : [{ key: "preview", name: "预览" }],
);

/** 移动弹框可选父目录：仅目录节点 */
const moveDirTree = computed(() => filterDirNodes(tree.value));
/** 不可选为父目录的节点（自身及其子孙） */
const moveBlockedIds = computed(() => {
  const ids = new Set<number>();
  if (movingNode.value) collectNodeIds(movingNode.value, ids);
  return ids;
});

function filterDirNodes(nodes: ApiDocNode[]): ApiDocNode[] {
  return nodes
    .filter((n) => n.kind === "dir")
    .map((n) => ({ ...n, children: filterDirNodes(n.children ?? []) }));
}

function collectNodeIds(node: ApiDocNode, out: Set<number>) {
  out.add(node.id);
  for (const child of node.children ?? []) collectNodeIds(child, out);
}

function isMoveTargetDisabled(item: Record<string, any>) {
  return moveBlockedIds.value.has(item.id as number);
}

async function loadTree() {
  try {
    tree.value = await listDocTree(props.project.id);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "文档树加载失败");
  }
}

async function selectNode(id?: number) {
  selectedID.value = id;
  if (!id) {
    selected.value = null;
    content.value = "";
    return;
  }
  try {
    const node = await getDocNode(props.project.id, id);
    selected.value = node;
    content.value = node.content ?? "";
    docPane.value = "preview";
  } catch (error) {
    message.error(error instanceof Error ? error.message : "读取文档失败");
  }
}

function openCreate(kind: "dir" | "doc", parentID?: number | null) {
  creatingKind.value = kind;
  createParentID.value = parentID !== undefined ? parentID : selectedDirectoryID();
  nodeForm.name = "";
  nodeDialogOpen.value = true;
}

function openCreateDoc(parentID: number) {
  openCreate("doc", parentID);
}

function selectedDirectoryID() {
  if (!selected.value) return null;
  return selected.value.kind === "dir" ? selected.value.id : (selected.value.parent_id ?? null);
}

async function createNode() {
  try {
    const node = await createDocNode(props.project.id, {
      kind: creatingKind.value,
      name: nodeForm.name,
      parent_id: createParentID.value,
    });
    nodeDialogOpen.value = false;
    await loadTree();
    await selectNode(node.id);
    message.success(creatingKind.value === "dir" ? "目录已创建" : "文档已创建");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "创建失败");
  }
}

async function saveContent() {
  if (!selected.value || selected.value.kind !== "doc") return;
  try {
    const node = await updateDocNode(props.project.id, selected.value.id, {
      content: content.value,
    });
    selected.value = node;
    content.value = node.content ?? "";
    await loadTree();
    message.success("已保存");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

async function removeNode() {
  if (!selected.value) return;
  try {
    await deleteDocNode(props.project.id, selected.value.id);
    await selectNode();
    await loadTree();
    message.success("节点已删除");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
}

function openMove(node: ApiDocNode) {
  movingNode.value = node;
  moveForm.parent_id = node.parent_id ?? undefined;
  moveForm.sort_order = node.sort_order;
  moveDialogOpen.value = true;
}

async function move() {
  if (!movingNode.value) return;
  try {
    const nodeID = movingNode.value.id;
    await moveDocNode(props.project.id, nodeID, {
      parent_id: moveForm.parent_id ?? null,
      sort_order: moveForm.sort_order,
    });
    moveDialogOpen.value = false;
    movingNode.value = null;
    await loadTree();
    await selectNode(nodeID);
    message.success("节点已移动");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "移动失败");
  }
}

async function uploadMarkdownFile(files: File[]) {
  const file = files[0];
  if (!file) return;
  try {
    const node = await uploadMarkdown(props.project.id, selectedDirectoryID(), file);
    await loadTree();
    await selectNode(node.id);
    message.success("Markdown 已导入");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "Markdown 导入失败");
  }
}

async function importZIPFile(files: File[]) {
  const file = files[0];
  if (!file) return;
  try {
    const items = await importDocsZIP(props.project.id, selectedDirectoryID(), file);
    await loadTree();
    if (items[0]) await selectNode(items[0].id);
    message.success(`已导入 ${items.length} 个 Markdown`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "ZIP 导入失败");
  }
}

watch(
  () => props.project.id,
  () => {
    selected.value = null;
    selectedID.value = undefined;
    void loadTree();
  },
  { immediate: true },
);

watch(canUpdate, (ok) => {
  if (!ok) docPane.value = "preview";
});
</script>

<template>
  <section class="docs">
    <aside class="tree-panel">
      <div class="tree-head">
        <strong>文档树</strong>
        <u-action v-if="canCreate" @run="openCreate('dir')">新建目录</u-action>
      </div>
      <u-tree
        v-model:selected="selectedID"
        class="doc-tree"
        :data="tree"
        label-key="name"
        value-key="id"
        children-key="children"
        selectable
        expand-all
        @update:selected="selectNode"
      >
        <template #default="{ data }">
          <div class="tree-node" :class="data.kind === 'dir' ? 'is-dir' : 'is-doc'">
            <span class="tree-node__main">
              <u-icon class="tree-node__icon" :size="14">
                <Folder v-if="data.kind === 'dir'" />
                <Books v-else />
              </u-icon>
              <span class="tree-node__name">{{ data.name }}</span>
            </span>
            <span
              v-if="canUpdate || (canCreate && data.kind === 'dir')"
              class="tree-node__actions"
              @click.stop
            >
              <u-action v-if="canUpdate" @run="openMove(data as ApiDocNode)">移动</u-action>
              <u-action v-if="canCreate && data.kind === 'dir'" @run="openCreateDoc(data.id)">
                新建文档
              </u-action>
            </span>
          </div>
        </template>
      </u-tree>
      <div v-if="canCreate" class="uploads">
        <u-file-picker accept=".md,text/markdown" @pick="uploadMarkdownFile" />
        <u-file-picker accept=".zip,application/zip" @pick="importZIPFile" />
      </div>
    </aside>

    <section class="editor-panel">
      <u-empty v-if="!selected" text="从左侧选择文档节点" />
      <template v-else>
        <div class="editor-head">
          <div>
            <h3>{{ selected.name }}</h3>
            <p>
              <u-tag size="small" :type="selected.kind === 'dir' ? undefined : 'primary'">{{
                selected.kind === "dir" ? "目录" : "文档"
              }}</u-tag>
            </p>
          </div>
          <u-action-group v-if="canDelete" :max="4">
            <u-action need-confirm type="danger" @run="removeNode">删除</u-action>
          </u-action-group>
        </div>

        <template v-if="selected.kind === 'doc'">
          <u-tabs
            v-model="docPane"
            :items="docPaneTabs"
            position="left"
            keep-alive
            class="doc-tabs"
          >
            <template #preview>
              <u-scroll class="doc-pane">
                <MarkdownViewer :content="content" />
              </u-scroll>
            </template>
            <template v-if="canUpdate" #edit>
              <u-code-editor v-model="content" :langs="['markdown']" class="doc-pane doc-editor" />
            </template>
          </u-tabs>
          <div v-if="canUpdate && docPane === 'edit'" class="doc-footer">
            <u-button type="primary" @click="saveContent">保存</u-button>
          </div>
        </template>
        <u-empty v-else text="目录不包含 Markdown 内容" />
      </template>
    </section>

    <FormDialog
      v-model="nodeDialogOpen"
      :title="creatingKind === 'dir' ? '新建目录' : '新建文档'"
      :model="nodeForm"
      label-width="80px"
      style="width: 420px"
      @submit="createNode"
    >
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
    </FormDialog>
    <FormDialog
      v-model="moveDialogOpen"
      title="移动节点"
      :model="moveForm"
      confirm-text="移动"
      label-width="100px"
      style="width: 420px"
      @submit="move"
      @closed="movingNode = null"
    >
      <u-tree-select
        label="父目录"
        field="parent_id"
        :data="moveDirTree"
        label-key="name"
        value-key="id"
        children-key="children"
        clearable
        filterable
        expand-all
        placeholder="根目录"
        :disabled-node="isMoveTargetDisabled"
      />
      <u-number-input label="排序" field="sort_order" />
    </FormDialog>
  </section>
</template>

<style scoped lang="scss">
.docs {
  display: grid;
  height: 100%;
  min-height: 0;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: 16px;
}

.tree-panel,
.editor-panel {
  min-width: 0;
  min-height: 0;
  padding: 14px;
  border-radius: 8px;
  background: var(--u-bg-color-top, #fff);
}

.tree-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  overflow: hidden;
}

.doc-tree {
  flex: 1;
  min-height: 0;
}

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  min-width: 0;
}

.tree-node__main {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tree-node__icon {
  flex-shrink: 0;
}

.tree-node.is-dir .tree-node__icon {
  color: var(--u-color-warning, #d48806);
}

.tree-node.is-doc .tree-node__icon {
  color: var(--u-color-primary, #1677ff);
}

.tree-node__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree-node__actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.tree-node:hover .tree-node__actions,
.tree-node:focus-within .tree-node__actions {
  opacity: 1;
}

.tree-head,
.editor-head,
.editor-head p,
.uploads,
.doc-footer {
  display: flex;
  align-items: center;
}

.tree-head,
.editor-head {
  flex-shrink: 0;
  justify-content: space-between;
  gap: 12px;
}

.uploads {
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 8px;
}

.editor-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.editor-head,
.doc-footer {
  flex-shrink: 0;
}

.editor-head h3,
.editor-head p {
  margin: 0;
}

.editor-head p {
  gap: 8px;
  margin-top: 5px;
  color: var(--u-text-color-assist, #7c8494);
  font-size: 13px;
}

.doc-tabs {
  flex: 1;
  height: 100%;
  min-height: 0;
}

.doc-pane {
  flex: 1 1 auto;
  width: 100%;
  min-height: 0;
}

.doc-editor {
  height: 100%;
}

.doc-footer {
  gap: 8px;
  justify-content: flex-end;
}
</style>
