package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"bedrock/internal/cicd/handler"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacservice "bedrock/internal/rbac/service"
	resourcemodel "bedrock/internal/resource/model"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
)

type liveAuth struct {
	isPAT  bool
	scopes []string
}

func (a *liveAuth) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("is_super_admin", true)
		c.Set("is_pat", a.isPAT)
		if a.isPAT {
			c.Set("pat_scopes", a.scopes)
		}
		c.Next()
	}
}

func TestEnqueueRunPATScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "pat-exec.sqlite"),
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
	if err := migration.Up(t.Context(), gdb, "sqlite"); err != nil {
		t.Fatal(err)
	}

	credRepo := resourcerepo.NewCredentialRepository(gdb)
	repoRepo := resourcerepo.NewRepositoryRepository(gdb)
	jobRepo := repository.NewBuildJobRepository(gdb)
	runRepo := repository.NewBuildRunRepository(gdb)
	scriptJobRepo := repository.NewScriptJobRepository(gdb)
	scriptRunRepo := repository.NewScriptRunRepository(gdb)
	pipelineRepo := repository.NewBuildPipelineRepository(gdb)
	pipelineRunRepo := repository.NewPipelineRunRepository(gdb)

	repoSvc := resourceservice.NewRepositoryService(repoRepo, resourceservice.NewCredentialService(credRepo))
	jobSvc := service.NewBuildJobService(jobRepo, repoRepo, nil)
	runSvc := service.NewBuildRunService(runRepo, jobRepo)
	scriptJobSvc := service.NewScriptJobService(scriptJobRepo, nil)
	scriptRunSvc := service.NewScriptRunService(scriptRunRepo, scriptJobRepo)
	pipelineSvc := service.NewBuildPipelineService(pipelineRepo, jobRepo, scriptJobRepo, nil)
	orch := service.NewPipelineOrchestrator(
		pipelineRepo, pipelineRunRepo, jobRepo, scriptJobRepo, runSvc, scriptRunSvc, nil, zap.NewNop(),
	)
	perm := &rbacservice.PermissionService{}

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "pat-repo", RepoURL: "https://example.com/pat.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "pat-job", BuildScript: "echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptJob, err := scriptJobSvc.Create(1, service.CreateScriptJobInput{
		Name: "pat-script", Script: "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := pipelineSvc.Create(1, service.CreateBuildPipelineInput{
		Name: "pat-pipe",
		GraphJSON: `{"nodes":[
			{"id":"start","type":"start","data":{}},
			{"id":"end","type":"end","data":{}}],"edges":[
			{"id":"e0","source":"start","target":"end"}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	auth := &liveAuth{}
	r := gin.New()
	g := r.Group("")
	handler.NewBuildJobHandler(jobSvc, runSvc, perm).RegisterRoutes(g, auth.middleware())
	handler.NewScriptJobHandler(scriptJobSvc, scriptRunSvc, perm).RegisterRoutes(g, auth.middleware())
	handler.NewBuildPipelineHandler(pipelineSvc, orch, perm).RegisterRoutes(g, auth.middleware())

	cases := []struct {
		name  string
		path  string
		scope string
		wrong string
	}{
		{"build", fmt.Sprintf("/build-jobs/%d/runs", job.ID), resourcemodel.ScopeBuildsRun, resourcemodel.ScopeScriptsRun},
		{"script", fmt.Sprintf("/script-jobs/%d/runs", scriptJob.ID), resourcemodel.ScopeScriptsRun, resourcemodel.ScopeBuildsRun},
		{"pipeline", fmt.Sprintf("/build-pipelines/%d/runs", pipeline.ID), resourcemodel.ScopePipelinesRun, resourcemodel.ScopeBuildsRun},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth.isPAT = false
			auth.scopes = nil
			if got := postRuns(t, r, tc.path); got != http.StatusAccepted {
				t.Fatalf("JWT execute = %d, want 202", got)
			}

			auth.isPAT = true
			auth.scopes = nil
			if got := postRuns(t, r, tc.path); got != http.StatusForbidden {
				t.Fatalf("PAT without scope = %d, want 403", got)
			}
			auth.scopes = []string{tc.wrong}
			if got := postRuns(t, r, tc.path); got != http.StatusForbidden {
				t.Fatalf("PAT wrong scope = %d, want 403", got)
			}
			auth.scopes = []string{tc.scope}
			if got := postRuns(t, r, tc.path); got != http.StatusAccepted {
				t.Fatalf("PAT %s = %d, want 202", tc.scope, got)
			}
		})
	}
}

func postRuns(t *testing.T, r http.Handler, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}
