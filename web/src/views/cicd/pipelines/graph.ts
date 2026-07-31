import type { Edge, Node } from "@vue-flow/core";
import type { InjectionKey, Ref } from "vue";

import type { PipelineEdgeCondition, PipelineNodeEnvVar } from "@/api/types";

/** 节点副标题解析：key 为 `${nodeType}:${targetId}`，值为任务/智能体名 */
export const PIPELINE_TARGET_NAMES: InjectionKey<Ref<Record<string, string>>> =
  Symbol("pipeline-target-names");

export type PipelineNodeType = "start" | "end" | "buildJob" | "scriptJob" | "agent";

export interface PipelineNodeData {
  label?: string;
  build_job_id?: number;
  script_job_id?: number;
  agent_id?: number;
  env_vars?: PipelineNodeEnvVar[];
  /** 运行时状态（仅运行详情注入，不落库） */
  status?: string;
  [key: string]: unknown;
}

export const NODE_TYPE_LABEL: Record<string, string> = {
  start: "开始",
  end: "结束",
  buildJob: "构建",
  scriptJob: "脚本",
  agent: "智能体",
};

export function edgeCondition(edge: Edge): PipelineEdgeCondition {
  const c = (edge.data as { condition?: PipelineEdgeCondition } | undefined)?.condition;
  return c === "on_failure" || c === "always" ? c : "on_success";
}

/** 按边条件应用颜色/虚线/标签（on_success 不显示标签） */
export function applyEdgeVisual(edge: Edge): Edge {
  const condition = edgeCondition(edge);
  const visual: Pick<Edge, "style" | "label" | "labelStyle" | "labelBgStyle"> =
    condition === "on_failure"
      ? {
          style: { stroke: "var(--u-color-danger, #ef4444)", strokeDasharray: "6 4" },
          label: "失败",
          labelStyle: { fill: "var(--u-color-danger, #ef4444)", fontSize: 11 },
          labelBgStyle: { fill: "var(--u-bg-color, #fff)" },
        }
      : condition === "always"
        ? {
            style: { stroke: "var(--u-text-color-secondary, #999)" },
            label: "总是",
            labelStyle: { fill: "var(--u-text-color-secondary, #999)", fontSize: 11 },
            labelBgStyle: { fill: "var(--u-bg-color, #fff)" },
          }
        : {
            style: { stroke: "var(--u-color-success, #22c55e)" },
            label: undefined,
            labelStyle: undefined,
            labelBgStyle: undefined,
          };
  return { ...edge, data: { condition }, ...visual };
}

/** 解析 graph_json；旧图 node.type 缺省按 buildJob 处理 */
export function parseGraphJson(raw: string): { nodes: Node[]; edges: Edge[] } {
  try {
    const g = JSON.parse(raw || '{"nodes":[],"edges":[]}') as {
      nodes?: Node[];
      edges?: Edge[];
    };
    const nodes = (g.nodes ?? []).map((n) => ({
      ...n,
      type: n.type || "buildJob",
      data: n.data ?? {},
    }));
    const edges = (g.edges ?? []).map(applyEdgeVisual);
    return { nodes, edges };
  } catch {
    return { nodes: [], edges: [] };
  }
}

/** 序列化为 graph_json v2：nodes 只留 {id,type,position,data}；edges 只留 {id,source,target,data?} */
export function serializeGraph(nodes: Node[], edges: Edge[]): string {
  return JSON.stringify({
    nodes: nodes.map((n) => {
      const data: PipelineNodeData = { ...(n.data as PipelineNodeData) };
      delete data.status;
      return {
        id: n.id,
        type: n.type || "buildJob",
        position: { x: Math.round(n.position.x), y: Math.round(n.position.y) },
        data,
      };
    }),
    edges: edges.map((e) => {
      const condition = edgeCondition(e);
      const base = { id: e.id, source: e.source, target: e.target };
      return condition === "on_success" ? base : { ...base, data: { condition } };
    }),
  });
}

export function createGraphNode(type: PipelineNodeType, position: { x: number; y: number }): Node {
  const data: PipelineNodeData = { label: NODE_TYPE_LABEL[type] };
  if (type === "buildJob") data.build_job_id = 0;
  if (type === "scriptJob") data.script_job_id = 0;
  if (type === "agent") data.agent_id = 0;
  return { id: `n-${type}-${Date.now()}`, type, position, data };
}

/** 空图种子化：start（左）+ end（右） */
export function seedGraph(): { nodes: Node[]; edges: Edge[] } {
  return {
    nodes: [
      { id: "start", type: "start", position: { x: 0, y: 140 }, data: { label: "开始" } },
      { id: "end", type: "end", position: { x: 480, y: 140 }, data: { label: "结束" } },
    ],
    edges: [],
  };
}
