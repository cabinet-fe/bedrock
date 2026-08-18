package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	systemrepository "bedrock/internal/system/repository"
	systemservice "bedrock/internal/system/service"

	"gorm.io/gorm"
)

func TestInstallFallsBackToSecondSourceAndKeepsLogs(t *testing.T) {
	gdb := newOpsDatabase(t)
	repo := repository.NewOpsRepository(gdb)
	audit := systemservice.NewAuditService(systemrepository.NewOperationLogRepository(gdb))
	svc := NewDevEnvironmentService(repo, audit)
	svc.SetInlineExec(true)

	env, err := svc.CreateCustom(DevEnvironmentInput{
		Name:          "fallback-test",
		Executable:    "sh",
		InstallScript: `if [ "{{source_url}}" = "second" ]; then echo second-ok; else echo first-failed; exit 1; fi`,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSource(env.ID, SourceInput{Name: "first-source", BaseURL: "first", Priority: -20, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSource(env.ID, SourceInput{Name: "second-source", BaseURL: "second", Priority: -10, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.Enqueue(env.ID, "install", JobInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := svc.GetJob(env.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != model.JobSuccess {
		t.Fatalf("job status = %s, logs:\n%s", finished.Status, finished.LogText)
	}
	if !strings.Contains(finished.LogText, `source "first-source" failed`) ||
		!strings.Contains(finished.LogText, `source "second-source" succeeded`) {
		t.Fatalf("fallback is not observable in logs:\n%s", finished.LogText)
	}
	audits, total, err := audit.List(systemrepository.OperationLogFilters{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if total < 3 {
		t.Fatalf("audit events = %d, want at least enqueue/start/completion", total)
	}
	wantResourceID := fmt.Sprint(job.ID)
	var completionFound bool
	for _, event := range audits {
		if event.Action == "dev_env_job_completed" && event.ResourceID == wantResourceID {
			completionFound = strings.Contains(event.Details, "source=second-source") &&
				strings.Contains(event.Details, "source_fallback=true")
			break
		}
	}
	if !completionFound {
		t.Fatalf("completion audit does not record fallback: %#v", audits)
	}
}

func TestRecoverInterruptsRunningJobAndRetryCreatesNewJob(t *testing.T) {
	repo := newOpsRepository(t)
	svc := NewDevEnvironmentService(repo)
	svc.Start()
	t.Cleanup(svc.Shutdown)

	env, err := svc.CreateCustom(DevEnvironmentInput{
		Name: "retry-test", Executable: "sh", UninstallScript: "true",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	running := &model.DevEnvJob{
		EnvironmentID: env.ID, Operation: "uninstall", Status: model.JobRunning, LogText: "kept log\n",
	}
	if err := repo.CreateJob(running); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverOnStartup(); err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.GetJob(env.ID, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != model.JobInterrupted || interrupted.LogText != "kept log\n" {
		t.Fatalf("restart must retain logs and interrupt: %#v", interrupted)
	}
	retry, err := svc.Retry(env.ID, running.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID == running.ID {
		t.Fatalf("retry must create a new job: %#v", retry)
	}
}

func TestKillSelfAndDangerousProcessAreRejected(t *testing.T) {
	svc := NewProcessService()
	if _, err := svc.KillProcess(int32(os.Getpid())); !errors.Is(err, ErrKillSelf) {
		t.Fatalf("self kill error = %v", err)
	}
	for _, item := range []struct {
		pid  int32
		name string
	}{{1, "anything"}, {999, "systemd"}, {999, "Bedrock"}} {
		if !IsDangerousProcess(item.pid, item.name) {
			t.Fatalf("expected protected process: %#v", item)
		}
	}
}

func TestDevEnvironmentLifecycleExecutesEachOperation(t *testing.T) {
	repo := newOpsRepository(t)
	svc := NewDevEnvironmentService(repo)
	svc.SetInlineExec(true)

	marker := filepath.Join(t.TempDir(), "dev-env-lifecycle.log")
	command := func(operation string) string {
		return fmt.Sprintf("printf '%s\\n' >> %q", operation, marker)
	}
	env, err := svc.CreateCustom(DevEnvironmentInput{
		Name:            "lifecycle-test",
		Executable:      "sh",
		DetectScript:    command("detect"),
		InstallScript:   command("install"),
		UpgradeScript:   command("upgrade"),
		UninstallScript: command("uninstall"),
		SwitchScript:    command("switch"),
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	detect, err := svc.Detect(env.ID)
	if err != nil || !detect.Detected {
		t.Fatalf("detect = %#v, %v", detect, err)
	}
	for _, operation := range []string{"install", "upgrade", "uninstall", "switch"} {
		job, err := svc.Enqueue(env.ID, operation, JobInput{Version: "1.2.3"}, 1)
		if err != nil {
			t.Fatalf("enqueue %s: %v", operation, err)
		}
		finished, err := svc.GetJob(env.ID, job.ID)
		if err != nil || finished.Status != model.JobSuccess {
			t.Fatalf("%s status = %s, logs:\n%s", operation, finished.Status, finished.LogText)
		}
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"detect", "install", "upgrade", "uninstall", "switch"} {
		if !strings.Contains(string(content), operation+"\n") {
			t.Fatalf("lifecycle marker is missing %q: %s", operation, content)
		}
	}
	updated, err := repo.FindEnvironment(env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultVersion != "1.2.3" {
		t.Fatalf("switch did not update default version: %q", updated.DefaultVersion)
	}
}

func TestSeededGoDevEnvironmentLifecycleExecutesWithStubbedManagers(t *testing.T) {
	repo := newOpsRepository(t)
	svc := NewDevEnvironmentService(repo)
	svc.SetInlineExec(true)

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "dev-env-lifecycle.log")
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("MISE_DATA_DIR", filepath.Join(fakeHome, ".local", "share", "mise"))
	writeStubExecutable(t, filepath.Join(stubDir, "go"), `#!/bin/sh
printf 'go %s\n' "$*" >> "$DEV_ENV_LIFECYCLE_LOG"
printf 'go version go1.23.4 stub/amd64\n'
`)
	writeStubExecutable(t, filepath.Join(stubDir, "mise"), `#!/bin/sh
printf 'mise %s\n' "$*" >> "$DEV_ENV_LIFECYCLE_LOG"
`)
	t.Setenv("DEV_ENV_LIFECYCLE_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	items, err := repo.ListEnvironments()
	if err != nil {
		t.Fatal(err)
	}
	var goEnv *model.DevEnvironment
	for i := range items {
		if items[i].Name == "Go" {
			goEnv = &items[i]
			break
		}
	}
	if goEnv == nil || goEnv.Kind != model.DevEnvBuiltin {
		t.Fatalf("seeded Go environment = %#v, want builtin definition", goEnv)
	}

	detected, err := svc.Detect(goEnv.ID)
	if err != nil || !detected.Detected {
		t.Fatalf("detect Go = %#v, %v", detected, err)
	}
	for _, operation := range []string{"install", "upgrade", "uninstall", "switch"} {
		job, err := svc.Enqueue(goEnv.ID, operation, JobInput{Version: "1.23.4"}, 1)
		if err != nil {
			t.Fatalf("enqueue Go %s: %v", operation, err)
		}
		finished, err := svc.GetJob(goEnv.ID, job.ID)
		if err != nil || finished.Status != model.JobSuccess {
			t.Fatalf("Go %s status = %s, logs:\n%s", operation, finished.Status, finished.LogText)
		}
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(logContent)
	for _, want := range []string{
		"go version",
		"mise install go@1.23.4",
		"mise use -g go@1.23.4",
		"mise uninstall go@1.23.4",
	} {
		if !strings.Contains(logs, want+"\n") {
			t.Fatalf("seeded Go lifecycle did not invoke %q:\n%s", want, logs)
		}
	}
	updated, err := repo.FindEnvironment(goEnv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultVersion != "1.23.4" {
		t.Fatalf("switch did not update seeded Go default version: %q", updated.DefaultVersion)
	}
}

func TestSeededGoDevEnvironmentUsesRealLifecycleScripts(t *testing.T) {
	repo := newOpsRepository(t)
	items, err := repo.ListEnvironments()
	if err != nil {
		t.Fatal(err)
	}
	var goEnv *model.DevEnvironment
	for i := range items {
		if items[i].Name == "Go" {
			goEnv = &items[i]
			break
		}
	}
	if goEnv == nil {
		t.Fatal("seeded Go environment is missing")
	}
	for _, operation := range []string{"install", "upgrade", "uninstall", "switch"} {
		script, err := commandScript(goEnv, operation)
		if err != nil {
			t.Fatalf("Go %s script: %v", operation, err)
		}
		if !strings.Contains(script, "mise") || strings.Contains(script, "asdf") ||
			strings.Contains(script, "fnm") || strings.Contains(script, "sdkman") ||
			strings.Contains(script, "pyenv") || strings.Contains(script, "go{{version}} version") {
			t.Fatalf("Go %s script is not mise-managed: %q", operation, script)
		}
	}
	if !strings.Contains(goEnv.VersionsScript, "mise ls-remote go") {
		t.Fatalf("Go versions script must use mise: %q", goEnv.VersionsScript)
	}
	if strings.Contains(goEnv.Description, "asdf") {
		t.Fatalf("Go description still mentions asdf: %q", goEnv.Description)
	}
	if len(goEnv.Sources) == 0 {
		t.Fatal("seeded Go environment must have per-environment install sources")
	}
}

func TestListVersionsParsesScriptAndReturnsCatalog(t *testing.T) {
	repo := newOpsRepository(t)
	svc := NewDevEnvironmentService(repo)
	env, err := svc.CreateCustom(DevEnvironmentInput{
		Name: "versions-test", Executable: "go",
		VersionsScript: "printf 'mise WARN skip\\n1.21.0\\n1.22.0\\n1.23.4\\n'",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ListVersions(env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CatalogURL != "https://go.dev/dl/" {
		t.Fatalf("catalog = %q", got.CatalogURL)
	}
	if got.Error != "" {
		t.Fatalf("unexpected error: %s", got.Error)
	}
	if strings.Join(got.Items, ",") != "1.23.4,1.22.0,1.21.0" {
		t.Fatalf("items = %#v", got.Items)
	}
}

func TestSeededBuiltinsUseMiseNotLegacyManagers(t *testing.T) {
	repo := newOpsRepository(t)
	items, err := repo.ListEnvironments()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	legacy := []string{"asdf", "fnm", "sdkman", "SDKMAN", "pyenv"}
	for _, item := range items {
		if item.Kind != model.DevEnvBuiltin {
			continue
		}
		found[item.Name] = true
		blob := strings.Join([]string{
			item.Description, item.DetectScript, item.InstallScript, item.UpgradeScript,
			item.UninstallScript, item.VersionsScript, item.SwitchScript,
		}, "\n")
		if !strings.Contains(blob, "mise") {
			t.Fatalf("%s scripts must use mise", item.Name)
		}
		for _, word := range legacy {
			if strings.Contains(blob, word) {
				t.Fatalf("%s still mentions %s:\n%s", item.Name, word, blob)
			}
		}
	}
	for _, name := range []string{"Go", "Node.js", "Java", "Python"} {
		if !found[name] {
			t.Fatalf("missing builtin %s", name)
		}
	}
}

func TestRedactSpacedCLISecrets(t *testing.T) {
	input := `tool --token token-secret -t short-token --password "password secret" --api-key api-secret --secret secret-value --access-token access-secret --auth auth-secret --credential credential-secret --header "Authorization: Bearer header-secret"`
	redacted := redact(input)
	for _, secret := range []string{
		"token-secret",
		"short-token",
		"password secret",
		"api-secret",
		"secret-value",
		"access-secret",
		"auth-secret",
		"credential-secret",
		"header-secret",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted command leaks %q: %s", secret, redacted)
		}
	}
	for _, want := range []string{
		"--token [REDACTED]",
		"-t [REDACTED]",
		"--password [REDACTED]",
		`--header "Authorization: Bearer [REDACTED]"`,
	} {
		if !strings.Contains(redacted, want) {
			t.Fatalf("redaction missing %q: %s", want, redacted)
		}
	}
}

func TestFailedJobPersistsRedactedSpacedCLISecrets(t *testing.T) {
	repo := newOpsRepository(t)
	svc := NewDevEnvironmentService(repo)
	job := &model.DevEnvJob{
		EnvironmentID:   1,
		Operation:       "install",
		Status:          model.JobRunning,
		CommandSnapshot: "tool --token snapshot-secret",
		LogText:         "tool --password log-secret",
	}
	if err := repo.CreateJob(job); err != nil {
		t.Fatal(err)
	}

	svc.fail(job, errors.New("tool --api-key error-secret --access-token access-secret --secret secret-value"), "", false)

	persisted, err := repo.FindJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{persisted.CommandSnapshot, persisted.LogText, persisted.ErrorMessage} {
		for _, secret := range []string{"snapshot-secret", "log-secret", "error-secret", "access-secret", "secret-value"} {
			if strings.Contains(value, secret) {
				t.Fatalf("persisted failed job leaks %q: %q", secret, value)
			}
		}
	}
}

func awaitJob(t *testing.T, svc *DevEnvironmentService, envID, id uint) *model.DevEnvJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.GetJob(envID, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != model.JobQueued && job.Status != model.JobRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not finish", id)
	return nil
}

func writeStubExecutable(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func newOpsRepository(t *testing.T) *repository.OpsRepository {
	t.Helper()
	return repository.NewOpsRepository(newOpsDatabase(t))
}

func newOpsDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "ops.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return gdb
}
