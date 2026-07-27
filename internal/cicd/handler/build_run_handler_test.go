package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"bedrock/internal/cicd/handler"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacservice "bedrock/internal/rbac/service"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
)

func TestBuildRunHandler_ArtifactDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "handler.sqlite"),
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

	repoSvc := resourceservice.NewRepositoryService(repoRepo, resourceservice.NewCredentialService(credRepo))
	jobSvc := service.NewBuildJobService(jobRepo, repoRepo)
	runSvc := service.NewBuildRunService(runRepo, jobRepo)

	repo, err := repoSvc.Create(1, resourceservice.CreateRepositoryInput{
		Name: "h-art", RepoURL: "https://example.com/h.git",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobSvc.Create(1, service.CreateBuildJobInput{
		RepositoryID: repo.ID, Name: "h-job", BuildScript: "echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runSvc.Enqueue(job.ID, 1, "all", service.EnqueueRunInput{TriggerType: "manual"})
	if err != nil {
		t.Fatal(err)
	}

	artPath := filepath.Join(t.TempDir(), "build-001.tar.gz")
	if err := os.WriteFile(artPath, []byte("artifact-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Table("build_runs").Where("id = ?", run.ID).Updates(map[string]interface{}{
		"status": "success", "stage": "idle", "artifact_path": artPath,
	}).Error; err != nil {
		t.Fatal(err)
	}

	h := handler.NewBuildRunHandler(runSvc, &rbacservice.PermissionService{})
	r := gin.New()
	r.GET("/build-runs/:id/artifact", func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("is_super_admin", true)
		c.Next()
	}, h.Artifact)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/build-runs/%d/artifact", run.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != "artifact-bytes" {
		t.Fatalf("body=%q", w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "build-001.tar.gz") {
		t.Fatalf("Content-Disposition=%q", cd)
	}
}
