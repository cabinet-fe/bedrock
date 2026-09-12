import type { ColorType } from "@veltra/utils";

/** 省略 type 时为默认灰底标签 */
export type TagType = ColorType | undefined;

export function tagType(value: string | undefined | null, map: Record<string, TagType>): TagType {
  if (!value) return undefined;
  return map[value];
}

/** 构建 / Agent / 安装任务等异步状态 */
export const JOB_STATUS_TAG: Record<string, TagType> = {
  queued: "info",
  pending: "info",
  running: "primary",
  success: "success",
  failed: "danger",
  cancelled: "warning",
  interrupted: "warning",
};

const JOB_STATUS_LABEL: Record<string, string> = {
  queued: "排队",
  pending: "等待",
  running: "运行中",
  success: "成功",
  failed: "失败",
  cancelled: "已取消",
  interrupted: "中断",
};

export function jobStatusLabel(value: string | undefined | null): string {
  if (!value) return "";
  return JOB_STATUS_LABEL[value] ?? value;
}

export const JOB_STATUS_OPTIONS = Object.entries(JOB_STATUS_LABEL).map(([value, label]) => ({
  value,
  label,
}));

export const TRIGGER_TYPE_TAG: Record<string, TagType> = {
  manual: undefined,
  api: "info",
  webhook: "info",
  cron: "primary",
  pipeline: "primary",
  build_event: "warning",
  docs_generate: "info",
};

const TRIGGER_TYPE_LABEL: Record<string, string> = {
  manual: "手动",
  api: "API",
  webhook: "Webhook",
  cron: "定时",
  pipeline: "流水线",
  build_event: "构建事件",
  docs_generate: "文档生成",
};

export function triggerTypeLabel(value: string | undefined | null): string {
  if (!value) return "";
  return TRIGGER_TYPE_LABEL[value] ?? value;
}

/** 构建流水线阶段 */
export const BUILD_STAGE_TAG: Record<string, TagType> = {
  pending: undefined,
  cloning: "primary",
  building: "primary",
  archiving: "primary",
  distributing: "warning",
  idle: "success",
};

const BUILD_STAGE_LABEL: Record<string, string> = {
  pending: "等待",
  cloning: "拉取代码",
  building: "构建",
  archiving: "打包",
  distributing: "分发",
  idle: "空闲",
};

export function buildStageLabel(value: string | undefined | null): string {
  if (!value) return "";
  return BUILD_STAGE_LABEL[value] ?? value;
}

/** 构建分发汇总 */
export const BUILD_DISTRIBUTION_TAG: Record<string, TagType> = {
  none: undefined,
  running: "primary",
  all_success: "success",
  partial: "warning",
  all_failed: "danger",
  cancelled: "warning",
};

const BUILD_DISTRIBUTION_LABEL: Record<string, string> = {
  none: "无",
  running: "分发中",
  all_success: "全部成功",
  partial: "部分成功",
  all_failed: "全部失败",
  cancelled: "已取消",
};

export function buildDistributionLabel(value: string | undefined | null): string {
  if (!value) return "";
  return BUILD_DISTRIBUTION_LABEL[value] ?? value;
}

export function splitCommaTags(raw?: string | null): string[] {
  if (!raw) return [];
  return raw
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * 代码仓库、构建任务等类型标签（对应数据字典 repo_type）
 * 前端采用 danger，后端采用 info，其它都是 primary
 */
export const REPO_TYPE_TAG: Record<string, TagType> = {
  frontend: "danger",
  前端: "danger",
  backend: "info",
  后端: "info",
};

export function repoTagType(tag: string | undefined | null): TagType {
  const trimmed = tag?.trim();
  if (!trimmed) return undefined;
  const lower = trimmed.toLowerCase();
  return REPO_TYPE_TAG[lower] ?? REPO_TYPE_TAG[trimmed] ?? "primary";
}

/** 缺陷状态标签与文案 */
export const BUG_STATUS_TAG: Record<string, TagType> = {
  open: "primary",
  in_progress: "warning",
  resolved: "success",
  closed: undefined,
  rejected: "danger",
};

const BUG_STATUS_LABEL: Record<string, string> = {
  open: "待处理",
  in_progress: "处理中",
  resolved: "已解决",
  closed: "已关闭",
  rejected: "已拒绝",
};

export function bugStatusLabel(value: string | undefined | null): string {
  if (!value) return "";
  return BUG_STATUS_LABEL[value] ?? value;
}

export const BUG_STATUS_OPTIONS = Object.entries(BUG_STATUS_LABEL).map(([value, label]) => ({
  value,
  label,
}));

/** 缺陷严重程度标签与文案 */
export const BUG_SEVERITY_TAG: Record<string, TagType> = {
  low: undefined,
  normal: "info",
  high: "warning",
  critical: "danger",
};

const BUG_SEVERITY_LABEL: Record<string, string> = {
  low: "轻微",
  normal: "一般",
  high: "严重",
  critical: "致命",
};

export function bugSeverityLabel(value: string | undefined | null): string {
  if (!value) return "";
  return BUG_SEVERITY_LABEL[value] ?? value;
}

export const BUG_SEVERITY_OPTIONS = Object.entries(BUG_SEVERITY_LABEL).map(([value, label]) => ({
  value,
  label,
}));

/** 缺陷优先级标签与文案 */
export const BUG_PRIORITY_TAG: Record<string, TagType> = {
  low: undefined,
  normal: "info",
  high: "warning",
  urgent: "danger",
};

const BUG_PRIORITY_LABEL: Record<string, string> = {
  low: "低",
  normal: "普通",
  high: "高",
  urgent: "紧急",
};

export function bugPriorityLabel(value: string | undefined | null): string {
  if (!value) return "";
  return BUG_PRIORITY_LABEL[value] ?? value;
}

export const BUG_PRIORITY_OPTIONS = Object.entries(BUG_PRIORITY_LABEL).map(([value, label]) => ({
  value,
  label,
}));
