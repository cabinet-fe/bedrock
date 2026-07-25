<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from "vue";
import { o } from "@cat-kit/core";
import { message } from "@veltra/desktop";

import {
  addProjectMember,
  listUserOptions,
  removeProjectMember,
  transferProjectOwner,
  updateProjectMember,
} from "@/api/projects";
import type { ProductProject, ProjectMember, ProjectRole, UserOption } from "@/api/types";
import FormDialog from "@/components/form-dialog";
import ProTable, { defineProTableColumns } from "@/components/pro-table";
import { useBusy, useBusyKey } from "@/composables/use-busy";
import { usePermission } from "@/composables/use-permission";
import { formatDateTime } from "@/lib/datetime";
import { tagType, type TagType } from "@/lib/tag";
import { useAuthStore } from "@/stores/auth";

const ROLE_TAG: Record<string, TagType> = {
  owner: "primary",
  admin: "info",
  member: undefined,
  readonly: "warning",
};

const ROLE_LABEL: Record<string, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  readonly: "Readonly",
};

const props = defineProps<{
  project: ProductProject;
  /** ProTable 高度；弹窗内建议固定高度 */
  height?: string;
}>();
const emit = defineEmits<{ ownerTransferred: [] }>();

const auth = useAuthStore();
const { hasPermission } = usePermission();
const { busyKey, bind } = useBusyKey();
const { busy: addBusy, run: runAdd } = useBusy();
const tableRef = useTemplateRef("table");
const members = ref<ProjectMember[]>([]);
const users = ref<UserOption[]>([]);
const dialogOpen = ref(false);
const editing = ref<ProjectMember | null>(null);
const form = reactive({
  user_id: undefined as number | undefined,
  role: "member" as Exclude<ProjectRole, "owner">,
});

const selfMember = computed(() => members.value.find((member) => member.user_id === auth.user?.id));
const canManageAll = computed(() => hasPermission("project_projects:manage_all"));
const canManageMembers = computed(
  () =>
    hasPermission("project_projects:update") &&
    (canManageAll.value ||
      selfMember.value?.role === "owner" ||
      selfMember.value?.role === "admin"),
);
const canTransferOwner = computed(
  () =>
    hasPermission("project_projects:update") &&
    (canManageAll.value || selfMember.value?.role === "owner"),
);

const memberUserIDs = computed(() => new Set(members.value.map((m) => m.user_id)));
const userOptions = computed(() =>
  users.value
    .filter((u) => !memberUserIDs.value.has(u.id))
    .map((u) => ({
      label: u.display_name ? `${u.display_name} (${u.username})` : u.username,
      value: u.id,
    })),
);

const columns = defineProTableColumns([
  { key: "username", name: "用户" },
  { key: "role", name: "项目角色", width: 140, align: "center" },
  {
    key: "created_at",
    name: "加入时间",
    width: 170,
    align: "center",
    render: ({ val }) => formatDateTime(val),
  },
  { key: "action", name: "操作", width: 320, align: "center", fixed: "right" },
]);

function memberLabel(member: ProjectMember) {
  if (member.display_name && member.username) {
    return `${member.display_name} (${member.username})`;
  }
  return member.display_name || member.username || `#${member.user_id}`;
}

function onLoaded(items: ProjectMember[]) {
  members.value = items;
}

async function openAdd() {
  editing.value = null;
  await runAdd(async () => {
    try {
      users.value = await listUserOptions();
      dialogOpen.value = true;
    } catch (error) {
      message.error(error instanceof Error ? error.message : "加载用户列表失败");
    }
  });
}

function openEdit(member: ProjectMember) {
  editing.value = member;
  o(form).extend(member);
  form.role = member.role as Exclude<ProjectRole, "owner">;
  dialogOpen.value = true;
}

async function refresh() {
  await tableRef.value?.reload();
}

async function save() {
  try {
    if (editing.value) {
      await updateProjectMember(props.project.id, editing.value.user_id, form.role);
      message.success("角色已更新");
    } else {
      if (!form.user_id) {
        message.error("请选择用户");
        return;
      }
      await addProjectMember(props.project.id, form.user_id, form.role);
      message.success("成员已添加");
    }
    dialogOpen.value = false;
    await refresh();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "保存失败");
  }
}

const remove = bind(async (member: ProjectMember) => {
  try {
    await removeProjectMember(props.project.id, member.user_id);
    message.success("成员已移除");
    await refresh();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "移除失败");
  }
});

const transfer = bind(async (member: ProjectMember) => {
  try {
    await transferProjectOwner(props.project.id, member.user_id);
    message.success("Owner 已转让");
    emit("ownerTransferred");
    await tableRef.value?.reload();
  } catch (error) {
    message.error(error instanceof Error ? error.message : "转让失败");
  }
});
</script>

<template>
  <section class="panel">
    <ProTable
      ref="table"
      :url="`/projects/${project.id}/members`"
      :columns="columns"
      :height="height"
      @loaded="onLoaded"
    >
      <template #filters>
        <u-button
          v-if="canManageMembers"
          type="primary"
          :loading="addBusy"
          style="margin-left: auto"
          @click.prevent="openAdd"
        >
          添加成员
        </u-button>
      </template>
      <template #column:username="{ rowData }">
        {{ memberLabel(rowData as ProjectMember) }}
      </template>
      <template #column:role="{ rowData }">
        <u-tag size="small" :type="tagType((rowData as ProjectMember).role, ROLE_TAG)">
          {{ ROLE_LABEL[(rowData as ProjectMember).role] ?? (rowData as ProjectMember).role }}
        </u-tag>
      </template>
      <template #column:action="{ rowData }">
        <u-action-group :max="3" :loading="busyKey === (rowData as ProjectMember).id">
          <u-action
            v-if="canManageMembers && (rowData as ProjectMember).role !== 'owner'"
            @run="openEdit(rowData as ProjectMember)"
          >
            修改角色
          </u-action>
          <u-action
            v-if="canTransferOwner && (rowData as ProjectMember).role !== 'owner'"
            need-confirm
            @run="transfer(rowData as ProjectMember)"
          >
            转让 Owner
          </u-action>
          <u-action
            v-if="canManageMembers && (rowData as ProjectMember).role !== 'owner'"
            need-confirm
            type="danger"
            @run="remove(rowData as ProjectMember)"
          >
            移除
          </u-action>
        </u-action-group>
      </template>
    </ProTable>

    <FormDialog
      v-model="dialogOpen"
      :title="editing ? '修改成员角色' : '添加成员'"
      :model="form"
      label-width="100px"
      style="width: 420px"
      @submit="save"
    >
      <u-select
        v-if="!editing"
        label="用户"
        field="user_id"
        :options="userOptions"
        filterable
        placeholder="请选择用户"
        :rules="{ required: '必填' }"
      />
      <u-select
        label="角色"
        field="role"
        :options="[
          { label: '项目管理员', value: 'admin' },
          { label: '成员', value: 'member' },
          { label: '只读', value: 'readonly' },
        ]"
      />
    </FormDialog>
  </section>
</template>

<style scoped>
.panel {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  gap: 16px;
}
</style>
