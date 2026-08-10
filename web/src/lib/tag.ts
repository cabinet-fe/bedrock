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

export const JOB_STATUS_LABEL: Record<string, string> = {
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

export const TRIGGER_TYPE_LABEL: Record<string, string> = {
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

export const BUILD_STAGE_LABEL: Record<string, string> = {
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

export const BUILD_DISTRIBUTION_LABEL: Record<string, string> = {
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
