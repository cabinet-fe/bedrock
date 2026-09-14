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

func TestMigration000051_ProjectBugs(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m051.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	tables := []string{
		"project_bugs",
		"project_bug_comments",
		"project_bug_attachments",
		"project_bug_activities",
	}

	for _, table := range tables {
		if !gdb.Migrator().HasTable(table) {
			t.Errorf("expected table %s to exist", table)
		}
	}

	expectedBugCols := []string{
		"id", "project_id", "title", "description", "status",
		"severity", "priority", "assignee_id", "repository_id",
		"branch", "created_by",
		"updated_by", "created_at", "updated_at", "deleted_at",
	}
	for _, col := range expectedBugCols {
		if !gdb.Migrator().HasColumn("project_bugs", col) {
			t.Errorf("expected column %s on project_bugs", col)
		}
	}

	expectedCommentCols := []string{
		"id", "bug_id", "content", "created_by", "created_at", "updated_at", "deleted_at",
	}
	for _, col := range expectedCommentCols {
		if !gdb.Migrator().HasColumn("project_bug_comments", col) {
			t.Errorf("expected column %s on project_bug_comments", col)
		}
	}

	expectedAttachmentCols := []string{
		"id", "bug_id", "storage_object_id", "filename", "created_by", "created_at",
	}
	for _, col := range expectedAttachmentCols {
		if !gdb.Migrator().HasColumn("project_bug_attachments", col) {
			t.Errorf("expected column %s on project_bug_attachments", col)
		}
	}

	expectedActivityCols := []string{
		"id", "bug_id", "action", "from_status", "to_status", "comment", "created_by", "created_at",
	}
	for _, col := range expectedActivityCols {
		if !gdb.Migrator().HasColumn("project_bug_activities", col) {
			t.Errorf("expected column %s on project_bug_activities", col)
		}
	}
}
