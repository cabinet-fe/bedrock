package migrations_test

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestMigration000065_SiteLinks(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m065.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	if !gdb.Migrator().HasTable("site_links") {
		t.Fatal("expected table site_links to exist")
	}
	for _, col := range []string{
		"id", "name", "url", "icon_base64", "icon_mime", "accounts_json",
		"remark", "enabled", "created_at", "updated_at",
	} {
		if !gdb.Migrator().HasColumn("site_links", col) {
			t.Errorf("expected column %s on site_links", col)
		}
	}
	for _, legacyCol := range []string{"account", "password_cipher", "project_id"} {
		if gdb.Migrator().HasColumn("site_links", legacyCol) {
			t.Fatalf("site_links must not have %s", legacyCol)
		}
	}
	if gdb.Migrator().HasTable("site_link_accounts") {
		t.Fatal("site links must not have an account child table")
	}

	// Simulate a legacy pre-000066 database with account/password_cipher columns and re-run 000066.
	if err := gdb.Exec("ALTER TABLE site_links ADD COLUMN `account` VARCHAR(200) NOT NULL DEFAULT ''").Error; err != nil {
		t.Fatalf("re-add legacy account column: %v", err)
	}
	if err := gdb.Exec("ALTER TABLE site_links ADD COLUMN `password_cipher` TEXT NOT NULL DEFAULT ''").Error; err != nil {
		t.Fatalf("re-add legacy password_cipher column: %v", err)
	}
	if err := gdb.Exec("INSERT INTO site_links (name, url, icon_base64, icon_mime, account, password_cipher, accounts_json, remark, enabled, created_at, updated_at) VALUES ('legacy', 'https://legacy.example', '', '', 'admin', 'cipher123', '', 'note', 1, datetime('now'), datetime('now'))").Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = '000066_site_link_accounts'").Error; err != nil {
		t.Fatalf("unapply 000066: %v", err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("re-apply 000066 failed: %v", err)
	}
	var accountsJSON string
	if err := gdb.Table("site_links").Where("name = ?", "legacy").Pluck("accounts_json", &accountsJSON).Error; err != nil {
		t.Fatalf("read accounts_json: %v", err)
	}
	if accountsJSON != `[{"account":"admin","password_cipher":"cipher123","remark":""}]` {
		t.Fatalf("unexpected backfilled accounts_json = %s", accountsJSON)
	}
	if gdb.Migrator().HasColumn("site_links", "account") || gdb.Migrator().HasColumn("site_links", "password_cipher") {
		t.Fatal("expected legacy account/password_cipher columns to be dropped")
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second migration.Up failed: %v", err)
	}
	var count int64
	if err := gdb.Table("schema_migrations").Where("version = ?", "000065_site_links").Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one applied row, got %d", count)
	}
}
