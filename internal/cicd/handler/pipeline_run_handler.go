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

type PipelineRunHandler struct {
	orchestrator *service.PipelineOrchestrator
	perm         *rbacservice.PermissionService
}

func NewPipelineRunHandler(orchestrator *service.PipelineOrchestrator, perm *rbacservice.PermissionService) *PipelineRunHandler {
	return &PipelineRunHandler{orchestrator: orchestrator, perm: perm}
}

func (h *PipelineRunHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/pipeline-runs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_pipeline_runs:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_pipeline_runs:view"), h.Get)
	g.POST("/:id/cancel", rbacmw.RequirePermission(h.perm, "cicd_pipelines:execute"), h.Cancel)
}

func (h *PipelineRunHandler) dataScope(c *gin.Context) (uint, string, bool) {
	userID := authmiddleware.GetUserID(c)
	scope, err := h.perm.ResolveDataScope(userID, authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return 0, "", false
	}
	return userID, scope, true
}

func (h *PipelineRunHandler) List(c *gin.Context) {
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	q := pkg.ParseListQuery(c)
	var pipelineID *uint
	if v := c.Query("build_pipeline_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			u := uint(id)
			pipelineID = &u
		}
	}
	projectID, err := parseOptionalUintQuery(c, "project_id")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 project_id")
		return
	}
	items, total, err := h.orchestrator.List(q, pipelineID, c.Query("status"), projectID, userID, scope)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *PipelineRunHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.orchestrator.Get(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *PipelineRunHandler) Cancel(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.orchestrator.Cancel(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}
