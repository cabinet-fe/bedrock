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
		"id", "name", "url", "icon_base64", "icon_mime", "account",
		"password_cipher", "remark", "enabled", "created_at", "updated_at",
	} {
		if !gdb.Migrator().HasColumn("site_links", col) {
			t.Errorf("expected column %s on site_links", col)
		}
	}
	if gdb.Migrator().HasColumn("site_links", "project_id") {
		t.Fatal("site_links must not have project_id")
	}
	if gdb.Migrator().HasTable("site_link_accounts") {
		t.Fatal("site links must not have an account child table")
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
