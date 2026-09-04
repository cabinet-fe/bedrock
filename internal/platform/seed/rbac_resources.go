package seed

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
)

type seedGroup struct {
	Code        string
	Name        string
	RoutePrefix string
	SortKey     int
	Menus       []seedMenu
}

type seedMenu struct {
	Code           string
	Title          string
	Route          string
	SortKey        int
	Hidden         bool
	SuperAdminOnly bool
	Actions        []string // feature codes (type=action)
	Cards          []seedCard
}

type seedCard struct {
	Code    string
	Title   string
	SortKey int
}

var standardCRUD = []string{"view", "create", "update", "delete"}

// EnsureRBACResources seeds menu groups, menus, and features. Existing rows matched
// by full_code / group code are left untouched (admin edits preserved).
func EnsureRBACResources(db *gorm.DB) error {
	tree := []seedGroup{
		{
			Code: "overview", Name: "工作台", RoutePrefix: "", SortKey: 10,
			Menus: []seedMenu{
				{
					Code: "dashboard", Title: "仪表盘", Route: "/", SortKey: 10,
					Actions: []string{"view"},
					Cards: []seedCard{
						{Code: "build_summary", Title: "构建任务", SortKey: 10},
						{Code: "agent_run_summary", Title: "智能体运行", SortKey: 20},
						{Code: "system_info", Title: "系统信息", SortKey: 30},
						{Code: "system_status", Title: "系统状态", SortKey: 40},
						{Code: "script_run_summary", Title: "脚本运行", SortKey: 50},
						{Code: "pipeline_run_summary", Title: "流水线运行", SortKey: 60},
						{Code: "cicd_task_overview", Title: "任务概览", SortKey: 70},
						{Code: "my_projects", Title: "我的项目", SortKey: 80},
					},
				},
				{Code: "handbook", Title: "操作手册", Route: "/handbook", SortKey: 20, Actions: []string{"view"}},
			},
		},
		{
			Code: "ops", Name: "运维", RoutePrefix: "/ops", SortKey: 20,
			Menus: []seedMenu{
				{
					Code: "ops_processes", Title: "进程", Route: "/ops/processes", SortKey: 10,
					SuperAdminOnly: true,
					Actions:        []string{"view", "execute"},
				},
				{
					Code: "ops_dev_environments", Title: "开发环境", Route: "/ops/dev-environments", SortKey: 20,
					SuperAdminOnly: true,
					Actions:        []string{"view", "create", "update", "delete", "execute"},
				},
			},
		},
		{
			Code: "resource", Name: "资源管理", RoutePrefix: "/resource", SortKey: 25,
			Menus: []seedMenu{
				{Code: "resource_repositories", Title: "代码仓库", Route: "/resource/repositories", SortKey: 10, Actions: standardCRUD},
				{Code: "resource_servers", Title: "服务器", Route: "/resource/servers", SortKey: 20, Actions: standardCRUD},
				{Code: "resource_credentials", Title: "凭证", Route: "/resource/credentials", SortKey: 30, Actions: append(append([]string{}, standardCRUD...), "use")},
				{Code: "resource_tokens", Title: "访问令牌", Route: "/resource/tokens", SortKey: 40, Actions: standardCRUD},
			},
		},
		{
			Code: "cicd", Name: "CI/CD", RoutePrefix: "/cicd", SortKey: 30,
			Menus: []seedMenu{
				{Code: "cicd_build_jobs", Title: "构建任务", Route: "/cicd/build-jobs", SortKey: 10, Actions: append(append([]string{}, standardCRUD...), "execute")},
				{Code: "cicd_build_runs", Title: "构建记录", Route: "/cicd/build-runs", SortKey: 20, Actions: []string{"view"}},
				{Code: "cicd_script_jobs", Title: "脚本任务", Route: "/cicd/script-jobs", SortKey: 25, Actions: append(append([]string{}, standardCRUD...), "execute")},
				{Code: "cicd_script_runs", Title: "脚本记录", Route: "/cicd/script-runs", SortKey: 26, Actions: []string{"view"}},
				{Code: "cicd_pipelines", Title: "构建流水线", Route: "/cicd/pipelines", SortKey: 30, Actions: append(append([]string{}, standardCRUD...), "execute")},
				{Code: "cicd_pipeline_runs", Title: "流水线运行", Route: "/cicd/pipeline-runs", SortKey: 40, Actions: []string{"view"}},
			},
		},
		{
			Code: "project", Name: "项目管理", RoutePrefix: "/project", SortKey: 40,
			Menus: []seedMenu{
				{
					Code: "project_projects", Title: "产品项目", Route: "/project/projects", SortKey: 10,
					Actions: append(append([]string{}, standardCRUD...), "view_all", "manage_all"),
				},
				// Hidden from nav: still seeded so project-detail tabs / API permissions keep working.
				{Code: "project_requirements", Title: "需求管理", Route: "/project/requirements", SortKey: 20, Hidden: true, Actions: standardCRUD},
				{Code: "project_docs", Title: "接口文档", Route: "/project/docs", SortKey: 30, Hidden: true, Actions: append(append([]string{}, standardCRUD...), "execute")},
				{Code: "project_dev_docs", Title: "开发文档", Route: "/project/dev-docs", SortKey: 40, Hidden: true, Actions: standardCRUD},
			},
		},
		{
			Code: "ai", Name: "AI", RoutePrefix: "/ai", SortKey: 50,
			Menus: []seedMenu{
				{Code: "ai_agents", Title: "智能体", Route: "/ai/agents", SortKey: 10, Actions: append(append([]string{}, standardCRUD...), "execute")},
				{Code: "ai_runs", Title: "运行记录", Route: "/ai/runs", SortKey: 20, Actions: []string{"view"}},
				{Code: "ai_skills", Title: "技能", Route: "/ai/skills", SortKey: 30, Actions: append(append([]string{}, standardCRUD...), "download")},
			},
		},
		{
			Code: "system", Name: "系统管理", RoutePrefix: "/system", SortKey: 90,
			Menus: []seedMenu{
				{Code: "system_users", Title: "用户", Route: "/system/users", SortKey: 10, Actions: standardCRUD},
				{Code: "system_roles", Title: "角色", Route: "/system/roles", SortKey: 20, Actions: standardCRUD},
				{Code: "system_resources", Title: "权限资源", Route: "/system/resources", SortKey: 30, Actions: standardCRUD},
				{Code: "system_dictionaries", Title: "字典", Route: "/system/dictionaries", SortKey: 40, Actions: standardCRUD},
				{Code: "system_operation_logs", Title: "操作日志", Route: "/system/operation-logs", SortKey: 50, Actions: []string{"view", "clear"}},
			},
		},
	}

	now := time.Now().UTC()
	return db.Transaction(func(tx *gorm.DB) error {
		for _, g := range tree {
			if err := ensureGroup(tx, g, now); err != nil {
				return err
			}
		}
		if err := hideMenus(tx, "project_requirements", "project_docs", "project_dev_docs"); err != nil {
			return err
		}
		return removeRetiredMenus(tx, "dashboard_system_info", "dashboard_system_status", "resource_clis")
	})
}

// hideMenus marks existing menus as hidden (seed ensure leaves existing rows untouched).
func hideMenus(tx *gorm.DB, fullCodes ...string) error {
	for _, code := range fullCodes {
		res := tx.Model(&model.RbacResource{}).
			Where("full_code = ? AND type = ? AND hidden = ?", code, model.ResourceTypeMenu, false).
			Updates(map[string]any{"hidden": true, "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return fmt.Errorf("hide menu %s: %w", code, res.Error)
		}
	}
	return nil
}

// removeRetiredMenus deletes obsolete menu resources and their feature children.
func removeRetiredMenus(tx *gorm.DB, fullCodes ...string) error {
	for _, code := range fullCodes {
		var menu model.RbacResource
		err := tx.Where("full_code = ? AND type = ?", code, model.ResourceTypeMenu).First(&menu).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("find retired menu %s: %w", code, err)
		}
		if err := tx.Where("parent_id = ?", menu.ID).Delete(&model.RbacResource{}).Error; err != nil {
			return fmt.Errorf("delete features of %s: %w", code, err)
		}
		if err := tx.Delete(&menu).Error; err != nil {
			return fmt.Errorf("delete menu %s: %w", code, err)
		}
		_ = tx.Where("permission LIKE ?", code+":%").Delete(&model.RolePermission{}).Error
		_ = tx.Where("permission = ?", code).Delete(&model.RolePermission{}).Error
	}
	return nil
}

func ensureGroup(tx *gorm.DB, g seedGroup, now time.Time) error {
	var group model.MenuGroup
	err := tx.Where("code = ?", g.Code).First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		group = model.MenuGroup{
			Name: g.Name, Code: g.Code, RoutePrefix: g.RoutePrefix,
			SortKey: g.SortKey, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&group).Error; err != nil {
			return fmt.Errorf("create menu group %s: %w", g.Code, err)
		}
	} else if err != nil {
		return fmt.Errorf("find menu group %s: %w", g.Code, err)
	}

	for _, m := range g.Menus {
		if err := ensureMenu(tx, group.ID, m, now); err != nil {
			return err
		}
	}
	return nil
}

func ensureMenu(tx *gorm.DB, groupID uint, m seedMenu, now time.Time) error {
	var res model.RbacResource
	err := tx.Where("full_code = ?", m.Code).First(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		gid := groupID
		res = model.RbacResource{
			Code: m.Code, FullCode: m.Code, Type: model.ResourceTypeMenu,
			GroupID: &gid, SuperAdminOnly: m.SuperAdminOnly, Hidden: m.Hidden,
			Enabled: true, SortKey: m.SortKey, Title: m.Title, Route: m.Route,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&res).Error; err != nil {
			return fmt.Errorf("create menu %s: %w", m.Code, err)
		}
	} else if err != nil {
		return fmt.Errorf("find menu %s: %w", m.Code, err)
	}

	for i, action := range m.Actions {
		if err := ensureFeature(tx, res, action, model.ResourceTypeAction, actionTitle(action), (i+1)*10, m.SuperAdminOnly, now); err != nil {
			return err
		}
	}
	for _, card := range m.Cards {
		if err := ensureFeature(tx, res, card.Code, model.ResourceTypeCard, card.Title, card.SortKey, m.SuperAdminOnly, now); err != nil {
			return err
		}
	}
	return nil
}

func ensureFeature(tx *gorm.DB, menu model.RbacResource, code, typ, title string, sortKey int, superAdminOnly bool, now time.Time) error {
	fullCode := rbac.FeatureFullCode(menu.Code, code)
	var existing model.RbacResource
	err := tx.Where("full_code = ?", fullCode).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		parentID := menu.ID
		feat := model.RbacResource{
			Code: code, FullCode: fullCode, Type: typ,
			ParentID: &parentID, SuperAdminOnly: superAdminOnly || menu.SuperAdminOnly,
			Enabled: true, SortKey: sortKey, Title: title,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&feat).Error; err != nil {
			return fmt.Errorf("create feature %s: %w", fullCode, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find feature %s: %w", fullCode, err)
	}
	return nil
}

func actionTitle(code string) string {
	titles := map[string]string{
		"view": "查看", "create": "创建", "update": "更新", "delete": "删除",
		"execute": "执行", "use": "使用", "view_all": "查看全部", "manage_all": "管理全部",
		"download": "下载", "clear": "清空",
	}
	if t, ok := titles[code]; ok {
		return t
	}
	return code
}
