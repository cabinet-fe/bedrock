package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/resource/service"
)

type CredentialHandler struct {
	svc  *service.CredentialService
	perm *rbacservice.PermissionService
}

func NewCredentialHandler(svc *service.CredentialService, perm *rbacservice.PermissionService) *CredentialHandler {
	return &CredentialHandler{svc: svc, perm: perm}
}

func (h *CredentialHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/resource/credentials", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "resource_credentials:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "resource_credentials:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "resource_credentials:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "resource_credentials:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "resource_credentials:delete"), h.Delete)
}

func (h *CredentialHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.svc.List(q, c.Query("keyword"))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *CredentialHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *CredentialHandler) Create(c *gin.Context) {
	var req service.CreateCredentialInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	item, err := h.svc.Create(authmiddleware.GetUserID(c), req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *CredentialHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.UpdateCredentialInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	item, err := h.svc.Update(id, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *CredentialHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.svc.Delete(id); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, nil)
}
