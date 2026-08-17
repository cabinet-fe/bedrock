<script setup lang="ts">
import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  createRequirement,
  createRequirementComment,
  deleteRequirement,
  deleteRequirementAttachment,
  deleteRequirementComment,
  downloadRequirementAttachment,
  getRequirement,
  listRequirementAttachments,
  listRequirementComments,
  listRequirementStatuses,
  updateRequirement,
  updateRequirementComment,
  uploadRequirementAttachment,
} from "@/api/projects";
import type {
  ProductProject,
  ProjectRole,
  Requirement,
  RequirementAttachment,
  RequirementComment,
  RequirementStatusOption,
} from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import { tagType, type TagType } from "@/lib/tag";
import { useAuthStore } from "@/stores/auth";

const PRIORITY_TAG: Record<string, TagType> = {
  low: undefined,
  normal: "info",
  high: "warning",
  urgent: "danger",
};

const PRIORITY_LABEL: Record<string, string> = {
  low: "低",
  normal: "普通",
  high: "高",
  urgent: "紧急",
};

const props = defineProps<{
  project: ProductProject;
  projectRole?: ProjectRole;
  manageAll: boolean;
}>();

const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const auth = useAuthStore();
const tableRef = useTemplateRef("table");
const query = reactive({ keyword: "", status: "", priority: "" });
const dialogOpen = ref(false);
const detailOpen = ref(false);
const editing = ref<Requirement | null>(null);
const selected = ref<Requirement | null>(null);
const comments = ref<RequirementComment[]>([]);
const attachments = ref<RequirementAttachment[]>([]);
const commentText = ref("");
const editingCommentID = ref<number>();
const editingCommentText = ref("");
const requirementStatuses = ref<RequirementStatusOption[]>([]);
const form = reactive({
  title: "",
  description: "",
  status: "",
  priority: "normal",
  assignee_id: undefined as number | undefined,
  tags: "",
});

const formGroups = [
  { key: "meta", title: "基本信息" },
  { key: "content", title: "描述" },
];

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
const canCreateRequirement = computed(
  () => hasPermission("project_requirements:create") && canEditProjectContent.value,
);
const canUpdateRequirement = computed(
  () => hasPermission("project_requirements:update") && canEditProjectContent.value,
);
const canDeleteRequirement = computed(
  () => hasPermission("project_requirements:delete") && canAdminProjectContent.value,
);
const canCreateComment = computed(
  () => hasPermission("project_requirements:create") && canEditProjectContent.value,
);
const statusOptions = computed(() =>
  requirementStatuses.value
    .filter((item) => item.enabled)
    .map((item) => ({ label: item.label, value: item.value })),
);
const filterStatusOptions = computed(() => [
  { label: "全部状态", value: "" },
  ...statusOptions.value,
]);

const columns = defineProTableColumns([
  { key: "title", name: "标题", sortable: true },
  { key: "status", name: "状态", width: 100, align: "center" },
  { key: "priority", name: "优先级", width: 100, align: "center", sortable: true },
  { key: "assignee_id", name: "负责人" },
  {
    key: "updated_at",
    name: "更新时间",
    width: 170,
    align: "center",
    sortable: true,
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 280, align: "center", fixed: "right" },
]);

function defaultStatus() {
  return statusOptions.value[0]?.value ?? "";
}

function statusLabel(value: string) {
  return statusOptions.value.find((item) => item.value === value)?.label ?? value;
}

function canEditComment(comment: RequirementComment) {
  return (
    canUpdateRequirement.value &&
    (canAdminProjectContent.value || comment.created_by === auth.user?.id)
  );
}

function canDeleteComment(comment: RequirementComment) {
  return (
    hasPermission("project_requirements:delete") &&
    canEditProjectContent.value &&
    (canAdminProjectContent.value || comment.created_by === auth.user?.id)
  );
}

async function loadRequirementStatuses() {
  try {
    requirementStatuses.value = await listRequirementStatuses();
    if (!form.status || !statusOptions.value.some((item) => item.value === form.status)) {
      form.status = defaultStatus();
    }
  } catch (error) {
    requirementStatuses.value = [];
    message.error(error instanceof Error ? error.message : "需求状态加载失败");
  }
}

function openCreate() {
  editing.value = null;
  form.status = defaultStatus();
  dialogOpen.value = true;
}

function openEdit(requirement: Requirement) {
  editing.value = requirement;
  o(form).extend(requirement);
  dialogOpen.value = true;
}

async function save() {
  try {
    const input = { ...form };
    if (editing.value) {
      await updateRequirement(props.project.id, editing.value.id, input);
      message.success("需求已更新");
    } else {
      await createRequirement(props.project.id, input);
      message.success("需求已创建");
    }
    dialogOpen.value = false;
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

const remove = bind(async (requirement: Requirement) => {
  try {
    await deleteRequirement(props.project.id, requirement.id);
    message.success("需求已删除");
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除失败");
  }
});

const showDetail = bind(async (requirement: Requirement) => {
  try {
    selected.value = await getRequirement(props.project.id, requirement.id);
    const [commentItems, attachmentItems] = await Promise.all([
      listRequirementComments(props.project.id, requirement.id),
      listRequirementAttachments(props.project.id, requirement.id),
    ]);
    comments.value = commentItems;
    attachments.value = attachmentItems;
    commentText.value = "";
    editingCommentID.value = undefined;
    editingCommentText.value = "";
    detailOpen.value = true;
  } catch (error) {
    message.error(error instanceof Error ? error.message : "读取需求详情失败");
  }
});

async function addComment() {
  if (!selected.value || !commentText.value.trim()) return;
  try {
    await createRequirementComment(props.project.id, selected.value.id, commentText.value);
    comments.value = await listRequirementComments(props.project.id, selected.value.id);
    commentText.value = "";
  } catch (error) {
    message.error(error instanceof Error ? error.message : "评论失败");
  }
}

function startCommentEdit(comment: RequirementComment) {
  editingCommentID.value = comment.id;
  editingCommentText.value = comment.content;
}

function cancelCommentEdit() {
  editingCommentID.value = undefined;
  editingCommentText.value = "";
}

async function saveCommentEdit(comment: RequirementComment) {
  if (!selected.value || !editingCommentText.value.trim()) return;
  try {
    const updated = await updateRequirementComment(
      props.project.id,
      selected.value.id,
      comment.id,
      editingCommentText.value,
    );
    comments.value = comments.value.map((item) => (item.id === updated.id ? updated : item));
    cancelCommentEdit();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "评论更新失败");
  }
}

async function removeComment(comment: RequirementComment) {
  if (!selected.value) return;
  try {
    await deleteRequirementComment(props.project.id, selected.value.id, comment.id);
    comments.value = comments.value.filter((item) => item.id !== comment.id);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除评论失败");
  }
}

async function uploadFiles(files: File[]) {
  if (!selected.value || !files[0]) return;
  try {
    await uploadRequirementAttachment(props.project.id, selected.value.id, files[0]);
    attachments.value = await listRequirementAttachments(props.project.id, selected.value.id);
    message.success("附件已上传");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "附件上传失败");
  }
}

async function removeAttachment(attachment: RequirementAttachment) {
  if (!selected.value) return;
  try {
    await deleteRequirementAttachment(props.project.id, selected.value.id, attachment.id);
    attachments.value = attachments.value.filter((item) => item.id !== attachment.id);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除附件失败");
  }
}

async function download(attachment: RequirementAttachment) {
  if (!selected.value) return;
  try {
    await downloadRequirementAttachment(
      props.project.id,
      selected.value.id,
      attachment.id,
      attachment.filename,
    );
  } catch (error) {
    message.error(error instanceof Error ? error.message : "下载失败");
  }
}

onMounted(() => void loadRequirementStatuses());
</script>

<template>
  <section class="panel">
    <ProTable
      ref="table"
      :url="`/projects/${project.id}/requirements`"
      :query="query"
      :columns="columns"
      pagination
      :auto-query-fields="['status', 'priority']"
    >
      <template #filters>
        <u-input v-model="query.keyword" placeholder="标题或标签" style="width: 200px" />
        <u-select v-model="query.status" :options="filterStatusOptions" style="width: 120px" />
        <u-select
          v-model="query.priority"
          :options="[
            { label: '全部优先级', value: '' },
            { label: '低', value: 'low' },
            { label: '普通', value: 'normal' },
            { label: '高', value: 'high' },
            { label: '紧急', value: 'urgent' },
          ]"
          style="width: 120px"
        />
      </template>
      <template #toolbar>
        <u-button v-if="canCreateRequirement" type="primary" @click.prevent="openCreate">
          新建需求
        </u-button>
      </template>
      <template #column:title="{ rowData }">
        <u-action @run="showDetail(rowData as Requirement)">
          {{ (rowData as Requirement).title }}
        </u-action>
      </template>
      <template #column:status="{ rowData }">
        <u-tag size="small" type="info">
          {{ statusLabel((rowData as Requirement).status) }}
        </u-tag>
      </template>
      <template #column:priority="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as Requirement).priority, PRIORITY_TAG)">
          {{
            PRIORITY_LABEL[(rowData as Requirement).priority] ?? (rowData as Requirement).priority
          }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as Requirement).id">
          <u-action @run="showDetail(rowData as Requirement)">详情</u-action>
          <u-action v-if="canUpdateRequirement" @run="openEdit(rowData as Requirement)">
            编辑
          </u-action>
          <u-action
            v-if="canDeleteRequirement"
            need-confirm
            type="danger"
            @run="remove(rowData as Requirement)"
          >
            删除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑需求' : '新建需求'"
      :model="form"
      :groups="formGroups"
      label-width="100px"
      style="width: 640px"
      @submit="save"
    >
      <template #group:meta>
        <u-input label="标题" field="title" :rules="{ required: '必填' }" />
        <u-select label="状态" field="status" :options="statusOptions" />
        <u-select
          label="优先级"
          field="priority"
          :options="[
            { label: '低', value: 'low' },
            { label: '普通', value: 'normal' },
            { label: '高', value: 'high' },
            { label: '紧急', value: 'urgent' },
          ]"
        />
        <u-number-input label="负责人 ID" field="assignee_id" />
        <u-input label="标签" field="tags" />
      </template>
      <template #group:content>
        <u-textarea label="描述" field="description" :rows="6" span="full" />
      </template>
    </FormDialog>

    <u-dialog v-model="detailOpen" :title="selected?.title || '需求详情'" style="width: 760px">
      <template v-if="selected">
        <p class="description">{{ selected.description || "暂无描述" }}</p>
        <section class="detail-section">
          <div class="detail-head">
            <strong>附件</strong>
            <u-file-picker v-if="canUpdateRequirement" accept="*/*" @pick="uploadFiles" />
          </div>
          <u-empty v-if="!attachments.length" text="暂无附件" />
          <div v-for="attachment in attachments" :key="attachment.id" class="attachment-row">
            <u-action @run="download(attachment)">{{ attachment.filename }}</u-action>
            <u-action v-if="canUpdateRequirement" type="danger" @run="removeAttachment(attachment)">
              删除
            </u-action>
          </div>
        </section>
        <section class="detail-section">
          <strong>评论</strong>
          <div v-for="comment in comments" :key="comment.id" class="comment">
            <template v-if="editingCommentID === comment.id">
              <u-textarea v-model="editingCommentText" :rows="3" />
              <div class="comment-actions">
                <u-action @run="saveCommentEdit(comment)">保存</u-action>
                <u-action @run="cancelCommentEdit">取消</u-action>
              </div>
            </template>
            <p v-else>{{ comment.content }}</p>
            <small>用户 #{{ comment.created_by }} · {{ formatDateTime(comment.created_at) }}</small>
            <u-action v-if="canEditComment(comment)" @run="startCommentEdit(comment)"
              >编辑</u-action
            >
            <u-action v-if="canDeleteComment(comment)" type="danger" @run="removeComment(comment)">
              删除
            </u-action>
          </div>
          <div v-if="canCreateComment" class="comment-input">
            <u-textarea v-model="commentText" :rows="3" placeholder="添加评论" />
            <u-button type="primary" @click="addComment">发送评论</u-button>
          </div>
        </section>
      </template>
    </u-dialog>
  </section>
</template>

<style scoped>
.panel {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  gap: 16px;
}
.detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.detail-section p {
  margin: 0;
}
.description {
  white-space: pre-wrap;
  line-height: 1.7;
}
.detail-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 20px;
}
.attachment-row,
.comment-input {
  display: flex;
  align-items: center;
  gap: 10px;
}
.comment-input {
  align-items: flex-end;
}
.comment-actions {
  display: flex;
  gap: 10px;
}
.comment-input :deep(.u-textarea) {
  flex: 1;
}
.comment {
  padding: 10px;
  border-radius: 6px;
  background: var(--u-bg-color-middle, #f6f7f9);
}
.comment p {
  margin: 0 0 6px;
  white-space: pre-wrap;
}
.comment small {
  margin-right: 10px;
  color: var(--u-text-color-assist, #7c8494);
}
</style>
