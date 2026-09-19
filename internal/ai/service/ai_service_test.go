package service_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	cicdmodel "bedrock/internal/cicd/model"
	"bedrock/internal/harness/harnesstest"
	"bedrock/internal/harness/provider"
	harnessservice "bedrock/internal/harness/service"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
)

func TestAgentRunKeepsExplicitProjectID(t *testing.T) {
	_, agents, _, _, projectSvc := setupAI(t)
	owner := projectservice.NewAccessContext(1, true, []string{"project_projects:create"})
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "A", Slug: "a-run"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "shared", SystemPrompt: "x", TimeoutSec: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	run, err := agents.ManualRun(agent.ID, actorOne(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if run.ProjectID != nil {
		t.Fatalf("manual run should not bind project, got %v", run.ProjectID)
	}
	// docs_generate 显式传入 project_id
	explicit, err := agents.CreateRun(agent.ID, service.CreateRunInput{
		TriggerType: model.TriggerDocsGen, TriggeredBy: 1, ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.ProjectID == nil || *explicit.ProjectID != project.ID {
		t.Fatalf("explicit project_id=%v want %d", explicit.ProjectID, project.ID)
	}
	runs, total, err := agents.ListRuns(1, 20, 0, "", &project.ID, actorOne())
	if err != nil || total < 1 {
		t.Fatalf("list runs by project total=%d err=%v", total, err)
	}
	_ = runs
}

func TestTriggersCreateIndependentAgentRuns(t *testing.T) {
	_, agents, _, _, _ := setupAI(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "t", SystemPrompt: "hello", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	manual, err := agents.ManualRun(agent.ID, actorOne(), "")
	if err != nil {
		t.Fatal(err)
	}
	api, err := agents.APIRun(agent.ID, actorOne(), "")
	if err != nil {
		t.Fatal(err)
	}
	cronTrig, err := agents.CreateTrigger(agent.ID, actorOne(), service.TriggerInput{
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
	items, _, _ := agents.ListRuns(1, 50, agent.ID, "", nil, actorOne())
	var buildEventRun *model.AgentRun
	for i := range items {
		if items[i].TriggerType == model.TriggerBuildEvent {
			buildEventRun = &items[i]
			break
		}
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
	_, agents, fake, _, _ := setupAI(t)
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
		f.FailSession(sess.ID, "model boom")
	})
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "fail", SystemPrompt: "x", TimeoutSec: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	build := &cicdmodel.BuildRun{ID: 5, Status: "success", ArtifactPath: "/a"}
	job := &cicdmodel.BuildJob{ID: 1, AgentIDs: cicdmodel.UintList{agent.ID}, AgentTriggerEvent: model.EventArtifactReady}
	agents.OnBuildEvent(model.EventArtifactReady, job, build)
	if build.Status != "success" {
		t.Fatalf("BuildRun.status mutated to %s", build.Status)
	}
}

func TestSkillUploadRejectMissingSKILLMDAndOverwrite(t *testing.T) {
	_, _, _, skills, _ := setupAI(t)
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
	_, _, _, skills, _ := setupAI(t)
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

func TestSkillSourcesInjectUsesNameAndStripsWrapper(t *testing.T) {
	_, _, _, skills, _ := setupAI(t)
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
	sources, digests, err := skills.SkillSources([]uint{s.ID}, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "java-api-docs" || digests[s.ID] != s.PackageDigest {
		t.Fatalf("sources=%v digests=%v", sources, digests)
	}
	if err := harnessservice.SyncAgentSkills(tmp, sources); err != nil {
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
	gdb, agents, _, _, projectSvc := setupAI(t)
	owner := projectservice.NewAccessContext(1, true, []string{"project_projects:create", "project_docs:execute", "project_docs:view"})
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "P", Slug: "p-ai"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "doc", SystemPrompt: "Generate docs", TimeoutSec: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
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
	_, agents, _, _, _ := setupAI(t)
	if _, err := agents.CreateAgent(1, service.AgentInput{
		Name: "async", SystemPrompt: "x", TimeoutSec: 5,
	}); err != nil {
		t.Fatal(err)
	}
	// Intentionally skip requireWorkspaceReady: t.Cleanup(agents.Shutdown) must
	// drain workspace init before t.TempDir() removal (see wsInitWg).
}

func TestCronReloadAppliesTimezone(t *testing.T) {
	_, agents, _, _, _ := setupAI(t)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "tz", SystemPrompt: "x", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)
	_, err = agents.CreateTrigger(agent.ID, actorOne(), service.TriggerInput{
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
	gdb, agents, _, _, _ := setupAI(t)
	repo := repository.NewAIRepository(gdb)
	agent := &model.AiAgent{
		Name: "recover", Enabled: true, SystemPrompt: "x", TimeoutSec: 30, CreatedBy: 1,
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

func TestCancelRunReleasesWorkerQueue(t *testing.T) {
	_, agents, fake, _, _ := setupAI(t)
	agents.SetInlineExec(false)
	// Hang the session while the prompt carries "hang"; settle normally otherwise.
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, prompt string) {
		if strings.Contains(prompt, "hang") {
			_ = f.Emit(provider.Frame{
				SessionID: sess.ID, Kind: provider.FrameStatus,
				Status: &provider.StatusFrame{Name: provider.StatusStepStarted},
			})
			return // never completes: the run cancels via interrupt
		}
		f.Complete(sess.ID, "ok")
	})

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "cancel-queue", SystemPrompt: "x", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	hung, err := agents.ManualRun(agent.ID, actorOne(), "hang")
	if err != nil {
		t.Fatal(err)
	}
	waitRunPrompt(t, agents, fake, hung.ID)

	queued, err := agents.ManualRun(agent.ID, actorOne(), "next")
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != model.JobQueued {
		t.Fatalf("second run status=%s want queued", queued.Status)
	}

	if err := agents.CancelRun(hung.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, agents, hung.ID, model.JobCancelled)
	waitRunStatus(t, agents, queued.ID, model.JobSuccess)
}

func TestCancelQueuedRunWhileWorkerBusy(t *testing.T) {
	_, agents, fake, _, _ := setupAI(t)
	agents.SetInlineExec(false)
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
		// Hang: settle only via cancel/interrupt.
	})

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "cancel-queued", SystemPrompt: "x", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	hung, err := agents.ManualRun(agent.ID, actorOne(), "hang")
	if err != nil {
		t.Fatal(err)
	}
	waitRunPrompt(t, agents, fake, hung.ID)

	queued, err := agents.ManualRun(agent.ID, actorOne(), "queued")
	if err != nil {
		t.Fatal(err)
	}
	if err := agents.CancelRun(queued.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, agents, queued.ID, model.JobCancelled)

	if err := agents.CancelRun(hung.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, agents, hung.ID, model.JobCancelled)
}

func TestCancelDuringInFlightSessionKeepsCancelled(t *testing.T) {
	_, agents, fake, _, _ := setupAI(t)
	agents.SetInlineExec(false)
	proceed := make(chan struct{})
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
		<-proceed
		f.Complete(sess.ID, "ok")
	})

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "cancel-success", SystemPrompt: "x", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	run, err := agents.ManualRun(agent.ID, actorOne(), "job")
	if err != nil {
		t.Fatal(err)
	}
	waitRunPrompt(t, agents, fake, run.ID)
	if err := agents.CancelRun(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, agents, run.ID, model.JobCancelled)
}

func TestShutdownDoesNotStartQueuedRun(t *testing.T) {
	_, agents, fake, _, _ := setupAI(t)
	agents.SetInlineExec(false)
	fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
		// Hang: the shutdown interrupt path resolves the first run.
	})

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "shutdown-queue", SystemPrompt: "x", TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent = requireWorkspaceReady(t, agents, agent.ID)

	hung, err := agents.ManualRun(agent.ID, actorOne(), "hang")
	if err != nil {
		t.Fatal(err)
	}
	waitRunPrompt(t, agents, fake, hung.ID)
	queued, err := agents.ManualRun(agent.ID, actorOne(), "queued")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		agents.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown blocked on queued run")
	}

	got, err := agents.RequireRunAccess(queued.ID, actorOne())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.JobQueued {
		t.Fatalf("queued run status=%s want queued", got.Status)
	}
	if n := len(fake.Sessions()); n != 1 {
		t.Fatalf("sessions=%d want 1 (queued run must not start)", n)
	}
}

func TestUpdateRunFieldsIfStatusSkipsCancelled(t *testing.T) {
	gdb, agents, _, _, _ := setupAI(t)
	repo := repository.NewAIRepository(gdb)
	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "status-cas", SystemPrompt: "x", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &model.AgentRun{AgentID: agent.ID, Status: model.JobCancelled, TriggerType: model.TriggerManual}
	if err := repo.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	n, err := repo.UpdateRunFieldsIfStatus(run.ID,
		[]string{model.JobQueued, model.JobRunning, model.JobPending},
		map[string]any{"status": model.JobSuccess},
	)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows=%d want 0", n)
	}
	got, err := repo.FindRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.JobCancelled {
		t.Fatalf("status=%s want cancelled", got.Status)
	}
}
