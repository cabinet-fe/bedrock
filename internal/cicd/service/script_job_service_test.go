package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/cicd/repository"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacmodel "bedrock/internal/rbac/model"
)

func setupScriptJobSvc(t *testing.T) *service.ScriptJobService {
	t.Helper()
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "script.sqlite"),
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
	svc := service.NewScriptJobService(repository.NewScriptJobRepository(gdb), nil)
	svc.SetWorkspaceDir(filepath.Join(t.TempDir(), "workspaces"))
	return svc
}

func TestScriptJobServiceCreateGet(t *testing.T) {
	svc := setupScriptJobSvc(t)
	enabled := true
	manual := true
	job, err := svc.Create(1, service.CreateScriptJobInput{
		Name:          " cleanup ",
		Description:   "desc",
		Enabled:       &enabled,
		ScriptType:    "bash",
		Script:        "echo ${{ workspace }}",
		TriggerManual: &manual,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == 0 || job.Name != "cleanup" {
		t.Fatalf("job=%+v", job)
	}
	if job.WebhookSecret != "" {
		t.Fatal("secret should be hidden on create response")
	}
	if job.WorkspacePath == "" {
		t.Fatal("expected workspace_path")
	}

	got, err := svc.Get(job.ID, 1, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "cleanup" {
		t.Fatalf("got name %q", got.Name)
	}

	secretJob, err := svc.GetWithSecret(job.ID, 1, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if secretJob.WebhookSecret == "" {
		t.Fatal("expected secret")
	}
}

func TestScriptRunServiceEnqueue(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "script-run.sqlite"),
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
	jobRepo := repository.NewScriptJobRepository(gdb)
	runRepo := repository.NewScriptRunRepository(gdb)
	jobSvc := service.NewScriptJobService(jobRepo, nil)
	runSvc := service.NewScriptRunService(runRepo, jobRepo)

	enabled := true
	manual := true
	job, err := jobSvc.Create(1, service.CreateScriptJobInput{
		Name: "run-me", Enabled: &enabled, Script: "true", TriggerManual: &manual,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runSvc.Enqueue(job.ID, 1, rbacmodel.DataScopeAll, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.RunNumber != 1 {
		t.Fatalf("run=%+v", run)
	}
}
