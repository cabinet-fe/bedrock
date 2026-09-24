package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

type handlerTestEnv struct {
	router      *gin.Engine
	adminID     uint
	normalID    uint
	backupSvc   *service.BackupService
	backupRepo  *repository.BackupRepository
	userRepo    *authrepo.UserRepository
	permSvc     *rbacservice.PermissionService
	backupDir   string
	createdID   uint
	createdPath string
}

func setupHandlerTestEnv(t *testing.T) *handlerTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "handler_test.sqlite")
	configPath := filepath.Join(root, "config.yaml")
	storageDir := filepath.Join(root, "data", "storage")
	backupDir := filepath.Join(storageDir, "backups")

	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	_ = os.MkdirAll(storageDir, 0o755)
	_ = os.MkdirAll(backupDir, 0o755)

	if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			Path:   dbPath,
		},
		Storage: config.StorageConfig{
			Root: storageDir,
		},
	}

	gdb, err := db.Open(&cfg.Database)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close(gdb)
	})

	if err := migration.Up(context.Background(), gdb, "sqlite"); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if err := seed.EnsureRBACResources(gdb); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}

	userRepo := authrepo.NewUserRepository(gdb)
	roleRepo := rbacrepo.NewRoleRepository(gdb)
	resourceRepo := rbacrepo.NewResourceRepository(gdb)
	menuGroupRepo := rbacrepo.NewMenuGroupRepository(gdb)
	permSvc := rbacservice.NewPermissionService(roleRepo, resourceRepo, menuGroupRepo)

	// Create super admin
	pwdHash, _ := pkg.HashPassword("admin-secret")
	admin := &authmodel.User{
		Username:     "superadmin",
		PasswordHash: pwdHash,
		IsActive:     true,
		IsSuperAdmin: true,
	}
	if err := userRepo.Create(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	// Create normal user with a backup-viewer role (per-role authorization)
	normal := &authmodel.User{
		Username:     "normaluser",
		PasswordHash: pwdHash,
		IsActive:     true,
		IsSuperAdmin: false,
	}
	if err := userRepo.Create(normal); err != nil {
		t.Fatalf("create normal user: %v", err)
	}
	roleSvc := rbacservice.NewRoleService(roleRepo, resourceRepo)
	viewerRole, err := roleSvc.Create("备份查看", "backup_viewer", "", "", []string{"system_backup:view"})
	if err != nil {
		t.Fatalf("create backup viewer role: %v", err)
	}
	if err := roleSvc.SetUserRoles(normal.ID, []uint{viewerRole.ID}); err != nil {
		t.Fatal(err)
	}

	backupRepo := repository.NewBackupRepository(gdb)
	backupEngine := service.NewBackupEngine(service.BackupEngineOptions{
		DB:         gdb,
		Config:     cfg,
		AppVersion: "1.0.0-test",
		ConfigPath: configPath,
		BackupDir:  backupDir,
	})
	backupSvc := service.NewBackupService(backupRepo, backupEngine, userRepo)
	backupHandler := NewBackupHandler(backupSvc, permSvc)

	router := gin.New()
	api := router.Group("/api/v1")

	// Dynamic auth middleware based on Header "X-Test-User"
	authMW := func(c *gin.Context) {
		u := c.GetHeader("X-Test-User")
		if u == "admin" {
			c.Set("user_id", admin.ID)
			c.Set("username", admin.Username)
			c.Set("is_super_admin", true)
			c.Next()
			return
		}
		if u == "normal" {
			c.Set("user_id", normal.ID)
			c.Set("username", normal.Username)
			c.Set("is_super_admin", false)
			c.Next()
			return
		}
		pkg.Error(c, http.StatusUnauthorized, "未登录")
		c.Abort()
	}

	backupHandler.RegisterRoutes(api, authMW)

	return &handlerTestEnv{
		router:     router,
		adminID:    admin.ID,
		normalID:   normal.ID,
		backupSvc:  backupSvc,
		backupRepo: backupRepo,
		userRepo:   userRepo,
		permSvc:    permSvc,
		backupDir:  backupDir,
	}
}

func TestBackupHandler_CRUD_And_Permissions(t *testing.T) {
	env := setupHandlerTestEnv(t)

	// 1. Unauthenticated request -> 401
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/backups", nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated, got %d", rec.Code)
		}
	}

	// 2. Normal user passes RBAC now (permissions resolve system-wide; the
	// backup list route is not super_admin_only).
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/backups", nil)
		req.Header.Set("X-Test-User", "normal")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for normal user, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3. Create backup parameter validation: empty modules -> 400
	{
		body, _ := json.Marshal(model.SystemBackupCreateRequest{Modules: []string{}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/backups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty modules, got %d", rec.Code)
		}
	}

	// 4. Create backup success -> 201
	var backupID uint
	{
		body, _ := json.Marshal(model.SystemBackupCreateRequest{
			Modules: []string{model.BackupModuleConfig},
			Note:    "test handler creation",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/backups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Code int                `json:"code"`
			Data model.SystemBackup `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal create response: %v", err)
		}
		if resp.Data.ID == 0 || resp.Data.Status != model.BackupStatusSuccess {
			t.Fatalf("invalid created backup data: %+v", resp.Data)
		}
		backupID = resp.Data.ID
	}

	// 5. List backups -> 200 PageSuccess envelope
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/backups?page=1&page_size=10", nil)
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var pageResp struct {
			Code int `json:"code"`
			Data struct {
				Items []model.SystemBackup `json:"items"`
				Total int64                `json:"total"`
				Page  int                  `json:"page"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &pageResp); err != nil {
			t.Fatalf("unmarshal list response: %v", err)
		}
		if pageResp.Data.Total != 1 || len(pageResp.Data.Items) != 1 {
			t.Fatalf("unexpected list data: total=%d, items=%d", pageResp.Data.Total, len(pageResp.Data.Items))
		}
	}

	// 6. Download backup -> 200 streaming zip
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/backups/"+strconv.FormatUint(uint64(backupID), 10)+"/download", nil)
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("download returned %d: %s", rec.Code, rec.Body.String())
		}
		if cType := rec.Header().Get("Content-Type"); cType != "application/zip" {
			t.Fatalf("expected Content-Type application/zip, got %q", cType)
		}
		if cDisp := rec.Header().Get("Content-Disposition"); cDisp == "" {
			t.Fatalf("expected Content-Disposition header")
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("expected non-empty downloaded zip body")
		}
	}

	// 7. Inspect upload -> 200
	var uploadToken string
	{
		backupRec, _ := env.backupRepo.FindByID(backupID)
		zipData, _ := os.ReadFile(backupRec.FilePath)

		bodyBuf := &bytes.Buffer{}
		mw := multipart.NewWriter(bodyBuf)
		fw, err := mw.CreateFormFile("file", "uploaded_backup.zip")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(zipData); err != nil {
			t.Fatalf("write form file: %v", err)
		}
		mw.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/backups/inspect", bodyBuf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("inspect returned %d: %s", rec.Code, rec.Body.String())
		}

		var inspectResp struct {
			Code int                       `json:"code"`
			Data model.BackupInspectResult `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &inspectResp); err != nil {
			t.Fatalf("unmarshal inspect response: %v", err)
		}
		if !inspectResp.Data.Compatible || inspectResp.Data.UploadToken == "" {
			t.Fatalf("inspect incompatible or missing token: %+v", inspectResp.Data)
		}
		uploadToken = inspectResp.Data.UploadToken
	}

	// 8. Restore: wrong password -> 400
	{
		body, _ := json.Marshal(model.SystemBackupRestoreRequest{
			BackupID:      &backupID,
			AdminPassword: "wrong-password",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/backups/restore", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for wrong password, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 9. Restore: correct password via upload token -> 200
	{
		noSnap := false
		body, _ := json.Marshal(model.SystemBackupRestoreRequest{
			UploadToken:   uploadToken,
			AdminPassword: "admin-secret",
			AutoSnapshot:  &noSnap,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/backups/restore", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for restore, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 10. Delete backup -> 200
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/system/backups/"+strconv.FormatUint(uint64(backupID), 10), nil)
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("delete returned %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 11. Delete non-existent backup -> 404
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/system/backups/99999", nil)
		req.Header.Set("X-Test-User", "admin")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for delete non-existent, got %d", rec.Code)
		}
	}
}
