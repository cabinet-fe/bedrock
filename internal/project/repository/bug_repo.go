package repository

import (
	"bedrock/internal/pkg"
	"bedrock/internal/project/model"

	"gorm.io/gorm"
)

// BugRepository is the type=bug facade over the unified IssueRepository
// (DESIGN D37). It preserves the legacy surface used by BugService so the
// /bugs compatibility aliases and their tests keep working.
type BugRepository struct {
	issues *IssueRepository
}

func NewBugRepository(db *gorm.DB) *BugRepository {
	return &BugRepository{issues: NewIssueRepository(db)}
}

// BugFilter encapsulates query parameters for bug searches.
type BugFilter struct {
	Keyword       string
	ProjectID     *uint
	Status        string
	Severity      string
	Priority      string
	AssigneeID    *uint
	ParticipantID *uint
	ExcludeClosed bool
}

func bugToIssueFilter(filter BugFilter) IssueFilter {
	return IssueFilter{
		Keyword:       filter.Keyword,
		ProjectID:     filter.ProjectID,
		Type:          model.IssueTypeBug,
		Status:        filter.Status,
		Severity:      filter.Severity,
		Priority:      filter.Priority,
		AssigneeID:    filter.AssigneeID,
		ParticipantID: filter.ParticipantID,
		ExcludeClosed: filter.ExcludeClosed,
	}
}

func issueToBug(issue model.ProjectIssue) model.ProjectBug {
	return model.ProjectBug{
		ID: issue.ID, ProjectID: issue.ProjectID,
		Title: issue.Title, Description: issue.Description,
		Status: issue.Status, Severity: issue.Severity, Priority: issue.Priority,
		AssigneeID: issue.AssigneeID, RepositoryID: issue.RepositoryID, Branch: issue.Branch,
		CreatedBy: issue.CreatedBy, UpdatedBy: issue.UpdatedBy,
		CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt,
		ProjectName: issue.ProjectName, AssigneeName: issue.AssigneeName,
		AssigneeUsername: issue.AssigneeUsername, CreatorName: issue.CreatorName,
		CreatorUsername: issue.CreatorUsername, RepositoryName: issue.RepositoryName,
	}
}

func bugToIssue(bug model.ProjectBug) model.ProjectIssue {
	return model.ProjectIssue{
		ID: bug.ID, ProjectID: bug.ProjectID, Type: model.IssueTypeBug,
		Title: bug.Title, Description: bug.Description,
		Status: bug.Status, Severity: bug.Severity, Priority: bug.Priority,
		AssigneeID: bug.AssigneeID, RepositoryID: bug.RepositoryID, Branch: bug.Branch,
		CreatedBy: bug.CreatedBy, UpdatedBy: bug.UpdatedBy,
		CreatedAt: bug.CreatedAt, UpdatedAt: bug.UpdatedAt,
	}
}

func issueToBugList(issues []model.ProjectIssue) []model.ProjectBug {
	bugs := make([]model.ProjectBug, 0, len(issues))
	for _, issue := range issues {
		bugs = append(bugs, issueToBug(issue))
	}
	return bugs
}

func (r *BugRepository) Create(bug *model.ProjectBug) error {
	issue := bugToIssue(*bug)
	if err := r.issues.Create(&issue); err != nil {
		return err
	}
	*bug = issueToBug(issue)
	return nil
}

func (r *BugRepository) FindByID(id uint) (*model.ProjectBug, error) {
	issue, err := r.issues.FindByID(id)
	if err != nil {
		return nil, err
	}
	if issue.Type != model.IssueTypeBug {
		return nil, gorm.ErrRecordNotFound
	}
	bug := issueToBug(*issue)
	return &bug, nil
}

func (r *BugRepository) Update(bug *model.ProjectBug) error {
	issue := bugToIssue(*bug)
	if err := r.issues.Update(&issue); err != nil {
		return err
	}
	*bug = issueToBug(issue)
	return nil
}

func (r *BugRepository) Delete(id uint) error {
	return r.issues.Delete(id)
}

// ListByProject queries bugs within a specific project.
func (r *BugRepository) ListByProject(projectID uint, filter BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	issues, total, err := r.issues.ListByProject(projectID, bugToIssueFilter(filter), q)
	if err != nil {
		return nil, 0, err
	}
	return issueToBugList(issues), total, nil
}

// ListAcrossProjects queries bugs across projects, optionally scoped to accessible projectIDs.
func (r *BugRepository) ListAcrossProjects(projectIDs []uint, filter BugFilter, q pkg.ListQuery) ([]model.ProjectBug, int64, error) {
	issues, total, err := r.issues.ListAcrossProjects(projectIDs, bugToIssueFilter(filter), q)
	if err != nil {
		return nil, 0, err
	}
	return issueToBugList(issues), total, nil
}

func (r *BugRepository) RecordActivity(activity *model.ProjectBugActivity) error {
	return r.issues.RecordActivity(&model.ProjectIssueActivity{
		IssueID: activity.BugID, Action: activity.Action,
		FromStatus: activity.FromStatus, ToStatus: activity.ToStatus,
		Comment: activity.Comment, CreatedBy: activity.CreatedBy, CreatedAt: activity.CreatedAt,
	})
}

func (r *BugRepository) ListActivities(bugID uint) ([]model.ProjectBugActivity, error) {
	activities, err := r.issues.ListActivities(bugID)
	if err != nil {
		return nil, err
	}
	result := make([]model.ProjectBugActivity, 0, len(activities))
	for _, a := range activities {
		result = append(result, model.ProjectBugActivity{
			ID: a.ID, BugID: a.IssueID, Action: a.Action,
			FromStatus: a.FromStatus, ToStatus: a.ToStatus, Comment: a.Comment,
			CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
			CreatorName: a.CreatorName, CreatorUsername: a.CreatorUsername,
		})
	}
	return result, nil
}

func (r *BugRepository) CountByStatus(projectID uint, participantID *uint) (map[string]int64, error) {
	return r.issues.CountByStatus(projectID, model.IssueTypeBug, participantID)
}

// CreateComment persists a new bug comment.
func (r *BugRepository) CreateComment(comment *model.ProjectBugComment) error {
	issueComment := &model.ProjectIssueComment{
		IssueID: comment.BugID, Content: comment.Content, CreatedBy: comment.CreatedBy,
		CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
	}
	if err := r.issues.CreateComment(issueComment); err != nil {
		return err
	}
	*comment = *issueCommentToBug(*issueComment)
	return nil
}

// FindCommentByID retrieves a bug comment by ID with creator info attached.
func (r *BugRepository) FindCommentByID(id uint) (*model.ProjectBugComment, error) {
	comment, err := r.issues.FindCommentByID(id)
	if err != nil {
		return nil, err
	}
	result := issueCommentToBug(*comment)
	return result, nil
}

// ListComments retrieves all comments for a bug in ascending chronological order.
func (r *BugRepository) ListComments(bugID uint) ([]model.ProjectBugComment, error) {
	comments, err := r.issues.ListComments(bugID)
	if err != nil {
		return nil, err
	}
	result := make([]model.ProjectBugComment, 0, len(comments))
	for _, c := range comments {
		result = append(result, *issueCommentToBug(c))
	}
	return result, nil
}

func issueCommentToBug(c model.ProjectIssueComment) *model.ProjectBugComment {
	comment := model.ProjectBugComment{
		ID: c.ID, BugID: c.IssueID, Content: c.Content,
		CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		CreatorName: c.CreatorName, CreatorUsername: c.CreatorUsername,
	}
	if len(c.Attachments) > 0 {
		comment.Attachments = make([]model.ProjectBugAttachment, 0, len(c.Attachments))
		for _, a := range c.Attachments {
			comment.Attachments = append(comment.Attachments, *issueAttachmentToBug(a))
		}
	}
	return &comment
}

// UpdateComment updates an existing bug comment.
func (r *BugRepository) UpdateComment(comment *model.ProjectBugComment) error {
	issueComment := &model.ProjectIssueComment{
		ID: comment.ID, IssueID: comment.BugID, Content: comment.Content,
		CreatedBy: comment.CreatedBy, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
	}
	if err := r.issues.UpdateComment(issueComment); err != nil {
		return err
	}
	*comment = *issueCommentToBug(*issueComment)
	return nil
}

// DeleteComment removes a bug comment.
func (r *BugRepository) DeleteComment(id uint) error {
	return r.issues.DeleteComment(id)
}

// CreateAttachment records a new bug attachment metadata record.
func (r *BugRepository) CreateAttachment(att *model.ProjectBugAttachment) error {
	issueAtt := issueAttachmentFromBug(att)
	if err := r.issues.CreateAttachment(issueAtt); err != nil {
		return err
	}
	*att = *issueAttachmentToBug(*issueAtt)
	return nil
}

func issueAttachmentFromBug(att *model.ProjectBugAttachment) *model.ProjectIssueAttachment {
	return &model.ProjectIssueAttachment{
		ID: att.ID, IssueID: att.BugID, CommentID: att.CommentID,
		StorageObjectID: att.StorageObjectID, Filename: att.Filename,
		CreatedBy: att.CreatedBy, CreatedAt: att.CreatedAt,
	}
}

func issueAttachmentToBug(att model.ProjectIssueAttachment) *model.ProjectBugAttachment {
	return &model.ProjectBugAttachment{
		ID: att.ID, BugID: att.IssueID, CommentID: att.CommentID,
		StorageObjectID: att.StorageObjectID, Filename: att.Filename,
		CreatedBy: att.CreatedBy, CreatedAt: att.CreatedAt,
		FileSize: att.FileSize, ContentType: att.ContentType,
		CreatorName: att.CreatorName, CreatorUsername: att.CreatorUsername,
	}
}

// FindAttachmentByID retrieves a bug attachment by ID with storage metadata and creator info.
func (r *BugRepository) FindAttachmentByID(id uint) (*model.ProjectBugAttachment, error) {
	att, err := r.issues.FindAttachmentByID(id)
	if err != nil {
		return nil, err
	}
	return issueAttachmentToBug(*att), nil
}

// ListAttachments retrieves all attachments associated with a bug.
func (r *BugRepository) ListAttachments(bugID uint) ([]model.ProjectBugAttachment, error) {
	atts, err := r.issues.ListAttachments(bugID)
	if err != nil {
		return nil, err
	}
	result := make([]model.ProjectBugAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, *issueAttachmentToBug(a))
	}
	return result, nil
}

// DeleteAttachment removes a bug attachment metadata record.
func (r *BugRepository) DeleteAttachment(id uint) error {
	return r.issues.DeleteAttachment(id)
}

// ListAttachmentsByCommentID returns raw attachment records linked to a comment.
func (r *BugRepository) ListAttachmentsByCommentID(commentID uint) ([]model.ProjectBugAttachment, error) {
	atts, err := r.issues.ListAttachmentsByCommentID(commentID)
	if err != nil {
		return nil, err
	}
	result := make([]model.ProjectBugAttachment, 0, len(atts))
	for _, a := range atts {
		result = append(result, *issueAttachmentToBug(a))
	}
	return result, nil
}
