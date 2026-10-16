package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
)

func TestAuthWithPATDownloadQueryToken(t *testing.T) {
	authSvc, _ := newAuthAndPAT(t)
	user, err := authSvc.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authSvc.GenerateTokenPair(user)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	mw := authmiddleware.AuthWithPAT(authSvc, nil)
	r.GET("/api/v1/build-runs/:id/artifact", mw, ok)
	r.GET("/api/v1/build-runs/:id", mw, ok)

	t.Run("artifact query token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/build-runs/1/artifact?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-download query token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/build-runs/1?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("bad authorization does not fall through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/build-runs/1/artifact?token="+token, nil)
		req.Header.Set("Authorization", "Basic "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
