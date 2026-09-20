<script setup lang="ts">
import { computed, nextTick, reactive, ref, shallowRef, useTemplateRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  message,
  messageConfirm,
  type ContextMenuItem,
  type TreeNode,
  type TreeExposed,
} from "@veltra/desktop";
import {
  ArrowLeft,
  ArrowRight,
  Books,
  Checklist,
  Delete,
  FileAdd,
  Folder,
  FolderAdd,
  Move,
  Search,
  Upload,
} from "@veltra/icons/normal";

import {
  createDevDocNode,
  createDocNode,
  deleteDevDocNode,
  deleteDocNode,
  getDevDocNode,
  getDocNode,
  importDevDocsZIP,
  importDocsZIP,
  listDevDocTree,
  listDocTree,
  moveDevDocNode,
  moveDocNode,
  updateDevDocNode,
  updateDocNode,
  uploadDevMarkdown,
  uploadMarkdown,
} from "@/api/projects";
import type { ProductProject, ProjectDocNode, ProjectRole } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { MarkdownScrollPane } from "@/components/markdown-viewer";
import { usePermission } from "@/composables/use-permission";

const props = withDefaults(
  defineProps<{
    project: ProductProject;
    projectRole?: ProjectRole;
    manageAll: boolean;
    /** api = 接口文档；dev = 开发文档 */
    docKind?: "api" | "dev";
  }>(),
  { docKind: "api" },
);
const { hasPermission } = usePermission();

const isDev = computed(() => props.docKind === "dev");
const permPrefix = computed(() => (isDev.value ? "project_dev_docs" : "project_docs"));

const route = useRoute();
const router = useRouter();
/** URL 查询参数名：记录当前查看的文档 id，刷新 / 切 tab 后可恢复 */
const docQueryKey = props.docKind === "dev" ? "devDocId" : "docId";

const tree = ref<ProjectDocNode[]>([]);
const treeRef = useTemplateRef<TreeExposed>("treeRef");
/** 当前是否全部展开；点击「展开/收起全部」时切换 */
const allExpanded = ref(true);
/** 当前选中的节点 id；与树单选双向绑定，并同步到 URL 查询参数 */
const selectedID = ref<number>();
/** 批量操作模式：进入后树才显示勾选框 */
const batchMode = ref(false);
/** 勾选的节点 id，批量模式下用于批量删除 */
const checked = ref<number[]>([]);
const selected = ref<ProjectDocNode | null>(null);
const content = ref("");
const docPane = ref("preview");
const treeCollapsed = ref(false);
const nodeDialogOpen = ref(false);
const moveDialogOpen = ref(false);
const creatingKind = ref<"dir" | "doc">("doc");
const createParentID = ref<number | null>(null);
/** 当前正在移动的节点（来自树节点操作，非右侧内容区） */
const movingNode = ref<ProjectDocNode | null>(null);
const nodeForm = reactive({ name: "" });
const moveForm = reactive({ parent_id: undefined as number | undefined, sort_order: 0 });
const searchKeyword = ref("");
/** 文档内容缓存：搜索文件内容时按需拉取，命中后不再重复请求 */
const contentCache = new Map<number, string>();
let searchTimer: ReturnType<typeof setTimeout> | undefined;

const menuOpen = ref(false);
const menuPos = ref({ x: 0, y: 0 });
const menuItems = shallowRef<ContextMenuItem[]>([]);

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
  () => hasPermission(`${permPrefix.value}:create`) && canEditProjectContent.value,
);
const canUpdate = computed(
  () => hasPermission(`${permPrefix.value}:update`) && canEditProjectContent.value,
);
const canDelete = computed(
  () => hasPermission(`${permPrefix.value}:delete`) && canAdminProjectContent.value,
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

function filterDirNodes(nodes: ProjectDocNode[]): ProjectDocNode[] {
  return nodes
    .filter((n) => n.kind === "dir")
    .map((n) => ({ ...n, children: filterDirNodes(n.children ?? []) }));
}

function collectNodeIds(node: ProjectDocNode, out: Set<number>) {
  out.add(node.id);
  for (const child of node.children ?? []) collectNodeIds(child, out);
}

function findNode(nodes: ProjectDocNode[], id: number): ProjectDocNode | undefined {
  for (const n of nodes) {
    if (n.id === id) return n;
    const found = findNode(n.children ?? [], id);
    if (found) return found;
  }
}

/** 勾选了父节点时去掉其子孙，避免重复删除（删目录会连带子树） */
function pruneDeleteTargets(ids: number[]): ProjectDocNode[] {
  const descendantIds = new Set<number>();
  const nodes: ProjectDocNode[] = [];
  for (const id of ids) {
    const node = findNode(tree.value, id);
    if (!node) continue;
    nodes.push(node);
    for (const child of node.children ?? []) collectNodeIds(child, descendantIds);
  }
  return nodes.filter((n) => !descendantIds.has(n.id));
}

function checkedNodes(): ProjectDocNode[] {
  return checked.value.flatMap((id) => {
    const node = findNode(tree.value, id);
    return node ? [node] : [];
  });
}

function isMoveTargetDisabled(item: Record<string, any>) {
  return moveBlockedIds.value.has(item.id as number);
}

/** 读取 URL 查询参数里的文档 id（无参数或非法值返回 undefined） */
function queryDocID(): number | undefined {
  const raw = route.query[docQueryKey];
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "string" ? Number(value) : Number.NaN;
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

/** 把当前选中的文档 id 同步到 URL 查询参数（replace 不产生历史记录） */
function syncDocQuery(id?: number) {
  const raw = route.query[docQueryKey];
  const current = Array.isArray(raw) ? raw[0] : raw;
  const next = id === undefined ? undefined : String(id);
  if (current === next) return;
  void router.replace({ query: { ...route.query, [docQueryKey]: next } });
}

/** 树加载后从 URL 恢复上次查看的文档；节点已不存在时清理参数 */
function restoreSelectedFromQuery() {
  const raw = route.query[docQueryKey];
  if (raw === undefined) return;
  const id = queryDocID();
  if (id !== undefined && findNode(tree.value, id)) {
    selectedID.value = id;
    return;
  }
  void router.replace({ query: { ...route.query, [docQueryKey]: undefined } });
}

async function loadTree() {
  try {
    tree.value = isDev.value
      ? await listDevDocTree(props.project.id)
      : await listDocTree(props.project.id);
    contentCache.clear();
    restoreSelectedFromQuery();
    if (allExpanded.value) {
      void nextTick(() => treeRef.value?.expandAll());
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : "文档树加载失败");
  }
}

function toggleExpandAll() {
  const t = treeRef.value;
  if (!t) return;
  if (allExpanded.value) t.collapseAll();
  else t.expandAll();
  allExpanded.value = !allExpanded.value;
}

function collectAllDocs(nodes: ProjectDocNode[]): ProjectDocNode[] {
  return nodes.flatMap((n) => [n, ...collectAllDocs(n.children ?? [])]);
}

async function fetchDocContent(node: ProjectDocNode) {
  try {
    const full = isDev.value
      ? await getDevDocNode(props.project.id, node.id)
      : await getDocNode(props.project.id, node.id);
    contentCache.set(node.id, full.content ?? "");
  } catch {
    contentCache.set(node.id, "");
  }
}

async function runConcurrent<T>(items: T[], limit: number, worker: (item: T) => Promise<void>) {
  let i = 0;
  await Promise.all(
    Array.from({ length: Math.min(limit, items.length) }, async () => {
      while (i < items.length) {
        await worker(items[i++]!);
      }
    }),
  );
}

/** 提取文档一级标题（第一个 `# ` 行），用于按标题搜索 */
function firstHeading(id: number): string {
  const match = (contentCache.get(id) ?? "").match(/^#\s+(.+)$/m);
  return (match?.[1] ?? "").trim().toLowerCase();
}

async function applySearch() {
  const t = treeRef.value;
  if (!t) return;
  const kw = searchKeyword.value.trim().toLowerCase();
  if (!kw) {
    t.filter(() => true);
    return;
  }
  t.filter((node) => node.label.toLowerCase().includes(kw));
  const missing = collectAllDocs(tree.value).filter(
    (n) => n.kind === "doc" && !contentCache.has(n.id),
  );
  if (missing.length) await runConcurrent(missing, 6, fetchDocContent);
  t.filter((node) => {
    if (node.label.toLowerCase().includes(kw)) return true;
    const data = node.data as ProjectDocNode;
    if (data.kind !== "doc") return false;
    return firstHeading(data.id).includes(kw);
  });
}

watch(searchKeyword, () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    void applySearch();
  }, 250);
});

async function loadSelected(id: number) {
  try {
    const node = isDev.value
      ? await getDevDocNode(props.project.id, id)
      : await getDocNode(props.project.id, id);
    if (selectedID.value !== id) return;
    selected.value = node;
    content.value = node.content ?? "";
    docPane.value = "preview";
  } catch (error) {
    message.error(error instanceof Error ? error.message : "读取文档失败");
  }
}

/** 选中变化：同步 URL 参数并加载文档内容（树的点击选中与程序化选中统一走这里） */
watch(selectedID, (id) => {
  syncDocQuery(id);
  if (id === undefined) {
    selected.value = null;
    content.value = "";
    return;
  }
  void loadSelected(id);
});

function openCreate(kind: "dir" | "doc", parentID?: number | null) {
  creatingKind.value = kind;
  createParentID.value = parentID !== undefined ? parentID : selectedDirectoryID();
  nodeForm.name = "";
  nodeDialogOpen.value = true;
}

function selectedDirectoryID() {
  if (!selected.value) return null;
  return selected.value.kind === "dir" ? selected.value.id : (selected.value.parent_id ?? null);
}

async function createNode() {
  try {
    const input = {
      kind: creatingKind.value,
      name: nodeForm.name,
      parent_id: createParentID.value,
    };
    const node = isDev.value
      ? await createDevDocNode(props.project.id, input)
      : await createDocNode(props.project.id, input);
    nodeDialogOpen.value = false;
    await loadTree();
    selectedID.value = node.id;
    message.success(creatingKind.value === "dir" ? "目录已创建" : "文档已创建");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "创建失败");
  }
}

async function saveContent() {
  if (!selected.value || selected.value.kind !== "doc") return;
  try {
    const node = isDev.value
      ? await updateDevDocNode(props.project.id, selected.value.id, { content: content.value })
      : await updateDocNode(props.project.id, selected.value.id, { content: content.value });
    selected.value = node;
    content.value = node.content ?? "";
    await loadTree();
    message.success("已保存");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

async function removeNodes(nodes: { id: number }[]) {
  const roots = pruneDeleteTargets(nodes.map((n) => n.id));
  if (!roots.length) return;
  const removed = new Set<number>();
  try {
    for (const node of roots) {
      if (isDev.value) await deleteDevDocNode(props.project.id, node.id);
      else await deleteDocNode(props.project.id, node.id);
      collectNodeIds(node, removed);
    }
    message.success(roots.length > 1 ? `已删除 ${roots.length} 个节点` : "节点已删除");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
  checked.value = [];
  if (selectedID.value && removed.has(selectedID.value)) selectedID.value = undefined;
  await loadTree();
}

async function confirmRemove(nodes: { id: number; name: string }[]) {
  const roots = pruneDeleteTargets(nodes.map((n) => n.id));
  if (!roots.length) return;
  /** 单个文件直接删，目录（连带子树）与批量删除需要确认 */
  const singleDoc = roots.length === 1 && roots[0].kind === "doc";
  if (!singleDoc) {
    const text =
      roots.length === 1
        ? `删除目录「${roots[0].name}」？目录下内容将一并删除。`
        : `删除选中的 ${roots.length} 个节点？目录下内容将一并删除。`;
    const action = await messageConfirm.danger(text, {
      cancelButtonText: "取消",
    }).onClosed;
    if (action !== "confirm") return;
  }
  await removeNodes(roots);
}

function confirmRemoveChecked() {
  void confirmRemove(checkedNodes());
}

/** 切换批量操作模式；退出时清空勾选 */
function toggleBatchMode() {
  batchMode.value = !batchMode.value;
  if (!batchMode.value) checked.value = [];
}

function openMove(node: ProjectDocNode) {
  movingNode.value = node;
  moveForm.parent_id = node.parent_id ?? undefined;
  moveForm.sort_order = node.sort_order;
  moveDialogOpen.value = true;
}

function openMenu(e: MouseEvent, items: ContextMenuItem[]) {
  menuPos.value = { x: e.clientX, y: e.clientY };
  menuItems.value = items;
  menuOpen.value = true;
}

function onNodeContextMenu(e: MouseEvent, node: TreeNode) {
  e.preventDefault();
  e.stopPropagation();
  const data = node.data as ProjectDocNode;
  const items: ContextMenuItem[] = [];
  if (canCreate.value) {
    const parentID = data.kind === "dir" ? data.id : (data.parent_id ?? null);
    items.push({
      label: data.kind === "dir" ? "新建文档" : "新建同级文档",
      icon: FileAdd,
      callback: () => openCreate("doc", parentID),
    });
  }
  if (canUpdate.value) {
    items.push({ label: "移动", icon: Move, callback: () => openMove(data) });
  }
  if (canDelete.value) {
    const target = { id: data.id, name: data.name };
    const batch =
      checked.value.includes(data.id) && checked.value.length > 1 ? checkedNodes() : [target];
    items.push({
      label: batch.length > 1 ? `删除选中项 (${batch.length})` : "删除",
      icon: Delete,
      callback: () => {
        menuOpen.value = false;
        window.setTimeout(() => {
          void confirmRemove(batch);
        }, 0);
      },
    });
  }
  if (!items.length) return;
  openMenu(e, items);
}

function onTreeBlankContextMenu(e: MouseEvent) {
  const el = e.target as HTMLElement | null;
  if (el?.closest(".tree-node, .u-tree-node")) return;
  if (!canCreate.value) return;
  e.preventDefault();
  openMenu(e, [
    { label: "新建文档", icon: FileAdd, callback: () => openCreate("doc", null) },
    { label: "新建目录", icon: Folder, callback: () => openCreate("dir", null) },
  ]);
}

async function move() {
  if (!movingNode.value) return;
  try {
    const nodeID = movingNode.value.id;
    const input = {
      parent_id: moveForm.parent_id ?? null,
      sort_order: moveForm.sort_order,
    };
    if (isDev.value) await moveDevDocNode(props.project.id, nodeID, input);
    else await moveDocNode(props.project.id, nodeID, input);
    moveDialogOpen.value = false;
    movingNode.value = null;
    await loadTree();
    selectedID.value = nodeID;
    message.success("节点已移动");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "移动失败");
  }
}

async function uploadMarkdownFile(files: File[]) {
  const file = files[0];
  if (!file) return;
  try {
    const node = isDev.value
      ? await uploadDevMarkdown(props.project.id, selectedDirectoryID(), file)
      : await uploadMarkdown(props.project.id, selectedDirectoryID(), file);
    await loadTree();
    selectedID.value = node.id;
    message.success("Markdown 已导入");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "Markdown 导入失败");
  }
}

async function importZIPFile(files: File[]) {
  const file = files[0];
  if (!file) return;
  try {
    const items = isDev.value
      ? await importDevDocsZIP(props.project.id, selectedDirectoryID(), file)
      : await importDocsZIP(props.project.id, selectedDirectoryID(), file);
    await loadTree();
    if (items[0]) selectedID.value = items[0].id;
    message.success(`已导入 ${items.length} 个 Markdown`);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "ZIP 导入失败");
  }
}

/** 按扩展名分流：zip 走文档包导入，其余按 Markdown 导入 */
async function importFile(files: File[]) {
  const file = files[0];
  if (!file) return;
  if (/\.zip$/i.test(file.name)) await importZIPFile(files);
  else await uploadMarkdownFile(files);
}

watch(
  () => [props.project.id, props.docKind] as const,
  () => {
    selected.value = null;
    selectedID.value = undefined;
    batchMode.value = false;
    checked.value = [];
    void loadTree();
  },
  { immediate: true },
);

watch(canUpdate, (ok) => {
  if (!ok) docPane.value = "preview";
});
</script>

<template>
  <section class="docs" :class="{ 'is-tree-collapsed': treeCollapsed }">
    <aside
      class="tree-panel"
      :class="{ 'is-collapsed': treeCollapsed }"
      @contextmenu="onTreeBlankContextMenu"
    >
      <div class="tree-head">
        <template v-if="!treeCollapsed">
          <div class="tree-head__actions">
            <u-button
              v-if="canCreate"
              plain
              size="small"
              aria-label="新建文档"
              title="新建文档"
              @click="openCreate('doc', null)"
            >
              <u-icon :size="14"><FileAdd /></u-icon>
            </u-button>
            <u-button
              v-if="canCreate"
              plain
              size="small"
              aria-label="新建目录"
              title="新建目录"
              @click="openCreate('dir')"
            >
              <u-icon :size="14"><FolderAdd /></u-icon>
            </u-button>
            <u-button
              plain
              size="small"
              :aria-label="allExpanded ? '收起全部' : '展开全部'"
              :title="allExpanded ? '收起全部' : '展开全部'"
              @click="toggleExpandAll"
            >
              <u-icon :size="14"><ArrowRight /></u-icon>
            </u-button>
            <u-button
              v-if="canDelete"
              plain
              :type="batchMode ? 'primary' : undefined"
              size="small"
              aria-label="批量操作"
              title="批量操作"
              @click="toggleBatchMode"
            >
              <u-icon :size="14"><Checklist /></u-icon>
            </u-button>
            <u-button
              v-if="batchMode && checked.length"
              plain
              type="danger"
              size="small"
              aria-label="删除选中项"
              title="删除选中项"
              @click="confirmRemoveChecked"
            >
              <u-icon :size="14"><Delete /></u-icon>
            </u-button>
          </div>
          <u-button
            plain
            size="small"
            aria-label="收窄文档树"
            title="收窄文档树"
            @click="treeCollapsed = true"
          >
            <u-icon :size="14"><ArrowLeft /></u-icon>
          </u-button>
        </template>
        <u-button v-else plain size="small" aria-label="展开文档树" @click="treeCollapsed = false">
          <u-icon :size="14"><ArrowRight /></u-icon>
        </u-button>
      </div>
      <template v-if="!treeCollapsed">
        <div class="tree-search">
          <u-input v-model="searchKeyword" placeholder="搜索文件名或标题" clearable>
            <template #suffix>
              <u-icon :size="14"><Search /></u-icon>
            </template>
          </u-input>
        </div>
        <u-tree
          ref="treeRef"
          v-model:checked="checked"
          v-model:selected="selectedID"
          class="doc-tree"
          :data="tree"
          label-key="name"
          value-key="id"
          children-key="children"
          selectable
          scroll-to-view
          :checkable="batchMode && canDelete"
          check-strictly
          :check-on-click-node="false"
          :expand-on-click-node="false"
          @node-contextmenu="onNodeContextMenu"
        >
          <template #default="{ data }">
            <div class="tree-node" :class="data.kind === 'dir' ? 'is-dir' : 'is-doc'">
              <u-icon class="tree-node__icon" :size="14">
                <Folder v-if="data.kind === 'dir'" />
                <Books v-else />
              </u-icon>
              <span class="tree-node__name">{{ data.name }}</span>
            </div>
          </template>
        </u-tree>
        <div v-if="canCreate" class="uploads">
          <u-file-picker accept=".md,text/markdown,.zip,application/zip" @pick="importFile">
            <u-button
              plain
              size="small"
              aria-label="导入 Markdown / ZIP"
              title="导入 Markdown / ZIP"
            >
              <u-icon :size="14"><Upload /></u-icon>
            </u-button>
          </u-file-picker>
        </div>
      </template>
    </aside>

    <section class="editor-panel">
      <div v-if="!selected" class="editor-panel__empty">
        <u-empty text="从左侧选择文档节点" />
      </div>
      <template v-else>
        <div class="editor-head">
          <h3>{{ selected.name }}</h3>
          <u-tag size="small" :type="selected.kind === 'dir' ? undefined : 'primary'">{{
            selected.kind === "dir" ? "目录" : "文档"
          }}</u-tag>
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
              <MarkdownScrollPane class="doc-pane" :content="content" />
            </template>
            <template v-if="canUpdate" #edit>
              <u-code-editor v-model="content" :langs="['markdown']" class="doc-pane doc-editor" />
            </template>
          </u-tabs>
          <div v-if="canUpdate && docPane === 'edit'" class="doc-footer">
            <u-button type="primary" @click="saveContent">保存</u-button>
          </div>
        </template>
        <div v-else class="editor-panel__empty">
          <u-empty text="目录不包含 Markdown 内容" />
        </div>
      </template>
    </section>

    <u-contextmenu
      v-if="menuOpen"
      :mouse-position="menuPos"
      :menus="menuItems"
      @destroy="menuOpen = false"
    />

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
@use "@/lib/empty-center.scss" as empty;

.docs {
  display: grid;
  height: 100%;
  min-height: 0;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: 16px;

  &.is-tree-collapsed {
    grid-template-columns: 48px minmax(0, 1fr);
  }
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

  &.is-collapsed {
    padding: 8px;
    align-items: center;
  }
}

.doc-tree {
  flex: 1;
  min-height: 0;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
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

.tree-head,
.tree-search,
.uploads,
.doc-footer {
  display: flex;
  align-items: center;
}

.tree-search {
  flex-shrink: 0;
}

.tree-head {
  flex-shrink: 0;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
}

.tree-head__actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.tree-panel.is-collapsed .tree-head {
  justify-content: center;
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

.editor-panel__empty {
  @include empty.center(240px);
}

.editor-head,
.doc-footer {
  flex-shrink: 0;
}

.editor-head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;

  h3 {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.doc-tabs {
  flex: 1;
  height: 100%;
  min-height: 0;

  /* UTabs 插槽会被其内部 u-scroll 包裹，需让内容占满容器高度（同 handbook） */
  :deep(.u-scroll__content) {
    height: 100%;
  }
}

.doc-pane {
  height: 100%;
  width: 100%;
  min-height: 0;
}

.doc-editor {
  height: 100%;
  min-height: 0;
  max-height: none;
}

.doc-footer {
  gap: 8px;
  justify-content: flex-end;
}
</style>
