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
import QueryTableCard from "./cards/query-table-card.vue";
import { useAiChatStore } from "@/stores/ai-chat";

const TABLE_CARD_HINT =
  "数据已在前端表格卡片中直观呈现。模型在文本回复中严禁重复输出 Markdown 表格或逐项罗列数据，仅做总体说明或给出后续指引。";

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
      "查询平台上的项目列表。支持按关键字 keyword、状态 status (active/archived) 及分页查询。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "项目名称", minWidth: 140, type: "link" as const, linkKey: "link" },
        { key: "slug", name: "标识 (Slug)", minWidth: 120 },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
        { key: "description", name: "描述", minWidth: 160 },
      ];

      const items = (res.items || []).map((p) => ({
        id: p.id,
        name: p.name,
        slug: p.slug,
        status: p.status,
        description: p.description || "—",
        link: `/project/projects/${p.id}`,
      }));

      return JSON.stringify({
        title: "项目列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 2. 代码仓库列表查询
  {
    name: "list_repositories",
    label: "查询代码仓库",
    icon: GitBranch,
    description:
      "查询平台已配置的代码仓库列表。支持按关键字 keyword 过滤搜索。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "仓库名称", minWidth: 140, type: "link" as const, linkKey: "link" },
        { key: "auth_type", name: "认证方式", width: 100, type: "tag" as const },
        { key: "repo_url", name: "仓库地址", minWidth: 200 },
        { key: "branch", name: "分支预览", minWidth: 100 },
      ];

      const items = (res.items || []).map((r) => ({
        id: r.id,
        name: r.name,
        auth_type: r.auth_type,
        repo_url: r.repo_url,
        branch: r.branches?.[0] || "—",
        link: "/resource/repositories",
      }));

      return JSON.stringify({
        title: "代码仓库列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 3. 服务器列表查询
  {
    name: "list_servers",
    label: "查询服务器",
    icon: Server,
    description:
      "查询部署服务器主机列表。支持按关键字 keyword、标签 tag 过滤。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "服务器名称", minWidth: 140, type: "link" as const, linkKey: "link" },
        { key: "host", name: "主机 / IP", minWidth: 130 },
        { key: "port", name: "SSH 端口", width: 90, align: "center" as const },
        { key: "os_type", name: "系统", width: 100 },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
      ];

      const items = (res.items || []).map((s) => ({
        id: s.id,
        name: s.name,
        host: s.host,
        port: s.port,
        os_type: s.os_type || "linux",
        status: s.status,
        link: "/resource/servers",
      }));

      return JSON.stringify({
        title: "服务器列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 4. 凭证列表查询
  {
    name: "list_credentials",
    label: "查询凭证",
    icon: Key,
    description:
      "查询平台凭证列表（密钥、密码、访问 Token 等）。结果仅展示基本摘要信息，绝不透出敏感机密。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "凭证名称", minWidth: 140, type: "link" as const, linkKey: "link" },
        { key: "type", name: "凭证类型", width: 110, type: "tag" as const },
        { key: "description", name: "描述", minWidth: 160 },
        { key: "updated_at", name: "更新时间", width: 170 },
      ];

      const items = (res.items || []).map((c) => ({
        id: c.id,
        name: c.name,
        type: c.type,
        description: c.description || "—",
        updated_at: c.updated_at ? c.updated_at.slice(0, 19).replace("T", " ") : "—",
        link: "/resource/credentials",
      }));

      return JSON.stringify({
        title: "凭证列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 5. CI/CD 构建任务列表查询
  {
    name: "list_build_jobs",
    label: "查询构建任务",
    icon: VideoPlay,
    description:
      "查询 CI/CD 构建任务定义列表。支持按关键字 keyword 过滤。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "任务名称", minWidth: 150, type: "link" as const, linkKey: "link" },
        { key: "repository_name", name: "关联仓库", minWidth: 120 },
        { key: "branch", name: "构建分支", minWidth: 100 },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
      ];

      const items = (res.items || []).map((j) => ({
        id: j.id,
        name: j.name,
        repository_name: `仓库 #${j.repository_id}`,
        branch: j.branch || "—",
        status: j.enabled ? "已启用" : "已禁用",
        link: "/cicd/build-jobs",
      }));

      return JSON.stringify({
        title: "CI/CD 构建任务列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 6. CI/CD 流水线列表查询
  {
    name: "list_pipelines",
    label: "查询流水线",
    icon: VideoPlay,
    description:
      "查询 CI/CD 流水线列表。支持按关键字 keyword 过滤。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "流水线名称", minWidth: 150, type: "link" as const, linkKey: "link" },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
        { key: "description", name: "描述", minWidth: 160 },
      ];

      const items = (res.items || []).map((p) => ({
        id: p.id,
        name: p.name,
        status: p.enabled ? "已启用" : "已禁用",
        description: p.description || "—",
        link: "/cicd/pipelines",
      }));

      return JSON.stringify({
        title: "CI/CD 流水线列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 7. CI/CD 构建运行记录列表查询
  {
    name: "list_build_runs",
    label: "查询构建记录",
    icon: VideoPlay,
    description:
      "查询最近的 CI/CD 构建运行历史记录。支持按任务 ID、流水线 ID、状态过滤。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        {
          key: "id",
          name: "运行 ID",
          width: 90,
          align: "center" as const,
          type: "link" as const,
          linkKey: "link",
        },
        { key: "build_job_id", name: "任务 ID", width: 90, align: "center" as const },
        { key: "branch", name: "构建分支", minWidth: 100 },
        { key: "commit_hash", name: "Commit", width: 90 },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
        { key: "stage", name: "阶段", width: 90, type: "tag" as const },
        { key: "trigger_type", name: "触发方式", width: 90, type: "tag" as const },
        {
          key: "action",
          name: "详情链接",
          width: 100,
          align: "center" as const,
          type: "link" as const,
          linkKey: "link",
        },
      ];

      const items = (res.items || []).map((r) => ({
        id: r.id,
        build_job_id: `#${r.build_job_id}`,
        branch: r.branch || "—",
        commit_hash: r.commit_hash ? r.commit_hash.slice(0, 8) : "—",
        status: r.status,
        stage: r.stage || "—",
        trigger_type: r.trigger_type || "manual",
        action: "查看详情",
        link: `/cicd/build-runs/${r.id}`,
      }));

      return JSON.stringify({
        title: "构建运行记录",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
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
    description:
      "查询平台上的 AI 智能体定义列表。支持按关键字 keyword 过滤搜索。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        { key: "id", name: "ID", width: 70, align: "center" as const },
        { key: "name", name: "智能体名称", minWidth: 140, type: "link" as const, linkKey: "link" },
        { key: "description", name: "描述", minWidth: 160 },
        { key: "status", name: "启用状态", width: 90, type: "tag" as const },
      ];

      const items = (res.items || []).map((a) => ({
        id: a.id,
        name: a.name,
        description: a.description || "—",
        status: a.enabled ? "已启用" : "已禁用",
        link: "/ai/agents",
      }));

      return JSON.stringify({
        title: "AI 智能体列表",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
    },
  },

  // 12. AI 智能体运行记录查询
  {
    name: "list_agent_runs",
    label: "查询智能体运行",
    icon: Brain,
    description:
      "查询 AI 智能体的运行历史记录。支持按智能体 ID、状态过滤。注意：结果已由前端卡片直接以表格形式呈现，模型回复时严禁重复输出 Markdown 表格。",
    render: QueryTableCard,
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

      const columns = [
        {
          key: "id",
          name: "运行 ID",
          width: 90,
          align: "center" as const,
          type: "link" as const,
          linkKey: "link",
        },
        { key: "agent_id", name: "智能体 ID", width: 90, align: "center" as const },
        { key: "status", name: "状态", width: 90, type: "tag" as const },
        { key: "duration", name: "耗时", width: 100, align: "center" as const },
        { key: "created_at", name: "触发时间", width: 170 },
        {
          key: "action",
          name: "详情链接",
          width: 100,
          align: "center" as const,
          type: "link" as const,
          linkKey: "link",
        },
      ];

      const items = (res.items || []).map((r) => ({
        id: r.id,
        agent_id: `#${r.agent_id}`,
        status: r.status,
        duration: formatDurationBetween(r.started_at, r.finished_at) || "—",
        created_at: formatDateTime(r.created_at) || "—",
        action: "查看详情",
        link: `/ai/runs/${r.id}`,
      }));

      return JSON.stringify({
        title: "AI 智能体运行记录",
        total: res.total,
        columns,
        items,
        _hint: TABLE_CARD_HINT,
      });
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
