package migrations

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000005_remove_system_menus", upRemoveSystemMenus)
}

// upRemoveSystemMenus drops the standalone admin "menu" resource (system.menus).
// Menu metadata lives on menu-type RbacResource nodes and is edited via permission resources.
func upRemoveSystemMenus(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	var res struct {
		ID uint
	}
	err := db.Table("rbac_resources").Select("id").Where("path = ?", "system.menus").Take(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM menu_metadata WHERE resource_id = ?", res.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM role_permissions WHERE permission LIKE ?", "system.menus:%").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM rbac_resources WHERE id = ?", res.ID).Error; err != nil {
			return err
		}
		return nil
	})
}
