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

func TestMigration000053_MailSMTPConfig(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m053.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	if !gdb.Migrator().HasTable("mail_smtp_configs") {
		t.Fatal("expected table mail_smtp_configs to exist")
	}
	for _, col := range []string{
		"id", "host", "port", "username", "password_cipher", "from_address", "created_at", "updated_at",
	} {
		if !gdb.Migrator().HasColumn("mail_smtp_configs", col) {
			t.Errorf("expected column %s on mail_smtp_configs", col)
		}
	}

	// Idempotent re-run must not fail or duplicate.
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second migration.Up failed: %v", err)
	}
	var count int64
	if err := gdb.Table("schema_migrations").Where("version = ?", "000053_mail_smtp_config").Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one applied row, got %d", count)
	}
}
