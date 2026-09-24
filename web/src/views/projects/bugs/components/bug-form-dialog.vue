<script setup lang="ts">
defineOptions({ name: "BugFormDialog" });

import { computed, reactive, ref, useTemplateRef, watch } from "vue";
import { message, type FormExposed } from "@veltra/desktop";

import {
  createProjectBug,
  listMembers,
  updateProjectBug,
  uploadProjectBugAttachment,
} from "@/api/projects";
import { listRepositoryBranches } from "@/api/resource";
import type { BugPriority, BugSeverity, ProjectBug } from "@/api/types";
import RepoSelect from "@/components/repo-select/repo-select.vue";
import { BUG_PRIORITY_OPTIONS, BUG_SEVERITY_OPTIONS, BUG_STATUS_OPTIONS } from "@/lib/tag";
import BugPendingFiles from "./bug-pending-files.vue";
import { clipboardImages } from "./attachment-staging";
import { compressImageToDataUrl } from "./image-data-url";
import { hasRichContent, toEditorHtml } from "./rich-text";

const open = defineModel<boolean>({ required: true });

const props = defineProps<{
  projectId?: number;
  bug?: ProjectBug | null;
  projectOptions?: { label: string; value: number }[];
  /** Preset status for quick-create from a kanban column. */
  initialStatus?: string;
}>();

const emit = defineEmits<{
  saved: [bug: ProjectBug];
}>();

const formRef = useTemplateRef<FormExposed>("form");
const descEditor = useTemplateRef<{
  uploadImages: (upload: (file: File) => Promise<string>) => Promise<string>;
}>("descEditor");
const busy = ref(false);

const isEdit = computed(() => !!props.bug);
const effectiveProjectId = computed(() => props.projectId || form.project_id);

const defaultForm = {
  project_id: undefined as number | undefined,
  title: "",
  description: "",
  status: "open",
  severity: "normal" as BugSeverity,
  priority: "normal" as BugPriority,
  assignee_id: undefined as number | undefined,
  repository_id: undefined as number | undefined,
  branch: "",
};
const form = reactive({ ...defaultForm });

const memberOptions = ref<{ label: string; value: number }[]>([]);
const branchOptions = ref<{ label: string; value: string }[]>([]);
const loadingBranches = ref(false);
const pendingFiles = ref<File[]>([]);

async function loadMembers(pid?: number) {
  if (!pid) {
    memberOptions.value = [];
    return;
  }
  try {
    const items = await listMembers(pid);
    memberOptions.value = items.map((m) => ({
      label: m.display_name ? `${m.display_name} (${m.username})` : m.username || `#${m.user_id}`,
      value: m.user_id,
    }));
  } catch {
    memberOptions.value = [];
  }
}

async function loadBranches(repoId?: number) {
  if (!repoId) {
    branchOptions.value = [];
    return;
  }
  loadingBranches.value = true;
  try {
    const { items } = await listRepositoryBranches(repoId);
    branchOptions.value = items.map((b) => ({ label: b, value: b }));
  } catch {
    branchOptions.value = [];
  } finally {
    loadingBranches.value = false;
  }
}

function handleRepoChange(repoId?: number) {
  form.repository_id = repoId;
  form.branch = "";
  void loadBranches(repoId);
}

function resetForm() {
  pendingFiles.value = [];
  if (props.bug) {
    Object.assign(form, defaultForm, {
      project_id: props.bug.project_id,
      title: props.bug.title,
      description: toEditorHtml(props.bug.description || ""),
      severity: props.bug.severity,
      priority: props.bug.priority,
      assignee_id: props.bug.assignee_id ?? undefined,
      repository_id: props.bug.repository_id ?? undefined,
      branch: props.bug.branch || "",
    });
    void loadMembers(props.bug.project_id);
    if (props.bug.repository_id) {
      void loadBranches(props.bug.repository_id);
    }
  } else {
    Object.assign(form, defaultForm, {
      project_id: props.projectId,
      status: props.initialStatus || defaultForm.status,
    });
    if (props.projectId) {
      void loadMembers(props.projectId);
    } else {
      memberOptions.value = [];
    }
    branchOptions.value = [];
  }
  formRef.value?.clearValidate();
}

watch(
  open,
  (visible) => {
    if (visible) {
      resetForm();
    }
  },
  { immediate: true },
);

watch(
  () => form.project_id,
  (pid) => {
    if (!props.projectId && pid) {
      void loadMembers(pid);
    }
  },
);

function handleFormPaste(event: ClipboardEvent) {
  // The rich text editor consumes image pastes itself (defaultPrevented);
  // only images pasted outside it fall back to the attachment staging list.
  if (event.defaultPrevented) return;
  const images = clipboardImages(event);
  if (!images) return;
  pendingFiles.value.push(...images);
  message.success(`已添加 ${images.length} 张截图，提交后自动上传`);
}

async function handleSubmit() {
  if (busy.value) return;
  const valid = await formRef.value?.validate();
  if (!valid) return;

  const pid = effectiveProjectId.value;
  if (!pid) {
    message.warning("请选择所属项目");
    return;
  }

  busy.value = true;
  try {
    // Embed pasted/dropped images as compressed data URLs and take the
    // final HTML back from the editor (blob previews are meaningless after save).
    let description = form.description;
    try {
      description =
        (await descEditor.value?.uploadImages(compressImageToDataUrl)) ?? form.description;
    } catch {
      message.warning("截图处理失败，请移除后重新粘贴");
      return;
    }

    const payload = {
      title: form.title.trim(),
      description: hasRichContent(description) ? description : undefined,
      status: isEdit.value ? undefined : form.status,
      severity: form.severity,
      priority: form.priority,
      assignee_id: form.assignee_id ?? null,
      repository_id: form.repository_id ?? null,
      branch: form.branch?.trim() || undefined,
    };

    let saved: ProjectBug;
    if (props.bug) {
      saved = await updateProjectBug(pid, props.bug.id, payload);
      message.success("缺陷更新成功");
    } else {
      saved = await createProjectBug(pid, payload);
      message.success("缺陷创建成功");
    }

    if (pendingFiles.value.length) {
      let failed = 0;
      for (const file of pendingFiles.value) {
        try {
          await uploadProjectBugAttachment(pid, saved.id, file);
        } catch {
          failed++;
        }
      }
      if (failed > 0) {
        message.warning(`${failed} 个附件上传失败，可稍后在缺陷详情中重新上传`);
      }
    }
    pendingFiles.value = [];

    open.value = false;
    emit("saved", saved);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存缺陷失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <u-dialog
    v-model="open"
    :title="isEdit ? '编辑缺陷' : '新建缺陷'"
    style="width: 860px; max-width: 95vw"
  >
    <div class="bug-form-header-bar">
      <span class="bug-form-header-tip">填写缺陷基本信息与协同责任人</span>
    </div>

    <u-form ref="form" :model="form" label-width="96px" :cols="2" @paste="handleFormPaste">
      <u-select
        v-if="!props.projectId && projectOptions?.length"
        v-model="form.project_id"
        label="所属项目"
        field="project_id"
        :options="projectOptions"
        :rules="{ required: '请选择所属项目' }"
        placeholder="选择项目"
        span="full"
      />

      <u-input
        v-model="form.title"
        label="缺陷标题"
        field="title"
        :rules="{ required: '请输入缺陷标题' }"
        placeholder="简要概括缺陷现象或错误"
        span="full"
      />

      <u-select
        v-if="!isEdit"
        v-model="form.status"
        label="状态"
        field="status"
        :options="BUG_STATUS_OPTIONS"
      />
      <u-select
        v-model="form.severity"
        label="严重程度"
        field="severity"
        :options="BUG_SEVERITY_OPTIONS"
        placeholder="选择严重程度"
      />
      <u-select
        v-model="form.priority"
        label="优先级"
        field="priority"
        :options="BUG_PRIORITY_OPTIONS"
        placeholder="选择优先级"
      />

      <u-select
        v-model="form.assignee_id"
        label="经办人"
        field="assignee_id"
        :options="memberOptions"
        clearable
        filterable
        placeholder="指派经办人 (可选)"
      />

      <u-form-item label="关联代码仓" field="repository_id">
        <RepoSelect
          :model-value="form.repository_id"
          clearable
          min-width="0"
          placeholder="关联代码仓 (可选)"
          style="width: 100%; min-width: 0"
          @update:model-value="handleRepoChange"
        />
      </u-form-item>
      <u-select
        v-model="form.branch"
        label="分支"
        field="branch"
        :options="branchOptions"
        filterable
        creatable
        clearable
        :disabled="!form.repository_id"
        :placeholder="
          form.repository_id ? (loadingBranches ? '加载分支…' : '选择或输入分支') : '请先选择代码仓'
        "
      />

      <u-rich-text-editor
        ref="descEditor"
        v-model="form.description"
        label="缺陷描述"
        field="description"
        placeholder="描述现象、复现步骤与期望表现；支持标题 / 列表 / 引用 / 链接，可直接粘贴或拖入截图"
        span="full"
      />

      <u-form-item label="附件" span="full">
        <BugPendingFiles v-model="pendingFiles" />
        <span class="attach-tip">描述里直接粘贴的截图会内嵌保存；这里添加的文件作为附件上传</span>
      </u-form-item>
    </u-form>

    <template #footer="{ close }">
      <u-button @click="close()">取消</u-button>
      <u-button type="primary" :loading="busy" @click="handleSubmit">保存</u-button>
    </template>
  </u-dialog>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.bug-form-header-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  padding: 8px 12px;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, muted);
}

.bug-form-header-tip {
  font-size: 13px;
  color: fn.use-var(text-color, secondary);
}

.attach-tip {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}
</style>
