<script setup lang="ts">
defineOptions({ name: "AiSkillDetail" });

import { useRoute } from "vue-router";
import { message } from "@veltra/desktop";

import type { SkillPackage } from "@/api/types";
import { useTabsStore } from "@/stores/tabs";

import SkillEditor from "../components/skill-editor.vue";

const route = useRoute();
const tabsStore = useTabsStore();

function parseRouteId(raw: unknown): number | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const id = typeof value === "number" ? value : Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

// Layout keys detail by path and keep-alive caches the instance. Freeze the id at
// setup so deactivated instances do not re-read the global route (which loses :id).
const detailPath = route.path;
const skillId = parseRouteId(route.params.id);

if (skillId == null) {
  message.error("无效 ID");
}

function onSkillLoaded(skill: SkillPackage) {
  if (skill.name) tabsStore.updateTitle(detailPath, skill.name);
}
</script>

<template>
  <div class="skill-detail">
    <SkillEditor v-if="skillId != null" :skill-id="skillId" @loaded="onSkillLoaded" />
    <div v-else class="skill-detail__empty">
      <u-empty text="无效的技能 ID" />
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "@/lib/empty-center.scss" as empty;

.skill-detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.skill-detail__empty {
  @include empty.center(320px);
}
</style>
