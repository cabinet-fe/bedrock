package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000027_build_job_agent_ids", upBuildJobAgentIDs)
}

// BuildJob.AgentID (single) is replaced by BuildJob.AgentIDs (JSON list).
func upBuildJobAgentIDs(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	job := &buildJobAgentIDsMigrationModel{}
	if !db.Migrator().HasColumn(job, "agent_ids") {
		if err := db.Migrator().AddColumn(job, "AgentIDs"); err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn(job, "agent_id") {
		var rows []struct {
			ID      uint
			AgentID *uint
		}
		if err := db.Model(job).Where("agent_id IS NOT NULL").Select("id", "agent_id").Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if r.AgentID == nil {
				continue
			}
			if err := db.Model(job).Where("id = ?", r.ID).
				Update("agent_ids", fmt.Sprintf("[%d]", *r.AgentID)).Error; err != nil {
				return err
			}
		}
		if err := db.Migrator().DropColumn(job, "AgentID"); err != nil {
			return err
		}
	}
	return nil
}

type buildJobAgentIDsMigrationModel struct {
	ID       uint   `gorm:"primaryKey"`
	AgentID  *uint  `gorm:"index"`
	AgentIDs string `gorm:"type:text"`
}

func (buildJobAgentIDsMigrationModel) TableName() string { return "build_jobs" }
