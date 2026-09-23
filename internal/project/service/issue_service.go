package service

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"
	storagemodel "bedrock/internal/storage/model"
	storageservice "bedrock/internal/storage/service"
	systemservice "bedrock/internal/system/service"

	"gorm.io/gorm"
)

// IssueService is the unified work-item service over project_issues
// (DESIGN D37–D39): type-aware CRUD, status transitions with dictionary
// validation, field-level activities, kanban aggregation, notifications and
// watchers.
type IssueService struct {
	issueRepo   *repository.IssueRepository
	projectRepo *repository.ProjectRepository
	storage     *storageservice.StorageService
	acl         *projectACL
	notify      *systemservice.NotificationService
}

func NewIssueService(issueRepo *repository.IssueRepository, projectRepo *repository.ProjectRepository, storage *storageservice.StorageService) *IssueService {
	return &IssueService{
		issueRepo:   issueRepo,
		projectRepo: projectRepo,
		storage:     storage,
		acl:         newProjectACL(projectRepo),
	}
}

// SetNotificationService wires the in-app notification push (phase 3).
func (s *IssueService) SetNotificationService(notify *systemservice.NotificationService) {
	s.notify = notify
}

type CreateIssueInput struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Severity     string `json:"severity"`
	Priority     string `json:"priority"`
	AssigneeID   *uint  `json:"assignee_id"`
	RepositoryID *uint  `json:"repository_id"`
	Branch       string `json:"branch"`
	Tags         string `json:"tags"`
	IterationID  *uint  `json:"iteration_id"`
}

type UpdateIssueInput struct {
	Title        *string `json:"title"`
	Description  *string `json:"description"`
	Status       *string `json:"status"`
	Severity     *string `json:"severity"`
	Priority     *string `json:"priority"`
	AssigneeID   *uint   `json:"assignee_id"`
	RepositoryID *uint   `json:"repository_id"`
	Branch       *string `json:"branch"`
	Tags         *string `json:"tags"`
	IterationID  *uint   `json:"iteration_id"`
}

type TransitionIssueStatusInput struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

type CreateIssueCommentInput struct {
	Content       string `json:"content"`
	MentionUserIDs []uint `json:"mention_user_ids"`
}

// KanbanColumn is one board column; cards use the unified issue shape.
type KanbanColumn struct {
	Status   string                `json:"status"`
	Label    string                `json:"label"`
	Terminal bool                  `json:"terminal"`
	Cards    []model.ProjectIssue  `json:"cards"`
}

type KanbanBoard struct {
	Columns []KanbanColumn `json:"columns"`
	Total   int            `json:"total"`
}

// issueDomain maps an issue type to its permission prefix and ACL capabilities.
func issueDomain(issueType string) (permPrefix string, view, edit, admin aclCapability) {
	if issueType == model.IssueTypeBug {
		return "project_bugs", capBugView, capBugEdit, capBugAdmin
	}
	return "project_requirements", capRequirementView, capRequirementEdit, capRequirementAdmin
}

// requireIssueProject verifies the issue exists, belongs to projectID, and
// the actor holds the type-mapped permission + ACL capability.
func (s *IssueService) requireIssueProject(actor AccessContext, projectID, issueID uint, action string) (*model.ProjectIssue, *model.ProjectMember, error) {
	issue, err := s.issueRepo.FindByID(issueID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, NewNotFound("工作项不存在")
	}
	if err != nil {
		return nil, nil, err
	}
	if issue.ProjectID != projectID {
		return nil, nil, NewNotFound("工作项不存在")
	}
	permPrefix, viewCap, editCap, adminCap := issueDomain(issue.Type)
	capability := editCap
	switch action {
	case "view":
		capability = viewCap
	case "delete":
		capability = adminCap
	}
	member, err := s.acl.Require(projectID, actor, permPrefix+":"+action, capability)
	if err != nil {
		return nil, nil, err
	}
	return issue, member, nil
}

// ResolveAssigneeRef resolves an assignee query value (username or user ID).
func (s *IssueService) ResolveAssigneeRef(ref string) (*uint, error) {
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

// validateStatus checks the value against the type's status dictionary,
// defaulting to the type's initial status when empty.
func (s *IssueService) validateStatus(issueType, value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return model.DefaultIssueStatus(issueType), nil
	}
	exists, err := s.projectRepo.IssueStatusExists(issueType, value)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", NewBadRequest("无效工作项状态")
	}
	return value, nil
}

func (s *IssueService) validateSeverity(issueType, value string) (string, error) {
	if issueType != model.IssueTypeBug {
		return "", nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return model.BugSeverityNormal, nil
	}
	if !model.IsValidBugSeverity(value) {
		return "", NewBadRequest("无效严重程度")
	}
	return value, nil
}

func normalizeIssuePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "normal", "high", "urgent":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "normal"
	}
}

func normalizeNullableID(id *uint) *uint {
	if id != nil && *id == 0 {
		return nil
	}
	return id
}

// ListAcrossProjects queries issues across projects with data scope enforcement.
func (s *IssueService) ListAcrossProjects(actor AccessContext, filter repository.IssueFilter, q pkg.ListQuery) ([]model.ProjectIssue, int64, error) {
	if !actor.Has("project_bugs:view") && !actor.Has("project_requirements:view") {
		return nil, 0, NewForbidden("缺少全局权限: project_bugs:view / project_requirements:view")
	}

	var projectIDs []uint
	if !actor.bypassProjectListFilter() {
		ids, err := s.projectRepo.ListUserProjectIDs(actor.UserID)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			return []model.ProjectIssue{}, 0, nil
		}
		projectIDs = ids
	}
	return s.issueRepo.ListAcrossProjects(projectIDs, filter, q)
}

// ListProjectIssues queries issues belonging to a single project.
func (s *IssueService) ListProjectIssues(actor AccessContext, projectID uint, filter repository.IssueFilter, q pkg.ListQuery) ([]model.ProjectIssue, int64, error) {
	issueType := strings.TrimSpace(filter.Type)
	if issueType == "" {
		// Mixed-type listing requires both domains' read permission.
		if !actor.Has("project_bugs:view") && !actor.Has("project_requirements:view") {
			return nil, 0, NewForbidden("缺少全局权限: project_bugs:view / project_requirements:view")
		}
		if _, err := s.acl.Require(projectID, actor, "project_projects:view", capProjectView); err != nil {
			return nil, 0, err
		}
		return s.issueRepo.ListByProject(projectID, filter, q)
	}
	if !model.IsValidIssueType(issueType) {
		return nil, 0, NewBadRequest("无效工作项类型")
	}
	permPrefix, viewCap, _, _ := issueDomain(issueType)
	if _, err := s.acl.Require(projectID, actor, permPrefix+":view", viewCap); err != nil {
		return nil, 0, err
	}
	return s.issueRepo.ListByProject(projectID, filter, q)
}

// CreateIssue validates input and creates a new issue within a project.
func (s *IssueService) CreateIssue(actor AccessContext, projectID uint, input CreateIssueInput) (*model.ProjectIssue, error) {
	issueType := strings.TrimSpace(input.Type)
	if !model.IsValidIssueType(issueType) {
		return nil, NewBadRequest("无效工作项类型")
	}
	permPrefix, _, editCap, _ := issueDomain(issueType)
	if _, err := s.acl.Require(projectID, actor, permPrefix+":create", editCap); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, NewBadRequest("工作项标题不能为空")
	}
	status, err := s.validateStatus(issueType, input.Status)
	if err != nil {
		return nil, err
	}
	severity, err := s.validateSeverity(issueType, input.Severity)
	if err != nil {
		return nil, err
	}

	issue := &model.ProjectIssue{
		ProjectID:    projectID,
		Type:         issueType,
		Title:        title,
		Description:  strings.TrimSpace(input.Description),
		Status:       status,
		Severity:     severity,
		Priority:     normalizeIssuePriority(input.Priority),
		AssigneeID:   normalizeNullableID(input.AssigneeID),
		RepositoryID: normalizeNullableID(input.RepositoryID),
		Branch:       strings.TrimSpace(input.Branch),
		Tags:         strings.TrimSpace(input.Tags),
		IterationID:  normalizeNullableID(input.IterationID),
		CreatedBy:    actor.UserID,
		UpdatedBy:    actor.UserID,
	}
	if err := s.issueRepo.Create(issue); err != nil {
		return nil, err
	}

	_ = s.issueRepo.RecordActivity(&model.ProjectIssueActivity{
		IssueID: issue.ID, Action: model.IssueActivityCreate, ToStatus: status,
		Comment: "创建工作项", CreatedBy: actor.UserID,
	})
	_ = s.issueRepo.AddWatcher(issue.ID, actor.UserID)

	s.notifyAssignment(actor, issue, nil)
	return s.issueRepo.FindByID(issue.ID)
}

// GetIssue retrieves an issue's details with the caller's watching flag.
func (s *IssueService) GetIssue(actor AccessContext, projectID, issueID uint) (*model.ProjectIssue, error) {
	issue, _, err := s.requireIssueProject(actor, projectID, issueID, "view")
	if err != nil {
		return nil, err
	}
	watching, err := s.issueRepo.IsWatching(issueID, actor.UserID)
	if err == nil {
		issue.Watching = watching
	}
	return issue, nil
}

// UpdateIssue updates specified fields, recording field-level activities.
func (s *IssueService) UpdateIssue(actor AccessContext, projectID, issueID uint, input UpdateIssueInput) (*model.ProjectIssue, error) {
	issue, _, err := s.requireIssueProject(actor, projectID, issueID, "update")
	if err != nil {
		return nil, err
	}

	type fieldChange struct {
		field, oldValue, newValue string
	}
	var changes []fieldChange
	record := func(field, oldValue, newValue string) {
		if oldValue != newValue {
			changes = append(changes, fieldChange{field, oldValue, newValue})
		}
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, NewBadRequest("工作项标题不能为空")
		}
		record("title", issue.Title, title)
		issue.Title = title
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		record("description", issue.Description, description)
		issue.Description = description
	}
	if input.Status != nil {
		status, err := s.validateStatus(issue.Type, *input.Status)
		if err != nil {
			return nil, err
		}
		if status != issue.Status {
			// Route status changes through the transition activity shape.
			if err := s.issueRepo.Update(issue); err != nil {
				return nil, err
			}
			return s.transitionStatus(actor, issue, status, "")
		}
	}
	if input.Severity != nil && issue.Type == model.IssueTypeBug {
		severity, err := s.validateSeverity(issue.Type, *input.Severity)
		if err != nil {
			return nil, err
		}
		record("severity", issue.Severity, severity)
		issue.Severity = severity
	}
	if input.Priority != nil {
		priority := normalizeIssuePriority(*input.Priority)
		record("priority", issue.Priority, priority)
		issue.Priority = priority
	}
	if input.AssigneeID != nil {
		oldAssignee := issue.AssigneeID
		issue.AssigneeID = normalizeNullableID(input.AssigneeID)
		record("assignee", assigneeLabel(oldAssignee), assigneeLabel(issue.AssigneeID))
		s.notifyAssignment(actor, issue, oldAssignee)
	}
	if input.RepositoryID != nil {
		issue.RepositoryID = normalizeNullableID(input.RepositoryID)
	}
	if input.Branch != nil {
		issue.Branch = strings.TrimSpace(*input.Branch)
	}
	if input.Tags != nil {
		tags := strings.TrimSpace(*input.Tags)
		record("tags", issue.Tags, tags)
		issue.Tags = tags
	}
	if input.IterationID != nil {
		issue.IterationID = normalizeNullableID(input.IterationID)
	}
	issue.UpdatedBy = actor.UserID

	if err := s.issueRepo.Update(issue); err != nil {
		return nil, err
	}
	for _, change := range changes {
		_ = s.issueRepo.RecordActivity(&model.ProjectIssueActivity{
			IssueID: issue.ID, Action: model.IssueActivityUpdate,
			Field: change.field, OldValue: change.oldValue, NewValue: change.newValue,
			CreatedBy: actor.UserID,
		})
	}
	return s.issueRepo.FindByID(issue.ID)
}

func assigneeLabel(id *uint) string {
	if id == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*id), 10)
}

// DeleteIssue removes an issue.
func (s *IssueService) DeleteIssue(actor AccessContext, projectID, issueID uint) error {
	_, _, err := s.requireIssueProject(actor, projectID, issueID, "delete")
	if err != nil {
		return err
	}
	return s.issueRepo.Delete(issueID)
}

// TransitionIssueStatus moves an issue between dictionary statuses.
func (s *IssueService) TransitionIssueStatus(actor AccessContext, projectID, issueID uint, input TransitionIssueStatusInput) (*model.ProjectIssue, error) {
	issue, _, err := s.requireIssueProject(actor, projectID, issueID, "update")
	if err != nil {
		return nil, err
	}
	targetStatus, err := s.validateStatus(issue.Type, input.Status)
	if err != nil {
		return nil, err
	}
	return s.transitionStatus(actor, issue, targetStatus, strings.TrimSpace(input.Comment))
}

func (s *IssueService) transitionStatus(actor AccessContext, issue *model.ProjectIssue, targetStatus, comment string) (*model.ProjectIssue, error) {
	fromStatus := issue.Status
	issue.Status = targetStatus
	issue.UpdatedBy = actor.UserID
	if err := s.issueRepo.Update(issue); err != nil {
		return nil, err
	}
	_ = s.issueRepo.RecordActivity(&model.ProjectIssueActivity{
		IssueID: issue.ID, Action: model.IssueActivityStatusChange,
		FromStatus: fromStatus, ToStatus: targetStatus, Comment: comment,
		CreatedBy: actor.UserID,
	})
	s.notifyStatusChange(actor, issue, fromStatus)
	return s.issueRepo.FindByID(issue.ID)
}

// ListIssueActivities returns all activity logs for an issue.
func (s *IssueService) ListIssueActivities(actor AccessContext, projectID, issueID uint) ([]model.ProjectIssueActivity, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return nil, err
	}
	return s.issueRepo.ListActivities(issueID)
}

// CountByStatus returns issue counts grouped by status for a type in a project.
func (s *IssueService) CountByStatus(actor AccessContext, projectID uint, issueType string) (map[string]int64, error) {
	if issueType == "" {
		if _, err := s.acl.Require(projectID, actor, "project_projects:view", capProjectView); err != nil {
			return nil, err
		}
		return s.issueRepo.CountByStatus(projectID, "")
	}
	permPrefix, viewCap, _, _ := issueDomain(issueType)
	if _, err := s.acl.Require(projectID, actor, permPrefix+":view", viewCap); err != nil {
		return nil, err
	}
	return s.issueRepo.CountByStatus(projectID, issueType)
}

// Kanban renders a board for a type: columns come from the status dictionary
// ordered by sort_order; terminal statuses are excluded by default (D38).
func (s *IssueService) Kanban(actor AccessContext, projectID *uint, issueType string, includeTerminal bool, filter repository.IssueFilter) (*KanbanBoard, error) {
	if !model.IsValidIssueType(issueType) {
		return nil, NewBadRequest("无效工作项类型")
	}
	permPrefix, viewCap, _, _ := issueDomain(issueType)

	var projectIDs []uint
	if projectID != nil {
		if _, err := s.acl.Require(*projectID, actor, permPrefix+":view", viewCap); err != nil {
			return nil, err
		}
		projectIDs = []uint{*projectID}
	} else {
		if !actor.Has(permPrefix + ":view") {
			return nil, NewForbidden("缺少全局权限: " + permPrefix + ":view")
		}
		if !actor.bypassProjectListFilter() {
			ids, err := s.projectRepo.ListUserProjectIDs(actor.UserID)
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				return s.emptyBoard(issueType, includeTerminal)
			}
			projectIDs = ids
		}
	}

	statuses, err := s.projectRepo.ListIssueStatuses(issueType)
	if err != nil {
		return nil, err
	}
	board := &KanbanBoard{Columns: make([]KanbanColumn, 0, len(statuses))}
	statusSet := make(map[string]bool, len(statuses))
	for _, st := range statuses {
		terminal := model.IsTerminalStatus(st.Value)
		if terminal && !includeTerminal {
			statusSet[st.Value] = false // excluded from cards, no column either
			continue
		}
		statusSet[st.Value] = true
		board.Columns = append(board.Columns, KanbanColumn{
			Status: st.Value, Label: st.Label, Terminal: terminal, Cards: []model.ProjectIssue{},
		})
	}

	filter.Type = issueType
	if !includeTerminal {
		filter.ExcludeTerminal = true
	}
	issues, err := s.issueRepo.ListForKanban(projectIDs, filter)
	if err != nil {
		return nil, err
	}
	columnByStatus := make(map[string]int, len(board.Columns))
	for i := range board.Columns {
		columnByStatus[board.Columns[i].Status] = i
	}
	for _, issue := range issues {
		if idx, ok := columnByStatus[issue.Status]; ok {
			board.Columns[idx].Cards = append(board.Columns[idx].Cards, issue)
			board.Total++
		}
	}
	return board, nil
}

func (s *IssueService) emptyBoard(issueType string, includeTerminal bool) (*KanbanBoard, error) {
	statuses, err := s.projectRepo.ListIssueStatuses(issueType)
	if err != nil {
		return nil, err
	}
	board := &KanbanBoard{Columns: make([]KanbanColumn, 0, len(statuses))}
	for _, st := range statuses {
		terminal := model.IsTerminalStatus(st.Value)
		if terminal && !includeTerminal {
			continue
		}
		board.Columns = append(board.Columns, KanbanColumn{
			Status: st.Value, Label: st.Label, Terminal: terminal, Cards: []model.ProjectIssue{},
		})
	}
	return board, nil
}

// Comments -----------------------------------------------------------------

// ListComments retrieves all comments for an issue.
func (s *IssueService) ListComments(actor AccessContext, projectID, issueID uint) ([]model.ProjectIssueComment, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return nil, err
	}
	return s.issueRepo.ListComments(issueID)
}

// CreateComment adds a new comment, records the activity, auto-watches the
// author, and notifies assignee/creator/watchers plus @mentions.
func (s *IssueService) CreateComment(actor AccessContext, projectID, issueID uint, input CreateIssueCommentInput) (*model.ProjectIssueComment, error) {
	issue, _, err := s.requireIssueProject(actor, projectID, issueID, "create")
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, NewBadRequest("评论内容不能为空")
	}

	mentions, err := s.resolveMentions(input.MentionUserIDs)
	if err != nil {
		return nil, err
	}
	mentionJSON := ""
	if len(mentions) > 0 {
		if raw, err := json.Marshal(mentions); err == nil {
			mentionJSON = string(raw)
		}
	}

	comment := &model.ProjectIssueComment{
		IssueID:    issueID,
		Content:    content,
		MentionIDs: mentionJSON,
		CreatedBy:  actor.UserID,
	}
	if err := s.issueRepo.CreateComment(comment); err != nil {
		return nil, err
	}

	_ = s.issueRepo.RecordActivity(&model.ProjectIssueActivity{
		IssueID: issueID, Action: model.IssueActivityComment,
		Comment: content, CreatedBy: actor.UserID,
	})
	_ = s.issueRepo.AddWatcher(issueID, actor.UserID)

	s.notifyComment(actor, issue, content, mentions)
	return s.issueRepo.FindCommentByID(comment.ID)
}

// resolveMentions dedupes mentioned user IDs (0 dropped).
func (s *IssueService) resolveMentions(ids []uint) ([]uint, error) {
	cleaned := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	return cleaned, nil
}

// UpdateComment modifies an existing comment with ownership check.
func (s *IssueService) UpdateComment(actor AccessContext, projectID, issueID, commentID uint, content string) (*model.ProjectIssueComment, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "update"); err != nil {
		return nil, err
	}
	member, err := s.commentOwnershipMember(actor, projectID, issueID, "update")
	if err != nil {
		return nil, err
	}
	comment, err := s.issueRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.IssueID != issueID {
		return nil, NewNotFound("评论不存在")
	}
	if err != nil {
		return nil, err
	}
	if !canEditComment(actor, member, comment.CreatedBy) {
		return nil, NewForbidden("只能编辑自己的评论")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, NewBadRequest("评论内容不能为空")
	}
	comment.Content = content
	if err := s.issueRepo.UpdateComment(comment); err != nil {
		return nil, err
	}
	return s.issueRepo.FindCommentByID(comment.ID)
}

// DeleteComment removes a comment with ownership check, purging its attachments.
func (s *IssueService) DeleteComment(actor AccessContext, projectID, issueID, commentID uint) error {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "delete"); err != nil {
		return err
	}
	member, err := s.commentOwnershipMember(actor, projectID, issueID, "delete")
	if err != nil {
		return err
	}
	comment, err := s.issueRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.IssueID != issueID {
		return NewNotFound("评论不存在")
	}
	if err != nil {
		return err
	}
	if !canEditComment(actor, member, comment.CreatedBy) {
		return NewForbidden("只能删除自己的评论")
	}

	atts, err := s.issueRepo.ListAttachmentsByCommentID(commentID)
	if err != nil {
		return err
	}
	for _, att := range atts {
		if err := s.issueRepo.DeleteAttachment(att.ID); err != nil {
			return err
		}
		if s.storage != nil {
			_ = s.storage.Delete(att.StorageObjectID)
		}
	}
	return s.issueRepo.DeleteComment(commentID)
}

func (s *IssueService) commentOwnershipMember(actor AccessContext, projectID, issueID uint, action string) (*model.ProjectMember, error) {
	issue, err := s.issueRepo.FindByID(issueID)
	if err != nil {
		return nil, NewNotFound("工作项不存在")
	}
	permPrefix, _, editCap, _ := issueDomain(issue.Type)
	return s.acl.Require(projectID, actor, permPrefix+":"+action, editCap)
}

func canEditComment(actor AccessContext, member *model.ProjectMember, commentOwner uint) bool {
	if actor.SuperAdmin || actor.Has("project_projects:manage_all") || commentOwner == actor.UserID {
		return true
	}
	return member != nil && (member.Role == model.ProjectRoleOwner || member.Role == model.ProjectRoleAdmin)
}

// Attachments ---------------------------------------------------------------

// ListAttachments retrieves all attachments belonging to an issue.
func (s *IssueService) ListAttachments(actor AccessContext, projectID, issueID uint) ([]model.ProjectIssueAttachment, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return nil, err
	}
	return s.issueRepo.ListAttachments(issueID)
}

// AddAttachment stores a file and links it to the issue.
func (s *IssueService) AddAttachment(actor AccessContext, projectID, issueID uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectIssueAttachment, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "update"); err != nil {
		return nil, err
	}
	return s.putAttachment(actor, issueID, nil, filename, contentType, source, size)
}

// AddCommentAttachment stores a file and links it to an issue comment.
func (s *IssueService) AddCommentAttachment(actor AccessContext, projectID, issueID, commentID uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectIssueAttachment, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "create"); err != nil {
		return nil, err
	}
	comment, err := s.issueRepo.FindCommentByID(commentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || comment == nil || comment.IssueID != issueID {
		return nil, NewNotFound("评论不存在")
	}
	if err != nil {
		return nil, err
	}
	return s.putAttachment(actor, issueID, &commentID, filename, contentType, source, size)
}

func (s *IssueService) putAttachment(actor AccessContext, issueID uint, commentID *uint, filename, contentType string, source io.Reader, size int64) (*model.ProjectIssueAttachment, error) {
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
	att := &model.ProjectIssueAttachment{
		IssueID:         issueID,
		CommentID:       commentID,
		StorageObjectID: object.ID,
		Filename:        filename,
		CreatedBy:       actor.UserID,
	}
	if err := s.issueRepo.CreateAttachment(att); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	return s.issueRepo.FindAttachmentByID(att.ID)
}

// OpenAttachment opens the attachment file stream for download.
func (s *IssueService) OpenAttachment(actor AccessContext, projectID, issueID, attachmentID uint) (*os.File, *model.ProjectIssueAttachment, string, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return nil, nil, "", err
	}
	att, err := s.issueRepo.FindAttachmentByID(attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || att == nil || att.IssueID != issueID {
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
func (s *IssueService) DeleteAttachment(actor AccessContext, projectID, issueID, attachmentID uint) error {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "update"); err != nil {
		return err
	}
	att, err := s.issueRepo.FindAttachmentByID(attachmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || att == nil || att.IssueID != issueID {
		return NewNotFound("附件不存在")
	}
	if err != nil {
		return err
	}
	if err := s.issueRepo.DeleteAttachment(attachmentID); err != nil {
		return err
	}
	if s.storage != nil {
		_ = s.storage.Delete(att.StorageObjectID)
	}
	return nil
}

// Watchers ------------------------------------------------------------------

// Watch adds the actor as a watcher of an issue.
func (s *IssueService) Watch(actor AccessContext, projectID, issueID uint) error {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return err
	}
	return s.issueRepo.AddWatcher(issueID, actor.UserID)
}

// Unwatch removes the actor from an issue's watchers.
func (s *IssueService) Unwatch(actor AccessContext, projectID, issueID uint) error {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return err
	}
	return s.issueRepo.RemoveWatcher(issueID, actor.UserID)
}

// Watching reports whether the actor watches an issue.
func (s *IssueService) Watching(actor AccessContext, projectID, issueID uint) (bool, error) {
	if _, _, err := s.requireIssueProject(actor, projectID, issueID, "view"); err != nil {
		return false, err
	}
	return s.issueRepo.IsWatching(issueID, actor.UserID)
}

// Notification hooks (D39) ----------------------------------------------------

func (s *IssueService) pushIssueNotification(userID uint, issue *model.ProjectIssue, event, title, message string) {
	if s.notify == nil || userID == 0 {
		return
	}
	s.notify.NotifyIssueEvent(userID, issue.ID, event, title, message)
}

func issueTypeLabel(issueType string) string {
	switch issueType {
	case model.IssueTypeBug:
		return "缺陷"
	case model.IssueTypeTask:
		return "任务"
	default:
		return "需求"
	}
}

func (s *IssueService) issueTitle(issue *model.ProjectIssue) string {
	label := issueTypeLabel(issue.Type)
	if issue.ProjectName != "" {
		return label + " · " + issue.ProjectName
	}
	return label
}

// notifyAssignment informs a newly assigned user (not the actor).
func (s *IssueService) notifyAssignment(actor AccessContext, issue *model.ProjectIssue, oldAssignee *uint) {
	if issue.AssigneeID == nil || *issue.AssigneeID == actor.UserID {
		return
	}
	if oldAssignee != nil && *oldAssignee == *issue.AssigneeID {
		return
	}
	s.pushIssueNotification(*issue.AssigneeID, issue, "issue_assigned",
		"你有一个新的工作项", s.issueTitle(issue)+": "+issue.Title)
}

// notifyStatusChange informs the assignee and creator (excluding the actor).
func (s *IssueService) notifyStatusChange(actor AccessContext, issue *model.ProjectIssue, fromStatus string) {
	message := s.issueTitle(issue) + " 「" + issue.Title + "」 " + fromStatus + " → " + issue.Status
	targets := map[uint]struct{}{}
	if issue.AssigneeID != nil {
		targets[*issue.AssigneeID] = struct{}{}
	}
	if issue.CreatedBy != 0 {
		targets[issue.CreatedBy] = struct{}{}
	}
	watchers, err := s.issueRepo.ListWatcherIDs(issue.ID)
	if err == nil {
		for _, id := range watchers {
			targets[id] = struct{}{}
		}
	}
	for id := range targets {
		if id == actor.UserID {
			continue
		}
		s.pushIssueNotification(id, issue, "issue_status", "工作项状态变更", message)
	}
}

// notifyComment informs assignee/creator/watchers and @mentioned users.
func (s *IssueService) notifyComment(actor AccessContext, issue *model.ProjectIssue, content string, mentions []uint) {
	targets := map[uint]struct{}{}
	if issue.AssigneeID != nil {
		targets[*issue.AssigneeID] = struct{}{}
	}
	if issue.CreatedBy != 0 {
		targets[issue.CreatedBy] = struct{}{}
	}
	watchers, err := s.issueRepo.ListWatcherIDs(issue.ID)
	if err == nil {
		for _, id := range watchers {
			targets[id] = struct{}{}
		}
	}
	for _, id := range mentions {
		targets[id] = struct{}{}
	}
	summary := content
	if len(summary) > 80 {
		summary = summary[:80] + "…"
	}
	for id := range targets {
		if id == actor.UserID {
			continue
		}
		s.pushIssueNotification(id, issue, "issue_comment", "工作项有新评论",
			s.issueTitle(issue)+" 「"+issue.Title+"」: "+summary)
	}
}

// ListStatusOptions returns the enabled status dictionary entries for a type.
func (s *IssueService) ListStatusOptions(issueType string) ([]model.RequirementStatusOption, error) {
	if !model.IsValidIssueType(issueType) {
		return nil, NewBadRequest("无效工作项类型")
	}
	return s.projectRepo.ListIssueStatuses(issueType)
}
