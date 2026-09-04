package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	rbacrepo "bedrock/internal/rbac/repository"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

func TestOperationLogHandlerClear(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "oplog_test.sqlite"),
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

	logRepo := repository.NewOperationLogRepository(gdb)
	auditSvc := service.NewAuditService(logRepo)
	permSvc := rbacservice.NewPermissionService(
		rbacrepo.NewRoleRepository(gdb),
		rbacrepo.NewResourceRepository(gdb),
		rbacrepo.NewMenuGroupRepository(gdb),
	)

	// Insert test logs
	for i := 1; i <= 3; i++ {
		if err := logRepo.Create(&model.OperationLog{
			UserID:       uint(i),
			Username:     "tester",
			Action:       "POST",
			ResourceType: "/api/test",
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	h := NewOperationLogHandler(auditSvc, permSvc)
	router := gin.New()
	api := router.Group("/api/v1")
	// Auth middleware mockup: superadmin
	authMW := func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("is_super_admin", true)
		c.Next()
	}
	h.RegisterRoutes(api, authMW)

	// DELETE /api/v1/operation-logs
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/operation-logs", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /operation-logs returned status %d: %s", rec.Code, rec.Body.String())
	}

	// Verify database is empty
	var count int64
	if err := gdb.Model(&model.OperationLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 logs after clear, got %d", count)
	}
}
