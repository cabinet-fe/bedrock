<script setup lang="ts">
defineOptions({ name: "SystemMailSettings" });

import { computed, onMounted, reactive, ref, useTemplateRef } from "vue";
import { message, type FormExposed } from "@veltra/desktop";

import { getMailSMTPConfig, saveMailSMTPConfig, sendMailSMTPTest } from "@/api/system";
import type { MailSMTPConfig } from "@/api/types";
import { usePermission } from "@/composables/use-permission";

const { hasPermission } = usePermission();

const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const hasPassword = ref(false);

const formRef = useTemplateRef<FormExposed>("form");
const testFormRef = useTemplateRef<FormExposed>("testForm");

const form = reactive({
  host: "",
  port: 0,
  username: "",
  password: "",
  from_address: "",
});

const testForm = reactive({
  to: "",
});

const passwordPlaceholder = computed(() =>
  hasPassword.value ? "已设置密码，留空则保持不变" : "未设置密码，可留空",
);

function applyConfig(config: MailSMTPConfig) {
  form.host = config.host;
  form.port = config.port;
  form.username = config.username;
  form.from_address = config.from_address;
  // Never backfill the password (the API only returns a mask).
  form.password = "";
  hasPassword.value = config.password === "******";
}

onMounted(async () => {
  loading.value = true;
  try {
    const config = await getMailSMTPConfig();
    if (config) {
      applyConfig(config);
    }
  } catch (err) {
    message.error(err instanceof Error ? err.message : "查询 SMTP 配置失败");
  } finally {
    loading.value = false;
  }
});

async function handleSave() {
  const valid = await formRef.value?.validate();
  if (!valid) return;

  saving.value = true;
  try {
    const saved = await saveMailSMTPConfig({
      host: form.host.trim(),
      port: form.port,
      username: form.username.trim(),
      password: form.password || undefined,
      from_address: form.from_address.trim(),
    });
    applyConfig(saved);
    message.success("SMTP 配置已保存");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "保存 SMTP 配置失败");
  } finally {
    saving.value = false;
  }
}

async function handleSendTest() {
  const valid = await testFormRef.value?.validate();
  if (!valid) return;

  testing.value = true;
  try {
    const result = await sendMailSMTPTest({ to: testForm.to.trim() });
    if (result.success) {
      message.success(result.message || "测试邮件已发送");
    } else {
      message.error(result.message || "测试邮件发送失败");
    }
  } catch (err) {
    message.error(err instanceof Error ? err.message : "测试邮件发送失败");
  } finally {
    testing.value = false;
  }
}
</script>

<template>
  <div class="mail-settings">
    <u-card>
      <u-card-content>
        <header class="card-head">
          <h3>SMTP 发件配置</h3>
          <p class="card-desc">系统级发件邮箱，用于向用户发送失败通知等站外邮件。</p>
        </header>

        <u-form ref="form" :model="form" :label-width="96" :disabled="loading">
          <u-input
            label="服务器地址"
            field="host"
            :rules="{ required: '请输入 SMTP 服务器地址' }"
            placeholder="例如：smtp.example.com"
          />
          <u-number-input
            label="端口"
            field="port"
            :min="1"
            :max="65535"
            :rules="{
              required: '请输入端口',
              min: [1, '端口范围为 1-65535'],
              max: [65535, '端口范围为 1-65535'],
            }"
            placeholder="465 走隐式 TLS，其余走 STARTTLS"
          />
          <u-input label="认证账号" field="username" placeholder="留空则不做 SMTP 认证" />
          <u-password-input
            label="认证密码"
            field="password"
            :placeholder="passwordPlaceholder"
            autocomplete="new-password"
          />
          <u-input
            label="发件人邮箱"
            field="from_address"
            :rules="{ required: '请输入发件人邮箱', preset: 'email' }"
            placeholder="例如：noreply@example.com"
          />
        </u-form>

        <div class="card-actions">
          <u-button
            v-if="hasPermission('system_settings:update')"
            type="primary"
            :loading="saving"
            :disabled="loading"
            @click="handleSave"
          >
            保存配置
          </u-button>
        </div>
      </u-card-content>
    </u-card>

    <u-card>
      <u-card-content>
        <header class="card-head">
          <h3>测试发送</h3>
          <p class="card-desc">向指定邮箱发送一封测试邮件，验证当前 SMTP 配置是否可用。</p>
        </header>

        <u-form ref="testForm" :model="testForm" :label-width="96" :disabled="loading">
          <u-input
            label="收件邮箱"
            field="to"
            :rules="{ required: '请输入收件邮箱', preset: 'email' }"
            placeholder="接收测试邮件的邮箱"
          />
        </u-form>

        <div class="card-actions">
          <u-button
            v-if="hasPermission('system_settings:update')"
            type="primary"
            :loading="testing"
            :disabled="loading"
            @click="handleSendTest"
          >
            发送测试邮件
          </u-button>
        </div>
      </u-card-content>
    </u-card>
  </div>
</template>

<style scoped lang="scss">
.mail-settings {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 720px;
}

.card-head {
  margin-bottom: 16px;

  h3 {
    font-size: 15px;
    font-weight: 600;
    margin: 0 0 4px;
  }
}

.card-desc {
  font-size: 13px;
  margin: 0;
}

.card-actions {
  margin-top: 8px;
}
</style>
