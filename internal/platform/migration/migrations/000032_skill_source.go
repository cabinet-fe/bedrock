package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000032_skill_source", upSkillSource)
}

// Add source (uploaded|builtin) so the UI/API can treat built-in skills as read-only.
func upSkillSource(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	m := &skillPackageSourceMigration{}
	if !db.Migrator().HasTable(m) {
		return nil
	}
	if db.Migrator().HasColumn(m, "Source") {
		return nil
	}
	if err := db.Migrator().AddColumn(m, "Source"); err != nil {
		return err
	}
	return db.Exec(`UPDATE skill_packages SET source = ? WHERE source = '' OR source IS NULL`, "uploaded").Error
}

type skillPackageSourceMigration struct {
	ID     uint   `gorm:"primaryKey"`
	Source string `gorm:"size:20;not null;default:uploaded;index"`
}

func (skillPackageSourceMigration) TableName() string { return "skill_packages" }
