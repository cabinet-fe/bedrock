package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	storagemodel "bedrock/internal/storage/model"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"

	"gorm.io/gorm"
)

func TestProjectACLListAndGlobalBypass(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1,
		"project_projects:create", "project_projects:view", "project_projects:update",
		"project_requirements:create", "project_requirements:view", "project_requirements:update",
		"project_docs:create", "project_docs:view", "project_docs:update",
	)
	project := createProject(t, svc, owner, "alpha")
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleReadonly}); err != nil {
		t.Fatal(err)
	}

	member := actor(2, "project_projects:view")
	items, total, err := svc.ListProjects(member, ProjectListFilter{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != project.ID {
		t.Fatalf("joined list = %#v total=%d err=%v", items, total, err)
	}

	viewAll := actor(3, "project_projects:view", "project_projects:view_all")
	items, total, err = svc.ListProjects(viewAll, ProjectListFilter{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("view_all list = %#v total=%d err=%v", items, total, err)
	}

	manager := actor(4, "project_projects:update", "project_projects:manage_all")
	if _, err := svc.AddMember(manager, project.ID, MemberInput{UserID: 5, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatalf("manage_all must manage without joining: %v", err)
	}
	ordinary := actor(6, "project_projects:update")
	if _, err := svc.AddMember(ordinary, project.ID, MemberInput{UserID: 7, Role: projectmodel.ProjectRoleMember}); !IsNotFound(err) {
		t.Fatalf("ordinary update must not bypass membership, got %v", err)
	}
}

func TestProjectListCapabilitiesReflectProjectACL(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1, "project_projects:create", "project_projects:update", "project_projects:delete")
	project := createProject(t, svc, owner, "capabilities")

	viewAll := actor(2,
		"project_projects:view",
		"project_projects:view_all",
		"project_projects:update",
		"project_projects:delete",
	)
	items, _, err := svc.ListProjects(viewAll, ProjectListFilter{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("view_all items = %#v", items)
	}
	if items[0].MyRole != "" {
		t.Fatalf("view_all user must not gain a project role: %#v", items[0])
	}
	if items[0].Permissions.Update || items[0].Permissions.Archive || items[0].Permissions.Delete {
		t.Fatalf("view_all without manage_all must not expose project mutations: %#v", items[0].Permissions)
	}
	if _, err := svc.UpdateProject(viewAll, project.ID, UpdateProjectInput{}); !IsNotFound(err) {
		t.Fatalf("view_all update must not bypass membership, got %v", err)
	}

	manager := actor(3,
		"project_projects:view",
		"project_projects:update",
		"project_projects:delete",
		"project_projects:manage_all",
	)
	items, _, err = svc.ListProjects(manager, ProjectListFilter{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Permissions.Update || !items[0].Permissions.Archive || !items[0].Permissions.Delete {
		t.Fatalf("manage_all must expose project mutations: %#v", items[0].Permissions)
	}
}

func TestRequirementStatusMetadataAllowsMemberWithoutDictionaryPermission(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1, "project_projects:create", "project_projects:update")
	project := createProject(t, svc, owner, "requirement-statuses")
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleReadonly}); err != nil {
		t.Fatal(err)
	}

	member := actor(2, "project_requirements:view")
	statuses, err := svc.ListRequirementStatuses(member)
	if err != nil {
		t.Fatalf("member without system_dictionaries:view must read requirement statuses: %v", err)
	}
	values := make([]string, len(statuses))
	for index, status := range statuses {
		values[index] = status.Value
	}
	want := []string{"backlog", "todo", "doing", "done", "cancelled"}
	if !slices.Equal(values, want) {
		t.Fatalf("requirement status values = %v, want %v", values, want)
	}

	if _, err := svc.ListRequirementStatuses(actor(3, "project_requirements:view")); !IsForbidden(err) {
		t.Fatalf("non-member requirement reader must not read metadata, got %v", err)
	}
}

func TestProjectACLUsesResolvedRolePermissions(t *testing.T) {
	svc, gdb := newProjectServiceWithDB(t)
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	users := authrepo.NewUserRepository(gdb)
	roles := rbacrepo.NewRoleRepository(gdb)
	resources := rbacrepo.NewResourceRepository(gdb)
	groups := rbacrepo.NewMenuGroupRepository(gdb)
	permissions := rbacservice.NewPermissionService(roles, resources, groups)
	roleService := rbacservice.NewRoleService(roles, resources)

	user := &authmodel.User{Username: "project_scope", PasswordHash: "hash", IsActive: true}
	if err := users.Create(user); err != nil {
		t.Fatal(err)
	}
	role, err := roleService.Create("项目范围管理员", "project_scope_admin", "", []string{
		"project_projects:view",
		"project_projects:view_all",
		"project_projects:update",
		"project_projects:manage_all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := roleService.SetUserRoles(user.ID, []uint{role.ID}); err != nil {
		t.Fatal(err)
	}

	owner := actor(99, "project_projects:create", "project_projects:update")
	project := createProject(t, svc, owner, "resolved-permissions")
	resolved, err := permissions.ResolvePermissions(user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	actorFromRole := NewAccessContext(user.ID, false, resolved)
	items, total, err := svc.ListProjects(actorFromRole, ProjectListFilter{ListQuery: pkg.ListQuery{Page: 1, PageSize: 20}})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != project.ID {
		t.Fatalf("view_all via resolved role = %#v total=%d err=%v", items, total, err)
	}
	if _, err := svc.AddMember(actorFromRole, project.ID, MemberInput{UserID: 100, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatalf("manage_all via resolved role must manage without joining: %v", err)
	}
}

func TestProjectRoleCapabilities(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1, "project_projects:create", "project_projects:update", "project_requirements:create")
	project := createProject(t, svc, owner, "roles")
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleReadonly}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 3, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 4, Role: projectmodel.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}

	readonly := actor(2, "project_requirements:create")
	if _, err := svc.CreateRequirement(readonly, project.ID, RequirementInput{Title: "blocked"}); !IsForbidden(err) {
		t.Fatalf("readonly create = %v, want forbidden", err)
	}
	member := actor(3, "project_requirements:create")
	if _, err := svc.CreateRequirement(member, project.ID, RequirementInput{Title: "allowed"}); err != nil {
		t.Fatalf("member create: %v", err)
	}
	admin := actor(4, "project_projects:update")
	if _, err := svc.AddMember(admin, project.ID, MemberInput{UserID: 5, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatalf("admin member management: %v", err)
	}
}

func TestOwnerTransferIsOwnerOrManageAllOnly(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1, "project_projects:create", "project_projects:update")
	project := createProject(t, svc, owner, "owner-transfer")
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 2, Role: projectmodel.ProjectRoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMember(owner, project.ID, MemberInput{UserID: 3, Role: projectmodel.ProjectRoleMember}); err != nil {
		t.Fatal(err)
	}
	admin := actor(2, "project_projects:update")
	if _, err := svc.TransferOwner(admin, project.ID, 3); !IsForbidden(err) {
		t.Fatalf("admin owner transfer = %v, want forbidden", err)
	}
	updated, err := svc.TransferOwner(owner, project.ID, 3)
	if err != nil || updated.OwnerID != 3 {
		t.Fatalf("owner transfer = %#v, err=%v", updated, err)
	}
}

func TestDocumentPublishConflictAndImportDraftOnly(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1,
		"project_projects:create", "project_projects:update",
		"project_docs:create", "project_docs:view", "project_docs:update",
	)
	project := createProject(t, svc, owner, "docs")
	published := "published"
	node, err := svc.CreateDocNode(owner, project.ID, DocNodeInput{
		Kind: projectmodel.DocNodeDocument, Name: "doc.md", DraftContent: &published,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishDocNode(owner, node.ID, 9); !IsConflict(err) {
		t.Fatalf("publish stale expected version = %v", err)
	}
	if _, err := svc.PublishDocNode(owner, node.ID, 0); err != nil {
		t.Fatal(err)
	}

	payload := makeZIP(t, map[string]string{"doc.md": "new draft"})
	if _, err := svc.ImportZIP(owner, project.ID, nil, "docs.zip", "application/zip", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.GetDocNode(owner, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PublishedContent != "published" || updated.DraftContent != "new draft" || updated.DraftUpdatedAt == nil {
		t.Fatalf("import must leave published unchanged: %#v", updated)
	}
}

func TestUpsertAndPublishDocByPath(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1,
		"project_projects:create",
		"project_docs:create", "project_docs:view", "project_docs:update",
	)
	project := createProject(t, svc, owner, "docs-path")

	if _, _, err := svc.UpsertDocByPath(owner, project.ID, "../escape", "UserController", "# bad"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if _, _, err := svc.UpsertDocByPath(owner, project.ID, "/abs", "UserController", "# bad"); err == nil {
		t.Fatal("absolute api_dir must be rejected")
	}

	created, isNew, err := svc.UpsertDocByPath(owner, project.ID, "ic-common-resource/controllers", "UserController", "# v1")
	if err != nil || !isNew {
		t.Fatalf("create upsert = %#v new=%v err=%v", created, isNew, err)
	}
	if created.Name != "UserController.md" || created.DraftContent != "# v1" || created.PublishedContent != "" {
		t.Fatalf("created draft: %#v", created)
	}
	tree, err := svc.ListDocTree(owner, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Name != "ic-common-resource" || len(tree[0].Children) != 1 {
		t.Fatalf("dirs must be auto-created: %#v", tree)
	}

	updated, isNew, err := svc.UpsertDocByPath(owner, project.ID, "ic-common-resource/controllers", "UserController.md", "# v2")
	if err != nil || isNew || updated.ID != created.ID {
		t.Fatalf("update upsert = %#v new=%v err=%v", updated, isNew, err)
	}
	if updated.DraftContent != "# v2" || updated.PublishedContent != "" {
		t.Fatalf("upsert must overwrite draft only: %#v", updated)
	}

	if _, err := svc.PublishDocByPath(owner, project.ID, "missing/dir", "UserController"); !IsNotFound(err) {
		t.Fatalf("missing path = %v", err)
	}
	published, err := svc.PublishDocByPath(owner, project.ID, "ic-common-resource/controllers", "UserController")
	if err != nil {
		t.Fatal(err)
	}
	if published.PublishedContent != "# v2" || published.DraftUpdatedAt != nil {
		t.Fatalf("publish-path: %#v", published)
	}
	if _, err := svc.PublishDocByPath(owner, project.ID, "ic-common-resource/controllers", "UserController"); err == nil {
		t.Fatal("publish without draft must fail")
	}

	// Conflict: dir name occupied by a document.
	blocker, err := svc.CreateDocNode(owner, project.ID, DocNodeInput{
		Kind: projectmodel.DocNodeDocument, Name: "blocked-dir",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = blocker
	if _, _, err := svc.UpsertDocByPath(owner, project.ID, "blocked-dir/nested", "x", "#"); err == nil {
		t.Fatal("path conflict with document must fail")
	}
}

func TestMarkdownUploadWritesDraftOnly(t *testing.T) {
	svc := newProjectService(t)
	owner := actor(1, "project_projects:create", "project_docs:create")
	project := createProject(t, svc, owner, "markdown-draft")

	node, err := svc.UploadMarkdown(
		owner,
		project.ID,
		nil,
		"guide.md",
		"text/markdown",
		bytes.NewReader([]byte("# Draft guide")),
		int64(len("# Draft guide")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if node.PublishedContent != "" || node.DraftContent != "# Draft guide" || node.DraftUpdatedAt == nil {
		t.Fatalf("Markdown upload must create only a draft: %#v", node)
	}
}

func TestUploadLimitsAndZIPSafety(t *testing.T) {
	svc := newProjectService(t)
	if _, err := svc.storage.Put(storagemodel.KindAttachment, "text/plain", bytes.NewReader([]byte("x")),
		storageservice.DefaultAttachmentMaxBytes+1, 1); !errors.Is(err, storageservice.ErrTooLarge) {
		t.Fatalf("attachment oversize = %v", err)
	}

	owner := actor(1, "project_projects:create", "project_docs:create")
	project := createProject(t, svc, owner, "zip")
	payload := makeZIP(t, map[string]string{"../outside.md": "escape"})
	if _, err := svc.ImportZIP(owner, project.ID, nil, "unsafe.zip", "application/zip", bytes.NewReader(payload), int64(len(payload))); err == nil {
		t.Fatal("ZIP traversal must be rejected")
	}

	bomb := makeZIPBomb(t)
	if _, err := svc.ImportZIP(owner, project.ID, nil, "bomb.zip", "application/zip", bytes.NewReader(bomb), int64(len(bomb))); err == nil {
		t.Fatal("ZIP with an excessive compression ratio must be rejected")
	}
}

func newProjectService(t *testing.T) *ProjectService {
	svc, _ := newProjectServiceWithDB(t)
	return svc
}

func newProjectServiceWithDB(t *testing.T) (*ProjectService, *gorm.DB) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: t.TempDir() + "/bedrock.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := migration.Up(context.Background(), gdb, migration.Driver("sqlite")); err != nil {
		t.Fatal(err)
	}
	storage, err := storageservice.NewStorageService(
		storagerepo.NewStorageRepository(gdb),
		t.TempDir(),
		storageservice.Limits{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return NewProjectService(projectrepo.NewProjectRepository(gdb), storage), gdb
}

func createProject(t *testing.T, svc *ProjectService, actor AccessContext, slug string) *projectmodel.ProductProject {
	t.Helper()
	project, err := svc.CreateProject(actor, CreateProjectInput{Name: slug, Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func actor(userID uint, permissions ...string) AccessContext {
	return NewAccessContext(userID, false, permissions)
}

func makeZIP(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZIPBomb(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("bomb.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(bytes.Repeat([]byte("A"), 128*1024)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
