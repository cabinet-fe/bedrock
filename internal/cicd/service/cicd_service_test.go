package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bedrock/internal/cicd/repository"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"

	"gorm.io/gorm"
)

type stubGit struct {
	branches []string
	err      error
}

func (s stubGit) ListBranches(repoURL, authType, username, password string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.branches, nil
}

func setupCICD(t *testing.T) (
	*resourceservice.CredentialService,
	*resourceservice.RepositoryService,
	*resourceservice.ServerService,
	*service.BuildJobService,
	*service.BuildRunService,
	*gorm.DB,
) {
	t.Helper()
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "cicd.sqlite"),
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
	serverRepo := resourcerepo.NewServerRepository(gdb)
	jobRepo := repository.NewBuildJobRepository(gdb)
	runRepo := repository.NewBuildRunRepository(gdb)

	credSvc := resourceservice.NewCredentialService(credRepo)
	repoSvc := resourceservice.NewRepositoryService(repoRepo, credSvc, stubGit{branches: []string{"main", "develop"}})
	serverSvc := resourceservice.NewServerService(serverRepo, credSvc)
	jobSvc := service.NewBuildJobService(jobRepo, repoRepo)
	runSvc := service.NewBuildRunService(runRepo, jobRepo)
	return credSvc, repoSvc, serverSvc, jobSvc, runSvc, gdb
}

func TestCredential_CRUD_neverReturnsPlaintext(t *testing.T) {
	credSvc, _, _, _, _, _ := setupCICD(t)

	created, err := credSvc.Create(1, resourceservice.CreateCredentialInput{
		Name:   "gh-token",
		Type:   "token",
		Secret: "super-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "super-secret-token") {
		t.Fatalf("plaintext leaked in JSON: %s", raw)
	}
	if !created.HasSecret {
		t.Fatal("expected has_secret=true")
	}

	got, err := credSvc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(got)
	if strings.Contains(string(raw), "super-secret-token") {
		t.Fatalf("plaintext leaked on get: %s", raw)
	}

	updated, err := credSvc.Update(created.ID, resourceservice.UpdateCredentialInput{
		Description: new("desc"),
		Secret:      new(""), // keep
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(updated)
	if strings.Contains(string(raw), "super-secret-token") {
		t.Fatalf("plaintext leaked on update: %s", raw)
	}

	_, secret, _, err := credSvc.GetDecrypted(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "super-secret-token" {
		t.Fatalf("decrypt got %q", secret)
	}
}

func TestCredential_DeleteProtection(t *testing.T) {
	credSvc, repoSvc, _, _, _, _ := setupCICD(t)

	cred, err := credSvc.Create(1, resourceservice.CreateCredentialInput{
		Name: "repo-cred", Type: "password", Username: "u", Secret: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := cred.ID
	_, err = repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r1", RepoURL: "https://example.com/a.git", AuthType: "credential", CredentialID: &cid,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	err = credSvc.Delete(cid)
	if err == nil || !resourceservice.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestRepository_CRUD_and_deleteProtection(t *testing.T) {
	_, repoSvc, _, jobSvc, _, _ := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "demo", RepoURL: "https://example.com/demo.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	synced, err := repoSvc.SyncBranches(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(synced.Branches) != 2 {
		t.Fatalf("branches=%v", synced.Branches)
	}

	_, err = jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID,
		Name:         "job-a",
		BuildScript:  "echo hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = repoSvc.Delete(repo.ID)
	if err == nil || !resourceservice.IsConflict(err) {
		t.Fatalf("expected conflict when jobs reference repo, got %v", err)
	}
}

func TestRepository_credentialsUseEnforcedOnBind(t *testing.T) {
	credSvc, repoSvc, _, _, _, _ := setupCICD(t)
	cred, err := credSvc.Create(1, resourceservice.CreateCredentialInput{
		Name: "c1", Type: "token", Secret: "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := cred.ID
	_, err = repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r", RepoURL: "https://example.com/x.git", AuthType: "credential", CredentialID: &cid,
	}, false)
	if err == nil || !resourceservice.IsForbidden(err) {
		t.Fatalf("expected forbidden without credentials:use, got %v", err)
	}
	_, err = repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r", RepoURL: "https://example.com/x.git", AuthType: "credential", CredentialID: &cid,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
}

func TestServer_CRUD_and_deleteProtection(t *testing.T) {
	_, repoSvc, serverSvc, jobSvc, _, _ := setupCICD(t)

	srv, err := serverSvc.Create(1, resourceservice.CreateServerInput{
		Name: "s1", Host: "10.0.0.1", Port: 22, AuthType: "password", Username: "root",
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r2", RepoURL: "https://example.com/y.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	sid := srv.ID
	_, err = jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID,
		Name:         "job-deploy",
		DeployTargets: []service.DeployTargetInput{
			{ServerID: &sid, RemotePath: "/var/www", Method: "rsync", SortOrder: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = serverSvc.Delete(srv.ID)
	if err == nil || !resourceservice.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestServer_credentialsUseOnBind(t *testing.T) {
	credSvc, _, serverSvc, _, _, _ := setupCICD(t)
	cred, err := credSvc.Create(1, resourceservice.CreateCredentialInput{
		Name: "ssh", Type: "password", Username: "root", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := cred.ID
	_, err = serverSvc.Create(1, resourceservice.CreateServerInput{
		Name: "s2", Host: "10.0.0.2", AuthType: "password", CredentialID: &cid,
	}, false)
	if err == nil || !resourceservice.IsForbidden(err) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestBuildJob_and_BuildRun_enqueue(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, _ := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r3", RepoURL: "https://example.com/z.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID,
		Name:         "build",
		EnvVarNames:  []string{"FOO", "BAR"},
		BuildScript:  "make",
		DeployTargets: []service.DeployTargetInput{
			{Method: "local", RemotePath: "/tmp/out", SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(job.DeployTargets) != 1 {
		t.Fatalf("deploy targets=%d", len(job.DeployTargets))
	}
	if len(job.EnvVarNames) != 2 {
		t.Fatalf("env names=%v", job.EnvVarNames)
	}

	run, err := runSvc.Enqueue(job.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.Stage != "pending" || run.DistributionSummary != "none" {
		t.Fatalf("run=%+v", run)
	}
	if run.SnapshotJSON == "" {
		t.Fatal("expected snapshot")
	}
	got, err := runSvc.Get(run.ID, 1, "all")
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildNumber != 1 {
		t.Fatalf("build_number=%d", got.BuildNumber)
	}
}

func TestBuildJob_TwoJobsSameRepo_EachEnqueue(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, _ := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r-multi-job", RepoURL: "https://example.com/multi-job.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	jobA, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "job-a", BuildScript: "echo a",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "job-b", BuildScript: "echo b",
	})
	if err != nil {
		t.Fatal(err)
	}

	runA, err := runSvc.Enqueue(jobA.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	runB, err := runSvc.Enqueue(jobB.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if runA.ID == runB.ID {
		t.Fatal("expected distinct runs")
	}
	if runA.BuildJobID != jobA.ID || runB.BuildJobID != jobB.ID {
		t.Fatalf("runA.job=%d runB.job=%d", runA.BuildJobID, runB.BuildJobID)
	}
	if runA.Status != "queued" || runB.Status != "queued" {
		t.Fatalf("statuses %s/%s", runA.Status, runB.Status)
	}
}

func TestBuildRun_RetryAfterInterrupted(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r-retry", RepoURL: "https://example.com/retry.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "retry-job", BuildScript: "echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	prev, err := runSvc.Enqueue(job.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(prev).Updates(map[string]interface{}{
		"status": "interrupted",
		"stage":  "idle",
	}).Error; err != nil {
		t.Fatal(err)
	}

	next, err := runSvc.Retry(prev.ID, 1, "all")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == prev.ID {
		t.Fatal("retry must create a new BuildRun")
	}
	if next.Status != "queued" || next.TriggerType != "retry" {
		t.Fatalf("next=%+v", next)
	}
	if next.BuildJobID != job.ID || next.Branch != "main" {
		t.Fatalf("next job/branch mismatch: %+v", next)
	}
	if next.BuildNumber != 2 {
		t.Fatalf("build_number=%d want 2", next.BuildNumber)
	}
}

func TestBuildRun_ArtifactPathDownloadable(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "r-art", RepoURL: "https://example.com/art.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "art-job", BuildScript: "echo", ArtifactFormat: "gzip",
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runSvc.Enqueue(job.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}

	artDir := t.TempDir()
	artPath := filepath.Join(artDir, "build-001.tar.gz")
	if err := os.WriteFile(artPath, []byte("fake-tar-gz-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(run).Updates(map[string]interface{}{
		"status":        "success",
		"stage":         "idle",
		"artifact_path": artPath,
	}).Error; err != nil {
		t.Fatal(err)
	}

	path, filename, err := runSvc.ArtifactPath(run.ID, 1, "all")
	if err != nil {
		t.Fatal(err)
	}
	if path != artPath || filename != "build-001.tar.gz" {
		t.Fatalf("path=%q filename=%q", path, filename)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "fake-tar-gz-bytes" {
		t.Fatalf("download content: %s %v", got, err)
	}
}

func TestBuildJob_DataScopeFiltersListAndMutate(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, _ := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "scope-repo", RepoURL: "https://example.com/scope.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "mine", BuildScript: "echo 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := jobSvc.Create(2, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "theirs", BuildScript: "echo 2",
	})
	if err != nil {
		t.Fatal(err)
	}

	items, total, err := jobSvc.List(pkg.ListQuery{Page: 1, PageSize: 20}, nil, "", 1, "self")
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != mine.ID {
		t.Fatalf("self list = %#v total=%d err=%v", items, total, err)
	}
	allItems, total, err := jobSvc.List(pkg.ListQuery{Page: 1, PageSize: 20}, nil, "", 1, "all")
	if err != nil || total != 2 || len(allItems) != 2 {
		t.Fatalf("all list = %#v total=%d err=%v", allItems, total, err)
	}
	if _, err := jobSvc.Get(theirs.ID, 1, "self"); !service.IsForbidden(err) {
		t.Fatalf("self get other = %v, want forbidden", err)
	}
	if _, err := jobSvc.Update(theirs.ID, 1, "self", service.UpdateBuildJobInput{}); !service.IsForbidden(err) {
		t.Fatalf("self update other = %v, want forbidden", err)
	}
	if err := jobSvc.Delete(theirs.ID, 1, "self"); !service.IsForbidden(err) {
		t.Fatalf("self delete other = %v, want forbidden", err)
	}

	if _, err := runSvc.Enqueue(theirs.ID, 1, "self", service.EnqueueRunInput{TriggerType: "manual"}); !service.IsForbidden(err) {
		t.Fatalf("self enqueue other = %v, want forbidden", err)
	}
	run, err := runSvc.Enqueue(theirs.ID, 2, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	runs, total, err := runSvc.List(pkg.ListQuery{Page: 1, PageSize: 20}, nil, "", 1, "self")
	if err != nil || total != 0 || len(runs) != 0 {
		t.Fatalf("self run list must hide other job runs = %#v total=%d err=%v", runs, total, err)
	}
	if _, err := runSvc.Get(run.ID, 1, "self"); !service.IsForbidden(err) {
		t.Fatalf("self get other run = %v, want forbidden", err)
	}
	if _, err := runSvc.Get(run.ID, 1, "all"); err != nil {
		t.Fatalf("all get other run: %v", err)
	}
}

func TestBuildJob_PublicReadableBySelfScope(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, _ := setupCICD(t)
	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "pub-repo", RepoURL: "https://example.com/pub.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	pub := true
	job, err := jobSvc.Create(2, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "public-job", BuildScript: "echo 1", IsPublic: &pub,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := jobSvc.List(pkg.ListQuery{Page: 1, PageSize: 20}, nil, "", 1, "self")
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != job.ID {
		t.Fatalf("self list public = %#v total=%d err=%v", items, total, err)
	}
	if _, err := jobSvc.Get(job.ID, 1, "self"); err != nil {
		t.Fatalf("self get public: %v", err)
	}
	if _, err := jobSvc.Update(job.ID, 1, "self", service.UpdateBuildJobInput{}); !service.IsForbidden(err) {
		t.Fatalf("public must not grant write, got %v", err)
	}
	if _, err := runSvc.Enqueue(job.ID, 1, "self", service.EnqueueRunInput{TriggerType: "manual"}); !service.IsForbidden(err) {
		t.Fatalf("public must not grant execute, got %v", err)
	}
	run, err := runSvc.Enqueue(job.ID, 2, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runSvc.Cancel(run.ID, 1, "self"); !service.IsForbidden(err) {
		t.Fatalf("public must not grant cancel, got %v", err)
	}
	if _, err := runSvc.Retry(run.ID, 1, "self"); !service.IsForbidden(err) {
		t.Fatalf("public must not grant retry, got %v", err)
	}
	if _, err := runSvc.Redeploy(run.ID, 1, "self", service.RedeployInput{}); !service.IsForbidden(err) {
		t.Fatalf("public must not grant redeploy, got %v", err)
	}
}
