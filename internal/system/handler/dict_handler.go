package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	"bedrock/internal/system/model"
	"bedrock/internal/system/service"
)

type DictionaryHandler struct {
	dicts *service.DictionaryService
	perm  *rbacservice.PermissionService
}

func NewDictionaryHandler(dicts *service.DictionaryService, perm *rbacservice.PermissionService) *DictionaryHandler {
	return &DictionaryHandler{dicts: dicts, perm: perm}
}

func (h *DictionaryHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/dictionaries", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "system_dictionaries:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "system_dictionaries:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "system_dictionaries:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "system_dictionaries:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "system_dictionaries:delete"), h.Delete)
}

func (h *DictionaryHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.dicts.List(q)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *DictionaryHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	d, err := h.dicts.Get(uint(id))
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "字典不存在")
		return
	}
	pkg.Success(c, d)
}

func (h *DictionaryHandler) Create(c *gin.Context) {
	var req struct {
		Name        string           `json:"name" binding:"required"`
		Code        string           `json:"code" binding:"required"`
		Description string           `json:"description"`
		Items       []model.DictItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	d, err := h.dicts.Create(req.Name, req.Code, req.Description, req.Items)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, d)
}

func (h *DictionaryHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Items       *[]model.DictItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	d, err := h.dicts.Update(uint(id), req.Name, req.Description, req.Items)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, d)
}

func (h *DictionaryHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := h.dicts.Delete(uint(id)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, nil)
}
