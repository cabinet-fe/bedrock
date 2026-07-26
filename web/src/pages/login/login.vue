<script setup lang="ts">
import { reactive, ref, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const usernameInputRef = useTemplateRef<HTMLInputElement>("username");
const passwordInputRef = useTemplateRef<HTMLInputElement>("password");
const loading = ref(false);
const formData = reactive({
  username: "",
  password: "",
});
const errors = reactive({
  username: "",
  password: "",
});

function validate() {
  errors.username = formData.username ? "" : "请输入用户名";
  errors.password = formData.password ? "" : "请输入密码";
  return !errors.username && !errors.password;
}

// 点击终端空白处时，聚焦第一个待填字段
function focusFirstEmptyField(event: MouseEvent) {
  if ((event.target as HTMLElement).closest("input, button")) return;
  if (!formData.username) usernameInputRef.value?.focus();
  else if (!formData.password) passwordInputRef.value?.focus();
}

async function handleSubmit() {
  if (loading.value || !validate()) return;

  loading.value = true;
  try {
    await auth.login(formData.username, formData.password);
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/";
    await router.replace(redirect || "/");
  } catch (err) {
    const msg = err instanceof Error ? err.message : "登录失败";
    message.error(msg);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="login-page">
    <!-- 栏外竖批，取《吕氏春秋》句，暗合「磐石」与「朱砂」 -->
    <p class="side-quote side-quote--left" aria-hidden="true">石可破也，而不可夺坚</p>
    <p class="side-quote side-quote--right" aria-hidden="true">丹可磨也，而不可夺赤</p>

    <div class="stage">
      <section class="editorial">
        <p class="kicker">
          <span class="kicker-name">BEDROCK</span>
          <span class="kicker-rule" aria-hidden="true" />
          <span class="kicker-issue">VOL.01</span>
        </p>

        <h1 class="masthead">磐石<span class="seal" aria-hidden="true">磐</span></h1>

        <p class="motto">诸事归一</p>
      </section>

      <!-- 终端即登录入口 -->
      <form class="code-note" @submit.prevent="handleSubmit" @click="focusFirstEmptyField">
        <pre class="code-comment">
/*
 * 磐石 Bedrock · 诸事归一
 * 代码托管 / 持续集成 / 部署运维 / 智能协同
 */</pre>
        <p class="term-line">$ bedrock login <span class="cursor" aria-hidden="true" /></p>

        <label class="term-line term-field">
          <span class="term-prompt">username:</span>
          <input
            ref="username"
            v-model.trim="formData.username"
            type="text"
            autocomplete="username"
            spellcheck="false"
            @input="errors.username = ''"
          />
        </label>
        <p v-if="errors.username" class="term-error">✗ {{ errors.username }}</p>

        <label class="term-line term-field">
          <span class="term-prompt">password:</span>
          <input
            ref="password"
            v-model="formData.password"
            type="password"
            autocomplete="current-password"
            @input="errors.password = ''"
          />
        </label>
        <p v-if="errors.password" class="term-error">✗ {{ errors.password }}</p>

        <button class="term-submit" type="submit" :disabled="loading">
          {{ loading ? "[ 验证中 … ]" : "[ 登 录 ]" }}
        </button>
      </form>
    </div>
  </div>
</template>

<style scoped lang="scss">
.login-page {
  // 品牌点缀色，主题 token 之外的唯一一处硬编码
  --seal: #b3452e;
  --serif: "Songti SC", "STSong", "SimSun", "Noto Serif CJK SC", serif;
  --mono: ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace;

  position: fixed;
  inset: 0;
  isolation: isolate;
  height: 100dvh;
  overflow: hidden;
  overscroll-behavior: none;
  display: grid;
  place-items: center;
  padding: 32px clamp(20px, 4vw, 56px);
  box-sizing: border-box;
  background:
    radial-gradient(ellipse 70% 42% at 50% 0%, var(--u-color-primary-light-9), transparent 75%),
    // 纸纹肌理
    repeating-linear-gradient(95deg, transparent 0 6px, rgb(64 54 32 / 1.1%) 6px 7px),
    var(--u-bg-color-bottom);
  color: var(--u-text-color-title);
}

/* 栏外竖批 */
.side-quote {
  position: absolute;
  top: 50%;
  translate: 0 -50%;
  margin: 0;
  writing-mode: vertical-rl;
  font-family: var(--serif);
  font-size: 15px;
  letter-spacing: 0.5em;
  color: var(--u-text-color-assist);

  &--left {
    left: clamp(20px, 3.5vw, 52px);
  }

  &--right {
    right: clamp(20px, 3.5vw, 52px);
  }
}

.stage {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: 1fr minmax(280px, 380px);
  align-items: center;
  gap: clamp(40px, 6vw, 96px);
  width: min(960px, 100%);
}

/* 刊头 */
.editorial {
  animation: rise 0.7s cubic-bezier(0.22, 1, 0.36, 1) both;
}

.kicker {
  display: flex;
  align-items: center;
  gap: 16px;
  margin: 0 0 28px;
}

.kicker-name {
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.42em;
  color: var(--u-text-color-assist);
}

.kicker-rule {
  width: 56px;
  height: 1px;
  background: var(--u-border-muted-color);
}

.kicker-issue {
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.14em;
  color: var(--u-text-color-assist);
}

.masthead {
  position: relative;
  display: inline-block;
  margin: 0 0 20px;
  font-family: var(--serif);
  font-size: clamp(84px, 9.5vw, 144px);
  font-weight: 700;
  line-height: 1.05;
  letter-spacing: 0.14em;
}

/* 朱砂小印，缀于题名之侧 */
.seal {
  position: absolute;
  right: -40px;
  bottom: 10px;
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: var(--u-radius-small);
  background: var(--seal);
  color: var(--u-bg-color-top);
  font-size: 17px;
  letter-spacing: 0;
  box-shadow: 0 1px 3px rgb(43 42 38 / 25%);
  transform: rotate(3deg);
}

.motto {
  margin: 0;
  font-family: var(--serif);
  font-size: clamp(15px, 1.6vw, 18px);
  letter-spacing: 0.36em;
  color: var(--u-text-color-main);
}

/* 终端即登录入口：文档注释 + 命令行之下直接输入 */
.code-note {
  display: flex;
  flex-direction: column;
  padding: 14px 16px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.9;
  color: var(--u-text-color-second);
  background: var(--u-bg-color-middle);
  border: var(--u-border);
  border-radius: var(--u-radius-default);
  cursor: text;
  animation: rise 0.7s cubic-bezier(0.22, 1, 0.36, 1) 0.08s both;
}

.code-comment {
  margin: 0;
  font: inherit;
  color: var(--u-text-color-assist);
}

.term-line {
  margin: 0;
}

.term-field {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.term-prompt {
  flex: none;
  width: 9ch;
  color: var(--u-color-primary);
}

.term-field input {
  flex: 1;
  min-width: 0;
  padding: 0 0 2px;
  font: inherit;
  color: var(--u-text-color-title);
  background: transparent;
  border: none;
  border-bottom: 1px dashed var(--u-border-color);
  border-radius: 0;
  outline: none;
  caret-color: var(--seal);
  transition: border-color 0.2s;

  &:focus {
    border-bottom-color: var(--seal);
    border-bottom-style: solid;
  }
}

.term-error {
  margin: 0;
  // 对齐输入列（prompt 宽 9ch + 间距 8px）
  padding-left: calc(9ch + 8px);
  color: var(--seal);
}

.term-submit {
  align-self: flex-end;
  margin-top: 10px;
  padding: 2px 10px;
  font: inherit;
  letter-spacing: 0.1em;
  color: var(--seal);
  background: transparent;
  border: 1px solid var(--seal);
  border-radius: var(--u-radius-small);
  cursor: pointer;
  transition:
    background-color 0.2s,
    color 0.2s;

  &:hover:not(:disabled) {
    color: var(--u-bg-color-top);
    background: var(--seal);
  }

  &:disabled {
    opacity: 0.55;
    cursor: default;
  }
}

/* 输入聚焦后隐去装饰光标，避免与原生 caret 争辉 */
.code-note:focus-within .cursor {
  animation: none;
  opacity: 0;
}

.cursor {
  display: inline-block;
  width: 7px;
  height: 13px;
  vertical-align: -2px;
  background: var(--u-color-primary);
  animation: blink 1.1s steps(2, jump-none) infinite;
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(16px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

@media (max-width: 1279px) {
  .side-quote {
    display: none;
  }
}

@media (max-width: 1023px) {
  .stage {
    grid-template-columns: 1fr;
    gap: 32px;
    width: min(480px, 100%);
  }

  .masthead {
    font-size: clamp(64px, 16vw, 96px);
  }

  .seal {
    right: -32px;
    bottom: 6px;
    width: 24px;
    height: 24px;
    font-size: 14px;
  }
}

/* iOS 对小于 16px 的输入框会自动放大页面，触屏设备上抬高一档 */
@media (pointer: coarse) {
  .term-field input {
    font-size: 16px;
  }
}
</style>
