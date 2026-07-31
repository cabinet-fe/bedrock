package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000037_build_pipelines", upBuildPipelines)
}

func upBuildPipelines(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	models := []interface{}{
		&buildPipelineMigrationModel{},
		&pipelineRunMigrationModel{},
		&pipelineStageRunMigrationModel{},
		&pipelineWebhookDeliveryMigrationModel{},
	}
	for _, m := range models {
		if db.Migrator().HasTable(m) {
			continue
		}
		if err := db.Migrator().CreateTable(m); err != nil {
			return err
		}
	}
	return nil
}

type buildPipelineMigrationModel struct {
	ID                 uint      `gorm:"primaryKey"`
	Name               string    `gorm:"size:100;not null"`
	Description        string    `gorm:"size:500"`
	Enabled            bool      `gorm:"not null"`
	GraphJSON          string    `gorm:"type:text"`
	TriggerManual      bool      `gorm:"not null"`
	TriggerWebhook     bool      `gorm:"not null;default:false"`
	TriggerCron        bool      `gorm:"not null;default:false"`
	WebhookSecret      string    `gorm:"size:64"`
	WebhookType        string    `gorm:"size:20;default:generic"`
	WebhookRefPath     string    `gorm:"size:300"`
	WebhookCommitPath  string    `gorm:"size:300"`
	WebhookMessagePath string    `gorm:"size:300"`
	CronExpression     string    `gorm:"size:100"`
	CronTimezone       string    `gorm:"size:100;default:UTC"`
	IsPublic           bool      `gorm:"not null;default:false;index"`
	CreatedBy          uint      `gorm:"index"`
	CreatedAt          time.Time `gorm:""`
	UpdatedAt          time.Time `gorm:""`
}

func (buildPipelineMigrationModel) TableName() string { return "build_pipelines" }

type pipelineRunMigrationModel struct {
	ID              uint       `gorm:"primaryKey"`
	BuildPipelineID uint       `gorm:"uniqueIndex:idx_pipeline_run_num;not null"`
	RunNumber       int        `gorm:"uniqueIndex:idx_pipeline_run_num;not null"`
	Status          string     `gorm:"size:20;not null;default:queued"`
	TriggerType     string     `gorm:"size:20"`
	TriggeredBy     uint       `gorm:""`
	SnapshotJSON    string     `gorm:"type:text"`
	ErrorMessage    string     `gorm:"type:text"`
	StartedAt       *time.Time `gorm:""`
	FinishedAt      *time.Time `gorm:""`
	CreatedAt       time.Time  `gorm:""`
}

func (pipelineRunMigrationModel) TableName() string { return "pipeline_runs" }

type pipelineStageRunMigrationModel struct {
	ID            uint       `gorm:"primaryKey"`
	PipelineRunID uint       `gorm:"index;not null"`
	NodeID        string     `gorm:"size:100;not null"`
	BuildJobID    uint       `gorm:"index;not null"`
	BuildRunID    *uint      `gorm:"index"`
	Status        string     `gorm:"size:20;not null;default:pending"`
	ErrorMessage  string     `gorm:"type:text"`
	StartedAt     *time.Time `gorm:""`
	FinishedAt    *time.Time `gorm:""`
	CreatedAt     time.Time  `gorm:""`
}

func (pipelineStageRunMigrationModel) TableName() string { return "pipeline_stage_runs" }

type pipelineWebhookDeliveryMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	BuildPipelineID uint      `gorm:"uniqueIndex:idx_pipeline_wh_delivery;not null"`
	DeliveryKey     string    `gorm:"size:200;uniqueIndex:idx_pipeline_wh_delivery;not null"`
	CreatedAt       time.Time `gorm:""`
}

func (pipelineWebhookDeliveryMigrationModel) TableName() string {
	return "pipeline_webhook_deliveries"
}
