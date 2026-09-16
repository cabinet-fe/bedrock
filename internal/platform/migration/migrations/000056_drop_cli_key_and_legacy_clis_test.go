package migrations_test

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestMigration000056_DropCLIKeyAndLegacyCLIs(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m056.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	// Fresh databases seed only opencode/reasonix and never create cli_key.
	if gdb.Migrator().HasColumn("ai_agents", "cli_key") {
		t.Error("expected ai_agents.cli_key to be absent on fresh databases")
	}
	assertCLIKeys(t, gdb, map[string]bool{"opencode": true, "reasonix": true, "claude_code": false, "codex": false})

	// Simulate a legacy database (pre-000056 state), then re-apply 000056.
	// The column is added with backticks like gorm-issued DDL: glebarez's
	// DDL rewriter matches columns in backticked form only.
	if err := gdb.Exec("ALTER TABLE ai_agents ADD COLUMN `cli_key` VARCHAR(40)").Error; err != nil {
		t.Fatalf("re-add legacy cli_key column: %v", err)
	}
	if err := gdb.Exec("CREATE INDEX idx_ai_agents_cli_key ON ai_agents(cli_key)").Error; err != nil {
		t.Fatalf("re-create legacy cli_key index: %v", err)
	}
	seedLegacyCLI := func(key string) {
		if err := gdb.Exec("INSERT INTO cli_runtime_definitions (`key`, name, binary_name, install_status, created_at, updated_at) VALUES (?, ?, ?, 'unknown', datetime('now'), datetime('now'))", key, key, key).Error; err != nil {
			t.Fatalf("insert legacy cli %s: %v", key, err)
		}
		if err := gdb.Exec("INSERT INTO cli_install_sources (cli_key, name, base_url, priority, enabled, created_at, updated_at) VALUES (?, 'npm registry', 'https://registry.npmjs.org', 10, 1, datetime('now'), datetime('now'))", key).Error; err != nil {
			t.Fatalf("insert legacy source %s: %v", key, err)
		}
	}
	seedLegacyCLI("claude_code")
	seedLegacyCLI("codex")
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = '000056_drop_cli_key_and_legacy_clis'").Error; err != nil {
		t.Fatalf("unapply 000056: %v", err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("re-apply 000056 failed: %v", err)
	}

	if gdb.Migrator().HasColumn("ai_agents", "cli_key") {
		t.Error("expected ai_agents.cli_key to be dropped on legacy databases")
	}
	if gdb.Migrator().HasIndex("ai_agents", "idx_ai_agents_cli_key") {
		t.Error("expected idx_ai_agents_cli_key to be dropped on legacy databases")
	}
	assertCLIKeys(t, gdb, map[string]bool{"opencode": true, "reasonix": true, "claude_code": false, "codex": false})

	// Idempotent re-run must not fail or duplicate.
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second migration.Up failed: %v", err)
	}
}

func assertCLIKeys(t *testing.T, gdb *gorm.DB, want map[string]bool) {
	t.Helper()
	for key, present := range want {
		var count int64
		if err := gdb.Raw("SELECT COUNT(*) FROM cli_runtime_definitions WHERE `key` = ?", key).Scan(&count).Error; err != nil {
			t.Fatalf("count cli %s: %v", key, err)
		}
		if (count > 0) != present {
			t.Errorf("cli_runtime_definitions %s present = %v, want %v", key, count > 0, present)
		}
		var srcCount int64
		if err := gdb.Raw("SELECT COUNT(*) FROM cli_install_sources WHERE cli_key = ?", key).Scan(&srcCount).Error; err != nil {
			t.Fatalf("count cli sources %s: %v", key, err)
		}
		if (srcCount > 0) != present {
			t.Errorf("cli_install_sources %s present = %v, want %v", key, srcCount > 0, present)
		}
	}
}
