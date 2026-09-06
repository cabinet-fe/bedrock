package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000050_system_backup", upSystemBackup)
}

func upSystemBackup(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	models := []any{
		&systemBackupMigrationModel{},
	}
	for _, m := range models {
		if db.Migrator().HasTable(m) {
			continue
		}
		if err := db.Migrator().CreateTable(m); err != nil {
			return err
		}
	}
	return nil
}

type systemBackupMigrationModel struct {
	ID           uint      `gorm:"primaryKey"`
	Filename     string    `gorm:"size:255;not null"`
	FilePath     string    `gorm:"size:500;not null"`
	FileSize     int64     `gorm:"not null;default:0"`
	ModulesJSON  string    `gorm:"column:modules_json;type:text;not null"`
	Note         string    `gorm:"size:500"`
	Status       string    `gorm:"size:50;not null;default:processing;index"`
	ErrorMessage string    `gorm:"type:text"`
	CreatedBy    uint      `gorm:"index"`
	CreatedAt    time.Time `gorm:"index"`
	UpdatedAt    time.Time `gorm:""`
}

func (systemBackupMigrationModel) TableName() string { return "system_backups" }
