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
)

// IssueHandler exposes the unified work-item API: /issues CRUD + kanban +
// watchers + iterations (api/project.md work items / kanban / iterations).
type IssueHandler struct {
	svc       *projectservice.IssueService
	iteration *projectservice.IterationService
	perm      *rbacservice.PermissionService
}

func NewIssueHandler(svc *projectservice.IssueService, iteration *projectservice.IterationService, perm *rbacservice.PermissionService) *IssueHandler {
	return &IssueHandler{svc: svc, iteration: iteration, perm: perm}
}

func (h *IssueHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/projects", authMW)

	// Unified read routes enforce their permission in the service layer
	// (either project_bugs:view or project_requirements:view applies).
	g.GET("/issues", h.ListAcrossProjects)
	g.GET("/issues/kanban", h.KanbanAcrossProjects)
	g.GET("/meta/issue-statuses", h.ListIssueStatuses)

	g.GET("/:id/issues", h.ListProjectIssues)
	g.POST("/:id/issues", h.CreateIssue)
	g.GET("/:id/issues/kanban", h.KanbanProjectIssues)
	g.GET("/:id/issues/:issueID", h.GetIssue)
	g.PUT("/:id/issues/:issueID", h.UpdateIssue)
	g.DELETE("/:id/issues/:issueID", h.DeleteIssue)
	g.PUT("/:id/issues/:issueID/status", h.TransitionStatus)
	g.GET("/:id/issues/:issueID/activities", h.ListActivities)
	g.GET("/:id/issues/:issueID/comments", h.ListComments)
	g.POST("/:id/issues/:issueID/comments", h.CreateComment)
	g.PUT("/:id/issues/:issueID/comments/:commentID", h.UpdateComment)
	g.DELETE("/:id/issues/:issueID/comments/:commentID", h.DeleteComment)
	g.GET("/:id/issues/:issueID/attachments", h.ListAttachments)
	g.POST("/:id/issues/:issueID/attachments", h.UploadAttachment)
	g.POST("/:id/issues/:issueID/comments/:commentID/attachments", h.UploadCommentAttachment)
	g.DELETE("/:id/issues/:issueID/attachments/:attachmentID", h.DeleteAttachment)
	g.GET("/:id/issues/:issueID/attachments/:attachmentID/download", h.DownloadAttachment)
	g.POST("/:id/issues/:issueID/watchers", h.Watch)
	g.DELETE("/:id/issues/:issueID/watchers", h.Unwatch)
	g.GET("/:id/issues/:issueID/watchers", h.Watching)

	g.GET("/:id/iterations", rbacmw.RequirePermission(h.perm, "project_projects:view"), h.ListIterations)
	g.POST("/:id/iterations", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.CreateIteration)
	g.PUT("/:id/iterations/:iterationID", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.UpdateIteration)
	g.DELETE("/:id/iterations/:iterationID", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.DeleteIteration)
	g.GET("/:id/iterations/:iterationID/burndown", rbacmw.RequirePermission(h.perm, "project_projects:view"), h.Burndown)
}

func (h *IssueHandler) actor(c *gin.Context) (projectservice.AccessContext, bool) {
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

type issueListQuery struct {
	pkg.ListQuery
	Keyword       string `form:"keyword"`
	ProjectID     *uint  `form:"project_id"`
	Type          string `form:"type"`
	Status        string `form:"status"`
	Severity      string `form:"severity"`
	Priority      string `form:"priority"`
	AssigneeID    *uint  `form:"assignee_id"`
	Assignee      string `form:"assignee"`
	ExcludeClosed bool   `form:"exclude_closed"`
	IterationID   *uint  `form:"iteration_id"`
}

type kanbanQuery struct {
	Type            string `form:"type"`
	IncludeTerminal bool   `form:"include_terminal"`
	IterationID     *uint  `form:"iteration_id"`
	ProjectID       *uint  `form:"project_id"`
	Keyword         string `form:"keyword"`
}

func (h *IssueHandler) issueFilterFromQuery(query issueListQuery) repository.IssueFilter {
	return repository.IssueFilter{
		Keyword: query.Keyword, ProjectID: query.ProjectID, Type: query.Type,
		Status: query.Status, Severity: query.Severity, Priority: query.Priority,
		AssigneeID: query.AssigneeID, ExcludeClosed: query.ExcludeClosed,
		IterationID: query.IterationID,
	}
}

func (h *IssueHandler) resolveAssignee(c *gin.Context, query issueListQuery, filter repository.IssueFilter) (repository.IssueFilter, bool) {
	if query.Assignee == "" {
		return filter, true
	}
	assigneeID, err := h.svc.ResolveAssigneeRef(query.Assignee)
	if err != nil {
		writeServiceError(c, err)
		return filter, false
	}
	filter.AssigneeID = assigneeID
	return filter, true
}

func (h *IssueHandler) ListAcrossProjects(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query issueListQuery
	q := pkg.BindList(c, &query)
	filter := h.issueFilterFromQuery(query)
	filter, ok = h.resolveAssignee(c, query, filter)
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

func (h *IssueHandler) ListProjectIssues(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query issueListQuery
	q := pkg.BindList(c, &query)
	query.ProjectID = nil
	filter := h.issueFilterFromQuery(query)
	filter, ok = h.resolveAssignee(c, query, filter)
	if !ok {
		return
	}
	items, total, err := h.svc.ListProjectIssues(actor, projectID, filter, q)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, q)
}

func (h *IssueHandler) CreateIssue(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var input projectservice.CreateIssueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	issue, err := h.svc.CreateIssue(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, issue)
}

func (h *IssueHandler) GetIssue(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	issue, err := h.svc.GetIssue(actor, projectID, issueID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, issue)
}

func (h *IssueHandler) issueActor(c *gin.Context) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	issueID, ok := parseID(c, "issueID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	actor, ok := h.actor(c)
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, issueID, actor, true
}

func (h *IssueHandler) UpdateIssue(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	var input projectservice.UpdateIssueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	issue, err := h.svc.UpdateIssue(actor, projectID, issueID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, issue)
}

func (h *IssueHandler) DeleteIssue(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteIssue(actor, projectID, issueID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": issueID})
}

func (h *IssueHandler) TransitionStatus(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	var input projectservice.TransitionIssueStatusInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	issue, err := h.svc.TransitionIssueStatus(actor, projectID, issueID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, issue)
}

func (h *IssueHandler) ListActivities(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	activities, err := h.svc.ListIssueActivities(actor, projectID, issueID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": activities})
}

func (h *IssueHandler) ListComments(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	comments, err := h.svc.ListComments(actor, projectID, issueID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": comments})
}

func (h *IssueHandler) CreateComment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	var input projectservice.CreateIssueCommentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	comment, err := h.svc.CreateComment(actor, projectID, issueID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, comment)
}

func (h *IssueHandler) UpdateComment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	comment, err := h.svc.UpdateComment(actor, projectID, issueID, commentID, req.Content)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, comment)
}

func (h *IssueHandler) DeleteComment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	if err := h.svc.DeleteComment(actor, projectID, issueID, commentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": commentID})
}

func (h *IssueHandler) ListAttachments(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	attachments, err := h.svc.ListAttachments(actor, projectID, issueID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": attachments})
}

func (h *IssueHandler) UploadAttachment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	file, fileHeader, ok := attachmentFormFile(c)
	if !ok {
		return
	}
	defer file.Close()
	attachment, err := h.svc.AddAttachment(actor, projectID, issueID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, attachment)
}

func (h *IssueHandler) UploadCommentAttachment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	file, fileHeader, ok := attachmentFormFile(c)
	if !ok {
		return
	}
	defer file.Close()
	attachment, err := h.svc.AddCommentAttachment(actor, projectID, issueID, commentID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, attachment)
}

func (h *IssueHandler) DeleteAttachment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	if err := h.svc.DeleteAttachment(actor, projectID, issueID, attachmentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": attachmentID})
}

func (h *IssueHandler) DownloadAttachment(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	file, att, contentType, err := h.svc.OpenAttachment(actor, projectID, issueID, attachmentID)
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

func (h *IssueHandler) Watch(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	if err := h.svc.Watch(actor, projectID, issueID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"watching": true})
}

func (h *IssueHandler) Unwatch(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	if err := h.svc.Unwatch(actor, projectID, issueID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"watching": false})
}

func (h *IssueHandler) Watching(c *gin.Context) {
	projectID, issueID, actor, ok := h.issueActor(c)
	if !ok {
		return
	}
	watching, err := h.svc.Watching(actor, projectID, issueID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"watching": watching})
}

// Kanban ---------------------------------------------------------------------

func (h *IssueHandler) KanbanProjectIssues(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query kanbanQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	filter := repository.IssueFilter{Keyword: query.Keyword, IterationID: query.IterationID}
	board, err := h.svc.Kanban(actor, &projectID, query.Type, query.IncludeTerminal, filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, board)
}

func (h *IssueHandler) KanbanAcrossProjects(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var query kanbanQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	filter := repository.IssueFilter{Keyword: query.Keyword, ProjectID: query.ProjectID}
	board, err := h.svc.Kanban(actor, nil, query.Type, query.IncludeTerminal, filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, board)
}

func (h *IssueHandler) ListIssueStatuses(c *gin.Context) {
	issueType := c.Query("type")
	if issueType == "" {
		issueType = "requirement"
	}
	statuses, err := h.svc.ListStatusOptions(issueType)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": statuses})
}

// Iterations -------------------------------------------------------------------

func (h *IssueHandler) iterationActor(c *gin.Context) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	iterationID, ok := parseID(c, "iterationID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	actor, ok := h.actor(c)
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, iterationID, actor, true
}

func (h *IssueHandler) ListIterations(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	iterations, err := h.iteration.ListIterations(actor, projectID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": iterations})
}

func (h *IssueHandler) CreateIteration(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var input projectservice.IterationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	iteration, err := h.iteration.CreateIteration(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, iteration)
}

func (h *IssueHandler) UpdateIteration(c *gin.Context) {
	projectID, iterationID, actor, ok := h.iterationActor(c)
	if !ok {
		return
	}
	var input projectservice.IterationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	iteration, err := h.iteration.UpdateIteration(actor, projectID, iterationID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, iteration)
}

func (h *IssueHandler) DeleteIteration(c *gin.Context) {
	projectID, iterationID, actor, ok := h.iterationActor(c)
	if !ok {
		return
	}
	if err := h.iteration.DeleteIteration(actor, projectID, iterationID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": iterationID})
}

func (h *IssueHandler) Burndown(c *gin.Context) {
	projectID, iterationID, actor, ok := h.iterationActor(c)
	if !ok {
		return
	}
	chart, err := h.iteration.Burndown(actor, projectID, iterationID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, chart)
}
