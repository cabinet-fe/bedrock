package service_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/harness/harnesstest"
	harnessservice "bedrock/internal/harness/service"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbacmodel "bedrock/internal/rbac/model"
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

// actorOne is the all-scope actor used across the service tests (user 1).
func actorOne() service.AgentActor {
	return service.AgentActor{UserID: 1, DataScope: rbacmodel.DataScopeAll}
}

// wireTestHarness builds a fake harness backend (session + stream services
// over a scriptable fake provider) and wires it into the agent service. The
// fake defaults to a successful session script.
func wireTestHarness(t *testing.T, agents *service.AgentService, workspaceRoot string) (*harnesstest.Fake, *harnessservice.StreamService) {
	t.Helper()
	fake := harnesstest.New()
	sessions := harnessservice.NewSessionService(fake, harnessservice.SessionConfig{
		WorkspaceRoot: workspaceRoot,
	}, nil)
	streams := harnessservice.NewStreamService(fake, harnessservice.StreamConfig{
		ApprovalMode: harnessservice.ApprovalManual,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go streams.Run(ctx)
	agents.SetHarnessBackend(sessions, streams, fake)
	agents.SetHarnessTimers(200*time.Millisecond, 2*time.Second)
	return fake, streams
}

func setupAI(t *testing.T) (*gorm.DB, *service.AgentService, *harnesstest.Fake, *service.SkillService, *projectservice.ProjectService) {
	t.Helper()
	root := t.TempDir()
	gdb := openAITestDB(t)
	repo := repository.NewAIRepository(gdb)

	storageRoot := filepath.Join(root, "storage")
	storageSvc, err := storageservice.NewStorageService(storagerepo.NewStorageRepository(gdb), storageRoot, storageservice.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	skills := service.NewSkillService(repo, storageSvc, filepath.Join(storageRoot, "skills"))
	work := filepath.Join(root, "work")
	arts := filepath.Join(root, "artifacts")
	logs := filepath.Join(root, "logs")
	agents := service.NewAgentService(repo, skills, nil, zap.NewNop(), work, arts, logs)
	fake, _ := wireTestHarness(t, agents, work)
	agents.SetSyncWorkspaceInit(true)
	agents.SetInlineExec(true)
	agents.Start()
	t.Cleanup(agents.Shutdown)

	projectRepo := projectrepo.NewProjectRepository(gdb)
	projectSvc := projectservice.NewProjectService(projectRepo, storageSvc)
	agents.SetDocDraftWriter(projectSvc)
	projectSvc.SetDocsAIBridge(service.NewDocsBridge(agents))
	return gdb, agents, fake, skills, projectSvc
}

func setupAgentWorkspace(t *testing.T) (*service.AgentService, *harnesstest.Fake, *service.SkillService, string, string) {
	t.Helper()
	root := t.TempDir()
	gdb := openAITestDB(t)
	repo := repository.NewAIRepository(gdb)
	storageRoot := filepath.Join(root, "storage")
	storageSvc, err := storageservice.NewStorageService(storagerepo.NewStorageRepository(gdb), storageRoot, storageservice.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	skills := service.NewSkillService(repo, storageSvc, filepath.Join(storageRoot, "skills"))
	work := filepath.Join(root, "work")
	arts := filepath.Join(root, "artifacts")
	logs := filepath.Join(root, "logs")
	agents := service.NewAgentService(repo, skills, nil, zap.NewNop(), work, arts, logs)
	agents.SetGitCheckout(stubGitCheckout)
	fake, _ := wireTestHarness(t, agents, work)
	agents.SetSyncWorkspaceInit(true)
	agents.SetInlineExec(true)
	agents.Start()
	t.Cleanup(agents.Shutdown)
	return agents, fake, skills, work, arts
}

func requireWorkspaceReady(t *testing.T, agents *service.AgentService, agentID uint) *model.AiAgent {
	t.Helper()
	got, err := agents.GetAgent(agentID, service.AgentActor{DataScope: rbacmodel.DataScopeAll})
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
	got, err := agents.RequireRunAccess(runID, service.AgentActor{DataScope: rbacmodel.DataScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != want {
		t.Fatalf("run status=%s want=%s err=%s log=%s", got.Status, want, got.ErrorMessage, readRunLog(t, got.LogPath))
	}
	return got
}

func waitRunStatus(t *testing.T, agents *service.AgentService, runID uint, want string) *model.AgentRun {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last *model.AgentRun
	for time.Now().Before(deadline) {
		got, err := agents.RequireRunAccess(runID, service.AgentActor{DataScope: rbacmodel.DataScopeAll})
		if err != nil {
			t.Fatal(err)
		}
		last = got
		if got.Status == want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d status=%s want=%s err=%s log=%s",
		runID, last.Status, want, last.ErrorMessage, readRunLog(t, last.LogPath))
	return last
}

// waitRunPrompt blocks until the run's harness session received its prompt
// and returns it (runs on the async worker).
func waitRunPrompt(t *testing.T, agents *service.AgentService, fake *harnesstest.Fake, runID uint) string {
	t.Helper()
	var sessionID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := agents.RequireRunAccess(runID, service.AgentActor{DataScope: rbacmodel.DataScopeAll})
		if err != nil {
			t.Fatal(err)
		}
		if run.HarnessSessionID != nil && *run.HarnessSessionID != "" {
			sessionID = *run.HarnessSessionID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sessionID == "" {
		t.Fatalf("run %d never got a harness session", runID)
	}
	for time.Now().Before(deadline) {
		if prompts := fake.Prompts(sessionID); len(prompts) > 0 {
			return prompts[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d session %s never received a prompt", runID, sessionID)
	return ""
}
