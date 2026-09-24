<script setup lang="ts">
import { ref, toRaw, useTemplateRef, watch } from "vue";
import type { FormExposed } from "@veltra/desktop";

/** Grouped form config: when set, the default slot is disabled; use the `group:${key}` named slots instead */
export interface FormDialogGroup {
  title: string;
  key: string;
}

const model = defineModel<boolean>({ default: false });

const props = withDefaults(
  defineProps<{
    title?: string;
    /**
     * Form model. Deep-copied as defaults at mount; restored automatically on close.
     * For editing, write into model before opening the dialog.
     * In grouped mode, the same model is passed to every group form.
     */
    model: Record<string, any>;
    /** Group config. When set, renders multiple forms per group, with slot names `group:${key}` */
    groups?: FormDialogGroup[];
    labelWidth?: string | number;
    confirmText?: string;
    /**
     * Called after validation passes. Parents write `@submit="save"`;
     * if it returns a Promise, the confirm button auto-loads and double clicks are guarded.
     */
    onSubmit?: () => void | Promise<void>;
  }>(),
  {
    labelWidth: "88px",
    confirmText: "保存",
  },
);

const emit = defineEmits<{
  closed: [];
}>();

const formRef = useTemplateRef("form");
/** Form instances in grouped mode (v-for collects them into an array) */
const groupFormRefs = useTemplateRef<FormExposed[]>("group-forms");
/** Increments per open so forms re-snapshot the current model (for in-session u-form.reset) */
const sessionKey = ref(0);
/** Submitting: confirm button loading, cancel disabled, double-clicks guarded */
const busy = ref(false);

/** Default-value snapshot at mount (keeps undefined) */
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

/** The active form instance(s): one per group in grouped mode, one in default mode */
function forms(): FormExposed[] {
  return groupFormRefs.value ?? (formRef.value ? [formRef.value] : []);
}

/** Restores model to the mount-time defaults and clears validation */
function reset() {
  const next = plainClone(defaults) as Record<string, unknown>;
  for (const key of Object.keys(props.model)) {
    props.model[key] = next[key];
  }
  forms().forEach((form) => form.clearValidate());
}

watch(model, (open, wasOpen) => {
  if (open) {
    sessionKey.value += 1;
    return;
  }
  // Restore defaults synchronously so reopening edit before the close animation ends never shows stale data
  if (wasOpen) {
    busy.value = false;
    reset();
  }
});

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
  <u-dialog v-model="model" :title="title" @closed="onClosed">
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
  padding: fn.use-var(gap, large);

  // Group spacing must fit the previous group's bottom edge to this group's title (title protrudes by half its height)
  & + & {
    margin-top: calc(fn.use-var(gap, large) * 2);
  }

  // Leave room for the title between the first group and the dialog top edge
  &:first-of-type {
    margin-top: fn.use-var(gap, large);
  }
}

// Title sits on the group box's top-left border; its bg matches the dialog body to "cut" the border
.form-dialog__group-title {
  position: absolute;
  top: 0;
  left: fn.use-var(gap, default);
  transform: translateY(-50%);
  padding: 0 fn.use-var(gap, small);
  background-color: fn.use-var(bg-color, top);
  color: fn.use-var(text-color, title);
  font-size: fn.use-var(font-size-main, default);
  font-weight: 500;
}
</style>
