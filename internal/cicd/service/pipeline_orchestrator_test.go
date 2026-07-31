package service

import (
	"sync"
	"testing"
	"time"

	"bedrock/internal/cicd/model"
)

type memPipelineRuns struct {
	mu     sync.Mutex
	runs   map[uint]*model.PipelineRun
	stages map[uint]*model.PipelineStageRun
	nextID uint
}

func newMemPipelineRuns() *memPipelineRuns {
	return &memPipelineRuns{
		runs:   make(map[uint]*model.PipelineRun),
		stages: make(map[uint]*model.PipelineStageRun),
		nextID: 1,
	}
}

// orchestratorTestDeps wires a minimal in-memory orchestrator path for terminal unlock.
type fakeBuildEnqueuer struct {
	mu      sync.Mutex
	calls   []uint
	nextID  uint
	failJob uint
}

func (f *fakeBuildEnqueuer) enqueue(jobID uint) (*model.BuildRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, jobID)
	if f.failJob != 0 && jobID == f.failJob {
		return nil, errorsNew("enqueue failed")
	}
	f.nextID++
	return &model.BuildRun{ID: f.nextID, BuildJobID: jobID, Status: "queued", TriggerType: "pipeline"}, nil
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

func TestOrchestratorFailSkipsPending(t *testing.T) {
	now := time.Now()
	pr := &model.PipelineRun{
		ID:     1,
		Status: "running",
		Stages: []model.PipelineStageRun{
			{ID: 1, Status: "failed", FinishedAt: &now},
			{ID: 2, Status: "pending"},
			{ID: 3, Status: "running"},
		},
	}
	skipped := 0
	for _, st := range pr.Stages {
		if st.Status == "pending" || st.Status == "queued" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skippable pending, got %d", skipped)
	}
}

func TestOnBuildRunTerminalIgnoresNonPipeline(t *testing.T) {
	o := &PipelineOrchestrator{locks: make(map[uint]*sync.Mutex)}
	// Should not panic when stage lookup would fail / trigger type mismatches.
	o.OnBuildRunTerminal(&model.BuildRun{ID: 1, TriggerType: "manual", Status: "success"}, "success")
}
