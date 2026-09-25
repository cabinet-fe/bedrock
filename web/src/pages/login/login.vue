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
// Injected by the server for embedded deployments; treated as open in dev / when not injected
const allowRegister = window.__BEDROCK_ALLOW_REGISTER__ !== false;
const loading = ref(false);
// Optional built-in roles for signup (one-to-one with the backend seed)
const ROLE_OPTIONS = [
  { label: "开发", value: "developer" },
  { label: "测试", value: "tester" },
  { label: "运维", value: "ops" },
  { label: "实施", value: "implementer" },
  { label: "产品", value: "product" },
];
const formData = reactive({
  username: "",
  password: "",
  confirm: "",
  role: "developer",
});
const errors = reactive({
  username: "",
  password: "",
  confirm: "",
});

// The login panel is the platform hub: canvas left-column anchors flow in, right-column anchors flow out
useLoginFlow(flowCanvasRef, hubCardRef);

// Panel 3D tilt: pointer coords are consumed once per rAF callback; angle/glare go into CSS vars; smoothing is left to transition
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

// On leave, the panel returns to center; glare position is kept to avoid gradient jumps
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

// Switch login/signup: keep the username, clear password and errors
function toggleMode() {
  mode.value = mode.value === "login" ? "register" : "login";
  formData.password = "";
  formData.confirm = "";
  errors.username = "";
  errors.password = "";
  errors.confirm = "";
}

// Clicking the panel's blank area focuses the first empty field
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
      await auth.register(formData.username, formData.password, formData.role);
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

      <label v-if="mode === 'register'" class="term-line term-field">
        <span class="term-prompt">role:</span>
        <select v-model="formData.role" class="term-select">
          <option v-for="opt in ROLE_OPTIONS" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
      </label>

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
  // Brand accent; the only hard-coded color outside theme tokens
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
    // Paper texture
    repeating-linear-gradient(95deg, transparent 0 6px, rgb(64 54 32 / 1.1%) 6px 7px),
    var(--u-bg-color-bottom);
  color: var(--u-text-color-title);
}

/* Node flow animation: blueprint grid + left/right anchor connections and particles */
.flow {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

/* Faint ink "磐" watermark above the canvas, below the panel */
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

/* Login panel: the central hub of the canvas network */
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

  // Engineering-drawing corner mark
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

/* Cinnabar seal */
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

/* Platform capability tags: things flowing through the hub */
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

.term-select {
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
  cursor: pointer;
  transition: border-color 0.2s;

  &:focus {
    border-bottom-color: var(--seal);
    border-bottom-style: solid;
  }

  option {
    color: initial;
    background: initial;
  }
}

.term-error {
  margin: 0;
  // Align the input column (prompt 9ch wide + 8px gap)
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

/* Login/signup mode switch: the next command in the terminal */
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

/* Hide the decorative cursor on focus so it does not fight the native caret */
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

/* rise uses translate, orthogonal to the tilt transform so they never overwrite each other */
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

/* Panel 3D: tilt/glare driven by --rx/--ry/--gx/--gy (written by script), all on the compositor layer;
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

  /* Depth layering: brand floats highest, input closest to the panel */
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

  /* Background giant ink characters parallax opposite the panel, deepening the depth */
  .backdrop-glyph {
    translate: calc(-50% + var(--px, 0) * -22px) calc(-50% + var(--py, 0) * -14px);
    transition: translate 0.25s ease-out;
  }
}

/* Collapse the node network on narrow screens, leaving only the login panel */
@media (max-width: 759px) {
  .flow {
    display: none;
  }
}

/* iOS auto-zooms pages for inputs under 16px; bump the size on touch devices */
@media (pointer: coarse) {
  .term-field input {
    font-size: 16px;
  }
}
</style>
