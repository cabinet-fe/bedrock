package seed

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"bedrock/internal/platform/config"
	"bedrock/internal/rbac/model"
)

// UserRow is the minimal row used for super-admin seeding (avoids auth→platform cycle).
type UserRow struct {
	ID           uint      `gorm:"primaryKey"`
	Username     string    `gorm:"size:50;uniqueIndex;not null"`
	PasswordHash string    `gorm:"size:255;not null"`
	DisplayName  string    `gorm:"size:100"`
	Email        string    `gorm:"size:200"`
	Avatar       string    `gorm:"size:500"`
	IsActive     bool      `gorm:"not null;default:true"`
	IsSuperAdmin bool      `gorm:"not null;default:false"`
	CreatedAt    time.Time `gorm:""`
	UpdatedAt    time.Time `gorm:""`
}

func (UserRow) TableName() string { return "users" }

// EnsureSuperAdmin creates the built-in super-admin from config when no users exist,
// ensures the builtin super_admin role, and keeps a 1:1 bind with the sole is_super_admin user.
func EnsureSuperAdmin(db *gorm.DB, admin config.AdminConfig) error {
	if err := ensureBuiltinSuperAdminRole(db); err != nil {
		return err
	}

	var count int64
	if err := db.Model(&UserRow{}).Count(&count).Error; err != nil {
		return fmt.Errorf("counting users: %w", err)
	}
	if count == 0 {
		if admin.Username == "" || admin.Password == "" {
			return fmt.Errorf("admin.username and admin.password must be set when no users exist")
		}
		displayName := admin.DisplayName
		if displayName == "" {
			displayName = "Administrator"
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(admin.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hashing admin password: %w", err)
		}
		now := time.Now().UTC()
		row := UserRow{
			Username:     admin.Username,
			PasswordHash: string(hash),
			DisplayName:  displayName,
			IsActive:     true,
			IsSuperAdmin: true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := db.Create(&row).Error; err != nil {
			return fmt.Errorf("creating super-admin: %w", err)
		}
	}

	return syncSuperAdminRoleBinding(db)
}

func ensureBuiltinSuperAdminRole(db *gorm.DB) error {
	var role model.Role
	err := db.Where("code = ?", model.RoleCodeSuperAdmin).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		role = model.Role{
			Name:        "超级管理员",
			Code:        model.RoleCodeSuperAdmin,
			Description: "内置超级管理员角色，与 is_super_admin 用户 1:1 同步",
			Type:        model.RoleTypeBuiltin,
			DataScope:   model.DataScopeAll,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := db.Create(&role).Error; err != nil {
			return fmt.Errorf("creating builtin super_admin role: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find super_admin role: %w", err)
	}
	if role.Type != model.RoleTypeBuiltin {
		role.Type = model.RoleTypeBuiltin
		if err := db.Save(&role).Error; err != nil {
			return fmt.Errorf("fix super_admin role type: %w", err)
		}
	}
	return nil
}

func syncSuperAdminRoleBinding(db *gorm.DB) error {
	var role model.Role
	if err := db.Where("code = ?", model.RoleCodeSuperAdmin).First(&role).Error; err != nil {
		return fmt.Errorf("find super_admin role for sync: %w", err)
	}

	var supers []UserRow
	if err := db.Where("is_super_admin = ?", true).Order("id ASC").Find(&supers).Error; err != nil {
		return fmt.Errorf("list super-admin users: %w", err)
	}
	if len(supers) == 0 {
		return nil
	}
	// Application invariant: exactly one is_super_admin; bind the first and strip others from the role.
	keep := supers[0]
	if err := db.Where("role_id = ? AND user_id <> ?", role.ID, keep.ID).
		Delete(&model.UserRole{}).Error; err != nil {
		return fmt.Errorf("strip extra super_admin role binds: %w", err)
	}
	var n int64
	if err := db.Model(&model.UserRole{}).
		Where("user_id = ? AND role_id = ?", keep.ID, role.ID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		if err := db.Create(&model.UserRole{UserID: keep.ID, RoleID: role.ID}).Error; err != nil {
			return fmt.Errorf("bind super_admin role: %w", err)
		}
	}
	return nil
}
