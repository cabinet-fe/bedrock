package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aihandler "bedrock/internal/ai/handler"
	airepo "bedrock/internal/ai/repository"
	aiservice "bedrock/internal/ai/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "ai_handler_test.sqlite")
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: dbPath})
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
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("seed rbac resources: %v", err)
	}

	permSvc := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)

	aiRepository := airepo.NewAIRepository(gdb)
	providerRepository := airepo.NewProviderRepository(gdb)
	providerSvc := aiservice.NewProviderService(providerRepository)

	// dummy agents and skills services
	agents := aiservice.NewAgentService(aiRepository, nil, nil, nil, nil, t.TempDir(), t.TempDir(), t.TempDir())
	skills := aiservice.NewSkillService(aiRepository, nil, t.TempDir())

	h := aihandler.NewHandler(agents, skills, permSvc, providerSvc)

	r := gin.New()
	api := r.Group("/api/v1")

	// Mock auth middleware that reads test headers
	authMW := func(c *gin.Context) {
		uid := c.GetHeader("X-Test-User-ID")
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
			return
		}
		var userID uint
		if _, err := fmt.Sscanf(uid, "%d", &userID); err != nil || userID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
			return
		}
		c.Set("user_id", userID)
		if c.GetHeader("X-Test-Is-Admin") == "true" {
			c.Set("is_super_admin", true)
		} else {
			c.Set("is_super_admin", false)
		}
		c.Next()
	}

	h.RegisterRoutes(api, authMW)
	return r, gdb
}

func TestProviderHandler_ForbiddenForNonAdmin(t *testing.T) {
	router, _ := setupTestRouter(t)

	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil)
	reqList.Header.Set("X-Test-User-ID", "2")
	reqList.Header.Set("X-Test-Is-Admin", "false")
	wList := httptest.NewRecorder()
	router.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin on list providers, got %d", wList.Code)
	}

	createBody := bytes.NewBufferString(`{"name":"OpenAI","api_url":"https://api.openai.com/v1"}`)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers", createBody)
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.Header.Set("X-Test-User-ID", "2")
	reqCreate.Header.Set("X-Test-Is-Admin", "false")
	wCreate := httptest.NewRecorder()
	router.ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin on create provider, got %d", wCreate.Code)
	}

	reqModels := httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers/1/models", nil)
	reqModels.Header.Set("X-Test-User-ID", "2")
	reqModels.Header.Set("X-Test-Is-Admin", "false")
	wModels := httptest.NewRecorder()
	router.ServeHTTP(wModels, reqModels)
	if wModels.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin on list models, got %d", wModels.Code)
	}
}

func TestProviderHandler_AdminCRUDAndAPIKeyMasking(t *testing.T) {
	router, _ := setupTestRouter(t)

	// 1. Create Provider
	createBody := `{"name":"OpenAI Official","api_url":"https://api.openai.com/v1","api_key":"sk-secret-123456","notes":"main provider"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider status = %d, body: %s", w.Code, w.Body.String())
	}
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "sk-secret-123456") {
		t.Fatalf("response body should NEVER contain plain apiKey: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"has_api_key":true`) {
		t.Fatalf("expected has_api_key:true in response: %s", bodyStr)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	providerID := resp.Data.ID

	// 2. List Providers
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list providers status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "sk-secret-123456") {
		t.Fatalf("list response should not contain plaintext apiKey")
	}

	// 3. Get Provider
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/providers/%d", providerID), nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get provider status = %d", w.Code)
	}

	// 4. Update Provider
	updateBody := `{"name":"OpenAI Production","notes":"updated notes"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/ai/providers/%d", providerID), bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update provider status = %d", w.Code)
	}

	// 5. Create Model under Provider
	modelBody := `{
		"name": "GPT-4o",
		"model_id": "gpt-4o",
		"sort_order": 10,
		"reasoning_efforts": [
			{"value": "low", "label": "低"},
			{"value": "high", "label": "高"}
		],
		"default_params": {"temperature": 0.7},
		"notes": "flagship model"
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ai/providers/%d/models", providerID), bytes.NewBufferString(modelBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create model status = %d, body: %s", w.Code, w.Body.String())
	}
	var modelResp struct {
		Code int `json:"code"`
		Data struct {
			ID      uint   `json:"id"`
			ModelID string `json:"model_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &modelResp); err != nil {
		t.Fatalf("unmarshal create model response: %v", err)
	}
	modelID := modelResp.Data.ID

	// 6. List Models under Provider
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/providers/%d/models", providerID), nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list models status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "GPT-4o") {
		t.Fatalf("list models should include GPT-4o: %s", w.Body.String())
	}

	// 7. Get Model
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/ai/providers/%d/models/%d", providerID, modelID), nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get model status = %d", w.Code)
	}

	// 8. Update Model
	modelUpdateBody := `{"name":"GPT-4o-2024","sort_order":5}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/ai/providers/%d/models/%d", providerID, modelID), bytes.NewBufferString(modelUpdateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update model status = %d", w.Code)
	}

	// 9. Delete Model
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/ai/providers/%d/models/%d", providerID, modelID), nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete model status = %d", w.Code)
	}

	// 10. Delete Provider
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/ai/providers/%d", providerID), nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Is-Admin", "true")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete provider status = %d", w.Code)
	}
}
