package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
)

func TestAuthChatCompletionsLoopbackToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "br_harness_test_loopback"

	r := gin.New()
	r.POST("/completions", authmiddleware.AuthChatCompletions(nil, nil, token), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": authmiddleware.GetUserID(c)})
	})

	t.Run("loopback matching token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/completions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-loopback rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/completions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = "192.168.1.2:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/completions", nil)
		req.Header.Set("Authorization", "Bearer br_harness_wrong")
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}
