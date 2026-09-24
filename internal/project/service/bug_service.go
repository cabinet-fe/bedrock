package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"
	storagemodel "bedrock/internal/storage/model"
	storageservice "bedrock/internal/storage/service"

	"gorm.io/gorm"
)

type CreateBugInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Severity     string `json:"severity"`
	Priority     string `json:"priority"`
	AssigneeID   *uint  `json:"assignee_id"`
	RepositoryID *uint  `json:"repository_id"`
	Branch       string `json:"branch"`
}

type UpdateBugInput struct {
	Title        *string `json:"title"`
	Description  *string `json:"description"`
	Severity     *string `json:"severity"`
	Priority     *string `json:"priority"`
	AssigneeID   *uint   `json:"assignee_id"`
	RepositoryID *uint   `json:"repository_id"`
	Branch       *string `json:"branch"`
}

type TransitionBugStatusInput struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

type BugService struct {
	bugRepo     *repository.BugRepository
	projectRepo *repository.ProjectRepository
	storage     *storageservice.StorageService
	acl         *projectACL
}

func NewBugService(bugRepo *repository.BugRepository, projectRepo *repository.ProjectRepository, storage *storageservice.StorageService) *BugService {
	return &BugService{
		bugRepo:     bugRepo,
		projectRepo: projectRepo,
		storage:     storage,
		acl:         newProjectACL(projectRepo),
	}
}

// CheckBugProject verifies that a bug exists, belongs to projectID, and actor has required permissions.
// Read checks additionally enforce the collaborator scope: member/readonly
// roles only reach bugs they created or are assigned to.
func (s *BugService) CheckBugProject(actor AccessContext, projectID, bugID uint, globalPermission string, capability aclCapability) (*model.ProjectBug, error) {
	bug, err := s.bugRepo.FindByID(bugID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("缺陷不存在")
	}
	if err != nil {
		return nil, err
	}
	if bug.ProjectID != projectID {
		return nil, NewNotFound("缺陷不存在")
	}
	member, err := s.acl.Require(projectID, actor, globalPermission, capability)
	if err != nil {
		return nil, err
	}
	if capability == capBugView {
		if scope := issueReadScope(actor, member); scope != nil &&
			!issueInvolvesUser(bug.CreatedBy, bug.AssigneeID, *scope) {
			return nil, NewNotFound("缺陷不存在")
		}
	}
	return bug, nil
}

// ResolveAssigneeRef resolves an assignee query value (username or numeric user ID)
// to a user ID; empty input yields nil (no filter).
func (s *BugService) ResolveAssigneeRef(ref string) (*uint, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil && id > 0 {
		resolved := uint(id)
		return &resolved, nil
	}
	id, err := s.projectRepo.FindUserIDByUsername(ref)
	if err != nil {
		return nil, NewBadRequest("assignee 用户不存在")
	}
	return &id, nil
}

// ListAcrossProjects queries bugs across projects with data scope enforcement.
func (s *BugService) ListAcrossProjects(actor AccessContext, filter repository.BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	if !actor.Has("project_bugs:view") {
		return nil, 0, NewForbidden("缺少全局权限: project_bugs:view")
	}

	var projectIDs []uint
	if !actor.bypassProjectListFilter() {
		ids, err := s.projectRepo.ListUserProjectIDs(actor.UserID)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			return []model.ProjectBug{}, 0, nil
		}
		projectIDs = ids
		// Cross-project views are personal: collaborators only see their own
		// or assigned bugs in every listed project.
		userID := actor.UserID
		filter.ParticipantID = &userID
	}

	return s.bugRepo.ListAcrossProjects(projectIDs, filter, q)
}

// ListProjectBugs queries bugs belonging to a single project.
func (s *BugService) ListProjectBugs(actor AccessContext, projectID uint, filter repository.BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	member, err := s.acl.Require(projectID, actor, "project_bugs:view", capBugView)
	if err != nil {
		return nil, 0, err
	}
	filter.ParticipantID = issueReadScope(actor, member)
	return s.bugRepo.ListByProject(projectID, filter, q)
}

// CreateBug validates input and creates a new bug within a project.
func (s *BugService) CreateBug(actor AccessContext, projectID uint, input CreateBugInput) (*model.ProjectBug, error) {
	if _, err := s.acl.Require(projectID, actor, "project_bugs:create", capBugEdit); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, NewBadRequest("缺陷标题不能为空")
	}

	severity := strings.TrimSpace(input.Severity)
	if severity == "" {
		severity = model.BugSeverityNormal
	} else if !model.IsValidBugSeverity(severity) {
		return nil, NewBadRequest("无效严重程度")
	}

	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = model.BugPriorityNormal
	} else if !model.IsValidBugPriority(priority) {
		return nil, NewBadRequest("无效优先级")
	}

	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = model.BugStatusOpen
	} else {
		exists, err := s.projectRepo.IssueStatusExists(model.IssueTypeBug, status)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, NewBadRequest("无效缺陷状态")
		}
	}

	bug := &model.ProjectBug{
		ProjectID:    projectID,
		Title:        title,
		Description:  strings.TrimSpace(input.Description),
		Status:       status,
		Severity:     severity,
		Priority:     priority,
		AssigneeID:   input.AssigneeID,
		RepositoryID: input.RepositoryID,
		Branch:       strings.TrimSpace(input.Branch),
		CreatedBy:    actor.UserID,
		UpdatedBy:    actor.UserID,
	}

	if err := s.bugRepo.Create(bug); err != nil {
		return nil, err
	}

	activity := &model.ProjectBugActivity{
		BugID:     bug.ID,
		Action:    model.BugActivityCreate,
		ToStatus:  model.BugStatusOpen,
		Comment:   "创建缺陷",
		CreatedBy: actor.UserID,
	}
	_ = s.bugRepo.RecordActivity(activity)

	return s.bugRepo.FindByID(bug.ID)
}

// GetBug retrieves a bug's details.
func (s *BugService) GetBug(actor AccessContext, projectID, bugID uint) (*model.ProjectBug, error) {
	return s.CheckBugProject(actor, projectID, bugID, "project_bugs:view", capBugView)
}

// UpdateBug updates specified fields of a bug.
func (s *BugService) UpdateBug(actor AccessContext, projectID, bugID uint, input UpdateBugInput) (*model.ProjectBug, error) {
	bug, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:update", capBugEdit)
	if err != nil {
		return nil, err
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, NewBadRequest("缺陷标题不能为空")
		}
		bug.Title = title
	}
	if input.Description != nil {
		bug.Description = strings.TrimSpace(*input.Description)
	}
	if input.Severity != nil {
		sev := strings.TrimSpace(*input.Severity)
		if !model.IsValidBugSeverity(sev) {
			return nil, NewBadRequest("无效严重程度")
		}
		bug.Severity = sev
	}
	if input.Priority != nil {
		pri := strings.TrimSpace(*input.Priority)
		if !model.IsValidBugPriority(pri) {
			return nil, NewBadRequest("无效优先级")
		}
		bug.Priority = pri
	}
	if input.AssigneeID != nil {
		if *input.AssigneeID == 0 {
			bug.AssigneeID = nil
		} else {
			bug.AssigneeID = input.AssigneeID
		}
	}
	if input.RepositoryID != nil {
		if *input.RepositoryID == 0 {
			bug.RepositoryID = nil
		} else {
			bug.RepositoryID = input.RepositoryID
		}
	}
	if input.Branch != nil {
		bug.Branch = strings.TrimSpace(*input.Branch)
	}
	bug.UpdatedBy = actor.UserID

	if err := s.bugRepo.Update(bug); err != nil {
		return nil, err
	}

	return s.bugRepo.FindByID(bug.ID)
}

// DeleteBug removes a bug.
func (s *BugService) DeleteBug(actor AccessContext, projectID, bugID uint) error {
	bug, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:delete", capBugAdmin)
	if err != nil {
		return err
	}
	return s.bugRepo.Delete(bug.ID)
}

// TransitionBugStatus transitions a bug between the 5 valid states and records the activity.
func (s *BugService) TransitionBugStatus(actor AccessContext, projectID, bugID uint, input TransitionBugStatusInput) (*model.ProjectBug, error) {
	bug, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:update", capBugEdit)
	if err != nil {
		return nil, err
	}

	targetStatus := strings.TrimSpace(input.Status)
	if !model.IsValidBugStatus(targetStatus) {
		return nil, NewBadRequest("无效缺陷状态")
	}

	fromStatus := bug.Status
	bug.Status = targetStatus
	bug.UpdatedBy = actor.UserID

	if err := s.bugRepo.Update(bug); err != nil {
		return nil, err
	}

	activity := &model.ProjectBugActivity{
		BugID:      bug.ID,
		Action:     model.BugActivityStatusChange,
		FromStatus: fromStatus,
		ToStatus:   targetStatus,
		Comment:    strings.TrimSpace(input.Comment),
		CreatedBy:  actor.UserID,
	}
	_ = s.bugRepo.RecordActivity(activity)

	return s.bugRepo.FindByID(bug.ID)
}

// ListBugActivities returns all activity logs for a bug.
func (s *BugService) ListBugActivities(actor AccessContext, projectID, bugID uint) ([]model.ProjectBugActivity, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:view", capBugView); err != nil {
		return nil, err
	}
	return s.bugRepo.ListActivities(bugID)
}

// CountByStatus returns bug counts grouped by status.
func (s *BugService) CountByStatus(actor AccessContext, projectID uint) (map[string]int64, error) {
	member, err := s.acl.Require(projectID, actor, "project_bugs:view", capBugView)
	if err != nil {
		return nil, err
	}
	return s.bugRepo.CountByStatus(projectID, issueReadScope(actor, member))
}

// ListComments retrieves all comments for a bug.
func (s *BugService) ListComments(actor AccessContext, projectID, bugID uint) ([]model.ProjectBugComment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:view", capBugView); err != nil {
		return nil, err
	}
	return s.bugRepo.ListComments(bugID)
}

// CreateComment adds a new comment to a bug.
func (s *BugService) CreateComment(actor AccessContext, projectID, bugID uint, content string) (*model.ProjectBugComment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:create", capBugEdit); err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, NewBadRequest("评论内容不能为空")
	}

	comment := &model.ProjectBugComment{
		BugID:     bugID,
		Content:   content,
		CreatedBy: actor.UserID,
	}
	if err := s.bugRepo.CreateComment(comment); err != nil {
		return nil, err
	}

	activity := &model.ProjectBugActivity{
		BugID:     bugID,
		Action:    model.BugActivityComment,
		Comment:   content,
		CreatedBy: actor.UserID,
	}
	_ = s.bugRepo.RecordActivity(activity)

	return s.bugRepo.FindCommentByID(comment.ID)
}

// UpdateComment modifies an existing bug comment with ownership check.
func (s *BugService) UpdateComment(actor AccessContext, projectID, bugID, commentID uint, content string) (*model.ProjectBugComment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:update", capBugEdit); err != nil {
		return nil, err
	}

	member, err := s.acl.Require(projectID, actor, "project_bugs:update", capBugEdit)
	if err != nil {
		return nil, err
	}

	comment, err := s.bugRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.BugID != bugID {
		return nil, NewNotFound("评论不存在")
	}
	if err != nil {
		return nil, err
	}

	if !actor.SuperAdmin && !actor.Has("project_projects:manage_all") && comment.CreatedBy != actor.UserID &&
		(member == nil || (member.Role != model.ProjectRoleOwner && member.Role != model.ProjectRoleAdmin)) {
		return nil, NewForbidden("只能编辑自己的评论")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, NewBadRequest("评论内容不能为空")
	}

	comment.Content = content
	if err := s.bugRepo.UpdateComment(comment); err != nil {
		return nil, err
	}

	return s.bugRepo.FindCommentByID(comment.ID)
}

// DeleteComment removes a comment with ownership check.
func (s *BugService) DeleteComment(actor AccessContext, projectID, bugID, commentID uint) error {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:delete", capBugEdit); err != nil {
		return err
	}

	member, err := s.acl.Require(projectID, actor, "project_bugs:delete", capBugEdit)
	if err != nil {
		return err
	}

	comment, err := s.bugRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.BugID != bugID {
		return NewNotFound("评论不存在")
	}
	if err != nil {
		return err
	}

	if !actor.SuperAdmin && !actor.Has("project_projects:manage_all") && comment.CreatedBy != actor.UserID &&
		(member == nil || (member.Role != model.ProjectRoleOwner && member.Role != model.ProjectRoleAdmin)) {
		return NewForbidden("只能删除自己的评论")
	}

	// Cascade: purge comment-scoped attachments and their storage objects first.
	atts, err := s.bugRepo.ListAttachmentsByCommentID(commentID)
	if err != nil {
		return err
	}
	for _, att := range atts {
		if err := s.bugRepo.DeleteAttachment(att.ID); err != nil {
			return err
		}
		if s.storage != nil {
			_ = s.storage.Delete(att.StorageObjectID)
		}
	}

	return s.bugRepo.DeleteComment(commentID)
}

// ListAttachments retrieves all attachments belonging to a bug.
func (s *BugService) ListAttachments(actor AccessContext, projectID, bugID uint) ([]model.ProjectBugAttachment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:view", capBugView); err != nil {
		return nil, err
	}
	return s.bugRepo.ListAttachments(bugID)
}

// AddAttachment validates file size, type, stores the content in StorageService, and creates attachment record.
func (s *BugService) AddAttachment(actor AccessContext, projectID, bugID uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectBugAttachment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:update", capBugEdit); err != nil {
		return nil, err
	}
	return s.putAttachment(actor, bugID, nil, filename, contentType, source, size)
}

// AddCommentAttachment stores a file and links it to a bug comment.
func (s *BugService) AddCommentAttachment(actor AccessContext, projectID, bugID, commentID uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectBugAttachment, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:create", capBugEdit); err != nil {
		return nil, err
	}
	comment, err := s.bugRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.BugID != bugID {
		return nil, NewNotFound("评论不存在")
	}
	if err != nil {
		return nil, err
	}
	return s.putAttachment(actor, bugID, &commentID, filename, contentType, source, size)
}

// putAttachment validates and stores a file, then records the attachment row
// linked to the bug (and optionally the comment).
func (s *BugService) putAttachment(actor AccessContext, bugID uint, commentID *uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectBugAttachment, error) {
	filename = safeFilename(filename)
	if filename == "" {
		return nil, NewBadRequest("附件文件名不能为空")
	}

	if s.storage != nil && size > s.storage.MaxBytes(storagemodel.KindAttachment) {
		return nil, storageservice.ErrTooLarge
	}

	if !isAllowedBugAttachment(filename, contentType) {
		return nil, NewBadRequest("不支持的文件类型，仅支持上传图片或日志/文本/压缩包等附件")
	}

	if s.storage == nil {
		return nil, errors.New("存储服务未配置")
	}

	object, err := s.storage.Put(storagemodel.KindAttachment, contentType, source, size, actor.UserID)
	if err != nil {
		return nil, err
	}

	att := &model.ProjectBugAttachment{
		BugID:           bugID,
		CommentID:       commentID,
		StorageObjectID: object.ID,
		Filename:        filename,
		CreatedBy:       actor.UserID,
	}
	if err := s.bugRepo.CreateAttachment(att); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}

	return s.bugRepo.FindAttachmentByID(att.ID)
}

// OpenAttachment opens the attachment file stream for download.
func (s *BugService) OpenAttachment(actor AccessContext, projectID, bugID, attachmentID uint) (*os.File, *model.ProjectBugAttachment, string, error) {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:view", capBugView); err != nil {
		return nil, nil, "", err
	}

	att, err := s.bugRepo.FindAttachmentByID(attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || att == nil || att.BugID != bugID {
		return nil, nil, "", NewNotFound("附件不存在")
	}
	if err != nil {
		return nil, nil, "", err
	}

	if s.storage == nil {
		return nil, nil, "", errors.New("存储服务未配置")
	}

	file, object, err := s.storage.Open(att.StorageObjectID)
	if err != nil {
		return nil, nil, "", err
	}
	return file, att, object.ContentType, nil
}

// DeleteAttachment removes an attachment and purges the backing storage object.
func (s *BugService) DeleteAttachment(actor AccessContext, projectID, bugID, attachmentID uint) error {
	if _, err := s.CheckBugProject(actor, projectID, bugID, "project_bugs:update", capBugEdit); err != nil {
		return err
	}

	att, err := s.bugRepo.FindAttachmentByID(attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || att == nil || att.BugID != bugID {
		return NewNotFound("附件不存在")
	}
	if err != nil {
		return err
	}

	if err := s.bugRepo.DeleteAttachment(attachmentID); err != nil {
		return err
	}

	if s.storage != nil {
		_ = s.storage.Delete(att.StorageObjectID)
	}
	return nil
}

func isAllowedBugAttachment(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	allowedExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true, ".bmp": true,
		".log": true, ".txt": true, ".json": true, ".xml": true, ".yaml": true, ".yml": true, ".md": true,
		".csv": true, ".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".tgz": true,
	}
	if allowedExts[ext] {
		return true
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "text/") ||
		ct == "application/json" || ct == "application/xml" || ct == "application/pdf" ||
		ct == "application/zip" || ct == "application/gzip" || ct == "application/x-tar" {
		return true
	}
	return false
}
