package service_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
	"bedrock/internal/resource/service"
)

func setupCLI(t *testing.T) (*gorm.DB, *service.CLIService) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "cli.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return gdb, service.NewCLIService(repository.NewCLIRepository(gdb))
}

func TestFourCLIDetectReferencePaths(t *testing.T) {
	_, cli := setupCLI(t)
	for _, key := range []string{"claude_code", "opencode", "reasonix", "codex"} {
		result, err := cli.Detect(key)
		if err != nil {
			t.Fatalf("%s detect: %v", key, err)
		}
		if result.RiskNotice == "" {
			t.Fatalf("%s missing risk notice", key)
		}
		// Observable success (installed) or failure (missing) both satisfy Gate.
		if result.Detected && !result.Healthy {
			t.Fatalf("%s detected but not healthy", key)
		}
		if !result.Detected && result.Output == "" {
			t.Fatalf("%s missing failure output", key)
		}
	}
}

func TestCLIListSeeded(t *testing.T) {
	_, cli := setupCLI(t)
	items, err := cli.ListCLIs()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 CLIs, got %d", len(items))
	}
	wantDefaultArgs := map[string]string{
		"claude_code": "--print",
		"codex":       "exec",
		"opencode":    "run",
		"reasonix":    "run",
	}
	for _, item := range items {
		got := strings.TrimSpace(item.DefaultArgs)
		want := wantDefaultArgs[item.Key]
		if got != want {
			t.Fatalf("cli %s default_args=%q want %q", item.Key, got, want)
		}
	}
}

func TestDetectExtractsVersionNotPath(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "claude_code").
		Updates(map[string]any{
			"detect_command": `printf '/usr/local/bin/claude\nclaude version 2.3.4\n'`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := cli.Detect("claude_code")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Detected {
		t.Fatal("expected detected")
	}
	if result.Version != "2.3.4" {
		t.Fatalf("version=%q want 2.3.4", result.Version)
	}
	if strings.Contains(result.Version, "/") {
		t.Fatalf("version looks like path: %q", result.Version)
	}
}

func TestDetectFindsPATHBinaryWithoutMise(t *testing.T) {
	_, cli := setupCLI(t)
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "codex"), []byte("#!/bin/sh\necho 'codex-cli 3.1.4'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	result, err := cli.Detect("codex")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Detected {
		t.Fatalf("expected PATH binary to be detected, got %+v", result)
	}
	if result.Version != "3.1.4" {
		t.Fatalf("version=%q output=%q", result.Version, result.Output)
	}
}

func TestDetectClearsStaleWhenMissing(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "codex").
		Updates(map[string]any{
			"detect_command":    "false",
			"installed_path":    "/stale/codex",
			"installed_version": "9.9.9",
			"install_status":    "installed",
			"healthy":           true,
		}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := cli.Detect("codex")
	if err != nil {
		t.Fatal(err)
	}
	if result.Detected {
		t.Fatal("expected missing")
	}
	var got model.CliRuntimeDefinition
	if err := gdb.Where("key = ?", "codex").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.InstallStatus != "missing" || got.InstalledPath != "" || got.InstalledVersion != "" || got.Healthy {
		t.Fatalf("stale fields not cleared: %+v", got)
	}
}

func TestExecuteSyncSuccessAndFailure(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "reasonix").
		Updates(map[string]any{
			"install_template":   `echo install-ok`,
			"upgrade_template":   `echo upgrade-ok`,
			"uninstall_template": `echo uninstall-ok; exit 1`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	ok, err := cli.Execute(context.Background(), "reasonix", "install", service.ExecuteInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok.Success || !strings.Contains(ok.Output, "install-ok") {
		t.Fatalf("install: %+v", ok)
	}
	fail, err := cli.Execute(context.Background(), "reasonix", "uninstall", service.ExecuteInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if fail.Success || fail.Error == "" || !strings.Contains(fail.Output, "uninstall-ok") {
		t.Fatalf("uninstall: %+v", fail)
	}
	if gdb.Migrator().HasTable("cli_install_jobs") {
		t.Fatal("cli_install_jobs table should not exist")
	}
}

func TestExecuteMultiSourceFallback(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "opencode").
		Updates(map[string]any{
			"install_template": `base="{{base_url}}"; if [ "$base" = "https://bad.example" ]; then echo fail; exit 1; fi; echo ok-from-$base`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Where("cli_key = ?", "opencode").Delete(&model.CliInstallSource{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, src := range []struct {
		name string
		url  string
		prio int
	}{
		{"bad", "https://bad.example", 10},
		{"good", "https://good.example", 20},
	} {
		if err := gdb.Create(&model.CliInstallSource{
			CliKey: "opencode", Name: src.name, BaseURL: src.url, Priority: src.prio, Enabled: true,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := cli.Execute(context.Background(), "opencode", "install", service.ExecuteInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.Output, "ok-from-https://good.example") {
		t.Fatalf("fallback result: %+v", result)
	}
	if !strings.Contains(result.Output, `source "bad" failed`) {
		t.Fatalf("expected first source failure in output: %s", result.Output)
	}
}

func TestExecuteDefaultRegistryWhenNoSources(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "codex").
		Updates(map[string]any{
			"install_template": `base="{{base_url}}"; if [ -n "$base" ]; then echo unexpected-registry; exit 1; fi; echo default-registry-ok`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Where("cli_key = ?", "codex").Delete(&model.CliInstallSource{}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := cli.Execute(context.Background(), "codex", "install", service.ExecuteInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.Output, "default-registry-ok") {
		t.Fatalf("default registry result: %+v", result)
	}
}

func TestCLINPMTemplates(t *testing.T) {
	_, cli := setupCLI(t)
	items, err := cli.ListCLIs()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.Contains(item.DetectCommand, "mise") ||
			strings.Contains(item.InstallTemplate, "mise") ||
			strings.Contains(item.UpgradeTemplate, "mise") ||
			strings.Contains(item.UninstallTemplate, "mise") {
			t.Fatalf("%s still uses mise: detect=%q install=%q", item.Key, item.DetectCommand, item.InstallTemplate)
		}
		if !strings.Contains(item.DetectCommand, "command -v "+item.BinaryName) {
			t.Fatalf("%s detect should use PATH binary: %s", item.Key, item.DetectCommand)
		}
		if !strings.Contains(item.InstallTemplate, "npm install -g") {
			t.Fatalf("%s install should use npm -g: %s", item.Key, item.InstallTemplate)
		}
		if !strings.Contains(item.UpgradeTemplate, "npm install -g") {
			t.Fatalf("%s upgrade should use npm -g: %s", item.Key, item.UpgradeTemplate)
		}
		if !strings.Contains(item.InstallTemplate, "--registry") {
			t.Fatalf("%s install should wire --registry: %s", item.Key, item.InstallTemplate)
		}
		if !strings.Contains(item.UninstallTemplate, "npm uninstall -g") {
			t.Fatalf("%s uninstall should use npm -g: %s", item.Key, item.UninstallTemplate)
		}
	}
}

func TestCheckUpdateRequiresNpmPackage(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "codex").
		Update("install_template", `echo no-npm`).Error; err != nil {
		t.Fatal(err)
	}
	_, err := cli.CheckUpdate(context.Background(), "codex")
	if err == nil || !strings.Contains(err.Error(), "npm 包") {
		t.Fatalf("expected npm package error, got %v", err)
	}
}

func TestCheckUpdateReportsAvailability(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "codex").
		Updates(map[string]any{
			"install_template":  `npm install -g @openai/codex${version:+@$version}`,
			"installed_version": "0.0.0",
			"install_status":    "installed",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Where("cli_key = ?", "codex").Delete(&model.CliInstallSource{}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := cli.CheckUpdate(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if result.Package != "@openai/codex" {
		t.Fatalf("package: %s", result.Package)
	}
	if result.Error != "" {
		t.Skipf("npm view unavailable in this environment: %s", result.Error)
	}
	if result.LatestVersion == "" {
		t.Fatal("expected latest version")
	}
	if !result.UpdateAvailable {
		t.Fatalf("0.0.0 should be outdated vs %s", result.LatestVersion)
	}
	if result.CurrentVersion != "0.0.0" {
		t.Fatalf("current: %s", result.CurrentVersion)
	}
}

func TestListCLIVersionsWithoutPackage(t *testing.T) {
	gdb, cli := setupCLI(t)
	if err := gdb.Model(&model.CliRuntimeDefinition{}).
		Where("key = ?", "codex").
		Update("install_template", `echo no-npm`).Error; err != nil {
		t.Fatal(err)
	}
	got, err := cli.ListVersions(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Error == "" || !strings.Contains(got.Error, "npm 包") {
		t.Fatalf("expected package error, got %+v", got)
	}
	if !strings.Contains(got.CatalogURL, "npmjs.com") {
		t.Fatalf("catalog = %q", got.CatalogURL)
	}
}

func TestCLIInstallJobsTableDropped(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "migrate.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	if gdb.Migrator().HasTable("cli_install_jobs") {
		t.Fatal("cli_install_jobs table should be dropped by 000014")
	}
}
