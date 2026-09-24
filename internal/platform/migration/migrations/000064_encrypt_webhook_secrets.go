package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"bedrock/internal/pkg"
	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000064_encrypt_webhook_secrets", upEncryptWebhookSecrets)
}

// upEncryptWebhookSecrets encrypts stored webhook secrets (AES-GCM) and widens
// the column: a 24-byte secret hex-encodes to 48 chars, the GCM ciphertext to
// ~104, so size:64 no longer fits. Existing plaintext rows are encrypted in
// place; empty secrets stay empty.
//
// SQLite keeps the legacy varchar(64) (declared type length is advisory and
// long values are stored whole), so only Postgres/MySQL get an explicit
// ALTER COLUMN TYPE TEXT.
func upEncryptWebhookSecrets(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx

	for _, table := range []string{"build_jobs", "script_jobs", "build_pipelines"} {
		if err := db.Transaction(func(tx *gorm.DB) error {
			type row struct {
				ID            uint
				WebhookSecret string
			}
			var rows []row
			if err := tx.Table(table).Select("id", "webhook_secret").Find(&rows).Error; err != nil {
				return fmt.Errorf("read %s webhook secrets: %w", table, err)
			}
			for _, r := range rows {
				if r.WebhookSecret == "" {
					continue
				}
				cipher, err := pkg.Encrypt(r.WebhookSecret)
				if err != nil {
					return fmt.Errorf("encrypt %s#%d webhook secret: %w", table, r.ID, err)
				}
				if err := tx.Table(table).Where("id = ?", r.ID).Update("webhook_secret", cipher).Error; err != nil {
					return fmt.Errorf("update %s#%d webhook secret: %w", table, r.ID, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}

		if driver != "sqlite" {
			m := &cicdWebhookSecretMigrationModel{tableName: table}
			if err := db.Migrator().AlterColumn(m, "WebhookSecret"); err != nil {
				return fmt.Errorf("widen %s.webhook_secret: %w", table, err)
			}
		}
	}
	return nil
}

type cicdWebhookSecretMigrationModel struct {
	tableName     string
	ID            uint
	WebhookSecret string `gorm:"type:text"`
}

func (m cicdWebhookSecretMigrationModel) TableName() string {
	return m.tableName
}
