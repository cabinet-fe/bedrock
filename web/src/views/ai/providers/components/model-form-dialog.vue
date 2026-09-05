<script setup lang="ts">
defineOptions({ name: "ModelFormDialog" });

import { computed, reactive, ref, useTemplateRef, watch } from "vue";
import { message, type FormExposed } from "@veltra/desktop";

import { createModel, updateModel } from "@/api/ai";
import type { AiModel, AiModelInput, ReasoningEffortOption } from "@/api/types";

const open = defineModel<boolean>({ required: true });

const props = defineProps<{
  providerId: number;
  model: AiModel | null;
}>();

const emit = defineEmits<{
  saved: [model: AiModel];
}>();

const formRef = useTemplateRef<FormExposed>("form");
const busy = ref(false);

const form = reactive({
  name: "",
  model_id: "",
  enabled: true,
  sort_order: 0,
  reasoning_efforts: [] as ReasoningEffortOption[],
  default_params_text: "",
  notes: "",
});

const isEdit = computed(() => !!props.model);

function resetForm() {
  if (props.model) {
    form.name = props.model.name;
    form.model_id = props.model.model_id;
    form.enabled = props.model.enabled;
    form.sort_order = props.model.sort_order ?? 0;
    form.reasoning_efforts = (props.model.reasoning_efforts ?? []).map((item) => ({
      value: item.value,
      label: item.label,
    }));
    form.default_params_text = props.model.default_params
      ? JSON.stringify(props.model.default_params, null, 2)
      : "";
    form.notes = props.model.notes || "";
  } else {
    form.name = "";
    form.model_id = "";
    form.enabled = true;
    form.sort_order = 0;
    form.reasoning_efforts = [];
    form.default_params_text = "";
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

function formatJson() {
  const text = form.default_params_text.trim();
  if (!text) {
    form.default_params_text = "{}";
    return;
  }
  try {
    const parsed = JSON.parse(text);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      message.warning("默认参数必须为 JSON 对象");
      return;
    }
    form.default_params_text = JSON.stringify(parsed, null, 2);
    message.success("格式化成功");
  } catch (err) {
    message.error("JSON 格式错误: " + (err instanceof Error ? err.message : String(err)));
  }
}

async function handleSubmit() {
  if (busy.value) return;
  const valid = await formRef.value?.validate();
  if (!valid) return;

  for (const item of form.reasoning_efforts) {
    const val = item.value?.trim();
    const lbl = item.label?.trim();
    if ((val && !lbl) || (!val && lbl)) {
      message.error("推理等级档位的英文值与显示名均需填写");
      return;
    }
  }
  const validEfforts = form.reasoning_efforts
    .filter((item) => item.value?.trim() && item.label?.trim())
    .map((item) => ({
      value: item.value.trim(),
      label: item.label.trim(),
    }));

  let defaultParams: Record<string, unknown> | undefined;
  const text = form.default_params_text.trim();
  if (text) {
    try {
      const parsed = JSON.parse(text);
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        message.error('默认参数必须为 JSON 对象，如 {"temperature": 0.7}');
        return;
      }
      defaultParams = parsed;
    } catch (err) {
      message.error(
        "默认参数 JSON 格式不合法: " + (err instanceof Error ? err.message : String(err)),
      );
      return;
    }
  }

  busy.value = true;
  try {
    const payload: AiModelInput = {
      name: form.name.trim(),
      model_id: form.model_id.trim(),
      enabled: form.enabled,
      sort_order: Number(form.sort_order) || 0,
      reasoning_efforts: validEfforts,
      default_params: defaultParams,
      notes: form.notes.trim(),
    };

    let savedModel: AiModel;
    if (props.model) {
      savedModel = await updateModel(props.providerId, props.model.id, payload);
      message.success("模型已更新");
    } else {
      savedModel = await createModel(props.providerId, payload);
      message.success("模型已创建");
    }
    open.value = false;
    emit("saved", savedModel);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存模型失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <u-dialog v-model="open" :title="isEdit ? '编辑模型' : '新建模型'" style="width: 620px">
    <u-form ref="form" :model="form" label-width="90px">
      <u-input
        label="显示名称"
        field="name"
        :rules="{ required: '请输入显示名称' }"
        placeholder="例如：GPT-4o / DeepSeek-V3"
      />
      <u-input
        label="模型标识"
        field="model_id"
        :rules="{ required: '请输入模型标识' }"
        placeholder="例如：gpt-4o / deepseek-chat"
      />
      <u-number-input label="排序权重" field="sort_order" :min="0" tips="数值越小排序越靠前" />
      <u-switch label="启用状态" field="enabled" />
      <u-group-input
        field="reasoning_efforts"
        label="推理档位"
        span="full"
        tips="为支持推理等级的模型配置档位，如 low/低、medium/中、high/高"
        :item-default="{ value: '', label: '' }"
        :item-style="{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%' }"
      >
        <template #default="{ item }">
          <u-input v-model="item.value" placeholder="英文值 (如 low / medium / high)" />
          <u-input v-model="item.label" placeholder="中文显示名 (如 低 / 中 / 高)" />
        </template>
      </u-group-input>
      <u-form-item
        field="default_params_text"
        label="默认参数"
        span="full"
        tips='可选，必须为合法的 JSON 对象，如 { "temperature": 0.7 }'
      >
        <div class="json-editor">
          <div class="json-editor__toolbar">
            <u-button size="small" text type="primary" @click.prevent="formatJson">
              格式化 JSON
            </u-button>
          </div>
          <u-textarea
            v-model="form.default_params_text"
            :rows="4"
            placeholder='{&#10;  "temperature": 0.7&#10;}'
          />
        </div>
      </u-form-item>
      <u-textarea label="备注" field="notes" :rows="2" placeholder="可选备注说明" />
    </u-form>

    <template #footer="{ close }">
      <u-button @click="close()">取消</u-button>
      <u-button type="primary" :loading="busy" @click="handleSubmit">保存</u-button>
    </template>
  </u-dialog>
</template>

<style scoped>
.json-editor {
  display: flex;
  flex-direction: column;
  width: 100%;
  gap: 4px;
}
.json-editor__toolbar {
  display: flex;
  justify-content: flex-end;
}
:deep(.u-group-input__item > .u-input) {
  flex: 1;
  min-width: 0;
}
</style>
