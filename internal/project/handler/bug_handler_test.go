package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
		// Simulate a PAT request when X-PAT-Scopes is present (comma-separated).
		if scopes := c.GetHeader("X-PAT-Scopes"); scopes != "" {
			c.Set("is_pat", true)
			c.Set("pat_scopes", strings.Split(scopes, ","))
		} else {
			c.Set("is_pat", false)
		}
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

// doPATRequest simulates a PAT bearer request carrying the given scopes.
func doPATRequest(r *gin.Engine, method, url string, body []byte, userID uint, scopes string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", strconv.FormatUint(uint64(userID), 10))
	if scopes != "" {
		req.Header.Set("X-PAT-Scopes", scopes)
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
		t.Fatalf("delete attachment expected 200, got %d: %s", resp.Code, rec.Body.String())
	}
}

func TestBugHandlerPATScopeAccess(t *testing.T) {
	router, _, _, _, projectSvc, _ := setupBugHandlerTest(t)

	// Super admin creates a project; user 4 (no RBAC role) joins as member.
	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{
		Name: "PAT Bug Project",
		Slug: "pat-bug-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectSvc.AddMember(owner, project.ID, projectservice.MemberInput{
		UserID: 4,
		Role:   projectmodel.ProjectRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	projIDStr := strconv.FormatUint(uint64(project.ID), 10)

	// Seed one bug + attachment as the JWT owner for read-path assertions.
	resp := doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", jsonBytes(map[string]any{
		"title": "Seed bug",
	}), 1, true)
	if resp.Code != http.StatusCreated {
		t.Fatalf("seed bug expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	var seedResp struct {
		Data projectmodel.ProjectBug `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &seedResp); err != nil {
		t.Fatal(err)
	}
	bugIDStr := strconv.FormatUint(uint64(seedResp.Data.ID), 10)

	bodyBuf := &bytes.Buffer{}
	mw := multipart.NewWriter(bodyBuf)
	fw, err := mw.CreateFormFile("file", "pat_log.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("pat attachment\n"))
	_ = mw.Close()
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments", bodyBuf)
	uploadReq.Header.Set("Content-Type", mw.FormDataContentType())
	uploadReq.Header.Set("X-User-ID", "1")
	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("seed attachment expected 201, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
	var attResp struct {
		Data projectmodel.ProjectBugAttachment `json:"data"`
	}
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &attResp)
	attIDStr := strconv.FormatUint(uint64(attResp.Data.ID), 10)

	readPaths := []string{
		"/api/v1/projects/bugs?page=1&page_size=10",
		"/api/v1/projects/" + projIDStr + "/bugs?page=1&page_size=10",
		"/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr,
		"/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/activities",
		"/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/comments",
		"/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/attachments",
		"/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/attachments/" + attIDStr + "/download",
	}

	// 1. PAT with bugs:read can read the whole bug domain.
	for _, path := range readPaths {
		resp := doPATRequest(router, http.MethodGet, path, nil, 4, "bugs:read")
		if resp.Code != http.StatusOK {
			t.Fatalf("PAT bugs:read GET %s expected 200, got %d: %s", path, resp.Code, resp.Body.String())
		}
	}

	// 2. PAT with bugs:read lists projects with the minimal id/name/slug shape.
	resp = doPATRequest(router, http.MethodGet, "/api/v1/projects?page=1&page_size=10", nil, 4, "bugs:read")
	if resp.Code != http.StatusOK {
		t.Fatalf("PAT bugs:read list projects expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, leaked := range []string{"my_role", "permissions", "description"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("PAT project list must not expose %q: %s", leaked, body)
		}
	}
	var projectPage struct {
		Data struct {
			Items []struct {
				ID   uint   `json:"id"`
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &projectPage); err != nil {
		t.Fatal(err)
	}
	if len(projectPage.Data.Items) != 1 || projectPage.Data.Items[0].Slug != "pat-bug-project" {
		t.Fatalf("PAT project list must contain the member project only, got %+v", projectPage.Data.Items)
	}

	// 3. PAT without bugs:read gets 403 on reads and project list.
	for _, path := range readPaths {
		resp := doPATRequest(router, http.MethodGet, path, nil, 4, "docs:read")
		if resp.Code != http.StatusForbidden {
			t.Fatalf("PAT wrong scope GET %s expected 403, got %d", path, resp.Code)
		}
	}
	resp = doPATRequest(router, http.MethodGet, "/api/v1/projects?page=1&page_size=10", nil, 4, "docs:read")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("PAT wrong scope list projects expected 403, got %d", resp.Code)
	}

	// 4. PAT with bugs:read cannot write.
	writeChecks := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/v1/projects/" + projIDStr + "/bugs", jsonBytes(map[string]any{"title": "PAT bug"})},
		{http.MethodPut, "/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/status", jsonBytes(map[string]any{"status": "in_progress"})},
		{http.MethodPost, "/api/v1/projects/" + projIDStr + "/bugs/" + bugIDStr + "/comments", jsonBytes(map[string]any{"content": "pat comment"})},
	}
	for _, check := range writeChecks {
		resp := doPATRequest(router, check.method, check.path, check.body, 4, "bugs:read")
		if resp.Code != http.StatusForbidden {
			t.Fatalf("PAT bugs:read %s %s expected 403, got %d: %s", check.method, check.path, resp.Code, resp.Body.String())
		}
	}

	// 5. PAT with bugs:write can create, transition, and comment.
	for _, check := range writeChecks {
		resp := doPATRequest(router, check.method, check.path, check.body, 4, "bugs:write")
		want := http.StatusOK
		if check.method == http.MethodPost {
			want = http.StatusCreated
		}
		if resp.Code != want {
			t.Fatalf("PAT bugs:write %s %s expected %d, got %d: %s", check.method, check.path, want, resp.Code, resp.Body.String())
		}
	}

	// 6. PAT with bugs:write can upload an attachment.
	patBuf := &bytes.Buffer{}
	patMW := multipart.NewWriter(patBuf)
	patFW, err := patMW.CreateFormFile("file", "screenshot.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = patFW.Write([]byte("png bytes"))
	_ = patMW.Close()
	patReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs/"+bugIDStr+"/attachments", patBuf)
	patReq.Header.Set("Content-Type", patMW.FormDataContentType())
	patReq.Header.Set("X-User-ID", "4")
	patReq.Header.Set("X-PAT-Scopes", "bugs:write")
	patRec := httptest.NewRecorder()
	router.ServeHTTP(patRec, patReq)
	if patRec.Code != http.StatusCreated {
		t.Fatalf("PAT bugs:write upload expected 201, got %d: %s", patRec.Code, patRec.Body.String())
	}

	// 7. JWT behavior unchanged: member without RBAC bug permissions gets 403.
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs?page=1&page_size=10", nil, 4, false)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("JWT member without RBAC expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
	resp = doRequest(router, http.MethodGet, "/api/v1/projects?page=1&page_size=10", nil, 4, false)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("JWT member without project_projects:view expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestBugHandlerListFilters(t *testing.T) {
	router, _, _, _, projectSvc, _ := setupBugHandlerTest(t)

	owner := projectservice.NewAccessContext(1, true, nil)
	project, err := projectSvc.CreateProject(owner, projectservice.CreateProjectInput{
		Name: "Filter Bug Project",
		Slug: "filter-bug-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	projIDStr := strconv.FormatUint(uint64(project.ID), 10)

	createBug := func(title string, assigneeID uint) uint {
		resp := doRequest(router, http.MethodPost, "/api/v1/projects/"+projIDStr+"/bugs", jsonBytes(map[string]any{
			"title": title, "assignee_id": assigneeID,
		}), 1, true)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create bug %s expected 201, got %d: %s", title, resp.Code, resp.Body.String())
		}
		var createResp struct {
			Data projectmodel.ProjectBug `json:"data"`
		}
		_ = json.Unmarshal(resp.Body.Bytes(), &createResp)
		return createResp.Data.ID
	}
	bugOpen := createBug("open for user2", 2)
	bugClosed := createBug("closed for user2", 2)
	bugProgress := createBug("in progress for user3", 3)

	// Close one bug.
	resp := doRequest(router, http.MethodPut, "/api/v1/projects/"+projIDStr+"/bugs/"+strconv.FormatUint(uint64(bugClosed), 10)+"/status",
		jsonBytes(map[string]any{"status": "closed"}), 1, true)
	if resp.Code != http.StatusOK {
		t.Fatalf("close bug expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	_ = bugProgress

	listBugs := func(query string) []projectmodel.ProjectBug {
		resp := doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs?"+query, nil, 1, true)
		if resp.Code != http.StatusOK {
			t.Fatalf("list %q expected 200, got %d: %s", query, resp.Code, resp.Body.String())
		}
		var page struct {
			Data struct {
				Items []projectmodel.ProjectBug `json:"items"`
				Total int64                     `json:"total"`
			} `json:"data"`
		}
		_ = json.Unmarshal(resp.Body.Bytes(), &page)
		return page.Data.Items
	}

	// Filter by assignee username and by numeric user ID.
	for _, query := range []string{"assignee=user2", "assignee=2", "assignee_id=2"} {
		items := listBugs(query)
		if len(items) != 2 {
			t.Fatalf("%s expected 2 bugs, got %d", query, len(items))
		}
	}

	// Unclosed scope excludes closed bugs.
	if items := listBugs("assignee=user2&exclude_closed=true"); len(items) != 1 || items[0].ID != bugOpen {
		t.Fatalf("assignee=user2&exclude_closed=true expected only bug %d, got %+v", bugOpen, items)
	}
	if items := listBugs("exclude_closed=true"); len(items) != 2 {
		t.Fatalf("exclude_closed=true expected 2 bugs, got %d", len(items))
	}
	if items := listBugs(""); len(items) != 3 {
		t.Fatalf("no filter expected 3 bugs, got %d", len(items))
	}

	// project_id is only honored by the cross-project list; the
	// project-scoped endpoint ignores it (path id already fixes the project).
	if items := listBugs("project_id=999"); len(items) != 3 {
		t.Fatalf("project_id on project list expected to be ignored, got %d items", len(items))
	}
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/bugs?project_id=999&page=1&page_size=10", nil, 1, true)
	if resp.Code != http.StatusOK {
		t.Fatalf("across list expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var acrossPage struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &acrossPage)
	if acrossPage.Data.Total != 0 {
		t.Fatalf("across list project_id=999 expected 0 bugs, got %d", acrossPage.Data.Total)
	}

	// Unknown username resolves to 400.
	resp = doRequest(router, http.MethodGet, "/api/v1/projects/"+projIDStr+"/bugs?assignee=ghost", nil, 1, true)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unknown assignee expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}
