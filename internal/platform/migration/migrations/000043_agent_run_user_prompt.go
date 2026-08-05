package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000043_agent_run_user_prompt", upAgentRunUserPrompt)
}

// upAgentRunUserPrompt adds optional per-run user_prompt on agent_runs.
func upAgentRunUserPrompt(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	run := &agentRunUserPromptMigrationModel{}
	if !db.Migrator().HasTable(run) {
		return nil
	}
	if db.Migrator().HasColumn(run, "UserPrompt") {
		return nil
	}
	return db.Migrator().AddColumn(run, "UserPrompt")
}

type agentRunUserPromptMigrationModel struct {
	ID         uint   `gorm:"primaryKey"`
	UserPrompt string `gorm:"type:text"`
}

func (agentRunUserPromptMigrationModel) TableName() string { return "agent_runs" }
