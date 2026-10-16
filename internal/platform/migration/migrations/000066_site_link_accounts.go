package migrations

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000066_site_link_accounts", upSiteLinkAccounts)
}

// upSiteLinkAccounts upgrades site_links from single account/password_cipher
// columns to the multi-account accounts_json column, backfilling any existing
// single-account rows before dropping the legacy columns.
func upSiteLinkAccounts(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	m := &siteLinkAccountsMigrationModel{}
	if !db.Migrator().HasTable(m) {
		return nil
	}
	if !db.Migrator().HasColumn(m, "AccountsJSON") {
		if err := db.Migrator().AddColumn(m, "AccountsJSON"); err != nil {
			return err
		}
	}

	hasAccount := db.Migrator().HasColumn(m, "Account")
	hasCipher := db.Migrator().HasColumn(m, "PasswordCipher")
	if hasAccount || hasCipher {
		type legacyRow struct {
			ID             uint
			Account        string
			PasswordCipher string
			AccountsJSON   string
		}
		var rows []legacyRow
		if err := db.Table("site_links").Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if strings.TrimSpace(r.AccountsJSON) != "" {
				continue
			}
			encoded := "[]"
			if strings.TrimSpace(r.Account) != "" || r.PasswordCipher != "" {
				raw, err := json.Marshal([]map[string]string{{
					"account":         r.Account,
					"password_cipher": r.PasswordCipher,
					"remark":          "",
				}})
				if err != nil {
					return err
				}
				encoded = string(raw)
			}
			if err := db.Table("site_links").Where("id = ?", r.ID).Update("accounts_json", encoded).Error; err != nil {
				return err
			}
		}
	}

	if err := db.Table("site_links").Where("accounts_json IS NULL OR accounts_json = ''").Update("accounts_json", "[]").Error; err != nil {
		return err
	}

	if hasAccount {
		if err := db.Migrator().DropColumn(m, "Account"); err != nil {
			return err
		}
	}
	if hasCipher {
		if err := db.Migrator().DropColumn(m, "PasswordCipher"); err != nil {
			return err
		}
	}
	return nil
}

type siteLinkAccountsMigrationModel struct {
	ID             uint   `gorm:"primaryKey"`
	Account        string `gorm:"column:account;size:200"`
	PasswordCipher string `gorm:"column:password_cipher;type:text"`
	AccountsJSON   string `gorm:"column:accounts_json;type:text"`
}

func (siteLinkAccountsMigrationModel) TableName() string { return "site_links" }
