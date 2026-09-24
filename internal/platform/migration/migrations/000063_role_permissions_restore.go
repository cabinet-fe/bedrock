package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000063_role_permissions_restore", upRolePermissionsRestore)
}

// upRolePermissionsRestore re-enables per-role authorization: the role_permissions
// binding table (dropped by 000059) is recreated, and the ops menus stop being
// super_admin_only so the builtin ops role can be granted them. Builtin roles
// and their permission rows are seeded afterwards by seed.EnsureBuiltinRoles.
func upRolePermissionsRestore(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasTable("role_permissions") {
			if err := tx.Migrator().CreateTable(&rolePermissionMigrationModel{}); err != nil {
				return fmt.Errorf("create role_permissions: %w", err)
			}
		}
		// ops menus were force-marked super_admin_only since 000059; that gate is
		// not an admin choice, so clear it on menu rows and their features.
		res := tx.Exec(`
			UPDATE rbac_resources SET super_admin_only = ?
			WHERE full_code IN ? OR parent_id IN (
				SELECT id FROM rbac_resources WHERE type = 'menu' AND full_code IN ?
			)`,
			false,
			[]string{"ops_processes", "ops_dev_environments"},
			[]string{"ops_processes", "ops_dev_environments"},
		)
		return res.Error
	})
}
