package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000048_cli_restore_npm", upCLIRestoreNPM)
}

// upCLIRestoreNPM puts AI CLIs back on npm -g / PATH detect.
// 000047 previously rewrote CLI scripts onto mise; language runtimes stay on mise.
func upCLIRestoreNPM(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	updates := []struct {
		key, pkg, label, binary string
	}{
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
