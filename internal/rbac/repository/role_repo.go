package repository

import (
	"bedrock/internal/pkg"
	"bedrock/internal/rbac/model"

	"gorm.io/gorm"
)

type RoleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) Create(role *model.Role) error {
	if role.Type == "" {
		role.Type = model.RoleTypeCustom
	}
	return r.db.Create(role).Error
}

func (r *RoleRepository) FindByID(id uint) (*model.Role, error) {
	var role model.Role
	err := r.db.Preload("Permissions").First(&role, id).Error
	return &role, err
}

func (r *RoleRepository) FindByCode(code string) (*model.Role, error) {
	var role model.Role
	err := r.db.Preload("Permissions").Where("code = ?", code).First(&role).Error
	return &role, err
}

func (r *RoleRepository) List(q pkg.ListQuery) ([]model.Role, int64, error) {
	var items []model.Role
	var total int64
	if err := r.db.Model(&model.Role{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Preload("Permissions").
		Offset(q.Offset()).Limit(q.PageSize).
		Order("id ASC").Find(&items).Error
	return items, total, err
}

func (r *RoleRepository) Update(role *model.Role) error {
	return r.db.Save(role).Error
}

func (r *RoleRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Role{}, id).Error
	})
}

func (r *RoleRepository) ReplacePermissions(roleID uint, permissions []string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		for _, p := range permissions {
			if p == "" {
				continue
			}
			row := model.RolePermission{RoleID: roleID, Permission: p}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *RoleRepository) ListPermissionsByUserID(userID uint) ([]string, error) {
	var perms []string
	err := r.db.Model(&model.RolePermission{}).
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ?", userID).
		Pluck("role_permissions.permission", &perms).Error
	return perms, err
}

// ListDataScopesByUserID returns data_scope values of roles assigned to the user.
func (r *RoleRepository) ListDataScopesByUserID(userID uint) ([]string, error) {
	var scopes []string
	err := r.db.Model(&model.Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Pluck("roles.data_scope", &scopes).Error
	return scopes, err
}

func (r *RoleRepository) ListRoleIDsByUserID(userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.Model(&model.UserRole{}).Where("user_id = ?", userID).Pluck("role_id", &ids).Error
	return ids, err
}

func (r *RoleRepository) ReplaceUserRoles(userID uint, roleIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		for _, rid := range roleIDs {
			row := model.UserRole{UserID: userID, RoleID: rid}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *RoleRepository) ListDistinctPermissions() ([]string, error) {
	var perms []string
	err := r.db.Model(&model.RolePermission{}).Distinct("permission").Pluck("permission", &perms).Error
	return perms, err
}

func (r *RoleRepository) EnsureUserHasRole(userID, roleID uint) error {
	var n int64
	if err := r.db.Model(&model.UserRole{}).
		Where("user_id = ? AND role_id = ?", userID, roleID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return r.db.Create(&model.UserRole{UserID: userID, RoleID: roleID}).Error
}
