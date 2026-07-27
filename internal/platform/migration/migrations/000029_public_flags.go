package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000029_public_flags", upPublicFlags)
}

// product_projects.is_public / build_jobs.is_public: read-only visibility exception
// for data_scope=self users.
func upPublicFlags(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	if err := addIsPublicColumn(db, &productProjectPublicMigration{}); err != nil {
		return err
	}
	return addIsPublicColumn(db, &buildJobPublicMigration{})
}

func addIsPublicColumn(db *gorm.DB, model any) error {
	if !db.Migrator().HasTable(model) {
		return nil
	}
	if db.Migrator().HasColumn(model, "IsPublic") {
		return nil
	}
	return db.Migrator().AddColumn(model, "IsPublic")
}

type productProjectPublicMigration struct {
	ID       uint `gorm:"primaryKey"`
	IsPublic bool `gorm:"not null;default:false;index"`
}

func (productProjectPublicMigration) TableName() string { return "product_projects" }

type buildJobPublicMigration struct {
	ID       uint `gorm:"primaryKey"`
	IsPublic bool `gorm:"not null;default:false;index"`
}

func (buildJobPublicMigration) TableName() string { return "build_jobs" }
