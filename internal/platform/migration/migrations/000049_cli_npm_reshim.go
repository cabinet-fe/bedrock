package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000049_cli_npm_reshim", upCLINpmReshim)
}

// upCLINpmReshim appends `mise reshim` to the AI CLI npm templates: when npm
// is mise-managed, globally installed binaries do not get shims on their own
// and stay invisible on PATH right after a successful install/upgrade.
func upCLINpmReshim(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	updates := []struct{ key, pkg, label, binary string }{
		{"claude_code", "@anthropic-ai/claude-code", "claude_code", "claude"},
		{"opencode", "opencode-ai", "opencode", "opencode"},
		{"reasonix", "reasonix", "reasonix", "reasonix"},
		{"codex", "@openai/codex", "codex", "codex"},
	}
	for _, u := range updates {
		if err := db.Model(&cliRuntimeDefinitionMigrationModel{}).
			Where("`key` = ?", u.key).
			Updates(map[string]any{
				"detect_command":     "command -v " + u.binary + " && " + u.binary + " --version",
				"install_template":   npmCLIInstallTemplate(u.pkg, u.label),
				"upgrade_template":   npmCLIUpgradeTemplate(u.pkg, u.label),
				"uninstall_template": npmCLIUninstallTemplate(u.pkg),
			}).Error; err != nil {
			return err
		}
	}
	return nil
}
