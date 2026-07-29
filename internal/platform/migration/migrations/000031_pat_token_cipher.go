package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000031_pat_token_cipher", upPATTokenCipher)
}

// Add AES-GCM ciphertext so owners can reveal ciphertext and decrypt client-side.
// Legacy rows keep empty token_cipher (hash-only); Reveal returns not-copyable.
func upPATTokenCipher(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	m := &patTokenCipherMigration{}
	if !db.Migrator().HasTable(m) {
		return nil
	}
	if db.Migrator().HasColumn(m, "TokenCipher") {
		return nil
	}
	return db.Migrator().AddColumn(m, "TokenCipher")
}

type patTokenCipherMigration struct {
	ID          uint   `gorm:"primaryKey"`
	TokenCipher string `gorm:"type:text"`
}

func (patTokenCipherMigration) TableName() string { return "personal_access_tokens" }
