package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
)

func TestUpBugRemoveAIAndCleanup_DropsColumnsAndLegacyRows(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "m052.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	// Full Up already applied 000052 and 000061 (legacy bug tables merged into
	// project_issues): on fresh installs the legacy schema must be gone.
	if gdb.Migrator().HasTable("project_bugs") {
		t.Fatal("expected legacy project_bugs to be merged into project_issues by 000061")
	}

	// Recreate legacy pre-000052 state: tables (with AI columns), permission
	// resource, dispatch activity. (role_permissions bindings are not
	// recreated: migration 000059 dropped that table.)
	if err := gdb.Exec(`CREATE TABLE project_bugs (
		id integer primary key autoincrement, project_id integer not null, title text not null,
		description text, status text not null default 'open', severity text not null default 'normal',
		priority text not null default 'normal', assignee_id integer, repository_id integer, branch text,
		created_by integer, updated_by integer, created_at datetime, updated_at datetime, deleted_at datetime,
		ai_analysis TEXT, last_agent_run_id INTEGER)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`CREATE TABLE project_bug_activities (
		id integer primary key autoincrement, bug_id integer not null, action text not null,
		from_status text, to_status text, comment text, created_by integer, created_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO rbac_resources (code, full_code, type, title, enabled, sort_key, created_at, updated_at)
		VALUES ('execute', 'project_bugs:execute', 'action', '执行', 1, 50, datetime('now'), datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO project_bugs (project_id, title, status, severity, priority, ai_analysis, last_agent_run_id, created_by, updated_by, created_at, updated_at)
		VALUES (1, 'legacy bug', 'open', 'normal', 'normal', 'legacy analysis', 7, 1, 1, datetime('now'), datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO project_bug_activities (bug_id, action, comment, created_by, created_at)
		VALUES (1, 'agent_dispatch', '派发 Agent 排查', 1, datetime('now')),
		       (1, 'create', '创建缺陷', 1, datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}

	if err := upBugRemoveAIAndCleanup(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("upBugRemoveAIAndCleanup: %v", err)
	}

	// Column-drop behavior is only observable on legacy databases that still
	// carry the AI columns; on fresh installs 000061 drops the whole table.

	var resourceCount, dispatchCount, activityCount int64
	if err := gdb.Table("rbac_resources").Where("full_code = ?", "project_bugs:execute").Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 0 {
		t.Errorf("expected project_bugs:execute resource to be removed, got %d rows", resourceCount)
	}
	if err := gdb.Table("project_bug_activities").Where("action = ?", "agent_dispatch").Count(&dispatchCount).Error; err != nil {
		t.Fatal(err)
	}
	if dispatchCount != 0 {
		t.Errorf("expected agent_dispatch activities to be removed, got %d rows", dispatchCount)
	}
	if err := gdb.Table("project_bug_activities").Count(&activityCount).Error; err != nil {
		t.Fatal(err)
	}
	if activityCount != 1 {
		t.Errorf("expected non-dispatch activities preserved, got %d rows", activityCount)
	}
}
