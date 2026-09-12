<script setup lang="ts">
defineOptions({ name: "BugDetailDialog" });

import { computed, onUnmounted, ref, watch } from "vue";
import { message } from "@veltra/desktop";

import { getRun, listAgents } from "@/api/ai";
import {
  aiAnalyzeBug,
  deleteProjectBugAttachment,
  deleteProjectBugComment,
  dispatchAgentForBug,
  downloadProjectBugAttachment,
  getProjectBug,
  listProjectBugActivities,
  listProjectBugAttachments,
  listProjectBugComments,
  transitionProjectBugStatus,
  updateProjectBugComment,
  createProjectBugComment,
  uploadProjectBugAttachment,
} from "@/api/projects";
import type {
  AgentRun,
  BugActivity,
  BugAttachment,
  BugComment,
  BugStatus,
  ProjectBug,
  ProjectRole,
} from "@/api/types";
import MarkdownViewer from "@/components/markdown-viewer";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime, formatDurationMs } from "@/lib/datetime";
import {
  BUG_PRIORITY_TAG,
  BUG_SEVERITY_TAG,
  BUG_STATUS_TAG,
  JOB_STATUS_TAG,
  bugPriorityLabel,
  bugSeverityLabel,
  bugStatusLabel,
  jobStatusLabel,
  tagType,
} from "@/lib/tag";
import { useAuthStore } from "@/stores/auth";
import { useRepositoryStore } from "@/stores/repositories";

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
const agentRun = ref<AgentRun | null>(null);
let pollTimer: ReturnType<typeof setInterval> | null = null;

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
const canExecuteBug = computed(
  () => hasPermission("project_bugs:execute") && canEditProjectContent.value,
);
const canCreateComment = computed(
  () => hasPermission("project_bugs:create") && canEditProjectContent.value,
);

const ALL_STATUSES: BugStatus[] = ["open", "in_progress", "resolved", "closed", "rejected"];
const transitionTargets = computed(() => ALL_STATUSES.filter((s) => s !== bug.value?.status));

// 状态流转弹窗
const transitionDialogOpen = ref(false);
const targetStatus = ref<BugStatus>("open");
const transitionComment = ref("");
const transitioning = ref(false);

// AI 根因分析
const analyzingAI = ref(false);

// Agent 派发排查
const dispatchDialogOpen = ref(false);
const dispatchPrompt = ref("");
const selectedAgentId = ref<number | undefined>(undefined);
const agentOptions = ref<{ label: string; value: number }[]>([]);
const dispatching = ref(false);
const refreshingRun = ref(false);

// 评论
const newCommentText = ref("");
const submittingComment = ref(false);
const editingCommentId = ref<number | undefined>(undefined);
const editingCommentContent = ref("");
const savingComment = ref(false);

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

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

async function loadAgentRun(runId: number) {
  try {
    const run = await getRun(runId);
    agentRun.value = run;
    if (run.status === "queued" || run.status === "running") {
      startPollingRun(runId);
    } else {
      stopPollingRun();
    }
  } catch {
    /* ignore run load error */
  }
}

function startPollingRun(runId: number) {
  stopPollingRun();
  pollTimer = setInterval(async () => {
    if (!open.value || !bug.value) {
      stopPollingRun();
      return;
    }
    try {
      const run = await getRun(runId);
      agentRun.value = run;
      if (run.status !== "queued" && run.status !== "running") {
        stopPollingRun();
        emit("refresh");
      }
    } catch {
      stopPollingRun();
    }
  }, 4000);
}

function stopPollingRun() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
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

    if (bugData.last_agent_run_id) {
      void loadAgentRun(bugData.last_agent_run_id);
    } else {
      agentRun.value = null;
    }
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

async function handleAIAnalyze() {
  if (!bug.value) return;
  const pid = bug.value.project_id;
  const bid = bug.value.id;

  analyzingAI.value = true;
  try {
    const res = await aiAnalyzeBug(pid, bid);
    bug.value.ai_analysis = res.ai_analysis;
    message.success("AI 根因分析完成");
    emit("refresh");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "AI 根因分析失败");
  } finally {
    analyzingAI.value = false;
  }
}

async function openDispatchDialog() {
  dispatchPrompt.value = "";
  selectedAgentId.value = undefined;
  dispatchDialogOpen.value = true;
  try {
    const res = await listAgents({ page: 1, page_size: 100 });
    agentOptions.value = (res.items ?? []).map((a) => ({
      label: a.name,
      value: a.id,
    }));
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载智能体列表失败");
  }
}

async function handleDispatchAgent() {
  if (!bug.value || !selectedAgentId.value) return;
  const pid = bug.value.project_id;
  const bid = bug.value.id;

  dispatching.value = true;
  try {
    const res = await dispatchAgentForBug(pid, bid, {
      agent_id: selectedAgentId.value,
      user_prompt: dispatchPrompt.value.trim() || undefined,
    });
    bug.value.last_agent_run_id = res.agent_run_id;
    message.success("Agent 代码排查任务已派发");
    dispatchDialogOpen.value = false;
    await loadAgentRun(res.agent_run_id);
    emit("refresh");
  } catch (error) {
    message.error(error instanceof Error ? error.message : "派发 Agent 失败");
  } finally {
    dispatching.value = false;
  }
}

async function refreshRunManually() {
  if (!bug.value?.last_agent_run_id) return;
  refreshingRun.value = true;
  try {
    await loadAgentRun(bug.value.last_agent_run_id);
    message.success("运行状态已更新");
  } finally {
    refreshingRun.value = false;
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

async function handleAddComment() {
  if (!bug.value || !newCommentText.value.trim()) return;
  submittingComment.value = true;
  try {
    const newComment = await createProjectBugComment(
      bug.value.project_id,
      bug.value.id,
      newCommentText.value.trim(),
    );
    comments.value.push(newComment);
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

async function handleDeleteAttachment(att: BugAttachment) {
  if (!bug.value) return;
  try {
    await deleteProjectBugAttachment(bug.value.project_id, bug.value.id, att.id);
    attachments.value = attachments.value.filter((item) => item.id !== att.id);
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
    } else {
      stopPollingRun();
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

onUnmounted(() => {
  stopPollingRun();
});
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
          <pre class="bug-description-text">{{ bug.description || "暂无描述" }}</pre>
        </div>
      </div>

      <!-- AI 根因分析卡片 -->
      <div class="bug-card ai-card">
        <div class="card-header">
          <div class="card-title">
            <span class="card-icon">🧠</span>
            <span class="card-title-text">AI 根因分析与修复建议</span>
          </div>
          <u-button
            v-if="canExecuteBug"
            size="small"
            type="primary"
            :loading="analyzingAI"
            @click="handleAIAnalyze"
          >
            {{ bug.ai_analysis ? "重新诊断" : "AI 智能诊断" }}
          </u-button>
        </div>
        <div class="card-body">
          <div v-if="analyzingAI" class="card-loading-tip">
            大模型正在深入分析缺陷调用栈与代码上下文，推导根本原因与修复建议...
          </div>
          <MarkdownViewer
            v-else-if="bug.ai_analysis"
            :content="bug.ai_analysis"
            class="ai-analysis-render"
          />
          <div v-else class="card-empty-tip">
            暂无 AI 诊断记录。点击右上角「AI 智能诊断」，由大模型自动定位根本原因并输出修复建议。
          </div>
        </div>
      </div>

      <!-- Bedrock Agent 代码排查卡片 -->
      <div class="bug-card agent-card">
        <div class="card-header">
          <div class="card-title">
            <span class="card-icon">⚡</span>
            <span class="card-title-text">Bedrock Agent 自动化代码排查</span>
          </div>
          <u-button v-if="canExecuteBug" size="small" type="secondary" @click="openDispatchDialog">
            {{ bug.last_agent_run_id ? "重新派发 Agent" : "派发 Agent 排查" }}
          </u-button>
        </div>
        <div class="card-body">
          <div v-if="bug.last_agent_run_id" class="agent-run-detail">
            <div class="agent-run-header">
              <span class="agent-run-id">关联运行任务 #{{ bug.last_agent_run_id }}</span>
              <u-tag size="small" :type="tagType(agentRun?.status, JOB_STATUS_TAG)">
                {{ jobStatusLabel(agentRun?.status) || agentRun?.status || "加载中" }}
              </u-tag>
              <span v-if="agentRun?.duration_ms" class="agent-run-duration">
                耗时: {{ formatDurationMs(agentRun.duration_ms) }}
              </span>
              <router-link
                :to="`/ai/runs/${bug.last_agent_run_id}`"
                class="agent-run-link"
                target="_blank"
              >
                查看完整执行日志 →
              </router-link>
              <u-button text size="small" :loading="refreshingRun" @click="refreshRunManually">
                刷新
              </u-button>
            </div>
            <div v-if="agentRun?.output_text" class="agent-output-block">
              <div class="agent-block-title">排查结论输出：</div>
              <pre class="agent-output-pre">{{ agentRun.output_text }}</pre>
            </div>
            <div v-else-if="agentRun?.error_message" class="agent-error-block">
              <div class="agent-block-title">执行错误：</div>
              <div class="agent-error-text">{{ agentRun.error_message }}</div>
            </div>
          </div>
          <div v-else class="card-empty-tip">
            尚未派发智能体。可指定关联当前代码仓的 Bedrock Agent 进行代码排查，自动生成修复方案。
          </div>
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
            </div>
          </div>
          <u-empty v-else text="暂无讨论评论，欢迎留下排查进展" />

          <div v-if="canCreateComment" class="comment-input-area">
            <u-textarea
              v-model="newCommentText"
              :rows="3"
              placeholder="发表评论或排查协同进展..."
            />
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
            <div v-for="att in attachments" :key="att.id" class="attachment-row">
              <div class="attachment-details">
                <span class="attachment-title">{{ att.filename }}</span>
                <span class="attachment-meta">
                  {{ formatBytes(att.file_size) }} ·
                  {{ att.creator_name || att.creator_username || "—" }} ·
                  {{ formatDateTime(att.created_at) }}
                </span>
              </div>
              <div class="attachment-actions">
                <u-action @run="handleDownloadAttachment(att)">下载</u-action>
                <u-action
                  v-if="canUpdateBug || canDeleteBug"
                  type="danger"
                  need-confirm
                  @run="handleDeleteAttachment(att)"
                >
                  删除
                </u-action>
              </div>
            </div>
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

    <!-- 派发 Agent 排查弹窗 -->
    <u-dialog
      v-model="dispatchDialogOpen"
      title="派发 Bedrock Agent 自动化排查"
      style="width: 540px"
    >
      <div class="dispatch-dialog-body">
        <div class="dispatch-dialog-field">
          <label class="dispatch-label">选择智能体</label>
          <u-select
            v-model="selectedAgentId"
            :options="agentOptions"
            placeholder="请选择用于排查的智能体"
            filterable
            style="width: 100%"
          />
        </div>
        <div class="dispatch-dialog-field">
          <label class="dispatch-label">排查指令 (可选)</label>
          <u-textarea
            v-model="dispatchPrompt"
            :rows="4"
            placeholder="输入针对此缺陷的补充排查要求，例如重点检查某目录或特定错误现象..."
          />
        </div>
      </div>
      <template #footer="{ close }">
        <u-button @click="close()">取消</u-button>
        <u-button
          type="primary"
          :loading="dispatching"
          :disabled="!selectedAgentId"
          @click="handleDispatchAgent"
        >
          立即派发
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
  max-height: 200px;
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

.bug-card {
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
  overflow: hidden;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: fn.use-var(bg-color, muted);
  border-bottom: fn.use-var(border, muted);
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-icon {
  font-size: 16px;
}

.card-title-text {
  font-size: 14px;
  font-weight: 600;
  color: fn.use-var(text-color, title);
}

.card-body {
  padding: 12px 14px;
}

.card-loading-tip,
.card-empty-tip {
  font-size: 13px;
  color: fn.use-var(text-color, secondary);
  line-height: 1.5;
}

.ai-analysis-render {
  max-height: 320px;
  overflow-y: auto;
}

.agent-run-header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 8px;
}

.agent-run-id {
  font-weight: 600;
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.agent-run-duration {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.agent-run-link {
  font-size: 12px;
  color: fn.use-var(color, primary);
  text-decoration: none;

  &:hover {
    text-decoration: underline;
  }
}

.agent-output-block,
.agent-error-block {
  margin-top: 8px;
  padding: 8px 10px;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, muted);
}

.agent-block-title {
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 4px;
  color: fn.use-var(text-color, secondary);
}

.agent-output-pre {
  margin: 0;
  font-size: 12px;
  font-family: monospace;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 160px;
  overflow-y: auto;
}

.agent-error-text {
  font-size: 12px;
  color: fn.use-var(color, danger);
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

.attachment-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.attachment-details {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.attachment-title {
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.attachment-meta {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.attachment-actions {
  display: flex;
  align-items: center;
  gap: 12px;
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

.dispatch-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dispatch-dialog-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.dispatch-label {
  font-size: 13px;
  color: fn.use-var(text-color, title);
}
</style>
