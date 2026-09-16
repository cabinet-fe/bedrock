package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000058_agent_inject_default_prompt", upAgentInjectDefaultPrompt)
}

// upAgentInjectDefaultPrompt adds ai_agents.inject_default_prompt: when true
// (default), bedrock injects the workspace/.env hints into compiled agent
// definitions and run prompts.
func upAgentInjectDefaultPrompt(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	agent := &aiAgentInjectDefaultPromptModel{}
	if !db.Migrator().HasTable(agent) {
		return nil
	}
	if db.Migrator().HasColumn(agent, "InjectDefaultPrompt") {
		return nil
	}
	return db.Migrator().AddColumn(agent, "InjectDefaultPrompt")
}

type aiAgentInjectDefaultPromptModel struct {
	ID                  uint `gorm:"primaryKey"`
	InjectDefaultPrompt bool `gorm:"column:inject_default_prompt;not null;default:true"`
}

func (aiAgentInjectDefaultPromptModel) TableName() string { return "ai_agents" }
