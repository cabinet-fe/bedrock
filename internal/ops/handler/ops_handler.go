package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/ops/model"
	"bedrock/internal/ops/service"
	"bedrock/internal/pkg"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
)

type OpsHandler struct {
	processes *service.ProcessService
	devEnvs   *service.DevEnvironmentService
	perm      *rbacservice.PermissionService
}

func NewOpsHandler(processes *service.ProcessService, devEnvs *service.DevEnvironmentService, perm *rbacservice.PermissionService) *OpsHandler {
	return &OpsHandler{processes: processes, devEnvs: devEnvs, perm: perm}
}

func (h *OpsHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/ops", authMW)

	g.GET("/processes", rbacmw.RequirePermission(h.perm, "ops_processes:view"), h.ListProcesses)
	g.POST("/processes/:pid/kill", rbacmw.RequirePermission(h.perm, "ops_processes:execute"), h.KillProcess)

	g.GET("/dev-environments", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.ListEnvironments)
	g.POST("/dev-environments", rbacmw.RequirePermission(h.perm, "ops_dev_environments:create"), h.CreateEnvironment)
	g.PUT("/dev-environments/:id", rbacmw.RequirePermission(h.perm, "ops_dev_environments:update"), h.UpdateEnvironment)
	g.DELETE("/dev-environments/:id", rbacmw.RequirePermission(h.perm, "ops_dev_environments:delete"), h.DeleteEnvironment)
	g.POST("/dev-environments/:id/detect", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.DetectEnvironment)
	g.POST("/dev-environments/:id/install", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.EnqueueInstall)
	g.POST("/dev-environments/:id/upgrade", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.EnqueueUpgrade)
	g.POST("/dev-environments/:id/uninstall", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.EnqueueUninstall)
	g.POST("/dev-environments/:id/switch", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.EnqueueSwitch)

	g.GET("/dev-environments/:id/sources", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.ListSources)
	g.POST("/dev-environments/:id/sources", rbacmw.RequirePermission(h.perm, "ops_dev_environments:create"), h.CreateSource)
	g.PUT("/dev-environments/:id/sources/:sourceId", rbacmw.RequirePermission(h.perm, "ops_dev_environments:update"), h.UpdateSource)
	g.DELETE("/dev-environments/:id/sources/:sourceId", rbacmw.RequirePermission(h.perm, "ops_dev_environments:delete"), h.DeleteSource)
	g.POST("/dev-environments/:id/sources/:sourceId/ping", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.PingSource)

	g.GET("/dev-environments/:id/jobs", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.ListJobs)
	g.GET("/dev-environments/:id/jobs/:jobId", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.GetJob)
	g.GET("/dev-environments/:id/jobs/:jobId/logs", rbacmw.RequirePermission(h.perm, "ops_dev_environments:view"), h.JobLogs)
	g.POST("/dev-environments/:id/jobs/:jobId/retry", rbacmw.RequirePermission(h.perm, "ops_dev_environments:execute"), h.RetryJob)
}

func (h *OpsHandler) ListProcesses(c *gin.Context) {
	opts := model.ProcessListOptions{
		Keyword: c.Query("keyword"), Sort: c.Query("sort"),
	}
	if raw := c.Query("pid"); raw != "" {
		pid, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "无效 PID")
			return
		}
		value := int32(pid)
		opts.PID = &value
	}
	if raw := c.Query("port"); raw != "" {
		port, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "无效端口")
			return
		}
		value := uint32(port)
		opts.Port = &value
	}
	items, err := h.processes.ListProcesses(opts)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询进程失败")
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *OpsHandler) KillProcess(c *gin.Context) {
	pid, err := strconv.ParseInt(c.Param("pid"), 10, 32)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效 PID")
		return
	}
	name, err := h.processes.KillProcess(int32(pid))
	if errors.Is(err, service.ErrKillSelf) || errors.Is(err, service.ErrDangerousProcess) {
		pkg.Error(c, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Success(c, gin.H{"pid": pid, "name": name, "status": "terminated"})
}

func (h *OpsHandler) ListEnvironments(c *gin.Context) {
	items, err := h.devEnvs.ListEnvironments()
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "查询开发环境失败")
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *OpsHandler) CreateEnvironment(c *gin.Context) {
	var input service.DevEnvironmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效开发环境定义")
		return
	}
	item, err := h.devEnvs.CreateCustom(input, authmiddleware.GetUserID(c))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Created(c, item)
}

func (h *OpsHandler) UpdateEnvironment(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input service.DevEnvironmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效开发环境定义")
		return
	}
	item, err := h.devEnvs.UpdateCustom(id, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *OpsHandler) DeleteEnvironment(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.devEnvs.DeleteCustom(id); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": id})
}

func (h *OpsHandler) DetectEnvironment(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	result, err := h.devEnvs.Detect(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *OpsHandler) EnqueueInstall(c *gin.Context)   { h.enqueue(c, "install") }
func (h *OpsHandler) EnqueueUpgrade(c *gin.Context)   { h.enqueue(c, "upgrade") }
func (h *OpsHandler) EnqueueUninstall(c *gin.Context) { h.enqueue(c, "uninstall") }
func (h *OpsHandler) EnqueueSwitch(c *gin.Context)    { h.enqueue(c, "switch") }

func (h *OpsHandler) enqueue(c *gin.Context, operation string) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input service.JobInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效任务参数")
		return
	}
	job, err := h.devEnvs.Enqueue(id, operation, input, authmiddleware.GetUserID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: job})
}

func (h *OpsHandler) ListSources(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.devEnvs.ListSources(envID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *OpsHandler) CreateSource(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input service.SourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效安装源")
		return
	}
	item, err := h.devEnvs.CreateSource(envID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, item)
}

func (h *OpsHandler) UpdateSource(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseID(c, "sourceId")
	if !ok {
		return
	}
	var input service.SourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效安装源")
		return
	}
	item, err := h.devEnvs.UpdateSource(envID, sourceID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *OpsHandler) DeleteSource(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseID(c, "sourceId")
	if !ok {
		return
	}
	if err := h.devEnvs.DeleteSource(envID, sourceID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": sourceID})
}

func (h *OpsHandler) PingSource(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseID(c, "sourceId")
	if !ok {
		return
	}
	okResult, detail, err := h.devEnvs.PingSource(envID, sourceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"ok": okResult, "detail": detail})
}

func (h *OpsHandler) ListJobs(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	q := pkg.ParseListQuery(c)
	items, total, err := h.devEnvs.ListJobs(envID, q, c.Query("status"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *OpsHandler) GetJob(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	jobID, ok := parseID(c, "jobId")
	if !ok {
		return
	}
	item, err := h.devEnvs.GetJob(envID, jobID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, item)
}

func (h *OpsHandler) JobLogs(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	jobID, ok := parseID(c, "jobId")
	if !ok {
		return
	}
	logs, err := h.devEnvs.JobLogs(envID, jobID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(logs))
}

func (h *OpsHandler) RetryJob(c *gin.Context) {
	envID, ok := parseID(c, "id")
	if !ok {
		return
	}
	jobID, ok := parseID(c, "jobId")
	if !ok {
		return
	}
	job, err := h.devEnvs.Retry(envID, jobID, authmiddleware.GetUserID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: job})
}

func parseID(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return 0, false
	}
	return uint(id), true
}

func writeServiceError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pkg.Error(c, http.StatusNotFound, "资源不存在")
		return
	}
	if errors.Is(err, service.ErrBuiltinImmutable) || errors.Is(err, service.ErrMissingScript) ||
		errors.Is(err, service.ErrInvalidOperation) {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	pkg.Error(c, http.StatusBadRequest, err.Error())
}
