<script setup lang="ts">
defineOptions({ name: "BugAttachmentPreview" });

import { computed, onScopeDispose, ref } from "vue";

import { http } from "@/api/http";
import type { BugAttachment } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";

const IMAGE_CONTENT_TYPES = new Set([
  "image/png",
  "image/jpeg",
  "image/jpg",
  "image/gif",
  "image/webp",
]);

const props = defineProps<{
  attachment: BugAttachment;
  projectId: number;
  bugId: number;
  canManage?: boolean;
}>();

const emit = defineEmits<{
  download: [attachment: BugAttachment];
  delete: [attachment: BugAttachment];
}>();

const isImage = computed(() => {
  const contentType = props.attachment.content_type?.toLowerCase();
  return !!contentType && IMAGE_CONTENT_TYPES.has(contentType);
});

// Thumbnails reuse the authenticated download endpoint: fetch the blob once,
// then serve both thumbnail and fullscreen viewer from the same object URL.
const previewUrl = ref("");
const lightboxOpen = ref(false);
let disposed = false;

onScopeDispose(() => {
  disposed = true;
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value);
});

if (isImage.value) void loadPreview();

async function loadPreview(): Promise<void> {
  try {
    const { body } = await http.get<Blob>(
      `/projects/${props.projectId}/bugs/${props.bugId}/attachments/${props.attachment.id}/download`,
      { responseType: "blob" },
    );
    const url = URL.createObjectURL(body);
    if (disposed) {
      URL.revokeObjectURL(url);
      return;
    }
    previewUrl.value = url;
  } catch {
    // Thumbnail stays hidden; filename + download remain available.
  }
}

const viewerFiles = computed(() =>
  previewUrl.value
    ? [
        {
          id: String(props.attachment.id),
          name: props.attachment.filename,
          src: previewUrl.value,
          kind: "image" as const,
        },
      ]
    : [],
);

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
</script>

<template>
  <div class="attachment-row">
    <div class="attachment-main">
      <button
        v-if="previewUrl"
        type="button"
        class="attachment-thumb"
        title="点击放大查看"
        @click="lightboxOpen = true"
      >
        <img :src="previewUrl" :alt="attachment.filename" />
      </button>
      <div class="attachment-details">
        <span class="attachment-title">{{ attachment.filename }}</span>
        <span class="attachment-meta">
          {{ formatBytes(attachment.file_size) }} ·
          {{ attachment.creator_name || attachment.creator_username || "—" }} ·
          {{ formatDateTime(attachment.created_at) }}
        </span>
      </div>
    </div>
    <div class="attachment-actions">
      <u-action @run="emit('download', attachment)">下载</u-action>
      <u-action v-if="canManage" type="danger" need-confirm @run="emit('delete', attachment)">
        删除
      </u-action>
    </div>
  </div>

  <u-file-viewer v-model:open="lightboxOpen" :files="viewerFiles" />
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.attachment-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, default);
  background: fn.use-var(bg-color, top);
}

.attachment-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.attachment-thumb {
  display: block;
  flex-shrink: 0;
  width: 48px;
  height: 48px;
  padding: 0;
  overflow: hidden;
  cursor: pointer;
  border: fn.use-var(border, muted);
  border-radius: fn.use-var(radius, small);
  background: fn.use-var(bg-color, muted);

  img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
}

.attachment-details {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.attachment-title {
  font-size: 13px;
  color: fn.use-var(text-color, title);
}

.attachment-meta {
  font-size: 12px;
  color: fn.use-var(text-color, secondary);
}

.attachment-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
</style>
