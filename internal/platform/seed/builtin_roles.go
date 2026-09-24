package seed

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/rbac/model"
)

type builtinRoleSeed struct {
	Code        string
	Name        string
	Description string
	Permissions []string
}

func crud(menu string) []string {
	return []string{menu + ":view", menu + ":create", menu + ":update", menu + ":delete"}
}

func withActions(menu string, actions ...string) []string {
	out := []string{menu + ":view"}
	for _, a := range actions {
		if a != "view" {
			out = append(out, menu+":"+a)
		}
	}
	return out
}

// builtinRoleSeeds is the default permission matrix. Data scope is self for
// every role; collaboration modules (projects / issues) still surface data
// where the user is creator, member, assignee or watcher.
var builtinRoleSeeds = []builtinRoleSeed{
	{
		Code:        model.RoleCodeDeveloper,
		Name:        "开发",
		Description: "内置开发角色：项目与文档全量、CI/CD 任务编排执行、AI 编排，资源只读为主",
		Permissions: concat(
			[]string{
				"dashboard:view", "dashboard:build_summary", "dashboard:agent_run_summary",
				"dashboard:script_run_summary", "dashboard:pipeline_run_summary",
				"dashboard:cicd_task_overview", "dashboard:my_projects",
				"handbook:view",
			},
			withActions("resource_repositories", "create", "update"),
			withActions("resource_servers"),
			withActions("resource_credentials", "use"),
			crud("resource_tokens"),
			withActions("cicd_build_jobs", "create", "update", "delete", "execute"),
			withActions("cicd_build_runs"),
			withActions("cicd_script_jobs", "create", "update", "delete", "execute"),
			withActions("cicd_script_runs"),
			withActions("cicd_pipelines", "create", "update", "delete", "execute"),
			withActions("cicd_pipeline_runs"),
			withActions("project_projects", "create", "update"),
			crud("project_bugs"),
			crud("project_requirements"),
			withActions("project_docs", "create", "update", "delete", "execute"),
			crud("project_dev_docs"),
			withActions("ai_agents", "create", "update", "delete", "execute"),
			withActions("ai_runs"),
			withActions("harness_chat", "send", "approve"),
			withActions("ai_skills", "create", "update", "delete", "download"),
			withActions("ai_providers"),
		),
	},
	{
		Code:        model.RoleCodeTester,
		Name:        "测试",
		Description: "内置测试角色：缺陷全量、需求协同、验证类执行（构建/脚本触发）、AI 与技能使用",
		Permissions: concat(
			[]string{
				"dashboard:view", "dashboard:build_summary", "dashboard:agent_run_summary",
				"dashboard:script_run_summary", "dashboard:pipeline_run_summary",
				"dashboard:cicd_task_overview", "dashboard:my_projects",
				"handbook:view",
			},
			withActions("resource_repositories"),
			withActions("resource_servers"),
			crud("resource_tokens"),
			withActions("cicd_build_jobs", "execute"),
			withActions("cicd_build_runs"),
			withActions("cicd_script_jobs", "execute"),
			withActions("cicd_script_runs"),
			withActions("cicd_pipelines"),
			withActions("cicd_pipeline_runs"),
			withActions("project_projects"),
			crud("project_bugs"),
			withActions("project_requirements", "create", "update"),
			withActions("project_docs"),
			withActions("project_dev_docs"),
			withActions("ai_agents", "execute"),
			withActions("ai_runs"),
			withActions("ai_skills", "download"),
		),
	},
	{
		Code:        model.RoleCodeOps,
		Name:        "运维",
		Description: "内置运维角色：进程与开发环境、服务器与凭证管理、CI/CD 全量、备份与操作日志",
		Permissions: concat(
			[]string{
				"dashboard:view", "dashboard:build_summary",
				"dashboard:script_run_summary", "dashboard:pipeline_run_summary",
				"dashboard:cicd_task_overview", "dashboard:my_projects",
				"handbook:view",
			},
			withActions("ops_processes", "execute"),
			withActions("ops_dev_environments", "create", "update", "delete", "execute"),
			crud("resource_servers"),
			withActions("resource_credentials", "create", "update", "delete", "use"),
			withActions("resource_repositories"),
			crud("resource_tokens"),
			withActions("cicd_build_jobs", "create", "update", "delete", "execute"),
			withActions("cicd_build_runs"),
			withActions("cicd_script_jobs", "create", "update", "delete", "execute"),
			withActions("cicd_script_runs"),
			withActions("cicd_pipelines", "create", "update", "delete", "execute"),
			withActions("cicd_pipeline_runs"),
			withActions("project_projects"),
			withActions("project_bugs", "create"),
			withActions("project_dev_docs", "create", "update"),
			withActions("system_backup", "create", "restore", "download"),
			withActions("system_operation_logs"),
		),
	},
	{
		Code:        model.RoleCodeImplementer,
		Name:        "实施",
		Description: "内置实施角色：交付物查看（构建/流水线）、现场缺陷反馈、部署文档编写、目标环境信息",
		Permissions: concat(
			[]string{
				"dashboard:view", "dashboard:build_summary",
				"dashboard:pipeline_run_summary", "dashboard:cicd_task_overview",
				"dashboard:my_projects",
				"handbook:view",
			},
			withActions("cicd_build_jobs"),
			withActions("cicd_build_runs"),
			withActions("cicd_script_jobs"),
			withActions("cicd_script_runs"),
			withActions("cicd_pipelines"),
			withActions("cicd_pipeline_runs"),
			withActions("project_projects"),
			withActions("project_bugs", "create", "update"),
			withActions("project_requirements"),
			withActions("project_docs"),
			withActions("project_dev_docs", "create", "update"),
			withActions("resource_repositories"),
			withActions("resource_servers"),
			withActions("resource_credentials", "use"),
			crud("resource_tokens"),
			withActions("ai_skills", "download"),
		),
	},
	{
		Code:        model.RoleCodeProduct,
		Name:        "产品",
		Description: "内置产品角色：需求全量管理、项目与缺陷协同、接口文档编写、AI 辅助",
		Permissions: concat(
			[]string{
				"dashboard:view", "dashboard:my_projects", "dashboard:agent_run_summary",
				"handbook:view",
			},
			withActions("project_projects", "create", "update"),
			withActions("project_bugs", "create", "update"),
			crud("project_requirements"),
			withActions("project_docs", "create", "update"),
			withActions("project_dev_docs"),
			withActions("ai_agents", "execute"),
			withActions("ai_runs"),
			withActions("ai_skills", "download"),
			crud("resource_tokens"),
		),
	},
}

func concat(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// EnsureBuiltinRoles seeds the registration-selectable builtin roles and their
// default permission matrix. Runs after EnsureRBACResources so the full_codes
// exist. Permission rows are only written when the role currently has none
// (first boot); later admin edits are preserved.
func EnsureBuiltinRoles(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, s := range builtinRoleSeeds {
			if err := ensureBuiltinRole(tx, s); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensureBuiltinRole(tx *gorm.DB, s builtinRoleSeed) error {
	var role model.Role
	err := tx.Where("code = ?", s.Code).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		role = model.Role{
			Name: s.Name, Code: s.Code, Description: s.Description,
			Type: model.RoleTypeBuiltin, DataScope: model.DataScopeSelf,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&role).Error; err != nil {
			return fmt.Errorf("creating builtin role %s: %w", s.Code, err)
		}
	} else if err != nil {
		return fmt.Errorf("find builtin role %s: %w", s.Code, err)
	}

	var bound int64
	if err := tx.Model(&model.RolePermission{}).Where("role_id = ?", role.ID).Count(&bound).Error; err != nil {
		return err
	}
	if bound > 0 {
		return nil
	}

	rows := make([]model.RolePermission, 0, len(s.Permissions))
	for _, code := range s.Permissions {
		rows = append(rows, model.RolePermission{RoleID: role.ID, Permission: code})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}
