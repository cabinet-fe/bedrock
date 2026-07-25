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

type RepositoryHandler struct {
	svc  *service.RepositoryService
	perm *rbacservice.PermissionService
}

func NewRepositoryHandler(svc *service.RepositoryService, perm *rbacservice.PermissionService) *RepositoryHandler {
	return &RepositoryHandler{svc: svc, perm: perm}
}

func (h *RepositoryHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/resource/repositories", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "resource_repositories:view"), h.List)
	g.POST("", rbacmw.RequirePermission(h.perm, "resource_repositories:create"), h.Create)
	g.POST("/sync-branches", rbacmw.RequirePermission(h.perm, "resource_repositories:update"), h.SyncBranchesBatch)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "resource_repositories:view"), h.Get)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "resource_repositories:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "resource_repositories:delete"), h.Delete)
	g.GET("/:id/branches", rbacmw.RequirePermission(h.perm, "resource_repositories:view"), h.Branches)
	g.POST("/:id/sync-branches", rbacmw.RequirePermission(h.perm, "resource_repositories:update"), h.SyncBranches)
	g.POST("/:id/test", rbacmw.RequirePermission(h.perm, "resource_repositories:view"), h.Test)
}

func (h *RepositoryHandler) canUseCredential(c *gin.Context) bool {
	userID := authmiddleware.GetUserID(c)
	isSuper := authmiddleware.IsSuperAdmin(c)
	return h.perm.CheckAccess(userID, isSuper, "resource_credentials:use") == nil
}

func (h *RepositoryHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	items, total, err := h.svc.List(q, c.Query("keyword"))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *RepositoryHandler) Get(c *gin.Context) {
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

func (h *RepositoryHandler) Create(c *gin.Context) {
	var req service.CreateRepositoryInput
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

func (h *RepositoryHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.UpdateRepositoryInput
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

func (h *RepositoryHandler) Delete(c *gin.Context) {
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

func (h *RepositoryHandler) Branches(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	items, syncedAt, err := h.svc.CachedBranches(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items, "synced_at": syncedAt})
}

func (h *RepositoryHandler) SyncBranches(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	repo, err := h.svc.SyncBranches(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{
		"items":     repo.Branches,
		"synced_at": repo.BranchesSyncedAt,
	})
}

type syncBranchesBatchRequest struct {
	IDs []uint `json:"ids"`
}

func (h *RepositoryHandler) SyncBranchesBatch(c *gin.Context) {
	var req syncBranchesBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	results := h.svc.SyncBranchesBatch(req.IDs)
	pkg.Success(c, gin.H{"items": results})
}

func (h *RepositoryHandler) Test(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	result, err := h.svc.TestFetch(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, result)
}
