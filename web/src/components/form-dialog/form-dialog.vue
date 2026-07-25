<script setup lang="ts">
import { ref, toRaw, useTemplateRef, watch } from "vue";

const props = withDefaults(
  defineProps<{
    modelValue?: boolean;
    title?: string;
    /**
     * 表单 model。组件挂载时深拷贝为默认值；关闭时自动恢复默认值。
     * 编辑打开前直接写入 model，再打开弹框即可。
     */
    model: Record<string, any>;
    labelWidth?: string | number;
    confirmText?: string;
    confirmLoading?: boolean;
  }>(),
  {
    modelValue: false,

    labelWidth: "88px",
    confirmText: "保存",
    confirmLoading: false,
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  /** 校验通过后触发 */
  submit: [];
  closed: [];
}>();

const formRef = useTemplateRef("form");
/** 每次打开递增，让表单按当前 model 重新快照（供会话内 u-form.reset） */
const sessionKey = ref(0);

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

/** 将 model 恢复为挂载时默认值，并清除校验 */
function reset() {
  const next = plainClone(defaults) as Record<string, unknown>;
  for (const key of Object.keys(props.model)) {
    props.model[key] = next[key];
  }
  formRef.value?.clearValidate();
}

watch(
  () => props.modelValue,
  (open, wasOpen) => {
    if (open) {
      sessionKey.value += 1;
      return;
    }
    // 同步恢复默认值，避免关闭动画结束前再次打开编辑时脏数据残留
    if (wasOpen) reset();
  },
);

async function onConfirm() {
  const ok = await formRef.value?.validate();
  if (!ok) return;
  emit("submit");
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
    <u-form
      v-if="$slots.default"
      :key="sessionKey"
      ref="form"
      :model="model"
      :label-width="labelWidth"
    >
      <slot />
    </u-form>

    <template #footer="{ close }">
      <u-button text @click="close()">取消</u-button>
      <u-button type="primary" :loading="confirmLoading" @click="onConfirm">
        {{ confirmText }}
      </u-button>
    </template>
  </u-dialog>
</template>
