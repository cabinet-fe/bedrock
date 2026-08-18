package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/service"
)

type CLIHandler struct {
	svc  *service.CLIService
	perm *rbacservice.PermissionService
}

func NewCLIHandler(svc *service.CLIService, perm *rbacservice.PermissionService) *CLIHandler {
	return &CLIHandler{svc: svc, perm: perm}
}

func (h *CLIHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	clis := rg.Group("/resource/clis", authMW)
	clis.GET("", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.List)
	clis.GET("/:key/versions", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.ListVersions)
	clis.POST("/:key/detect", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.Detect)
	clis.POST("/:key/check-update", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.CheckUpdate)
	clis.POST("/:key/install", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.Install)
	clis.POST("/:key/upgrade", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.Upgrade)
	clis.POST("/:key/uninstall", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.Uninstall)

	sources := rg.Group("/resource/cli-sources", authMW)
	sources.GET("", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.ListSources)
	sources.POST("", rbacmw.RequirePermission(h.perm, "ops_dev_environments:create"), h.CreateSource)
	sources.PUT("/:id", rbacmw.RequirePermission(h.perm, "ops_dev_environments:update"), h.UpdateSource)
	sources.DELETE("/:id", rbacmw.RequirePermission(h.perm, "ops_dev_environments:delete"), h.DeleteSource)
}

func (h *CLIHandler) List(c *gin.Context) {
	items, err := h.svc.ListCLIs()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询 CLI 失败")
		return
	}
	pkg.Success(c, gin.H{"items": items, "risk_notice": model.RiskNoticeSameUID})
}

func (h *CLIHandler) Detect(c *gin.Context) {
	result, err := h.svc.Detect(c.Param("key"))
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *CLIHandler) CheckUpdate(c *gin.Context) {
	result, err := h.svc.CheckUpdate(c.Request.Context(), c.Param("key"))
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *CLIHandler) ListVersions(c *gin.Context) {
	result, err := h.svc.ListVersions(c.Request.Context(), c.Param("key"))
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *CLIHandler) Install(c *gin.Context) {
	h.execute(c, "install")
}

func (h *CLIHandler) Upgrade(c *gin.Context) {
	h.execute(c, "upgrade")
}

func (h *CLIHandler) Uninstall(c *gin.Context) {
	h.execute(c, "uninstall")
}

func (h *CLIHandler) execute(c *gin.Context, op string) {
	var input service.ExecuteInput
	_ = c.ShouldBindJSON(&input)
	result, err := h.svc.Execute(c.Request.Context(), c.Param("key"), op, input, authmiddleware.GetUserID(c))
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *CLIHandler) ListSources(c *gin.Context) {
	items, err := h.svc.ListSources(c.Query("cli_key"))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *CLIHandler) CreateSource(c *gin.Context) {
	var input service.SourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.svc.CreateSource(input)
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *CLIHandler) UpdateSource(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var input service.SourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.svc.UpdateSource(id, input)
	if err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *CLIHandler) DeleteSource(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.svc.DeleteSource(id); err != nil {
		writeCLIError(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func writeCLIError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pkg.Error(c, http.StatusNotFound, "资源不存在")
		return
	}
	pkg.Error(c, http.StatusBadRequest, err.Error())
}
