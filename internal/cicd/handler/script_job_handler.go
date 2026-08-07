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
)

type ScriptJobHandler struct {
	svc  *service.ScriptJobService
	runs *service.ScriptRunService
	perm *rbacservice.PermissionService
}

func NewScriptJobHandler(svc *service.ScriptJobService, runs *service.ScriptRunService, perm *rbacservice.PermissionService) *ScriptJobHandler {
	return &ScriptJobHandler{svc: svc, runs: runs, perm: perm}
}

func (h *ScriptJobHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/script-jobs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:view"), h.Get)
	g.POST("", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:create"), h.Create)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:update"), h.Update)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:delete"), h.Delete)
	g.GET("/:id/webhook-secret", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:view"), h.GetWebhookSecret)
	g.POST("/:id/webhook-secret/rotate", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:update"), h.RotateWebhookSecret)
	g.POST("/:id/runs", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:execute"), h.EnqueueRun)
}

func (h *ScriptJobHandler) dataScope(c *gin.Context) (uint, string, bool) {
	userID := authmiddleware.GetUserID(c)
	scope, err := h.perm.ResolveDataScope(userID, authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return 0, "", false
	}
	return userID, scope, true
}

func (h *ScriptJobHandler) List(c *gin.Context) {
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

func (h *ScriptJobHandler) Get(c *gin.Context) {
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

func (h *ScriptJobHandler) Create(c *gin.Context) {
	var req service.CreateScriptJobInput
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

func (h *ScriptJobHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	var req service.UpdateScriptJobInput
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

func (h *ScriptJobHandler) Delete(c *gin.Context) {
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

func (h *ScriptJobHandler) EnqueueRun(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.runs.Enqueue(id, userID, scope, "manual")
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: item})
}

func (h *ScriptJobHandler) GetWebhookSecret(c *gin.Context) {
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
		"webhook_url":    fmt.Sprintf("/api/v1/webhook/script-jobs/%d/%s", item.ID, item.WebhookSecret),
	})
}

func (h *ScriptJobHandler) RotateWebhookSecret(c *gin.Context) {
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
		"webhook_url":    fmt.Sprintf("/api/v1/webhook/script-jobs/%d/%s", item.ID, item.WebhookSecret),
	})
}
