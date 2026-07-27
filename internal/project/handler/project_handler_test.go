package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	aimodel "bedrock/internal/ai/model"
	airepository "bedrock/internal/ai/repository"
	aiservice "bedrock/internal/ai/service"
	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	resourcerepo "bedrock/internal/resource/repository"
	resourceservice "bedrock/internal/resource/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
)

func TestPublishDocNodeRequiresExpectedVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newProjectHandlerForTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := service.CreateProject(owner, projectservice.CreateProjectInput{Name: "Docs", Slug: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	draft := "draft"
	node, err := service.CreateDocNode(owner, project.ID, projectservice.DocNodeInput{
		Kind: "doc", Name: "guide.md", DraftContent: &draft,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := publishDocNodeRequest(t, handler, project.ID, node.ID, `{}`); got != http.StatusBadRequest {
		t.Fatalf("missing expected_version status = %d, want %d", got, http.StatusBadRequest)
	}
	if got := publishDocNodeRequest(t, handler, project.ID, node.ID, `{"expected_version":1}`); got != http.StatusConflict {
		t.Fatalf("stale expected_version status = %d, want %d", got, http.StatusConflict)
	}
}

func TestPushAndPublishPathPATScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newProjectHandlerForTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := service.CreateProject(owner, projectservice.CreateProjectInput{Name: "PAT Docs", Slug: "pat-docs"})
	if err != nil {
		t.Fatal(err)
	}
	projectID := strconv.FormatUint(uint64(project.ID), 10)

	pushBody := `{"api_dir":"a/b","api_doc_name":"Doc","api_doc":"# hi"}`
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", projectID, pushBody, true, nil); got != http.StatusForbidden {
		t.Fatalf("PAT without docs:write = %d, want 403", got)
	}
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", projectID, pushBody, true, []string{"skills:read"}); got != http.StatusForbidden {
		t.Fatalf("PAT wrong scope = %d, want 403", got)
	}
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", projectID, pushBody, true, []string{"docs:write"}); got != http.StatusCreated {
		t.Fatalf("PAT docs:write push = %d, want 201", got)
	}
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", projectID, pushBody, true, []string{"docs:write"}); got != http.StatusOK {
		t.Fatalf("PAT docs:write upsert = %d, want 200", got)
	}

	pubBody := `{"api_dir":"a/b","api_doc_name":"Doc"}`
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/publish-path", projectID, pubBody, true, []string{"docs:write"}); got != http.StatusForbidden {
		t.Fatalf("PAT without docs:publish = %d, want 403", got)
	}
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/publish-path", projectID, pubBody, true, []string{"docs:publish"}); got != http.StatusOK {
		t.Fatalf("PAT docs:publish = %d, want 200", got)
	}
}

func TestPushDocByPathAcceptsSlug(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newProjectHandlerForTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := service.CreateProject(owner, projectservice.CreateProjectInput{Name: "Slug Docs", Slug: "slug-docs"})
	if err != nil {
		t.Fatal(err)
	}

	pushBody := `{"api_dir":"svc","api_doc_name":"api","api_doc":"# slug push"}`
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", project.Slug, pushBody, true, []string{"docs:write"}); got != http.StatusCreated {
		t.Fatalf("slug push = %d, want 201", got)
	}
	if got := docsPathRequest(t, handler, http.MethodPost, "/docs/push", "missing-slug", pushBody, true, []string{"docs:write"}); got != http.StatusNotFound {
		t.Fatalf("missing slug = %d, want 404", got)
	}
}

func docsPathRequest(t *testing.T, handler *ProjectHandler, method, suffix, projectID, body string, isPAT bool, scopes []string) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/api/v1/projects/"+projectID+suffix, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: projectID}}
	c.Set("user_id", uint(1))
	c.Set("is_super_admin", !isPAT)
	c.Set("is_pat", isPAT)
	if scopes != nil {
		c.Set("pat_scopes", scopes)
	}
	switch suffix {
	case "/docs/push":
		handler.PushDocByPath(c)
	case "/docs/publish-path":
		handler.PublishDocByPath(c)
	default:
		t.Fatalf("unknown suffix %s", suffix)
	}
	return recorder.Code
}

func TestListRequirementStatusesAllowsLeastPrivilegeMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service, gdb := newProjectHandlerForTestWithDB(t)
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatal(err)
	}

	users := authrepo.NewUserRepository(gdb)
	roles := rbacrepo.NewRoleRepository(gdb)
	resources := rbacrepo.NewResourceRepository(gdb)
	roleService := rbacservice.NewRoleService(roles, resources)
	member := &authmodel.User{Username: "requirement_reader", PasswordHash: "hash", IsActive: true}
	if err := users.Create(member); err != nil {
		t.Fatal(err)
	}
	role, err := roleService.Create("需求只读", "requirement_reader", "", "", []string{"project_requirements:view"})
	if err != nil {
		t.Fatal(err)
	}
	if err := roleService.SetUserRoles(member.ID, []uint{role.ID}); err != nil {
		t.Fatal(err)
	}

	owner := projectservice.NewAccessContext(999, true, nil)
	project, err := service.CreateProject(owner, projectservice.CreateProjectInput{Name: "Requirements", Slug: "requirements"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMember(owner, project.ID, projectservice.MemberInput{
		UserID: member.ID,
		Role:   "readonly",
	}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/projects/meta/requirement-statuses", nil)
	c.Set("user_id", member.ID)
	c.Set("is_super_admin", false)
	handler.ListRequirementStatuses(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"backlog"`)) {
		t.Fatalf("response must include baseline requirement statuses: %s", recorder.Body.String())
	}
}

func TestGenerateDocsUnwiredReturnsNotImplemented(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newProjectHandlerForTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := service.CreateProject(owner, projectservice.CreateProjectInput{Name: "Docs", Slug: "generate-docs"})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/docs/generate",
		bytes.NewBufferString(`{"agent_id":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(project.ID), 10)}}
	c.Set("user_id", uint(1))
	c.Set("is_super_admin", true)
	handler.GenerateDocs(c)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGenerateDocsWiredReturnsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, projectSvc, gdb := newProjectHandlerForTestWithDB(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "Docs Wired", Slug: "generate-docs-wired"})
	if err != nil {
		t.Fatal(err)
	}

	aiRepo := airepository.NewAIRepository(gdb)
	cli := resourceservice.NewCLIService(resourcerepo.NewCLIRepository(gdb))
	agents := aiservice.NewAgentService(aiRepo, cli, nil, nil, zap.NewNop(), t.TempDir(), t.TempDir())
	agents.Start()
	t.Cleanup(agents.Shutdown)
	agents.SetDocDraftWriter(projectSvc)
	projectSvc.SetDocsAIBridge(aiservice.NewDocsBridge(agents))

	agent, err := agents.CreateAgent(1, aiservice.AgentInput{
		Name: "docs", CliKey: "claude_code", SystemPrompt: "generate", TimeoutSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := agents.GetAgent(agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.WorkspaceStatus == aimodel.WorkspaceReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	body := fmt.Sprintf(`{"agent_id":%d}`, agent.ID)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/docs/generate", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(project.ID), 10)}}
	c.Set("user_id", uint(1))
	c.Set("is_super_admin", true)
	handler.GenerateDocs(c)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"agent_run_id"`)) {
		t.Fatalf("expected agent_run_id in response: %s", recorder.Body.String())
	}
}

func publishDocNodeRequest(t *testing.T, handler *ProjectHandler, projectID, nodeID uint, body string) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/projects/1/docs/1/publish",
		bytes.NewBufferString(body),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{
		{Key: "id", Value: "1"},
		{Key: "nodeID", Value: "1"},
	}
	c.Params[0].Value = strconv.FormatUint(uint64(projectID), 10)
	c.Params[1].Value = strconv.FormatUint(uint64(nodeID), 10)
	c.Set("user_id", uint(1))
	c.Set("is_super_admin", true)
	handler.PublishDocNode(c)
	return recorder.Code
}

func newProjectHandlerForTest(t *testing.T) (*ProjectHandler, *projectservice.ProjectService) {
	handler, service, _ := newProjectHandlerForTestWithDB(t)
	return handler, service
}

func newProjectHandlerForTestWithDB(t *testing.T) (*ProjectHandler, *projectservice.ProjectService, *gorm.DB) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: t.TempDir() + "/project-handler.sqlite"})
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
	service := projectservice.NewProjectService(projectrepo.NewProjectRepository(gdb), storage)
	permissions := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)
	return NewProjectHandler(service, permissions), service, gdb
}
