<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from "vue";
import type { CodeEditorLang } from "@veltra/desktop";
import { message } from "@veltra/desktop";
import { Books, Delete, Edit, FileAdd, FolderAdd, FolderOpened } from "@veltra/icons/normal";

import {
  createSkillEntry,
  deleteSkillEntry,
  getSkill,
  listSkillFiles,
  readSkillFile,
  renameSkillEntry,
  writeSkillFile,
} from "@/api/ai";
import type { SkillFileNode, SkillPackage } from "@/api/types";
import FormDialog from "@/components/form-dialog";

const props = defineProps<{
  skillId: number;
}>();

const emit = defineEmits<{
  loaded: [skill: SkillPackage];
}>();

const skill = shallowRef<SkillPackage | null>(null);
const tree = shallowRef<SkillFileNode[]>([]);
const selectedPath = ref<string>();
const selectedKind = ref<"file" | "dir">();
const content = ref("");
const savedContent = ref("");
const binary = ref(false);
const loadingTree = ref(false);
const saving = ref(false);
/** Path whose content is currently loaded; used to revert tree selection on discard cancel. */
let activePath: string | undefined;

const createOpen = ref(false);
const createKind = ref<"file" | "dir">("file");
const createParent = ref("");
const createForm = reactive({ name: "" });

const renameOpen = ref(false);
const renameTarget = ref<SkillFileNode | null>(null);
const renameForm = reactive({ name: "" });

const dirty = computed(
  () => selectedKind.value === "file" && !binary.value && content.value !== savedContent.value,
);
const editable = computed(() => !!skill.value?.editable);
const editorLangs = computed<CodeEditorLang[]>(() => guessLangs(selectedPath.value));

watch(
  () => props.skillId,
  () => {
    void bootstrap();
  },
);

onMounted(() => {
  void bootstrap();
  window.addEventListener("beforeunload", onBeforeUnload);
});

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", onBeforeUnload);
});

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (!dirty.value) return;
  e.preventDefault();
  e.returnValue = "";
}

async function bootstrap() {
  activePath = undefined;
  selectedPath.value = undefined;
  selectedKind.value = undefined;
  content.value = "";
  savedContent.value = "";
  binary.value = false;
  try {
    skill.value = await getSkill(props.skillId);
    emit("loaded", skill.value);
    await loadTree();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载技能失败");
  }
}

async function loadTree() {
  loadingTree.value = true;
  try {
    tree.value = await listSkillFiles(props.skillId);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "文件树加载失败");
  } finally {
    loadingTree.value = false;
  }
}

async function selectPath(path?: string, data?: SkillFileNode) {
  if (path === activePath && data?.kind === selectedKind.value) return;
  if (dirty.value && !(await confirmDiscard())) {
    selectedPath.value = activePath;
    return;
  }
  activePath = path;
  selectedPath.value = path;
  if (!path || !data) {
    selectedKind.value = undefined;
    content.value = "";
    savedContent.value = "";
    binary.value = false;
    return;
  }
  selectedKind.value = data.kind;
  if (data.kind === "dir") {
    content.value = "";
    savedContent.value = "";
    binary.value = false;
    return;
  }
  try {
    const file = await readSkillFile(props.skillId, path);
    binary.value = file.binary;
    content.value = file.binary ? "" : file.content;
    savedContent.value = content.value;
  } catch (error) {
    message.error(error instanceof Error ? error.message : "读取文件失败");
  }
}

async function confirmDiscard() {
  return window.confirm("当前文件有未保存修改，确定丢弃？");
}

async function save() {
  if (!editable.value || !selectedPath.value || selectedKind.value !== "file" || binary.value)
    return;
  saving.value = true;
  try {
    await writeSkillFile(props.skillId, selectedPath.value, content.value);
    savedContent.value = content.value;
    skill.value = await getSkill(props.skillId);
    message.success("已保存");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  } finally {
    saving.value = false;
  }
}

function parentOfSelected() {
  if (!selectedPath.value) return "";
  if (selectedKind.value === "dir") return selectedPath.value;
  const i = selectedPath.value.lastIndexOf("/");
  return i >= 0 ? selectedPath.value.slice(0, i) : "";
}

function openCreate(kind: "file" | "dir") {
  createKind.value = kind;
  createParent.value = parentOfSelected();
  createForm.name = "";
  createOpen.value = true;
}

async function submitCreate() {
  const name = createForm.name.trim();
  if (!name || name.includes("/") || name.includes("\\")) {
    message.error("名称不能为空，且不能包含路径分隔符");
    return;
  }
  const path = createParent.value ? `${createParent.value}/${name}` : name;
  try {
    await createSkillEntry(props.skillId, {
      path,
      kind: createKind.value,
      content: createKind.value === "file" ? "" : undefined,
    });
    createOpen.value = false;
    await loadTree();
    if (createKind.value === "file") {
      await selectPath(path, { name, path, kind: "file" });
    }
    message.success(createKind.value === "dir" ? "目录已创建" : "文件已创建");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "创建失败");
  }
}

function openRename(node: SkillFileNode) {
  renameTarget.value = node;
  renameForm.name = node.name;
  renameOpen.value = true;
}

async function submitRename() {
  const target = renameTarget.value;
  if (!target) return;
  const name = renameForm.name.trim();
  if (!name || name.includes("/") || name.includes("\\")) {
    message.error("名称不能为空，且不能包含路径分隔符");
    return;
  }
  const parent = target.path.includes("/")
    ? target.path.slice(0, target.path.lastIndexOf("/"))
    : "";
  const toPath = parent ? `${parent}/${name}` : name;
  if (dirty.value && selectedPath.value === target.path && !(await confirmDiscard())) return;
  try {
    await renameSkillEntry(props.skillId, target.path, toPath);
    renameOpen.value = false;
    await loadTree();
    if (selectedPath.value === target.path || selectedPath.value?.startsWith(`${target.path}/`)) {
      const next =
        selectedPath.value === target.path
          ? toPath
          : `${toPath}${selectedPath.value!.slice(target.path.length)}`;
      const kind = selectedPath.value === target.path ? target.kind : selectedKind.value;
      await selectPath(next, kind ? { name: nameFromPath(next), path: next, kind } : undefined);
    }
    message.success("已重命名");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "重命名失败");
  }
}

async function removeNode(node: SkillFileNode) {
  if (
    dirty.value &&
    (selectedPath.value === node.path || selectedPath.value?.startsWith(`${node.path}/`))
  ) {
    if (!(await confirmDiscard())) return;
  }
  try {
    await deleteSkillEntry(props.skillId, node.path);
    if (selectedPath.value === node.path || selectedPath.value?.startsWith(`${node.path}/`)) {
      await selectPath(undefined);
    }
    await loadTree();
    message.success("已删除");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
}

function nameFromPath(path: string) {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

function guessLangs(path?: string): CodeEditorLang[] {
  if (!path) return ["markdown"];
  const ext = path.includes(".") ? path.slice(path.lastIndexOf(".") + 1).toLowerCase() : "";
  switch (ext) {
    case "md":
    case "markdown":
      return ["markdown"];
    case "json":
      return ["json"];
    case "js":
    case "mjs":
    case "cjs":
    case "ts":
      return ["js"];
    case "java":
      return ["java"];
    case "sql":
      return ["sql"];
    case "sh":
    case "bash":
      return ["bash"];
    case "ps1":
      return ["powershell"];
    default:
      return ["markdown"];
  }
}

function onTreeSelect(path?: string, data?: Record<string, any>) {
  void selectPath(path, data as SkillFileNode | undefined);
}
</script>

<template>
  <section class="skill-editor">
    <header class="skill-editor__head">
      <div class="skill-editor__title">
        <h2>{{ skill?.name ?? "技能" }}</h2>
        <p>
          <u-tag size="small" :type="skill?.source === 'builtin' ? undefined : 'success'">
            {{ skill?.source === "builtin" ? "内置 · 只读" : "上传 · 可编辑" }}
          </u-tag>
          <span v-if="dirty" class="dirty-dot">未保存</span>
        </p>
      </div>
      <div class="skill-editor__actions">
        <u-button
          v-if="editable && selectedKind === 'file' && !binary"
          type="primary"
          :loading="saving"
          @click="save"
        >
          保存
        </u-button>
      </div>
    </header>

    <div class="skill-editor__body">
      <aside class="tree-panel">
        <div class="tree-head">
          <strong>文件</strong>
          <div v-if="editable" class="tree-head__ops">
            <u-action @run="openCreate('file')">
              <u-icon :size="14"><FileAdd /></u-icon>
            </u-action>
            <u-action @run="openCreate('dir')">
              <u-icon :size="14"><FolderAdd /></u-icon>
            </u-action>
          </div>
        </div>
        <u-scroll class="tree-scroll">
          <u-empty v-if="!loadingTree && tree.length === 0" text="暂无文件" />
          <u-tree
            v-else
            v-model:selected="selectedPath"
            class="file-tree"
            :data="tree"
            label-key="name"
            value-key="path"
            children-key="children"
            selectable
            expand-all
            @update:selected="onTreeSelect"
          >
            <template #default="{ data }">
              <div
                class="tree-node"
                :class="[data.kind === 'dir' ? 'is-dir' : 'is-file', { 'has-actions': editable }]"
              >
                <span class="tree-node__main">
                  <u-icon class="tree-node__icon" :size="14">
                    <FolderOpened v-if="data.kind === 'dir'" />
                    <Books v-else />
                  </u-icon>
                  <span class="tree-node__name">{{ data.name }}</span>
                </span>
                <span v-if="editable" class="tree-node__actions" @click.stop>
                  <u-action title="重命名" @run="openRename(data as SkillFileNode)">
                    <u-icon :size="14"><Edit /></u-icon>
                  </u-action>
                  <u-action
                    title="删除"
                    need-confirm
                    type="danger"
                    @run="removeNode(data as SkillFileNode)"
                  >
                    <u-icon :size="14"><Delete /></u-icon>
                  </u-action>
                </span>
              </div>
            </template>
          </u-tree>
        </u-scroll>
      </aside>

      <section class="editor-panel">
        <u-empty v-if="!selectedPath" text="从左侧选择文件开始浏览或编辑" />
        <u-empty v-else-if="selectedKind === 'dir'" text="已选择目录，可在左侧新建子项或选择文件" />
        <u-empty v-else-if="binary" text="二进制文件不支持在线编辑，请下载 ZIP 后本地处理" />
        <template v-else>
          <div class="editor-head">
            <code>{{ selectedPath }}</code>
            <u-tag v-if="!editable" size="small">只读</u-tag>
          </div>
          <u-code-editor
            v-model="content"
            class="code-pane"
            :langs="editorLangs"
            :readonly="!editable"
          />
        </template>
      </section>
    </div>

    <FormDialog
      v-model="createOpen"
      :title="createKind === 'dir' ? '新建目录' : '新建文件'"
      :model="createForm"
      label-width="80px"
      style="width: 420px"
      @submit="submitCreate"
    >
      <u-input
        label="名称"
        field="name"
        :placeholder="createKind === 'dir' ? '例如 scripts' : '例如 helper.md'"
        :rules="{ required: '必填' }"
      />
      <p v-if="createParent" class="hint">将创建于 {{ createParent }}/</p>
      <p v-else class="hint">将创建于技能根目录</p>
    </FormDialog>

    <FormDialog
      v-model="renameOpen"
      title="重命名"
      :model="renameForm"
      label-width="80px"
      style="width: 420px"
      @submit="submitRename"
    >
      <u-input label="新名称" field="name" :rules="{ required: '必填' }" />
    </FormDialog>
  </section>
</template>

<style scoped lang="scss">
.skill-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1;
  height: 100%;
  min-height: 0;
}

.skill-editor__head {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 16px;
  border-radius: 10px;
  background:
    linear-gradient(
      135deg,
      color-mix(in srgb, var(--u-color-primary, #3b82f6) 8%, transparent),
      transparent 55%
    ),
    var(--u-bg-color-top, #fff);
  border: 1px solid color-mix(in srgb, var(--u-color-border, #e5e7eb) 80%, transparent);
}

.skill-editor__title {
  display: flex;
  flex-direction: column;
  min-width: 0;

  h2 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
  }

  p {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 4px 0 0;
    color: var(--u-color-text-secondary, #666);
    font-size: 13px;
  }
}

.dirty-dot {
  color: var(--u-color-warning, #d97706);
}

.skill-editor__body {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 12px;
  min-height: 0;
  flex: 1;
}

.tree-panel,
.editor-panel {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-radius: 10px;
  background: var(--u-bg-color-top, #fff);
  border: 1px solid color-mix(in srgb, var(--u-color-border, #e5e7eb) 80%, transparent);
  overflow: hidden;
}

.tree-head,
.editor-head {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--u-color-border, #e5e7eb) 80%, transparent);
}

.tree-head__ops {
  display: flex;
  gap: 4px;
}

.tree-scroll {
  flex: 1;
  min-height: 0;
  padding: 8px;
}

.file-tree {
  min-height: 100%;
}

.tree-node {
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-width: 0;
  height: 28px;
}

.tree-node.has-actions {
  padding-right: 44px;
}

.tree-node__main {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.tree-node__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree-node__icon {
  flex-shrink: 0;
  color: var(--u-color-text-secondary, #888);
}

.tree-node.is-dir .tree-node__icon {
  color: var(--u-color-primary, #3b82f6);
}

.tree-node__actions {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
  width: 44px;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.12s ease;
}

.tree-node:hover .tree-node__actions,
.tree-node:focus-within .tree-node__actions {
  opacity: 1;
  pointer-events: auto;
}

.editor-head code {
  font-size: 12px;
  color: var(--u-color-text-secondary, #666);
}

.code-pane {
  flex: 1;
  height: 100%;
  min-height: 0;
  max-height: none;
  margin: 0;
}

.hint {
  margin: 0;
  font-size: 12px;
  color: var(--u-color-text-secondary, #666);
}

@media (max-width: 900px) {
  .skill-editor__body {
    grid-template-columns: 1fr;
  }
}
</style>
