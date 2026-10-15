package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/ops/repository"
	opsservice "bedrock/internal/ops/service"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	"bedrock/internal/rbac"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

type siteLinkHTTPEnv struct {
	router *gin.Engine
	repo   *repository.OpsRepository
}

func setupSiteLinkHTTP(t *testing.T) *siteLinkHTTPEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "site_link_http.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(gdb) })
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	users := authrepo.NewUserRepository(gdb)
	hash, err := pkg.HashPassword("pass")
	if err != nil {
		t.Fatal(err)
	}
	admin := &authmodel.User{Username: "admin", PasswordHash: hash, IsActive: true, IsSuperAdmin: true}
	normal := &authmodel.User{Username: "normal", PasswordHash: hash, IsActive: true}
	if err := users.Create(admin); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(normal); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewOpsRepository(gdb)
	perm := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)
	authMW := func(c *gin.Context) {
		switch c.GetHeader("X-Test-User") {
		case "admin":
			c.Set("user_id", admin.ID)
			c.Set("username", admin.Username)
			c.Set("is_super_admin", true)
			c.Next()
		case "normal":
			c.Set("user_id", normal.ID)
			c.Set("username", normal.Username)
			c.Set("is_super_admin", false)
			c.Next()
		default:
			pkg.Error(c, http.StatusUnauthorized, "未登录")
		}
	}
	r := gin.New()
	NewOpsHandler(nil, nil, opsservice.NewSiteLinkService(repo), perm).
		RegisterRoutes(r.Group("/api/v1"), authMW)
	return &siteLinkHTTPEnv{router: r, repo: repo}
}

func doSiteLink(t *testing.T, r *gin.Engine, user, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSiteLinkRoutesAndHTTP(t *testing.T) {
	env := setupSiteLinkHTTP(t)

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/ops/site-links"},
		{http.MethodPost, "/api/v1/ops/site-links"},
		{http.MethodPut, "/api/v1/ops/site-links/1"},
		{http.MethodDelete, "/api/v1/ops/site-links/1"},
	} {
		if w := doSiteLink(t, env.router, "", call.method, call.path, map[string]any{"name": "n", "url": "https://e.example"}); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s unauthenticated = %d %s", call.method, w.Code, w.Body.String())
		}
	}

	if w := doSiteLink(t, env.router, "normal", http.MethodGet, "/api/v1/ops/site-links", nil); w.Code != http.StatusOK {
		t.Fatalf("normal GET = %d %s", w.Code, w.Body.String())
	}
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/ops/site-links"},
		{http.MethodPut, "/api/v1/ops/site-links/1"},
		{http.MethodDelete, "/api/v1/ops/site-links/1"},
	} {
		w := doSiteLink(t, env.router, "normal", call.method, call.path, map[string]any{"name": "n", "url": "https://e.example"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s normal = %d %s", call.method, w.Code, w.Body.String())
		}
	}
	items, err := env.repo.ListSiteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("forbidden write persisted %d rows", len(items))
	}

	if w := doSiteLink(t, env.router, "admin", http.MethodPost, "/api/v1/ops/site-links", map[string]any{
		"name": "  ", "url": "https://blank.example",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("blank name = %d %s", w.Code, w.Body.String())
	}
	huge := make([]byte, rbac.MaxMenuIconBytes+1)
	for i := range huge {
		huge[i] = 3
	}
	if w := doSiteLink(t, env.router, "admin", http.MethodPost, "/api/v1/ops/site-links", map[string]any{
		"name": "big", "url": "https://big.example", "icon_base64": base64.StdEncoding.EncodeToString(huge),
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("huge icon = %d %s", w.Code, w.Body.String())
	}
	items, err = env.repo.ListSiteLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("rejected create persisted %d rows", len(items))
	}

	const plain = "plain-secret"
	w := doSiteLink(t, env.router, "admin", http.MethodPost, "/api/v1/ops/site-links", map[string]any{
		"name": "site", "url": "https://site.example", "account": "user", "password": plain,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data opsservice.SiteLinkView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.Password != plain {
		t.Fatalf("create password = %q", created.Data.Password)
	}
	stored, err := env.repo.FindSiteLink(created.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordCipher == "" || stored.PasswordCipher == plain || strings.Contains(w.Body.String(), stored.PasswordCipher) || strings.Contains(w.Body.String(), "password_cipher") {
		t.Fatalf("ciphertext leaked or missing: body=%s cipher=%q", w.Body.String(), stored.PasswordCipher)
	}
	cipherBeforeReject := stored.PasswordCipher

	if w := doSiteLink(t, env.router, "normal", http.MethodGet, "/api/v1/ops/site-links", nil); w.Code != http.StatusOK {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), plain) || strings.Contains(w.Body.String(), stored.PasswordCipher) {
		t.Fatalf("list password mismatch: %s", w.Body.String())
	}

	if w := doSiteLink(t, env.router, "admin", http.MethodPut, "/api/v1/ops/site-links/"+strconv.FormatUint(uint64(created.Data.ID), 10), map[string]any{
		"name": "site", "url": "   ",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("blank url update = %d %s", w.Code, w.Body.String())
	}
	stored, err = env.repo.FindSiteLink(created.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.URL != "https://site.example" || stored.PasswordCipher != cipherBeforeReject {
		t.Fatalf("rejected update changed the row: %+v", stored)
	}

	if w := doSiteLink(t, env.router, "admin", http.MethodPut, "/api/v1/ops/site-links/abc", map[string]any{
		"name": "site", "url": "https://site.example",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d %s", w.Code, w.Body.String())
	}

	cleared := doSiteLink(t, env.router, "admin", http.MethodPut, "/api/v1/ops/site-links/"+strconv.FormatUint(uint64(created.Data.ID), 10), map[string]any{
		"name": "site", "url": "https://site.example", "password": "",
	})
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear password = %d %s", cleared.Code, cleared.Body.String())
	}
	stored, err = env.repo.FindSiteLink(created.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordCipher != "" || !strings.Contains(cleared.Body.String(), `"password":""`) {
		t.Fatalf("empty password stored=%q body=%s", stored.PasswordCipher, cleared.Body.String())
	}
}
