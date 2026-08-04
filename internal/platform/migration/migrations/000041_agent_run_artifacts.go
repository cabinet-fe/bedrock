package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000041_agent_run_artifacts", upAgentRunArtifacts)
}

// upAgentRunArtifacts restores per-run snapshot archive columns on agent_runs.
func upAgentRunArtifacts(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	run := &agentRunArtifactsMigrationModel{}
	if !db.Migrator().HasTable(run) {
		return nil
	}
	if !db.Migrator().HasColumn(run, "ArtifactPath") {
		if err := db.Migrator().AddColumn(run, "ArtifactPath"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(run, "ArtifactKind") {
		if err := db.Migrator().AddColumn(run, "ArtifactKind"); err != nil {
			return err
		}
	}
	return nil
}

type agentRunArtifactsMigrationModel struct {
	ID           uint   `gorm:"primaryKey"`
	ArtifactPath string `gorm:"size:500"`
	ArtifactKind string `gorm:"size:20"`
}

func (agentRunArtifactsMigrationModel) TableName() string { return "agent_runs" }
