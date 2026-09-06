package seed_test

import (
	"context"
	"path/filepath"
	"testing"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	rbacmodel "bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
)

func TestEnsureRBACResources_SystemBackup(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "seed_backup.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatalf("migration.Up failed: %v", err)
	}

	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("EnsureRBACResources failed: %v", err)
	}

	// Verify menu resource
	var menu rbacmodel.RbacResource
	if err := gdb.Where("full_code = ? AND type = ?", "system_backup", rbacmodel.ResourceTypeMenu).First(&menu).Error; err != nil {
		t.Fatalf("expected system_backup menu resource: %v", err)
	}
	if menu.Title != "系统备份" || menu.Route != "/system/backup" {
		t.Errorf("unexpected menu title/route: %s / %s", menu.Title, menu.Route)
	}

	// Verify 5 action features
	expectedActions := map[string]string{
		"system_backup:view":     "查看",
		"system_backup:create":   "创建",
		"system_backup:restore":  "恢复",
		"system_backup:download": "下载",
		"system_backup:delete":   "删除",
	}

	for fullCode, expectedTitle := range expectedActions {
		var feat rbacmodel.RbacResource
		if err := gdb.Where("full_code = ? AND type = ?", fullCode, rbacmodel.ResourceTypeAction).First(&feat).Error; err != nil {
			t.Errorf("expected feature %s: %v", fullCode, err)
			continue
		}
		if feat.Title != expectedTitle {
			t.Errorf("feature %s title = %q, want %q", fullCode, feat.Title, expectedTitle)
		}
	}

	// Verify super-admin receives all 5 actions
	roles := rbacrepo.NewRoleRepository(gdb)
	resources := rbacrepo.NewResourceRepository(gdb)
	groups := rbacrepo.NewMenuGroupRepository(gdb)
	permSvc := rbacservice.NewPermissionService(roles, resources, groups)

	perms, err := permSvc.ResolvePermissions(1, true)
	if err != nil {
		t.Fatalf("ResolvePermissions for super admin: %v", err)
	}

	permSet := make(map[string]bool)
	for _, p := range perms {
		permSet[p] = true
	}

	for fullCode := range expectedActions {
		if !permSet[fullCode] {
			t.Errorf("super admin missing permission: %s", fullCode)
		}
	}
}
