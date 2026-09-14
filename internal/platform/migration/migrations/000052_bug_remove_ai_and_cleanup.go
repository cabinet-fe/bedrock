package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000052_bug_remove_ai_and_cleanup", upBugRemoveAIAndCleanup)
}

// upBugRemoveAIAndCleanup removes leftover AI artifacts from the bug module:
// the ai_analysis / last_agent_run_id columns, the project_bugs:execute
// permission resource with its role bindings, and agent_dispatch activities.
func upBugRemoveAIAndCleanup(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	return db.Transaction(func(tx *gorm.DB) error {
		bug := &projectBugMigrationModel{}
		for _, col := range []string{"ai_analysis", "last_agent_run_id"} {
			if tx.Migrator().HasColumn(bug, col) {
				if err := tx.Migrator().DropColumn(bug, col); err != nil {
					return err
				}
			}
		}

		if err := tx.Exec("DELETE FROM role_permissions WHERE permission = ?", "project_bugs:execute").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM rbac_resources WHERE full_code = ?", "project_bugs:execute").Error; err != nil {
			return err
		}

		if err := tx.Exec("DELETE FROM project_bug_activities WHERE action = ?", "agent_dispatch").Error; err != nil {
			return err
		}
		return nil
	})
}
