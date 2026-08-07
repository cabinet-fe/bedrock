package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000045_resource_project_id", upResourceProjectID)
}

// upResourceProjectID adds nullable project_id (+ index) to CI/CD resource tables
// and historically also ai_agents (removed by 000046; AiAgent is cross-project).
func upResourceProjectID(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	for _, item := range []struct {
		model any
		index string
	}{
		{&buildJobProjectIDMigrationModel{}, "idx_build_jobs_project_id"},
		{&scriptJobProjectIDMigrationModel{}, "idx_script_jobs_project_id"},
		{&buildPipelineProjectIDMigrationModel{}, "idx_build_pipelines_project_id"},
		{&aiAgentProjectIDMigrationModel{}, "idx_ai_agents_project_id"},
	} {
		if !db.Migrator().HasTable(item.model) {
			continue
		}
		if !db.Migrator().HasColumn(item.model, "ProjectID") {
			if err := db.Migrator().AddColumn(item.model, "ProjectID"); err != nil {
				return err
			}
		}
		if !db.Migrator().HasIndex(item.model, item.index) {
			if err := db.Migrator().CreateIndex(item.model, item.index); err != nil {
				return err
			}
		}
	}
	return nil
}

type buildJobProjectIDMigrationModel struct {
	ID        uint  `gorm:"primaryKey"`
	ProjectID *uint `gorm:"index:idx_build_jobs_project_id"`
}

func (buildJobProjectIDMigrationModel) TableName() string { return "build_jobs" }

type scriptJobProjectIDMigrationModel struct {
	ID        uint  `gorm:"primaryKey"`
	ProjectID *uint `gorm:"index:idx_script_jobs_project_id"`
}

func (scriptJobProjectIDMigrationModel) TableName() string { return "script_jobs" }

type buildPipelineProjectIDMigrationModel struct {
	ID        uint  `gorm:"primaryKey"`
	ProjectID *uint `gorm:"index:idx_build_pipelines_project_id"`
}

func (buildPipelineProjectIDMigrationModel) TableName() string { return "build_pipelines" }

type aiAgentProjectIDMigrationModel struct {
	ID        uint  `gorm:"primaryKey"`
	ProjectID *uint `gorm:"index:idx_ai_agents_project_id"`
}

func (aiAgentProjectIDMigrationModel) TableName() string { return "ai_agents" }
