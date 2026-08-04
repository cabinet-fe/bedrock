package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000042_dev_doc_nodes", upDevDocNodes)
}

func upDevDocNodes(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	m := &devDocNodeMigrationModel{}
	if db.Migrator().HasTable(m) {
		return nil
	}
	return db.Migrator().CreateTable(m)
}

type devDocNodeMigrationModel struct {
	ID           uint           `gorm:"primaryKey"`
	ProjectID    uint           `gorm:"not null;index"`
	ParentID     *uint          `gorm:"index"`
	Kind         string         `gorm:"size:10;not null;index"`
	Name         string         `gorm:"size:300;not null"`
	SortOrder    int            `gorm:"not null;default:0;index"`
	RepositoryID *uint          `gorm:"index"`
	Content      string         `gorm:"type:text"`
	CreatedBy    uint           `gorm:"index"`
	UpdatedBy    uint           `gorm:"index"`
	CreatedAt    time.Time      `gorm:""`
	UpdatedAt    time.Time      `gorm:""`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (devDocNodeMigrationModel) TableName() string { return "dev_doc_nodes" }
