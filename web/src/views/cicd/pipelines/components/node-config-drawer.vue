<script setup lang="ts">
import type { Node } from "@vue-flow/core";
import { message } from "@veltra/desktop";
import { computed, reactive, ref, watch } from "vue";

import { getBuildJob, getScriptJob, listBuildJobs, listScriptJobs } from "@/api/cicd";
import { listAgents } from "@/api/ai";
import type { PipelineNodeEnvVar } from "@/api/types";

import { NODE_TYPE_LABEL, type PipelineNodeData } from "../graph";

interface EnvRow {
  key: string;
  value: string;
  /** 节点已存密值（图上 env_vars 回显） */
  has_value: boolean;
  /** 自定义 key 行（非任务定义） */
  custom: boolean;
}

const props = defineProps<{
  modelValue: boolean;
  node: Node | null;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  save: [data: PipelineNodeData];
}>();

const nodeType = computed(() => props.node?.type || "buildJob");
const isJobType = computed(() => nodeType.value === "buildJob" || nodeType.value === "scriptJob");
const drawerTitle = computed(() => `配置节点 · ${NODE_TYPE_LABEL[nodeType.value] ?? ""}`);

const form = reactive({ label: "", target_id: undefined as number | undefined });
const envRows = ref<EnvRow[]>([]);
const targetOptions = ref<{ label: string; value: number }[]>([]);
/** 智能体列表加载失败（如无 ai_agents:view 权限）时降级为 ID 输入 */
const agentListFailed = ref(false);

let savedEnvVars: PipelineNodeEnvVar[] = [];
/** 已完成初始化的目标 id；与 form.target_id 不一致时才视为用户切换目标 */
let loadedTargetId: number | undefined;

function targetIdOf(data: PipelineNodeData): number | undefined {
  if (nodeType.value === "buildJob") return data.build_job_id || undefined;
  if (nodeType.value === "scriptJob") return data.script_job_id || undefined;
  if (nodeType.value === "agent") return data.agent_id || undefined;
  return undefined;
}

async function loadOptions() {
  agentListFailed.value = false;
  try {
    if (nodeType.value === "buildJob") {
      const page = await listBuildJobs({ page: 1, page_size: 200 });
      targetOptions.value = (page.items ?? []).map((j) => ({ label: j.name, value: j.id }));
    } else if (nodeType.value === "scriptJob") {
      const page = await listScriptJobs({ page: 1, page_size: 200 });
      targetOptions.value = (page.items ?? []).map((j) => ({ label: j.name, value: j.id }));
    } else if (nodeType.value === "agent") {
      const page = await listAgents({ page: 1, page_size: 200 });
      targetOptions.value = (page.items ?? []).map((a) => ({ label: a.name, value: a.id }));
    }
  } catch {
    if (nodeType.value === "agent") agentListFailed.value = true;
    else message.error("加载列表失败");
  }
}

/** 拉取任务定义的变量 key 列表，重建变量行；initial 时合并节点已存 env_vars */
async function loadTargetDetail(id: number | undefined, initial: boolean) {
  loadedTargetId = id;
  envRows.value = [];
  if (!id || !isJobType.value) return;
  try {
    const detail = nodeType.value === "buildJob" ? await getBuildJob(id) : await getScriptJob(id);
    const taskKeys = (detail.env_vars ?? []).map((e) => e.key);
    const saved = new Map(savedEnvVars.map((e) => [e.key, e]));
    const rows: EnvRow[] = taskKeys.map((key) => ({
      key,
      value: "",
      has_value: initial ? (saved.get(key)?.has_value ?? false) : false,
      custom: false,
    }));
    if (initial) {
      for (const e of savedEnvVars) {
        if (!taskKeys.includes(e.key)) {
          rows.push({ key: e.key, value: "", has_value: e.has_value ?? false, custom: true });
        }
      }
    }
    envRows.value = rows;
  } catch {
    message.error("加载任务详情失败");
  }
}

watch(
  () => props.modelValue,
  async (open) => {
    if (!open || !props.node) return;
    const data = (props.node.data ?? {}) as PipelineNodeData;
    savedEnvVars = data.env_vars ?? [];
    form.label = data.label ?? "";
    loadedTargetId = targetIdOf(data);
    form.target_id = loadedTargetId;
    await loadOptions();
    await loadTargetDetail(form.target_id, true);
  },
);

// 切换目标时清空该节点已填的变量行，按新任务重建
watch(
  () => form.target_id,
  (id) => {
    if (id === loadedTargetId) return;
    void loadTargetDetail(id, false);
  },
);

function addCustomRow() {
  envRows.value = [...envRows.value, { key: "", value: "", has_value: false, custom: true }];
}

function buildEnvVars(): PipelineNodeEnvVar[] | undefined {
  const out: PipelineNodeEnvVar[] = [];
  const seen = new Set<string>();
  for (const row of envRows.value) {
    const key = row.key.trim();
    if (!key) continue;
    if (seen.has(key)) {
      message.error(`环境变量 key 重复: ${key}`);
      return undefined;
    }
    seen.add(key);
    if (row.value !== "") out.push({ key, value: row.value });
    else if (row.has_value) out.push({ key });
  }
  return out;
}

function save() {
  const type = nodeType.value;
  const data: PipelineNodeData = { label: form.label.trim() || NODE_TYPE_LABEL[type] };
  if (isJobType.value) {
    if (!form.target_id) {
      message.warning(type === "buildJob" ? "请选择构建任务" : "请选择脚本任务");
      return;
    }
    const envVars = buildEnvVars();
    if (!envVars) return;
    if (type === "buildJob") data.build_job_id = form.target_id;
    else data.script_job_id = form.target_id;
    data.env_vars = envVars;
  } else if (type === "agent") {
    if (!form.target_id) {
      message.warning("请选择智能体");
      return;
    }
    data.agent_id = form.target_id;
  }
  emit("save", data);
  emit("update:modelValue", false);
}
</script>

<template>
  <u-drawer
    :model-value="modelValue"
    :title="drawerTitle"
    show-close
    style="width: 440px"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="node-config">
      <div class="node-config__body">
        <u-form :model="form" label-position="top">
          <u-input label="名称" field="label" :placeholder="NODE_TYPE_LABEL[nodeType]" />

          <template v-if="isJobType">
            <u-select
              :label="nodeType === 'buildJob' ? '构建任务' : '脚本任务'"
              field="target_id"
              :options="targetOptions"
              filterable
              placeholder="搜索并选择任务"
            />
          </template>

          <template v-else-if="nodeType === 'agent'">
            <u-number-input
              v-if="agentListFailed"
              label="智能体 ID"
              field="target_id"
              tips="无法加载智能体列表（可能无查看权限），请直接输入 ID"
            />
            <u-select
              v-else
              label="智能体"
              field="target_id"
              :options="targetOptions"
              filterable
              placeholder="搜索并选择智能体"
            />
          </template>
        </u-form>

        <p v-if="nodeType === 'start'" class="node-config__tip">
          开始节点：流水线的唯一入口，只出不进。
        </p>
        <p v-else-if="nodeType === 'end'" class="node-config__tip">
          结束节点：流水线分支的收尾，只进不出，可添加多个。
        </p>

        <template v-if="isJobType && form.target_id">
          <div class="node-config__env-title">变量覆盖</div>
          <div class="env-rows">
            <div v-for="(row, i) in envRows" :key="i" class="env-row">
              <template v-if="!row.custom">
                <code class="env-row__key">{{ row.key }}</code>
                <u-input
                  v-model="row.value"
                  :placeholder="row.has_value ? '已设置，留空保持不变' : '不覆盖，使用任务默认值'"
                />
              </template>
              <template v-else>
                <u-input v-model="row.key" class="env-row__key-input" placeholder="KEY" />
                <u-input
                  v-model="row.value"
                  :placeholder="row.has_value ? '已设置，留空保持不变' : 'value'"
                />
                <u-button text type="danger" size="small" @click="envRows.splice(i, 1)">
                  删除
                </u-button>
              </template>
            </div>
            <p v-if="!envRows.length" class="node-config__tip">
              该任务未定义变量，可添加自定义变量。
            </p>
          </div>
          <u-button size="small" plain @click="addCustomRow">添加自定义变量</u-button>
        </template>
      </div>

      <footer class="node-config__footer">
        <u-button @click="emit('update:modelValue', false)">取消</u-button>
        <u-button type="primary" @click="save">保存</u-button>
      </footer>
    </div>
  </u-drawer>
</template>

<style scoped lang="scss">
.node-config {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 16px;
}

.node-config__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.node-config__footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--u-border-color, #e5e5e5);
}

.node-config__tip {
  margin: 8px 0;
  font-size: 12px;
  color: var(--u-text-color-secondary, #888);
}

.node-config__env-title {
  margin: 16px 0 8px;
  font-size: 13px;
  font-weight: 600;
}

.env-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 8px;
}

.env-row {
  display: flex;
  align-items: center;
  gap: 8px;

  .u-input {
    flex: 1;
    min-width: 0;
  }
}

.env-row__key {
  flex-shrink: 0;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
}

.env-row__key-input {
  max-width: 40%;
}
</style>
