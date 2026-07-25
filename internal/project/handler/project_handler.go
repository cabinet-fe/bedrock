package handler

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	projectservice "bedrock/internal/project/service"
	rbacmw "bedrock/internal/rbac/middleware"
	rbacservice "bedrock/internal/rbac/service"
	storageservice "bedrock/internal/storage/service"
)

type ProjectHandler struct {
	svc  *projectservice.ProjectService
	perm *rbacservice.PermissionService
}

func NewProjectHandler(svc *projectservice.ProjectService, perm *rbacservice.PermissionService) *ProjectHandler {
	return &ProjectHandler{svc: svc, perm: perm}
}

func (h *ProjectHandler) RegisterRoutes(rg *gin.RouterGroup, authMW gin.HandlerFunc) {
	g := rg.Group("/projects", authMW)

	g.GET("", rbacmw.RequirePermission(h.perm, "project_projects:view"), h.ListProjects)
	g.POST("", rbacmw.RequirePermission(h.perm, "project_projects:create"), h.CreateProject)
	g.GET("/meta/requirement-statuses", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.ListRequirementStatuses)
	g.GET("/:id", rbacmw.RequirePermission(h.perm, "project_projects:view"), h.GetProject)
	g.PUT("/:id", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.UpdateProject)
	g.POST("/:id/archive", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.ArchiveProject)
	g.DELETE("/:id", rbacmw.RequirePermission(h.perm, "project_projects:delete"), h.DeleteProject)

	g.GET("/:id/members", rbacmw.RequirePermission(h.perm, "project_projects:view"), h.ListMembers)
	g.POST("/:id/members", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.AddMember)
	g.PUT("/:id/members/:userID", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.UpdateMember)
	g.DELETE("/:id/members/:userID", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.RemoveMember)
	g.POST("/:id/members/transfer-owner", rbacmw.RequirePermission(h.perm, "project_projects:update"), h.TransferOwner)

	g.GET("/:id/requirements", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.ListRequirements)
	g.POST("/:id/requirements", rbacmw.RequirePermission(h.perm, "project_requirements:create"), h.CreateRequirement)
	g.GET("/:id/requirements/:requirementID", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.GetRequirement)
	g.PUT("/:id/requirements/:requirementID", rbacmw.RequirePermission(h.perm, "project_requirements:update"), h.UpdateRequirement)
	g.DELETE("/:id/requirements/:requirementID", rbacmw.RequirePermission(h.perm, "project_requirements:delete"), h.DeleteRequirement)

	g.GET("/:id/requirements/:requirementID/comments", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.ListComments)
	g.POST("/:id/requirements/:requirementID/comments", rbacmw.RequirePermission(h.perm, "project_requirements:create"), h.CreateComment)
	g.PUT("/:id/requirements/:requirementID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_requirements:update"), h.UpdateComment)
	g.DELETE("/:id/requirements/:requirementID/comments/:commentID", rbacmw.RequirePermission(h.perm, "project_requirements:delete"), h.DeleteComment)

	g.GET("/:id/requirements/:requirementID/attachments", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.ListAttachments)
	g.POST("/:id/requirements/:requirementID/attachments", rbacmw.RequirePermission(h.perm, "project_requirements:update"), h.UploadAttachment)
	g.GET("/:id/requirements/:requirementID/attachments/:attachmentID/download", rbacmw.RequirePermission(h.perm, "project_requirements:view"), h.DownloadAttachment)
	g.DELETE("/:id/requirements/:requirementID/attachments/:attachmentID", rbacmw.RequirePermission(h.perm, "project_requirements:update"), h.DeleteAttachment)

	g.GET("/:id/docs", rbacmw.RequirePermission(h.perm, "project_docs:view"), h.ListDocTree)
	g.POST("/:id/docs", rbacmw.RequirePermission(h.perm, "project_docs:create"), h.CreateDocNode)
	g.GET("/:id/docs/:nodeID", rbacmw.RequirePermission(h.perm, "project_docs:view"), h.GetDocNode)
	g.PUT("/:id/docs/:nodeID", rbacmw.RequirePermission(h.perm, "project_docs:update"), h.UpdateDocNode)
	g.POST("/:id/docs/:nodeID/move", rbacmw.RequirePermission(h.perm, "project_docs:update"), h.MoveDocNode)
	g.DELETE("/:id/docs/:nodeID", rbacmw.RequirePermission(h.perm, "project_docs:delete"), h.DeleteDocNode)
	g.POST("/:id/docs/upload", rbacmw.RequirePermission(h.perm, "project_docs:create"), h.UploadMarkdown)
	g.POST("/:id/docs/import-zip", rbacmw.RequirePermission(h.perm, "project_docs:create"), h.ImportZIP)
	// External push/publish: PAT scope or JWT RBAC checked inside handler (see APIRun pattern).
	g.POST("/:id/docs/push", h.PushDocByPath)
	g.POST("/:id/docs/publish-path", h.PublishDocByPath)
	g.POST("/:id/docs/:nodeID/publish", rbacmw.RequirePermission(h.perm, "project_docs:update"), h.PublishDocNode)
	g.GET("/:id/docs/:nodeID/diff", rbacmw.RequirePermission(h.perm, "project_docs:view"), h.GetDocDiff)
	g.POST("/:id/docs/generate", rbacmw.RequirePermission(h.perm, "project_docs:execute"), h.GenerateDocs)
}

func (h *ProjectHandler) ListProjects(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	page := pkg.ParsePage(c)
	items, total, err := h.svc.ListProjects(actor, projectservice.ProjectListFilter{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Page: uint(page.Page), PageSize: uint(page.PageSize),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, page)
}

func (h *ProjectHandler) ListRequirementStatuses(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.svc.ListRequirementStatuses(actor)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var input projectservice.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效项目参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	project, err := h.svc.CreateProject(actor, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, project)
}

func (h *ProjectHandler) GetProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	project, err := h.svc.GetProject(actor, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, project)
}

func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input projectservice.UpdateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效项目参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	project, err := h.svc.UpdateProject(actor, id, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, project)
}

func (h *ProjectHandler) ArchiveProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	project, err := h.svc.ArchiveProject(actor, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, project)
}

func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(actor, id); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": id})
}

func (h *ProjectHandler) ListMembers(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.svc.ListMembers(actor, projectID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) AddMember(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input projectservice.MemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效成员参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	member, err := h.svc.AddMember(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, member)
}

func (h *ProjectHandler) UpdateMember(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID, ok := parseID(c, "userID")
	if !ok {
		return
	}
	var input projectservice.MemberInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效成员参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	member, err := h.svc.UpdateMember(actor, projectID, userID, input.Role)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, member)
}

func (h *ProjectHandler) RemoveMember(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	userID, ok := parseID(c, "userID")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(actor, projectID, userID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"user_id": userID})
}

func (h *ProjectHandler) TransferOwner(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input struct {
		UserID uint `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.UserID == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效 Owner")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	project, err := h.svc.TransferOwner(actor, projectID, input.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, project)
}

func (h *ProjectHandler) ListRequirements(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	page := pkg.ParsePage(c)
	items, total, err := h.svc.ListRequirements(actor, projectID, projectservice.RequirementFilter{
		Keyword:  c.Query("keyword"),
		Status:   c.Query("status"),
		Priority: c.Query("priority"),
		Assignee: c.Query("assignee_id"),
		Sort:     c.Query("sort"),
		Page:     uint(page.Page),
		PageSize: uint(page.PageSize),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.PageSuccess(c, items, total, page)
}

func (h *ProjectHandler) CreateRequirement(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input projectservice.RequirementInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效需求参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	requirement, err := h.svc.CreateRequirement(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, requirement)
}

func (h *ProjectHandler) GetRequirement(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:view", false)
	if !ok {
		return
	}
	requirement, err := h.svc.GetRequirement(actor, requirementID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, requirement)
}

func (h *ProjectHandler) UpdateRequirement(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:update", true)
	if !ok {
		return
	}
	var input projectservice.RequirementInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效需求参数")
		return
	}
	requirement, err := h.svc.UpdateRequirement(actor, requirementID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, requirement)
}

func (h *ProjectHandler) DeleteRequirement(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:delete", true)
	if !ok {
		return
	}
	if err := h.svc.DeleteRequirement(actor, requirementID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": requirementID})
}

func (h *ProjectHandler) ListComments(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:view", false)
	if !ok {
		return
	}
	items, err := h.svc.ListComments(actor, requirementID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) CreateComment(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:create", true)
	if !ok {
		return
	}
	var input projectservice.CommentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效评论参数")
		return
	}
	comment, err := h.svc.CreateComment(actor, requirementID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, comment)
}

func (h *ProjectHandler) UpdateComment(c *gin.Context) {
	projectID, requirementID, actor, ok := h.requirementActor(c, "project_requirements:update", true)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	if err := h.svc.CheckCommentRequirementProject(actor, projectID, requirementID, commentID, "project_requirements:update", true); err != nil {
		writeServiceError(c, err)
		return
	}
	var input projectservice.CommentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效评论参数")
		return
	}
	comment, err := h.svc.UpdateComment(actor, commentID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, comment)
}

func (h *ProjectHandler) DeleteComment(c *gin.Context) {
	projectID, requirementID, actor, ok := h.requirementActor(c, "project_requirements:delete", true)
	if !ok {
		return
	}
	commentID, ok := parseID(c, "commentID")
	if !ok {
		return
	}
	if err := h.svc.CheckCommentRequirementProject(actor, projectID, requirementID, commentID, "project_requirements:delete", true); err != nil {
		writeServiceError(c, err)
		return
	}
	if err := h.svc.DeleteComment(actor, commentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": commentID})
}

func (h *ProjectHandler) ListAttachments(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:view", false)
	if !ok {
		return
	}
	items, err := h.svc.ListAttachments(actor, requirementID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) UploadAttachment(c *gin.Context) {
	_, requirementID, actor, ok := h.requirementActor(c, "project_requirements:update", true)
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
	attachment, err := h.svc.AddAttachment(actor, requirementID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, attachment)
}

func (h *ProjectHandler) DownloadAttachment(c *gin.Context) {
	projectID, requirementID, actor, ok := h.requirementActor(c, "project_requirements:view", false)
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	if err := h.svc.CheckAttachmentRequirementProject(actor, projectID, requirementID, attachmentID, "project_requirements:view", false); err != nil {
		writeServiceError(c, err)
		return
	}
	file, attachment, contentType, err := h.svc.OpenAttachment(actor, attachmentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	defer file.Close()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
	c.DataFromReader(http.StatusOK, -1, contentType, file, nil)
}

func (h *ProjectHandler) DeleteAttachment(c *gin.Context) {
	projectID, requirementID, actor, ok := h.requirementActor(c, "project_requirements:update", true)
	if !ok {
		return
	}
	attachmentID, ok := parseID(c, "attachmentID")
	if !ok {
		return
	}
	if err := h.svc.CheckAttachmentRequirementProject(actor, projectID, requirementID, attachmentID, "project_requirements:update", true); err != nil {
		writeServiceError(c, err)
		return
	}
	if err := h.svc.DeleteAttachment(actor, attachmentID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": attachmentID})
}

func (h *ProjectHandler) ListDocTree(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.svc.ListDocTree(actor, projectID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) CreateDocNode(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input projectservice.DocNodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效文档节点参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	node, err := h.svc.CreateDocNode(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, node)
}

func (h *ProjectHandler) GetDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:view", false)
	if !ok {
		return
	}
	node, err := h.svc.GetDocNode(actor, nodeID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) UpdateDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:update", true)
	if !ok {
		return
	}
	var input projectservice.DocNodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效文档节点参数")
		return
	}
	node, err := h.svc.UpdateDocNode(actor, nodeID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) MoveDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:update", true)
	if !ok {
		return
	}
	var input projectservice.DocMoveInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效移动参数")
		return
	}
	node, err := h.svc.MoveDocNode(actor, nodeID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) DeleteDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:delete", true)
	if !ok {
		return
	}
	if err := h.svc.DeleteDocNode(actor, nodeID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": nodeID})
}

func (h *ProjectHandler) UploadMarkdown(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	parentID, ok := parseOptionalID(c, "parent_id")
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "请提供 Markdown file")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无法读取 Markdown 文件")
		return
	}
	defer file.Close()
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	node, err := h.svc.UploadMarkdown(actor, projectID, parentID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, node)
}

func (h *ProjectHandler) ImportZIP(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	parentID, ok := parseOptionalID(c, "parent_id")
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "请提供 ZIP file")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "无法读取 ZIP 文件")
		return
	}
	defer file.Close()
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.svc.ImportZIP(actor, projectID, parentID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, gin.H{"items": items})
}

func (h *ProjectHandler) PushDocByPath(c *gin.Context) {
	if !h.requireDocsAuth(c, "docs:write", "project_docs:create") {
		return
	}
	projectID, ok := h.resolveProjectID(c)
	if !ok {
		return
	}
	var input struct {
		ApiDir     string `json:"api_dir"`
		ApiDocName string `json:"api_doc_name"`
		ApiDoc     string `json:"api_doc"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效推送参数")
		return
	}
	if strings.TrimSpace(input.ApiDocName) == "" {
		pkg.Error(c, http.StatusBadRequest, "api_doc_name 不能为空")
		return
	}
	if input.ApiDoc == "" {
		pkg.Error(c, http.StatusBadRequest, "api_doc 不能为空")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions["project_docs:create"] = struct{}{}
	}
	node, created, err := h.svc.UpsertDocByPath(actor, projectID, input.ApiDir, input.ApiDocName, input.ApiDoc)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if created {
		pkg.Created(c, node)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) PublishDocByPath(c *gin.Context) {
	if !h.requireDocsAuth(c, "docs:publish", "project_docs:update") {
		return
	}
	projectID, ok := h.resolveProjectID(c)
	if !ok {
		return
	}
	var input struct {
		ApiDir     string `json:"api_dir"`
		ApiDocName string `json:"api_doc_name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效发布参数")
		return
	}
	if strings.TrimSpace(input.ApiDocName) == "" {
		pkg.Error(c, http.StatusBadRequest, "api_doc_name 不能为空")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions["project_docs:update"] = struct{}{}
	}
	node, err := h.svc.PublishDocByPath(actor, projectID, input.ApiDir, input.ApiDocName)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

// requireDocsAuth mirrors AI APIRun: PAT needs scope; JWT needs RBAC permission.
func (h *ProjectHandler) requireDocsAuth(c *gin.Context, patScope, rbacPermission string) bool {
	if authmiddleware.IsPAT(c) {
		if err := authmiddleware.RequirePATScope(c, patScope); err != nil {
			pkg.Error(c, http.StatusForbidden, "token scope insufficient")
			return false
		}
		return true
	}
	if err := h.perm.CheckAccess(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), rbacPermission); err != nil {
		pkg.Error(c, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *ProjectHandler) PublishDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:update", true)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion *int `json:"expected_version"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效发布参数")
		return
	}
	if input.ExpectedVersion == nil {
		pkg.Error(c, http.StatusBadRequest, "expected_version 为必填项")
		return
	}
	node, err := h.svc.PublishDocNode(actor, nodeID, *input.ExpectedVersion)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) GetDocDiff(c *gin.Context) {
	_, nodeID, actor, ok := h.docActor(c, "project_docs:view", false)
	if !ok {
		return
	}
	diff, err := h.svc.GetDocDiff(actor, nodeID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, diff)
}

func (h *ProjectHandler) GenerateDocs(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var input projectservice.GenerateDocsInput
	_ = c.ShouldBindJSON(&input)
	result, err := h.svc.GenerateDocs(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, pkg.Response{Code: 0, Message: "accepted", Data: result})
}

func (h *ProjectHandler) actor(c *gin.Context) (projectservice.AccessContext, bool) {
	permissions, err := h.perm.ResolvePermissions(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "权限校验失败")
		return projectservice.AccessContext{}, false
	}
	return projectservice.NewAccessContext(authmiddleware.GetUserID(c), authmiddleware.IsSuperAdmin(c), permissions), true
}

func (h *ProjectHandler) requirementActor(c *gin.Context, globalPermission string, write bool) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	requirementID, ok := parseID(c, "requirementID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	actor, ok := h.actor(c)
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	if err := h.svc.CheckRequirementProject(actor, projectID, requirementID, globalPermission, write); err != nil {
		writeServiceError(c, err)
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, requirementID, actor, true
}

func (h *ProjectHandler) docActor(c *gin.Context, globalPermission string, write bool) (uint, uint, projectservice.AccessContext, bool) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	nodeID, ok := parseID(c, "nodeID")
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	actor, ok := h.actor(c)
	if !ok {
		return 0, 0, projectservice.AccessContext{}, false
	}
	if err := h.svc.CheckDocProject(actor, projectID, nodeID, globalPermission, write); err != nil {
		writeServiceError(c, err)
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, nodeID, actor, true
}

func parseID(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效 ID")
		return 0, false
	}
	return uint(value), true
}

// resolveProjectID 支持数字 ID 或项目 slug（供 docs/push、publish-path 开放 API）。
func (h *ProjectHandler) resolveProjectID(c *gin.Context) (uint, bool) {
	id, err := h.svc.ResolveProjectRef(c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return 0, false
	}
	return id, true
}

func parseOptionalID(c *gin.Context, name string) (*uint, bool) {
	value := strings.TrimSpace(c.PostForm(name))
	if value == "" {
		return nil, true
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		pkg.Error(c, http.StatusBadRequest, "无效父节点 ID")
		return nil, false
	}
	return new(uint(parsed)), true
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storageservice.ErrTooLarge):
		pkg.Error(c, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, storageservice.ErrUnavailable):
		pkg.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, projectservice.ErrAIDomainUnavailable):
		pkg.Error(c, http.StatusNotImplemented, err.Error())
	case projectservice.IsConflict(err):
		pkg.Error(c, http.StatusConflict, err.Error())
	case projectservice.IsForbidden(err):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case projectservice.IsNotFound(err), errors.Is(err, gorm.ErrRecordNotFound):
		pkg.Error(c, http.StatusNotFound, err.Error())
	default:
		pkg.Error(c, http.StatusBadRequest, fmt.Sprint(err))
	}
}
