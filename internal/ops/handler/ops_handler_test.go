package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"bedrock/internal/ops/model"
	"bedrock/internal/ops/repository"
	opsservice "bedrock/internal/ops/service"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestGetJobRedactsCommandAndSourceSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newTestOpsRepository(t)
	svc := opsservice.NewDevEnvironmentService(repo)
	svc.SetInlineExec(true)

	env, err := svc.CreateCustom(opsservice.DevEnvironmentInput{
		Name:       "redaction-test",
		Executable: "sh",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := repo.ListSources(env.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range sources {
		sources[i].Enabled = false
		if err := repo.UpdateSource(&sources[i]); err != nil {
			t.Fatal(err)
		}
	}
	const templateSecret = "template-super-secret"
	const sourceSecret = "source-super-secret"
	const cliTokenSecret = "cli-token-secret"
	const cliPasswordSecret = "cli-password-secret"
	const cliAPIKeySecret = "cli-api-key-secret"
	const cliSecretSecret = "cli-secret-secret"
	const cliAccessTokenSecret = "cli-access-token-secret"
	const cliAuthSecret = "cli-auth-secret"
	const cliCredentialSecret = "cli-credential-secret"
	const cliShortTokenSecret = "cli-short-token-secret"
	const headerTokenSecret = "header-token-secret"
	const logTokenSecret = "log-token-secret"
	source := &model.DevEnvInstallSource{
		EnvironmentID: env.ID,
		Name:          "redaction-source",
		BaseURL:       "https://operator:source-password@packages.example/simple?api_key=" + sourceSecret,
		Priority:      1,
		Enabled:       true,
	}
	if err := repo.CreateSource(source); err != nil {
		t.Fatal(err)
	}
	env.InstallScript = "TOKEN=" + templateSecret + `; : --token ` + cliTokenSecret +
		` --password ` + cliPasswordSecret +
		` --api-key ` + cliAPIKeySecret +
		` --secret ` + cliSecretSecret +
		` --access-token ` + cliAccessTokenSecret +
		` --auth ` + cliAuthSecret +
		` --credential ` + cliCredentialSecret +
		` -t ` + cliShortTokenSecret +
		` --header "Authorization: Bearer ` + headerTokenSecret +
		`"; printf '%s\n' '--token ` + logTokenSecret + `' '{{source_url}}'`
	if err := repo.UpdateEnvironment(env); err != nil {
		t.Fatal(err)
	}
	job, err := svc.Enqueue(env.ID, "install", opsservice.JobInput{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	awaitCompletedJob(t, svc, env.ID, job.ID)

	persisted, err := repo.FindJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{persisted.CommandSnapshot, persisted.LogText, persisted.ErrorMessage} {
		for _, secret := range []string{
			templateSecret,
			sourceSecret,
			cliTokenSecret,
			cliPasswordSecret,
			cliAPIKeySecret,
			cliSecretSecret,
			cliAccessTokenSecret,
			cliAuthSecret,
			cliCredentialSecret,
			cliShortTokenSecret,
			headerTokenSecret,
			logTokenSecret,
			"source-password",
			"packages.example",
		} {
			if strings.Contains(value, secret) {
				t.Fatalf("persisted job text leaks %q: %q", secret, value)
			}
		}
	}

	router := gin.New()
	router.GET("/dev-environments/:id/jobs/:jobId", NewOpsHandler(nil, svc, nil).GetJob)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/dev-environments/"+strconv.FormatUint(uint64(env.ID), 10)+"/jobs/"+strconv.FormatUint(uint64(job.ID), 10),
		nil,
	)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET job status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, secret := range []string{
		templateSecret,
		sourceSecret,
		cliTokenSecret,
		cliPasswordSecret,
		cliAPIKeySecret,
		cliSecretSecret,
		cliAccessTokenSecret,
		cliAuthSecret,
		cliCredentialSecret,
		cliShortTokenSecret,
		headerTokenSecret,
		logTokenSecret,
		"source-password",
		"packages.example",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("API response leaks %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("API response did not show redaction: %s", body)
	}
}

func awaitCompletedJob(t *testing.T, svc *opsservice.DevEnvironmentService, envID, id uint) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.GetJob(envID, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != model.JobQueued && job.Status != model.JobRunning {
			if job.Status != model.JobSuccess {
				t.Fatalf("job status = %s, logs:\n%s", job.Status, job.LogText)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not complete", id)
}

func newTestOpsRepository(t *testing.T) *repository.OpsRepository {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "ops-handler.sqlite"),
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
	return repository.NewOpsRepository(gdb)
}
