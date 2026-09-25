package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
)

// TestMigration000061_UnifiedProjectIssues verifies the fresh-install schema
// (full chain) plus the data migration path: legacy rows are copied into
// project_issues with child remapping and legacy tables dropped.
func TestMigration000061_UnifiedProjectIssues(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m061.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	for _, table := range []string{
		"project_issues", "project_issue_comments", "project_issue_attachments",
		"project_issue_activities", "project_issue_watchers", "project_iterations",
	} {
		if !gdb.Migrator().HasTable(table) {
			t.Errorf("expected table %s to exist", table)
		}
	}
	for _, table := range []string{
		"requirements", "requirement_comments", "requirement_attachments",
		"project_bugs", "project_bug_comments", "project_bug_attachments", "project_bug_activities",
	} {
		if gdb.Migrator().HasTable(table) {
			t.Errorf("expected legacy table %s to be dropped", table)
		}
	}

	// bug_status dictionary seeded alongside requirement_status.
	var bugStatusCount int64
	if err := gdb.Raw(`
		SELECT count(*) FROM dict_items di JOIN dictionaries d ON di.dictionary_id = d.id
		WHERE d.code = 'bug_status'`).Scan(&bugStatusCount).Error; err != nil {
		t.Fatal(err)
	}
	if bugStatusCount != 5 {
		t.Fatalf("bug_status items = %d, want 5", bugStatusCount)
	}

	// --- Data migration path: recreate legacy shapes and re-run the step. ---
	legacyDDL := []string{
		`CREATE TABLE requirements (id integer primary key autoincrement, project_id integer not null, title text not null, description text, status text not null, priority text not null default 'normal', assignee_id integer, repository_id integer, tags text, created_by integer, updated_by integer, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE project_bugs (id integer primary key autoincrement, project_id integer not null, title text not null, description text, status text not null default 'open', severity text not null default 'normal', priority text not null default 'normal', assignee_id integer, repository_id integer, branch text, created_by integer, updated_by integer, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE requirement_comments (id integer primary key autoincrement, requirement_id integer not null, content text not null, created_by integer, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE project_bug_comments (id integer primary key autoincrement, bug_id integer not null, content text not null, created_by integer, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE requirement_attachments (id integer primary key autoincrement, requirement_id integer not null, storage_object_id integer not null, filename text not null, created_by integer, created_at datetime)`,
		`CREATE TABLE project_bug_attachments (id integer primary key autoincrement, bug_id integer not null, storage_object_id integer not null, filename text not null, created_by integer, created_at datetime)`,
		`CREATE TABLE project_bug_activities (id integer primary key autoincrement, bug_id integer not null, action text not null, from_status text, to_status text, comment text, created_by integer, created_at datetime)`,
	}
	for _, ddl := range legacyDDL {
		if err := gdb.Exec(ddl).Error; err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}

	seed := []string{
		`INSERT INTO requirements (project_id, title, status, tags, created_by, created_at, updated_at) VALUES (1, 'req-1', 'doing', 'v1', 2, '2026-01-01 10:00:00', '2026-01-02 10:00:00')`,
		`INSERT INTO project_bugs (project_id, title, status, severity, branch, created_by, created_at, updated_at) VALUES (1, 'bug-1', 'resolved', 'high', 'main', 3, '2026-01-03 10:00:00', '2026-01-04 10:00:00')`,
		`INSERT INTO requirement_comments (requirement_id, content, created_by, created_at, updated_at) VALUES (1, 'req comment', 2, '2026-01-01 11:00:00', '2026-01-01 11:00:00')`,
		`INSERT INTO project_bug_comments (bug_id, content, created_by, created_at, updated_at) VALUES (1, 'bug comment', 3, '2026-01-03 11:00:00', '2026-01-03 11:00:00')`,
		`INSERT INTO requirement_attachments (requirement_id, storage_object_id, filename, created_by, created_at) VALUES (1, 101, 'req.png', 2, '2026-01-01 12:00:00')`,
		`INSERT INTO project_bug_attachments (bug_id, storage_object_id, filename, created_by, created_at) VALUES (1, 102, 'bug.log', 3, '2026-01-03 12:00:00')`,
		`INSERT INTO project_bug_activities (bug_id, action, from_status, to_status, created_by, created_at) VALUES (1, 'status_change', 'open', 'resolved', 3, '2026-01-04 09:00:00')`,
	}
	for _, stmt := range seed {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
	}

	if err := upUnifiedProjectIssues(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("upUnifiedProjectIssues: %v", err)
	}

	type issueRow struct {
		ID       uint
		Type     string
		Title    string
		Status   string
		Severity string
		Tags     string
	}
	var issues []issueRow
	if err := gdb.Raw("SELECT id, type, title, status, severity, tags FROM project_issues ORDER BY id").Scan(&issues).Error; err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("migrated issues = %d, want 2", len(issues))
	}
	byTitle := map[string]issueRow{}
	for _, row := range issues {
		byTitle[row.Title] = row
	}
	if r := byTitle["req-1"]; r.Type != "requirement" || r.Tags != "v1" || r.Severity != "" {
		t.Fatalf("requirement row mismatch: %+v", r)
	}
	if b := byTitle["bug-1"]; b.Type != "bug" || b.Severity != "high" || b.Status != "resolved" {
		t.Fatalf("bug row mismatch: %+v", b)
	}

	var commentCount, attachmentCount, activityCount int64
	for _, probe := range []struct {
		table string
		count *int64
		where string
	}{
		{"project_issue_comments", &commentCount, "content IN ('req comment', 'bug comment')"},
		{"project_issue_attachments", &attachmentCount, "filename IN ('req.png', 'bug.log')"},
		{"project_issue_activities", &activityCount, "1=1"},
	} {
		if err := gdb.Raw("SELECT count(*) FROM " + probe.table + " WHERE " + probe.where).Scan(probe.count).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 2 requirement activities (synthesized create + comment? no: comments are
	// separate rows) — requirement create is synthesized, bug has its
	// status_change carried over.
	if commentCount != 2 {
		t.Fatalf("migrated comments = %d, want 2", commentCount)
	}
	if attachmentCount != 2 {
		t.Fatalf("migrated attachments = %d, want 2", attachmentCount)
	}
	if activityCount != 2 {
		t.Fatalf("migrated activities = %d, want 2 (synthesized create + status_change)", activityCount)
	}

	// Children point at remapped issue IDs, not the legacy 1/1 collision.
	var orphanChildren int64
	if err := gdb.Raw(`
		SELECT (SELECT count(*) FROM project_issue_comments c LEFT JOIN project_issues i ON c.issue_id = i.id WHERE i.id IS NULL)
		     + (SELECT count(*) FROM project_issue_attachments a LEFT JOIN project_issues i ON a.issue_id = i.id WHERE i.id IS NULL)
		     + (SELECT count(*) FROM project_issue_activities ac LEFT JOIN project_issues i ON ac.issue_id = i.id WHERE i.id IS NULL)
	`).Scan(&orphanChildren).Error; err != nil {
		t.Fatal(err)
	}
	if orphanChildren != 0 {
		t.Fatalf("orphan children = %d, want 0", orphanChildren)
	}

	for _, table := range []string{"requirements", "project_bugs", "project_bug_comments", "project_bug_activities"} {
		if gdb.Migrator().HasTable(table) {
			t.Errorf("legacy table %s still present after data migration", table)
		}
	}
}
