package model

import "time"

// Dictionary is a named code table with items.
type Dictionary struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name" gorm:"size:100;not null"`
	Code        string     `json:"code" gorm:"size:100;uniqueIndex;not null"`
	Description string     `json:"description" gorm:"size:500"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Items       []DictItem `json:"items,omitempty" gorm:"foreignKey:DictionaryID"`
}

func (Dictionary) TableName() string { return "dictionaries" }

// DictItem is one entry in a dictionary.
type DictItem struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	DictionaryID uint      `json:"dictionary_id" gorm:"index;not null"`
	Label        string    `json:"label" gorm:"size:200;not null"`
	Value        string    `json:"value" gorm:"size:200;not null"`
	SortOrder    int       `json:"sort_order" gorm:"not null;default:0"`
	Enabled      bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (DictItem) TableName() string { return "dict_items" }

// OperationLog records mutating API actions for audit query.
type OperationLog struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	UserID       uint      `json:"user_id" gorm:"index"`
	Username     string    `json:"username" gorm:"size:50"`
	Action       string    `json:"action" gorm:"size:50;not null;index"`
	ResourceType string    `json:"resource_type" gorm:"size:50"`
	ResourceID   string    `json:"resource_id" gorm:"size:64"`
	Details      string    `json:"details" gorm:"type:text"`
	IPAddress    string    `json:"ip_address" gorm:"size:45"`
	CreatedAt    time.Time `json:"created_at" gorm:"index"`
}

func (OperationLog) TableName() string { return "operation_logs" }

// Notification is a per-user in-app inbox item (DESIGN §12).
type Notification struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	UserID     uint      `json:"user_id" gorm:"index;not null"`
	Type       string    `json:"type" gorm:"size:50;not null"`
	Title      string    `json:"title" gorm:"size:200;not null"`
	Message    string    `json:"message" gorm:"size:500"`
	BuildRunID *uint     `json:"build_run_id" gorm:"index"`
	AgentRunID *uint     `json:"agent_run_id" gorm:"index"`
	IssueID    *uint     `json:"issue_id" gorm:"index"`
	IsRead     bool      `json:"is_read" gorm:"not null;default:false;index"`
	CreatedAt  time.Time `json:"created_at" gorm:"index"`
}

func (Notification) TableName() string { return "notifications" }
