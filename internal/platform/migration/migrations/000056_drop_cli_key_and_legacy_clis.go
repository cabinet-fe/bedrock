package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000056_drop_cli_key_and_legacy_clis", upDropCLIKeyAndLegacyCLIs)
}

// legacyCLIs are the retired CLI runtimes: the harness session backend
// replaced the per-CLI agent execution path, so claude_code/codex stay
// uninstallable and their seed rows are removed.
var legacyCLIs = []string{"claude_code", "codex"}

// upDropCLIKeyAndLegacyCLIs removes the claude_code/codex CLI definitions and
// install sources, then drops the deprecated ai_agents.cli_key column
// (fresh databases never create it — 000009 no longer seeds either side).
func upDropCLIKeyAndLegacyCLIs(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	if err := db.Where("`key` IN ?", legacyCLIs).
		Delete(&cliRuntimeDefinitionMigrationModel{}).Error; err != nil {
		return err
	}
	if err := db.Where("cli_key IN ?", legacyCLIs).
		Delete(&cliInstallSourceMigrationModel{}).Error; err != nil {
		return err
	}

	agent := &aiAgentDropCLIKeyModel{}
	if !db.Migrator().HasTable(agent) {
		return nil
	}
	if db.Migrator().HasIndex(agent, "idx_ai_agents_cli_key") {
		if err := db.Migrator().DropIndex(agent, "idx_ai_agents_cli_key"); err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn(agent, "CliKey") {
		if err := db.Migrator().DropColumn(agent, "CliKey"); err != nil {
			return err
		}
	}
	return nil
}

type aiAgentDropCLIKeyModel struct {
	ID     uint   `gorm:"primaryKey"`
	CliKey string `gorm:"column:cli_key;size:40;index:idx_ai_agents_cli_key"`
}

func (aiAgentDropCLIKeyModel) TableName() string { return "ai_agents" }
