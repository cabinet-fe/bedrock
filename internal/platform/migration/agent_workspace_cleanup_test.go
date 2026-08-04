package migration_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"

	aimodel "bedrock/internal/ai/model"
	airepository "bedrock/internal/ai/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestAgentPersistentWorkspaceUpgradeCleanupResumesAndPreservesBuildArtifacts(t *testing.T) {
	gdb, repo, agent, run := setupLegacyAgentWorkspaceUpgrade(t)
	root := safeCleanupTempDir(t)
	workspace := filepath.Join(root, "workspace")
	artifacts := filepath.Join(root, "artifacts")
	agentRoot := filepath.Join(workspace, "agents", agentDirName(agent.ID))
	runsDir := filepath.Join(agentRoot, "runs")
	mustWriteFile(t, filepath.Join(runsDir, runDirName(run.ID), "output", "old.txt"), "old")
	mustWriteFile(t, filepath.Join(agentRoot, "keep.txt"), "keep")

	agentArchive := filepath.Join(artifacts, agentDirName(agent.ID), runArchiveName(run.ID, ".tar.gz"))
	mustWriteFile(t, agentArchive, "agent archive")
	if err := gdb.Table("agent_runs").Where("id = ?", run.ID).Update("artifact_path", agentArchive).Error; err != nil {
		t.Fatal(err)
	}
	otherAgentFile := filepath.Join(artifacts, agentDirName(agent.ID), "notes.txt")
	mustWriteFile(t, otherAgentFile, "keep")
	buildArtifact := filepath.Join(artifacts, "repo-7", "job-9", "run-11", "build-003.tar.gz")
	mustWriteFile(t, buildArtifact, "build archive")

	cleanup, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err != nil {
		t.Fatalf("prepare cleanup: %v", err)
	}
	if _, err := os.Lstat(runsDir); !os.IsNotExist(err) {
		t.Fatalf("legacy runs should be isolated, err=%v", err)
	}
	if _, err := os.Lstat(agentArchive); !os.IsNotExist(err) {
		t.Fatalf("legacy Agent archive should be isolated, err=%v", err)
	}
	for _, preserved := range []string{filepath.Join(agentRoot, "keep.txt"), otherAgentFile, buildArtifact} {
		if _, err := os.Stat(preserved); err != nil {
			t.Fatalf("preserved file missing before migration %s: %v", preserved, err)
		}
	}

	// Simulate a crash after isolation but before the schema migration.
	resumed, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err != nil {
		t.Fatalf("resume cleanup: %v", err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("apply 000018: %v", err)
	}
	if err := resumed.Finalize(); err != nil {
		t.Fatalf("finalize cleanup: %v", err)
	}
	if err := cleanup.Finalize(); err != nil {
		t.Fatalf("repeat finalize cleanup: %v", err)
	}

	for _, column := range []string{"artifact_format", "max_artifacts"} {
		if gdb.Migrator().HasColumn("ai_agents", column) {
			t.Fatalf("ai_agents.%s still exists", column)
		}
	}
	if !gdb.Migrator().HasColumn("ai_agents", "output_dir") {
		t.Fatal("ai_agents.output_dir was removed")
	}
	if gdb.Migrator().HasColumn("agent_runs", "artifact_path") {
		t.Fatal("agent_runs.artifact_path still exists")
	}
	if !gdb.Migrator().HasColumn("agent_runs", "work_dir") {
		t.Fatal("agent_runs.work_dir was removed")
	}
	got, err := repo.FindRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkDir != run.WorkDir {
		t.Fatalf("work_dir=%q want=%q", got.WorkDir, run.WorkDir)
	}
	for _, preserved := range []string{filepath.Join(agentRoot, "keep.txt"), otherAgentFile, buildArtifact} {
		if _, err := os.Stat(preserved); err != nil {
			t.Fatalf("preserved file missing after migration %s: %v", preserved, err)
		}
	}
	for _, quarantine := range []string{
		filepath.Join(workspace, ".bedrock-000018-agent-workspace-quarantine"),
		filepath.Join(artifacts, ".bedrock-000018-agent-artifact-quarantine"),
	} {
		if _, err := os.Lstat(quarantine); !os.IsNotExist(err) {
			t.Fatalf("quarantine should be removed %s, err=%v", quarantine, err)
		}
	}

	again, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err != nil {
		t.Fatalf("prepare after applied migration: %v", err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	if err := again.Finalize(); err != nil {
		t.Fatalf("idempotent finalize: %v", err)
	}
}

func TestAgentPersistentWorkspaceCleanupRejectsArtifactPathEscape(t *testing.T) {
	gdb, _, agent, run := setupLegacyAgentWorkspaceUpgrade(t)
	root := safeCleanupTempDir(t)
	workspace := filepath.Join(root, "workspace")
	artifacts := filepath.Join(root, "artifacts")
	runsDir := filepath.Join(workspace, "agents", agentDirName(agent.ID), "runs")
	mustWriteFile(t, filepath.Join(runsDir, "keep.txt"), "old")
	outside := filepath.Join(t.TempDir(), runArchiveName(run.ID, ".zip"))
	mustWriteFile(t, outside, "outside")
	if err := gdb.Table("agent_runs").Where("id = ?", run.ID).Update("artifact_path", outside).Error; err != nil {
		t.Fatal(err)
	}

	_, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err == nil || !strings.Contains(err.Error(), "strictly bounded") {
		t.Fatalf("expected bounded artifact_path error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(runsDir, "keep.txt")); statErr != nil {
		t.Fatalf("workspace changed after validation failure: %v", statErr)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("outside file changed after validation failure: %v", statErr)
	}
}

func TestAgentPersistentWorkspaceCleanupRejectsSymlinkAncestor(t *testing.T) {
	gdb, _, agent, _ := setupLegacyAgentWorkspaceUpgrade(t)
	root := safeCleanupTempDir(t)
	workspace := filepath.Join(root, "workspace")
	artifacts := filepath.Join(root, "artifacts")
	outsideAgent := filepath.Join(t.TempDir(), "outside-agent")
	mustWriteFile(t, filepath.Join(outsideAgent, "runs", "old.txt"), "old")
	agentsRoot := filepath.Join(workspace, "agents")
	if err := os.MkdirAll(agentsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agentsRoot, agentDirName(agent.ID))
	if err := os.Symlink(outsideAgent, link); err != nil {
		t.Fatal(err)
	}

	_, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err == nil || !strings.Contains(err.Error(), "symlink path component") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outsideAgent, "runs", "old.txt")); statErr != nil {
		t.Fatalf("symlink target changed: %v", statErr)
	}
}

func TestAgentPersistentWorkspaceCleanupRequiresLegacySchema(t *testing.T) {
	gdb := openCleanupTestDB(t)
	if err := migration.EnsureSchemaMigrationsTable(gdb); err != nil {
		t.Fatal(err)
	}
	root := safeCleanupTempDir(t)
	workspace := filepath.Join(root, "workspace")
	artifacts := filepath.Join(root, "artifacts")
	runsFile := filepath.Join(workspace, "agents", "agent-1", "runs", "keep.txt")
	archive := filepath.Join(artifacts, "agent-1", "run-1.zip")
	mustWriteFile(t, runsFile, "keep")
	mustWriteFile(t, archive, "keep")

	cleanup, err := migration.PrepareAgentPersistentWorkspaceCleanup(gdb, workspace, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{runsFile, archive} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fresh database cleanup touched %s: %v", path, err)
		}
	}
}

func setupLegacyAgentWorkspaceUpgrade(t *testing.T) (*gorm.DB, *airepository.AIRepository, *aimodel.AiAgent, *aimodel.AgentRun) {
	t.Helper()
	gdb := openCleanupTestDB(t)
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("initial migrations: %v", err)
	}
	legacyAgent := &legacyAgentArtifactColumns{}
	for _, field := range []string{"ArtifactFormat", "MaxArtifacts"} {
		if !gdb.Migrator().HasColumn(legacyAgent, field) {
			if err := gdb.Migrator().AddColumn(legacyAgent, field); err != nil {
				t.Fatalf("restore ai_agents.%s: %v", field, err)
			}
		}
	}
	legacyRun := &legacyRunArtifactColumn{}
	// Full Up may already include 000041 which re-adds artifact_path; only restore when absent
	// so this fixture still looks like a pre-000018 schema for cleanup.
	if !gdb.Migrator().HasColumn(legacyRun, "ArtifactPath") {
		if err := gdb.Migrator().AddColumn(legacyRun, "ArtifactPath"); err != nil {
			t.Fatalf("restore agent_runs.artifact_path: %v", err)
		}
	}
	if err := gdb.Exec("DELETE FROM schema_migrations WHERE version = ?", migration.AgentPersistentWorkspaceMigrationVersion).Error; err != nil {
		t.Fatal(err)
	}

	repo := airepository.NewAIRepository(gdb)
	agent := &aimodel.AiAgent{
		Name: "legacy", Enabled: true, CliKey: "claude_code",
		SkillIDsJSON: "[]", OutputDir: "output", TimeoutSec: 30,
	}
	if err := repo.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}
	run := &aimodel.AgentRun{
		AgentID: agent.ID, TriggerType: aimodel.TriggerManual, Status: aimodel.JobSuccess,
		WorkDir: filepath.Join("workspace", "agents", agentDirName(agent.ID)),
	}
	if err := repo.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	return gdb, repo, agent, run
}

func openCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "cleanup.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func safeCleanupTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".agent-cleanup-test-")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(absolute) })
	return absolute
}

func agentDirName(id uint) string {
	return "agent-" + uintString(id)
}

func runDirName(id uint) string {
	return "run-" + uintString(id)
}

func runArchiveName(id uint, extension string) string {
	return runDirName(id) + extension
}

func uintString(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

type legacyAgentArtifactColumns struct {
	ID             uint   `gorm:"primaryKey"`
	OutputDir      string `gorm:"size:200;not null;default:output"`
	ArtifactFormat string `gorm:"size:20;not null;default:gzip"`
	MaxArtifacts   int    `gorm:"not null;default:10"`
}

func (legacyAgentArtifactColumns) TableName() string { return "ai_agents" }

type legacyRunArtifactColumn struct {
	ID           uint   `gorm:"primaryKey"`
	ArtifactPath string `gorm:"size:500"`
}

func (legacyRunArtifactColumn) TableName() string { return "agent_runs" }
