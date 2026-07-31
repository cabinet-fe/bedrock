<script setup lang="ts">
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import {
  VueFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeTypesObject,
} from "@vue-flow/core";
import { markRaw } from "vue";

import type { BuildJob } from "@/api/types";

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
  addJob: [job: BuildJob, position: { x: number; y: number }];
}>();

const nodeTypes: NodeTypesObject = {
  buildJob: markRaw(PipelineNode),
};

function onConnect(connection: Connection) {
  if (props.readonly) return;
  if (!connection.source || !connection.target) return;
  edges.value = [
    ...edges.value,
    {
      id: `e-${connection.source}-${connection.target}-${Date.now()}`,
      source: connection.source,
      target: connection.target,
    },
  ];
}

function onDrop(e: DragEvent) {
  if (props.readonly) return;
  const jobId = e.dataTransfer?.getData("application/bedrock-build-job");
  if (!jobId) return;
  const bounds = (e.currentTarget as HTMLElement).getBoundingClientRect();
  emit("addJob", { id: Number(jobId) } as BuildJob, {
    x: e.clientX - bounds.left - 70,
    y: e.clientY - bounds.top - 20,
  });
}

function onDragOver(e: DragEvent) {
  if (props.readonly) return;
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
}
</script>

<template>
  <div class="pipeline-canvas" @drop="onDrop" @dragover="onDragOver">
    <VueFlow
      v-model:nodes="nodes"
      v-model:edges="edges"
      :node-types="nodeTypes"
      :nodes-draggable="!readonly"
      :nodes-connectable="!readonly"
      :edges-updatable="!readonly"
      fit-view-on-init
      @connect="onConnect"
    >
      <Background />
      <Controls />
    </VueFlow>
  </div>
</template>

<style scoped lang="scss">
.pipeline-canvas {
  flex: 1;
  min-height: 420px;
  height: 100%;
}
</style>
