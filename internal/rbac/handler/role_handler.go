package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	"bedrock/internal/rbac/service"
)

type RoleHandler struct {
	roles *service.RoleService
	perm  *service.PermissionService
}

func NewRoleHandler(roles *service.RoleService, perm *service.PermissionService) *RoleHandler {
	return &RoleHandler{roles: roles, perm: perm}
}

func (h *RoleHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/roles", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "system_roles:view"), h.List)
	g.GET("/permission-catalog", rbacmw.RequirePermission(h.perm, "system_roles:update"), h.PermissionCatalog)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "system_roles:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "system_roles:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "system_roles:update"), h.Update)
	g.PUT("/:id/permissions", rbacmw.RequirePermission(h.perm, "system_roles:update"), h.SetPermissions)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "system_roles:delete"), h.Delete)
}

func (h *RoleHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.roles.List(q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *RoleHandler) PermissionCatalog(c *gin.Context) {
	items, err := h.perm.PermissionCatalog()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *RoleHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	role, err := h.roles.Get(uint(id))
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "角色不存在")
		return
	}
	pkg.Success(c, role)
}

func (h *RoleHandler) Create(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Code        string   `json:"code" binding:"required"`
		Description string   `json:"description"`
		DataScope   string   `json:"data_scope"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	role, err := h.roles.Create(req.Name, req.Code, req.Description, req.DataScope, req.Permissions)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, role)
}

func (h *RoleHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		DataScope   string `json:"data_scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	role, err := h.roles.Update(uint(id), req.Name, req.Description, req.DataScope)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, role)
}

func (h *RoleHandler) SetPermissions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Permissions []string `json:"permissions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	role, err := h.roles.SetPermissions(uint(id), req.Permissions)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, role)
}

func (h *RoleHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.roles.Delete(uint(id)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, nil)
}
