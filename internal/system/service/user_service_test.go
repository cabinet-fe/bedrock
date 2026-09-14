package service_test

import (
	"path/filepath"
	"testing"

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

func setupUserDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "user.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&authmodel.User{}, &rbacmodel.Role{}, &rbacmodel.RolePermission{}, &rbacmodel.UserRole{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newUserService(db *gorm.DB) *service.UserService {
	return service.NewUserService(
		authrepo.NewUserRepository(db),
		rbacservice.NewRoleService(rbacrepo.NewRoleRepository(db), rbacrepo.NewResourceRepository(db)),
	)
}

func createTestUser(t *testing.T, db *gorm.DB, username, email, password string) *authmodel.User {
	t.Helper()
	hash, err := pkg.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &authmodel.User{Username: username, PasswordHash: hash, Email: email, IsActive: true}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestUserService_UpdateMyEmail(t *testing.T) {
	db := setupUserDB(t)
	svc := newUserService(db)
	u := createTestUser(t, db, "alice", "alice@example.com", "pass")

	got, err := svc.UpdateMyEmail(u.ID, service.UpdateMyEmailInput{Email: " alice@new.com "})
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if got.Email != "alice@new.com" {
		t.Fatalf("dto email = %q", got.Email)
	}
	stored, err := svc.Get(u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.Email != "alice@new.com" {
		t.Fatalf("users.email = %q, want alice@new.com", stored.Email)
	}
}

func TestUserService_UpdateMyEmailRejects(t *testing.T) {
	db := setupUserDB(t)
	svc := newUserService(db)
	alice := createTestUser(t, db, "alice", "alice@example.com", "pass")
	createTestUser(t, db, "bob", "bob@example.com", "pass")

	cases := []struct {
		name  string
		email string
	}{
		{"empty", ""},
		{"bad format", "not-an-email"},
		{"display name form", "Bob <bob@example.com>"},
		{"taken by others", "bob@example.com"},
	}
	for _, tc := range cases {
		if _, err := svc.UpdateMyEmail(alice.ID, service.UpdateMyEmailInput{Email: tc.email}); err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
	stored, err := svc.Get(alice.ID)
	if err != nil || stored.Email != "alice@example.com" {
		t.Fatalf("email changed after rejection: %q err=%v", stored.Email, err)
	}
}

func TestUserService_ChangeMyPassword(t *testing.T) {
	db := setupUserDB(t)
	svc := newUserService(db)
	u := createTestUser(t, db, "carol", "carol@example.com", "old-secret")

	if err := svc.ChangeMyPassword(u.ID, service.ChangeMyPasswordInput{OldPassword: "wrong", NewPassword: "new-secret"}); err == nil {
		t.Fatal("expected wrong old password to be rejected")
	}
	stored, err := svc.Get(u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !pkg.CheckPassword("old-secret", stored.PasswordHash) {
		t.Fatal("hash should be unchanged after rejection")
	}

	if err := svc.ChangeMyPassword(u.ID, service.ChangeMyPasswordInput{OldPassword: "old-secret", NewPassword: "new-secret"}); err != nil {
		t.Fatalf("change password: %v", err)
	}
	stored, err = svc.Get(u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !pkg.CheckPassword("new-secret", stored.PasswordHash) {
		t.Fatal("new password should verify")
	}
	if pkg.CheckPassword("old-secret", stored.PasswordHash) {
		t.Fatal("old password should not verify")
	}

	if err := svc.ChangeMyPassword(u.ID, service.ChangeMyPasswordInput{NewPassword: "another"}); err == nil {
		t.Fatal("expected empty old password to be rejected")
	}
}
