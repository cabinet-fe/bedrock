package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"
)

type memScriptJobStore struct {
	mu   sync.Mutex
	jobs map[uint]*model.ScriptJob
}

func (s *memScriptJobStore) FindByID(id uint) (*model.ScriptJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := *j
	return &cp, nil
}

func (s *memScriptJobStore) ListCronEnabled() ([]model.ScriptJob, error) {
	return nil, nil
}

type memScriptRunStore struct {
	mu   sync.Mutex
	runs map[uint]*model.ScriptRun
}

func (s *memScriptRunStore) FindByID(id uint) (*model.ScriptRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := *r
	return &cp, nil
}

func (s *memScriptRunStore) UpdateFields(id uint, fields map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return os.ErrNotExist
	}
	if v, ok := fields["status"].(string); ok {
		r.Status = v
	}
	if v, ok := fields["stage"].(string); ok {
		r.Stage = v
	}
	if v, ok := fields["log_path"].(string); ok {
		r.LogPath = v
	}
	if v, ok := fields["error_message"].(string); ok {
		r.ErrorMessage = v
	}
	if v, ok := fields["started_at"].(time.Time); ok {
		r.StartedAt = &v
	}
	if v, ok := fields["finished_at"].(time.Time); ok {
		r.FinishedAt = &v
	}
	if v, ok := fields["duration_ms"].(int64); ok {
		r.DurationMs = v
	}
	return nil
}

func (s *memScriptRunStore) ListByStatuses(statuses ...string) ([]model.ScriptRun, error) {
	return nil, nil
}

func (s *memScriptRunStore) MarkRunningInterrupted() (int64, error) { return 0, nil }

func (s *memScriptRunStore) HasNonTerminal(jobID uint) (bool, error) { return false, nil }

func TestScriptPipelineExecuteSuccess(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	logDir := filepath.Join(tmp, "logs")

	jobStore := &memScriptJobStore{jobs: map[uint]*model.ScriptJob{
		1: {
			ID:         1,
			Name:       "hello",
			Enabled:    true,
			ScriptType: "bash",
			Script:     "echo hello-${{ job.name }}",
		},
	}}
	runStore := &memScriptRunStore{runs: map[uint]*model.ScriptRun{
		10: {
			ID:          10,
			ScriptJobID: 1,
			RunNumber:   1,
			Status:      "queued",
			Stage:       "pending",
		},
	}}

	p := NewScriptPipeline(runStore, jobStore, nil, zap.NewNop(), ws, logDir)
	p.Execute(context.Background(), 10)

	run, err := runStore.FindByID(10)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" {
		t.Fatalf("status=%s err=%s", run.Status, run.ErrorMessage)
	}
	if run.LogPath == "" {
		t.Fatal("expected log path")
	}
	data, err := os.ReadFile(run.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello-hello") {
		t.Fatalf("log missing expanded output: %s", data)
	}
}

func TestScriptPipelineUnknownTemplateVar(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	jobStore := &memScriptJobStore{jobs: map[uint]*model.ScriptJob{
		1: {
			ID:         1,
			Name:       "x",
			Enabled:    true,
			ScriptType: "bash",
			Script:     "echo ${{ missing.var }}",
		},
	}}
	runStore := &memScriptRunStore{runs: map[uint]*model.ScriptRun{
		11: {ID: 11, ScriptJobID: 1, RunNumber: 1, Status: "queued", Stage: "pending"},
	}}
	p := NewScriptPipeline(runStore, jobStore, nil, zap.NewNop(), tmp, tmp)
	p.Execute(context.Background(), 11)
	run, _ := runStore.FindByID(11)
	if run.Status != "failed" {
		t.Fatalf("want failed, got %s", run.Status)
	}
	if !strings.Contains(run.ErrorMessage, "模板") {
		t.Fatalf("unexpected err: %s", run.ErrorMessage)
	}
}

func TestBuildScriptJobTemplateVars(t *testing.T) {
	job := &model.ScriptJob{ID: 2, Name: "n", EnvVarNames: []string{}}
	run := &model.ScriptRun{ID: 5}
	vars := buildScriptJobTemplateVars(job, run, "/tmp/ws", map[string]string{"FOO": "bar"})
	if vars["job.id"] != "2" || vars["job.name"] != "n" || vars["run.id"] != "5" {
		t.Fatalf("vars=%v", vars)
	}
	if vars["env.FOO"] != "bar" {
		t.Fatalf("env.FOO=%q", vars["env.FOO"])
	}
	if vars["workspace"] == "" {
		t.Fatal("workspace empty")
	}
}
