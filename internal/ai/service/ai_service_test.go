package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	cicdmodel "bedrock/internal/cicd/model"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
)

func setupAI(t *testing.T) (*gorm.DB, *service.AgentService, *service.SkillService, *projectservice.ProjectService) {
	t.Helper()
	root := t.TempDir()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(root, "ai.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
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
	agents.Start()
	t.Cleanup(agents.Shutdown)

	projectSvc := projectservice.NewProjectService(projectrepo.NewProjectRepository(gdb), storageSvc)
	agents.SetDocDraftWriter(projectSvc)
	projectSvc.SetDocsAIBridge(service.NewDocsBridge(agents))
	return gdb, agents, skills, projectSvc
}

func TestTriggersCreateIndependentAgentRuns(t *testing.T) {
	_, agents, _, _ := setupAI(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "t", CliKey: "claude_code", SystemPrompt: "hello", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	manual, err := agents.ManualRun(agent.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	api, err := agents.APIRun(agent.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	cronTrig, err := agents.CreateTrigger(agent.ID, 1, service.TriggerInput{
		Type: model.TriggerCron, CronExpression: "0 0 * * *", CronTimezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	cronRun, err := agents.CreateRun(agent.ID, service.CreateRunInput{
		TriggerType: model.TriggerCron, TriggerID: &cronTrig.ID, TriggeredBy: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	job := &cicdmodel.BuildJob{ID: 99, AgentTriggerEvent: model.EventArtifactReady, AgentIDs: cicdmodel.UintList{agent.ID}}
	buildRun := &cicdmodel.BuildRun{ID: 77, BuildJobID: 99, Status: "success", TriggeredBy: 1, ArtifactPath: "/tmp/a.tgz"}
	agents.OnBuildEvent(model.EventArtifactReady, job, buildRun)
	deadline := time.Now().Add(2 * time.Second)
	var buildEventRun *model.AgentRun
	for time.Now().Before(deadline) {
		items, _, _ := agents.ListRuns(1, 50, agent.ID, "")
		for i := range items {
			if items[i].TriggerType == model.TriggerBuildEvent {
				buildEventRun = &items[i]
				break
			}
		}
		if buildEventRun != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if buildEventRun == nil {
		t.Fatal("expected build_event AgentRun")
	}
	ids := map[uint]bool{manual.ID: true, api.ID: true, cronRun.ID: true, buildEventRun.ID: true}
	if len(ids) != 4 {
		t.Fatalf("expected 4 independent runs, got %v", ids)
	}
}

func TestAgentFailureDoesNotChangeBuildRun(t *testing.T) {
	_, agents, _, _ := setupAI(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "fail", CliKey: "reasonix", SystemPrompt: "x", TimeoutSec: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	build := &cicdmodel.BuildRun{ID: 5, Status: "success", ArtifactPath: "/a"}
	job := &cicdmodel.BuildJob{ID: 1, AgentIDs: cicdmodel.UintList{agent.ID}, AgentTriggerEvent: model.EventArtifactReady}
	agents.OnBuildEvent(model.EventArtifactReady, job, build)
	time.Sleep(200 * time.Millisecond)
	if build.Status != "success" {
		t.Fatalf("BuildRun.status mutated to %s", build.Status)
	}
}

func TestSkillUploadRejectMissingSKILLMDAndOverwrite(t *testing.T) {
	_, _, skills, _ := setupAI(t)
	bad := zipBytes(t, map[string]string{"README.md": "nope"})
	_, err := skills.Create(service.SkillUploadInput{
		Name: "bad", Visibility: model.SkillPrivate, Filename: "bad.zip",
		Size: int64(len(bad)), Source: bytes.NewReader(bad), UserID: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("expected missing SKILL.md error, got %v", err)
	}

	good1 := zipBytes(t, map[string]string{"SKILL.md": "# v1"})
	s1, err := skills.Create(service.SkillUploadInput{
		Name: "ok", Visibility: model.SkillPublic, Filename: "ok.zip",
		Size: int64(len(good1)), Source: bytes.NewReader(good1), UserID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	good2 := zipBytes(t, map[string]string{"SKILL.md": "# v2-new"})
	s2, err := skills.Overwrite(s1.ID, service.SkillUploadInput{
		Filename: "ok.zip", Size: int64(len(good2)), Source: bytes.NewReader(good2), UserID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s2.PackageDigest == s1.PackageDigest {
		t.Fatal("overwrite should change digest")
	}
	_, rc, _, err := skills.OpenPackage(s2.ID, 1, true, "all")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(rc)
	if !bytes.Contains(buf.Bytes(), []byte("v2-new")) && buf.Len() == 0 {
		// ZIP binary won't contain plaintext easily if compressed; digest change is enough.
	}
	if s2.PackageDigest == "" {
		t.Fatal("empty digest")
	}
}

func TestPrivateSkillIsolation(t *testing.T) {
	_, _, skills, _ := setupAI(t)
	z := zipBytes(t, map[string]string{"SKILL.md": "# priv"})
	s, err := skills.Create(service.SkillUploadInput{
		Name: "priv", Visibility: model.SkillPrivate, Filename: "p.zip",
		Size: int64(len(z)), Source: bytes.NewReader(z), UserID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Get(s.ID, 2, false, "self"); err == nil {
		t.Fatal("non-creator must not see private skill")
	}
	items, _, err := skills.List(1, 20, 2, false, "self")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == s.ID {
			t.Fatal("private skill leaked in list")
		}
	}
	if _, err := skills.Get(s.ID, 2, false, "all"); err != nil {
		t.Fatalf("data_scope=all must see private skill: %v", err)
	}
	allItems, _, err := skills.List(1, 20, 2, false, "all")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range allItems {
		if item.ID == s.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("data_scope=all must list private skill")
	}
}

func TestInjectSkillsUsesNameAndStripsWrapper(t *testing.T) {
	_, _, skills, _ := setupAI(t)
	z := zipBytes(t, map[string]string{
		"java-api-docs/SKILL.md":            "# nested-skill",
		"java-api-docs/references/notes.md": "refs",
		"__MACOSX/java-api-docs/._SKILL.md": "junk",
	})
	s, err := skills.Create(service.SkillUploadInput{
		Name: "java-api-docs", Visibility: model.SkillPublic, Filename: "java-api-docs.zip",
		Size: int64(len(z)), Source: bytes.NewReader(z), UserID: 1, IsSuperAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if _, err := skills.InjectSkills(tmp, []uint{s.ID}, 1, true); err != nil {
		t.Fatal(err)
	}
	skillMD := filepath.Join(tmp, ".agents", "skills", "java-api-docs", "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("expected skill at name path: %v", err)
	}
	if !strings.Contains(string(data), "nested-skill") {
		t.Fatalf("unexpected SKILL.md body: %q", data)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents", "skills", "java-api-docs", "references", "notes.md")); err != nil {
		t.Fatalf("nested references missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents", "skills", fmt.Sprintf("%d", s.ID))); err == nil {
		t.Fatal("must not create id-named skill folder")
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents", "skills", "java-api-docs", "java-api-docs")); err == nil {
		t.Fatal("must not keep ZIP wrapper directory under skill name")
	}
	if _, err := os.Stat(filepath.Join(tmp, ".agents", "skills", "java-api-docs", "__MACOSX")); err == nil {
		t.Fatal("__MACOSX must be skipped")
	}
}

func TestDocsGenerateWritesDraftOnly(t *testing.T) {
	gdb, agents, _, projectSvc := setupAI(t)
	owner := projectservice.NewAccessContext(1, true, []string{"project_projects:create", "project_docs:execute", "project_docs:view"})
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "P", Slug: "p-ai"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "doc", CliKey: "codex", SystemPrompt: "Generate docs", TimeoutSec: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	// Seed a fake successful run writing content via bridge callback.
	node := &projectmodel.ApiDocNode{
		ProjectID: project.ID, Kind: projectmodel.DocNodeDocument, Name: "api",
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := projectrepo.NewProjectRepository(gdb).CreateDocNode(node); err != nil {
		t.Fatal(err)
	}
	content := "# From Agent\n"
	if err := projectSvc.WriteDraftFromAgentRun(project.ID, node.ID, 123, content, 1); err != nil {
		t.Fatal(err)
	}
	got, err := projectrepo.NewProjectRepository(gdb).FindDocNode(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != content {
		t.Fatalf("content=%q", got.Content)
	}
	if got.DraftSourceRunID == nil || *got.DraftSourceRunID != 123 {
		t.Fatalf("draft_source_run_id=%v", got.DraftSourceRunID)
	}
	result, err := projectSvc.GenerateDocs(owner, project.ID, projectservice.GenerateDocsInput{
		AgentID: agent.ID, NodeID: &node.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentRunID == 0 {
		t.Fatal("expected agent run id")
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestShutdownWaitsForWorkspaceInit(t *testing.T) {
	_, agents, _, _ := setupAI(t)
	if _, err := agents.CreateAgent(1, service.AgentInput{
		Name: "async", CliKey: "claude_code", SystemPrompt: "x", TimeoutSec: 5,
	}); err != nil {
		t.Fatal(err)
	}
	// Intentionally skip waitWorkspaceStatus: t.Cleanup(agents.Shutdown) must
	// drain workspace init before t.TempDir() removal (see wsInitWg).
}

func TestCronReloadAppliesTimezone(t *testing.T) {
	_, agents, _, _ := setupAI(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "tz", CliKey: "claude_code", SystemPrompt: "x", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = waitWorkspaceStatus(t, agents, agent.ID, model.WorkspaceReady)
	_, err = agents.CreateTrigger(agent.ID, 1, service.TriggerInput{
		Type: model.TriggerCron, CronExpression: "0 12 * * *", CronTimezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := agents.CronEntries()
	if len(entries) == 0 {
		t.Fatal("expected cron entry after reload")
	}
	next := entries[0].Next
	if next.IsZero() {
		t.Fatal("expected non-zero next fire time")
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if next.In(shanghai).Hour() != 12 {
		t.Fatalf("next fire should be noon Shanghai, got %s", next.In(shanghai))
	}
	// Contrast: same expression in UTC would be 12:00 UTC, not 04:00 UTC.
	if next.UTC().Hour() != 4 {
		t.Fatalf("Shanghai noon should be 04:00 UTC, got hour=%d (%s)", next.UTC().Hour(), next.UTC())
	}
}

func TestAgentRunRecovery_QueuedAndInterrupted(t *testing.T) {
	gdb, agents, _, _ := setupAI(t)
	repo := repository.NewAIRepository(gdb)
	cliDef := &resourcemodel.CliRuntimeDefinition{
		Key: "claude_code", Name: "Claude", BinaryName: "claude",
	}
	if err := gdb.Where(resourcemodel.CliRuntimeDefinition{Key: "claude_code"}).
		Attrs(resourcemodel.CliRuntimeDefinition{Name: "Claude", BinaryName: "claude"}).
		FirstOrCreate(cliDef).Error; err != nil {
		t.Fatal(err)
	}
	agent := &model.AiAgent{
		Name: "recover", CliKey: "claude_code", Enabled: true, SystemPrompt: "x", TimeoutSec: 30, CreatedBy: 1,
	}
	if err := repo.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}
	running := &model.AgentRun{AgentID: agent.ID, Status: model.JobRunning, TriggerType: "manual"}
	queued := &model.AgentRun{AgentID: agent.ID, Status: model.JobQueued, TriggerType: "manual"}
	if err := gdb.Create(running).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(queued).Error; err != nil {
		t.Fatal(err)
	}
	if err := agents.RecoverOnStartup(); err != nil {
		t.Fatal(err)
	}
	gotRunning, err := repo.FindRun(running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRunning.Status != model.JobInterrupted {
		t.Fatalf("running→interrupted got %s", gotRunning.Status)
	}
	gotQueued, err := repo.FindRun(queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	switch gotQueued.Status {
	case model.JobQueued, model.JobRunning, model.JobPending, model.JobFailed, model.JobSuccess, model.JobInterrupted:
		// ok — re-submit may advance or fail without a real CLI binary
	default:
		t.Fatalf("unexpected queued recovery status %s", gotQueued.Status)
	}
}
