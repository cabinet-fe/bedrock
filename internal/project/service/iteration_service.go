package service

import (
	"errors"
	"strings"
	"time"

	"bedrock/internal/project/model"
	"bedrock/internal/project/repository"

	"gorm.io/gorm"
)

// IterationService manages project iterations and burndown data (DESIGN D40).
type IterationService struct {
	issueRepo   *repository.IssueRepository
	projectRepo *repository.ProjectRepository
}

func NewIterationService(issueRepo *repository.IssueRepository, projectRepo *repository.ProjectRepository) *IterationService {
	return &IterationService{issueRepo: issueRepo, projectRepo: projectRepo}
}

type IterationInput struct {
	Name      string `json:"name"`
	Goal      string `json:"goal"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Status    string `json:"status"`
}

type BurndownPoint struct {
	Date      string `json:"date"`
	Remaining int    `json:"remaining"`
}

type BurndownChart struct {
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"`
	Total     int             `json:"total"`
	Points    []BurndownPoint `json:"points"`
}

func (s *IterationService) requireIteration(actor AccessContext, projectID, iterationID uint) (*model.ProjectIteration, error) {
	iteration, err := s.projectRepo.FindIteration(iterationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("迭代不存在")
	}
	if err != nil {
		return nil, err
	}
	if iteration.ProjectID != projectID {
		return nil, NewNotFound("迭代不存在")
	}
	return iteration, nil
}

// ListIterations returns a project's iterations with per-status issue counts.
func (s *IterationService) ListIterations(actor AccessContext, projectID uint) ([]model.ProjectIteration, error) {
	if _, err := s.acl(projectID, actor); err != nil {
		return nil, err
	}
	iterations, err := s.projectRepo.ListIterations(projectID)
	if err != nil {
		return nil, err
	}
	for i := range iterations {
		counts, err := s.issueRepo.CountByStatusAndIteration(iterations[i].ID)
		if err != nil {
			return nil, err
		}
		iterations[i].IssueCounts = counts
	}
	return iterations, nil
}

func (s *IterationService) acl(projectID uint, actor AccessContext) (*model.ProjectMember, error) {
	a := newProjectACL(s.projectRepo)
	return a.Require(projectID, actor, "project_projects:view", capProjectView)
}

func (s *IterationService) aclManage(projectID uint, actor AccessContext) (*model.ProjectMember, error) {
	a := newProjectACL(s.projectRepo)
	return a.Require(projectID, actor, "project_projects:update", capMemberManage)
}

// CreateIteration adds a planned iteration to a project.
func (s *IterationService) CreateIteration(actor AccessContext, projectID uint, input IterationInput) (*model.ProjectIteration, error) {
	if _, err := s.aclManage(projectID, actor); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, NewBadRequest("迭代名称不能为空")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = model.IterationStatusPlanned
	}
	if !model.IsValidIterationStatus(status) {
		return nil, NewBadRequest("无效迭代状态")
	}
	iteration := &model.ProjectIteration{
		ProjectID: projectID, Name: name, Goal: strings.TrimSpace(input.Goal),
		StartDate: strings.TrimSpace(input.StartDate), EndDate: strings.TrimSpace(input.EndDate),
		Status: status, CreatedBy: actor.UserID,
	}
	if err := s.projectRepo.CreateIteration(iteration); err != nil {
		return nil, err
	}
	return iteration, nil
}

// UpdateIteration updates fields and/or moves an iteration through its
// lifecycle (planned → active → closed).
func (s *IterationService) UpdateIteration(actor AccessContext, projectID, iterationID uint, input IterationInput) (*model.ProjectIteration, error) {
	iteration, err := s.requireIteration(actor, projectID, iterationID)
	if err != nil {
		return nil, err
	}
	if _, err := s.aclManage(projectID, actor); err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		iteration.Name = name
	}
	if input.Goal != "" {
		iteration.Goal = strings.TrimSpace(input.Goal)
	}
	if input.StartDate != "" {
		iteration.StartDate = strings.TrimSpace(input.StartDate)
	}
	if input.EndDate != "" {
		iteration.EndDate = strings.TrimSpace(input.EndDate)
	}
	if status := strings.TrimSpace(input.Status); status != "" {
		if !model.IsValidIterationStatus(status) {
			return nil, NewBadRequest("无效迭代状态")
		}
		if model.IterationStatusClosed == status && iteration.Status != model.IterationStatusClosed {
			// Closing an iteration returns its issues to the backlog.
			if err := s.issueRepo.ClearIteration(iterationID); err != nil {
				return nil, err
			}
		}
		iteration.Status = status
	}
	if err := s.projectRepo.UpdateIteration(iteration); err != nil {
		return nil, err
	}
	return iteration, nil
}

// DeleteIteration removes an iteration that has no issues attached.
func (s *IterationService) DeleteIteration(actor AccessContext, projectID, iterationID uint) error {
	iteration, err := s.requireIteration(actor, projectID, iterationID)
	if err != nil {
		return err
	}
	if _, err := s.aclManage(projectID, actor); err != nil {
		return err
	}
	counts, err := s.issueRepo.CountByStatusAndIteration(iterationID)
	if err != nil {
		return err
	}
	total := int64(0)
	for _, c := range counts {
		total += c
	}
	if total > 0 {
		return NewConflict("迭代内仍有工作项，请先移出或关闭迭代")
	}
	_ = iteration
	return s.projectRepo.DeleteIteration(iterationID)
}

// Burndown reconstructs per-day remaining counts for an active or closed
// iteration from issue activities (DESIGN D40: no daily snapshot table).
func (s *IterationService) Burndown(actor AccessContext, projectID, iterationID uint) (*BurndownChart, error) {
	iteration, err := s.requireIteration(actor, projectID, iterationID)
	if err != nil {
		return nil, err
	}
	if _, err := s.acl(projectID, actor); err != nil {
		return nil, err
	}

	issues, err := s.issueRepo.ListByIteration(iterationID)
	if err != nil {
		return nil, err
	}

	startDate, err := parseIterationDate(iteration.StartDate, iteration.CreatedAt)
	if err != nil {
		return nil, err
	}
	endDate, err := parseIterationDate(iteration.EndDate, time.Now())
	if err != nil {
		return nil, err
	}
	if endDate.Before(startDate) {
		endDate = startDate
	}
	today := time.Now().Truncate(24 * time.Hour)
	if endDate.After(today) {
		endDate = today
	}

	activities, err := s.issueRepo.ListStatusTransitions(projectID, "")
	if err != nil {
		return nil, err
	}
	// Timeline per issue: status after each timestamp.
	timelines := make(map[uint][]model.ProjectIssueActivity)
	for _, a := range activities {
		timelines[a.IssueID] = append(timelines[a.IssueID], a)
	}
	statusAt := func(issueID uint, at time.Time) (string, bool) {
		status := ""
		found := false
		for _, a := range timelines[issueID] {
			if !a.CreatedAt.After(at) {
				if a.Action == model.IssueActivityCreate {
					status = a.ToStatus
					found = true
				} else if a.Action == model.IssueActivityStatusChange {
					status = a.ToStatus
					found = true
				}
			}
		}
		return status, found
	}

	chart := &BurndownChart{
		StartDate: startDate.Format("2006-01-02"),
		EndDate:   endDate.Format("2006-01-02"),
		Total:     len(issues),
		Points:    []BurndownPoint{},
	}
	for day := startDate; !day.After(endDate); day = day.AddDate(0, 0, 1) {
		endOfDay := day.Add(24*time.Hour - time.Second)
		remaining := 0
		for _, issue := range issues {
			status, ok := statusAt(issue.ID, endOfDay)
			if ok && !model.IsTerminalStatus(status) {
				remaining++
			} else if !ok && !model.IsTerminalStatus(issue.Status) {
				// No timeline (legacy rows): fall back to the current status
				// for the whole range.
				remaining++
			}
		}
		chart.Points = append(chart.Points, BurndownPoint{Date: day.Format("2006-01-02"), Remaining: remaining})
	}
	return chart, nil
}

func parseIterationDate(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback.Truncate(24 * time.Hour), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, NewBadRequest("日期格式应为 YYYY-MM-DD")
	}
	return parsed, nil
}
