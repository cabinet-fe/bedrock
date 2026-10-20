<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { JOB_STATUS_TAG, jobStatusLabel, tagType } from "@/lib/tag";

export interface RedeployTarget {
  id: number;
  method: string;
  remote_path: string;
  server_id?: number | null;
  last_status?: string;
}

const open = defineModel<boolean>({ required: true });

const { targets, loading = false } = defineProps<{
  targets: RedeployTarget[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  confirm: [targetIds: number[]];
}>();

const selected = ref<number[]>([]);

const allChecked = computed(() => targets.length > 0 && selected.value.length === targets.length);
const indeterminate = computed(
  () => selected.value.length > 0 && selected.value.length < targets.length,
);

watch(open, (isOpen) => {
  if (isOpen) selected.value = targets.map((t) => t.id);
});

function toggleAll(checked: boolean) {
  selected.value = checked ? targets.map((t) => t.id) : [];
}

function isChecked(id: number) {
  return selected.value.includes(id);
}

function toggle(id: number, checked: boolean) {
  selected.value = checked ? [...selected.value, id] : selected.value.filter((x) => x !== id);
}

function onConfirm() {
  if (loading || !selected.value.length) return;
  emit("confirm", selected.value.slice());
}

function targetTitle(target: RedeployTarget) {
  if (target.remote_path) return target.remote_path;
  return `目标 #${target.id}`;
}
</script>

<template>
  <u-dialog v-model="open" title="重新分发" style="width: 520px">
    <p class="hint">勾选本次要分发的目标，未勾选的会跳过。</p>
    <div class="toolbar">
      <u-checkbox
        :model-value="allChecked"
        :indeterminate="indeterminate"
        :disabled="loading || !targets.length"
        @change="toggleAll"
      >
        全选
      </u-checkbox>
      <span class="count">{{ selected.length }}/{{ targets.length }}</span>
    </div>
    <ul class="list">
      <li v-for="(target, idx) in targets" :key="target.id" class="item">
        <span class="index">{{ idx + 1 }}</span>
        <u-checkbox
          class="check"
          :model-value="isChecked(target.id)"
          :disabled="loading"
          @change="(checked: boolean) => toggle(target.id, checked)"
        >
          <span class="label">
            <span class="label__main">
              <u-tag v-if="target.method" size="small">{{ target.method }}</u-tag>
              <span class="path" :title="targetTitle(target)">{{ targetTitle(target) }}</span>
            </span>
            <span v-if="target.method !== 'local' && target.server_id" class="meta">
              服务器 #{{ target.server_id }}
            </span>
          </span>
        </u-checkbox>
        <u-tag
          v-if="target.last_status"
          size="small"
          :type="tagType(target.last_status, JOB_STATUS_TAG)"
        >
          {{ jobStatusLabel(target.last_status) }}
        </u-tag>
      </li>
    </ul>
    <template #footer="{ close }">
      <u-button text :disabled="loading" @click="close()">取消</u-button>
      <u-button type="primary" :loading="loading" :disabled="!selected.length" @click="onConfirm">
        开始分发
      </u-button>
    </template>
  </u-dialog>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.hint {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.5;
  color: fn.use-var(text-color, second);
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.count {
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}

.list {
  margin: 12px 0 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
  padding: 10px 12px;
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, middle);
  border: fn.use-var(border, muted);
}

.index {
  flex-shrink: 0;
  width: 22px;
  height: 22px;
  margin-top: 1px;
  border-radius: 50%;
  background: fn.use-var(bg-color, top);
  color: fn.use-var(text-color, assist);
  font-size: 12px;
  line-height: 22px;
  text-align: center;
}

.check {
  flex: 1;
  min-width: 0;
}

.label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.label__main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  font-weight: 500;
  color: fn.use-var(text-color, main);
}

.meta {
  font-size: 12px;
  color: fn.use-var(text-color, assist);
}
</style>
