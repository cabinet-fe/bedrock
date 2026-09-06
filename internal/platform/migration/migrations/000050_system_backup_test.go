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

func TestMigration000050_SystemBackup(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m050.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	if !gdb.Migrator().HasTable("system_backups") {
		t.Fatal("expected system_backups table to exist")
	}

	expectedColumns := []string{
		"id", "filename", "file_path", "file_size", "modules_json",
		"note", "status", "error_message", "created_by", "created_at", "updated_at",
	}

	for _, col := range expectedColumns {
		if !gdb.Migrator().HasColumn("system_backups", col) {
			t.Errorf("expected column %s on system_backups", col)
		}
	}
}
