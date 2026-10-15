package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000065_site_links", upSiteLinks)
}

func upSiteLinks(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	model := siteLinkMigrationModel{}
	if db.Migrator().HasTable(&model) {
		return nil
	}
	return db.Migrator().CreateTable(&model)
}

type siteLinkMigrationModel struct {
	ID             uint      `gorm:"primaryKey"`
	Name           string    `gorm:"size:200;not null"`
	URL            string    `gorm:"column:url;size:2000;not null"`
	IconBase64     string    `gorm:"type:text;not null"`
	IconMime       string    `gorm:"size:64;not null;default:''"`
	Account        string    `gorm:"size:200;not null;default:''"`
	PasswordCipher string    `gorm:"column:password_cipher;type:text;not null"`
	Remark         string    `gorm:"type:text;not null"`
	Enabled        bool      `gorm:"not null;default:true"`
	CreatedAt      time.Time `gorm:""`
	UpdatedAt      time.Time `gorm:""`
}

func (siteLinkMigrationModel) TableName() string { return "site_links" }
