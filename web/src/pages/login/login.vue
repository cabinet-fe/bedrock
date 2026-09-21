<script setup lang="ts">
import { onScopeDispose, reactive, ref, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";
import { message } from "@veltra/desktop";

import { useAuthStore } from "@/stores/auth";
import { useLoginFlow } from "./flow-canvas";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const usernameInputRef = useTemplateRef<HTMLInputElement>("username");
const passwordInputRef = useTemplateRef<HTMLInputElement>("password");
const confirmInputRef = useTemplateRef<HTMLInputElement>("confirm");
const flowCanvasRef = useTemplateRef<HTMLCanvasElement>("flowCanvas");
const hubCardRef = useTemplateRef<HTMLFormElement>("hubCard");
const stageRef = useTemplateRef<HTMLElement>("stage");
const mode = ref<"login" | "register">("login");
// 嵌入部署由服务端注入；dev / 未注入时视为开放
const allowRegister = window.__BEDROCK_ALLOW_REGISTER__ !== false;
const loading = ref(false);
const formData = reactive({
  username: "",
  password: "",
  confirm: "",
});
const errors = reactive({
  username: "",
  password: "",
  confirm: "",
});

// 登录面板即平台枢纽：画布上的左列端点流入、右列端点流出
useLoginFlow(flowCanvasRef, hubCardRef);

// 面板 3D 倾斜：指针坐标只在 rAF 回调里消费一次，角度/眩光写成 CSS 变量，平滑交给 transition
const tiltReady =
  window.matchMedia("(hover: hover) and (pointer: fine)").matches &&
  !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
const MAX_TILT = 7;
let tiltRaf = 0;
let ptrX = 0;
let ptrY = 0;

function onStagePointer(event: PointerEvent) {
  if (!tiltReady) return;
  ptrX = event.clientX;
  ptrY = event.clientY;
  if (!tiltRaf) tiltRaf = requestAnimationFrame(applyTilt);
}

function applyTilt() {
  tiltRaf = 0;
  const hub = hubCardRef.value;
  const stage = stageRef.value;
  if (!hub || !stage) return;
  const r = hub.getBoundingClientRect();
  const nx = Math.max(-1, Math.min(1, (ptrX / innerWidth) * 2 - 1));
  const ny = Math.max(-1, Math.min(1, (ptrY / innerHeight) * 2 - 1));
  stage.style.setProperty("--rx", `${(-ny * MAX_TILT).toFixed(2)}deg`);
  stage.style.setProperty("--ry", `${(nx * MAX_TILT).toFixed(2)}deg`);
  stage.style.setProperty("--gx", `${(((ptrX - r.left) / r.width) * 100).toFixed(1)}%`);
  stage.style.setProperty("--gy", `${(((ptrY - r.top) / r.height) * 100).toFixed(1)}%`);
  stage.style.setProperty("--px", nx.toFixed(3));
  stage.style.setProperty("--py", ny.toFixed(3));
}

// 离开页面时面板回正；眩光位置保留，避免渐变跳变
function onStageLeave() {
  if (!tiltReady) return;
  cancelAnimationFrame(tiltRaf);
  tiltRaf = 0;
  const stage = stageRef.value;
  if (!stage) return;
  for (const [prop, value] of [
    ["--rx", "0deg"],
    ["--ry", "0deg"],
    ["--px", "0"],
    ["--py", "0"],
  ] as const) {
    stage.style.setProperty(prop, value);
  }
}

onScopeDispose(() => cancelAnimationFrame(tiltRaf));

function validate() {
  errors.username = formData.username ? "" : "请输入用户名";
  errors.password = formData.password ? "" : "请输入密码";
  if (mode.value === "register") {
    if (formData.username && (formData.username.length < 3 || formData.username.length > 50)) {
      errors.username = "用户名需 3-50 个字符";
    }
    if (formData.password && formData.password.length < 8) {
      errors.password = "密码至少 8 位";
    }
    errors.confirm = formData.confirm === formData.password ? "" : "两次输入的密码不一致";
    return !errors.username && !errors.password && !errors.confirm;
  }
  return !errors.username && !errors.password;
}

// 切换登录/注册：保留用户名，清掉密码与错误
function toggleMode() {
  mode.value = mode.value === "login" ? "register" : "login";
  formData.password = "";
  formData.confirm = "";
  errors.username = "";
  errors.password = "";
  errors.confirm = "";
}

// 点击面板空白处时，聚焦第一个待填字段
function focusFirstEmptyField(event: MouseEvent) {
  if ((event.target as HTMLElement).closest("input, button")) return;
  if (!formData.username) usernameInputRef.value?.focus();
  else if (!formData.password) passwordInputRef.value?.focus();
  else if (mode.value === "register" && !formData.confirm) confirmInputRef.value?.focus();
}

async function handleSubmit() {
  if (loading.value || !validate()) return;

  loading.value = true;
  try {
    if (mode.value === "register") {
      await auth.register(formData.username, formData.password);
    } else {
      await auth.login(formData.username, formData.password);
    }
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "/";
    await router.replace(redirect || "/");
  } catch (err) {
    const fallback = mode.value === "register" ? "注册失败" : "登录失败";
    message.error(err instanceof Error ? err.message : fallback);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div ref="stage" class="login-page" @pointermove="onStagePointer" @pointerleave="onStageLeave">
    <canvas ref="flowCanvas" class="flow" aria-hidden="true" />

    <!-- 淡墨巨字，衬于面板之后 -->
    <p class="backdrop-glyph" aria-hidden="true">磐</p>

    <!-- 中央枢纽即登录入口：角色端流入，交付端流出 -->
    <form ref="hubCard" class="hub" @submit.prevent="handleSubmit" @click="focusFirstEmptyField">
      <header class="hub-brand">
        <span class="seal" aria-hidden="true">磐</span>
        <div class="brand-copy">
          <p class="brand-name">BEDROCK</p>
          <p class="brand-motto">磐石 · 人-AI智能协作开发平台</p>
        </div>
      </header>

      <ul class="hub-modules" aria-label="平台能力">
        <li>构建</li>
        <li>流水线</li>
        <li>智能体</li>
        <li>技能</li>
      </ul>

      <p class="term-line">
        $ bedrock {{ mode === "login" ? "login" : "register --new-user" }}
        <span class="cursor" aria-hidden="true" />
      </p>

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
          :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
          @input="errors.password = ''"
        />
      </label>
      <p v-if="errors.password" class="term-error">✗ {{ errors.password }}</p>

      <label v-if="mode === 'register'" class="term-line term-field">
        <span class="term-prompt">confirm:</span>
        <input
          ref="confirm"
          v-model="formData.confirm"
          type="password"
          autocomplete="new-password"
          @input="errors.confirm = ''"
        />
      </label>
      <p v-if="mode === 'register' && errors.confirm" class="term-error">✗ {{ errors.confirm }}</p>

      <button class="term-submit" type="submit" :disabled="loading">
        <template v-if="loading">{{ mode === "login" ? "[ 验证中 … ]" : "[ 创建中 … ]" }}</template>
        <template v-else>{{ mode === "login" ? "[ 登 录 ]" : "[ 注 册 ]" }}</template>
      </button>

      <button v-if="allowRegister" class="term-switch" type="button" @click="toggleMode">
        {{ mode === "login" ? "no account? register →" : "has account? login →" }}
      </button>
    </form>
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
  perspective: 1100px;
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

/* 节点流动画：蓝图网格 + 左右端点连线与粒子 */
.flow {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

/* 淡墨「磐」字水印，位于画布之上、面板之下 */
.backdrop-glyph {
  position: absolute;
  top: 50%;
  left: 50%;
  translate: -50% -50%;
  margin: 0;
  font-family: var(--serif);
  font-size: clamp(320px, 44vw, 620px);
  font-weight: 700;
  line-height: 1;
  color: var(--u-text-color-title);
  opacity: 0.05;
  pointer-events: none;
  user-select: none;
}

/* 登录面板：画布网络的中央枢纽 */
.hub {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  width: min(400px, 100%);
  box-sizing: border-box;
  padding: 24px 26px 22px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.9;
  color: var(--u-text-color-second);
  background: var(--u-bg-color-top);
  border: var(--u-border);
  border-radius: var(--u-radius-large);
  box-shadow: var(--u-shadow-lg, var(--u-shadow-sm));
  cursor: text;
  animation: rise 0.6s cubic-bezier(0.22, 1, 0.36, 1) both;

  // 工程制图角标
  &::before,
  &::after {
    content: "";
    position: absolute;
    width: 14px;
    height: 14px;
    border: 1px solid var(--u-color-primary);
    opacity: 0.75;
  }

  &::before {
    top: -6px;
    left: -6px;
    border-right: none;
    border-bottom: none;
  }

  &::after {
    right: -6px;
    bottom: -6px;
    border-left: none;
    border-top: none;
  }
}

.hub-brand {
  display: flex;
  align-items: center;
  gap: 14px;
  margin: 0 0 16px;
}

/* 朱砂小印 */
.seal {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: var(--u-radius-small);
  background: var(--seal);
  color: var(--u-bg-color-top);
  font-family: var(--serif);
  font-size: 21px;
  box-shadow: 0 1px 3px rgb(43 42 38 / 25%);
  transform: rotate(3deg);
}

.brand-name {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.32em;
  color: var(--u-text-color-title);
}

.brand-motto {
  margin: 0;
  font-family: var(--serif);
  font-size: 12px;
  letter-spacing: 0.3em;
  color: var(--u-text-color-assist);
}

/* 平台能力标签：流经枢纽之物 */
.hub-modules {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0 0 16px;
  padding: 0;
  list-style: none;

  li {
    padding: 0 9px;
    font-size: 11px;
    letter-spacing: 0.12em;
    color: var(--u-text-color-second);
    border: 1px solid var(--u-border-muted-color);
    border-radius: var(--u-radius-small);
  }
}

.term-line {
  margin: 0 0 4px;
  color: var(--u-color-primary);
}

.term-field {
  display: flex;
  align-items: baseline;
  gap: 8px;
  color: var(--u-text-color-second);
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

/* 登录/注册模式切换：终端里的下一条命令 */
.term-switch {
  align-self: flex-end;
  margin-top: 6px;
  padding: 0;
  font: inherit;
  color: var(--u-color-primary);
  background: transparent;
  border: none;
  cursor: pointer;
  opacity: 0.85;
  transition: opacity 0.2s;

  &:hover {
    opacity: 1;
    text-decoration: underline dotted;
  }
}

/* 输入聚焦后隐去装饰光标，避免与原生 caret 争辉 */
.hub:focus-within .cursor {
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

/* rise 走 translate 属性，与倾斜的 transform 正交组合互不覆盖 */
@keyframes rise {
  from {
    opacity: 0;
    translate: 0 16px;
  }

  to {
    opacity: 1;
    translate: 0 0;
  }
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

/* 面板 3D：倾斜角/眩光位置由 --rx/--ry/--gx/--gy 驱动（脚本写入），全在合成器层；
   仅支持悬停指针且未开启「减弱动态效果」时启用 */
@media (hover: hover) and (pointer: fine) and (prefers-reduced-motion: no-preference) {
  .hub {
    transform: rotateX(var(--rx, 0deg)) rotateY(var(--ry, 0deg));
    transition: transform 0.2s ease-out;
    transform-style: preserve-3d;
    will-change: transform;
    background:
      radial-gradient(
        360px circle at var(--gx, 50%) var(--gy, 30%),
        color-mix(in srgb, var(--u-color-primary) 13%, transparent),
        transparent 65%
      ),
      var(--u-bg-color-top);
  }

  /* 景深分层：品牌浮得最高，输入区最贴近面板 */
  .hub-brand {
    translate: 0 0 34px;
  }

  .hub-modules {
    translate: 0 0 22px;
  }

  .term-field {
    translate: 0 0 14px;
  }

  .term-submit {
    translate: 0 0 18px;
  }

  /* 背景淡墨巨字与面板反向视差，强化纵深 */
  .backdrop-glyph {
    translate: calc(-50% + var(--px, 0) * -22px) calc(-50% + var(--py, 0) * -14px);
    transition: translate 0.25s ease-out;
  }
}

/* 窄屏收起节点网络，只留登录面板 */
@media (max-width: 759px) {
  .flow {
    display: none;
  }
}

/* iOS 对小于 16px 的输入框会自动放大页面，触屏设备上抬高一档 */
@media (pointer: coarse) {
  .term-field input {
    font-size: 16px;
  }
}
</style>
