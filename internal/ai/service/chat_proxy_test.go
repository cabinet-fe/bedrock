package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
)

func TestChatProxy_CompletionsStreamAndAuthInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("ef", 32)); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "proxy_test.sqlite")
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

	providerRepo := repository.NewProviderRepository(gdb)
	providerSvc := service.NewProviderService(providerRepo)
	chatRepo := repository.NewChatRepository(gdb)
	chatSvc := service.NewChatService(chatRepo)
	chatProxy := service.NewChatProxy(providerSvc, chatSvc)

	// Mock upstream OpenAI-compatible server
	var receivedAuthHeader string
	var receivedPayload map[string]any
	var receivedPath string

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		receivedPath = r.URL.Path

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)

		// Return SSE chunks
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		chunk1 := `data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"你好！","reasoning_content":"推理：分析问候语"}}]}` + "\n\n"
		_, _ = w.Write([]byte(chunk1))
		flusher.Flush()

		chunk2 := `data: {"id":"chatcmpl-2","choices":[{"delta":{"content":"很高兴为你服务。"},"finish_reason":"stop"}]}` + "\n\n"
		_, _ = w.Write([]byte(chunk2))
		flusher.Flush()

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer upstreamServer.Close()

	// 1. Create provider with encrypted API key and upstream URL
	secretKey := "sk-super-secret-key-999"
	p, err := providerSvc.CreateProvider(1, model.ProviderInput{
		Name:   "MockProvider",
		APIURL: upstreamServer.URL,
		APIKey: secretKey,
	})
	if err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	// 2. Create model with default parameters
	m, err := providerSvc.CreateModel(p.ID, model.ModelInput{
		Name:    "GPT-4o Mock",
		ModelID: "gpt-4o-mock",
		DefaultParams: map[string]any{
			"temperature": 0.7,
			"max_tokens":  2000,
		},
		ReasoningEfforts: []model.ReasoningEffortOption{
			{Value: "low", Label: "低"},
			{Value: "medium", Label: "中"},
			{Value: "high", Label: "高"},
		},
	})
	if err != nil {
		t.Fatalf("create model failed: %v", err)
	}
	if m.ID == 0 {
		t.Fatal("expected non-zero model ID")
	}

	// 3. Create a session to receive exchange
	userID := uint(501)
	sess, err := chatSvc.CreateSession(userID, model.ChatSessionInput{
		Title:   "新对话",
		ModelID: m.ModelID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. Construct proxy request
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/ai/chat/completions", nil)
	c.Request = httpReq

	reqPayload := model.ChatCompletionRequest{
		Model: "gpt-4o-mock",
		Messages: []model.ChatCompletionMessage{
			{Role: model.RoleUser, Content: "你好，请回复"},
		},
		ReasoningEffort: "high",
		SessionID:       &sess.ID,
		Extra: map[string]any{
			"temperature": 0.2, // override default param
			"custom_arg":  "hello_custom",
		},
	}

	err = chatProxy.ProxyCompletions(c, userID, reqPayload)
	if err != nil {
		t.Fatalf("proxy completions error: %v", err)
	}

	// 5. Verify upstream request details
	if receivedAuthHeader != "Bearer "+secretKey {
		t.Fatalf("expected Authorization 'Bearer %s', got '%s'", secretKey, receivedAuthHeader)
	}
	if receivedPath != "/chat/completions" {
		t.Fatalf("expected path '/chat/completions', got '%s'", receivedPath)
	}

	// Verify reasoning_effort and parameters passthrough
	if receivedPayload["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort 'high', got '%v'", receivedPayload["reasoning_effort"])
	}
	// temperature was overridden to 0.2
	if fmt.Sprintf("%v", receivedPayload["temperature"]) != "0.2" {
		t.Fatalf("expected temperature 0.2, got '%v'", receivedPayload["temperature"])
	}
	// max_tokens from default params
	if fmt.Sprintf("%v", receivedPayload["max_tokens"]) != "2000" {
		t.Fatalf("expected max_tokens 2000, got '%v'", receivedPayload["max_tokens"])
	}
	if receivedPayload["custom_arg"] != "hello_custom" {
		t.Fatalf("expected custom_arg 'hello_custom', got '%v'", receivedPayload["custom_arg"])
	}

	// 6. Verify client response received SSE stream
	clientRespBody := w.Body.String()
	if !strings.Contains(clientRespBody, "你好！") || !strings.Contains(clientRespBody, "data: [DONE]") {
		t.Fatalf("client did not receive expected SSE stream: %s", clientRespBody)
	}

	// 7. Verify session question and answer persisted
	msgs, err := chatSvc.ListMessages(sess.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in session, got %d", len(msgs))
	}
	if msgs[0].Content != "你好，请回复" {
		t.Fatalf("expected user message '你好，请回复', got '%s'", msgs[0].Content)
	}
	if msgs[1].Content != "你好！很高兴为你服务。" {
		t.Fatalf("expected assistant message content '你好！很高兴为你服务。', got '%s'", msgs[1].Content)
	}
	if msgs[1].ReasoningContent != "推理：分析问候语" {
		t.Fatalf("expected reasoning content '推理：分析问候语', got '%s'", msgs[1].ReasoningContent)
	}
}
