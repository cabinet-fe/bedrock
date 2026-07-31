<script setup lang="ts">
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import {
  useVueFlow,
  VueFlow,
  type Connection,
  type Edge,
  type EdgeMouseEvent,
  type Node,
  type NodeMouseEvent,
  type NodeTypesObject,
} from "@vue-flow/core";
import { message, type ContextMenuItem } from "@veltra/desktop";
import { Agent, Build, Delete, Edit, Flag, Terminal } from "@veltra/icons/normal";
import { markRaw, reactive, ref, shallowRef, useId, useTemplateRef } from "vue";

import type { PipelineEdgeCondition } from "@/api/types";

import { applyEdgeVisual, createGraphNode, edgeCondition, type PipelineNodeType } from "../graph";

import PipelineNode from "./pipeline-node.vue";

import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import "@vue-flow/controls/dist/style.css";

const nodes = defineModel<Node[]>("nodes", { required: true });
const edges = defineModel<Edge[]>("edges", { required: true });

const props = defineProps<{
  readonly?: boolean;
}>();

const emit = defineEmits<{
  configureNode: [node: Node];
  nodeAdded: [node: Node];
}>();

const nodeTypes: NodeTypesObject = {
  start: markRaw(PipelineNode),
  end: markRaw(PipelineNode),
  buildJob: markRaw(PipelineNode),
  scriptJob: markRaw(PipelineNode),
  agent: markRaw(PipelineNode),
};

const flowId = `pipeline-canvas-${useId()}`;
const vf = useVueFlow(flowId);
const wrapperRef = useTemplateRef("wrapper");

// —— 右键菜单 ——
const menuOpen = ref(false);
const menuPos = ref({ x: 0, y: 0 });
const menuItems = shallowRef<ContextMenuItem[]>([]);
let paneEvent: MouseEvent | null = null;

function openMenu(e: MouseEvent, items: ContextMenuItem[]) {
  menuPos.value = { x: e.clientX, y: e.clientY };
  menuItems.value = items;
  menuOpen.value = true;
}

function onPaneContextMenu(e: MouseEvent) {
  if (props.readonly) return;
  e.preventDefault();
  paneEvent = e;
  openMenu(e, [
    { label: "添加构建任务", icon: Build, callback: () => addNode("buildJob") },
    { label: "添加脚本任务", icon: Terminal, callback: () => addNode("scriptJob") },
    { label: "添加智能体", icon: Agent, callback: () => addNode("agent") },
    { label: "添加结束节点", icon: Flag, callback: () => addNode("end") },
  ]);
}

function addNode(type: PipelineNodeType) {
  const rect = wrapperRef.value?.getBoundingClientRect();
  const position =
    paneEvent && rect
      ? vf.project({ x: paneEvent.clientX - rect.left, y: paneEvent.clientY - rect.top })
      : { x: 120, y: 120 };
  const node = createGraphNode(type, position);
  nodes.value = [...nodes.value, node];
  emit("nodeAdded", node);
}

function onNodeContextMenu({ event, node }: NodeMouseEvent) {
  if (props.readonly) return;
  event.preventDefault();
  const items: ContextMenuItem[] = [
    { label: "配置", icon: Edit, callback: () => emit("configureNode", node) },
  ];
  if (node.type !== "start") {
    items.push({ label: "删除", icon: Delete, callback: () => removeNode(node) });
  }
  openMenu(event as MouseEvent, items);
}

function removeNode(node: Node) {
  if (node.type === "end" && nodes.value.filter((n) => n.type === "end").length <= 1) {
    message.warning("至少保留一个结束节点");
    return;
  }
  vf.removeNodes([node.id], true);
}

function onNodeClick({ node }: NodeMouseEvent) {
  if (props.readonly) return;
  emit("configureNode", node);
}

// —— 连线 ——
function onConnect(connection: Connection) {
  if (props.readonly) return;
  const { source, target } = connection;
  if (!source || !target) return;
  if (source === target) {
    message.warning("不能连接到自身");
    return;
  }
  const sourceNode = nodes.value.find((n) => n.id === source);
  const targetNode = nodes.value.find((n) => n.id === target);
  if (targetNode?.type === "start") {
    message.warning("开始节点不能被连接");
    return;
  }
  if (sourceNode?.type === "end") {
    message.warning("结束节点不能再连线");
    return;
  }
  edges.value = [
    ...edges.value,
    applyEdgeVisual({
      id: `e-${source}-${target}-${Date.now()}`,
      source,
      target,
      data: { condition: "on_success" satisfies PipelineEdgeCondition },
    }),
  ];
}

// —— 边条件弹窗 ——
const CONDITION_OPTIONS = [
  { value: "on_success", label: "成功时继续" },
  { value: "on_failure", label: "失败时继续" },
  { value: "always", label: "总是继续" },
];

const edgeDialogOpen = ref(false);
const editingEdgeId = ref("");
const edgeForm = reactive({ condition: "on_success" as PipelineEdgeCondition });

function onEdgeClick({ edge }: EdgeMouseEvent) {
  if (props.readonly) return;
  editingEdgeId.value = edge.id;
  edgeForm.condition = edgeCondition(edge);
  edgeDialogOpen.value = true;
}

function saveEdgeCondition() {
  edges.value = edges.value.map((e) =>
    e.id === editingEdgeId.value
      ? applyEdgeVisual({ ...e, data: { condition: edgeForm.condition } })
      : e,
  );
  edgeDialogOpen.value = false;
}

function removeEditingEdge() {
  edges.value = edges.value.filter((e) => e.id !== editingEdgeId.value);
  edgeDialogOpen.value = false;
}
</script>

<template>
  <div ref="wrapper" class="pipeline-canvas">
    <VueFlow
      :id="flowId"
      v-model:nodes="nodes"
      v-model:edges="edges"
      :node-types="nodeTypes"
      :nodes-draggable="!readonly"
      :nodes-connectable="!readonly"
      :edges-updatable="!readonly"
      fit-view-on-init
      @connect="onConnect"
      @pane-context-menu="onPaneContextMenu"
      @node-context-menu="onNodeContextMenu"
      @node-click="onNodeClick"
      @edge-click="onEdgeClick"
    >
      <Background />
      <Controls />
    </VueFlow>

    <u-contextmenu
      v-if="menuOpen"
      :mouse-position="menuPos"
      :menus="menuItems"
      @destroy="menuOpen = false"
    />

    <u-dialog v-model="edgeDialogOpen" title="边条件" style="width: 360px">
      <u-radio-group v-model="edgeForm.condition" :items="CONDITION_OPTIONS" block />
      <template #footer>
        <u-button plain type="danger" @click="removeEditingEdge">删除边</u-button>
        <u-button type="primary" @click="saveEdgeCondition">保存</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
.pipeline-canvas {
  flex: 1;
  min-height: 420px;
  height: 100%;
}
</style>
