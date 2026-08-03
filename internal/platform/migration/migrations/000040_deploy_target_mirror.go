package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000040_deploy_target_mirror", upDeployTargetMirror)
}

// upDeployTargetMirror adds deploy_targets.mirror (default false = rsync merge).
func upDeployTargetMirror(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	m := &deployTargetMirrorMigrationModel{}
	if !db.Migrator().HasTable(m) {
		return nil
	}
	if db.Migrator().HasColumn(m, "Mirror") {
		return nil
	}
	return db.Migrator().AddColumn(m, "Mirror")
}

type deployTargetMirrorMigrationModel struct {
	ID     uint `gorm:"primaryKey"`
	Mirror bool `gorm:"not null;default:false"`
}

func (deployTargetMirrorMigrationModel) TableName() string { return "deploy_targets" }
