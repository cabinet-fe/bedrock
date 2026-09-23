package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"

	ProjectRoleOwner    = "owner"
	ProjectRoleAdmin    = "admin"
	ProjectRoleMember   = "member"
	ProjectRoleReadonly = "readonly"

	DocNodeDirectory = "dir"
	DocNodeDocument  = "doc"
)

type ProductProject struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	Name         string `json:"name" gorm:"size:200;not null"`
	Slug         string `json:"slug" gorm:"size:120;not null;uniqueIndex"`
	Description  string `json:"description" gorm:"type:text"`
	Status       string `json:"status" gorm:"size:20;not null;default:active;index"`
	OwnerID      uint   `json:"owner_id" gorm:"not null;index"`
	RepositoryID *uint  `json:"repository_id,omitempty" gorm:"index"`
	Tags         string `json:"tags"`
	// IsPublic 不再影响读可见性（D2 全员可读），保留字段以兼容存量数据与 API。
	IsPublic  bool           `json:"is_public" gorm:"not null;default:false;index"`
	CreatedBy uint           `json:"created_by" gorm:"index"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (ProductProject) TableName() string { return "product_projects" }

type ProjectMember struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"not null;uniqueIndex:idx_project_member"`
	UserID      uint      `json:"user_id" gorm:"not null;uniqueIndex:idx_project_member;index"`
	Role        string    `json:"role" gorm:"size:20;not null;index"`
	Username    string    `json:"username,omitempty" gorm:"-"`
	DisplayName string    `json:"display_name,omitempty" gorm:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ProjectMember) TableName() string { return "project_members" }

// UserOption is a lightweight picker item for assigning project members.
type UserOption struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// RequirementStatusOption is an enabled item from the requirement_status
// dictionary, exposed through the project domain's read-only metadata API.
type RequirementStatusOption struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	SortOrder int    `json:"sort_order"`
	Enabled   bool   `json:"enabled"`
}

// Requirement and its comment/attachment are legacy wire DTOs for the
// /requirements compatibility aliases; persistence lives in ProjectIssue.
type Requirement struct {
	ID           uint           `json:"id" gorm:"-"`
	ProjectID    uint           `json:"project_id"`
	Title        string         `json:"title" gorm:"-"`
	Description  string         `json:"description"`
	Status       string         `json:"status"`
	Priority     string         `json:"priority"`
	AssigneeID   *uint          `json:"assignee_id,omitempty"`
	RepositoryID *uint          `json:"repository_id,omitempty"`
	Tags         string         `json:"tags"`
	CreatedBy    uint           `json:"created_by"`
	UpdatedBy    uint           `json:"updated_by"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type RequirementComment struct {
	ID            uint      `json:"id" gorm:"-"`
	RequirementID uint      `json:"requirement_id"`
	Content       string    `json:"content"`
	CreatedBy     uint      `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RequirementAttachment struct {
	ID              uint      `json:"id" gorm:"-"`
	RequirementID   uint      `json:"requirement_id"`
	StorageObjectID uint      `json:"storage_object_id"`
	Filename        string    `json:"filename"`
	CreatedBy       uint      `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// ApiDocNode models both directory and Markdown document nodes.
// Document bodies live in Content (no draft/published split, no version).
type ApiDocNode struct {
	ID               uint           `json:"id" gorm:"primaryKey"`
	ProjectID        uint           `json:"project_id" gorm:"not null;index"`
	ParentID         *uint          `json:"parent_id,omitempty" gorm:"index"`
	Kind             string         `json:"kind" gorm:"size:10;not null;index"`
	Name             string         `json:"name" gorm:"size:300;not null"`
	SortOrder        int            `json:"sort_order" gorm:"not null;default:0;index"`
	RepositoryID     *uint          `json:"repository_id,omitempty" gorm:"index"`
	Content          string         `json:"content,omitempty" gorm:"type:text"`
	DraftSourceRunID *uint          `json:"draft_source_run_id,omitempty" gorm:"index"`
	CreatedBy        uint           `json:"created_by" gorm:"index"`
	UpdatedBy        uint           `json:"updated_by" gorm:"index"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `json:"-" gorm:"index"`
	Children         []ApiDocNode   `json:"children,omitempty" gorm:"-"`
}

func (ApiDocNode) TableName() string { return "api_doc_nodes" }

// DevDocNode models project development documentation (directory or Markdown), mirroring ApiDocNode.
type DevDocNode struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	ProjectID    uint           `json:"project_id" gorm:"not null;index"`
	ParentID     *uint          `json:"parent_id,omitempty" gorm:"index"`
	Kind         string         `json:"kind" gorm:"size:10;not null;index"`
	Name         string         `json:"name" gorm:"size:300;not null"`
	SortOrder    int            `json:"sort_order" gorm:"not null;default:0;index"`
	RepositoryID *uint          `json:"repository_id,omitempty" gorm:"index"`
	Content      string         `json:"content,omitempty" gorm:"type:text"`
	CreatedBy    uint           `json:"created_by" gorm:"index"`
	UpdatedBy    uint           `json:"updated_by" gorm:"index"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
	Children     []DevDocNode   `json:"children,omitempty" gorm:"-"`
}

func (DevDocNode) TableName() string { return "dev_doc_nodes" }

// AllProjectModels returns all persisted model instances in the project package.
func AllProjectModels() []any {
	return []any{
		&ProductProject{},
		&ProjectMember{},
		&ApiDocNode{},
		&DevDocNode{},
		&ProjectIssue{},
		&ProjectIssueComment{},
		&ProjectIssueAttachment{},
		&ProjectIssueActivity{},
		&ProjectIteration{},
		&ProjectIssueWatcher{},
	}
}
