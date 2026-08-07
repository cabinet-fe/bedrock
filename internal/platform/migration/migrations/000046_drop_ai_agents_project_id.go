package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000046_drop_ai_agents_project_id", upDropAIAgentsProjectID)
}

// upDropAIAgentsProjectID removes ai_agents.project_id added by 000045.
// AiAgent is shared across projects and must not bind project_id (DESIGN §4.4 / D4).
func upDropAIAgentsProjectID(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	agent := &aiAgentDropProjectIDModel{}
	if !db.Migrator().HasTable(agent) {
		return nil
	}
	if db.Migrator().HasIndex(agent, "idx_ai_agents_project_id") {
		if err := db.Migrator().DropIndex(agent, "idx_ai_agents_project_id"); err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn(agent, "ProjectID") {
		if err := db.Migrator().DropColumn(agent, "ProjectID"); err != nil {
			return err
		}
	}
	return nil
}

type aiAgentDropProjectIDModel struct {
	ID        uint  `gorm:"primaryKey"`
	ProjectID *uint `gorm:"index:idx_ai_agents_project_id"`
}

func (aiAgentDropProjectIDModel) TableName() string { return "ai_agents" }
