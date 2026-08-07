package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	aimodel "bedrock/internal/ai/model"
	aiservice "bedrock/internal/ai/service"
	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
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

type stubAgentLauncher struct {
	mu        sync.Mutex
	nextID    uint
	created   []uint
	cancelled []uint
	existing  map[uint]bool
}

func (s *stubAgentLauncher) GetAgent(id uint) (*aimodel.AiAgent, error) {
	if s.existing[id] {
		return &aimodel.AiAgent{ID: id, Name: fmt.Sprintf("agent-%d", id)}, nil
	}
	return nil, errors.New("agent not found")
}

func (s *stubAgentLauncher) CreateRun(agentID uint, in aiservice.CreateRunInput) (*aimodel.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.created = append(s.created, agentID)
	return &aimodel.AgentRun{ID: s.nextID, AgentID: agentID, TriggerType: in.TriggerType}, nil
}

func (s *stubAgentLauncher) CancelRun(id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = append(s.cancelled, id)
	return nil
}

func (s *stubAgentLauncher) createdIDs() []uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint, len(s.created))
	copy(out, s.created)
	return out
}

type orchFixture struct {
	orch       *PipelineOrchestrator
	runs       *repository.PipelineRunRepository
	build      *BuildRunService
	scriptRuns *ScriptRunService
	agents     *stubAgentLauncher
	sched      *stubRunScheduler
	jobIDs     []uint
	scriptIDs  []uint
	agentIDs   []uint
	pipeID     uint
}

// setupOrchestrator builds a v2 orchestrator over a temp sqlite DB. graphNodes/
// graphEdges are raw JSON fragments spliced between the start/end wrapper; the
// caller supplies the full node/edge list (wrapper included) via graphJSON.
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
	scriptJobRepo := repository.NewScriptJobRepository(gdb)
	scriptRunRepo := repository.NewScriptRunRepository(gdb)
	pipeRepo := repository.NewBuildPipelineRepository(gdb)
	pipeRunRepo := repository.NewPipelineRunRepository(gdb)

	repoSvc := resourceservice.NewRepositoryService(
		repoRepo,
		resourceservice.NewCredentialService(credRepo),
		stubGitBranches{branches: []string{"main"}},
	)
	jobSvc := NewBuildJobService(jobRepo, repoRepo, nil)
	buildRuns := NewBuildRunService(buildRunRepo, jobRepo)
	sched := &stubRunScheduler{}
	buildRuns.SetScheduler(sched)
	scriptRuns := NewScriptRunService(scriptRunRepo, scriptJobRepo)
	scriptRuns.SetScheduler(sched)
	agents := &stubAgentLauncher{existing: map[uint]bool{}}

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "orch-repo", RepoURL: "https://example.com/orch.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var jobIDs []uint
	for _, name := range []string{"job-a", "job-b", "job-c", "job-d"} {
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
	var scriptIDs []uint
	for _, name := range []string{"script-a", "script-b"} {
		sj := &model.ScriptJob{Name: name, Enabled: true, ScriptType: "bash", Script: "true", TriggerManual: true}
		if err := scriptJobRepo.Create(sj); err != nil {
			t.Fatal(err)
		}
		scriptIDs = append(scriptIDs, sj.ID)
	}
	agents.existing[1] = true
	agents.existing[2] = true

	orch := NewPipelineOrchestrator(pipeRepo, pipeRunRepo, jobRepo, scriptJobRepo, buildRuns, scriptRuns, agents, nil)
	buildRuns.SetTerminalHook(orch)
	scriptRuns.SetTerminalHook(orch)

	return &orchFixture{
		orch:       orch,
		runs:       pipeRunRepo,
		build:      buildRuns,
		scriptRuns: scriptRuns,
		agents:     agents,
		sched:      sched,
		jobIDs:     jobIDs,
		scriptIDs:  scriptIDs,
		pipeID:     0,
	}
}

// createPipeline persists a v2 pipeline with the given inner nodes/edges JSON.
// inner must include the start/end wrapper nodes and their edges.
func (f *orchFixture) createPipeline(t *testing.T, graphJSON string) uint {
	t.Helper()
	pipeRepo := f.orch.pipelines
	pipe := &model.BuildPipeline{
		Name:          "orch-pipe",
		Enabled:       true,
		GraphJSON:     graphJSON,
		TriggerManual: true,
		CreatedBy:     1,
	}
	if err := pipeRepo.Create(pipe); err != nil {
		t.Fatal(err)
	}
	return pipe.ID
}

func stageStatusByNode(pr *model.PipelineRun) map[string]string {
	m := make(map[string]string, len(pr.Stages))
	for _, st := range pr.Stages {
		m[st.NodeID] = st.Status
	}
	return m
}

func stageByNodeID(pr *model.PipelineRun, nodeID string) *model.PipelineStageRun {
	for i := range pr.Stages {
		if pr.Stages[i].NodeID == nodeID {
			return &pr.Stages[i]
		}
	}
	return nil
}

// --- Core flow tests ---

func TestFailureBranchReachesEnd_Success(t *testing.T) {
	f := setupOrchestrator(t)
	// start→a; a→b on_success; a→d on_failure; b→end; d→end. a fails → b skipped,
	// d runs; d succeeds → end reached → pipeline success despite the failure.
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"b","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"d","type":"scriptJob","data":{"script_job_id":%d}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"a","target":"b","data":{"condition":"on_success"}},
		{"id":"e2","source":"a","target":"d","data":{"condition":"on_failure"}},
		{"id":"e3","source":"b","target":"end"},
		{"id":"e4","source":"d","target":"end"}]}`,
		f.jobIDs[0], f.jobIDs[1], f.scriptIDs[0])
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	statuses := stageStatusByNode(pr)
	if statuses["start"] != "success" || statuses["a"] != "running" {
		t.Fatalf("initial statuses=%v", statuses)
	}

	// a fails.
	stageA := stageByNodeID(pr, "a")
	f.orch.OnBuildRunTerminal(&model.BuildRun{
		ID: *stageA.BuildRunID, BuildJobID: f.jobIDs[0], TriggerType: "pipeline", ErrorMessage: "boom",
	}, "failed")

	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses = stageStatusByNode(got)
	if got.Status != "running" {
		t.Fatalf("pipeline should survive branchable failure, got %s", got.Status)
	}
	if statuses["b"] != "skipped" {
		t.Fatalf("b should be skipped (success edge unmatched), got %s", statuses["b"])
	}
	if statuses["d"] != "running" {
		t.Fatalf("d should be running (failure edge matched), got %s", statuses["d"])
	}

	// d (script) succeeds → end reached → success; the skipped b stays skipped.
	stageD := stageByNodeID(got, "d")
	if stageD.ScriptRunID == nil {
		t.Fatal("stage d missing script_run_id")
	}
	f.orch.OnScriptRunTerminal(&model.ScriptRun{
		ID: *stageD.ScriptRunID, ScriptJobID: f.scriptIDs[0], TriggerType: "pipeline",
	}, "success")

	got, err = f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses = stageStatusByNode(got)
	if got.Status != "success" {
		t.Fatalf("pipeline status=%s, want success (failure was branched to end)", got.Status)
	}
	if got.ErrorMessage != "" {
		t.Fatalf("success must not leave an error_message, got %q", got.ErrorMessage)
	}
	if statuses["a"] != "failed" || statuses["d"] != "success" || statuses["end"] != "success" {
		t.Fatalf("final statuses=%v", statuses)
	}
}

func TestFailureWithoutBranch_FailsAtQuiescence(t *testing.T) {
	f := setupOrchestrator(t)
	// start→a→b→end (all on_success). a fails → b, end skipped → failed.
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"b","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"a","target":"b"},
		{"id":"e2","source":"b","target":"end"}]}`,
		f.jobIDs[0], f.jobIDs[1])
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageA := stageByNodeID(pr, "a")
	f.orch.OnBuildRunTerminal(&model.BuildRun{
		ID: *stageA.BuildRunID, TriggerType: "pipeline", ErrorMessage: "boom",
	}, "failed")

	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := stageStatusByNode(got)
	if got.Status != "failed" {
		t.Fatalf("pipeline status=%s, want failed", got.Status)
	}
	if statuses["b"] != "skipped" || statuses["end"] != "skipped" {
		t.Fatalf("skipped should propagate to b/end, got %v", statuses)
	}
}

func TestEndNodeCancelsInFlightSiblings(t *testing.T) {
	f := setupOrchestrator(t)
	// start→a, start→slow; a→end. a succeeds → end → success; slow cancelled.
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"slow","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"start","target":"slow"},
		{"id":"e2","source":"a","target":"end"}]}`,
		f.jobIDs[0], f.jobIDs[1])
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageA := stageByNodeID(pr, "a")
	stageSlow := stageByNodeID(pr, "slow")
	// Simulate the worker picking both builds up (Cancel via scheduler path).
	_ = f.build.runs.UpdateFields(*stageA.BuildRunID, map[string]interface{}{"status": "running", "stage": "building"})
	_ = f.build.runs.UpdateFields(*stageSlow.BuildRunID, map[string]interface{}{"status": "running", "stage": "building"})
	f.orch.OnBuildRunTerminal(&model.BuildRun{ID: *stageA.BuildRunID, TriggerType: "pipeline"}, "success")

	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("pipeline status=%s, want success after end", got.Status)
	}
	statuses := stageStatusByNode(got)
	if statuses["slow"] != "cancelled" {
		t.Fatalf("in-flight slow stage=%s, want cancelled", statuses["slow"])
	}
	found := false
	for _, id := range f.sched.cancelledIDs() {
		if id == *stageSlow.BuildRunID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Cancel on slow build run %d, got %v", *stageSlow.BuildRunID, f.sched.cancelledIDs())
	}

	// Late terminal for the cancelled sibling still syncs without changing outcome.
	f.orch.OnBuildRunTerminal(&model.BuildRun{ID: *stageSlow.BuildRunID, TriggerType: "pipeline"}, "cancelled")
	got, err = f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" || stageStatusByNode(got)["slow"] != "cancelled" {
		t.Fatalf("late terminal mutated outcome: %s %v", got.Status, stageStatusByNode(got))
	}
}

func TestAndJoinWithConditions(t *testing.T) {
	f := setupOrchestrator(t)
	// start→a, start→b; a→c on_success, b→c on_success; c→end. b finishes first:
	// c must wait for a. Then a succeeds → c fires → end → success.
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"b","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"c","type":"scriptJob","data":{"script_job_id":%d}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"start","target":"b"},
		{"id":"e2","source":"a","target":"c"},
		{"id":"e3","source":"b","target":"c"},
		{"id":"e4","source":"c","target":"end"}]}`,
		f.jobIDs[0], f.jobIDs[1], f.scriptIDs[0])
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageB := stageByNodeID(pr, "b")
	f.orch.OnBuildRunTerminal(&model.BuildRun{ID: *stageB.BuildRunID, TriggerType: "pipeline"}, "success")

	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s := stageStatusByNode(got)["c"]; s != "pending" {
		t.Fatalf("c fired before all predecessors terminal: %s", s)
	}

	stageA := stageByNodeID(got, "a")
	f.orch.OnBuildRunTerminal(&model.BuildRun{ID: *stageA.BuildRunID, TriggerType: "pipeline"}, "success")
	got, err = f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s := stageStatusByNode(got)["c"]; s != "running" {
		t.Fatalf("c should run after AND-join satisfied, got %s", s)
	}
}

func TestAgentNodeDispatchAndTerminal(t *testing.T) {
	f := setupOrchestrator(t)
	// start→agent→end; agent success → end → pipeline success.
	graph := `{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"ai","type":"agent","data":{"agent_id":1}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"ai"},
		{"id":"e1","source":"ai","target":"end"}]}`
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	created := f.agents.createdIDs()
	if len(created) != 1 || created[0] != 1 {
		t.Fatalf("agent CreateRun calls=%v", created)
	}
	stageAI := stageByNodeID(pr, "ai")
	if stageAI.AgentRunID == nil {
		t.Fatal("agent stage missing agent_run_id")
	}
	if stageAI.NodeType != "agent" || stageAI.AgentID != 1 {
		t.Fatalf("agent stage refs wrong: %+v", stageAI)
	}

	f.orch.OnAgentRunTerminal(&aimodel.AgentRun{ID: *stageAI.AgentRunID, TriggerType: aimodel.TriggerPipeline}, "success")
	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("pipeline status=%s, want success", got.Status)
	}
}

func TestAgentNodeFailureTakesFailureEdge(t *testing.T) {
	f := setupOrchestrator(t)
	// start→ai; ai→end on_failure. agent fails → end reached → success.
	graph := `{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"ai","type":"agent","data":{"agent_id":1}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"ai"},
		{"id":"e1","source":"ai","target":"end","data":{"condition":"on_failure"}}]}`
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageAI := stageByNodeID(pr, "ai")
	f.orch.OnAgentRunTerminal(&aimodel.AgentRun{ID: *stageAI.AgentRunID, TriggerType: aimodel.TriggerPipeline}, "failed")
	got, err := f.runs.FindByID(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("pipeline status=%s, want success via failure edge", got.Status)
	}
}

func TestNodeEnvOverridesPassedToRun(t *testing.T) {
	f := setupOrchestrator(t)
	cipher, err := pkg.Encrypt("override-value")
	if err != nil {
		t.Fatal(err)
	}
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d,"env_vars":[{"key":"FOO","value":"enc:v1:%s"}]}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"a","target":"end"}]}`,
		f.jobIDs[0], cipher)
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageA := stageByNodeID(pr, "a")
	run, err := f.build.runs.FindByID(*stageA.BuildRunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.EnvOverridesCipher == "" {
		t.Fatal("build run missing env overrides cipher")
	}
	vars, err := decryptJobEnvVars(run.EnvOverridesCipher)
	if err != nil {
		t.Fatal(err)
	}
	if vars["FOO"] != "override-value" {
		t.Fatalf("override FOO=%q", vars["FOO"])
	}
}

func TestCancelPipelineRunAPI(t *testing.T) {
	f := setupOrchestrator(t)
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d}},
		{"id":"s","type":"scriptJob","data":{"script_job_id":%d}},
		{"id":"ai","type":"agent","data":{"agent_id":1}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"a","target":"s"},
		{"id":"e2","source":"s","target":"ai"},
		{"id":"e3","source":"ai","target":"end"}]}`,
		f.jobIDs[0], f.scriptIDs[0])
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	stageA := stageByNodeID(pr, "a")
	_ = f.build.runs.UpdateFields(*stageA.BuildRunID, map[string]interface{}{"status": "running", "stage": "building"})

	got, err := f.orch.Cancel(pr.ID, 1, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("status=%s, want cancelled", got.Status)
	}
	statuses := stageStatusByNode(got)
	if statuses["a"] != "cancelled" {
		t.Fatalf("running stage a=%s, want cancelled", statuses["a"])
	}
	for _, n := range []string{"s", "ai", "end"} {
		if statuses[n] != "skipped" {
			t.Fatalf("pending stage %s=%s, want skipped", n, statuses[n])
		}
	}
	found := false
	for _, id := range f.sched.cancelledIDs() {
		if id == *stageA.BuildRunID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Cancel for build run %d, got %v", *stageA.BuildRunID, f.sched.cancelledIDs())
	}
	if _, err := f.orch.Cancel(pr.ID, 1, rbacmodel.DataScopeAll); !IsConflict(err) {
		t.Fatalf("second cancel err=%v, want conflict", err)
	}
}

func TestEnqueueRejectsInvalidGraph(t *testing.T) {
	f := setupOrchestrator(t)
	// No start node (legacy graph): rejected at enqueue (migration backfills stored graphs).
	pipeID := f.createPipeline(t, fmt.Sprintf(`{"nodes":[{"id":"a","type":"buildJob","data":{"build_job_id":%d}}],"edges":[]}`, f.jobIDs[0]))
	if _, err := f.orch.EnqueueInternal(pipeID, 1, "manual"); err == nil {
		t.Fatal("expected validation error for graph without start node")
	}
}

func TestGetSanitizesSnapshotEnvVars(t *testing.T) {
	f := setupOrchestrator(t)
	cipher, err := pkg.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	graph := fmt.Sprintf(`{"nodes":[
		{"id":"start","type":"start","data":{}},
		{"id":"a","type":"buildJob","data":{"build_job_id":%d,"env_vars":[{"key":"TOKEN","value":"enc:v1:%s"}]}},
		{"id":"end","type":"end","data":{}}],"edges":[
		{"id":"e0","source":"start","target":"a"},
		{"id":"e1","source":"a","target":"end"}]}`,
		f.jobIDs[0], cipher)
	pipeID := f.createPipeline(t, graph)

	pr, err := f.orch.EnqueueInternal(pipeID, 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.orch.Get(pr.ID, 1, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParsePipelineGraph(got.SnapshotJSON)
	if err != nil {
		t.Fatal(err)
	}
	kv := NodeByID(g)["a"].Data.EnvVars[0]
	if kv.Value != "" || !kv.HasValue {
		t.Fatalf("snapshot env not sanitized: %+v", kv)
	}
}

func TestTerminalHooksIgnoreNonPipeline(t *testing.T) {
	o := &PipelineOrchestrator{locks: make(map[uint]*sync.Mutex)}
	o.OnBuildRunTerminal(&model.BuildRun{ID: 1, TriggerType: "manual", Status: "success"}, "success")
	o.OnScriptRunTerminal(&model.ScriptRun{ID: 1, TriggerType: "manual", Status: "success"}, "success")
	o.OnAgentRunTerminal(&aimodel.AgentRun{ID: 1, TriggerType: aimodel.TriggerManual}, "success")
}
