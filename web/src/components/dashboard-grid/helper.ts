import type { InjectionKey } from "vue";

import type { GridStackWidget } from "@/lib/gridstack-vue";
import type {
  AgentRunSummary,
  BuildSummary,
  DashboardCardID,
  DashboardCardLayout,
  MyProject,
  PipelineRunSummary,
  ScriptRunSummary,
  SystemInfo,
  SystemStatus,
  TaskOverview,
} from "@/api/types";

export interface DashboardWidgetHostContext {
  editing: boolean;
  buildSummary: BuildSummary | null;
  agentRunSummary: AgentRunSummary | null;
  scriptRunSummary: ScriptRunSummary | null;
  pipelineRunSummary: PipelineRunSummary | null;
  taskOverview: TaskOverview | null;
  myProjects: MyProject[] | null;
  systemInfo: SystemInfo | null;
  systemStatus: SystemStatus | null;
  openBuildRun: (id: number) => void;
  openAgentRun: (id: number) => void;
  openScriptRun: (id: number) => void;
  openPipelineRun: (id: number) => void;
  openProject: (id: number) => void;
  openBuildJobs: () => void;
  openScriptJobs: () => void;
  openPipelines: () => void;
  showRunning: (kind: "build" | "script" | "pipeline") => void;
}

export const DASHBOARD_WIDGET_CTX: InjectionKey<DashboardWidgetHostContext> =
  Symbol("dashboard-widget-ctx");

export const DASHBOARD_GRID_COLUMNS = 12;
const DASHBOARD_MIN_W = 2;
const DASHBOARD_MIN_H = 2;

export const DEFAULT_CARD_GEOMETRY: Record<
  DashboardCardID,
  Pick<DashboardCardLayout, "x" | "y" | "w" | "h">
> = {
  build_summary: { x: 0, y: 0, w: 6, h: 2 },
  pipeline_run_summary: { x: 6, y: 0, w: 6, h: 2 },
  agent_run_summary: { x: 0, y: 2, w: 6, h: 2 },
  script_run_summary: { x: 6, y: 2, w: 6, h: 2 },
  cicd_task_overview: { x: 0, y: 4, w: 6, h: 2 },
  my_projects: { x: 6, y: 4, w: 6, h: 2 },
  system_info: { x: 0, y: 6, w: 6, h: 3 },
  system_status: { x: 6, y: 6, w: 6, h: 3 },
};

/** Reset all cards to their default compact geometry */
export function resetCardGeometry(cards: DashboardCardLayout[]): DashboardCardLayout[] {
  return cards.map((card, index) => {
    const fallback = DEFAULT_CARD_GEOMETRY[card.id] ?? {
      x: 0,
      y: index * DASHBOARD_MIN_H,
      w: 6,
      h: 2,
    };
    return {
      ...card,
      x: fallback.x,
      y: fallback.y,
      w: fallback.w,
      h: fallback.h,
      order: fallback.y * DASHBOARD_GRID_COLUMNS + fallback.x,
    };
  });
}

/** Fill missing geometry from defaults (legacy layouts / incomplete API payloads). */
export function ensureCardGeometry(cards: DashboardCardLayout[]): DashboardCardLayout[] {
  return cards.map((card, index) => {
    const fallback = DEFAULT_CARD_GEOMETRY[card.id] ?? {
      x: 0,
      y: index * DASHBOARD_MIN_H,
      w: 6,
      h: 2,
    };
    const x = Number.isFinite(card.x) ? card.x : fallback.x;
    const y = Number.isFinite(card.y) ? card.y : fallback.y;
    const w = Number.isFinite(card.w) && card.w >= DASHBOARD_MIN_W ? card.w : fallback.w;
    const h = Number.isFinite(card.h) && card.h >= DASHBOARD_MIN_H ? card.h : fallback.h;
    return {
      ...card,
      x,
      y,
      w: Math.min(w, DASHBOARD_GRID_COLUMNS),
      h,
      order: y * DASHBOARD_GRID_COLUMNS + x,
    };
  });
}

export function toGridWidgets(cards: DashboardCardLayout[]): GridStackWidget[] {
  return ensureCardGeometry(cards).map((card) => ({
    id: card.id,
    x: card.x,
    y: card.y,
    w: card.w,
    h: card.h,
    minW: DASHBOARD_MIN_W,
    minH: DASHBOARD_MIN_H,
    component: card.id,
  }));
}

/** Merge grid geometry into the full layout (hidden cards keep prior x/y/w/h). */
export function geometryFromWidgets(
  widgets: GridStackWidget[],
  previous: DashboardCardLayout[],
): DashboardCardLayout[] {
  const geoById = new Map(widgets.map((widget) => [String(widget.id ?? ""), widget] as const));
  return previous.map((card) => {
    const widget = geoById.get(card.id);
    if (!widget) return card;
    const x = widget.x ?? card.x;
    const y = widget.y ?? card.y;
    const w = widget.w ?? card.w;
    const h = widget.h ?? card.h;
    return {
      ...card,
      x,
      y,
      w,
      h,
      order: y * DASHBOARD_GRID_COLUMNS + x,
    };
  });
}

export function visibleCardsSignature(cards: DashboardCardLayout[]): string {
  return ensureCardGeometry(cards)
    .filter((card) => card.visible)
    .map((card) => `${card.id}:${card.x},${card.y},${card.w},${card.h}`)
    .sort()
    .join("|");
}
