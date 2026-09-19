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

func setupAuthRouter(t *testing.T, allowRegister bool) *gin.Engine {
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
	if err := seed.EnsureDefaultUserRole(gdb); err != nil {
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
	roleSvc := rbacservice.NewRoleService(roles, resources)
	authSvc, err := authservice.NewAuthService(cfg, users, permSvc, roleSvc)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	api := r.Group("/api/v1")
	h := authhandler.NewAuthHandler(authSvc, allowRegister)
	h.RegisterRoutes(api, authmiddleware.AuthWithPAT(authSvc, nil))
	return r
}

func TestLogin_passwordCipher(t *testing.T) {
	r := setupAuthRouter(t, true)
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

func postJSON(r *gin.Engine, path string, body map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRegister_success(t *testing.T) {
	r := setupAuthRouter(t, true)

	w := postJSON(r, "/api/v1/auth/register", map[string]string{
		"username": "alice", "password": "password123",
	})
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
			Menus       []struct {
				Title    string `json:"title"`
				Children []struct {
					Title string `json:"title"`
					Path  string `json:"path"`
				} `json:"children"`
			} `json:"menus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.AccessToken == "" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	if resp.Data.User.Username != "alice" || resp.Data.User.IsSuperAdmin {
		t.Fatalf("user=%+v", resp.Data.User)
	}

	perms := map[string]bool{}
	for _, p := range resp.Data.Permissions {
		perms[p] = true
	}
	for _, want := range []string{
		"cicd_build_jobs:view", "cicd_script_jobs:view", "cicd_pipelines:view",
		"project_projects:view", "project_bugs:view",
		"ai_agents:view", "ai_skills:view",
	} {
		if !perms[want] {
			t.Fatalf("registered user missing permission %s: %v", want, resp.Data.Permissions)
		}
	}
	if perms["project_projects:view_all"] || perms["project_projects:manage_all"] {
		t.Fatalf("registered user must not bypass project data scope: %v", resp.Data.Permissions)
	}

	paths := map[string]bool{}
	for _, g := range resp.Data.Menus {
		for _, item := range g.Children {
			paths[item.Path] = true
		}
	}
	for _, want := range []string{
		"/cicd/build-jobs", "/cicd/script-jobs", "/cicd/pipelines",
		"/project/projects", "/project/bugs", "/ai/agents", "/ai/skills",
	} {
		if !paths[want] {
			t.Fatalf("registered user missing menu %s: %v", want, paths)
		}
	}

	var refreshCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil || refreshCookie.Value == "" || !refreshCookie.HttpOnly {
		t.Fatalf("expected HttpOnly refresh_token cookie, got %v", w.Header().Values("Set-Cookie"))
	}

	// The stored hash must authenticate through the plain login path.
	wLogin := postJSON(r, "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "password123",
	})
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login after register status=%d body=%s", wLogin.Code, wLogin.Body.String())
	}
}

func TestRegister_rejections(t *testing.T) {
	tests := []struct {
		name          string
		allowRegister bool
		body          map[string]string
		wantStatus    int
	}{
		{"disabled", false, map[string]string{"username": "bob", "password": "password123"}, http.StatusForbidden},
		{"duplicate username", true, map[string]string{"username": "admin", "password": "password123"}, http.StatusBadRequest},
		{"short username", true, map[string]string{"username": "ab", "password": "password123"}, http.StatusBadRequest},
		{"short password", true, map[string]string{"username": "bob", "password": "short"}, http.StatusBadRequest},
		{"missing password", true, map[string]string{"username": "bob"}, http.StatusBadRequest},
		{"missing username", true, map[string]string{"password": "password123"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupAuthRouter(t, tt.allowRegister)
			w := postJSON(r, "/api/v1/auth/register", tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
