package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000055_harness_sessions", upHarnessSessions)
}

// upHarnessSessions adds the harness session columns: agent_runs gains the
// backend session binding (nullable unique: legacy runs keep NULL) plus the
// mirrored session status and final output; ai_agents gains model selection
// and approval mode. Existing cli_key values and legacy runs are not migrated.
func upHarnessSessions(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	run := &agentRunHarnessMigrationModel{}
	if db.Migrator().HasTable(run) {
		for _, column := range []string{"HarnessSessionID", "HarnessSessionStatus", "FinalOutput"} {
			if db.Migrator().HasColumn(run, column) {
				continue
			}
			if err := db.Migrator().AddColumn(run, column); err != nil {
				return err
			}
		}
		if !db.Migrator().HasIndex(run, "uidx_agent_runs_harness_session_id") {
			if err := db.Migrator().CreateIndex(run, "uidx_agent_runs_harness_session_id"); err != nil {
				return err
			}
		}
	}

	agent := &aiAgentHarnessMigrationModel{}
	if db.Migrator().HasTable(agent) {
		for _, column := range []string{"ModelProvider", "ModelID", "ApprovalMode"} {
			if db.Migrator().HasColumn(agent, column) {
				continue
			}
			if err := db.Migrator().AddColumn(agent, column); err != nil {
				return err
			}
		}
	}
	return nil
}

type agentRunHarnessMigrationModel struct {
	ID                   uint    `gorm:"primaryKey"`
	HarnessSessionID     *string `gorm:"column:harness_session_id;size:100;uniqueIndex:uidx_agent_runs_harness_session_id"`
	HarnessSessionStatus string  `gorm:"column:harness_session_status;size:40"`
	FinalOutput          string  `gorm:"column:final_output;type:text"`
}

func (agentRunHarnessMigrationModel) TableName() string { return "agent_runs" }

type aiAgentHarnessMigrationModel struct {
	ID            uint   `gorm:"primaryKey"`
	ModelProvider string `gorm:"column:model_provider;size:100"`
	ModelID       string `gorm:"column:model_id;size:200"`
	ApprovalMode  string `gorm:"column:approval_mode;size:20;not null;default:manual"`
}

func (aiAgentHarnessMigrationModel) TableName() string { return "ai_agents" }
