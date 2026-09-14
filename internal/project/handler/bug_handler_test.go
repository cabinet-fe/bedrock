package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authmodel "bedrock/internal/auth/model"
	authrepo "bedrock/internal/auth/repository"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/platform/seed"
	projectmodel "bedrock/internal/project/model"
	projectrepo "bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbacmodel "bedrock/internal/rbac/model"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	storagerepo "bedrock/internal/storage/repository"
	storageservice "bedrock/internal/storage/service"
)

func setupBugHandlerTest(t *testing.T) (*gin.Engine, *BugHandler, *ProjectHandler, *projectservice.BugService, *projectservice.ProjectService, *gorm.DB) {
	t.Helper()
	gdb, err := db.Open(&config.DatabaseConfig{Driver: "sqlite", Path: t.TempDir() + "/bug-handler.sqlite"})
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
	if err := seed.EnsureRBACResources(gdb); err != nil {
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

	projectRepo := projectrepo.NewProjectRepository(gdb)
	bugRepo := projectrepo.NewBugRepository(gdb)
	roleRepo := rbacrepo.NewRoleRepository(gdb)
	permSvc := rbacservice.NewPermissionService(
		roleRepo,
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)

	projectSvc := projectservice.NewProjectService(projectRepo, storage)
	bugSvc := projectservice.NewBugService(bugRepo, projectRepo, storage)

	bugHandler := NewBugHandler(bugSvc, permSvc)
	projectHandler := NewProjectHandler(projectSvc, permSvc)
	projectHandler.SetBugHandler(bugHandler)

	userRepo := authrepo.NewUserRepository(gdb)
	for i := 1; i <= 5; i++ {
		_ = userRepo.Create(&authmodel.User{
			Username:     "user" + strconv.Itoa(i),
			DisplayName:  "User " + strconv.Itoa(i),
			PasswordHash: "hashed",
			IsActive:     true,
		})
	}

	// Create a role with all project & bug permissions
	devRole := &rbacmodel.Role{
		Name:      "Developer",
		Code:      "dev",
		DataScope: rbacmodel.DataScopeSelf,
	}
	if err := roleRepo.Create(devRole); err != nil {
		t.Fatal(err)
	}
	if err := roleRepo.ReplacePermissions(devRole.ID, []string{
		"project_projects:view", "project_projects:create", "project_projects:update", "project_projects:delete",
		"project_bugs:view", "project_bugs:create", "project_bugs:update", "project_bugs:delete",
	}); err != nil {
		t.Fatal(err)
	}

	// Assign devRole to User 1, 2, 3
	for uid := uint(1); uid <= 3; uid++ {
		if err := roleRepo.ReplaceUserRoles(uid, []uint{devRole.ID}); err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	authMW := func(c *gin.Context) {
		uidStr := c.GetHeader("X-User-ID")
		if uidStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "unauthorized"})
			return
		}
		uid, _ := strconv.ParseUint(uidStr, 10, 64)
		c.Set("user_id", uint(uid))
		c.Set("is_super_admin", c.GetHeader("X-Super-Admin") == "true")
		c.Next()
	}

	api := r.Group("/api/v1")
	projectHandler.RegisterRoutes(api, authMW)

	return r, bugHandler, projectHandler, bugSvc, projectSvc, gdb
}

func doRequest(r *gin.Engine, method, url string, body []byte, userID uint, isSuper bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", strconv.FormatUint(uint64(userID), 10))
	if isSuper {
		req.Header.Set("X-Super-Admin", "true")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBugHandlerHTTPFlow(t *testing.T) {
	router, _, _, _, projectSvc, _ := setupBugHandlerTest(t)

	// Create project with owner User 1
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{
		Name: "HTTP Bug Project",
		Slug: "http-bug-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Add User 2 as project member
	if _, err := projectSvc.AddMember(owner, project.ID, projectservice.MemberInput{
		UserID: 2,
		Role:   projectmodel.ProjectRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	// Add User 3 as readonly member
	if _, err := projectSvc.AddMember(owner, project.ID, projectservice.MemberInput{
		UserID: 3,
		Role:   projectmodel.ProjectRoleReadonly,
	}); err != nil {
		t.Fatal(err)
	}

	projIDStr := strconv.FormatUint(uint64(project.ID), 10)

	// 1. Create bug with invalid inputs (400)
	resp := doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", []byte(`{"title":""}`), 2, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("empty title expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	resp = doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", []byte(`{"title":"valid","severity":"bogus"}`), 2, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("bogus severity expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	// 2. Readonly member cannot create bug (403)
	resp = doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", []byte(`{"title":"Readonly Bug"}`), 3, false)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("readonly create bug expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	// 3. Member creates bug (201)
	createJSON := `{"title":"UI Glitch","description":"Button misalignment","severity":"high","priority":"urgent","branch":"feat/ui"}`
	resp = doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", []byte(createJSON), 2, false)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create bug expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var createResp struct {
		Code int                     `json:"code"`
		Data projectmodel.ProjectBug `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	bugID := createResp.Data.ID
	if bugID == 0 || createResp.Data.Status != projectmodel.BugStatusOpen {
		t.Fatalf("unexpected bug returned: %+v", createResp.Data)
	}
	bugIDStr := strconv.FormatUint(uint64(bugID), 10)

	// 4. Get bug details (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("get bug expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 5. Non-member access get bug (403)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, nil, 4, false)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("non-member get bug expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	// 6. Update bug (200)
	updateJSON := `{"title":"UI Glitch Fixed Soon","severity":"low"}`
	resp = doRequest(router, http.MethodPut, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, []byte(updateJSON), 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("update bug expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 7. Transition status (200)
	statusJSON := `{"status":"in_progress","comment":"Working on it"}`
	resp = doRequest(router, http.MethodPut, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/status", []byte(statusJSON), 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("status transition expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 8. Invalid status transition (400)
	invalidStatusJSON := `{"status":"not_a_valid_status"}`
	resp = doRequest(router, http.MethodPut, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/status", []byte(invalidStatusJSON), 2, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid status transition expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	// 9. List activities (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/activities", nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("list activities expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var actResp struct {
		Code int                               `json:"code"`
		Data []projectmodel.ProjectBugActivity `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &actResp); err != nil {
		t.Fatal(err)
	}
	if len(actResp.Data) < 2 {
		t.Fatalf("expected at least 2 activities, got %d", len(actResp.Data))
	}

	// 10. List project bugs (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs?page=1&page_size=10", nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("list project bugs expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 11. List across projects (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/bugs?page=1&page_size=10", nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("list across projects expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 12. Delete bug: regular member cannot delete (403)
	resp = doRequest(router, http.MethodDelete, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, nil, 2, false)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("regular member delete bug expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	// 13. Delete bug: project owner can delete (200)
	resp = doRequest(router, http.MethodDelete, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, nil, 1, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("owner delete bug expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 14. Subsequent get returns 404
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr, nil, 1, false)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("deleted bug expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestBugHandlerComments(t *testing.T) {
	router, _, projectHandler, _, projectSvc, _ := setupBugHandlerTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	proj, _ := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "Comment Proj", Slug: "comment-proj"})
	projIDStr := strconv.Itoa(int(proj.ID))
	_ = projectHandler

	// Add member 2
	_, _ = projectSvc.AddMember(owner, proj.ID, projectservice.MemberInput{UserID: 2, Role: projectmodel.ProjectRoleMember})

	// Create a bug
	resp := doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", jsonBytes(map[string]any{
		"title": "Bug for Comment Test",
	}), 1, false)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create bug failed: %s", resp.Body.String())
	}
	var createBugResp struct {
		Data projectmodel.ProjectBug `json:"data"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &createBugResp)
	bugIDStr := strconv.Itoa(int(createBugResp.Data.ID))

	// 1. Create comment (201)
	resp = doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/comments", jsonBytes(map[string]any{
		"content": "This is a test comment",
	}), 2, false)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create comment expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	var commentResp struct {
		Data projectmodel.ProjectBugComment `json:"data"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &commentResp)
	commentIDStr := strconv.Itoa(int(commentResp.Data.ID))

	// 2. List comments (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/comments", nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("list comments expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var listResp struct {
		Data []projectmodel.ProjectBugComment `json:"data"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &listResp)
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(listResp.Data))
	}

	// 3. Update comment (200)
	resp = doRequest(router, http.MethodPut, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/comments/"+commentIDStr, jsonBytes(map[string]any{
		"content": "Updated comment content",
	}), 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("update comment expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 4. Delete comment (200)
	resp = doRequest(router, http.MethodDelete, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/comments/"+commentIDStr, nil, 2, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete comment expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestBugHandlerAttachments(t *testing.T) {
	router, _, _, _, projectSvc, _ := setupBugHandlerTest(t)
	owner := projectservice.NewAccessContext(1, true, nil)
	proj, _ := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{Name: "Attach Proj", Slug: "attach-proj"})
	projIDStr := strconv.Itoa(int(proj.ID))

	// Create a bug
	resp := doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", jsonBytes(map[string]any{
		"title": "Bug for Attachment Test",
	}), 1, false)
	var createBugResp struct {
		Data projectmodel.ProjectBug `json:"data"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &createBugResp)
	bugIDStr := strconv.Itoa(int(createBugResp.Data.ID))

	// 1. Upload attachment via multipart (201)
	bodyBuf := &bytes.Buffer{}
	mw := multipart.NewWriter(bodyBuf)
	fw, err := mw.CreateFormFile("file", "test_log.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("panic: runtime error\n"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments", bodyBuf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User-ID", "1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload attachment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var attResp struct {
		Data projectmodel.ProjectBugAttachment `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &attResp)
	attIDStr := strconv.Itoa(int(attResp.Data.ID))

	// 2. List attachments (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments", nil, 1, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("list attachments expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// 3. Download attachment (200)
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments/"+attIDStr+"/download", nil, 1, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("download attachment expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != "panic: runtime error\n" {
		t.Fatalf("unexpected downloaded content: %s", resp.Body.String())
	}

	// 4. Delete attachment (200)
	resp = doRequest(router, http.MethodDelete, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments/"+attIDStr, nil, 1, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete attachment expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}
