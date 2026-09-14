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

	// Full Up already applied 000052: AI columns must be gone on fresh installs.
	for _, col := range []string{"ai_analysis", "last_agent_run_id"} {
		if gdb.Migrator().HasColumn("project_bugs", col) {
			t.Errorf("expected column %s on project_bugs to be dropped", col)
		}
	}

	// Recreate legacy pre-000052 state: columns, permission resource + binding, dispatch activity.
	// Column definitions are backticked to match how migration 000051 originally created them.
	if err := gdb.Exec("ALTER TABLE project_bugs ADD COLUMN `ai_analysis` TEXT").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("ALTER TABLE project_bugs ADD COLUMN `last_agent_run_id` INTEGER").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO rbac_resources (code, full_code, type, title, enabled, sort_key, created_at, updated_at)
		VALUES ('execute', 'project_bugs:execute', 'action', '执行', 1, 50, datetime('now'), datetime('now'))
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		INSERT INTO role_permissions (role_id, permission)
		VALUES (1, 'project_bugs:execute')
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

	for _, col := range []string{"ai_analysis", "last_agent_run_id"} {
		if gdb.Migrator().HasColumn("project_bugs", col) {
			t.Errorf("expected column %s on project_bugs to be dropped", col)
		}
	}

	var resourceCount, bindingCount, dispatchCount, activityCount int64
	if err := gdb.Table("rbac_resources").Where("full_code = ?", "project_bugs:execute").Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 0 {
		t.Errorf("expected project_bugs:execute resource to be removed, got %d rows", resourceCount)
	}
	if err := gdb.Table("role_permissions").Where("permission = ?", "project_bugs:execute").Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if bindingCount != 0 {
		t.Errorf("expected project_bugs:execute role bindings to be removed, got %d rows", bindingCount)
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
