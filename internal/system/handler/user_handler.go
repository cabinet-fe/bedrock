package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/service"
)

type UserHandler struct {
	users *service.UserService
	perm  *rbacservice.PermissionService
}

func NewUserHandler(users *service.UserService, perm *rbacservice.PermissionService) *UserHandler {
	return &UserHandler{users: users, perm: perm}
}

func (h *UserHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/users", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "system_users:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "system_users:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "system_users:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "system_users:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "system_users:delete"), h.Delete)
}

func (h *UserHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.users.List(q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *UserHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	u, err := h.users.Get(uint(id))
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "用户不存在")
		return
	}
	pkg.Success(c, u)
}

func (h *UserHandler) Create(c *gin.Context) {
	var req service.CreateUserInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	u, err := h.users.Create(req)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, u)
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.UpdateUserInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	u, err := h.users.Update(uint(id), req)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, u)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.users.Delete(uint(id)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, nil)
}
