package model

import (
	"time"

	"gorm.io/gorm"
)

// Bug status constants.
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

// Bug activity action constants.
const (
	BugActivityStatusChange  = "status_change"
	BugActivityCreate        = "create"
	BugActivityComment       = "comment"
	BugActivityAgentDispatch = "agent_dispatch"
)

// ProjectBug represents a bug entity in a project.
type ProjectBug struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	ProjectID      uint           `json:"project_id" gorm:"not null;index"`
	Title          string         `json:"title" gorm:"size:500;not null"`
	Description    string         `json:"description" gorm:"type:text"`
	Status         string         `json:"status" gorm:"size:50;not null;default:open;index"`
	Severity       string         `json:"severity" gorm:"size:30;not null;default:normal;index"`
	Priority       string         `json:"priority" gorm:"size:30;not null;default:normal;index"`
	AssigneeID     *uint          `json:"assignee_id,omitempty" gorm:"index"`
	RepositoryID   *uint          `json:"repository_id,omitempty" gorm:"index"`
	Branch         string         `json:"branch,omitempty" gorm:"size:255"`
	LastAgentRunID *uint          `json:"last_agent_run_id,omitempty" gorm:"index"`
	AIAnalysis     string         `json:"ai_analysis,omitempty" gorm:"column:ai_analysis;type:text"`
	CreatedBy      uint           `json:"created_by" gorm:"index"`
	UpdatedBy      uint           `json:"updated_by" gorm:"index"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`

	// Non-persisted view fields
	ProjectName      string `json:"project_name,omitempty" gorm:"-"`
	AssigneeName     string `json:"assignee_name,omitempty" gorm:"-"`
	AssigneeUsername string `json:"assignee_username,omitempty" gorm:"-"`
	CreatorName      string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername  string `json:"creator_username,omitempty" gorm:"-"`
	RepositoryName   string `json:"repository_name,omitempty" gorm:"-"`
}

func (ProjectBug) TableName() string { return "project_bugs" }

// ProjectBugComment represents a user comment on a bug.
type ProjectBugComment struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	BugID     uint           `json:"bug_id" gorm:"not null;index"`
	Content   string         `json:"content" gorm:"type:text;not null"`
	CreatedBy uint           `json:"created_by" gorm:"index"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	// Non-persisted view fields
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

func (ProjectBugComment) TableName() string { return "project_bug_comments" }

// ProjectBugAttachment links an uploaded storage object to a bug.
type ProjectBugAttachment struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	BugID           uint      `json:"bug_id" gorm:"not null;index"`
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

func (ProjectBugAttachment) TableName() string { return "project_bug_attachments" }

// ProjectBugActivity logs lifecycle and transition events of a bug.
type ProjectBugActivity struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	BugID      uint      `json:"bug_id" gorm:"not null;index"`
	Action     string    `json:"action" gorm:"size:50;not null;index"`
	FromStatus string    `json:"from_status,omitempty" gorm:"size:50"`
	ToStatus   string    `json:"to_status,omitempty" gorm:"size:50"`
	Comment    string    `json:"comment,omitempty" gorm:"type:text"`
	CreatedBy  uint      `json:"created_by" gorm:"index"`
	CreatedAt  time.Time `json:"created_at"`

	// Non-persisted view fields
	CreatorName     string `json:"creator_name,omitempty" gorm:"-"`
	CreatorUsername string `json:"creator_username,omitempty" gorm:"-"`
}

func (ProjectBugActivity) TableName() string { return "project_bug_activities" }

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
