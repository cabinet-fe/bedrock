package handler

import (
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	"bedrock/internal/project/repository"
	projectservice "bedrock/internal/project/service"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	resourcemodel "bedrock/internal/resource/model"
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

// RegisterRoutesOnGroup mirrors the requireDocsAuth pattern: PAT requests need
// the bug scope (read for queries, write for create/transition/comment/upload),
// JWT requests keep RBAC. Routes not covered by a bug scope stay JWT-only.
func (h *BugHandler) RegisterRoutesOnGroup(g *gin.RouterGroup) {
	g.GET("/bugs", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.ListAcrossProjects)
	g.GET("/:id/bugs", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.ListProjectBugs)
	g.POST("/:id/bugs", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:create", resourcemodel.ScopeBugsWrite), h.CreateBug)
	g.GET("/:id/bugs/:bugID", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.GetBug)
	g.PUT("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateBug)
	g.DELETE("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:delete"), h.DeleteBug)
	g.PUT("/:id/bugs/:bugID/status", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:update", resourcemodel.ScopeBugsWrite), h.UpdateBugStatus)
	g.GET("/:id/bugs/:bugID/activities", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.ListBugActivities)
	g.GET("/:id/bugs/:bugID/comments", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.ListComments)
	g.POST("/:id/bugs/:bugID/comments", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:create", resourcemodel.ScopeBugsWrite), h.CreateComment)
	g.PUT("/:id/bugs/:bugID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateComment)
	g.DELETE("/:id/bugs/:bugID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_bugs:delete"), h.DeleteComment)
	g.GET("/:id/bugs/:bugID/attachments", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.ListAttachments)
	g.POST("/:id/bugs/:bugID/attachments", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:update", resourcemodel.ScopeBugsWrite), h.UploadAttachment)
	g.DELETE("/:id/bugs/:bugID/attachments/:attachmentID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.DeleteAttachment)
	g.GET("/:id/bugs/:bugID/attachments/:attachmentID/download", rbacmw.RequirePermissionOrPATScope(h.perm, "project_bugs:view", resourcemodel.ScopeBugsRead), h.DownloadAttachment)
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

// actorWithPATPermission grants patPermission to PAT requests: the route's PAT
// scope substitutes the global RBAC permission in service-layer checks.
func (h *BugHandler) actorWithPATPermission(c *gin.Context, patPermission string) (projectservice.AccessContext, bool) {
	actor, ok := h.actor(c)
	if !ok {
		return projectservice.AccessContext{}, false
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions[patPermission] = struct{}{}
	}
	return actor, true
}

// bugActor parses :id / :bugID and resolves the access context. A non-empty
// patPermission is granted to PAT requests (see actorWithPATPermission).
func (h *BugHandler) bugActor(c *gin.Context, patPermission ...string) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	bugID, ok := parseID(c, "bugID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	var actor projectservice.AccessContext
	if len(patPermission) > 0 {
		actor, ok = h.actorWithPATPermission(c, patPermission[0])
	} else {
		actor, ok = h.actor(c)
	}
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, bugID, actor, true
}

type bugListQuery struct {
	pkg.ListQuery
	Keyword       string `form:"keyword"`
	ProjectID     *uint  `form:"project_id"`
	Status        string `form:"status"`
	Severity      string `form:"severity"`
	Priority      string `form:"priority"`
	AssigneeID    *uint  `form:"assignee_id"`
	Assignee      string `form:"assignee"`
	ExcludeClosed bool   `form:"exclude_closed"`
}

// bugListFilter builds the repo filter; assignee (username or user ID) takes
// precedence over assignee_id. Writes the error response and returns false on
// resolution failure.
func (h *BugHandler) bugListFilter(c *gin.Context, query bugListQuery) (repository.BugFilter, bool) {
	filter := repository.BugFilter{
		Keyword:       query.Keyword,
		ProjectID:     query.ProjectID,
		Status:        query.Status,
		Severity:      query.Severity,
		Priority:      query.Priority,
		AssigneeID:    query.AssigneeID,
		ExcludeClosed: query.ExcludeClosed,
	}
	if query.Assignee != "" {
		assigneeID, err := h.svc.ResolveAssigneeRef(query.Assignee)
		if err != nil {
			writeServiceError(c, err)
			return filter, false
		}
		filter.AssigneeID = assigneeID
	}
	return filter, true
}

func (h *BugHandler) ListAcrossProjects(c *gin.Context) {
	actor, ok := h.actorWithPATPermission(c, "project_bugs:view")
	if !ok {
		return
	}
	var query bugListQuery
	q := pkg.BindList(c, &query)
	filter, ok := h.bugListFilter(c, query)
	if !ok {
		return
	}
	items, total, err := h.svc.ListAcrossProjects(actor, filter, q)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *BugHandler) ListProjectBugs(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actorWithPATPermission(c, "project_bugs:view")
	if !ok {
		return
	}
	var query bugListQuery
	q := pkg.BindList(c, &query)
	// The endpoint is already project-scoped by the path id: ignore the
	// project_id query param, which is only meaningful on GET /projects/bugs.
	query.ProjectID = nil
	filter, ok := h.bugListFilter(c, query)
	if !ok {
		return
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
	actor, ok := h.actorWithPATPermission(c, "project_bugs:create")
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
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:view")
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
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:update")
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
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:view")
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

func (h *BugHandler) ListComments(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:view")
	if !ok {
		return
	}
	comments, err := h.svc.ListComments(actor, projectID, bugID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, comments)
}

type bugCommentRequest struct {
	Content string `json:"content"`
}

func (h *BugHandler) CreateComment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:create")
	if !ok {
		return
	}
	var req bugCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	comment, err := h.svc.CreateComment(actor, projectID, bugID, req.Content)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, comment)
}

func (h *BugHandler) UpdateComment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	var req bugCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	comment, err := h.svc.UpdateComment(actor, projectID, bugID, commentID, req.Content)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, comment)
}

func (h *BugHandler) DeleteComment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	if err := h.svc.DeleteComment(actor, projectID, bugID, commentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": commentID})
}

func (h *BugHandler) ListAttachments(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:view")
	if !ok {
		return
	}
	attachments, err := h.svc.ListAttachments(actor, projectID, bugID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, attachments)
}

func (h *BugHandler) UploadAttachment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:update")
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "请提供附件 file")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无法读取附件")
		return
	}
	defer file.Close()

	attachment, err := h.svc.AddAttachment(actor, projectID, bugID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, attachment)
}

func (h *BugHandler) DeleteAttachment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	if err := h.svc.DeleteAttachment(actor, projectID, bugID, attachmentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": attachmentID})
}

func (h *BugHandler) DownloadAttachment(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c, "project_bugs:view")
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	file, att, contentType, err := h.svc.OpenAttachment(actor, projectID, bugID, attachmentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	defer file.Close()

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
	c.DataFromReader(http.StatusOK, -1, contentType, file, nil)
}
