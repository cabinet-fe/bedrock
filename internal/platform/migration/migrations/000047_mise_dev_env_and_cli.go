package migrations

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000047_mise_dev_env_and_cli", upMiseDevEnvAndCLI)
}

const misePathPrelude = `export PATH="$HOME/.local/bin:${MISE_DATA_DIR:-$HOME/.local/share/mise}/shims:$PATH"; export MISE_YES=1; command -v mise >/dev/null 2>&1 || { echo 'mise is required'; exit 1; }`

// upMiseDevEnvAndCLI switches builtin language runtimes (Go / Node.js / Java / Python) to mise.
// Historical asdf / fnm / SDKMAN! / pyenv templates are replaced, not kept as fallback.
// AI CLIs stay on npm; they are not switched here.
func upMiseDevEnvAndCLI(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	return refreshBuiltinLanguagesToMise(db)
}

func refreshBuiltinLanguagesToMise(db *gorm.DB) error {
	for _, item := range []struct {
		name        string
		description string
		detect      string
		tool        string
		sourceEnv   string
		sources     []struct{ oldName, newName, url string }
	}{
		{
			name: "Go", description: "Go 编译环境（由 mise 管理）",
			detect: "go version", tool: "go",
			sources: []struct{ oldName, newName, url string }{
				{"proxy.golang.org", "go.dev", "https://go.dev/dl/"},
				{"goproxy.cn", "golang.google.cn", "https://golang.google.cn/dl/"},
			},
		},
		{
			name: "Node.js", description: "Node.js 运行时（由 mise 管理）",
			detect: "node --version", tool: "node", sourceEnv: "MISE_NODE_MIRROR",
		},
		{
			name: "Java", description: "Java 运行时（由 mise 管理）",
			detect: "java -version", tool: "java",
			sources: []struct{ oldName, newName, url string }{
				{"sdkman candidates", "Adoptium", "https://api.adoptium.net"},
			},
		},
		{
			name: "Python", description: "Python 运行时（由 mise 管理）",
			detect: "python3 --version", tool: "python", sourceEnv: "PYTHON_BUILD_MIRROR_URL",
		},
	} {
		var env devEnvironmentMigrationModel
		err := db.Where("name = ? AND kind = ?", item.name, "builtin").First(&env).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		install, upgrade := miseInstallScripts(item.tool, item.sourceEnv)
		if err := db.Model(&env).Updates(map[string]any{
			"description":      item.description,
			"detect_script":    misePathPrelude + "; " + item.detect,
			"versions_script":  misePathPrelude + "; mise ls-remote " + item.tool,
			"install_script":   install,
			"upgrade_script":   upgrade,
			"uninstall_script": miseUninstallScript(item.tool),
			"switch_script":    miseSwitchScript(item.tool),
		}).Error; err != nil {
			return err
		}
		for _, source := range item.sources {
			if err := db.Model(&devEnvInstallSourceMigrationModel{}).
				Where("environment_id = ? AND name = ?", env.ID, source.oldName).
				Updates(map[string]any{"name": source.newName, "base_url": source.url}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func miseInstallScripts(tool, sourceEnv string) (install, upgrade string) {
	body := `version="{{version}}"; [ -n "$version" ] || version=latest; `
	if sourceEnv != "" {
		body += `src="{{source_url}}"; [ -n "$src" ] && export ` + sourceEnv + `="$src"; `
	}
	body += `mise install "` + tool + `@$version" && mise use -g "` + tool + `@$version"`
	script := misePathPrelude + "; " + body
	return script, script
}

func miseUninstallScript(tool string) string {
	return misePathPrelude + `; version="{{version}}"; [ -n "$version" ] || { echo 'a version is required'; exit 2; }; mise uninstall "` + tool + `@$version"`
}

func miseSwitchScript(tool string) string {
	return misePathPrelude + `; version="{{version}}"; [ -n "$version" ] || { echo 'a version is required'; exit 2; }; mise use -g "` + tool + `@$version"`
}
