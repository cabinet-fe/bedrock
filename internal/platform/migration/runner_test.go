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
	for _, column := range []string{"artifact_path", "artifact_kind", "work_dir"} {
		if !gdb.Migrator().HasColumn("agent_runs", column) {
			t.Fatalf("agent_runs.%s missing", column)
		}
	}
	if !gdb.Migrator().HasColumn("build_jobs", "artifact_paths_json") {
		t.Fatal("build_jobs.artifact_paths_json missing")
	}
	if !gdb.Migrator().HasColumn("build_runs", "artifact_kind") {
		t.Fatal("build_runs.artifact_kind missing")
	}
	for _, table := range []string{"build_jobs", "script_jobs", "build_pipelines", "ai_agents"} {
		if !gdb.Migrator().HasColumn(table, "project_id") {
			t.Fatalf("%s.project_id missing", table)
		}
	}
	for _, check := range []struct {
		model any
		index string
	}{
		{runnerProjectIDIndexBuildJobs{}, "idx_build_jobs_project_id"},
		{runnerProjectIDIndexScriptJobs{}, "idx_script_jobs_project_id"},
		{runnerProjectIDIndexBuildPipelines{}, "idx_build_pipelines_project_id"},
		{runnerProjectIDIndexAIAgents{}, "idx_ai_agents_project_id"},
	} {
		if !gdb.Migrator().HasIndex(check.model, check.index) {
			t.Fatalf("%s missing", check.index)
		}
	}
}

type runnerProjectIDIndexBuildJobs struct {
	ProjectID *uint `gorm:"index:idx_build_jobs_project_id"`
}

func (runnerProjectIDIndexBuildJobs) TableName() string { return "build_jobs" }

type runnerProjectIDIndexScriptJobs struct {
	ProjectID *uint `gorm:"index:idx_script_jobs_project_id"`
}

func (runnerProjectIDIndexScriptJobs) TableName() string { return "script_jobs" }

type runnerProjectIDIndexBuildPipelines struct {
	ProjectID *uint `gorm:"index:idx_build_pipelines_project_id"`
}

func (runnerProjectIDIndexBuildPipelines) TableName() string { return "build_pipelines" }

type runnerProjectIDIndexAIAgents struct {
	ProjectID *uint `gorm:"index:idx_ai_agents_project_id"`
}

func (runnerProjectIDIndexAIAgents) TableName() string { return "ai_agents" }
