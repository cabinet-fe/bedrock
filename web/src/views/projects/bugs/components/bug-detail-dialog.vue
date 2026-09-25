<script setup lang="ts">
defineOptions({ name: "BugDetailDialog" });

import { computed, ref, watch } from "vue";
import { message } from "@veltra/desktop";

import {
  deleteProjectBugAttachment,
  deleteProjectBugComment,
  downloadProjectBugAttachment,
  getProjectBug,
  listProjectBugActivities,
  listProjectBugAttachments,
  listProjectBugComments,
  transitionProjectBugStatus,
  updateProjectBugComment,
  createProjectBugComment,
  uploadProjectBugAttachment,
  uploadProjectBugCommentAttachment,
} from "@/api/projects";
import type {
  BugActivity,
  BugAttachment,
  BugComment,
  BugStatus,
  ProjectBug,
  ProjectRole,
} from "@/api/types";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import {
  BUG_PRIORITY_TAG,
  BUG_SEVERITY_TAG,
  BUG_STATUS_TAG,
  bugPriorityLabel,
  bugSeverityLabel,
  bugStatusLabel,
  tagType,
} from "@/lib/tag";
import { useAuthStore } from "@/stores/auth";
import { useRepositoryStore } from "@/stores/repositories";
import BugAttachmentPreview from "./bug-attachment-preview.vue";
import BugPendingFiles from "./bug-pending-files.vue";
import { clipboardImages } from "./attachment-staging";
import { isRichText } from "./rich-text";

const open = defineModel<boolean>({ required: true });

const props = defineProps<{
  projectId?: number;
  bugId?: number;
  projectRole?: ProjectRole;
  manageAll?: boolean;
}>();

const emit = defineEmits<{
  edit: [bug: ProjectBug];
  refresh: [];
}>();

const { hasPermission } = usePermission();
const auth = useAuthStore();
const repoStore = useRepositoryStore();

const bug = ref<ProjectBug | null>(null);
const loading = ref(false);
const activities = ref<BugActivity[]>([]);
const comments = ref<BugComment[]>([]);
const attachments = ref<BugAttachment[]>([]);

const activeTab = ref<"activities" | "comments" | "attachments">("activities");
const collabTabs = computed(() => [
  { key: "activities", name: `流转记录 (${activities.value.length})` },
  { key: "comments", name: `讨论评论 (${comments.value.length})` },
  { key: "attachments", name: `附件文件 (${attachments.value.length})` },
]);

const canEditProjectContent = computed(() => {
  if (props.manageAll) return true;
  if (props.projectRole) {
    return (
      props.projectRole === "owner" ||
      props.projectRole === "admin" ||
      props.projectRole === "member"
    );
  }
  return true;
});

const canAdminProjectContent = computed(() => {
  if (props.manageAll) return true;
  if (props.projectRole) {
    return props.projectRole === "owner" || props.projectRole === "admin";
  }
  return false;
});

const canUpdateBug = computed(
  () => hasPermission("project_bugs:update") && canEditProjectContent.value,
);
const canDeleteBug = computed(
  () => hasPermission("project_bugs:delete") && (canAdminProjectContent.value || props.manageAll),
);
const canCreateComment = computed(
  () => hasPermission("project_bugs:create") && canEditProjectContent.value,
);

const ALL_STATUSES: BugStatus[] = ["open", "in_progress", "resolved", "closed", "rejected"];
const transitionTargets = computed(() => ALL_STATUSES.filter((s) => s !== bug.value?.status));

// Status transition dialog
const transitionDialogOpen = ref(false);
const targetStatus = ref<BugStatus>("open");
const transitionComment = ref("");
const transitioning = ref(false);

// Comments
const newCommentText = ref("");
const submittingComment = ref(false);
const commentPendingFiles = ref<File[]>([]);
const editingCommentId = ref<number | undefined>(undefined);
const editingCommentContent = ref("");
const savingComment = ref(false);

function formatActivityAction(act: BugActivity): string {
  if (act.from_status && act.to_status) {
    return `将状态由「${bugStatusLabel(act.from_status)}」流转为「${bugStatusLabel(act.to_status)}」`;
  }
  if (act.action === "create") return "创建了缺陷";
  if (act.action === "update") return "更新了缺陷信息";
  if (act.action === "status_transition") {
    if (act.to_status) return `将状态流转为「${bugStatusLabel(act.to_status)}」`;
    return "流转了缺陷状态";
  }
  return act.action;
}

function resolveRepoName(b: ProjectBug): string {
  if (b.repository_name) return b.repository_name;
  if (b.repository_id) {
    return repoStore.nameMap.get(b.repository_id) ?? `#${b.repository_id}`;
  }
  return "—";
}

async function loadDetail() {
  const pid = props.projectId || bug.value?.project_id;
  const bid = props.bugId;
  if (!pid || !bid) return;

  loading.value = true;
  try {
    const [bugData, activityList, commentList, attachmentList] = await Promise.all([
      getProjectBug(pid, bid),
      listProjectBugActivities(pid, bid).catch(() => []),
      listProjectBugComments(pid, bid).catch(() => []),
      listProjectBugAttachments(pid, bid).catch(() => []),
    ]);
    bug.value = bugData;
    activities.value = activityList;
    comments.value = commentList;
    attachments.value = attachmentList;
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载缺陷详情失败");
  } finally {
    loading.value = false;
  }
}

function openTransition(status: BugStatus) {
  targetStatus.value = status;
  transitionComment.value = "";
  transitionDialogOpen.value = true;
}

async function confirmTransition() {
  if (!bug.value) return;
  const pid = bug.value.project_id;
  const bid = bug.value.id;
  transitioning.value = true;
  try {
    const updated = await transitionProjectBugStatus(pid, bid, {
      status: targetStatus.value,
      comment: transitionComment.value.trim() || undefined,
    });
    bug.value = updated;
    message.success(`缺陷状态已流转为「${bugStatusLabel(targetStatus.value)}」`);
    transitionDialogOpen.value = false;
    activities.value = await listProjectBugActivities(pid, bid);
    emit("refresh");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "状态流转失败");
  } finally {
    transitioning.value = false;
  }
}

function handleEditBug() {
  if (!bug.value) return;
  emit("edit", bug.value);
  open.value = false;
}

function canManageComment(c: BugComment) {
  return canUpdateBug.value && (canAdminProjectContent.value || c.created_by === auth.user?.id);
}

function handleCommentPaste(event: ClipboardEvent) {
  const images = clipboardImages(event);
  if (!images) return;
  commentPendingFiles.value.push(...images);
  message.success(`已添加 ${images.length} 张截图，随评论一起发送`);
}

async function handleAddComment() {
  if (!bug.value || !newCommentText.value.trim()) return;
  submittingComment.value = true;
  try {
    const pid = bug.value.project_id;
    const bid = bug.value.id;
    const newComment = await createProjectBugComment(pid, bid, newCommentText.value.trim());
    newComment.attachments = [];
    let failed = 0;
    for (const file of commentPendingFiles.value) {
      try {
        newComment.attachments.push(
          await uploadProjectBugCommentAttachment(pid, bid, newComment.id, file),
        );
      } catch {
        failed++;
      }
    }
    if (failed > 0) {
      message.warning(`${failed} 个附件上传失败，可重新发表评论时再试`);
    }
    commentPendingFiles.value = [];
    comments.value.push(newComment);
    attachments.value.push(...newComment.attachments);
    newCommentText.value = "";
    message.success("评论发表成功");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "发表评论失败");
  } finally {
    submittingComment.value = false;
  }
}

function startEditComment(c: BugComment) {
  editingCommentId.value = c.id;
  editingCommentContent.value = c.content;
}

function cancelEditComment() {
  editingCommentId.value = undefined;
  editingCommentContent.value = "";
}

async function saveEditComment(c: BugComment) {
  if (!bug.value || !editingCommentContent.value.trim()) return;
  savingComment.value = true;
  try {
    const updated = await updateProjectBugComment(
      bug.value.project_id,
      bug.value.id,
      c.id,
      editingCommentContent.value.trim(),
    );
    const idx = comments.value.findIndex((item) => item.id === c.id);
    if (idx !== -1) comments.value[idx] = updated;
    cancelEditComment();
    message.success("评论更新成功");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "更新评论失败");
  } finally {
    savingComment.value = false;
  }
}

async function handleDeleteComment(c: BugComment) {
  if (!bug.value) return;
  try {
    await deleteProjectBugComment(bug.value.project_id, bug.value.id, c.id);
    comments.value = comments.value.filter((item) => item.id !== c.id);
    for (const att of c.attachments ?? []) {
      forgetAttachment(att);
    }
    message.success("评论已删除");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除评论失败");
  }
}

async function handleUploadAttachment(files: File[]) {
  if (!bug.value || !files[0]) return;
  try {
    const att = await uploadProjectBugAttachment(bug.value.project_id, bug.value.id, files[0]);
    attachments.value.push(att);
    message.success("附件上传成功");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "上传附件失败");
  }
}

async function handleDownloadAttachment(att: BugAttachment) {
  if (!bug.value) return;
  try {
    await downloadProjectBugAttachment(bug.value.project_id, bug.value.id, att.id, att.filename);
  } catch (error) {
    message.error(error instanceof Error ? error.message : "下载附件失败");
  }
}

// Removes an attachment from both the bug-level tab list and the owning
// comment's inline list (the backend already purged the storage object).
function forgetAttachment(att: BugAttachment) {
  attachments.value = attachments.value.filter((item) => item.id !== att.id);
  for (const c of comments.value) {
    if (c.attachments?.length) {
      c.attachments = c.attachments.filter((item) => item.id !== att.id);
    }
  }
}

async function handleDeleteAttachment(att: BugAttachment) {
  if (!bug.value) return;
  try {
    await deleteProjectBugAttachment(bug.value.project_id, bug.value.id, att.id);
    forgetAttachment(att);
    message.success("附件已删除");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "删除附件失败");
  }
}

watch(
  open,
  (visible) => {
    if (visible) {
      void loadDetail();
      void repoStore.load();
    }
  },
  { immediate: true },
);

watch(
  () => props.bugId,
  (bid) => {
    if (open.value && bid) {
      void loadDetail();
    }
  },
);
</script>

<template>
  <u-dialog
    v-model="open"
    :title="bug ? `缺陷详情 #${bug.id}` : '缺陷详情'"
    style="width: 860px; max-width: 95vw"
  >
    <div v-if="loading && !bug" class="detail-loading">加载缺陷详情中...</div>

    <div v-else-if="bug" class="bug-detail-content">
      <!-- 头部状态与主要信息 -->
      <div class="bug-detail-header">
        <div class="header-main">
          <div class="header-tags">
            <u-tag size="small" :type="tagType(bug.status, BUG_STATUS_TAG)">
              {{ bugStatusLabel(bug.status) }}
            </u-tag>
            <u-tag size="small" :type="tagType(bug.severity, BUG_SEVERITY_TAG)">
              {{ bugSeverityLabel(bug.severity) }}
            </u-tag>
            <u-tag size="small" :type="tagType(bug.priority, BUG_PRIORITY_TAG)">
              {{ bugPriorityLabel(bug.priority) }}
            </u-tag>
          </div>
          <h2 class="bug-title">{{ bug.title }}</h2>
        </div>
        <div v-if="canUpdateBug" class="header-actions">
          <u-button size="small" @click="handleEditBug">编辑缺陷</u-button>
        </div>
      </div>

      <!-- 快捷流转状态栏 -->
      <div class="quick-status-bar">
        <span class="quick-status-label">快捷流转状态：</span>
        <div class="quick-status-buttons">
          <u-button
            v-for="st in transitionTargets"
            :key="st"
            size="small"
            type="secondary"
            :disabled="!canUpdateBug"
            @click="openTransition(st)"
          >
            {{ bugStatusLabel(st) }}
          </u-button>
        </div>
      </div>

      <!-- 属性元数据栏 -->
      <div class="metadata-grid">
        <div class="metadata-item">
          <span class="meta-label">所属项目</span>
          <span class="meta-value">{{ bug.project_name || `#${bug.project_id}` }}</span>
        </div>
        <div class="metadata-item">
          <span class="meta-label">经办人</span>
          <span class="meta-value">{{
            bug.assignee_name || bug.assignee_username || "未指派"
          }}</span>
        </div>
        <div class="metadata-item">
          <span class="meta-label">关联代码仓</span>
          <span class="meta-value">{{ resolveRepoName(bug) }}</span>
        </div>
        <div class="metadata-item">
          <span class="meta-label">关联分支</span>
          <span class="meta-value">{{ bug.branch || "—" }}</span>
        </div>
        <div class="metadata-item">
          <span class="meta-label">创建人</span>
          <span class="meta-value">{{
            bug.creator_name || bug.creator_username || `#${bug.created_by}`
          }}</span>
        </div>
        <div class="metadata-item">
          <span class="meta-label">创建时间</span>
          <span class="meta-value">{{ formatDateTime(bug.created_at) }}</span>
        </div>
      </div>

      <!-- 缺陷描述 -->
      <div class="detail-block">
        <div class="detail-block-title">缺陷描述</div>
        <div class="bug-description-box">
          <!-- eslint-disable-next-line vue/no-v-html -- content is editor-generated whitelist HTML -->
          <div
            v-if="isRichText(bug.description)"
            class="bug-description-rich"
            v-html="bug.description"
          />
          <pre v-else class="bug-description-text">{{ bug.description || "暂无描述" }}</pre>
        </div>
      </div>

      <!-- 协同协作 Tab：流转记录、评论、附件 -->
      <div class="collab-tabs-container">
        <u-tabs v-model="activeTab" :items="collabTabs" />

        <!-- 活动流转记录 -->
        <div v-if="activeTab === 'activities'" class="collab-tab-pane">
          <div v-if="activities.length" class="activity-timeline">
            <div v-for="item in activities" :key="item.id" class="activity-row">
              <div class="activity-dot" />
              <div class="activity-main">
                <div class="activity-meta">
                  <span class="activity-user">{{
                    item.creator_name || item.creator_username || "系统"
                  }}</span>
                  <span class="activity-action">{{ formatActivityAction(item) }}</span>
                  <span class="activity-time">{{ formatDateTime(item.created_at) }}</span>
                </div>
                <div v-if="item.comment" class="activity-comment">“{{ item.comment }}”</div>
              </div>
            </div>
          </div>
          <u-empty v-else text="暂无流转记录" />
        </div>

        <!-- 讨论评论 -->
        <div v-else-if="activeTab === 'comments'" class="collab-tab-pane">
          <div v-if="comments.length" class="comments-list">
            <div v-for="c in comments" :key="c.id" class="comment-card">
              <div class="comment-head">
                <span class="comment-author">
                  {{ c.creator_name || c.creator_username || `用户#${c.created_by}` }}
                </span>
                <span class="comment-time">{{ formatDateTime(c.created_at) }}</span>
                <div v-if="canManageComment(c)" class="comment-actions">
                  <u-action @run="startEditComment(c)">编辑</u-action>
                  <u-action type="danger" need-confirm @run="handleDeleteComment(c)">删除</u-action>
                </div>
              </div>

              <div v-if="editingCommentId === c.id" class="comment-edit-form">
                <u-textarea v-model="editingCommentContent" :rows="3" />
                <div class="comment-edit-btns">
                  <u-button size="small" @click="cancelEditComment">取消</u-button>
                  <u-button
                    size="small"
                    type="primary"
                    :loading="savingComment"
                    @click="saveEditComment(c)"
                  >
                    保存
                  </u-button>
                </div>
              </div>
              <div v-else class="comment-body">
                {{ c.content }}
              </div>

              <div v-if="c.attachments?.length" class="comment-attachments">
                <BugAttachmentPreview
                  v-for="att in c.attachments"
                  :key="att.id"
                  :attachment="att"
                  :project-id="bug.project_id"
                  :bug-id="bug.id"
                  :can-manage="canManageComment(c)"
                  @download="handleDownloadAttachment"
                  @delete="handleDeleteAttachment"
                />
              </div>
            </div>
          </div>
          <u-empty v-else text="暂无讨论评论，欢迎留下排查进展" />

          <div v-if="canCreateComment" class="comment-input-area" @paste="handleCommentPaste">
            <u-textarea
              v-model="newCommentText"
              :rows="3"
              placeholder="发表评论或排查协同进展...（支持粘贴截图）"
            />
            <BugPendingFiles v-model="commentPendingFiles" />
            <div class="comment-submit-row">
              <u-button
                type="primary"
                size="small"
                :loading="submittingComment"
                :disabled="!newCommentText.trim()"
                @click="handleAddComment"
              >
                发表评论
              </u-button>
            </div>
          </div>
        </div>

        <!-- 附件列表 -->
        <div v-else-if="activeTab === 'attachments'" class="collab-tab-pane">
          <div class="attachments-toolbar">
            <span class="attachments-count">共 {{ attachments.length }} 个附件</span>
            <u-file-picker v-if="canUpdateBug" accept="*/*" @pick="handleUploadAttachment" />
          </div>
          <div v-if="attachments.length" class="attachments-list">
            <BugAttachmentPreview
              v-for="att in attachments"
              :key="att.id"
              :attachment="att"
              :project-id="bug.project_id"
              :bug-id="bug.id"
              :can-manage="canUpdateBug || canDeleteBug"
              @download="handleDownloadAttachment"
              @delete="handleDeleteAttachment"
            />
          </div>
          <u-empty v-else text="暂无附件文件" />
        </div>
      </div>
    </div>

    <template #footer="{ close }">
      <u-button @click="close()">关闭</u-button>
    </template>

    <!-- 状态流转确认弹窗 -->
    <u-dialog
      v-model="transitionDialogOpen"
      :title="`流转状态至「${bugStatusLabel(targetStatus)}」`"
      style="width: 500px"
    >
      <div class="transition-dialog-body">
        <p class="transition-tip">
          确定将当前缺陷状态流转为
          <strong>{{ bugStatusLabel(targetStatus) }}</strong> 吗？
        </p>
        <u-textarea
          v-model="transitionComment"
          :rows="3"
          placeholder="可输入处理说明、复现/修复备注 (可选)..."
        />
      </div>
      <template #footer="{ close }">
        <u-button @click="close()">取消</u-button>
        <u-button type="primary" :loading="transitioning" @click="confirmTransition">
          确认流转
        </u-button>
      </template>
    </u-dialog>
  </u-dialog>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.detail-loading {
  padding: 36px 0;
  text-align: center;
  color: fn.use-var(text-color, secondary);
}

.bug-detail-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.bug-detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.header-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.bug-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
  line-height: 1.4;
}

.quick-status-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, muted);
}

.quick-status-label {
  font-size: 13px;
  color: fn.use-var(text-color, secondary);
}

.quick-status-buttons {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.metadata-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  padding: 10px 14px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.metadata-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.meta-label {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.meta-value {
  font-size: 13px;
  color: fn.use-var(text-color, title);
  word-break: break-word;
}

.detail-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.detail-block-title {
  font-size: 13px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.bug-description-box {
  padding: 10px 12px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
  max-height: 320px;
  overflow-y: auto;
}

.bug-description-text {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  color: fn.use-var(text-color, default);
  font-family: inherit;
}

.bug-description-rich {
  font-size: 13px;
  line-height: 1.6;
  color: fn.use-var(text-color, default);
  word-break: break-word;

  :deep(p) {
    margin: 0 0 4px;
  }

  :deep(h1, .u-rte-h1) {
    margin: 8px 0 4px;
    font-size: 16px;
    font-weight: 600;
  }

  :deep(h2, h3, h4, .u-rte-h2, .u-rte-h3) {
    margin: 8px 0 4px;
    font-size: 14px;
    font-weight: 600;
  }

  :deep(ul, ol) {
    margin: 4px 0;
    padding-left: 20px;
  }

  :deep(blockquote) {
    margin: 4px 0;
    padding: 2px 10px;
    border-left: 3px solid fn.use-var(border-color, default);
    color: fn.use-var(text-color, secondary);
  }

  :deep(a) {
    color: fn.use-var(color, primary);
  }

  // Embedded screenshots (compressed data URLs written by the editor)
  :deep(img.u-rte-image) {
    display: block;
    max-width: 100%;
    margin: 6px 0;
    border: fn.use-var(border, muted);
    border-radius: fn.use-var(radius, default);
  }
}

.collab-tabs-container {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.collab-tab-pane {
  min-height: 100px;
}

.activity-timeline {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 6px 0;
}

.activity-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.activity-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: fn.use-var(color, primary);
  margin-top: 6px;
  flex-shrink: 0;
}

.activity-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.activity-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.activity-user {
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.activity-action {
  color: fn.use-var(text-color, default);
}

.activity-time {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.activity-comment {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
  padding: 4px 8px;
  background: fn.use-var(bg-color, muted);
  border-radius: fn.use-var(radius, small);
  margin-top: 2px;
}

.comments-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 12px;
}

.comment-card {
  padding: 10px 12px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.comment-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.comment-author {
  font-weight: 600;
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.comment-time {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
  margin-left: 8px;
  margin-right: auto;
}

.comment-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.comment-body {
  font-size: 13px;
  color: fn.use-var(text-color, default);
  white-space: pre-wrap;
  line-height: 1.4;
}

.comment-edit-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.comment-edit-btns {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.comment-input-area {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}

.comment-attachments {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 8px;
}

.comment-submit-row {
  display: flex;
  justify-content: flex-end;
}

.attachments-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.attachments-count {
  font-size: 13px;
  color: fn.use-var(text-color, secondary);
}

.attachments-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.transition-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.transition-tip {
  margin: 0;
  font-size: 13px;
  color: fn.use-var(text-color, default);
}
</style>
