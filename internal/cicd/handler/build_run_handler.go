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

type BuildRunHandler struct {
	svc  *service.BuildRunService
	perm *rbacservice.PermissionService
}

func NewBuildRunHandler(svc *service.BuildRunService, perm *rbacservice.PermissionService) *BuildRunHandler {
	return &BuildRunHandler{svc: svc, perm: perm}
}

func (h *BuildRunHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/build-runs", authMW)
	g.GET("", rbacmw.RequirePermission(h.perm, "cicd_build_runs:view"), h.List)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "cicd_build_runs:view"), h.Get)
	g.GET("/:id/log", rbacmw.RequirePermission(h.perm, "cicd_build_runs:view"), h.Log)
	g.GET("/:id/artifact", rbacmw.RequirePermission(h.perm, "cicd_build_runs:view"), h.Artifact)
	g.POST("/:id/cancel", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:execute"), h.Cancel)
	g.POST("/:id/retry", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:execute"), h.Retry)
	g.POST("/:id/redeploy", rbacmw.RequirePermission(h.perm, "cicd_build_jobs:execute"), h.Redeploy)
}

func (h *BuildRunHandler) List(c *gin.Context) {
	q := pkg.ParseListQuery(c)
	var jobID *uint
	if v := c.Query("build_job_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			u := uint(id)
			jobID = &u
		}
	}
	items, total, err := h.svc.List(q, jobID, c.Query("status"))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询失败")
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *BuildRunHandler) Get(c *gin.Context) {
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

func (h *BuildRunHandler) Cancel(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	item, err := h.svc.Cancel(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *BuildRunHandler) Retry(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	item, err := h.svc.Retry(id, authmiddleware.GetUserID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: item})
}

func (h *BuildRunHandler) Redeploy(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	var req service.RedeployInput
	_ = c.ShouldBindJSON(&req)
	item, err := h.svc.Redeploy(id, req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: item})
}

func (h *BuildRunHandler) Artifact(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	path, filename, err := h.svc.ArtifactPath(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.FileAttachment(path, filename)
}

func (h *BuildRunHandler) Log(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return
	}
	path, err := h.svc.LogPath(id)
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
