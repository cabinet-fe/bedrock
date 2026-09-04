package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/repository"
	"bedrock/internal/system/service"
)

type OperationLogHandler struct {
	audit *service.AuditService
	perm  *rbacservice.PermissionService
}

func NewOperationLogHandler(audit *service.AuditService, perm *rbacservice.PermissionService) *OperationLogHandler {
	return &OperationLogHandler{audit: audit, perm: perm}
}

func (h *OperationLogHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/operation-logs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "system_operation_logs:view"), h.List)
	g.DELETE("", rbacmw.RequirePermission(h.perm, "system_operation_logs:clear"), h.Clear)
}

func (h *OperationLogHandler) Clear(c *gin.Context) {
	if err := h.audit.Clear(); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "清空操作日志失败")
		return
	}
	pkg.Success(c, nil)
}

func (h *OperationLogHandler) List(c *gin.Context) {
	var f repository.OperationLogFilters
	q := pkg.BindList(c, &f)
	// from/to 使用日期字符串，非 RFC3339，需单独解析
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			f.From = &t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			end := t.Add(24*time.Hour - time.Nanosecond)
			f.To = &end
		}
	}
	items, total, err := h.audit.List(f)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}
