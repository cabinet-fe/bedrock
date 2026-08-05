package service_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
)

var (
	aiTestTemplateOnce sync.Once
	aiTestTemplatePath string
	aiTestTemplateErr  error
)

func openAITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	aiTestTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "bedrock-ai-mig-")
		if err != nil {
			aiTestTemplateErr = err
			return
		}
		path := filepath.Join(dir, "template.sqlite")
		gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: path})
		if err != nil {
			aiTestTemplateErr = err
			return
		}
		if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
			aiTestTemplateErr = err
			return
		}
		sqlDB, err := gdb.DB()
		if err != nil {
			aiTestTemplateErr = err
			return
		}
		if err := sqlDB.Close(); err != nil {
			aiTestTemplateErr = err
			return
		}
		aiTestTemplatePath = path
	})
	if aiTestTemplateErr != nil {
		t.Fatalf("ai test template db: %v", aiTestTemplateErr)
	}
	dst := filepath.Join(t.TempDir(), "ai.sqlite")
	in, err := os.Open(aiTestTemplatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func defaultStubCLIRunner(_ context.Context, _ service.CLIRunRequest) (string, error) {
	return "stub-cli-ok\n", nil
}

func configureTestAgentService(agents *service.AgentService) {
	agents.SetCLIRunner(defaultStubCLIRunner)
	agents.SetSyncWorkspaceInit(true)
	agents.SetInlineExec(true)
}

func setupAI(t *testing.T) (*gorm.DB, *service.AgentService, *service.SkillService, *projectservice.ProjectService) {
	t.Helper()
	root := t.TempDir()
	gdb := openAITestDB(t)
	repo := repository.NewAIRepository(gdb)
	cli := resourceservice.NewCLIService(resourcerepo.NewCLIRepository(gdb))

	storageRoot := filepath.Join(root, "storage")
	storageSvc, err := storageservice.NewStorageService(storagerepo.NewStorageRepository(gdb), storageRoot, storageservice.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	skills := service.NewSkillService(repo, storageSvc, filepath.Join(storageRoot, "skills"))
	work := filepath.Join(root, "work")
	arts := filepath.Join(root, "artifacts")
	logs := filepath.Join(root, "logs")
	agents := service.NewAgentService(repo, cli, skills, nil, zap.NewNop(), work, arts, logs)
	configureTestAgentService(agents)
	agents.Start()
	t.Cleanup(agents.Shutdown)

	projectSvc := projectservice.NewProjectService(projectrepo.NewProjectRepository(gdb), storageSvc)
	agents.SetDocDraftWriter(projectSvc)
	projectSvc.SetDocsAIBridge(service.NewDocsBridge(agents))
	return gdb, agents, skills, projectSvc
}

func setupAgentWorkspace(t *testing.T) (*service.AgentService, *service.SkillService, *resourcerepo.CLIRepository, string, string) {
	t.Helper()
	root := t.TempDir()
	gdb := openAITestDB(t)
	repo := repository.NewAIRepository(gdb)
	cliRepo := resourcerepo.NewCLIRepository(gdb)
	cli := resourceservice.NewCLIService(cliRepo)
	storageRoot := filepath.Join(root, "storage")
	storageSvc, err := storageservice.NewStorageService(storagerepo.NewStorageRepository(gdb), storageRoot, storageservice.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	skills := service.NewSkillService(repo, storageSvc, filepath.Join(storageRoot, "skills"))
	work := filepath.Join(root, "work")
	arts := filepath.Join(root, "artifacts")
	logs := filepath.Join(root, "logs")
	agents := service.NewAgentService(repo, cli, skills, nil, zap.NewNop(), work, arts, logs)
	agents.SetGitCheckout(stubGitCheckout)
	configureTestAgentService(agents)
	agents.Start()
	t.Cleanup(agents.Shutdown)
	return agents, skills, cliRepo, work, arts
}

func markCLIInstalled(t *testing.T, repo *resourcerepo.CLIRepository, key, defaultArgs string) {
	t.Helper()
	cli, err := repo.FindByKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cli.InstalledPath = filepath.Join(t.TempDir(), key+"-stub")
	cli.DefaultArgs = defaultArgs
	cli.InstallStatus = "installed"
	cli.Healthy = true
	if err := repo.Update(cli); err != nil {
		t.Fatal(err)
	}
}

func requireWorkspaceReady(t *testing.T, agents *service.AgentService, agentID uint) *model.AiAgent {
	t.Helper()
	got, err := agents.GetAgent(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceStatus != model.WorkspaceReady {
		t.Fatalf("workspace_status=%q want=%q err=%q", got.WorkspaceStatus, model.WorkspaceReady, got.WorkspaceError)
	}
	return got
}

func requireRunStatus(t *testing.T, agents *service.AgentService, runID uint, want string) *model.AgentRun {
	t.Helper()
	got, err := agents.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != want {
		t.Fatalf("run status=%s want=%s err=%s log=%s", got.Status, want, got.ErrorMessage, readRunLog(t, got.LogPath))
	}
	return got
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

func recordingCLIRunner(last *service.CLIRunRequest) service.CLIRunner {
	return func(_ context.Context, req service.CLIRunRequest) (string, error) {
		cp := req
		cp.Args = append([]string{}, req.Args...)
		cp.Env = append([]string{}, req.Env...)
		*last = cp
		return "stub-cli-ok\n", nil
	}
}
