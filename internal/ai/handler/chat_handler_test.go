package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aihandler "bedrock/internal/ai/handler"
	"bedrock/internal/ai/model"
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

func setupChatHandlerTestRouter(t *testing.T) (*gin.Engine, *aiservice.ProviderService, *aiservice.ChatService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "chat_handler_test.sqlite")
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
	chatRepository := airepo.NewChatRepository(gdb)
	chatSvc := aiservice.NewChatService(chatRepository)
	chatProxy := aiservice.NewChatProxy(providerSvc, chatSvc)

	agents := aiservice.NewAgentService(aiRepository, nil, nil, nil, nil, t.TempDir(), t.TempDir(), t.TempDir())
	skills := aiservice.NewSkillService(aiRepository, nil, t.TempDir())

	chatHandler := aihandler.NewChatHandler(chatSvc, chatProxy, providerSvc)
	h := aihandler.NewHandler(agents, skills, permSvc, providerSvc, chatHandler)

	r := gin.New()
	api := r.Group("/api/v1")

	// Auth middleware using X-Test-User-ID header
	authMW := func(c *gin.Context) {
		uid := c.GetHeader("X-Test-User-ID")
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "未登录"})
			return
		}
		var parsed uint
		for _, ch := range uid {
			parsed = parsed*10 + uint(ch-'0')
		}
		c.Set("user_id", parsed)
		c.Next()
	}

	h.RegisterRoutes(api, authMW)
	return r, providerSvc, chatSvc
}

func TestChatHandler_HTTPFlowAndSecurity(t *testing.T) {
	r, providerSvc, _ := setupChatHandlerTestRouter(t)

	// 1. Unauthorized request should return 401
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/sessions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated request, got %d", w.Code)
		}
	}

	// 2. User 1 creates session
	var sessionID uint
	{
		body, _ := json.Marshal(map[string]string{
			"title":    "我的会话 1",
			"model_id": "gpt-4o",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/sessions", bytes.NewReader(body))
		req.Header.Set("X-Test-User-ID", "1")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Code int               `json:"code"`
			Data model.ChatSession `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		sessionID = resp.Data.ID
		if sessionID == 0 || resp.Data.Title != "我的会话 1" {
			t.Fatalf("unexpected session response: %+v", resp.Data)
		}
	}

	// 3. User 1 lists sessions -> sees 1 session
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/sessions", nil)
		req.Header.Set("X-Test-User-ID", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Data struct {
				Items []model.ChatSession `json:"items"`
				Total int64               `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Data.Total != 1 || len(resp.Data.Items) != 1 {
			t.Fatalf("expected 1 session, got %d", resp.Data.Total)
		}
	}

	// 4. User 2 lists sessions -> sees 0 sessions (isolation)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/sessions", nil)
		req.Header.Set("X-Test-User-ID", "2")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Data struct {
				Items []model.ChatSession `json:"items"`
				Total int64               `json:"total"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Data.Total != 0 {
			t.Fatalf("expected 0 sessions for user 2, got %d", resp.Data.Total)
		}
	}

	// 5. User 2 tries to update User 1's session -> 404
	{
		body, _ := json.Marshal(map[string]string{"title": "改名测试"})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/ai/chat/sessions/"+strings.TrimSpace(jsonNumber(sessionID)), bytes.NewReader(body))
		req.Header.Set("X-Test-User-ID", "2")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-user update, got %d", w.Code)
		}
	}

	// 6. User 1 adds message and gets message history
	{
		msgBody, _ := json.Marshal(map[string]string{
			"role":              "user",
			"content":           "今天天气怎么样？",
			"reasoning_content": "",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/sessions/"+jsonNumber(sessionID)+"/messages", bytes.NewReader(msgBody))
		req.Header.Set("X-Test-User-ID", "1")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created message, got %d", w.Code)
		}

		// User 2 tries to read User 1's messages -> 404
		reqGetB := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/sessions/"+jsonNumber(sessionID)+"/messages", nil)
		reqGetB.Header.Set("X-Test-User-ID", "2")
		wGetB := httptest.NewRecorder()
		r.ServeHTTP(wGetB, reqGetB)
		if wGetB.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-user get messages, got %d", wGetB.Code)
		}

		// User 1 reads messages -> 200
		reqGetA := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/sessions/"+jsonNumber(sessionID)+"/messages", nil)
		reqGetA.Header.Set("X-Test-User-ID", "1")
		wGetA := httptest.NewRecorder()
		r.ServeHTTP(wGetA, reqGetA)
		if wGetA.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", wGetA.Code)
		}
		var msgsResp struct {
			Data []model.ChatMessage `json:"data"`
		}
		_ = json.Unmarshal(wGetA.Body.Bytes(), &msgsResp)
		if len(msgsResp.Data) != 1 || msgsResp.Data[0].Content != "今天天气怎么样？" {
			t.Fatalf("unexpected messages: %+v", msgsResp.Data)
		}
	}

	// 7. Test Available Models endpoint
	{
		// Create a provider and model
		p, _ := providerSvc.CreateProvider(1, model.ProviderInput{
			Name:   "TestProvider",
			APIURL: "https://api.example.com",
		})
		_, _ = providerSvc.CreateModel(p.ID, model.ModelInput{
			Name:      "ModelAlpha",
			ModelID:   "model-alpha",
			SortOrder: intPtr(10),
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/chat/models", nil)
		req.Header.Set("X-Test-User-ID", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 from chat models, got %d", w.Code)
		}
		var modelsResp struct {
			Data []model.AiModel `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &modelsResp)
		if len(modelsResp.Data) == 0 || modelsResp.Data[0].ModelID != "model-alpha" {
			t.Fatalf("expected model-alpha in available models, got: %+v", modelsResp.Data)
		}
	}

	// 8. Delete session cascade
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/ai/chat/sessions/"+jsonNumber(sessionID), nil)
		req.Header.Set("X-Test-User-ID", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on delete, got %d", w.Code)
		}
	}
}

func TestChatHandler_CompletionsProxyEndpoint(t *testing.T) {
	r, providerSvc, _ := setupChatHandlerTestRouter(t)

	// Mock upstream server
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"流式输出片段\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstreamServer.Close()

	p, err := providerSvc.CreateProvider(1, model.ProviderInput{
		Name:   "StreamProvider",
		APIURL: upstreamServer.URL,
		APIKey: "sk-mock-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = providerSvc.CreateModel(p.ID, model.ModelInput{
		Name:    "StreamModel",
		ModelID: "stream-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Send POST /api/v1/ai/chat/completions
	body, _ := json.Marshal(map[string]any{
		"model": "stream-model",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/completions", bytes.NewReader(body))
	req.Header.Set("X-Test-User-ID", "100")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "流式输出片段") {
		t.Fatalf("expected response to contain streamed content, got %s", w.Body.String())
	}
}

func jsonNumber(n uint) string {
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if len(b) == 0 {
		return "0"
	}
	return string(b)
}

func intPtr(v int) *int {
	return &v
}
