package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	authhandler "bedrock/internal/auth/handler"
	authmiddleware "bedrock/internal/auth/middleware"
	authrepo "bedrock/internal/auth/repository"
	authservice "bedrock/internal/auth/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

func setupAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := pkg.InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}

	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "auth.sqlite"),
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
	if err := seed.EnsureSuperAdmin(gdb, config.AdminConfig{
		Username:    "admin",
		Password:    "admin123",
		DisplayName: "管理员",
	}); err != nil {
		t.Fatal(err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		JWT: config.JWTConfig{Secret: "test-secret", AccessTTL: "1h", RefreshTTL: "24h"},
	}
	users := authrepo.NewUserRepository(gdb)
	roles := rbacrepo.NewRoleRepository(gdb)
	resources := rbacrepo.NewResourceRepository(gdb)
	groups := rbacrepo.NewMenuGroupRepository(gdb)
	permSvc := rbacservice.NewPermissionService(roles, resources, groups)
	authSvc, err := authservice.NewAuthService(cfg, users, permSvc)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	api := r.Group("/api/v1")
	h := authhandler.NewAuthHandler(authSvc)
	h.RegisterRoutes(api, authmiddleware.AuthWithPAT(authSvc, nil))
	return r
}

func TestLogin_passwordCipher(t *testing.T) {
	r := setupAuthRouter(t)
	const cipher = "000102030405060708090a0b0c0d0e0f17f1b26aff75e950ec141048626a9ed8"

	body, _ := json.Marshal(map[string]string{
		"username":        "admin",
		"password_cipher": cipher,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
			User        struct {
				Username     string `json:"username"`
				IsSuperAdmin bool   `json:"is_super_admin"`
			} `json:"user"`
			Permissions []string `json:"permissions"`
			Menus       []any    `json:"menus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.AccessToken == "" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	if !resp.Data.User.IsSuperAdmin || resp.Data.User.Username != "admin" {
		t.Fatalf("user=%+v", resp.Data.User)
	}
	if len(resp.Data.Permissions) == 0 {
		t.Fatalf("super-admin should have permissions")
	}
	if len(resp.Data.Menus) == 0 {
		t.Fatalf("super-admin should have menus")
	}

	var refreshCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil || refreshCookie.Value == "" {
		t.Fatalf("expected Set-Cookie refresh_token, got %v", w.Header().Values("Set-Cookie"))
	}
	if !refreshCookie.HttpOnly {
		t.Fatalf("refresh_token cookie must be HttpOnly")
	}
	if refreshCookie.Secure {
		t.Fatalf("refresh_token cookie must not set Secure (HTTP deployments)")
	}

	wRefresh := httptest.NewRecorder()
	reqRefresh := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader([]byte("{}")))
	reqRefresh.Header.Set("Content-Type", "application/json")
	reqRefresh.AddCookie(refreshCookie)
	r.ServeHTTP(wRefresh, reqRefresh)
	if wRefresh.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", wRefresh.Code, wRefresh.Body.String())
	}
	var refreshResp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wRefresh.Body.Bytes(), &refreshResp); err != nil {
		t.Fatal(err)
	}
	if refreshResp.Code != 0 || refreshResp.Data.AccessToken == "" {
		t.Fatalf("refresh response: %s", wRefresh.Body.String())
	}
	if refreshResp.Data.RefreshToken != "" {
		t.Fatalf("refresh_token must not appear in JSON body")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+resp.Data.AccessToken)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", w2.Code, w2.Body.String())
	}
}
