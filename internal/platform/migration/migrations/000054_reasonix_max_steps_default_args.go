package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000054_reasonix_max_steps_default_args", upReasonixMaxStepsDefaultArgs)
}

// upReasonixMaxStepsDefaultArgs gives unattended reasonix agent runs an explicit
// tool-call round budget so long tasks are not paused by automatic progress
// management mid-run. Only the untouched seeded value "run" is rewritten;
// customized default_args are preserved.
func upReasonixMaxStepsDefaultArgs(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	return db.Model(&cliRuntimeDefinitionMigrationModel{}).
		Where("`key` = ? AND default_args = ?", "reasonix", "run").
		Update("default_args", "run --max-steps 400").Error
}
