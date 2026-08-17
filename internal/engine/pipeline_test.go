package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	resourcemodel "bedrock/internal/resource/model"
)

type memRunStore struct {
	mu       sync.Mutex
	runs     map[uint]*model.BuildRun
	attempts []model.BuildDeployAttempt
	nextID   uint
}

func newMemRunStore(runs ...*model.BuildRun) *memRunStore {
	m := &memRunStore{runs: map[uint]*model.BuildRun{}, nextID: 1}
	for _, r := range runs {
		cp := *r
		m.runs[r.ID] = &cp
	}
	return m
}

func (m *memRunStore) FindByID(id uint) (*model.BuildRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := *r
	return &cp, nil
}

func (m *memRunStore) UpdateFields(id uint, fields map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return os.ErrNotExist
	}
	applyRunFields(r, fields)
	return nil
}

func applyRunFields(r *model.BuildRun, fields map[string]interface{}) {
	for k, v := range fields {
		switch k {
		case "status":
			r.Status = v.(string)
		case "stage":
			r.Stage = v.(string)
		case "distribution_summary":
			r.DistributionSummary = v.(string)
		case "error_message":
			r.ErrorMessage = v.(string)
		case "log_path":
			r.LogPath = v.(string)
		case "artifact_path":
			r.ArtifactPath = v.(string)
		case "artifact_kind":
			r.ArtifactKind = v.(string)
		case "commit_hash":
			r.CommitHash = v.(string)
		case "trigger_type":
			r.TriggerType = v.(string)
		case "snapshot_json":
			r.SnapshotJSON = v.(string)
		case "duration_ms":
			r.DurationMs = v.(int64)
		case "finished_at":
			if t, ok := v.(time.Time); ok {
				r.FinishedAt = &t
			} else if t, ok := v.(*time.Time); ok {
				r.FinishedAt = t
			}
		case "started_at":
			if t, ok := v.(*time.Time); ok {
				r.StartedAt = t
			}
		}
	}
}

func (m *memRunStore) CreateAttempt(a *model.BuildDeployAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cp := *a
	cp.ID = m.nextID
	m.attempts = append(m.attempts, cp)
	*a = cp
	return nil
}

func (m *memRunStore) UpdateAttempt(a *model.BuildDeployAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.attempts {
		if m.attempts[i].ID == a.ID {
			m.attempts[i] = *a
			return nil
		}
	}
	return os.ErrNotExist
}

func (m *memRunStore) NextBatchNo(runID uint) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	max := 0
	for _, a := range m.attempts {
		if a.BuildRunID == runID && a.BatchNo > max {
			max = a.BatchNo
		}
	}
	return max + 1, nil
}

func (m *memRunStore) ListByStatuses(statuses ...string) ([]model.BuildRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	var out []model.BuildRun
	for _, r := range m.runs {
		if want[r.Status] {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memRunStore) MarkRunningInterrupted() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, r := range m.runs {
		if r.Status == "running" {
			r.Status = "interrupted"
			r.Stage = "idle"
			n++
		}
	}
	return n, nil
}

func (m *memRunStore) HasNonTerminal(jobID uint) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.BuildJobID == jobID && (r.Status == "queued" || r.Status == "running") {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRunStore) ListArtifactsByJob(jobID uint) ([]model.BuildRun, error) {
	return nil, nil
}

type memJobStore struct {
	job     *model.BuildJob
	targets []model.DeployTarget
}

func (m *memJobStore) FindByID(id uint) (*model.BuildJob, error) {
	if m.job == nil || m.job.ID != id {
		return nil, os.ErrNotExist
	}
	cp := *m.job
	return &cp, nil
}
func (m *memJobStore) ListDeployTargets(jobID uint) ([]model.DeployTarget, error) {
	return append([]model.DeployTarget(nil), m.targets...), nil
}
func (m *memJobStore) ListCronEnabled() ([]model.BuildJob, error) { return nil, nil }
func (m *memJobStore) ListByRepositoryID(uint) ([]model.BuildJob, error) {
	return nil, nil
}

type memRepoStore struct{ repo *resourcemodel.Repository }

func (m *memRepoStore) FindByID(id uint) (*resourcemodel.Repository, error) {
	if m.repo == nil || m.repo.ID != id {
		return nil, os.ErrNotExist
	}
	cp := *m.repo
	return &cp, nil
}

type memServerStore struct{}

func (m *memServerStore) FindByID(uint) (*resourcemodel.Server, error) { return nil, os.ErrNotExist }

type nopSecrets struct{}

func (nopSecrets) Resolve(uint) (string, string, string, string, error) {
	return "", "", "", "", nil
}

func TestDistributionFailureKeepsSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "out")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0644)

	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "success", Stage: "distributing", DistributionSummary: "running",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{ID: 10, ArtifactFormat: "gzip"},
		targets: []model.DeployTarget{
			{ID: 1, BuildJobID: 10, Method: "local", RemotePath: filepath.Join(tmp, "ok")},
			{ID: 2, BuildJobID: 10, Method: "local", RemotePath: "relative-bad"}, // fails: not absolute
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), tmp, tmp, tmp, tmp)
	p.runDistributions(context.Background(), run, jobStore.job, src, func(string) {}, nil)

	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s want success", got.Status)
	}
	if got.DistributionSummary != "partial" {
		t.Fatalf("summary=%s want partial", got.DistributionSummary)
	}
	if len(store.attempts) != 2 {
		t.Fatalf("attempts=%d want 2", len(store.attempts))
	}
}

func TestDistributionAllFailedKeepsSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "out")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0644)

	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "success", Stage: "distributing", DistributionSummary: "running",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{ID: 10, ArtifactFormat: "gzip"},
		targets: []model.DeployTarget{
			{ID: 1, BuildJobID: 10, Method: "local", RemotePath: "relative-bad-1"},
			{ID: 2, BuildJobID: 10, Method: "local", RemotePath: "relative-bad-2"},
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), tmp, tmp, tmp, tmp)
	p.runDistributions(context.Background(), run, jobStore.job, src, func(string) {}, nil)

	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s want success", got.Status)
	}
	if got.DistributionSummary != "all_failed" {
		t.Fatalf("summary=%s want all_failed", got.DistributionSummary)
	}
	if got.Stage != "idle" {
		t.Fatalf("stage=%s want idle", got.Stage)
	}
	if len(store.attempts) != 2 {
		t.Fatalf("attempts=%d want 2", len(store.attempts))
	}
}

// TestArchiveMarksSuccessAndArtifactDownloadable exercises clone→build→archive→markArtifactSuccess
// (not only distribute-assuming-already-success) and proves the artifact file is on disk.
func TestArchiveMarksSuccessAndArtifactDownloadable(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "ws")
	artifactRoot := filepath.Join(tmp, "artifacts")
	logDir := filepath.Join(tmp, "logs")
	cacheDir := filepath.Join(tmp, "cache")

	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 3,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID:             10,
			RepositoryID:   1,
			Branch:         "main",
			BuildScript:    "mkdir -p dist && echo hello > dist/app.txt",
			ArtifactPaths:  []string{"dist"},
			ArtifactFormat: "gzip",
			MaxArtifacts:   5,
		},
	}
	repoStore := &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}
	p := NewPipeline(store, jobStore, repoStore, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		workspace, artifactRoot, logDir, cacheDir)

	p.Execute(context.Background(), 1)

	got, err := store.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("status=%s want success (error=%q)", got.Status, got.ErrorMessage)
	}
	if got.Stage != "idle" {
		t.Fatalf("stage=%s want idle (no deploy targets)", got.Stage)
	}
	if got.DistributionSummary != "none" {
		t.Fatalf("summary=%s want none", got.DistributionSummary)
	}
	if strings.TrimSpace(got.ArtifactPath) == "" {
		t.Fatal("expected artifact_path after archive")
	}
	if !strings.HasSuffix(got.ArtifactPath, "build-003.tar.gz") {
		t.Fatalf("artifact_path=%q want build-003.tar.gz", got.ArtifactPath)
	}
	info, err := os.Stat(got.ArtifactPath)
	if err != nil {
		t.Fatalf("artifact not downloadable on disk: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("artifact file is empty")
	}
	// Round-trip: archive must contain the build output.
	extractDir := filepath.Join(tmp, "extract")
	if err := extractArtifactArchive(got.ArtifactPath, extractDir, "gzip"); err != nil {
		t.Fatalf("extract artifact: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(extractDir, "app.txt"))
	if err != nil {
		t.Fatalf("read extracted app.txt: %v", err)
	}
	if strings.TrimSpace(string(body)) != "hello" {
		t.Fatalf("artifact content=%q want hello", body)
	}
}

func TestMarkArtifactSuccess_WithDistributeStage(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	run := &model.BuildRun{ID: 1, BuildJobID: 10, BuildNumber: 1, Status: "running", Stage: "archiving"}
	store := newMemRunStore(run)
	p := NewPipeline(store, &memJobStore{}, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), tmp, tmp, tmp, tmp)

	p.markArtifactSuccess(run, func(string) {}, true)
	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s", got.Status)
	}
	if got.Stage != "distributing" {
		t.Fatalf("stage=%s want distributing", got.Stage)
	}
	if got.DistributionSummary != "running" {
		t.Fatalf("summary=%s want running", got.DistributionSummary)
	}
}

func TestPipeline_MissingWorkDirFailsClearly(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			WorkDir:     "does-not-exist",
			BuildScript: "echo should-not-run",
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)

	got, _ := store.FindByID(1)
	if got.Status != "failed" {
		t.Fatalf("status=%s want failed (error=%q)", got.Status, got.ErrorMessage)
	}
	if !strings.Contains(got.ErrorMessage, "工作目录不存在") {
		t.Fatalf("error_message=%q", got.ErrorMessage)
	}
	if strings.Contains(got.ErrorMessage, "fork/exec") {
		t.Fatalf("must not misreport as missing sh: %q", got.ErrorMessage)
	}
	logBody, err := os.ReadFile(got.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBody), "Build work dir:") {
		t.Fatalf("log missing work dir line: %s", logBody)
	}
	if !strings.Contains(string(logBody), "工作目录不存在") {
		t.Fatalf("log missing clear error: %s", logBody)
	}
}

func TestPipeline_PostBuildScriptRunsBeforeArchive(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 2,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			BuildScript:     "mkdir -p target && echo jar > target/app.jar",
			PostBuildScript: "mkdir -p dist && cp target/app.jar dist/app.jar",
			ArtifactPaths:   []string{"dist"},
			ArtifactFormat:  "gzip",
			MaxArtifacts:    5,
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)

	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status=%s want success (error=%q)", got.Status, got.ErrorMessage)
	}
	logBody, err := os.ReadFile(got.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(logBody)
	if !strings.Contains(text, "=== Stage: Post-build ===") {
		t.Fatalf("missing post-build stage in log: %s", text)
	}
	postIdx := strings.Index(text, "=== Stage: Post-build ===")
	archIdx := strings.Index(text, "=== Stage: Collecting Artifact ===")
	if postIdx < 0 || archIdx < 0 || postIdx > archIdx {
		t.Fatalf("post-build should run before archive: post=%d archive=%d", postIdx, archIdx)
	}
	extractDir := filepath.Join(tmp, "extract")
	if err := extractArtifactArchive(got.ArtifactPath, extractDir, "gzip"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(extractDir, "app.jar"))
	if err != nil {
		t.Fatalf("artifact missing post-build output: %v", err)
	}
	if strings.TrimSpace(string(body)) != "jar" {
		t.Fatalf("content=%q", body)
	}
}

func TestPipeline_PostBuildFailureFailsRun(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := initLocalGitRepo(t)
	tmp := t.TempDir()
	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "queued", Stage: "pending", Branch: "main",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job: &model.BuildJob{
			ID: 10, RepositoryID: 1, Branch: "main",
			BuildScript:     "true",
			PostBuildScript: "exit 7",
			ArtifactPaths:   []string{"dist"},
		},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{
		repo: &resourcemodel.Repository{ID: 1, RepoURL: repoDir, AuthType: "none"},
	}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(),
		filepath.Join(tmp, "ws"), filepath.Join(tmp, "art"), filepath.Join(tmp, "logs"), filepath.Join(tmp, "cache"))

	p.Execute(context.Background(), 1)

	got, _ := store.FindByID(1)
	if got.Status != "failed" {
		t.Fatalf("status=%s want failed (error=%q)", got.Status, got.ErrorMessage)
	}
	if !strings.Contains(got.ErrorMessage, "构建后脚本失败") {
		t.Fatalf("error_message=%q", got.ErrorMessage)
	}
}

func initLocalGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("repo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestRedeployAppendsAttempts(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "out")
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0644)
	dest := filepath.Join(tmp, "dest")

	run := &model.BuildRun{
		ID: 1, BuildJobID: 10, BuildNumber: 1,
		Status: "success", Stage: "idle", DistributionSummary: "all_success",
	}
	store := newMemRunStore(run)
	jobStore := &memJobStore{
		job:     &model.BuildJob{ID: 10, ArtifactFormat: "gzip"},
		targets: []model.DeployTarget{{ID: 1, BuildJobID: 10, Method: "local", RemotePath: dest}},
	}
	p := NewPipeline(store, jobStore, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), tmp, tmp, tmp, tmp)

	p.runDistributions(context.Background(), run, jobStore.job, src, func(string) {}, nil)
	if len(store.attempts) != 1 || store.attempts[0].BatchNo != 1 {
		t.Fatalf("batch1 attempts=%v", store.attempts)
	}
	p.runDistributions(context.Background(), run, jobStore.job, src, func(string) {}, nil)
	if len(store.attempts) != 2 {
		t.Fatalf("after redeploy attempts=%d want 2", len(store.attempts))
	}
	if store.attempts[1].BatchNo != 2 {
		t.Fatalf("batch_no=%d want 2", store.attempts[1].BatchNo)
	}
	got, _ := store.FindByID(1)
	if got.Status != "success" || got.DistributionSummary != "all_success" {
		t.Fatalf("status=%s summary=%s", got.Status, got.DistributionSummary)
	}
}

func TestSchedulerRecovery_QueuedAndInterrupted(t *testing.T) {
	t.Parallel()
	store := newMemRunStore(
		&model.BuildRun{ID: 1, Status: "queued", Stage: "pending"},
		&model.BuildRun{ID: 2, Status: "running", Stage: "building"},
		&model.BuildRun{ID: 3, Status: "success", Stage: "idle"},
	)
	// Pipeline that no-ops quickly for missing job
	p := NewPipeline(store, &memJobStore{}, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	s := NewScheduler(2, p, store, zap.NewNop())
	s.Start()
	defer s.Shutdown()

	if err := s.RecoverOnStartup(); err != nil {
		t.Fatal(err)
	}
	r2, _ := store.FindByID(2)
	if r2.Status != "interrupted" {
		t.Fatalf("running→interrupted got %s", r2.Status)
	}
	// queued should still be queued (submitted to channel; may flip to failed quickly due to missing job)
	time.Sleep(50 * time.Millisecond)
	r1, _ := store.FindByID(1)
	if r1.Status == "running" {
		// ok if started
	} else if r1.Status != "queued" && r1.Status != "failed" {
		t.Fatalf("unexpected queued recovery status %s", r1.Status)
	}
}

func TestCronOverlapSkip(t *testing.T) {
	t.Parallel()
	store := newMemRunStore(&model.BuildRun{ID: 1, BuildJobID: 5, Status: "running"})
	var enqueued int
	enq := &stubEnqueuer{fn: func(jobID, _ uint, _ EnqueueParams) (*model.BuildRun, error) {
		enqueued++
		return &model.BuildRun{ID: 99, BuildJobID: jobID, Status: "queued"}, nil
	}}
	cs := NewCronScheduler(&memJobStore{}, store, enq, nil, zap.NewNop())
	cs.TriggerNow(5)
	if enqueued != 0 {
		t.Fatalf("expected skip on overlap, enqueued=%d", enqueued)
	}
	store.runs[1].Status = "success"
	cs.TriggerNow(5)
	if enqueued != 1 {
		t.Fatalf("expected enqueue after terminal, enqueued=%d", enqueued)
	}
}

func TestCronTimezoneValidation(t *testing.T) {
	t.Parallel()
	cs := NewCronScheduler(&memJobStore{}, newMemRunStore(), &stubEnqueuer{}, nil, zap.NewNop())
	err := cs.Add(model.BuildJob{
		ID: 1, Enabled: true, TriggerCron: true,
		CronExpression: "0 * * * *", CronTimezone: "Not/AZone",
	})
	if err == nil {
		t.Fatal("expected invalid timezone error")
	}
	err = cs.Add(model.BuildJob{
		ID: 2, Enabled: true, TriggerCron: true,
		CronExpression: "0 * * * *", CronTimezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
}

type stubEnqueuer struct {
	fn func(jobID, triggeredBy uint, in EnqueueParams) (*model.BuildRun, error)
}

func (s *stubEnqueuer) EnqueueInternal(jobID, triggeredBy uint, in EnqueueParams) (*model.BuildRun, error) {
	if s.fn != nil {
		return s.fn(jobID, triggeredBy, in)
	}
	return &model.BuildRun{ID: 1, BuildJobID: jobID, Status: "queued"}, nil
}

func TestNewScheduler_MinConcurrent(t *testing.T) {
	t.Parallel()
	s := NewScheduler(0, &Pipeline{}, newMemRunStore(), zap.NewNop())
	if s.maxConcurrent != 1 {
		t.Fatalf("maxConcurrent: got %d want 1", s.maxConcurrent)
	}
}

func TestFailRunDoesNotOverwriteSuccess(t *testing.T) {
	t.Parallel()
	run := &model.BuildRun{ID: 1, Status: "success", Stage: "idle"}
	store := newMemRunStore(run)
	p := NewPipeline(store, &memJobStore{}, &memRepoStore{}, &memServerStore{}, nopSecrets{}, nil, zap.NewNop(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	p.failRun(run, "should ignore")
	got, _ := store.FindByID(1)
	if got.Status != "success" {
		t.Fatalf("status overwritten to %s", got.Status)
	}
}
