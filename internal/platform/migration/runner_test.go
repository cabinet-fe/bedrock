package migration_test

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"

	_ "bedrock/internal/platform/migration/migrations"
)

func TestUp_idempotent(t *testing.T) {
	dir := t.TempDir()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "t.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	if err := migration.Up(ctx, gdb, "sqlite"); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	applied, err := migration.AppliedVersions(gdb)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := applied["000001_users"]; !ok {
		t.Fatal("expected 000001_users applied")
	}
	if err := migration.Up(ctx, gdb, "sqlite"); err != nil {
		t.Fatalf("second Up (idempotent): %v", err)
	}
	applied2, err := migration.AppliedVersions(gdb)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied2) != len(applied) {
		t.Fatalf("applied count changed: %d -> %d", len(applied), len(applied2))
	}
	if !gdb.Migrator().HasTable("users") {
		t.Fatal("users table missing")
	}
	if !gdb.Migrator().HasTable("schema_migrations") {
		t.Fatal("schema_migrations table missing")
	}
	for _, column := range []string{"artifact_format", "max_artifacts"} {
		if gdb.Migrator().HasColumn("ai_agents", column) {
			t.Fatalf("ai_agents.%s should be removed", column)
		}
	}
	if !gdb.Migrator().HasColumn("ai_agents", "output_dir") {
		t.Fatal("ai_agents.output_dir should be retained")
	}
	if gdb.Migrator().HasColumn("agent_runs", "artifact_path") {
		t.Fatal("agent_runs.artifact_path should be removed")
	}
	if !gdb.Migrator().HasColumn("agent_runs", "work_dir") {
		t.Fatal("agent_runs.work_dir should be retained")
	}
	if !gdb.Migrator().HasColumn("build_jobs", "artifact_paths_json") {
		t.Fatal("build_jobs.artifact_paths_json missing")
	}
	if !gdb.Migrator().HasColumn("build_runs", "artifact_kind") {
		t.Fatal("build_runs.artifact_kind missing")
	}
}
