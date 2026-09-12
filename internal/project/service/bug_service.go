package service

import (
	"errors"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"

	"gorm.io/gorm"
)

type CreateBugInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
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
	acl         *projectACL
}

func NewBugService(bugRepo *repository.BugRepository, projectRepo *repository.ProjectRepository) *BugService {
	return &BugService{
		bugRepo:     bugRepo,
		projectRepo: projectRepo,
		acl:         newProjectACL(projectRepo),
	}
}

// CheckBugProject verifies that a bug exists, belongs to projectID, and actor has required permissions.
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
	if _, err := s.acl.Require(projectID, actor, globalPermission, capability); err != nil {
		return nil, err
	}
	return bug, nil
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
	}

	return s.bugRepo.ListAcrossProjects(projectIDs, filter, q)
}

// ListProjectBugs queries bugs belonging to a single project.
func (s *BugService) ListProjectBugs(actor AccessContext, projectID uint, filter repository.BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	if _, err := s.acl.Require(projectID, actor, "project_bugs:view", capBugView); err != nil {
		return nil, 0, err
	}
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

	bug := &model.ProjectBug{
		ProjectID:    projectID,
		Title:        title,
		Description:  strings.TrimSpace(input.Description),
		Status:       model.BugStatusOpen,
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
	if _, err := s.acl.Require(projectID, actor, "project_bugs:view", capBugView); err != nil {
		return nil, err
	}
	return s.bugRepo.CountByStatus(projectID)
}
