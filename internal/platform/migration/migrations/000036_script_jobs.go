package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000036_script_jobs", upScriptJobs)
}

func upScriptJobs(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	models := []interface{}{
		&scriptJobMigrationModel{},
		&scriptRunMigrationModel{},
		&scriptWebhookDeliveryMigrationModel{},
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

type scriptJobMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	Name            string    `gorm:"size:100;not null"`
	Description     string    `gorm:"size:500"`
	Enabled         bool      `gorm:"not null"`
	ScriptType      string    `gorm:"size:20;default:bash"`
	Script          string    `gorm:"type:text"`
	WorkDir         string    `gorm:"size:300"`
	EnvVarNamesJSON string    `gorm:"type:text"`
	EnvVarsCipher   string    `gorm:"type:text"`
	TriggerManual   bool      `gorm:"not null"`
	TriggerWebhook  bool      `gorm:"not null;default:false"`
	TriggerCron     bool      `gorm:"not null;default:false"`
	WebhookSecret   string    `gorm:"size:64"`
	WebhookType     string    `gorm:"size:20;default:generic"`
	CronExpression  string    `gorm:"size:100"`
	CronTimezone    string    `gorm:"size:100;default:UTC"`
	IsPublic        bool      `gorm:"not null;default:false;index"`
	CreatedBy       uint      `gorm:"index"`
	CreatedAt       time.Time `gorm:""`
	UpdatedAt       time.Time `gorm:""`
}

func (scriptJobMigrationModel) TableName() string { return "script_jobs" }

type scriptRunMigrationModel struct {
	ID           uint       `gorm:"primaryKey"`
	ScriptJobID  uint       `gorm:"uniqueIndex:idx_script_job_run_num;not null"`
	RunNumber    int        `gorm:"uniqueIndex:idx_script_job_run_num;not null"`
	Status       string     `gorm:"size:20;not null;default:queued"`
	Stage        string     `gorm:"size:20;not null;default:pending"`
	TriggerType  string     `gorm:"size:20"`
	TriggeredBy  uint       `gorm:""`
	LogPath      string     `gorm:"size:500"`
	DurationMs   int64      `gorm:""`
	ErrorMessage string     `gorm:"type:text"`
	SnapshotJSON string     `gorm:"type:text"`
	StartedAt    *time.Time `gorm:""`
	FinishedAt   *time.Time `gorm:""`
	CreatedAt    time.Time  `gorm:""`
}

func (scriptRunMigrationModel) TableName() string { return "script_runs" }

type scriptWebhookDeliveryMigrationModel struct {
	ID          uint      `gorm:"primaryKey"`
	ScriptJobID uint      `gorm:"uniqueIndex:idx_script_wh_delivery;not null"`
	DeliveryKey string    `gorm:"size:200;uniqueIndex:idx_script_wh_delivery;not null"`
	CreatedAt   time.Time `gorm:""`
}

func (scriptWebhookDeliveryMigrationModel) TableName() string {
	return "script_webhook_deliveries"
}
