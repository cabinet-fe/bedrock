<script setup lang="ts">
defineOptions({ name: "DashboardRouteDialog" });

import { computed } from "vue";
import { useRouter } from "vue-router";

import type { RouteTarget } from "./index";

const { target } = defineProps<{
  title: string;
  target: RouteTarget | null;
}>();

const open = defineModel<boolean>({ default: false });

const router = useRouter();

const src = computed(() => {
  if (!target) return "";
  return router.resolve({
    name: target.name,
    params: target.params,
    query: { _embed: "1" },
  }).href;
});
</script>

<template>
  <u-dialog v-model="open" :title="title" style="width: 92vw; max-width: 1440px">
    <template #default="{ maximized }">
      <div
        v-if="open && src"
        class="dashboard-route-dialog__body"
        :style="{ height: maximized ? '100%' : '80vh', minHeight: maximized ? 0 : '480px' }"
      >
        <iframe :key="src" :src="src" class="dashboard-route-dialog__iframe" :title="title" />
      </div>
    </template>
  </u-dialog>
</template>

<style scoped lang="scss">
.dashboard-route-dialog__body {
  overflow: hidden;
}

.dashboard-route-dialog__iframe {
  width: 100%;
  height: 100%;
  border: 0;
}
</style>
