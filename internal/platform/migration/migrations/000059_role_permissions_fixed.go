package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000059_role_permissions_fixed", upRolePermissionsFixed)
}

// upRolePermissionsFixed makes permissions non-editable: every role now carries
// all feature permissions except super_admin_only ones. The dashboard
// system_info / system_status cards become super_admin_only (超管专属)，and the
// role_permissions binding table is dropped along with its gate on resource
// deletion (the stale-code cleanup no longer has rows to touch).
func upRolePermissionsFixed(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	return db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"UPDATE rbac_resources SET super_admin_only = ? WHERE full_code IN ?",
			true, []string{"dashboard:system_info", "dashboard:system_status"},
		)
		if res.Error != nil {
			return fmt.Errorf("gating dashboard cards: %w", res.Error)
		}

		if tx.Migrator().HasTable("role_permissions") {
			if err := tx.Migrator().DropTable("role_permissions"); err != nil {
				return fmt.Errorf("drop role_permissions: %w", err)
			}
		}
		return nil
	})
}
