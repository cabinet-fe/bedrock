package service_test

import (
	"context"
	"encoding/base64"
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
	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
	"bedrock/internal/rbac/service"
)

func setupRBAC(t *testing.T) (*service.PermissionService, *service.RoleService, *service.ResourceService, *authrepo.UserRepository, *rbacrepo.RoleRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "rbac.sqlite"),
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
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}
	if err := seed.EnsureSuperAdmin(gdb, config.AdminConfig{
		Username: "admin", Password: "admin123", DisplayName: "Admin",
	}); err != nil {
		t.Fatal(err)
	}

	roles := rbacrepo.NewRoleRepository(gdb)
	resources := rbacrepo.NewResourceRepository(gdb)
	groups := rbacrepo.NewMenuGroupRepository(gdb)
	users := authrepo.NewUserRepository(gdb)
	return service.NewPermissionService(roles, resources, groups),
		service.NewRoleService(roles, resources),
		service.NewResourceService(resources, groups),
		users,
		roles
}

func TestPermissionUnion(t *testing.T) {
	perm, roles, _, users, _ := setupRBAC(t)

	hash, _ := pkg.HashPassword("pass")
	u := &authmodel.User{Username: "u1", PasswordHash: hash, IsActive: true}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}

	r1, err := roles.Create("A", "role_a", "", "", []string{"system_users:view", "resource_repositories:view"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := roles.Create("B", "role_b", "", "", []string{"system_roles:view", "resource_repositories:create"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{r1.ID, r2.ID}); err != nil {
		t.Fatal(err)
	}

	codes, err := perm.ResolvePermissions(u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	set := rbac.ToSet(codes)
	for _, want := range []string{
		"system_users:view", "resource_repositories:view", "system_roles:view", "resource_repositories:create",
	} {
		if !rbac.HasPermission(set, want) {
			t.Fatalf("missing %s in %v", want, codes)
		}
	}
}

func TestResolveDataScopeWidestWins(t *testing.T) {
	perm, roles, _, users, _ := setupRBAC(t)

	hash, _ := pkg.HashPassword("pass")
	u := &authmodel.User{Username: "scope_user", PasswordHash: hash, IsActive: true}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}

	selfRole, err := roles.Create("SelfOnly", "self_only", "", model.DataScopeSelf, []string{"system_users:view"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{selfRole.ID}); err != nil {
		t.Fatal(err)
	}
	scope, err := perm.ResolveDataScope(u.ID, false)
	if err != nil || scope != model.DataScopeSelf {
		t.Fatalf("expected self, got %q err=%v", scope, err)
	}

	allRole, err := roles.Create("AllScope", "all_scope", "", model.DataScopeAll, []string{"system_roles:view"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{selfRole.ID, allRole.ID}); err != nil {
		t.Fatal(err)
	}
	scope, err = perm.ResolveDataScope(u.ID, false)
	if err != nil || scope != model.DataScopeAll {
		t.Fatalf("expected all from union, got %q err=%v", scope, err)
	}

	scope, err = perm.ResolveDataScope(u.ID, true)
	if err != nil || scope != model.DataScopeAll {
		t.Fatalf("super-admin must be all, got %q err=%v", scope, err)
	}
}

func TestProjectScopeActionsAreSeededAndResolvable(t *testing.T) {
	perm, roles, resources, users, _ := setupRBAC(t)

	tree, err := resources.ListTree(service.ListResourcesFilter{})
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]model.RbacResource{}
	var collect func([]model.RbacResource)
	collect = func(nodes []model.RbacResource) {
		for _, node := range nodes {
			actions[node.FullCode] = node
			collect(node.Children)
		}
	}
	collect(tree)
	for _, permission := range []string{
		"project_projects:view_all",
		"project_projects:manage_all",
	} {
		action, ok := actions[permission]
		if !ok || action.Type != model.ResourceTypeAction {
			t.Fatalf("seeded scope action %q = %#v", permission, action)
		}
	}

	user := &authmodel.User{Username: "project_scope", PasswordHash: "hash", IsActive: true}
	if err := users.Create(user); err != nil {
		t.Fatal(err)
	}
	role, err := roles.Create("项目范围管理员", "project_scope_admin", "", "", []string{
		"project_projects:view",
		"project_projects:view_all",
		"project_projects:update",
		"project_projects:manage_all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(user.ID, []uint{role.ID}); err != nil {
		t.Fatal(err)
	}

	resolved, err := perm.ResolvePermissions(user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{
		"project_projects:view_all",
		"project_projects:manage_all",
	} {
		if !rbac.HasPermission(rbac.ToSet(resolved), permission) {
			t.Fatalf("resolved permissions missing %s: %v", permission, resolved)
		}
	}
}

func TestSuperAdminOnlyGate(t *testing.T) {
	perm, roles, _, users, roleRepo := setupRBAC(t)

	hash, _ := pkg.HashPassword("pass")
	u := &authmodel.User{Username: "opsfan", PasswordHash: hash, IsActive: true}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}

	// Binding super_admin_only features must be rejected.
	if _, err := roles.Create("OpsMistaken", "ops_mistaken", "", "", []string{
		"ops_processes:view", "system_users:view",
	}); err == nil {
		t.Fatal("expected reject binding ops_processes:view")
	}

	r, err := roles.Create("OpsMistaken", "ops_mistaken", "", "", []string{"system_users:view"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a stale grant that slipped into role_permissions.
	if err := roleRepo.ReplacePermissions(r.ID, []string{
		"ops_processes:view",
		"ops_processes:removed_legacy",
		"system_users:view",
	}); err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{r.ID}); err != nil {
		t.Fatal(err)
	}

	codes, err := perm.ResolvePermissions(u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range codes {
		if strings.HasPrefix(c, "ops_") {
			t.Fatalf("ops permission leaked to non-super: %s", c)
		}
	}
	if err := perm.CheckAccess(u.ID, false, "ops_processes:view"); err == nil || !service.IsForbidden(err) {
		t.Fatalf("expected super_admin_only hard gate 403, got %v", err)
	}
	if err := perm.CheckAccess(u.ID, false, "system_users:view"); err != nil {
		t.Fatalf("system_users:view should pass: %v", err)
	}
	if err := perm.CheckAccess(1, true, "ops_processes:view"); err != nil {
		t.Fatalf("super-admin should pass ops: %v", err)
	}
}

func TestMenuTrimTwoLevelGroups(t *testing.T) {
	perm, roles, _, users, _ := setupRBAC(t)

	hash, _ := pkg.HashPassword("pass")
	u := &authmodel.User{Username: "viewer", PasswordHash: hash, IsActive: true}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}
	r, err := roles.Create("Viewer", "viewer", "", "", []string{"system_users:view"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{r.ID}); err != nil {
		t.Fatal(err)
	}

	menus, err := perm.TrimMenus(u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus) != 1 || menus[0].Title != "系统管理" {
		t.Fatalf("expected 系统管理 group, got %+v", menus)
	}
	if len(menus[0].Children) != 1 || menus[0].Children[0].Title != "用户" || menus[0].Children[0].Path != "/system/users" {
		t.Fatalf("expected 用户 leaf, got %+v", menus[0].Children)
	}

	r2, err := roles.Create("NoView", "noview", "", "", []string{"system_users:create"})
	if err != nil {
		t.Fatal(err)
	}
	u2 := &authmodel.User{Username: "noview", PasswordHash: hash, IsActive: true}
	if err := users.Create(u2); err != nil {
		t.Fatal(err)
	}
	_ = roles.SetUserRoles(u2.ID, []uint{r2.ID})
	menus2, err := perm.TrimMenus(u2.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus2) != 0 {
		t.Fatalf("expected empty menus without :view, got %+v", menus2)
	}
	if err := perm.CheckAccess(u2.ID, false, "system_users:view"); err == nil {
		t.Fatal("expected 403 without :view")
	}
}

func TestHiddenMenusExcludedFromNav(t *testing.T) {
	perm, _, resources, _, _ := setupRBAC(t)
	tree, err := resources.ListTree(service.ListResourcesFilter{Type: model.ResourceTypeMenu})
	if err != nil || len(tree) == 0 {
		t.Fatalf("need menus: %v", err)
	}
	var gid *uint
	for _, m := range tree {
		if m.Code == "dashboard" && m.GroupID != nil {
			gid = m.GroupID
			break
		}
	}
	if gid == nil {
		t.Fatal("dashboard menu missing group_id")
	}
	hidden := true
	enabled := true
	if _, err := resources.Create(service.CreateResourceInput{
		Type: model.ResourceTypeMenu, Code: "hidden_mount", Title: "隐藏挂载",
		GroupID: gid, Hidden: &hidden, Enabled: &enabled,
	}, true); err != nil {
		t.Fatal(err)
	}
	menus, err := perm.TrimMenus(1, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range menus {
		for _, item := range g.Children {
			if item.Title == "隐藏挂载" {
				t.Fatalf("hidden menu leaked into nav: %+v", item)
			}
		}
	}
}

func TestListTreeFilterKeepsAncestors(t *testing.T) {
	_, _, resources, _, _ := setupRBAC(t)

	tree, err := resources.ListTree(service.ListResourcesFilter{Keyword: "system_users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Code != "system_users" {
		t.Fatalf("expected system_users root, got %+v", summarizeTree(tree))
	}
	var foundView bool
	var walk func([]model.RbacResource)
	walk = func(nodes []model.RbacResource) {
		for _, n := range nodes {
			if n.FullCode == "system_users:view" {
				foundView = true
			}
			walk(n.Children)
		}
	}
	walk(tree)
	if !foundView {
		t.Fatalf("expected system_users:view under menu, got %+v", summarizeTree(tree))
	}

	actions, err := resources.ListTree(service.ListResourcesFilter{Type: model.ResourceTypeAction})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) == 0 {
		t.Fatal("expected action matches with ancestors")
	}
	var walkActions func([]model.RbacResource)
	walkActions = func(nodes []model.RbacResource) {
		for _, n := range nodes {
			if len(n.Children) == 0 && n.Type != model.ResourceTypeAction {
				t.Fatalf("leaf %q should be action when filtering type=action", n.FullCode)
			}
			walkActions(n.Children)
		}
	}
	walkActions(actions)

	if _, err := resources.ListTree(service.ListResourcesFilter{Type: "nope"}); err == nil {
		t.Fatal("expected invalid type error")
	}
}

func summarizeTree(nodes []model.RbacResource) []string {
	var out []string
	var walk func([]model.RbacResource, string)
	walk = func(items []model.RbacResource, prefix string) {
		for _, n := range items {
			out = append(out, prefix+n.FullCode+":"+n.Type)
			walk(n.Children, prefix+"  ")
		}
	}
	walk(nodes, "")
	return out
}

func TestMenuIconRejectOver32KB(t *testing.T) {
	_, _, resources, _, _ := setupRBAC(t)

	tree, err := resources.ListTree(service.ListResourcesFilter{Type: model.ResourceTypeMenu})
	if err != nil {
		t.Fatal(err)
	}
	var menuID uint
	for _, n := range tree {
		if n.Code == "system_users" {
			menuID = n.ID
			break
		}
	}
	if menuID == 0 {
		t.Fatal("system_users menu not found")
	}

	big := make([]byte, rbac.MaxMenuIconBytes+1)
	for i := range big {
		big[i] = 'A'
	}
	payload := base64.StdEncoding.EncodeToString(big)
	_, err = resources.UpdateMenuIcon(menuID, payload, "image/png")
	if err == nil || !strings.Contains(err.Error(), "32KB") {
		t.Fatalf("expected 32KB reject, got %v", err)
	}

	ok := make([]byte, 16)
	_, err = resources.UpdateMenuIcon(menuID, base64.StdEncoding.EncodeToString(ok), "image/png")
	if err != nil {
		t.Fatalf("small icon should pass: %v", err)
	}
}

func TestBuiltinRoleGuards(t *testing.T) {
	_, roles, _, users, roleRepo := setupRBAC(t)

	builtin, err := roleRepo.FindByCode(model.RoleCodeSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.Delete(builtin.ID); err == nil {
		t.Fatal("expected delete builtin reject")
	}
	if _, err := roles.SetPermissions(builtin.ID, []string{"system_users:view"}); err == nil {
		t.Fatal("expected set permissions on builtin reject")
	}

	hash, _ := pkg.HashPassword("pass")
	u := &authmodel.User{Username: "normal", PasswordHash: hash, IsActive: true}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}
	if err := roles.SetUserRoles(u.ID, []uint{builtin.ID}); err == nil {
		t.Fatal("expected reject binding super_admin role")
	}
}

func TestPermissionCatalog(t *testing.T) {
	perm, _, _, _, _ := setupRBAC(t)
	catalog, err := perm.PermissionCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) == 0 {
		t.Fatal("expected catalog groups")
	}
	var foundSystemUsers bool
	for _, g := range catalog {
		for _, m := range g.Menus {
			if m.Code == "system_users" && len(m.Features) > 0 {
				foundSystemUsers = true
			}
			if m.Code == "ops_processes" && !m.SuperAdminOnly {
				t.Fatal("ops_processes should be super_admin_only")
			}
		}
	}
	if !foundSystemUsers {
		t.Fatal("expected system_users in catalog")
	}
}

func TestValidCode(t *testing.T) {
	if !rbac.ValidCode("system_users") {
		t.Fatal("system_users should be valid")
	}
	if rbac.ValidCode("system.users") {
		t.Fatal("dotted code should be invalid")
	}
}

func TestMenuGroupDeleteRejectsNonEmpty(t *testing.T) {
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "menu_groups.sqlite"),
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
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	groups := rbacrepo.NewMenuGroupRepository(gdb)
	svc := service.NewMenuGroupService(groups)

	if _, err := svc.Create(service.CreateMenuGroupInput{Name: "Bad", Code: "bad.group"}); err == nil {
		t.Fatal("expected dotted code reject")
	}

	created, err := svc.Create(service.CreateMenuGroupInput{
		Name: "临时分组", Code: "tmp_group", RoutePrefix: "/tmp", SortKey: 999,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := groups.List()
	if err != nil {
		t.Fatal(err)
	}
	var systemID uint
	for _, item := range items {
		if item.Code == "system" {
			systemID = item.ID
			break
		}
	}
	if systemID == 0 {
		t.Fatal("system menu group not found")
	}
	if err := svc.Delete(systemID); err == nil {
		t.Fatal("expected delete non-empty group reject")
	}
	if err := svc.Delete(created.ID); err != nil {
		t.Fatalf("empty group should delete: %v", err)
	}
}
