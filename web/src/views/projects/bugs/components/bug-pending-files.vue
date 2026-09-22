<script setup lang="ts">
defineOptions({ name: "BugPendingFiles" });

const files = defineModel<File[]>({ required: true });

function handlePick(picked: File[]) {
  files.value.push(...picked);
}

function removeAt(index: number) {
  files.value.splice(index, 1);
}
</script>

<template>
  <div class="pending-files">
    <div v-if="files.length" class="pending-list">
      <span v-for="(file, i) in files" :key="`${file.name}-${i}`" class="pending-chip">
        <span class="pending-name" :title="file.name">{{ file.name }}</span>
        <button type="button" class="pending-remove" title="移除" @click="removeAt(i)">×</button>
      </span>
    </div>
    <u-file-picker accept="*/*" @pick="handlePick" />
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.pending-files {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  width: 100%;
}

.pending-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.pending-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 2px 6px 2px 8px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, small);
  background: fn.use-var(bg-color, muted);
  font-size: 12px;
  color: fn.use-var(text-color, default);
}

.pending-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pending-remove {
  padding: 0 2px;
  border: none;
  background: none;
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  color: fn.use-var(text-color, secondary);

  &:hover {
    color: fn.use-var(color, danger);
  }
}
</style>
