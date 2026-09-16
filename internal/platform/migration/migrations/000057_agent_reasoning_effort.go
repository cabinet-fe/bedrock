package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000057_agent_reasoning_effort", upAgentReasoningEffort)
}

// upAgentReasoningEffort adds ai_agents.reasoning_effort: the default
// reasoning effort applied to the agent's model via the workspace
// opencode.json model options ("" = model default). Valid values are
// constrained by the chosen model's reasoning_efforts options, not by this
// column.
func upAgentReasoningEffort(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	agent := &aiAgentReasoningEffortModel{}
	if !db.Migrator().HasTable(agent) {
		return nil
	}
	if db.Migrator().HasColumn(agent, "ReasoningEffort") {
		return nil
	}
	return db.Migrator().AddColumn(agent, "ReasoningEffort")
}

type aiAgentReasoningEffortModel struct {
	ID             uint   `gorm:"primaryKey"`
	ReasoningEffort string `gorm:"column:reasoning_effort;size:40;not null;default:''"`
}

func (aiAgentReasoningEffortModel) TableName() string { return "ai_agents" }
