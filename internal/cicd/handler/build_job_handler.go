package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
)

type BuildJobHandler struct {
	svc  *service.BuildJobService
	runs *service.BuildRunService
	perm *rbacservice.PermissionService
}

func NewBuildJobHandler(svc *service.BuildJobService, runs *service.BuildRunService, perm *rbacservice.PermissionService) *BuildJobHandler {
	return &BuildJobHandler{svc: svc, runs: runs, perm: perm}
}

func (h *BuildJobHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/build-jobs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:delete"), h.Delete)
	g.GET("/:id/webhook-secret", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:view"), h.GetWebhookSecret)
	g.POST("/:id/webhook-secret/rotate", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:update"), h.RotateWebhookSecret)
	// Execute: only cicd_build_jobs:execute required (not credentials:use) — DESIGN §4.5 / Wave 4 engine.
	g.POST("/:id/runs", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:execute"), h.EnqueueRun)
}

func (h *BuildJobHandler) dataScope(c *gin.Context) (uint, string, bool) {
	userID := authmiddleware.GetUserID(c)
	scope, err := h.perm.ResolveDataScope(userID, authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return 0, "", false
	}
	return userID, scope, true
}

func (h *BuildJobHandler) List(c *gin.Context) {
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	q := pkg.ParseListQuery(c)
	var repoID *uint
	if v := c.Query("repository_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			u := uint(id)
			repoID = &u
		}
	}
	projectID, err := parseOptionalUintQuery(c, "project_id")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 project_id")
		return
	}
	items, total, err := h.svc.List(q, repoID, c.Query("keyword"), c.Query("tag"), projectID, userID, scope)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *BuildJobHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.Get(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *BuildJobHandler) Create(c *gin.Context) {
	var req service.CreateBuildJobInput
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

func (h *BuildJobHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	var req service.UpdateBuildJobInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	item, err := h.svc.Update(id, userID, scope, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *BuildJobHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(id, userID, scope); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, nil)
}

func (h *BuildJobHandler) EnqueueRun(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	var req service.EnqueueRunInput
	_ = c.ShouldBindJSON(&req)
	run, err := h.runs.Enqueue(id, userID, scope, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: run})
}

func (h *BuildJobHandler) GetWebhookSecret(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.GetWithSecret(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{
		"webhook_secret": item.WebhookSecret,
		"webhook_url":    "/api/v1/webhook/jobs/" + strconv.FormatUint(uint64(item.ID), 10) + "/" + item.WebhookSecret,
	})
}

func (h *BuildJobHandler) RotateWebhookSecret(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.RotateWebhookSecret(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{
		"webhook_secret": item.WebhookSecret,
		"webhook_url":    "/api/v1/webhook/jobs/" + strconv.FormatUint(uint64(item.ID), 10) + "/" + item.WebhookSecret,
	})
}
