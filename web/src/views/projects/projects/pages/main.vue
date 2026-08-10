<script setup lang="ts">
defineOptions({ name: "Projects" });

import { onMounted, reactive, ref, watch } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";
import { useRouter } from "vue-router";

import { archiveProject, createProject, deleteProject, updateProject } from "@/api/projects";
import { http } from "@/api/http";
import type { PageResult, ProductProject } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import { useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";

import MembersPanel from "../../components/members-panel.vue";
import ProjectCard from "../components/project-card.vue";

const router = useRouter();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();

const query = reactive({ keyword: "", status: "" });
const items = ref<ProductProject[]>([]);
const loading = ref(false);
const page = ref(1);
const pageSize = ref(12);
const total = ref(0);

const dialogOpen = ref(false);
const membersOpen = ref(false);
const membersProject = ref<ProductProject | null>(null);
const editing = ref<ProductProject | null>(null);
const form = reactive({
  name: "",
  slug: "",
  description: "",
  tags: "",
  is_public: false,
});

function cleanQuery(params: Record<string, unknown>): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "") continue;
    if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
      out[k] = v;
    }
  }
  return out;
}

async function load() {
  loading.value = true;
  try {
    const { body } = await http.get<PageResult<ProductProject>>("/projects", {
      query: cleanQuery({
        ...query,
        page: page.value,
        page_size: pageSize.value,
      }),
    });
    items.value = body?.items ?? [];
    total.value = body?.total ?? 0;
    if (typeof body?.page === "number") page.value = body.page;
    if (typeof body?.page_size === "number") pageSize.value = body.page_size;
  } catch (error) {
    message.error(error instanceof Error ? error.message : "加载失败");
    items.value = [];
    total.value = 0;
  } finally {
    loading.value = false;
  }
}

function search() {
  page.value = 1;
  return load();
}

function reload() {
  return load();
}

function onPageSizeChange(size: number) {
  if (typeof size === "number" && size > 0) {
    pageSize.value = size;
  }
  page.value = 1;
  void load();
}

function openCreate() {
  editing.value = null;
  o(form).extend({ name: "", slug: "", description: "", tags: "", is_public: false });
  dialogOpen.value = true;
}

function openEdit(project: ProductProject) {
  editing.value = project;
  o(form).extend(project);
  dialogOpen.value = true;
}

function openMembers(project: ProductProject) {
  membersProject.value = project;
  membersOpen.value = true;
}

async function save() {
  try {
    const input = { ...form };
    if (editing.value) {
      await updateProject(editing.value.id, input);
      message.success("项目已更新");
    } else {
      await createProject(input);
      message.success("项目已创建");
    }
    dialogOpen.value = false;
    await reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

const archive = bind(async (project: ProductProject) => {
  try {
    await archiveProject(project.id);
    message.success("项目已归档");
    await reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "归档失败");
  }
});

const remove = bind(async (project: ProductProject) => {
  try {
    await deleteProject(project.id);
    message.success("项目已解散");
    await reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "解散失败");
  }
});

function openProject(project: ProductProject) {
  void router.push({ name: "project-detail", params: { id: project.id } });
}

async function onOwnerTransferred() {
  await reload();
}

watch(
  () => query.status,
  () => {
    page.value = 1;
    void load();
  },
);

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="projects-page">
    <form class="projects-page__toolbar" @submit.prevent="search">
      <div class="projects-page__filters">
        <u-input v-model="query.keyword" placeholder="名称、标识或标签" style="width: 240px" />
        <u-select
          v-model="query.status"
          placeholder="全部状态"
          :options="[
            { label: '全部状态', value: '' },
            { label: '活跃', value: 'active' },
            { label: '已归档', value: 'archived' },
          ]"
          style="width: 130px"
        />
        <u-button type="primary" :loading="loading" @click="search">查询</u-button>
      </div>
      <div class="projects-page__actions">
        <u-button
          v-if="hasPermission('project_projects:create')"
          type="primary"
          @click.prevent="openCreate"
        >
          新建项目
        </u-button>
      </div>
    </form>

    <div v-loading="loading" class="projects-page__body">
      <div v-if="items.length" class="projects-page__grid">
        <ProjectCard
          v-for="project in items"
          :key="project.id"
          :project="project"
          :loading="busyKey === project.id"
          @enter="openProject(project)"
          @members="openMembers(project)"
          @edit="openEdit(project)"
          @archive="archive(project)"
          @remove="remove(project)"
        />
      </div>
      <div v-else-if="!loading" class="projects-page__empty">
        <u-empty text="暂无项目" />
      </div>
    </div>

    <div v-if="total > 0" class="projects-page__footer">
      <u-paginator
        v-model:page-number="page"
        v-model:page-size="pageSize"
        :page-size-options="[12, 24, 48]"
        :total="total"
        @change:page-number="load"
        @change:page-size="onPageSizeChange"
      />
    </div>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '编辑项目' : '新建项目'"
      :model="form"
      label-width="100px"
      style="width: 560px"
      @submit="save"
    >
      <template #prepend>
        <p class="slug-tip">标识创建后通常作为稳定引用，请使用字母、数字与连字符。</p>
      </template>
      <u-input label="名称" field="name" :rules="{ required: '必填' }" />
      <u-input
        label="标识"
        field="slug"
        placeholder="仅字母、数字、连字符，如 my-project"
        :pattern="/^[a-zA-Z0-9\- ]*$/"
        :rules="{ required: '必填' }"
      />
      <u-input label="标签" field="tags" placeholder="逗号分隔" />
      <u-switch
        label="公开"
        field="is_public"
        tips="兼容字段：项目读可见性已对全员放开，此开关不再影响能否查看"
      />
      <u-textarea label="描述" field="description" :rows="4" />
    </FormDialog>

    <u-dialog
      v-model="membersOpen"
      :title="membersProject ? `项目成员 · ${membersProject.name}` : '项目成员'"
      style="width: 800px"
    >
      <MembersPanel
        v-if="membersOpen && membersProject"
        :project="membersProject"
        height="420px"
        @owner-transferred="onOwnerTransferred"
      />
      <template #footer="{ close }">
        <u-button text @click="close()">关闭</u-button>
      </template>
    </u-dialog>
  </div>
</template>

<style scoped lang="scss">
@use "@/lib/empty-center.scss" as empty;

.projects-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
  height: 100%;
}

.projects-page__toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.projects-page__filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.projects-page__body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.projects-page__empty {
  @include empty.center(280px);
}

.projects-page__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
  align-items: stretch;
}

.projects-page__footer {
  display: flex;
  justify-content: flex-end;
}

.slug-tip {
  margin: 0 0 4px;
  font-size: 13px;
  color: var(--u-color-text-secondary, #666);
  line-height: 1.5;
}
</style>
