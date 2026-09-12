package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	"bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
)

type BugHandler struct {
	svc  *projectservice.BugService
	perm *rbacservice.PermissionService
}

func NewBugHandler(svc *projectservice.BugService, perm *rbacservice.PermissionService) *BugHandler {
	return &BugHandler{svc: svc, perm: perm}
}

func (h *BugHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/projects", authMW)
	h.RegisterRoutesOnGroup(g)
}

func (h *BugHandler) RegisterRoutesOnGroup(g *gin.RouterGroup) {
	g.GET("/bugs", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListAcrossProjects)
	g.GET("/:id/bugs", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListProjectBugs)
	g.POST("/:id/bugs", rbacmw.RequirePermission(h.perm, "project_bugs:create"), h.CreateBug)
	g.GET("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.GetBug)
	g.PUT("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateBug)
	g.DELETE("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:delete"), h.DeleteBug)
	g.PUT("/:id/bugs/:bugID/status", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateBugStatus)
	g.GET("/:id/bugs/:bugID/activities", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListBugActivities)
}

func (h *BugHandler) actor(c *gin.Context) (projectservice.AccessContext, bool) {
	userID := authmiddleware.GetUserID(c)
	isSuper := authmiddleware.IsSuperAdmin(c)
	permissions, err := h.perm.ResolvePermissions(userID, isSuper)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return projectservice.AccessContext{}, false
	}
	dataScope, err := h.perm.ResolveDataScope(userID, isSuper)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return projectservice.AccessContext{}, false
	}
	return projectservice.NewAccessContextWithDataScope(userID, isSuper, permissions, dataScope), true
}

func (h *BugHandler) bugActor(c *gin.Context) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	bugID, ok := parseID(c, "bugID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	actor, ok := h.actor(c)
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, bugID, actor, true
}

type acrossBugQuery struct {
	pkg.ListQuery
	Keyword    string `form:"keyword"`
	ProjectID  *uint  `form:"project_id"`
	Status     string `form:"status"`
	Severity   string `form:"severity"`
	Priority   string `form:"priority"`
	AssigneeID *uint  `form:"assignee_id"`
}

func (h *BugHandler) ListAcrossProjects(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query acrossBugQuery
	q := pkg.BindList(c, &query)
	filter := repository.BugFilter{
		Keyword:    query.Keyword,
		ProjectID:  query.ProjectID,
		Status:     query.Status,
		Severity:   query.Severity,
		Priority:   query.Priority,
		AssigneeID: query.AssigneeID,
	}
	items, total, err := h.svc.ListAcrossProjects(actor, filter, q)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

type projectBugQuery struct {
	pkg.ListQuery
	Keyword    string `form:"keyword"`
	Status     string `form:"status"`
	Severity   string `form:"severity"`
	Priority   string `form:"priority"`
	AssigneeID *uint  `form:"assignee_id"`
}

func (h *BugHandler) ListProjectBugs(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query projectBugQuery
	q := pkg.BindList(c, &query)
	filter := repository.BugFilter{
		Keyword:    query.Keyword,
		Status:     query.Status,
		Severity:   query.Severity,
		Priority:   query.Priority,
		AssigneeID: query.AssigneeID,
	}
	items, total, err := h.svc.ListProjectBugs(actor, projectID, filter, q)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *BugHandler) CreateBug(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var input projectservice.CreateBugInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	bug, err := h.svc.CreateBug(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, bug)
}

func (h *BugHandler) GetBug(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	bug, err := h.svc.GetBug(actor, projectID, bugID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, bug)
}

func (h *BugHandler) UpdateBug(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	var input projectservice.UpdateBugInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	bug, err := h.svc.UpdateBug(actor, projectID, bugID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, bug)
}

func (h *BugHandler) DeleteBug(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteBug(actor, projectID, bugID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": bugID})
}

func (h *BugHandler) UpdateBugStatus(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	var input projectservice.TransitionBugStatusInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	bug, err := h.svc.TransitionBugStatus(actor, projectID, bugID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, bug)
}

func (h *BugHandler) ListBugActivities(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	activities, err := h.svc.ListBugActivities(actor, projectID, bugID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, activities)
}
