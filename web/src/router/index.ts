import { createRouter, createWebHistory } from "vue-router";

import { getAccessToken } from "@/api/http";
import { useAuthStore } from "@/stores/auth";

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: "/login",
      name: "login",
      component: () => import("@/pages/login/login.vue"),
      meta: { public: true },
    },
    {
      path: "/",
      component: () => import("@/pages/layout.vue"),
      children: [
        {
          path: "",
          name: "home",
          component: () => import("@/pages/home.vue"),
          meta: { title: "首页", keepAliveName: "HomePage" },
        },
        {
          path: "handbook",
          name: "handbook",
          component: () => import("@/views/help/handbook/pages/main.vue"),
          meta: {
            permission: "handbook:view",
            title: "操作手册",
            keepAliveName: "HelpHandbook",
          },
        },
        {
          path: "system/users",
          name: "system-users",
          component: () => import("@/views/system/users/pages/main.vue"),
          meta: {
            permission: "system_users:view",
            title: "用户管理",
            keepAliveName: "SystemUsers",
          },
        },
        {
          path: "system/roles",
          name: "system-roles",
          component: () => import("@/views/system/roles/pages/main.vue"),
          meta: {
            permission: "system_roles:view",
            title: "角色管理",
            keepAliveName: "SystemRoles",
          },
        },
        {
          path: "system/resources",
          name: "system-resources",
          component: () => import("@/views/system/resources/pages/main.vue"),
          meta: {
            permission: "system_resources:view",
            title: "权限资源",
            keepAliveName: "SystemResources",
          },
        },
        {
          path: "system/dictionaries",
          name: "system-dictionaries",
          component: () => import("@/views/system/dictionaries/pages/main.vue"),
          meta: {
            permission: "system_dictionaries:view",
            title: "数据字典",
            keepAliveName: "SystemDictionaries",
          },
        },
        {
          path: "system/operation-logs",
          name: "system-operation-logs",
          component: () => import("@/views/system/operation-logs/pages/main.vue"),
          meta: {
            permission: "system_operation_logs:view",
            title: "操作日志",
            keepAliveName: "SystemOperationLogs",
          },
        },
        {
          path: "resource/repositories",
          name: "resource-repositories",
          component: () => import("@/views/resource/repositories/pages/main.vue"),
          meta: {
            permission: "resource_repositories:view",
            title: "代码仓库",
            keepAliveName: "ResourceRepositories",
          },
        },
        {
          path: "resource/servers",
          name: "resource-servers",
          component: () => import("@/views/resource/servers/pages/main.vue"),
          meta: {
            permission: "resource_servers:view",
            title: "服务器",
            keepAliveName: "ResourceServers",
          },
        },
        {
          path: "resource/credentials",
          name: "resource-credentials",
          component: () => import("@/views/resource/credentials/pages/main.vue"),
          meta: {
            permission: "resource_credentials:view",
            title: "凭证管理",
            keepAliveName: "ResourceCredentials",
          },
        },
        {
          path: "resource/tokens",
          name: "resource-tokens",
          component: () => import("@/views/resource/tokens/pages/main.vue"),
          meta: {
            permission: "resource_tokens:view",
            title: "访问令牌",
            keepAliveName: "ResourceTokens",
          },
        },
        {
          path: "cicd/build-jobs",
          name: "cicd-build-jobs",
          component: () => import("@/views/cicd/build-jobs/pages/main.vue"),
          meta: {
            permission: "cicd_build_jobs:view",
            title: "构建任务",
            keepAliveName: "CicdBuildJobs",
          },
        },
        {
          path: "cicd/build-runs",
          name: "cicd-build-runs",
          component: () => import("@/views/cicd/build-runs/pages/main.vue"),
          meta: {
            permission: "cicd_build_runs:view",
            title: "构建记录",
            keepAliveName: "CicdBuildRuns",
          },
        },
        {
          path: "cicd/build-runs/:id",
          name: "cicd-build-run-detail",
          component: () => import("@/views/cicd/build-runs/pages/detail.vue"),
          meta: {
            permission: "cicd_build_runs:view",
            title: "构建详情",
            keepAliveName: "CicdBuildRunDetail",
          },
        },
        {
          path: "project",
          redirect: "/project/projects",
        },
        {
          path: "project/projects",
          name: "projects",
          component: () => import("@/views/projects/projects/pages/main.vue"),
          meta: {
            permission: "project_projects:view",
            title: "项目列表",
            keepAliveName: "Projects",
          },
        },
        {
          path: "project/projects/:id",
          name: "project-detail",
          component: () => import("@/views/projects/projects/pages/detail.vue"),
          meta: {
            permission: "project_projects:view",
            title: "项目详情",
            keepAliveName: "ProjectDetail",
          },
        },
        {
          // Requirements/docs are scoped to a project; menu entries pick a project first.
          path: "project/requirements",
          name: "project-requirements",
          component: () => import("@/views/projects/requirements/pages/main.vue"),
          meta: {
            permission: "project_requirements:view",
            projectTab: "requirements",
            title: "需求",
            keepAliveName: "ProjectRequirements",
          },
        },
        {
          path: "project/docs",
          name: "project-docs",
          component: () => import("@/views/projects/docs/pages/main.vue"),
          meta: {
            permission: "project_docs:view",
            projectTab: "docs",
            title: "接口文档",
            keepAliveName: "ProjectDocs",
          },
        },
        {
          path: "projects",
          redirect: "/project/projects",
        },
        {
          path: "projects/:id",
          redirect: (to) => `/project/projects/${String(to.params.id)}`,
        },
        {
          path: "cicd/:rest(.*)*",
          name: "cicd-placeholder",
          component: () => import("@/pages/placeholder.vue"),
          meta: { title: "CI/CD", keepAliveName: "PlaceholderPage" },
        },
        {
          path: "ops/processes",
          name: "ops-processes",
          component: () => import("@/views/ops/processes/pages/main.vue"),
          meta: {
            permission: "ops_processes:view",
            title: "进程管理",
            keepAliveName: "OpsProcesses",
          },
        },
        {
          path: "ops/dev-environments",
          name: "ops-dev-environments",
          component: () => import("@/views/ops/dev-environments/pages/main.vue"),
          meta: {
            permission: "ops_dev_environments:view",
            title: "开发环境",
            keepAliveName: "OpsDevEnvironments",
          },
        },
        {
          path: "ai/agents",
          name: "ai-agents",
          component: () => import("@/views/ai/agents/pages/main.vue"),
          meta: { permission: "ai_agents:view", title: "Agents", keepAliveName: "AiAgents" },
        },
        {
          path: "ai/runs",
          name: "ai-runs",
          component: () => import("@/views/ai/runs/pages/main.vue"),
          meta: { permission: "ai_runs:view", title: "运行记录", keepAliveName: "AiRuns" },
        },
        {
          path: "ai/runs/:id",
          name: "ai-run-detail",
          component: () => import("@/views/ai/runs/pages/detail.vue"),
          meta: {
            permission: "ai_runs:view",
            title: "运行详情",
            keepAliveName: "AiRunDetail",
          },
        },
        {
          path: "ai/skills",
          name: "ai-skills",
          component: () => import("@/views/ai/skills/pages/main.vue"),
          meta: { permission: "ai_skills:view", title: "Skills", keepAliveName: "AiSkills" },
        },
        {
          path: "ai/skills/:id",
          name: "ai-skill-detail",
          component: () => import("@/views/ai/skills/pages/detail.vue"),
          meta: {
            permission: "ai_skills:view",
            title: "技能详情",
            keepAliveName: "AiSkillDetail",
          },
        },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  const isPublic = to.meta.public === true;
  const hasToken = !!getAccessToken();

  if (!isPublic && !hasToken) {
    return { name: "login", query: { redirect: to.fullPath } };
  }
  if (to.name === "login" && hasToken) {
    return { name: "home" };
  }

  if (!isPublic && hasToken) {
    const auth = useAuthStore();
    await auth.refreshMe();
    if (!auth.user) {
      return { name: "login", query: { redirect: to.fullPath } };
    }
    const needed = typeof to.meta.permission === "string" ? to.meta.permission : "";
    if (needed && !auth.hasPermission(needed)) {
      return { name: "home" };
    }
  }
  return true;
});

export default router;
