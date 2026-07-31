package handler

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
)

type ScriptRunHandler struct {
	svc  *service.ScriptRunService
	perm *rbacservice.PermissionService
}

func NewScriptRunHandler(svc *service.ScriptRunService, perm *rbacservice.PermissionService) *ScriptRunHandler {
	return &ScriptRunHandler{svc: svc, perm: perm}
}

func (h *ScriptRunHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/script-runs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_script_runs:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_script_runs:view"), h.Get)
	g.GET("/:id/log", rbacmw.RequirePermission(h.perm, "cicd_script_runs:view"), h.Log)
	g.POST("/:id/cancel", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:execute"), h.Cancel)
	g.POST("/:id/retry", rbacmw.RequirePermission(h.perm, "cicd_script_jobs:execute"), h.Retry)
}

func (h *ScriptRunHandler) dataScope(c *gin.Context) (uint, string, bool) {
	userID := authmiddleware.GetUserID(c)
	scope, err := h.perm.ResolveDataScope(userID, authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return 0, "", false
	}
	return userID, scope, true
}

func (h *ScriptRunHandler) List(c *gin.Context) {
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	q := pkg.ParseListQuery(c)
	var jobID *uint
	if v := c.Query("script_job_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			u := uint(id)
			jobID = &u
		}
	}
	items, total, err := h.svc.List(q, jobID, c.Query("status"), userID, scope)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *ScriptRunHandler) Get(c *gin.Context) {
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

func (h *ScriptRunHandler) Cancel(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.Cancel(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *ScriptRunHandler) Retry(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	item, err := h.svc.Retry(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: item})
}

func (h *ScriptRunHandler) Log(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	userID, scope, ok := h.dataScope(c)
	if !ok {
		return
	}
	path, err := h.svc.LogPath(id, userID, scope)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "日志文件不存在")
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
}
