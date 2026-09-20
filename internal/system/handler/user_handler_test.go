package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/service"
)

func setupProfileRouter(t *testing.T, userID uint) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&authmodel.User{}, &rbacmodel.Role{}, &rbacmodel.UserRole{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users := service.NewUserService(
		authrepo.NewUserRepository(db),
		rbacservice.NewRoleService(rbacrepo.NewRoleRepository(db), rbacrepo.NewResourceRepository(db)),
	)

	r := gin.New()
	authMW := func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}
	NewUserHandler(users, nil).RegisterRoutes(r.Group("/api/v1"), authMW)
	return r, db
}

func doJSON(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUserHandler_UpdateMyEmailUsesAuthContext(t *testing.T) {
	r, db := setupProfileRouter(t, 7)
	hash, err := pkg.HashPassword("pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&authmodel.User{ID: 7, Username: "alice", PasswordHash: hash, Email: "old@example.com", IsActive: true}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	w := doJSON(r, http.MethodPut, "/api/v1/users/me/email", map[string]string{"email": "me@example.com"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var stored authmodel.User
	if err := db.First(&stored, 7).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}
	if stored.Email != "me@example.com" {
		t.Fatalf("users.email = %q, want me@example.com", stored.Email)
	}
}

func TestUserHandler_ChangeMyPassword(t *testing.T) {
	r, db := setupProfileRouter(t, 7)
	hash, err := pkg.HashPassword("old-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&authmodel.User{ID: 7, Username: "bob", PasswordHash: hash, IsActive: true}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	if w := doJSON(r, http.MethodPut, "/api/v1/users/me/password", map[string]string{"old_password": "wrong", "new_password": "new-secret"}); w.Code != http.StatusBadRequest {
		t.Fatalf("wrong old password: status = %d body = %s", w.Code, w.Body.String())
	}

	if w := doJSON(r, http.MethodPut, "/api/v1/users/me/password", map[string]string{"old_password": "old-secret", "new_password": "new-secret"}); w.Code != http.StatusOK {
		t.Fatalf("change password: status = %d body = %s", w.Code, w.Body.String())
	}
	var stored authmodel.User
	if err := db.First(&stored, 7).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}
	if !pkg.CheckPassword("new-secret", stored.PasswordHash) || pkg.CheckPassword("old-secret", stored.PasswordHash) {
		t.Fatal("password hash not rotated as expected")
	}
}
