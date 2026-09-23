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

// TestMigration000051_ProjectBugs runs the full chain: 000051 creates the
// legacy bug tables, which 000061 later merges into the unified
// project_issues tables (see TestMigration000061_UnifiedProjectIssues).
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

	// The bug domain now persists in the unified issue tables (000061).
	tables := []string{
		"project_issues",
		"project_issue_comments",
		"project_issue_attachments",
		"project_issue_activities",
	}

	for _, table := range tables {
		if !gdb.Migrator().HasTable(table) {
			t.Errorf("expected table %s to exist", table)
		}
	}

	expectedIssueCols := []string{
		"id", "project_id", "type", "title", "description", "status",
		"severity", "priority", "assignee_id", "repository_id",
		"branch", "tags", "created_by", "updated_by", "created_at", "updated_at", "deleted_at",
	}
	for _, col := range expectedIssueCols {
		if !gdb.Migrator().HasColumn("project_issues", col) {
			t.Errorf("expected column %s on project_issues", col)
		}
	}

	expectedCommentCols := []string{
		"id", "issue_id", "content", "created_by", "created_at", "updated_at", "deleted_at",
	}
	for _, col := range expectedCommentCols {
		if !gdb.Migrator().HasColumn("project_issue_comments", col) {
			t.Errorf("expected column %s on project_issue_comments", col)
		}
	}

	expectedAttachmentCols := []string{
		"id", "issue_id", "comment_id", "storage_object_id", "filename", "created_by", "created_at",
	}
	for _, col := range expectedAttachmentCols {
		if !gdb.Migrator().HasColumn("project_issue_attachments", col) {
			t.Errorf("expected column %s on project_issue_attachments", col)
		}
	}

	expectedActivityCols := []string{
		"id", "issue_id", "action", "field", "old_value", "new_value", "from_status", "to_status", "comment", "created_by", "created_at",
	}
	for _, col := range expectedActivityCols {
		if !gdb.Migrator().HasColumn("project_issue_activities", col) {
			t.Errorf("expected column %s on project_issue_activities", col)
		}
	}
}
