package seed

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/rbac/model"
)

// EnsureDefaultUserRole creates the builtin user role bound to self-registered
// accounts. Permissions are system-wide (all features except super_admin_only),
// so only the role shape is ensured here; DataScope stays self so the role's
// data visibility is narrowed to own data.
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
		return nil
	}
	if err != nil {
		return fmt.Errorf("find builtin user role: %w", err)
	}
	if role.Type != model.RoleTypeBuiltin || role.DataScope != model.DataScopeSelf {
		// A custom role squatting the code gets absorbed into the builtin shape.
		role.Type = model.RoleTypeBuiltin
		role.DataScope = model.DataScopeSelf
		if err := db.Save(&role).Error; err != nil {
			return fmt.Errorf("fix builtin user role: %w", err)
		}
	}
	return nil
}
