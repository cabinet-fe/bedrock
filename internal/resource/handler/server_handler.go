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

type ServerHandler struct {
	svc  *service.ServerService
	perm *rbacservice.PermissionService
}

func NewServerHandler(svc *service.ServerService, perm *rbacservice.PermissionService) *ServerHandler {
	return &ServerHandler{svc: svc, perm: perm}
}

func (h *ServerHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/resource/servers", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "resource_servers:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "resource_servers:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "resource_servers:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "resource_servers:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "resource_servers:delete"), h.Delete)
	g.POST("/:id/test", rbacmw.RequirePermission(h.perm, "resource_servers:view"), h.Test)
}

func (h *ServerHandler) canUseCredential(c *gin.Context) bool {
	return h.perm.CheckAccess(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), "resource_credentials:use") == nil
}

func (h *ServerHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.svc.List(q, c.Query("keyword"), c.Query("tag"))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *ServerHandler) Get(c *gin.Context) {
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

func (h *ServerHandler) Create(c *gin.Context) {
	var req service.CreateServerInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	item, err := h.svc.Create(authmiddleware.GetUserID(c), req, h.canUseCredential(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *ServerHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.UpdateServerInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	item, err := h.svc.Update(id, req, h.canUseCredential(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *ServerHandler) Delete(c *gin.Context) {
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

func (h *ServerHandler) Test(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	output, err := h.svc.TestConnection(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"ok": true, "output": output})
}
