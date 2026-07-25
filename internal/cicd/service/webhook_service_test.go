package service_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/cicd/service"
	resourceservice "bedrock/internal/resource/service"
	"gorm.io/gorm"
)

func TestWebhook_SignatureFailRejects(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-sig", RepoURL: "https://example.com/a.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, secret := createWebhookJob(t, jobSvc, repo.ID, "job-sig", "main", true)

	wh := newWebhookSvc(gdb, runSvc)
	body := []byte(`{"ref":"refs/heads/main","after":"abc"}`)
	headers := map[string]string{
		"X-GitHub-Event":      "push",
		"X-Hub-Signature-256": "sha256=deadbeef",
		"X-GitHub-Delivery":   "del-1",
	}
	_, err = wh.Receive(job.ID, secret, headers, body)
	if err == nil || !service.IsUnauthorized(err) {
		t.Fatalf("want unauthorized, got %v", err)
	}
}

func TestWebhook_GenericSecretAndDedup(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-gen", RepoURL: "https://example.com/b.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, secret := createWebhookJob(t, jobSvc, repo.ID, "job1", "main", true)
	_ = job

	wh := newWebhookSvc(gdb, runSvc)
	body := []byte(`{"ref":"refs/heads/main","after":"abc123","message":"hi"}`)
	headers := map[string]string{"X-GitHub-Delivery": "same-del"}

	r1, err := wh.Receive(job.ID, secret, headers, body)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Accepted || r1.Triggered != 1 {
		t.Fatalf("first: %+v", r1)
	}

	r2, err := wh.Receive(job.ID, secret, headers, body)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Duplicate || r2.Triggered != 0 {
		t.Fatalf("dedup: %+v", r2)
	}
}

func TestWebhook_LogsNoSecret(t *testing.T) {
	secret := "super-secret-value-xyz"
	msg := service.RedactSecret("invalid secret super-secret-value-xyz in path", secret)
	if strings.Contains(msg, secret) {
		t.Fatalf("secret leaked: %s", msg)
	}
	if !strings.Contains(msg, "***") {
		t.Fatalf("expected redaction: %s", msg)
	}
}

func TestWebhook_BranchMatching_FixedMissDoesNotEnqueue(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-branch-miss", RepoURL: "https://example.com/miss.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, secret := createWebhookJob(t, jobSvc, repo.ID, "fixed-main", "main", true)

	wh := newWebhookSvc(gdb, runSvc)
	res, err := wh.Receive(job.ID, secret, map[string]string{"X-GitHub-Delivery": "branch-miss-1"},
		[]byte(`{"ref":"refs/heads/develop","after":"abc"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 0 || len(res.RunIDs) != 0 {
		t.Fatalf("fixed-branch miss should not enqueue: %+v", res)
	}
}

func TestWebhook_BranchMatching_JobScopedOnlySelf(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-branch-multi", RepoURL: "https://example.com/multi.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	mainJob, mainSecret := createWebhookJob(t, jobSvc, repo.ID, "job-main", "main", true)
	devJob, devSecret := createWebhookJob(t, jobSvc, repo.ID, "job-develop", "develop", true)

	wh := newWebhookSvc(gdb, runSvc)
	res, err := wh.Receive(mainJob.ID, mainSecret, map[string]string{"X-GitHub-Delivery": "branch-multi-1"},
		[]byte(`{"ref":"refs/heads/main","after":"deadbeef"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 1 || len(res.JobIDs) != 1 || res.JobIDs[0] != mainJob.ID {
		t.Fatalf("main job webhook should only trigger itself: %+v", res)
	}

	resDev, err := wh.Receive(devJob.ID, devSecret, map[string]string{"X-GitHub-Delivery": "branch-multi-2"},
		[]byte(`{"ref":"refs/heads/main","after":"deadbeef"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resDev.Triggered != 0 {
		t.Fatalf("develop job should reject main branch: %+v", resDev)
	}
}

func TestWebhook_BranchMatching_MismatchDoesNotEnqueue(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-branch-mismatch", RepoURL: "https://example.com/mismatch.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	mainJob, secret := createWebhookJob(t, jobSvc, repo.ID, "job-main-only", "main", true)

	wh := newWebhookSvc(gdb, runSvc)
	res, err := wh.Receive(mainJob.ID, secret, map[string]string{"X-GitHub-Delivery": "branch-mismatch-1"},
		[]byte(`{"ref":"refs/heads/feature/x","after":"cafebabe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 0 || len(res.RunIDs) != 0 {
		t.Fatalf("branch mismatch should not enqueue: %+v", res)
	}
}

func TestWebhook_ValidGitHubSignature(t *testing.T) {
	_, repoSvc, _, jobSvc, runSvc, gdb := setupCICD(t)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "wh-ok", RepoURL: "https://example.com/c.git", AuthType: "none",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, secret := createWebhookJob(t, jobSvc, repo.ID, "j", "main", true)

	body := []byte(`{"ref":"refs/heads/main","after":"deadbeef","head_commit":{"id":"deadbeef","message":"m"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	wh := newWebhookSvc(gdb, runSvc)
	res, err := wh.Receive(job.ID, secret, map[string]string{
		"X-GitHub-Event":      "push",
		"X-Hub-Signature-256": sig,
		"X-GitHub-Delivery":   "del-ok",
	}, body)
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 1 {
		t.Fatalf("triggered=%d", res.Triggered)
	}
}

func createWebhookJob(
	t *testing.T,
	jobSvc *service.BuildJobService,
	repoID uint,
	name, branch string,
	triggerWebhook bool,
) (*model.BuildJob, string) {
	t.Helper()
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID:   repoID,
		Name:           name,
		Branch:         branch,
		TriggerWebhook: new(triggerWebhook),
		BuildScript:    "echo ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	revealed, err := jobSvc.GetWithSecret(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revealed.WebhookSecret == "" {
		rotated, err := jobSvc.RotateWebhookSecret(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		return rotated, rotated.WebhookSecret
	}
	return revealed, revealed.WebhookSecret
}

func newWebhookSvc(gdb *gorm.DB, runSvc *service.BuildRunService) *service.WebhookService {
	return service.NewWebhookService(
		repository.NewBuildJobRepository(gdb),
		repository.NewWebhookDeliveryRepository(gdb),
		runSvc,
	)
}
