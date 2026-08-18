<script setup lang="ts">
defineOptions({ name: "VersionPickDialog" });

import { reactive, ref, shallowRef, watch } from "vue";

import type { VersionCatalog } from "@/api/types";
import FormDialog from "@/components/form-dialog";

const open = defineModel<boolean>({ required: true });

const {
  title,
  initialVersion = "",
  loadVersions,
} = defineProps<{
  title: string;
  initialVersion?: string;
  loadVersions: () => Promise<VersionCatalog>;
}>();

const emit = defineEmits<{
  submit: [version: string];
}>();

const form = reactive({ version: "" });
const options = shallowRef<{ label: string; value: string }[]>([]);
const catalogUrl = ref("");
const loading = ref(false);
const loadError = ref("");

watch(open, async (isOpen) => {
  if (!isOpen) return;
  form.version = initialVersion;
  options.value = [];
  catalogUrl.value = "";
  loadError.value = "";
  loading.value = true;
  try {
    const data = await loadVersions();
    options.value = (data.items ?? []).map((item) => ({ label: item, value: item }));
    catalogUrl.value = data.catalog_url ?? "";
    loadError.value = data.error ?? "";
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : "无法加载版本列表";
  } finally {
    loading.value = false;
  }
});

function onSubmit() {
  emit("submit", form.version.trim());
  open.value = false;
}
</script>

<template>
  <FormDialog
    v-model="open"
    :title
    :model="form"
    confirm-text="确认"
    label-width="100px"
    style="width: 520px"
    @submit="onSubmit"
  >
    <template #prepend>
      <p v-if="loading" class="form-tip">正在加载可安装版本…</p>
      <p v-else-if="loadError" class="form-tip form-tip--warn">
        {{ loadError }}。可直接输入版本，或打开下方目录查阅。
      </p>
      <p v-else class="form-tip">可从列表选择，也可输入列表中没有的版本。</p>
    </template>
    <u-select
      label="目标版本"
      field="version"
      filterable
      creatable
      clearable
      :options="options"
      placeholder="选择或输入版本，例如 1.22.0"
    />
    <template #append>
      <p v-if="catalogUrl" class="form-tip">
        <a :href="catalogUrl" target="_blank" rel="noopener noreferrer">查看完整版本列表</a>
      </p>
    </template>
  </FormDialog>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.form-tip {
  margin: 0 0 4px;
  font-size: 13px;
  color: fn.use-var(text-color, second);
  line-height: 1.5;
}
.form-tip--warn {
  color: fn.use-var(color, warning);
}
a {
  color: fn.use-var(color, primary);
}
</style>
