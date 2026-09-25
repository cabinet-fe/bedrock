<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { UButton, UIcon, UTag } from "@veltra/desktop";
import { Books, Build, Folder, Layers, Link, Refresh } from "@veltra/icons/normal";

import { listBuildJobs, listBuildPipelines } from "@/api/cicd";
import { getProject, listRequirements } from "@/api/projects";
import type { ProductProject } from "@/api/types";
import { formatDateTime } from "@/lib/datetime";
import { useAiChatStore } from "@/stores/ai-chat";

const props = defineProps<{
  projectId: number;
}>();

const chatStore = useAiChatStore();
const router = useRouter();

const project = ref<ProductProject | null>(null);
const loading = ref(false);
const error = ref("");

const buildJobCount = ref<number | null>(null);
const pipelineCount = ref<number | null>(null);
const requirementCount = ref<number | null>(null);

async function loadData() {
  if (!props.projectId) return;
  loading.value = true;
  error.value = "";
  try {
    const res = await getProject(props.projectId);
    project.value = res;

    // Read related resource counts in parallel
    void Promise.allSettled([
      listBuildJobs({ project_id: props.projectId, page: 1, page_size: 1 }).then((r) => {
        buildJobCount.value = r.total;
      }),
      listBuildPipelines({ project_id: props.projectId, page: 1, page_size: 1 }).then((r) => {
        pipelineCount.value = r.total;
      }),
      listRequirements(props.projectId, { page: 1, page_size: 1 }).then((r) => {
        requirementCount.value = r.total;
      }),
    ]);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载项目详情失败";
  } finally {
    loading.value = false;
  }
}

function openInClassicMode() {
  if (!project.value) return;
  void chatStore.toggleAiMode(false);
  void router.push(`/project/projects/${project.value.id}`);
}

function viewDocs(docType: "api" | "dev") {
  if (!project.value) return;
  chatStore.openRightPanel({
    type: "doc",
    id: 0,
    projectId: project.value.id,
    docType,
    title: `${project.value.name} · ${docType === "api" ? "接口文档" : "开发文档"}`,
  });
}

onMounted(() => {
  void loadData();
});

watch(
  () => props.projectId,
  () => {
    void loadData();
  },
);
</script>

<template>
  <div class="project-detail-panel" :class="{ 'is-loading': loading }">
    <!-- 1. 错误提示 -->
    <div v-if="error" class="project-detail-panel__error">
      <p>{{ error }}</p>
      <u-button size="small" @click="loadData">
        <template #icon>
          <u-icon><Refresh /></u-icon>
        </template>
        重试
      </u-button>
    </div>

    <!-- 2. 主体内容 -->
    <div v-else-if="project" class="project-detail-panel__content">
      <!-- 项目基础概览卡片 -->
      <div class="project-detail-panel__card">
        <div class="project-detail-panel__header">
          <div class="project-title-row">
            <div class="project-title-group">
              <u-icon :size="18" class="project-icon">
                <Folder />
              </u-icon>
              <h3 class="project-name">{{ project.name }}</h3>
            </div>
            <u-button
              text
              size="small"
              class="open-classic-btn"
              title="在主工作区打开完整页面"
              @click="openInClassicMode"
            >
              <template #icon>
                <u-icon :size="13"><Link /></u-icon>
              </template>
              在主工作区打开
            </u-button>
          </div>

          <div class="project-tags-row">
            <u-tag size="small" :type="project.status === 'active' ? 'success' : undefined">
              {{ project.status === "active" ? "已激活" : "已归档" }}
            </u-tag>
            <u-tag size="small" type="info">
              {{ project.is_public ? "公开项目" : "私有项目" }}
            </u-tag>
            <span v-if="project.slug" class="project-slug-badge">
              <code>{{ project.slug }}</code>
            </span>
          </div>
        </div>

        <p class="project-desc">
          {{ project.description || "暂无项目描述" }}
        </p>

        <!-- 标签展示 -->
        <div v-if="project.tags" class="project-label-tags">
          <u-tag v-for="tag in project.tags.split(',').filter(Boolean)" :key="tag" size="small">
            {{ tag.trim() }}
          </u-tag>
        </div>
      </div>

      <!-- 快捷资源统计 -->
      <div class="project-detail-panel__section">
        <h4 class="section-title">关联资源概览</h4>
        <div class="resource-grid">
          <div class="resource-card">
            <div class="resource-card__icon is-build">
              <u-icon :size="16"><Build /></u-icon>
            </div>
            <div class="resource-card__info">
              <span class="resource-card__label">构建任务</span>
              <span class="resource-card__value">
                {{ buildJobCount != null ? `${buildJobCount} 个` : "—" }}
              </span>
            </div>
          </div>

          <div class="resource-card">
            <div class="resource-card__icon is-pipeline">
              <u-icon :size="16"><Layers /></u-icon>
            </div>
            <div class="resource-card__info">
              <span class="resource-card__label">流水线</span>
              <span class="resource-card__value">
                {{ pipelineCount != null ? `${pipelineCount} 条` : "—" }}
              </span>
            </div>
          </div>

          <div class="resource-card">
            <div class="resource-card__icon is-req">
              <u-icon :size="16"><Books /></u-icon>
            </div>
            <div class="resource-card__info">
              <span class="resource-card__label">需求条目</span>
              <span class="resource-card__value">
                {{ requirementCount != null ? `${requirementCount} 项` : "—" }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- 项目元信息 -->
      <div class="project-detail-panel__section">
        <h4 class="section-title">基本信息</h4>
        <div class="meta-list">
          <div class="meta-item">
            <span class="meta-label">项目 ID</span>
            <span class="meta-value">#{{ project.id }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">标识 (Slug)</span>
            <span class="meta-value">{{ project.slug || "—" }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">创建时间</span>
            <span class="meta-value">{{ formatDateTime(project.created_at) || "—" }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">更新时间</span>
            <span class="meta-value">{{ formatDateTime(project.updated_at) || "—" }}</span>
          </div>
        </div>
      </div>

      <!-- 文档快捷查看 -->
      <div class="project-detail-panel__section">
        <h4 class="section-title">项目文档</h4>
        <div class="doc-actions">
          <u-button size="small" @click="viewDocs('api')">
            <template #icon>
              <u-icon :size="13"><Books /></u-icon>
            </template>
            查看接口文档
          </u-button>
          <u-button size="small" @click="viewDocs('dev')">
            <template #icon>
              <u-icon :size="13"><Books /></u-icon>
            </template>
            查看开发文档
          </u-button>
        </div>
      </div>
    </div>

    <!-- 3. 加载占位 -->
    <div v-else class="project-detail-panel__empty">
      <p>暂无项目数据</p>
    </div>
  </div>
</template>

<style scoped lang="scss">
@use "pkg:@veltra/styles/functions" as fn;

.project-detail-panel {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;

  &.is-loading {
    opacity: 0.7;
    pointer-events: none;
  }
}

.project-detail-panel__error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 32px 16px;
  color: fn.use-var(color, danger);
  text-align: center;
}

.project-detail-panel__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px 16px;
  color: fn.use-var(text-color, description);
  font-size: 13px;
}

.project-detail-panel__content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.project-detail-panel__card {
  padding: 14px;
  background: fn.use-var(bg-color, middle);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 60%, transparent);
  border-radius: fn.use-var(radius, medium);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.project-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.project-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;

  .project-icon {
    color: fn.use-var(color, primary);
    flex-shrink: 0;
  }

  .project-name {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: fn.use-var(text-color, title);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.open-classic-btn {
  font-size: 12px;
  flex-shrink: 0;
}

.project-tags-row {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 6px;
}

.project-slug-badge {
  code {
    font-size: 11px;
    padding: 2px 6px;
    border-radius: 4px;
    background: color-mix(in srgb, fn.use-var(bg-color, bottom) 80%, transparent);
    color: fn.use-var(text-color, secondary);
  }
}

.project-desc {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: fn.use-var(text-color, regular);
}

.project-label-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.project-detail-panel__section {
  display: flex;
  flex-direction: column;
  gap: 8px;

  .section-title {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    color: fn.use-var(text-color, secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
}

.resource-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

.resource-card {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px;
  border-radius: fn.use-var(radius, small);
  background: fn.use-var(bg-color, middle);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 40%, transparent);

  &__icon {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
    flex-shrink: 0;

    &.is-build {
      color: fn.use-var(color, primary);
      background: color-mix(in srgb, fn.use-var(color, primary) 12%, transparent);
    }
    &.is-pipeline {
      color: fn.use-var(color, success);
      background: color-mix(in srgb, fn.use-var(color, success) 12%, transparent);
    }
    &.is-req {
      color: fn.use-var(color, warning);
      background: color-mix(in srgb, fn.use-var(color, warning) 12%, transparent);
    }
  }

  &__info {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  &__label {
    font-size: 11px;
    color: fn.use-var(text-color, description);
  }

  &__value {
    font-size: 13px;
    font-weight: 600;
    color: fn.use-var(text-color, title);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.meta-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border-radius: fn.use-var(radius, small);
  background: fn.use-var(bg-color, middle);
  border: 1px solid color-mix(in srgb, fn.use-var(border, muted-color) 40%, transparent);
}

.meta-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;

  .meta-label {
    color: fn.use-var(text-color, secondary);
  }

  .meta-value {
    color: fn.use-var(text-color, main);
    font-family: ui-monospace, monospace;
  }
}

.doc-actions {
  display: flex;
  gap: 8px;
}
</style>
