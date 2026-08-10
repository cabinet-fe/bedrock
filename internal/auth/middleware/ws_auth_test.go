package middleware_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	authmiddleware "bedrock/internal/auth/middleware"
	authrepo "bedrock/internal/auth/repository"
	authservice "bedrock/internal/auth/service"
	"bedrock/internal/pkg"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	resourcerepo "bedrock/internal/resource/repository"
	"bedrock/internal/resource/model"
	resourceservice "bedrock/internal/resource/service"
	systemrepo "bedrock/internal/system/repository"
	systemservice "bedrock/internal/system/service"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
)

func TestResolveQueryTokenRejectsEmpty(t *testing.T) {
	authSvc, patSvc := newAuthAndPAT(t)
	if _, err := authmiddleware.ResolveQueryToken(authSvc, patSvc, ""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestResolveQueryTokenAcceptsJWT(t *testing.T) {
	authSvc, patSvc := newAuthAndPAT(t)
	user, err := authSvc.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authSvc.GenerateTokenPair(user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := authmiddleware.ResolveQueryToken(authSvc, patSvc, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user.ID || got.IsSuperAdmin != user.IsSuperAdmin {
		t.Fatalf("user = %#v, want id=%d super=%v", got, user.ID, user.IsSuperAdmin)
	}
}

func TestResolveQueryTokenAcceptsPAT(t *testing.T) {
	authSvc, patSvc := newAuthAndPAT(t)
	created, err := patSvc.Create(1, resourceservice.CreatePATInput{
		Name:   "ws-test",
		Scopes: []string{model.ScopeSkillsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := authmiddleware.ResolveQueryToken(authSvc, patSvc, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != 1 {
		t.Fatalf("user = %#v", got)
	}
}

func newAuthAndPAT(t *testing.T) (*authservice.AuthService, *resourceservice.PATService) {
	t.Helper()
	if err := pkg.InitEncryption(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "ws-auth.sqlite"),
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
		Username: "admin",
		Password: "admin123",
	}); err != nil {
		t.Fatal(err)
	}
	userRepo := authrepo.NewUserRepository(gdb)
	roleRepo := rbacrepo.NewRoleRepository(gdb)
	resourceRepo := rbacrepo.NewResourceRepository(gdb)
	menuGroupRepo := rbacrepo.NewMenuGroupRepository(gdb)
	permSvc := rbacservice.NewPermissionService(roleRepo, resourceRepo, menuGroupRepo)
	authSvc, err := authservice.NewAuthService(&config.Config{
		JWT: config.JWTConfig{Secret: "test-secret", AccessTTL: "1h", RefreshTTL: "24h"},
	}, userRepo, permSvc)
	if err != nil {
		t.Fatal(err)
	}
	logRepo := systemrepo.NewOperationLogRepository(gdb)
	auditSvc := systemservice.NewAuditService(logRepo)
	patRepo := resourcerepo.NewPATRepository(gdb)
	patSvc := resourceservice.NewPATService(patRepo, auditSvc)
	return authSvc, patSvc
}
