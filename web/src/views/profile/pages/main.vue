<script setup lang="ts">
defineOptions({ name: "Profile" });

import { reactive, ref, useTemplateRef } from "vue";
import { message, type FormExposed } from "@veltra/desktop";

import { updateMyEmail, updateMyPassword } from "@/api/system";
import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();

const emailFormRef = useTemplateRef<FormExposed>("emailForm");
const passwordFormRef = useTemplateRef<FormExposed>("passwordForm");

const emailSaving = ref(false);
const passwordSaving = ref(false);

const emailForm = reactive({
  email: auth.user?.email ?? "",
});

const passwordForm = reactive({
  old_password: "",
  new_password: "",
  confirm_password: "",
});

async function handleEmailSave() {
  const valid = await emailFormRef.value?.validate();
  if (!valid) return;

  emailSaving.value = true;
  try {
    const updated = await updateMyEmail({ email: emailForm.email.trim() });
    auth.user = updated;
    message.success("邮箱已更新");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "修改邮箱失败");
  } finally {
    emailSaving.value = false;
  }
}

async function handlePasswordSave() {
  const valid = await passwordFormRef.value?.validate();
  if (!valid) return;
  if (passwordForm.new_password !== passwordForm.confirm_password) {
    message.error("两次输入的新密码不一致");
    return;
  }

  passwordSaving.value = true;
  try {
    await updateMyPassword({
      old_password: passwordForm.old_password,
      new_password: passwordForm.new_password,
    });
    passwordForm.old_password = "";
    passwordForm.new_password = "";
    passwordForm.confirm_password = "";
    passwordFormRef.value?.clearValidate();
    message.success("密码已修改，下次登录请使用新密码");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "修改密码失败");
  } finally {
    passwordSaving.value = false;
  }
}
</script>

<template>
  <div class="profile-page">
    <u-card>
      <u-card-content>
        <header class="card-head">
          <h3>收件邮箱</h3>
          <p class="card-desc">任务与运行失败时，通知邮件发送到该邮箱。</p>
        </header>

        <u-form ref="emailForm" :model="emailForm" :label-width="96">
          <u-input
            label="邮箱"
            field="email"
            :rules="{ required: '请输入邮箱', preset: 'email' }"
            placeholder="例如：me@example.com"
          />
        </u-form>

        <div class="card-actions">
          <u-button type="primary" :loading="emailSaving" @click="handleEmailSave">
            保存邮箱
          </u-button>
        </div>
      </u-card-content>
    </u-card>

    <u-card>
      <u-card-content>
        <header class="card-head">
          <h3>修改密码</h3>
          <p class="card-desc">需先验证当前密码；修改成功后旧密码立即失效。</p>
        </header>

        <u-form ref="passwordForm" :model="passwordForm" :label-width="96">
          <u-password-input
            label="当前密码"
            field="old_password"
            :rules="{ required: '请输入当前密码' }"
            autocomplete="current-password"
          />
          <u-password-input
            label="新密码"
            field="new_password"
            :rules="{ required: '请输入新密码' }"
            autocomplete="new-password"
          />
          <u-password-input
            label="确认新密码"
            field="confirm_password"
            :rules="{ required: '请再次输入新密码' }"
            autocomplete="new-password"
          />
        </u-form>

        <div class="card-actions">
          <u-button type="primary" :loading="passwordSaving" @click="handlePasswordSave">
            修改密码
          </u-button>
        </div>
      </u-card-content>
    </u-card>
  </div>
</template>

<style scoped lang="scss">
.profile-page {
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
