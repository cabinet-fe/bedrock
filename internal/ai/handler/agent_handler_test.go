package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aihandler "bedrock/internal/ai/handler"
	airepo "bedrock/internal/ai/repository"
	aiservice "bedrock/internal/ai/service"
	"bedrock/internal/harness/harnesstest"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

// setupAgentHandlerRouter builds the ai handler without a harness backend
// (harness.enabled=false flavor) unless wireHarness is set.
func setupAgentHandlerRouter(t *testing.T, wireHarness bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "agent_handler_test.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration up: %v", err)
	}
	permSvc := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)
	aiRepo := airepo.NewAIRepository(gdb)
	agents := aiservice.NewAgentService(aiRepo, nil, nil, nil, t.TempDir(), t.TempDir(), t.TempDir())
	skills := aiservice.NewSkillService(aiRepo, nil, t.TempDir())
	h := aihandler.NewHandler(agents, skills, permSvc, nil)
	if wireHarness {
		h.SetHarnessCatalog(harnesstest.New())
	}

	r := gin.New()
	api := r.Group("/api/v1")
	authMW := func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("is_super_admin", true)
		c.Next()
	}
	h.RegisterRoutes(api, authMW)
	return r
}

// harness.enabled=false: run execution endpoints answer 503 while agents
// CRUD stays available.
func TestAgentEndpointsDisabledWithoutHarness(t *testing.T) {
	r := setupAgentHandlerRouter(t, false)

	// Agent CRUD unaffected.
	create := httptest.NewRequest(http.MethodPost, "/api/v1/ai/agents", strings.NewReader(`{"name":"a"}`))
	create.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Run execution answers 503 (no CLI fallback).
	run := httptest.NewRequest(http.MethodPost, "/api/v1/ai/agents/1/runs", strings.NewReader(`{}`))
	run.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, run)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("manual run status=%d want 503 body=%s", rec.Code, rec.Body.String())
	}

	apiRun := httptest.NewRequest(http.MethodPost, "/api/v1/ai/agents/1/api-runs", strings.NewReader(`{}`))
	apiRun.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, apiRun)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("api run status=%d want 503 body=%s", rec.Code, rec.Body.String())
	}

	// Catalog passthrough answers 503 too.
	for _, path := range []string{"/api/v1/ai/models", "/api/v1/ai/agents-defs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status=%d want 503", path, rec.Code)
		}
	}
}

// With the harness catalog wired, GET /ai/models passes the catalog through.
func TestHarnessModelsPassthrough(t *testing.T) {
	r := setupAgentHandlerRouter(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("models status=%d body=%s", rec.Code, rec.Body.String())
	}
}
