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

func TestMigration000055_HarnessSessions(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m055.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	for _, col := range []string{"harness_session_id", "harness_session_status", "final_output"} {
		if !gdb.Migrator().HasColumn("agent_runs", col) {
			t.Errorf("expected column %s on agent_runs", col)
		}
	}
	if !gdb.Migrator().HasIndex("agent_runs", "uidx_agent_runs_harness_session_id") {
		t.Error("expected unique index uidx_agent_runs_harness_session_id on agent_runs")
	}
	for _, col := range []string{"model_provider", "model_id", "approval_mode"} {
		if !gdb.Migrator().HasColumn("ai_agents", col) {
			t.Errorf("expected column %s on ai_agents", col)
		}
	}
	// 000056 drops the deprecated cli_key column; agent_runs.output_text stays.
	if gdb.Migrator().HasColumn("ai_agents", "cli_key") {
		t.Error("expected deprecated column ai_agents.cli_key to be dropped by 000056")
	}
	if !gdb.Migrator().HasColumn("agent_runs", "output_text") {
		t.Error("expected legacy column agent_runs.output_text to be retained")
	}

	// approval_mode defaults to manual for existing rows.
	if err := gdb.Exec("INSERT INTO ai_agents (name, timeout_sec, created_at, updated_at) VALUES ('a', 60, datetime('now'), datetime('now'))").Error; err != nil {
		t.Fatalf("insert legacy agent: %v", err)
	}
	var approvalMode string
	if err := gdb.Raw("SELECT approval_mode FROM ai_agents").Scan(&approvalMode).Error; err != nil {
		t.Fatalf("select approval_mode: %v", err)
	}
	if approvalMode != "manual" {
		t.Fatalf("approval_mode default = %q, want manual", approvalMode)
	}

	// harness_session_id is unique when set; NULL stays allowed for legacy runs.
	insertRun := func(sessionID any) error {
		return gdb.Exec("INSERT INTO agent_runs (agent_id, trigger_type, status, harness_session_id) VALUES (1, 'manual', 'queued', ?)", sessionID).Error
	}
	if err := insertRun(nil); err != nil {
		t.Fatalf("insert legacy run with NULL session id: %v", err)
	}
	if err := insertRun(nil); err != nil {
		t.Fatalf("second NULL session id must be allowed: %v", err)
	}
	if err := insertRun("ses_dup"); err != nil {
		t.Fatalf("insert run with session id: %v", err)
	}
	if err := insertRun("ses_dup"); err == nil {
		t.Fatal("expected duplicate harness_session_id to be rejected")
	}

	// Idempotent re-run must not fail or duplicate.
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("second migration.Up failed: %v", err)
	}
	var count int64
	if err := gdb.Table("schema_migrations").Where("version = ?", "000055_harness_sessions").Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one applied row, got %d", count)
	}
}
