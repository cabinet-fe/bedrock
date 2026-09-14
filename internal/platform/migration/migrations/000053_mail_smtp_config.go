package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000053_mail_smtp_config", upMailSMTPConfig)
}

func upMailSMTPConfig(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	model := mailSMTPConfigMigrationModel{}
	if db.Migrator().HasTable(&model) {
		return nil
	}
	return db.Migrator().CreateTable(&model)
}

type mailSMTPConfigMigrationModel struct {
	ID             uint      `gorm:"primaryKey"`
	Host           string    `gorm:"size:255;not null;default:''"`
	Port           int       `gorm:"not null;default:0"`
	Username       string    `gorm:"size:255;not null;default:''"`
	PasswordCipher string    `gorm:"column:password_cipher;size:1024;not null;default:''"`
	FromAddress    string    `gorm:"size:255;not null;default:''"`
	CreatedAt      time.Time `gorm:""`
	UpdatedAt      time.Time `gorm:""`
}

func (mailSMTPConfigMigrationModel) TableName() string { return "mail_smtp_configs" }
