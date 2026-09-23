package model

import (
	"time"
)

// Bug status constants (values seeded in the bug_status dictionary).
const (
	BugStatusOpen       = "open"
	BugStatusInProgress = "in_progress"
	BugStatusResolved   = "resolved"
	BugStatusClosed     = "closed"
	BugStatusRejected   = "rejected"
)

// Bug severity constants.
const (
	BugSeverityLow      = "low"
	BugSeverityNormal   = "normal"
	BugSeverityHigh     = "high"
	BugSeverityCritical = "critical"
)

// Bug priority constants.
const (
	BugPriorityLow    = "low"
	BugPriorityNormal = "normal"
	BugPriorityHigh   = "high"
	BugPriorityUrgent = "urgent"
)

// Bug activity action constants (legacy values kept for compatibility).
const (
	BugActivityStatusChange = "status_change"
	BugActivityCreate       = "create"
	BugActivityComment      = "comment"
)

// ProjectBug and friends are legacy wire DTOs for the /bugs compatibility
// aliases; persistence lives in ProjectIssue (gorm:"-" prevents accidental
// table binding).
type ProjectBug struct {
	ID           uint      `json:"id" gorm:"-"`
	ProjectID    uint      `json:"project_id"`
	Title        string    `json:"title" gorm:"-"`
	Description  string    `json:"description"`
	Status       string    `json:"status"`
	Severity     string    `json:"severity"`
	Priority     string    `json:"priority"`
	AssigneeID   *uint     `json:"assignee_id,omitempty"`
	RepositoryID *uint     `json:"repository_id,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	CreatedBy    uint      `json:"created_by"`
	UpdatedBy    uint      `json:"updated_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Non-persisted view fields
	ProjectName      string `json:"project_name,omitempty" gorm:"-"`
	AssigneeName     string `json:"assignee_name,omitempty" gorm:"-"`
	AssigneeUsername string `json:"assignee_username,omitempty" gorm:"-"`
	CreatorName      string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername  string `json:"creator_username,omitempty" gorm:"-"`
	RepositoryName   string `json:"repository_name,omitempty" gorm:"-"`
}

// ProjectBugComment is a legacy wire DTO for bug comments.
type ProjectBugComment struct {
	ID        uint      `json:"id" gorm:"-"`
	BugID     uint      `json:"bug_id"`
	Content   string    `json:"content"`
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Non-persisted view fields
	CreatorName     string                 `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string                 `json:"creator_username,omitempty" gorm:"-"`
	Attachments     []ProjectBugAttachment `json:"attachments,omitempty" gorm:"-"`
}

// ProjectBugAttachment is a legacy wire DTO for bug attachments.
type ProjectBugAttachment struct {
	ID              uint      `json:"id" gorm:"-"`
	BugID           uint      `json:"bug_id"`
	CommentID       *uint     `json:"comment_id,omitempty"`
	StorageObjectID uint      `json:"storage_object_id"`
	Filename        string    `json:"filename"`
	CreatedBy       uint      `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`

	// Non-persisted view fields
	FileSize        int64  `json:"file_size,omitempty" gorm:"-"`
	ContentType     string `json:"content_type,omitempty" gorm:"-"`
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

// ProjectBugActivity is a legacy wire DTO for bug activities.
type ProjectBugActivity struct {
	ID         uint      `json:"id" gorm:"-"`
	BugID      uint      `json:"bug_id"`
	Action     string    `json:"action"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status,omitempty"`
	Comment    string    `json:"comment,omitempty"`
	CreatedBy  uint      `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`

	// Non-persisted view fields
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

// IsValidBugStatus checks whether the given status string is valid.
func IsValidBugStatus(status string) bool {
	switch status {
	case BugStatusOpen, BugStatusInProgress, BugStatusResolved, BugStatusClosed, BugStatusRejected:
		return true
	default:
		return false
	}
}

// IsValidBugSeverity checks whether the given severity string is valid.
func IsValidBugSeverity(sev string) bool {
	switch sev {
	case BugSeverityLow, BugSeverityNormal, BugSeverityHigh, BugSeverityCritical:
		return true
	default:
		return false
	}
}

// IsValidBugPriority checks whether the given priority string is valid.
func IsValidBugPriority(pri string) bool {
	switch pri {
	case BugPriorityLow, BugPriorityNormal, BugPriorityHigh, BugPriorityUrgent:
		return true
	default:
		return false
	}
}
