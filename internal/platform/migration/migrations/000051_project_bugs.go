package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000051_project_bugs", upProjectBugs)
}

func upProjectBugs(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	models := []any{
		&projectBugMigrationModel{},
		&projectBugCommentMigrationModel{},
		&projectBugAttachmentMigrationModel{},
		&projectBugActivityMigrationModel{},
	}
	for _, m := range models {
		if db.Migrator().HasTable(m) {
			continue
		}
		if err := db.Migrator().CreateTable(m); err != nil {
			return err
		}
	}
	return nil
}

type projectBugMigrationModel struct {
	ID             uint           `gorm:"primaryKey"`
	ProjectID      uint           `gorm:"not null;index"`
	Title          string         `gorm:"size:500;not null"`
	Description    string         `gorm:"type:text"`
	Status         string         `gorm:"size:50;not null;default:open;index"`
	Severity       string         `gorm:"size:30;not null;default:normal;index"`
	Priority       string         `gorm:"size:30;not null;default:normal;index"`
	AssigneeID     *uint          `gorm:"index"`
	RepositoryID   *uint          `gorm:"index"`
	Branch         string         `gorm:"size:255"`
	LastAgentRunID *uint          `gorm:"index"`
	AIAnalysis     string         `gorm:"column:ai_analysis;type:text"`
	CreatedBy      uint           `gorm:"index"`
	UpdatedBy      uint           `gorm:"index"`
	CreatedAt      time.Time      `gorm:""`
	UpdatedAt      time.Time      `gorm:""`
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

func (projectBugMigrationModel) TableName() string { return "project_bugs" }

type projectBugCommentMigrationModel struct {
	ID        uint           `gorm:"primaryKey"`
	BugID     uint           `gorm:"not null;index"`
	Content   string         `gorm:"type:text;not null"`
	CreatedBy uint           `gorm:"index"`
	CreatedAt time.Time      `gorm:""`
	UpdatedAt time.Time      `gorm:""`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (projectBugCommentMigrationModel) TableName() string { return "project_bug_comments" }

type projectBugAttachmentMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	BugID           uint      `gorm:"not null;index"`
	StorageObjectID uint      `gorm:"not null;index"`
	Filename        string    `gorm:"size:500;not null"`
	CreatedBy       uint      `gorm:"index"`
	CreatedAt       time.Time `gorm:""`
}

func (projectBugAttachmentMigrationModel) TableName() string { return "project_bug_attachments" }

type projectBugActivityMigrationModel struct {
	ID         uint      `gorm:"primaryKey"`
	BugID      uint      `gorm:"not null;index"`
	Action     string    `gorm:"size:50;not null;index"`
	FromStatus string    `gorm:"size:50"`
	ToStatus   string    `gorm:"size:50"`
	Comment    string    `gorm:"type:text"`
	CreatedBy  uint      `gorm:"index"`
	CreatedAt  time.Time `gorm:""`
}

func (projectBugActivityMigrationModel) TableName() string { return "project_bug_activities" }
