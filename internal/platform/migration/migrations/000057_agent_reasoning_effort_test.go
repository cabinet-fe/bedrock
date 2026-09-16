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

func TestMigration000057_AgentReasoningEffort(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m057.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	if !gdb.Migrator().HasColumn("ai_agents", "reasoning_effort") {
		t.Fatal("expected column ai_agents.reasoning_effort")
	}
	// Empty default keeps existing rows valid without a backfill.
	if err := gdb.Exec("INSERT INTO ai_agents (name, timeout_sec, created_at, updated_at) VALUES ('a', 60, datetime('now'), datetime('now'))").Error; err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	var effort string
	if err := gdb.Raw("SELECT reasoning_effort FROM ai_agents").Scan(&effort).Error; err != nil {
		t.Fatalf("select reasoning_effort: %v", err)
	}
	if effort != "" {
		t.Fatalf("reasoning_effort default = %q, want empty", effort)
	}

	// Idempotent re-run must not fail or duplicate.
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second migration.Up failed: %v", err)
	}
}
