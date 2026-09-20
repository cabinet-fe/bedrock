package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

// setupMailHandlerRouter mounts the mail handler behind real RBAC middleware;
// the SMTP transport is replaced by a no-op fake so no mail is sent.
func setupMailHandlerRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("init encryption: %v", err)
	}

	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "mail_handler.sqlite"),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close(gdb)
	})

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}

	userRepo := authrepo.NewUserRepository(gdb)
	roleRepo := rbacrepo.NewRoleRepository(gdb)
	resourceRepo := rbacrepo.NewResourceRepository(gdb)
	menuGroupRepo := rbacrepo.NewMenuGroupRepository(gdb)
	permSvc := rbacservice.NewPermissionService(roleRepo, resourceRepo, menuGroupRepo)

	pwdHash, err := pkg.HashPassword("admin-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := &authmodel.User{Username: "superadmin", PasswordHash: pwdHash, IsActive: true, IsSuperAdmin: true}
	if err := userRepo.Create(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	normal := &authmodel.User{Username: "normaluser", PasswordHash: pwdHash, IsActive: true}
	if err := userRepo.Create(normal); err != nil {
		t.Fatalf("create normal user: %v", err)
	}

	mailSvc := service.NewMailService(repository.NewMailRepository(gdb))
	mailSvc.SetSender(func(model.MailSMTPConfig, string, string, string, string) error { return nil })

	r := gin.New()
	authMW := func(c *gin.Context) {
		u := c.GetHeader("X-Test-User")
		if u == "admin" {
			c.Set("user_id", admin.ID)
			c.Set("username", admin.Username)
			c.Set("is_super_admin", true)
			c.Next()
			return
		}
		if u == "normal" {
			c.Set("user_id", normal.ID)
			c.Set("username", normal.Username)
			c.Set("is_super_admin", false)
			c.Next()
			return
		}
		pkg.Error(c, http.StatusUnauthorized, "未登录")
		c.Abort()
	}
	NewMailHandler(mailSvc, permSvc).RegisterRoutes(r.Group("/api/v1"), authMW)
	return r
}

func doMailJSON(r *gin.Engine, testUser, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if testUser != "" {
		req.Header.Set("X-Test-User", testUser)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMailHandler_Permissions(t *testing.T) {
	r := setupMailHandlerRouter(t)

	// Unauthenticated -> 401
	if w := doMailJSON(r, "", http.MethodGet, "/api/v1/system/mail/smtp", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated, got %d", w.Code)
	}

	// Super admin reads the not-yet-configured entry -> 200 without data
	// (kept before the normal-user PUT so the store is still empty).
	{
		w := doMailJSON(r, "admin", http.MethodGet, "/api/v1/system/mail/smtp", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Code int                         `json:"code"`
			Data *service.MailSMTPConfigView `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal get response: %v", err)
		}
		if resp.Data != nil {
			t.Fatalf("expected no data before first save, got %+v", resp.Data)
		}
	}

	// Normal user passes RBAC now on all three endpoints (permissions resolve
	// system-wide; mail settings are not super_admin_only)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/system/mail/smtp", nil},
		{http.MethodPut, "/api/v1/system/mail/smtp", map[string]any{"host": "smtp.example.com", "port": 465, "from_address": "noreply@example.com"}},
		{http.MethodPost, "/api/v1/system/mail/smtp/test", map[string]any{"to": "me@example.com"}},
	} {
		if w := doMailJSON(r, "normal", tc.method, tc.path, tc.body); w.Code != http.StatusOK {
			t.Fatalf("%s %s as normal user: expected 200, got %d: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// Super admin saves the config -> 200, password masked
	{
		w := doMailJSON(r, "admin", http.MethodPut, "/api/v1/system/mail/smtp", map[string]any{
			"host": "smtp.example.com", "port": 465, "username": "noreply@example.com",
			"password": "smtp-secret-pass", "from_address": "noreply@example.com",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin PUT, got %d: %s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, "smtp-secret-pass") {
			t.Fatalf("save response leaks plaintext password: %s", body)
		}
		var resp struct {
			Code int                        `json:"code"`
			Data service.MailSMTPConfigView `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal save response: %v", err)
		}
		if resp.Data.Password != "******" {
			t.Fatalf("expected masked password, got %q", resp.Data.Password)
		}
	}

	// Super admin sends a test mail -> 200 with success=true
	{
		w := doMailJSON(r, "admin", http.MethodPost, "/api/v1/system/mail/smtp/test", map[string]any{"to": "me@example.com"})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin test send, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Code int                    `json:"code"`
			Data service.MailTestResult `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal test response: %v", err)
		}
		if !resp.Data.Success {
			t.Fatalf("expected success=true, got %+v", resp.Data)
		}
	}

	// Normal user can still read after the config exists (RBAC passes
	// system-wide now; masking rules apply equally to any viewer).
	if w := doMailJSON(r, "normal", http.MethodGet, "/api/v1/system/mail/smtp", nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200 for normal GET after save, got %d", w.Code)
	}
}
