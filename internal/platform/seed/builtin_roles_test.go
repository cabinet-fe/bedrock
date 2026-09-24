package seed_test

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacmodel "bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
)

func migratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "seed_builtin_roles.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(t.Context(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}
	return gdb
}

func findRole(t *testing.T, gdb *gorm.DB, query string, args ...interface{}) rbacmodel.Role {
	t.Helper()
	var role rbacmodel.Role
	if err := gdb.Where(query, args...).First(&role).Error; err != nil {
		t.Fatalf("find role %v: %v", args, err)
	}
	return role
}

func countRoles(t *testing.T, gdb *gorm.DB, query string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := gdb.Model(&rbacmodel.Role{}).Where(query, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func countPermissions(t *testing.T, gdb *gorm.DB, roleID uint) int64 {
	t.Helper()
	var n int64
	if err := gdb.Model(&rbacmodel.RolePermission{}).Where("role_id = ?", roleID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnsureBuiltinRoles_FreshInstall(t *testing.T) {
	gdb := migratedDB(t)

	if err := seed.EnsureBuiltinRoles(gdb); err != nil {
		t.Fatalf("EnsureBuiltinRoles failed: %v", err)
	}

	for _, code := range rbacmodel.SelectableBuiltinRoleCodes() {
		role := findRole(t, gdb, "code = ?", code)
		if !role.IsBuiltin() {
			t.Errorf("role %s type = %q, want builtin", code, role.Type)
		}
		if role.DataScope != rbacmodel.DataScopeSelf {
			t.Errorf("role %s data_scope = %q, want self", code, role.DataScope)
		}
		if n := countPermissions(t, gdb, role.ID); n == 0 {
			t.Errorf("role %s has no seeded permissions", code)
		}
	}
}

// Installs upgraded from <=2.5.12 may hold a custom role squatting a builtin
// name; the seed must absorb it instead of failing the roles.name UNIQUE insert.
func TestEnsureBuiltinRoles_AbsorbsCustomRoleWithSameName(t *testing.T) {
	gdb := migratedDB(t)

	squatter := rbacmodel.Role{
		Name: "测试", Code: "qa_team", Description: "现场自建",
		Type: rbacmodel.RoleTypeCustom, DataScope: rbacmodel.DataScopeSelf,
	}
	if err := gdb.Create(&squatter).Error; err != nil {
		t.Fatal(err)
	}
	byStand := rbacmodel.Role{
		Name: "质检", Code: "qc", Type: rbacmodel.RoleTypeCustom, DataScope: rbacmodel.DataScopeSelf,
	}
	if err := gdb.Create(&byStand).Error; err != nil {
		t.Fatal(err)
	}

	if err := seed.EnsureBuiltinRoles(gdb); err != nil {
		t.Fatalf("EnsureBuiltinRoles failed: %v", err)
	}

	role := findRole(t, gdb, "code = ?", rbacmodel.RoleCodeTester)
	if role.ID != squatter.ID {
		t.Errorf("tester role ID = %d, want absorbed row %d", role.ID, squatter.ID)
	}
	if !role.IsBuiltin() {
		t.Errorf("absorbed role type = %q, want builtin", role.Type)
	}
	if role.Description != "现场自建" {
		t.Errorf("absorbed role description = %q, want preserved", role.Description)
	}
	if n := countPermissions(t, gdb, role.ID); n == 0 {
		t.Errorf("absorbed role has no seeded permissions")
	}
	if n := countRoles(t, gdb, "name = ?", "测试"); n != 1 {
		t.Errorf("roles named 测试 = %d, want 1", n)
	}

	untouched := findRole(t, gdb, "code = ?", "qc")
	if untouched.IsBuiltin() {
		t.Errorf("unrelated custom role was absorbed into builtin shape")
	}
}

func TestEnsureBuiltinRoles_IdempotentAndPreservesAdminPermissionEdits(t *testing.T) {
	gdb := migratedDB(t)

	if err := seed.EnsureBuiltinRoles(gdb); err != nil {
		t.Fatalf("first EnsureBuiltinRoles failed: %v", err)
	}
	role := findRole(t, gdb, "code = ?", rbacmodel.RoleCodeDeveloper)

	adminEdits := []string{"dashboard:view", "project_bugs:view"}
	if err := rbacrepo.NewRoleRepository(gdb).ReplacePermissions(role.ID, adminEdits); err != nil {
		t.Fatal(err)
	}

	if err := seed.EnsureBuiltinRoles(gdb); err != nil {
		t.Fatalf("second EnsureBuiltinRoles failed: %v", err)
	}

	if n := countRoles(t, gdb, "code = ?", rbacmodel.RoleCodeDeveloper); n != 1 {
		t.Errorf("developer roles after reseed = %d, want 1", n)
	}
	got := countPermissions(t, gdb, role.ID)
	if got != int64(len(adminEdits)) {
		t.Errorf("permission count after reseed = %d, want %d (admin edits must be preserved)", got, len(adminEdits))
	}
}
