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
	"bedrock/internal/resource/service"
)

// TokenHandler exposes personal access tokens (user_id-scoped: list/create/update/reveal/delete self only).
type TokenHandler struct {
	svc  *service.PATService
	perm *rbacservice.PermissionService
}

func NewTokenHandler(svc *service.PATService, perm *rbacservice.PermissionService) *TokenHandler {
	return &TokenHandler{svc: svc, perm: perm}
}

func (h *TokenHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/resource/tokens", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "resource_tokens:view"), h.List)
	g.POST("", rbacmw.RequirePermission(h.perm, "resource_tokens:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "resource_tokens:update"), h.Update)
	g.GET("/:id/reveal", rbacmw.RequirePermission(h.perm, "resource_tokens:view"), h.Reveal)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "resource_tokens:delete"), h.Delete)
}

func (h *TokenHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.svc.List(authmiddleware.GetUserID(c), q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *TokenHandler) Create(c *gin.Context) {
	var input service.CreatePATInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	result, err := h.svc.Create(authmiddleware.GetUserID(c), input)
	if err != nil {
		writeTokenError(c, err)
		return
	}
	pkg.Created(c, result)
}

func (h *TokenHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var input service.UpdatePATInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效请求")
		return
	}
	item, err := h.svc.Update(authmiddleware.GetUserID(c), id, input)
	if err != nil {
		writeTokenError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *TokenHandler) Reveal(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	result, err := h.svc.Reveal(authmiddleware.GetUserID(c), id)
	if err != nil {
		writeTokenError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *TokenHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.svc.Delete(authmiddleware.GetUserID(c), id); err != nil {
		writeTokenError(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func writeTokenError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pkg.Error(c, http.StatusNotFound, "资源不存在")
		return
	}
	if errors.Is(err, service.ErrPATNotCopyable) {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if errors.Is(err, service.ErrPATInvalid) {
		pkg.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	if errors.Is(err, service.ErrPATWrongScope) || errors.Is(err, service.ErrPATBadScope) {
		pkg.Error(c, http.StatusForbidden, err.Error())
		return
	}
	pkg.Error(c, http.StatusBadRequest, err.Error())
}
