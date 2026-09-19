package seed

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
)

// defaultUserRolePermissions is the permission set of the builtin user role
// bound to self-registered accounts: the seven end-user modules with full
// actions, data visibility narrowed to own data via the role's self scope.
var defaultUserRolePermissions = []struct {
	menu    string
	actions []string
}{
	{"cicd_build_jobs", []string{"view", "create", "update", "delete", "execute"}},
	{"cicd_build_runs", []string{"view"}},
	{"cicd_script_jobs", []string{"view", "create", "update", "delete", "execute"}},
	{"cicd_script_runs", []string{"view"}},
	{"cicd_pipelines", []string{"view", "create", "update", "delete", "execute"}},
	{"cicd_pipeline_runs", []string{"view"}},
	{"project_projects", []string{"view", "create", "update", "delete"}},
	{"project_bugs", []string{"view", "create", "update", "delete"}},
	{"ai_agents", []string{"view", "create", "update", "delete", "execute"}},
	{"ai_runs", []string{"view"}},
	{"ai_skills", []string{"view", "create", "update", "delete", "download"}},
}

// EnsureDefaultUserRole creates the builtin user role for self-registration and
// idempotently refreshes its permissions on boot (builtin roles are not
// editable via the role API, so re-seeding never clobbers admin changes).
func EnsureDefaultUserRole(db *gorm.DB) error {
	var role model.Role
	err := db.Where("code = ?", model.RoleCodeUser).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		role = model.Role{
			Name:        "普通用户",
			Code:        model.RoleCodeUser,
			Description: "内置普通用户角色，自助注册默认绑定，数据范围仅自己",
			Type:        model.RoleTypeBuiltin,
			DataScope:   model.DataScopeSelf,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := db.Create(&role).Error; err != nil {
			return fmt.Errorf("creating builtin user role: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("find builtin user role: %w", err)
	} else if role.Type != model.RoleTypeBuiltin || role.DataScope != model.DataScopeSelf {
		// A custom role squatting the code gets absorbed into the builtin shape.
		role.Type = model.RoleTypeBuiltin
		role.DataScope = model.DataScopeSelf
		if err := db.Save(&role).Error; err != nil {
			return fmt.Errorf("fix builtin user role: %w", err)
		}
	}

	perms := make([]string, 0, 32)
	for _, m := range defaultUserRolePermissions {
		for _, a := range m.actions {
			perms = append(perms, rbac.FeatureFullCode(m.menu, a))
		}
	}
	if err := rbacrepo.NewRoleRepository(db).ReplacePermissions(role.ID, perms); err != nil {
		return fmt.Errorf("seeding builtin user role permissions: %w", err)
	}
	return nil
}
