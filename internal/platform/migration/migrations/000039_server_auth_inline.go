package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000039_server_auth_inline", upServerAuthInline)
}

// upServerAuthInline adds servers.password_cipher and migrates auth_type /
// credential bindings into the new password | ssh_key | agent model.
func upServerAuthInline(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	col := &serverAuthInlineMigrationModel{}
	if !db.Migrator().HasColumn(col, "PasswordCipher") {
		if err := db.Migrator().AddColumn(col, "PasswordCipher"); err != nil {
			return err
		}
	}

	// key / ssh_agent → ssh_key; drop general credential binding (host SSH agent).
	if err := db.Table("servers").
		Where("LOWER(auth_type) IN ?", []string{"key", "ssh_agent", "agent_ssh"}).
		Updates(map[string]any{"auth_type": "ssh_key", "credential_id": nil}).Error; err != nil {
		return err
	}

	// password + password-type credential: copy ciphertext as-is (same AES-GCM key).
	type credRow struct {
		ID           uint
		SecretCipher string `gorm:"column:secret_cipher"`
	}
	var passwordCreds []credRow
	if err := db.Table("credentials").
		Select("id", "secret_cipher").
		Where("LOWER(type) = ?", "password").
		Find(&passwordCreds).Error; err != nil {
		return err
	}
	for _, c := range passwordCreds {
		if c.SecretCipher == "" {
			continue
		}
		if err := db.Table("servers").
			Where("credential_id = ? AND LOWER(auth_type) = ?", c.ID, "password").
			Updates(map[string]any{
				"password_cipher": c.SecretCipher,
				"credential_id":   nil,
			}).Error; err != nil {
			return err
		}
	}

	// Any server still bound to an ssh_key credential → ssh_key (no PEM in app).
	var sshKeyIDs []uint
	if err := db.Table("credentials").
		Where("LOWER(type) = ?", "ssh_key").
		Pluck("id", &sshKeyIDs).Error; err != nil {
		return err
	}
	if len(sshKeyIDs) > 0 {
		if err := db.Table("servers").
			Where("credential_id IN ?", sshKeyIDs).
			Updates(map[string]any{"auth_type": "ssh_key", "credential_id": nil}).Error; err != nil {
			return err
		}
	}

	// Clear any remaining general credential_id (agent keeps agent_credential_id only).
	if err := db.Table("servers").
		Where("credential_id IS NOT NULL").
		Update("credential_id", nil).Error; err != nil {
		return err
	}
	return nil
}

type serverAuthInlineMigrationModel struct {
	ID             uint   `gorm:"primaryKey"`
	PasswordCipher string `gorm:"type:text"`
}

func (serverAuthInlineMigrationModel) TableName() string { return "servers" }
