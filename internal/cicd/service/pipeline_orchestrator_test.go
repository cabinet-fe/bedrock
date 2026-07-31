package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacmodel "bedrock/internal/rbac/model"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
)

type stubRunScheduler struct {
	mu        sync.Mutex
	cancelled []uint
}

func (s *stubRunScheduler) Submit(uint) error { return nil }

func (s *stubRunScheduler) Cancel(runID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = append(s.cancelled, runID)
	return true
}

func (s *stubRunScheduler) cancelledIDs() []uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint, len(s.cancelled))
	copy(out, s.cancelled)
	return out
}

type stubGitBranches struct {
	branches []string
}

func (s stubGitBranches) ListBranches(string, string, string, string) ([]string, error) {
	return s.branches, nil
}

type orchFixture struct {
	orch   *PipelineOrchestrator
	runs   *repository.PipelineRunRepository
	build  *BuildRunService
	sched  *stubRunScheduler
	jobIDs []uint
	pipeID uint
}

func setupOrchestrator(t *testing.T) *orchFixture {
	t.Helper()
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "orch.sqlite"),
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

	credRepo := resourcerepo.NewCredentialRepository(gdb)
	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	jobRepo := repository.NewBuildJobRepository(gdb)
	buildRunRepo := repository.NewBuildRunRepository(gdb)
	pipeRepo := repository.NewBuildPipelineRepository(gdb)
	pipeRunRepo := repository.NewPipelineRunRepository(gdb)

	repoSvc := resourceservice.NewRepositoryService(
		repoRepo,
		resourceservice.NewCredentialService(credRepo),
		stubGitBranches{branches: []string{"main"}},
	)
	jobSvc := NewBuildJobService(jobRepo, repoRepo)
	buildRuns := NewBuildRunService(buildRunRepo, jobRepo)
	sched := &stubRunScheduler{}
	buildRuns.SetScheduler(sched)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "orch-repo", RepoURL: "https://example.com/orch.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var jobIDs []uint
	for _, name := range []string{"job-a", "job-b", "job-c"} {
		job, err := jobSvc.Create(1, CreateBuildJobInput{
			RepositoryID: repo.ID,
			Name:         name,
			BuildScript:  "true",
		})
		if err != nil {
			t.Fatal(err)
		}
		jobIDs = append(jobIDs, job.ID)
	}

	graph := fmt.Sprintf(
		`{"nodes":[{"id":"a","data":{"build_job_id":%d}},{"id":"b","data":{"build_job_id":%d}},{"id":"c","data":{"build_job_id":%d}}],"edges":[]}`,
		jobIDs[0], jobIDs[1], jobIDs[2],
	)
	pipe := &model.BuildPipeline{
		Name:          "orch-pipe",
		Enabled:       true,
		GraphJSON:     graph,
		TriggerManual: true,
		CreatedBy:     1,
	}
	if err := pipeRepo.Create(pipe); err != nil {
		t.Fatal(err)
	}

	orch := NewPipelineOrchestrator(pipeRepo, pipeRunRepo, jobRepo, buildRuns, nil)
	buildRuns.SetTerminalHook(orch)

	return &orchFixture{
		orch:   orch,
		runs:   pipeRunRepo,
		build:  buildRuns,
		sched:  sched,
		jobIDs: jobIDs,
		pipeID: pipe.ID,
	}
}

func seedParallelRunningPipeline(t *testing.T, f *orchFixture) (pr *model.PipelineRun, runA, runB *model.BuildRun) {
	t.Helper()
	now := time.Now()
	p, err := f.orch.pipelines.FindByID(f.pipeID)
	if err != nil {
		t.Fatal(err)
	}
	pr = &model.PipelineRun{
		BuildPipelineID: f.pipeID,
		RunNumber:       1,
		Status:          "running",
		TriggerType:     "manual",
		TriggeredBy:     1,
		SnapshotJSON:    p.GraphJSON,
		StartedAt:       &now,
	}
	if err := f.runs.Create(pr); err != nil {
		t.Fatal(err)
	}

	runA, err = f.build.EnqueueInternal(f.jobIDs[0], 1, engine.EnqueueParams{TriggerType: "pipeline"})
	if err != nil {
		t.Fatal(err)
	}
	runB, err = f.build.EnqueueInternal(f.jobIDs[1], 1, engine.EnqueueParams{TriggerType: "pipeline"})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.build.runs.UpdateFields(runA.ID, map[string]interface{}{"status": "running", "stage": "building"})
	_ = f.build.runs.UpdateFields(runB.ID, map[string]interface{}{"status": "running", "stage": "building"})
	runA.Status = "running"
	runB.Status = "running"

	idA, idB := runA.ID, runB.ID
	stages := []model.PipelineStageRun{
		{PipelineRunID: pr.ID, NodeID: "a", BuildJobID: f.jobIDs[0], BuildRunID: &idA, Status: "running", StartedAt: &now},
		{PipelineRunID: pr.ID, NodeID: "b", BuildJobID: f.jobIDs[1], BuildRunID: &idB, Status: "running", StartedAt: &now},
		{PipelineRunID: pr.ID, NodeID: "c", BuildJobID: f.jobIDs[2], Status: "pending"},
	}
	if err := f.runs.CreateStages(stages); err != nil {
		t.Fatal(err)
	}
	pr, err = f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	return pr, runA, runB
}

func stageStatusByNode(pr *model.PipelineRun) map[string]string {
	m := make(map[string]string, len(pr.Stages))
	for _, st := range pr.Stages {
		m[st.NodeID] = st.Status
	}
	return m
}

func TestParallelStageFailCancelsSiblings(t *testing.T) {
	f := setupOrchestrator(t)
	pr, runA, runB := seedParallelRunningPipeline(t, f)

	runA.ErrorMessage = "boom"
	f.orch.OnBuildRunTerminal(runA, "failed")

	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Fatalf("pipeline status=%s, want failed", got.Status)
	}
	statuses := stageStatusByNode(got)
	if statuses["a"] != "failed" {
		t.Fatalf("stage a=%s, want failed", statuses["a"])
	}
	if statuses["b"] != "cancelled" {
		t.Fatalf("stage b=%s, want cancelled", statuses["b"])
	}
	if statuses["c"] != "skipped" {
		t.Fatalf("stage c=%s, want skipped", statuses["c"])
	}

	cancelled := f.sched.cancelledIDs()
	foundB := false
	for _, id := range cancelled {
		if id == runB.ID {
			foundB = true
		}
		if id == runA.ID {
			t.Fatalf("should not cancel already-failed build run %d", runA.ID)
		}
	}
	if !foundB {
		t.Fatalf("expected Cancel on sibling build run %d, got %v", runB.ID, cancelled)
	}

	// Late terminal for sibling should still sync (idempotent cancelled).
	f.orch.OnBuildRunTerminal(runB, "cancelled")
	got, err = f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stageStatusByNode(got)["b"] != "cancelled" {
		t.Fatalf("late terminal left stage b=%s", stageStatusByNode(got)["b"])
	}
	if got.Status != "failed" {
		t.Fatalf("late terminal changed pipeline status to %s", got.Status)
	}
}

func TestCancelPipelineRunAPI(t *testing.T) {
	f := setupOrchestrator(t)
	pr, runA, runB := seedParallelRunningPipeline(t, f)

	got, err := f.orch.Cancel(pr.ID, 1, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("status=%s, want cancelled", got.Status)
	}
	statuses := stageStatusByNode(got)
	if statuses["a"] != "cancelled" || statuses["b"] != "cancelled" {
		t.Fatalf("running stages=%v, want cancelled", statuses)
	}
	if statuses["c"] != "skipped" {
		t.Fatalf("pending stage=%s, want skipped", statuses["c"])
	}

	cancelled := f.sched.cancelledIDs()
	want := map[uint]bool{runA.ID: true, runB.ID: true}
	for _, id := range cancelled {
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing Cancel for build runs %v; got %v", want, cancelled)
	}

	if _, err := f.orch.Cancel(pr.ID, 1, rbacmodel.DataScopeAll); !IsConflict(err) {
		t.Fatalf("second cancel err=%v, want conflict", err)
	}
}

func TestOrchestratorUnlockDownstream(t *testing.T) {
	// a → b, a → c ; success(a) should enqueue b and c
	graph := `{"nodes":[{"id":"a","data":{"build_job_id":1}},{"id":"b","data":{"build_job_id":2}},{"id":"c","data":{"build_job_id":3}}],"edges":[{"id":"e1","source":"a","target":"b"},{"id":"e2","source":"a","target":"c"}]}`
	g, err := ParsePipelineGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePipelineDAG(g, func(uint) bool { return true }); err != nil {
		t.Fatal(err)
	}

	stages := []model.PipelineStageRun{
		{ID: 1, PipelineRunID: 10, NodeID: "a", BuildJobID: 1, Status: "success"},
		{ID: 2, PipelineRunID: 10, NodeID: "b", BuildJobID: 2, Status: "pending"},
		{ID: 3, PipelineRunID: 10, NodeID: "c", BuildJobID: 3, Status: "pending"},
	}
	succ, pred := GraphAdjacency(g)
	statusByNode := map[string]string{"a": "success", "b": "pending", "c": "pending"}
	byNode := stageByNode(stages)

	ready := []string{}
	for _, nextID := range succ["a"] {
		st := byNode[nextID]
		if st.Status != "pending" {
			continue
		}
		ok := true
		for _, pID := range pred[nextID] {
			if statusByNode[pID] != "success" {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, nextID)
		}
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready stages, got %v", ready)
	}
}

func TestOnBuildRunTerminalIgnoresNonPipeline(t *testing.T) {
	o := &PipelineOrchestrator{locks: make(map[uint]*sync.Mutex)}
	o.OnBuildRunTerminal(&model.BuildRun{ID: 1, TriggerType: "manual", Status: "success"}, "success")
}
