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

export const TRIGGER_TYPE_TAG: Record<string, TagType> = {
  manual: undefined,
  api: "info",
  webhook: "info",
  cron: "primary",
  pipeline: "primary",
  build_event: "warning",
  docs_generate: "info",
};

/** 构建流水线阶段 */
export const BUILD_STAGE_TAG: Record<string, TagType> = {
  pending: undefined,
  cloning: "primary",
  building: "primary",
  archiving: "primary",
  distributing: "warning",
  idle: "success",
};

/** 构建分发汇总 */
export const BUILD_DISTRIBUTION_TAG: Record<string, TagType> = {
  none: undefined,
  running: "primary",
  all_success: "success",
  partial: "warning",
  all_failed: "danger",
  cancelled: "warning",
};
