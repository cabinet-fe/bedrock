import type { ChatTool } from "@veltra/ai";
import { Books, Brain, Folder, GitBranch, Key, Server, VideoPlay } from "@veltra/icons/normal";

import { listAgents, listRuns, manualRunAgent } from "@/api/ai";
import {
  enqueueBuildRun,
  enqueuePipelineRun,
  getBuildRun,
  getPipelineRun,
  listBuildJobs,
  listBuildPipelines,
  listBuildRuns,
} from "@/api/cicd";
import {
  getDevDocNode,
  getDocNode,
  listDevDocTree,
  listDocTree,
  listProjects,
} from "@/api/projects";
import { listCredentials, listRepositories, listServers } from "@/api/resource";
import type { ProjectDocNode } from "@/api/types";
import { formatDateTime, formatDurationBetween } from "@/lib/datetime";

import BuildTriggerCard from "./cards/build-trigger-card.vue";
import { useAiChatStore } from "@/stores/ai-chat";

function formatTreeSummary(nodes: ProjectDocNode[], indent = ""): string {
  const lines: string[] = [];
  for (const n of nodes) {
    if (n.kind === "dir") {
      lines.push(`${indent}- 📁 **${n.name}** (目录)`);
      if (n.children?.length) {
        lines.push(formatTreeSummary(n.children, indent + "  "));
      }
    } else {
      lines.push(`${indent}- 📄 [ID: ${n.id}] ${n.name}`);
    }
  }
  return lines.join("\n");
}

export const aiChatTools: ChatTool[] = [
  // 1. 项目列表查询
  {
    name: "list_projects",
    label: "查询项目列表",
    icon: Folder,
    description:
      "查询平台上的项目列表。支持按关键字 keyword、状态 status (active/archived) 及分页查询。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "项目名称或描述搜索关键字" },
        status: { type: "string", enum: ["active", "archived"], description: "状态过滤" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: {
      keyword?: string;
      status?: "active" | "archived";
      page?: number;
      page_size?: number;
    }) => {
      const res = await listProjects({
        keyword: args.keyword,
        status: args.status,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的项目。";
      }

      const rows = res.items
        .map(
          (p) =>
            `| ${p.id} | [${p.name}](/project/projects/${p.id}) | ${p.slug} | ${p.status} | ${p.description || "—"} |`,
        )
        .join("\n");

      return `### 项目列表（共 ${res.total} 条）\n\n| ID | 项目名称 | 标识 (Slug) | 状态 | 描述 |\n| :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 2. 代码仓库列表查询
  {
    name: "list_repositories",
    label: "查询代码仓库",
    icon: GitBranch,
    description: "查询平台已配置的代码仓库列表。支持按关键字 keyword 过滤搜索。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "仓库名称或地址关键字" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: { keyword?: string; page?: number; page_size?: number }) => {
      const res = await listRepositories({
        keyword: args.keyword,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的代码仓库。";
      }

      const rows = res.items
        .map(
          (r) =>
            `| ${r.id} | ${r.name} | ${r.auth_type} | ${r.repo_url} | ${r.branches?.[0] || "—"} |`,
        )
        .join("\n");

      return `### 代码仓库列表（共 ${res.total} 条）\n\n| ID | 仓库名称 | 认证方式 | 仓库地址 | 分支预览 |\n| :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 3. 服务器列表查询
  {
    name: "list_servers",
    label: "查询服务器",
    icon: Server,
    description: "查询部署服务器主机列表。支持按关键字 keyword、标签 tag 过滤。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "服务器名称或主机 IP 关键字" },
        tag: { type: "string", description: "标签过滤" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: {
      keyword?: string;
      tag?: string;
      page?: number;
      page_size?: number;
    }) => {
      const res = await listServers({
        keyword: args.keyword,
        tag: args.tag,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的服务器。";
      }

      const rows = res.items
        .map((s) => `| ${s.id} | ${s.name} | ${s.host} | ${s.port} | ${s.os_type} | ${s.status} |`)
        .join("\n");

      return `### 服务器列表（共 ${res.total} 条）\n\n| ID | 服务器名称 | 主机 / IP | SSH 端口 | 系统 | 状态 |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 4. 凭证列表查询
  {
    name: "list_credentials",
    label: "查询凭证",
    icon: Key,
    description:
      "查询平台凭证列表（密钥、密码、访问 Token 等）。结果仅展示基本摘要信息，绝不透出敏感机密。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "凭证名称关键字" },
        type: { type: "string", description: "凭证类型，如 password, ssh_key, token" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: {
      keyword?: string;
      type?: string;
      page?: number;
      page_size?: number;
    }) => {
      const res = await listCredentials({
        keyword: args.keyword,
        type: args.type,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的凭证。";
      }

      const rows = res.items
        .map(
          (c) =>
            `| ${c.id} | ${c.name} | ${c.type} | ${c.description || "—"} | ${c.updated_at ? c.updated_at.slice(0, 19).replace("T", " ") : "—"} |`,
        )
        .join("\n");

      return `### 凭证列表（共 ${res.total} 条）\n\n| ID | 凭证名称 | 凭证类型 | 描述 | 更新时间 |\n| :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 5. CI/CD 构建任务列表查询
  {
    name: "list_build_jobs",
    label: "查询构建任务",
    icon: VideoPlay,
    description: "查询 CI/CD 构建任务定义列表。支持按关键字 keyword 过滤。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "构建任务名称关键字" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: { keyword?: string; page?: number; page_size?: number }) => {
      const res = await listBuildJobs({
        keyword: args.keyword,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的构建任务。";
      }

      const rows = res.items
        .map(
          (j) =>
            `| ${j.id} | [${j.name}](/cicd/build-jobs) | 仓库 #${j.repository_id} | ${j.branch || "—"} | ${j.enabled ? "已启用" : "已禁用"} |`,
        )
        .join("\n");

      return `### CI/CD 构建任务列表（共 ${res.total} 条）\n\n| ID | 任务名称 | 关联仓库 | 构建分支 | 状态 |\n| :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 6. CI/CD 流水线列表查询
  {
    name: "list_pipelines",
    label: "查询流水线",
    icon: VideoPlay,
    description: "查询 CI/CD 流水线列表。支持按关键字 keyword 过滤。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "流水线名称关键字" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: { keyword?: string; page?: number; page_size?: number }) => {
      const res = await listBuildPipelines({
        keyword: args.keyword,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的流水线。";
      }

      const rows = res.items
        .map(
          (p) =>
            `| ${p.id} | [${p.name}](/cicd/pipelines) | ${p.enabled ? "已启用" : "已禁用"} | ${p.description || "—"} |`,
        )
        .join("\n");

      return `### CI/CD 流水线列表（共 ${res.total} 条）\n\n| ID | 流水线名称 | 状态 | 描述 |\n| :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 7. CI/CD 构建运行记录列表查询
  {
    name: "list_build_runs",
    label: "查询构建记录",
    icon: VideoPlay,
    description: "查询最近的 CI/CD 构建运行历史记录。支持按任务 ID、流水线 ID、状态过滤。",
    parameters: {
      type: "object",
      properties: {
        job_id: { type: "integer", description: "按构建任务 ID 过滤" },
        pipeline_id: { type: "integer", description: "按流水线 ID 过滤" },
        status: {
          type: "string",
          enum: ["queued", "running", "success", "failed", "cancelled", "interrupted"],
          description: "运行状态过滤",
        },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: {
      job_id?: number;
      pipeline_id?: number;
      status?: string;
      page?: number;
      page_size?: number;
    }) => {
      const res = await listBuildRuns({
        job_id: args.job_id,
        pipeline_id: args.pipeline_id,
        status: args.status,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的构建运行记录。";
      }

      const rows = res.items
        .map(
          (r) =>
            `| ${r.id} | #${r.build_job_id} | ${r.branch || "—"} | ${r.commit_hash ? r.commit_hash.slice(0, 8) : "—"} | ${r.status} | ${r.stage || "—"} | ${r.trigger_type || "manual"} | [/cicd/build-runs/${r.id}](/cicd/build-runs/${r.id}) |`,
        )
        .join("\n");

      return `### 构建运行记录（共 ${res.total} 条）\n\n| 运行 ID | 任务 ID | 构建分支 | Commit | 状态 | 阶段 | 触发方式 | 详情链接 |\n| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 8. 触发 CI/CD 构建任务（敏感，需确认，会话内确认 + 触发后自动打开右侧面板）
  {
    name: "trigger_build_job",
    label: "触发构建任务",
    icon: VideoPlay,
    description:
      "触发指定的 CI/CD 构建任务运行。注意：这是敏感操作，将在平台发起真实代码拉取与构建，执行前必须由用户确认。",
    needsConfirm: true,
    autoCollapse: false,
    render: BuildTriggerCard,
    parameters: {
      type: "object",
      properties: {
        job_id: { type: "integer", description: "构建任务 ID" },
        branch: { type: "string", description: "可选构建分支，缺省使用任务配置分支" },
        variables: { type: "object", description: "可选构建环境变量键值对" },
      },
      required: ["job_id"],
    },
    execute: async (args: {
      job_id: number;
      branch?: string;
      variables?: Record<string, unknown>;
    }) => {
      const run = await enqueueBuildRun(args.job_id, {
        branch: args.branch,
        variables: args.variables,
      });

      const chatStore = useAiChatStore();
      chatStore.openRightPanel({
        type: "build",
        id: run.id,
        title: `构建运行 #${run.id} · 任务 #${run.build_job_id}`,
      });

      return {
        success: true,
        run_id: run.id,
        run_type: "build",
        job_id: run.build_job_id,
        status: run.status,
        branch: run.branch,
        link: `/cicd/build-runs/${run.id}`,
        message: `构建任务 #${run.build_job_id} 已成功触发运行！运行 ID: #${run.id}，当前状态: ${run.status}，详情链接: [/cicd/build-runs/${run.id}](/cicd/build-runs/${run.id})。已在右侧面板打开实时状态与日志。`,
      };
    },
  },

  // 9. 触发 CI/CD 流水线（敏感，需确认，会话内确认 + 触发后自动打开右侧面板）
  {
    name: "trigger_pipeline",
    label: "触发流水线",
    icon: VideoPlay,
    description:
      "触发指定的 CI/CD 流水线运行。注意：这是敏感操作，将在平台发起整条流水线执行，执行前必须由用户确认。",
    needsConfirm: true,
    autoCollapse: false,
    render: BuildTriggerCard,
    parameters: {
      type: "object",
      properties: {
        pipeline_id: { type: "integer", description: "流水线 ID" },
        variables: { type: "object", description: "可选流水线环境变量键值对" },
      },
      required: ["pipeline_id"],
    },
    execute: async (args: { pipeline_id: number; variables?: Record<string, unknown> }) => {
      const run = await enqueuePipelineRun(args.pipeline_id, {
        variables: args.variables,
      });

      const chatStore = useAiChatStore();
      chatStore.openRightPanel({
        type: "pipeline",
        id: run.id,
        title: `流水线运行 #${run.id} · 流水线 #${run.build_pipeline_id}`,
      });

      return {
        success: true,
        run_id: run.id,
        run_type: "pipeline",
        pipeline_id: run.build_pipeline_id,
        status: run.status,
        link: `/cicd/pipeline-runs/${run.id}`,
        message: `流水线 #${run.build_pipeline_id} 已成功触发运行！运行 ID: #${run.id}，当前状态: ${run.status}，详情链接: [/cicd/pipeline-runs/${run.id}](/cicd/pipeline-runs/${run.id})。已在右侧面板打开实时状态。`,
      };
    },
  },

  // 10. 查看构建/流水线详情与实时日志
  {
    name: "view_build_run",
    label: "查看运行详情",
    icon: VideoPlay,
    description: "在右侧侧边面板中查看指定的构建运行或流水线运行的实时状态、日志与基本信息。",
    autoCollapse: false,
    render: BuildTriggerCard,
    parameters: {
      type: "object",
      properties: {
        run_id: { type: "integer", description: "运行记录 ID" },
        run_type: {
          type: "string",
          enum: ["build", "pipeline"],
          description: "运行类型：build（构建，默认）或 pipeline（流水线）",
        },
      },
      required: ["run_id"],
    },
    execute: async (args: { run_id: number; run_type?: "build" | "pipeline" }) => {
      const type = args.run_type ?? "build";
      const chatStore = useAiChatStore();
      if (type === "pipeline") {
        const run = await getPipelineRun(args.run_id);
        chatStore.openRightPanel({
          type: "pipeline",
          id: run.id,
          title: `流水线运行 #${run.id}`,
        });
        return {
          success: true,
          run_id: run.id,
          run_type: "pipeline",
          status: run.status,
          link: `/cicd/pipeline-runs/${run.id}`,
          message: `流水线运行 #${run.id} 当前状态: ${run.status}，已在右侧面板展示详情。`,
        };
      }
      const run = await getBuildRun(args.run_id);
      chatStore.openRightPanel({
        type: "build",
        id: run.id,
        title: `构建运行 #${run.id}`,
      });
      return {
        success: true,
        run_id: run.id,
        run_type: "build",
        status: run.status,
        link: `/cicd/build-runs/${run.id}`,
        message: `构建运行 #${run.id} 当前状态: ${run.status}，阶段: ${run.stage || "—"}，已在右侧面板展示详情与实时日志。`,
      };
    },
  },

  // 11. AI 智能体列表查询
  {
    name: "list_ai_agents",
    label: "查询智能体",
    icon: Brain,
    description: "查询平台上的 AI 智能体定义列表。支持按关键字 keyword 过滤搜索。",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "智能体名称关键字" },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页数量，默认 10" },
      },
    },
    execute: async (args: { keyword?: string; page?: number; page_size?: number }) => {
      const res = await listAgents({
        keyword: args.keyword,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的智能体。";
      }

      const rows = res.items
        .map(
          (a) =>
            `| ${a.id} | [${a.name}](/ai/agents) | ${a.cli_key} | ${a.description || "—"} | ${a.enabled ? "已启用" : "已禁用"} |`,
        )
        .join("\n");

      return `### AI 智能体列表（共 ${res.total} 个）\n\n| ID | 智能体名称 | CLI Key | 描述 | 启用状态 |\n| :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 12. AI 智能体运行记录查询
  {
    name: "list_agent_runs",
    label: "查询智能体运行",
    icon: Brain,
    description: "查询 AI 智能体的运行历史记录。支持按智能体 ID、状态过滤。",
    parameters: {
      type: "object",
      properties: {
        agent_id: { type: "integer", description: "智能体 ID" },
        status: {
          type: "string",
          enum: ["queued", "running", "success", "failed", "cancelled"],
          description: "状态过滤",
        },
        page: { type: "integer", description: "页码，默认 1" },
        page_size: { type: "integer", description: "每页条数，默认 10" },
      },
    },
    execute: async (args: {
      agent_id?: number;
      status?: string;
      page?: number;
      page_size?: number;
    }) => {
      const res = await listRuns({
        agent_id: args.agent_id,
        status: args.status,
        page: args.page ?? 1,
        page_size: args.page_size ?? 10,
      });

      if (!res.items || res.items.length === 0) {
        return "未找到符合条件的智能体运行记录。";
      }

      const rows = res.items
        .map(
          (r) =>
            `| ${r.id} | #${r.agent_id} | ${r.status} | ${formatDurationBetween(r.started_at, r.finished_at)} | ${formatDateTime(r.created_at)} | [/ai/runs/${r.id}](/ai/runs/${r.id}) |`,
        )
        .join("\n");

      return `### AI 智能体运行记录（共 ${res.total} 条）\n\n| 运行 ID | 智能体 ID | 状态 | 耗时 | 触发时间 | 详情链接 |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n${rows}`;
    },
  },

  // 13. 手动触发 AI 智能体（敏感，需确认）
  {
    name: "trigger_ai_agent",
    label: "触发智能体",
    icon: Brain,
    description:
      "手动触发指定的 AI 智能体运行入队。注意：该操作会唤醒智能体执行任务，执行前必须由用户确认。",
    needsConfirm: true,
    parameters: {
      type: "object",
      properties: {
        agent_id: { type: "integer", description: "智能体 ID" },
        user_prompt: { type: "string", description: "输入给智能体的初始提示词或指令" },
      },
      required: ["agent_id"],
    },
    execute: async (args: { agent_id: number; user_prompt?: string }) => {
      const run = await manualRunAgent(args.agent_id, {
        user_prompt: args.user_prompt,
      });

      return {
        success: true,
        run_id: run.id,
        agent_id: run.agent_id,
        status: run.status,
        link: `/ai/runs/${run.id}`,
        message: `AI 智能体 #${run.agent_id} 运行任务已成功入队！运行 ID: #${run.id}，当前状态: ${run.status}，详情链接: [/ai/runs/${run.id}](/ai/runs/${run.id})`,
      };
    },
  },

  // 14. 查看项目开发/接口文档
  {
    name: "view_project_doc",
    label: "查看项目文档",
    icon: Books,
    description:
      "查看指定项目的接口文档或开发文档树及选中文档的 Markdown 正文。在右侧侧边面板中渲染完整文档树与正文。",
    autoCollapse: false,
    parameters: {
      type: "object",
      properties: {
        project_id: { type: "integer", description: "项目 ID" },
        doc_type: {
          type: "string",
          enum: ["api", "dev"],
          description: "文档类型：api（接口文档，默认）或 dev（开发文档）",
        },
        node_id: { type: "integer", description: "可选的具体文档节点 ID" },
      },
      required: ["project_id"],
    },
    execute: async (args: { project_id: number; doc_type?: "api" | "dev"; node_id?: number }) => {
      const kind = args.doc_type === "dev" ? "dev" : "api";
      const tree =
        kind === "dev" ? await listDevDocTree(args.project_id) : await listDocTree(args.project_id);

      let nodeContent = "";
      let nodeName = "";
      if (args.node_id) {
        try {
          const detail =
            kind === "dev"
              ? await getDevDocNode(args.project_id, args.node_id)
              : await getDocNode(args.project_id, args.node_id);
          nodeContent = detail.content || "";
          nodeName = detail.name;
        } catch {
          // ignore
        }
      }

      const chatStore = useAiChatStore();
      chatStore.openRightPanel({
        type: "doc",
        id: args.node_id || 0,
        projectId: args.project_id,
        docType: kind,
        title: `${kind === "dev" ? "开发文档" : "接口文档"} · 项目 #${args.project_id}`,
      });

      const summaryTree = formatTreeSummary(tree);
      const link =
        kind === "dev"
          ? `/project/projects/${args.project_id}/dev-docs`
          : `/project/projects/${args.project_id}/docs`;

      return {
        project_id: args.project_id,
        doc_type: kind,
        node_id: args.node_id,
        node_name: nodeName,
        link,
        message: nodeName
          ? `已加载项目 #${args.project_id} 的文档「${nodeName}」，并在右侧侧边面板展示。正文摘要如下：\n\n${nodeContent.slice(0, 300)}...`
          : `已获取项目 #${args.project_id} 的${kind === "dev" ? "开发" : "接口"}文档树并在右侧面板展示。\n\n文档树目录：\n${summaryTree}`,
      };
    },
  },
];
