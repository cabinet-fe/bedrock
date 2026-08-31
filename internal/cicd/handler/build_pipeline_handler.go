package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	resourcemodel "bedrock/internal/resource/model"
)

type BuildPipelineHandler struct {
	svc          *service.BuildPipelineService
	orchestrator *service.PipelineOrchestrator
	perm         *rbacservice.PermissionService
}

func NewBuildPipelineHandler(
	svc *service.BuildPipelineService,
	orchestrator *service.PipelineOrchestrator,
	perm *rbacservice.PermissionService,
) *BuildPipelineHandler {
	return &BuildPipelineHandler{svc: svc, orchestrator: orchestrator, perm: perm}
}

func (h *BuildPipelineHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/build-pipelines", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_pipelines:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_pipelines:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "cicd_pipelines:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "cicd_pipelines:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "cicd_pipelines:delete"), h.Delete)
	g.GET("/:id/webhook-secret", rbacmw.RequirePermission(h.perm, "cicd_pipelines:view"), h.GetWebhookSecret)
	g.POST("/:id/webhook-secret/rotate", rbacmw.RequirePermission(h.perm, "cicd_pipelines:update"), h.RotateWebhookSecret)
	g.POST("/:id/runs", rbacmw.RequirePermissionOrPATScope(h.perm, "cicd_pipelines:execute", resourcemodel.ScopePipelinesRun), h.EnqueueRun)
}

func (h *BuildPipelineHandler) dataScope(c *gin.Context) (uint, string, bool) {
	userID := authmiddleware.GetUserID(c)
	scope, err := h.perm.ResolveDataScope(userID, authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return 0, "", false
	}
	return userID, scope, true
}

func (h *BuildPipelineHandler) List(c *gin.Context) {
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	q := pkg.ParseListQuery(c)
	projectID, err := parseOptionalUintQuery(c, "project_id")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 project_id")
		return
	}
	items, total, err := h.svc.List(q, c.Query("keyword"), projectID, userID, scope)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *BuildPipelineHandler) Get(c *gin.Context) {
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

func (h *BuildPipelineHandler) Create(c *gin.Context) {
	var req service.CreateBuildPipelineInput
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

func (h *BuildPipelineHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.UpdateBuildPipelineInput
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "参数错误")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.Update(id, userID, scope, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *BuildPipelineHandler) Delete(c *gin.Context) {
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

func (h *BuildPipelineHandler) GetWebhookSecret(c *gin.Context) {
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
		"webhook_url":    fmt.Sprintf("/api/v1/webhook/pipelines/%d/%s", item.ID, item.WebhookSecret),
	})
}

func (h *BuildPipelineHandler) RotateWebhookSecret(c *gin.Context) {
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
		"webhook_url":    fmt.Sprintf("/api/v1/webhook/pipelines/%d/%s", item.ID, item.WebhookSecret),
	})
}

func (h *BuildPipelineHandler) EnqueueRun(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.EnqueuePipelineInput
	_ = c.ShouldBindJSON(&req)
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	run, err := h.orchestrator.Enqueue(id, userID, scope, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: run})
}
