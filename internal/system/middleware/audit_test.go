package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	"bedrock/internal/platform/db"
	"bedrock/internal/platform/migration"
	_ "bedrock/internal/platform/migration/migrations"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

func TestAuditWriteUsesPIDAsProcessTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := db.Open(&config.DatabaseConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "audit.sqlite"),
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
	audit := service.NewAuditService(repository.NewOperationLogRepository(gdb))
	router := gin.New()
	router.Use(AuditWrite(audit))
	router.POST("/api/v1/ops/processes/:pid/kill", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ops/processes/4242/kill", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("response status = %d", recorder.Code)
	}
	events, total, err := audit.List(repository.OperationLogFilters{ListQuery: pkg.ListQuery{Page: 1, PageSize: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("audit event count = %d", total)
	}
	if events[0].ResourceID != "4242" {
		t.Fatalf("process audit target = %q, want PID", events[0].ResourceID)
	}
	if events[0].Details != "POST /api/v1/ops/processes/:pid/kill resource_id=4242" {
		t.Fatalf("audit details = %q", events[0].Details)
	}
}
