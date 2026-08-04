package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	authmiddleware "bedrock/internal/auth/middleware"
	"bedrock/internal/pkg"
	projectservice "bedrock/internal/project/service"
)

func (h *ProjectHandler) ListDevDocTree(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.svc.ListDevDocTree(actor, projectID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *ProjectHandler) CreateDevDocNode(c *gin.Context) {
	projectID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var input projectservice.DevDocNodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效文档节点参数")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	node, err := h.svc.CreateDevDocNode(actor, projectID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, node)
}

func (h *ProjectHandler) GetDevDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.devDocActor(c, "project_dev_docs:view", false)
	if !ok {
		return
	}
	node, err := h.svc.GetDevDocNode(actor, nodeID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) UpdateDevDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.devDocActor(c, "project_dev_docs:update", true)
	if !ok {
		return
	}
	var input projectservice.DevDocNodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效文档节点参数")
		return
	}
	node, err := h.svc.UpdateDevDocNode(actor, nodeID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) MoveDevDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.devDocActor(c, "project_dev_docs:update", true)
	if !ok {
		return
	}
	var input projectservice.DevDocMoveInput
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效移动参数")
		return
	}
	node, err := h.svc.MoveDevDocNode(actor, nodeID, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) DeleteDevDocNode(c *gin.Context) {
	_, nodeID, actor, ok := h.devDocActor(c, "project_dev_docs:delete", true)
	if !ok {
		return
	}
	if err := h.svc.DeleteDevDocNode(actor, nodeID); err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"id": nodeID})
}

func (h *ProjectHandler) UploadDevMarkdown(c *gin.Context) {
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
	node, err := h.svc.UploadDevMarkdown(actor, projectID, parentID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, node)
}

func (h *ProjectHandler) ImportDevZIP(c *gin.Context) {
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
	items, err := h.svc.ImportDevZIP(actor, projectID, parentID, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), file, fileHeader.Size)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Created(c, gin.H{"items": items})
}

func (h *ProjectHandler) PushDevDocByPath(c *gin.Context) {
	if !h.requireDevDocsAuth(c, "dev_docs:write", "project_dev_docs:create") {
		return
	}
	projectID, ok := h.resolveProjectID(c)
	if !ok {
		return
	}
	var input struct {
		DocDir  string `json:"doc_dir"`
		DocName string `json:"doc_name"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		pkg.Error(c, http.StatusBadRequest, "无效推送参数")
		return
	}
	if strings.TrimSpace(input.DocName) == "" {
		pkg.Error(c, http.StatusBadRequest, "doc_name 不能为空")
		return
	}
	if input.Content == "" {
		pkg.Error(c, http.StatusBadRequest, "content 不能为空")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions["project_dev_docs:create"] = struct{}{}
	}
	node, created, err := h.svc.UpsertDevDocByPath(actor, projectID, input.DocDir, input.DocName, input.Content)
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

func (h *ProjectHandler) PullDevDocByPath(c *gin.Context) {
	if !h.requireDevDocsAuth(c, "dev_docs:read", "project_dev_docs:view") {
		return
	}
	projectID, ok := h.resolveProjectID(c)
	if !ok {
		return
	}
	docDir := c.Query("doc_dir")
	docName := strings.TrimSpace(c.Query("doc_name"))
	if docName == "" {
		pkg.Error(c, http.StatusBadRequest, "doc_name 不能为空")
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions["project_dev_docs:view"] = struct{}{}
	}
	node, err := h.svc.GetDevDocByPath(actor, projectID, docDir, docName)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, node)
}

func (h *ProjectHandler) ExportDevDocs(c *gin.Context) {
	if !h.requireDevDocsAuth(c, "dev_docs:read", "project_dev_docs:view") {
		return
	}
	projectID, ok := h.resolveProjectID(c)
	if !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if authmiddleware.IsPAT(c) {
		actor.Permissions["project_dev_docs:view"] = struct{}{}
	}
	items, err := h.svc.ExportDevDocs(actor, projectID, c.Query("doc_dir"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

// requireDevDocsAuth mirrors requireDocsAuth: PAT needs scope; JWT needs RBAC permission.
func (h *ProjectHandler) requireDevDocsAuth(c *gin.Context, patScope, rbacPermission string) bool {
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

func (h *ProjectHandler) devDocActor(c *gin.Context, globalPermission string, write bool) (uint, uint, projectservice.AccessContext, bool) {
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
	if err := h.svc.CheckDevDocProject(actor, projectID, nodeID, globalPermission, write); err != nil {
		writeServiceError(c, err)
		return 0, 0, projectservice.AccessContext{}, false
	}
	return projectID, nodeID, actor, true
}
