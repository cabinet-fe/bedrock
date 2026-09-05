<script setup lang="ts">
defineOptions({ name: "ProviderFormDialog" });

import { computed, reactive, ref, useTemplateRef, watch } from "vue";
import { message, type FormExposed } from "@veltra/desktop";

import { createProvider, updateProvider } from "@/api/ai";
import type { AiProvider, AiProviderInput } from "@/api/types";

const open = defineModel<boolean>({ required: true });

const props = defineProps<{
  provider: AiProvider | null;
}>();

const emit = defineEmits<{
  saved: [provider: AiProvider];
}>();

const formRef = useTemplateRef<FormExposed>("form");
const busy = ref(false);

const form = reactive({
  name: "",
  api_url: "",
  api_key: "",
  enabled: true,
  notes: "",
});

const isEdit = computed(() => !!props.provider);

const apiKeyPlaceholder = computed(() => {
  if (isEdit.value && props.provider?.has_api_key) {
    return "已配置 API Key，留空则不修改";
  }
  return "请输入 API Key (可选)";
});

function resetForm() {
  if (props.provider) {
    form.name = props.provider.name;
    form.api_url = props.provider.api_url;
    form.api_key = "";
    form.enabled = props.provider.enabled;
    form.notes = props.provider.notes || "";
  } else {
    form.name = "";
    form.api_url = "";
    form.api_key = "";
    form.enabled = true;
    form.notes = "";
  }
  formRef.value?.clearValidate();
}

watch(
  open,
  (visible) => {
    if (visible) {
      resetForm();
    }
  },
  { immediate: true },
);

async function handleSubmit() {
  if (busy.value) return;
  const valid = await formRef.value?.validate();
  if (!valid) return;

  busy.value = true;
  try {
    const payload: AiProviderInput = {
      name: form.name.trim(),
      api_url: form.api_url.trim(),
      enabled: form.enabled,
      notes: form.notes.trim(),
    };
    if (form.api_key.trim()) {
      payload.api_key = form.api_key.trim();
    }

    let savedProvider: AiProvider;
    if (props.provider) {
      savedProvider = await updateProvider(props.provider.id, payload);
      message.success("服务商已更新");
    } else {
      savedProvider = await createProvider(payload);
      message.success("服务商已创建");
    }
    open.value = false;
    emit("saved", savedProvider);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存服务商失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <u-dialog v-model="open" :title="isEdit ? '编辑服务商' : '新建服务商'" style="width: 560px">
    <u-form ref="form" :model="form" label-width="90px">
      <u-input
        label="服务商名称"
        field="name"
        :rules="{ required: '请输入服务商名称' }"
        placeholder="例如：OpenAI / DeepSeek / 通义千问"
      />
      <u-input
        label="API 地址"
        field="api_url"
        :rules="{ required: '请输入 API 地址' }"
        placeholder="例如：https://api.openai.com/v1"
      />
      <u-password-input
        label="API Key"
        field="api_key"
        :placeholder="apiKeyPlaceholder"
        autocomplete="new-password"
      />
      <u-switch label="启用状态" field="enabled" />
      <u-textarea label="备注" field="notes" :rows="3" placeholder="可选备注信息" />
    </u-form>

    <template #footer="{ close }">
      <u-button @click="close()">取消</u-button>
      <u-button type="primary" :loading="busy" @click="handleSubmit">保存</u-button>
    </template>
  </u-dialog>
</template>
