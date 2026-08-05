import { date } from "@cat-kit/core";

const DATETIME_FORMAT = "yyyy-MM-dd HH:mm:ss";

/** Format a timestamp for table/list display; empty/invalid → "". */
export function formatDateTime(value: string | number | Date | null | undefined): string {
  if (value == null || value === "") return "";
  const d = date(value);
  if (Number.isNaN(d.timestamp)) return "";
  return d.format(DATETIME_FORMAT);
}

/** Format elapsed milliseconds for table/list display; missing/non-positive → "". */
export function formatDurationMs(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return "";
  const totalSec = Math.floor(ms / 1000);
  if (totalSec < 1) return `${ms}ms`;
  const hours = Math.floor(totalSec / 3600);
  const minutes = Math.floor((totalSec % 3600) / 60);
  const seconds = totalSec % 60;
  if (hours > 0) return `${hours}h ${minutes}m ${seconds}s`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}

/** Format duration between two timestamps; missing/invalid → "". */
export function formatDurationBetween(
  start: string | number | Date | null | undefined,
  end: string | number | Date | null | undefined,
): string {
  if (start == null || start === "" || end == null || end === "") return "";
  const startMs = date(start).timestamp;
  const endMs = date(end).timestamp;
  if (Number.isNaN(startMs) || Number.isNaN(endMs)) return "";
  return formatDurationMs(endMs - startMs);
}
