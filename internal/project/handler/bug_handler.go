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
	g.POST("/:id/bugs/ai-extract", rbacmw.RequirePermission(h.perm, "project_bugs:create"), h.AIExtract)
	g.GET("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.GetBug)
	g.PUT("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateBug)
	g.DELETE("/:id/bugs/:bugID", rbacmw.RequirePermission(h.perm, "project_bugs:delete"), h.DeleteBug)
	g.PUT("/:id/bugs/:bugID/status", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateBugStatus)
	g.GET("/:id/bugs/:bugID/activities", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListBugActivities)
	g.POST("/:id/bugs/:bugID/ai-analyze", rbacmw.RequirePermission(h.perm, "project_bugs:execute"), h.AIAnalyze)
	g.POST("/:id/bugs/:bugID/dispatch-agent", rbacmw.RequirePermission(h.perm, "project_bugs:execute"), h.DispatchAgent)
	g.GET("/:id/bugs/:bugID/comments", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListComments)
	g.POST("/:id/bugs/:bugID/comments", rbacmw.RequirePermission(h.perm, "project_bugs:create"), h.CreateComment)
	g.PUT("/:id/bugs/:bugID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UpdateComment)
	g.DELETE("/:id/bugs/:bugID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_bugs:delete"), h.DeleteComment)
	g.GET("/:id/bugs/:bugID/attachments", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.ListAttachments)
	g.POST("/:id/bugs/:bugID/attachments", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.UploadAttachment)
	g.DELETE("/:id/bugs/:bugID/attachments/:attachmentID", rbacmw.RequirePermission(h.perm, "project_bugs:update"), h.DeleteAttachment)
	g.GET("/:id/bugs/:bugID/attachments/:attachmentID/download", rbacmw.RequirePermission(h.perm, "project_bugs:view"), h.DownloadAttachment)
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

type aiExtractRequest struct {
	Content string `json:"content"`
}

func (h *BugHandler) AIExtract(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req aiExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	result, err := h.svc.AIExtract(actor, projectID, req.Content)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, result)
}

type aiAnalyzeRequest struct {
	Prompt string `json:"prompt"`
}

func (h *BugHandler) AIAnalyze(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	var req aiAnalyzeRequest
	_ = c.ShouldBindJSON(&req)

	result, err := h.svc.AIAnalyze(actor, projectID, bugID, req.Prompt)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"ai_analysis": result})
}

type dispatchAgentRequest struct {
	AgentID    uint   `json:"agent_id"`
	UserPrompt string `json:"user_prompt"`
}

func (h *BugHandler) DispatchAgent(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
	if !ok {
		return
	}
	var req dispatchAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效参数")
		return
	}
	if req.AgentID == 0 {
		pkg.Error(c, http.StatusBadRequest, "必须指定智能体 agent_id")
		return
	}
	runID, err := h.svc.DispatchAgent(actor, projectID, bugID, req.AgentID, req.UserPrompt)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: gin.H{"agent_run_id": runID}})
}

func (h *BugHandler) ListComments(c *gin.Context) {
	projectID, bugID, actor, ok := h.bugActor(c)
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
	projectID, bugID, actor, ok := h.bugActor(c)
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
	projectID, bugID, actor, ok := h.bugActor(c)
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
	projectID, bugID, actor, ok := h.bugActor(c)
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
	projectID, bugID, actor, ok := h.bugActor(c)
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
