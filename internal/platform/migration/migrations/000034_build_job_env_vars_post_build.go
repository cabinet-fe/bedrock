package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000034_build_job_env_vars_post_build", upBuildJobEnvVarsPostBuild)
}

func upBuildJobEnvVarsPostBuild(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	job := &buildJobEnvVarsPostBuildMigrationModel{}
	if !db.Migrator().HasColumn(job, "env_vars_cipher") {
		if err := db.Migrator().AddColumn(job, "EnvVarsCipher"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(job, "post_build_script") {
		if err := db.Migrator().AddColumn(job, "PostBuildScript"); err != nil {
			return err
		}
	}
	return nil
}

type buildJobEnvVarsPostBuildMigrationModel struct {
	ID              uint   `gorm:"primaryKey"`
	EnvVarsCipher   string `gorm:"type:text"`
	PostBuildScript string `gorm:"type:text"`
}

func (buildJobEnvVarsPostBuildMigrationModel) TableName() string { return "build_jobs" }
