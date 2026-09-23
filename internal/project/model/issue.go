package model

import (
	"time"

	"gorm.io/gorm"
)

// Issue type constants.
const (
	IssueTypeRequirement = "requirement"
	IssueTypeBug         = "bug"
	IssueTypeTask        = "task"
)

// Issue activity action constants.
const (
	IssueActivityCreate        = "create"
	IssueActivityComment       = "comment"
	IssueActivityStatusChange  = "status_change"
	IssueActivityUpdate        = "update"
	IssueActivityAssigneeField = "assignee"
)

// terminalStatuses is the convention-based terminal set (DESIGN D38):
// issues in these statuses are excluded from kanban boards by default.
// Custom dictionary statuses default to non-terminal.
var terminalStatuses = map[string]struct{}{
	BugStatusClosed:     {},
	BugStatusRejected:   {},
	"done":              {},
	RequirementStatusCancelled: {},
}

// Requirement status values seeded in the requirement_status dictionary.
const (
	RequirementStatusBacklog   = "backlog"
	RequirementStatusCancelled = "cancelled"
)

func IsValidIssueType(t string) bool {
	switch t {
	case IssueTypeRequirement, IssueTypeBug, IssueTypeTask:
		return true
	default:
		return false
	}
}

func IsTerminalStatus(status string) bool {
	_, ok := terminalStatuses[status]
	return ok
}

// TerminalStatuses returns the convention-based terminal status values.
func TerminalStatuses() []string {
	return []string{BugStatusClosed, BugStatusRejected, "done", RequirementStatusCancelled}
}

// StatusDictCode maps an issue type to its status dictionary code; task reuses
// the requirement dictionary (DESIGN D40).
func StatusDictCode(issueType string) string {
	if issueType == IssueTypeBug {
		return "bug_status"
	}
	return "requirement_status"
}

// DefaultIssueStatus is the initial status for a newly created issue.
func DefaultIssueStatus(issueType string) string {
	if issueType == IssueTypeBug {
		return BugStatusOpen
	}
	return RequirementStatusBacklog
}

// ProjectIssue is the unified work item (requirement / bug / task, DESIGN D37).
type ProjectIssue struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	ProjectID    uint           `json:"project_id" gorm:"not null;index"`
	Type         string         `json:"type" gorm:"size:20;not null;index"`
	Title        string         `json:"title" gorm:"size:500;not null"`
	Description  string         `json:"description" gorm:"type:text"`
	Status       string         `json:"status" gorm:"size:100;not null;index"`
	Severity     string         `json:"severity,omitempty" gorm:"size:30;not null;default:'';index"`
	Priority     string         `json:"priority" gorm:"size:30;not null;default:normal;index"`
	AssigneeID   *uint          `json:"assignee_id,omitempty" gorm:"index"`
	RepositoryID *uint          `json:"repository_id,omitempty" gorm:"index"`
	Branch       string         `json:"branch,omitempty" gorm:"size:255"`
	Tags         string         `json:"tags"`
	IterationID  *uint          `json:"iteration_id,omitempty" gorm:"index"`
	CreatedBy    uint           `json:"created_by" gorm:"index"`
	UpdatedBy    uint           `json:"updated_by" gorm:"index"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`

	// Non-persisted view fields
	ProjectName      string `json:"project_name,omitempty" gorm:"-"`
	AssigneeName     string `json:"assignee_name,omitempty" gorm:"-"`
	AssigneeUsername string `json:"assignee_username,omitempty" gorm:"-"`
	CreatorName      string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername  string `json:"creator_username,omitempty" gorm:"-"`
	RepositoryName   string `json:"repository_name,omitempty" gorm:"-"`
	CommentCount     int64  `json:"comment_count,omitempty" gorm:"-"`
	Watching         bool   `json:"watching,omitempty" gorm:"-"`
}

func (ProjectIssue) TableName() string { return "project_issues" }

// ProjectIssueComment is a user comment on an issue, optionally carrying
// @mentioned user IDs (JSON array) for notification and highlight.
type ProjectIssueComment struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	IssueID     uint           `json:"issue_id" gorm:"not null;index"`
	Content     string         `json:"content" gorm:"type:text;not null"`
	MentionIDs  string         `json:"mention_ids,omitempty" gorm:"size:500"`
	CreatedBy   uint           `json:"created_by" gorm:"index"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`

	// Non-persisted view fields
	CreatorName     string                 `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string                 `json:"creator_username,omitempty" gorm:"-"`
	Attachments     []ProjectIssueAttachment `json:"attachments,omitempty" gorm:"-"`
}

func (ProjectIssueComment) TableName() string { return "project_issue_comments" }

// ProjectIssueAttachment links an uploaded storage object to an issue, optionally
// scoped to a single comment (comment_id nil = issue-level attachment).
type ProjectIssueAttachment struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	IssueID         uint      `json:"issue_id" gorm:"not null;index"`
	CommentID       *uint     `json:"comment_id,omitempty" gorm:"index"`
	StorageObjectID uint      `json:"storage_object_id" gorm:"not null;index"`
	Filename        string    `json:"filename" gorm:"size:500;not null"`
	CreatedBy       uint      `json:"created_by" gorm:"index"`
	CreatedAt       time.Time `json:"created_at"`

	// Non-persisted view fields
	FileSize        int64  `json:"file_size,omitempty" gorm:"-"`
	ContentType     string `json:"content_type,omitempty" gorm:"-"`
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

func (ProjectIssueAttachment) TableName() string { return "project_issue_attachments" }

// ProjectIssueActivity logs lifecycle, transition, and field-level change
// events of an issue (DESIGN D37/D38).
type ProjectIssueActivity struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	IssueID    uint      `json:"issue_id" gorm:"not null;index"`
	Action     string    `json:"action" gorm:"size:50;not null;index"`
	Field      string    `json:"field,omitempty" gorm:"size:50"`
	OldValue   string    `json:"old_value,omitempty" gorm:"size:500"`
	NewValue   string    `json:"new_value,omitempty" gorm:"size:500"`
	FromStatus string    `json:"from_status,omitempty" gorm:"size:100"`
	ToStatus   string    `json:"to_status,omitempty" gorm:"size:100"`
	Comment    string    `json:"comment,omitempty" gorm:"type:text"`
	CreatedBy  uint      `json:"created_by" gorm:"index"`
	CreatedAt  time.Time `json:"created_at"`

	// Non-persisted view fields
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

func (ProjectIssueActivity) TableName() string { return "project_issue_activities" }

// ProjectIteration is a sprint-like container for planning work items
// (DESIGN D40). Issues without an iteration form the backlog.
type ProjectIteration struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	ProjectID uint           `json:"project_id" gorm:"not null;index"`
	Name      string         `json:"name" gorm:"size:200;not null"`
	Goal      string         `json:"goal,omitempty" gorm:"type:text"`
	StartDate string         `json:"start_date,omitempty" gorm:"size:10"`
	EndDate   string         `json:"end_date,omitempty" gorm:"size:10"`
	Status    string         `json:"status" gorm:"size:20;not null;default:planned;index"`
	CreatedBy uint           `json:"created_by" gorm:"index"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	// Non-persisted view fields
	IssueCounts map[string]int64 `json:"issue_counts,omitempty" gorm:"-"`
}

func (ProjectIteration) TableName() string { return "project_iterations" }

// Iteration status constants.
const (
	IterationStatusPlanned = "planned"
	IterationStatusActive  = "active"
	IterationStatusClosed  = "closed"
)

func IsValidIterationStatus(status string) bool {
	switch status {
	case IterationStatusPlanned, IterationStatusActive, IterationStatusClosed:
		return true
	default:
		return false
	}
}

// ProjectIssueWatcher records a user following an issue for notifications
// (DESIGN D39).
type ProjectIssueWatcher struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	IssueID   uint      `json:"issue_id" gorm:"not null;uniqueIndex:idx_issue_watcher"`
	UserID    uint      `json:"user_id" gorm:"not null;uniqueIndex:idx_issue_watcher;index"`
	CreatedAt time.Time `json:"created_at"`
}

func (ProjectIssueWatcher) TableName() string { return "project_issue_watchers" }
