<script setup lang="ts">
import { ref, toRaw, useTemplateRef, watch } from "vue";
import type { FormExposed } from "@veltra/desktop";

/** 分组表单配置：传入后默认插槽失效，改用 `group:${key}` 具名插槽 */
export interface FormDialogGroup {
  title: string;
  key: string;
}

const props = withDefaults(
  defineProps<{
    modelValue?: boolean;
    title?: string;
    /**
     * 表单 model。组件挂载时深拷贝为默认值；关闭时自动恢复默认值。
     * 编辑打开前直接写入 model，再打开弹框即可。
     * 分组模式下同一个 model 传入各分组表单。
     */
    model: Record<string, any>;
    /** 分组配置。传入后按分组渲染多个表单，插槽名为 `group:${key}` */
    groups?: FormDialogGroup[];
    labelWidth?: string | number;
    confirmText?: string;
    /**
     * 校验通过后调用。父组件写 `@submit="save"` 即可；
     * 若返回 Promise，确认按钮自动进入 loading 并防连点。
     */
    onSubmit?: () => void | Promise<void>;
  }>(),
  {
    modelValue: false,

    labelWidth: "88px",
    confirmText: "保存",
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  closed: [];
}>();

const formRef = useTemplateRef("form");
/** 分组模式下的表单实例（v-for 自动收集为数组） */
const groupFormRefs = useTemplateRef<FormExposed[]>("group-forms");
/** 每次打开递增，让表单按当前 model 重新快照（供会话内 u-form.reset） */
const sessionKey = ref(0);
/** 提交中：确认按钮 loading，取消按钮 disabled，并防连点 */
const busy = ref(false);

/** 挂载时的默认值快照（保留 undefined） */
const defaults = plainClone(props.model);

function plainClone(value: unknown): unknown {
  if (value === null || typeof value !== "object") return value;
  const raw = toRaw(value as object);
  if (Array.isArray(raw)) return raw.map(plainClone);
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(raw as Record<string, unknown>)) {
    out[key] = plainClone((raw as Record<string, unknown>)[key]);
  }
  return out;
}

/** 当前生效的表单实例：分组模式多个，默认模式单个 */
function forms(): FormExposed[] {
  return groupFormRefs.value ?? (formRef.value ? [formRef.value] : []);
}

/** 将 model 恢复为挂载时默认值，并清除校验 */
function reset() {
  const next = plainClone(defaults) as Record<string, unknown>;
  for (const key of Object.keys(props.model)) {
    props.model[key] = next[key];
  }
  forms().forEach((form) => form.clearValidate());
}

watch(
  () => props.modelValue,
  (open, wasOpen) => {
    if (open) {
      sessionKey.value += 1;
      return;
    }
    // 同步恢复默认值，避免关闭动画结束前再次打开编辑时脏数据残留
    if (wasOpen) {
      busy.value = false;
      reset();
    }
  },
);

async function onConfirm() {
  if (busy.value) return;
  const results = await Promise.all(forms().map((form) => form.validate()));
  if (!results.every(Boolean)) return;
  busy.value = true;
  try {
    await props.onSubmit?.();
  } finally {
    busy.value = false;
  }
}

function onClosed() {
  emit("closed");
}
</script>

<template>
  <u-dialog
    :model-value="modelValue"
    :title="title"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="onClosed"
  >
    <!-- 表单前置内容（提示、说明等） -->
    <slot name="prepend" />

    <template v-if="groups?.length">
      <section v-for="group in groups" :key="group.key" class="form-dialog__group">
        <span class="form-dialog__group-title">{{ group.title }}</span>
        <u-form
          :key="sessionKey + group.key"
          ref="group-forms"
          :model="model"
          :label-width="labelWidth"
        >
          <slot :name="`group:${group.key}`" />
        </u-form>
      </section>
    </template>

    <u-form
      v-else-if="$slots.default"
      :key="sessionKey"
      ref="form"
      :model="model"
      :label-width="labelWidth"
    >
      <slot />
    </u-form>

    <!-- 表单后置内容 -->
    <slot name="append" />

    <template #footer="{ close }">
      <u-button text :disabled="busy" @click="close()">取消</u-button>
      <u-button type="primary" :loading="busy" @click="onConfirm">
        {{ confirmText }}
      </u-button>
    </template>
  </u-dialog>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.form-dialog__group {
  position: relative;
  border: fn.use-var(border);
  border-radius: fn.use-var(radius, default);
  padding: fn.use-var(gap, default);

  // 给压在边框上的标题留出空间，并与上一组拉开间距
  & + & {
    margin-top: fn.use-var(gap, default);
  }

  &:first-of-type {
    margin-top: fn.use-var(gap, small);
  }
}

// 标题压在分组框左上角边框上，背景与 dialog body 一致以"切开"边框
.form-dialog__group-title {
  position: absolute;
  top: 0;
  left: fn.use-var(gap, default);
  transform: translateY(-50%);
  padding: 0 fn.use-var(gap, small);
  background-color: fn.use-var(bg-color, top);
  color: fn.use-var(text-color, title);
  font-size: fn.use-var(font-size-main, default);
}
</style>
