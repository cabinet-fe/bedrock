package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000061_unified_project_issues", upUnifiedProjectIssues)
}

// upUnifiedProjectIssues merges requirements + project_bugs into the unified
// project_issues model (DESIGN D37): creates the new tables, seeds the
// bug_status dictionary, copies rows with oldID→newID remapping for children,
// and drops the legacy tables. Fresh installs copy nothing.
func upUnifiedProjectIssues(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	tables := []any{
		&issueMigrationModel{},
		&issueCommentMigrationModel{},
		&issueAttachmentMigrationModel{},
		&issueActivityMigrationModel{},
		&issueWatcherMigrationModel{},
		&iterationMigrationModel{},
	}
	for _, m := range tables {
		if db.Migrator().HasTable(m) {
			continue
		}
		if err := db.Migrator().CreateTable(m); err != nil {
			return err
		}
	}

	if err := seedBugStatuses(db); err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := migrateIssues(tx); err != nil {
			return err
		}
		return dropLegacyIssueTables(tx)
	})
}

type issueMigrationModel struct {
	ID           uint           `gorm:"primaryKey"`
	ProjectID    uint           `gorm:"not null;index"`
	Type         string         `gorm:"size:20;not null;index"`
	Title        string         `gorm:"size:500;not null"`
	Description  string         `gorm:"type:text"`
	Status       string         `gorm:"size:100;not null;index"`
	Severity     string         `gorm:"size:30;not null;default:'';index"`
	Priority     string         `gorm:"size:30;not null;default:normal;index"`
	AssigneeID   *uint          `gorm:"index"`
	RepositoryID *uint          `gorm:"index"`
	Branch       string         `gorm:"size:255"`
	Tags         string         `gorm:""`
	IterationID  *uint          `gorm:"index"`
	CreatedBy    uint           `gorm:"index"`
	UpdatedBy    uint           `gorm:"index"`
	CreatedAt    time.Time      `gorm:""`
	UpdatedAt    time.Time      `gorm:""`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (issueMigrationModel) TableName() string { return "project_issues" }

type issueCommentMigrationModel struct {
	ID         uint           `gorm:"primaryKey"`
	IssueID    uint           `gorm:"not null;index"`
	Content    string         `gorm:"type:text;not null"`
	MentionIDs string         `gorm:"size:500"`
	CreatedBy  uint           `gorm:"index"`
	CreatedAt  time.Time      `gorm:""`
	UpdatedAt  time.Time      `gorm:""`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (issueCommentMigrationModel) TableName() string { return "project_issue_comments" }

type issueAttachmentMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	IssueID         uint      `gorm:"not null;index"`
	CommentID       *uint     `gorm:"index"`
	StorageObjectID uint      `gorm:"not null;index"`
	Filename        string    `gorm:"size:500;not null"`
	CreatedBy       uint      `gorm:"index"`
	CreatedAt       time.Time `gorm:""`
}

func (issueAttachmentMigrationModel) TableName() string { return "project_issue_attachments" }

type issueActivityMigrationModel struct {
	ID         uint      `gorm:"primaryKey"`
	IssueID    uint      `gorm:"not null;index"`
	Action     string    `gorm:"size:50;not null;index"`
	Field      string    `gorm:"size:50"`
	OldValue   string    `gorm:"size:500"`
	NewValue   string    `gorm:"size:500"`
	FromStatus string    `gorm:"size:100"`
	ToStatus   string    `gorm:"size:100"`
	Comment    string    `gorm:"type:text"`
	CreatedBy  uint      `gorm:"index"`
	CreatedAt  time.Time `gorm:""`
}

func (issueActivityMigrationModel) TableName() string { return "project_issue_activities" }

type issueWatcherMigrationModel struct {
	ID        uint      `gorm:"primaryKey"`
	IssueID   uint      `gorm:"not null;uniqueIndex:idx_issue_watcher"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_issue_watcher;index"`
	CreatedAt time.Time `gorm:""`
}

func (issueWatcherMigrationModel) TableName() string { return "project_issue_watchers" }

type iterationMigrationModel struct {
	ID        uint           `gorm:"primaryKey"`
	ProjectID uint           `gorm:"not null;index"`
	Name      string         `gorm:"size:200;not null"`
	Goal      string         `gorm:"type:text"`
	StartDate string         `gorm:"size:10"`
	EndDate   string         `gorm:"size:10"`
	Status    string         `gorm:"size:20;not null;default:planned;index"`
	CreatedBy uint           `gorm:"index"`
	CreatedAt time.Time      `gorm:""`
	UpdatedAt time.Time      `gorm:""`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (iterationMigrationModel) TableName() string { return "project_iterations" }

func seedBugStatuses(db *gorm.DB) error {
	dictionary := requirementStatusDictionaryMigrationModel{
		Name: "缺陷状态", Code: "bug_status", Description: "缺陷的可扩展状态字典（统一工作项）",
	}
	if err := db.Where("code = ?", dictionary.Code).FirstOrCreate(&dictionary).Error; err != nil {
		return err
	}
	items := []requirementStatusItemMigrationModel{
		{DictionaryID: dictionary.ID, Label: "打开", Value: "open", SortOrder: 10, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "处理中", Value: "in_progress", SortOrder: 20, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "已解决", Value: "resolved", SortOrder: 30, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "已关闭", Value: "closed", SortOrder: 40, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "已拒绝", Value: "rejected", SortOrder: 50, Enabled: true},
	}
	for _, item := range items {
		if err := db.Where("dictionary_id = ? AND value = ?", item.DictionaryID, item.Value).FirstOrCreate(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateIssues copies legacy requirement/bug rows into project_issues,
// remapping child tables (comments/attachments/activities) via oldID→newID.
// Already-migrated databases (project_issues populated, legacy tables gone)
// are skipped naturally because the legacy tables no longer exist.
func migrateIssues(tx *gorm.DB) error {
	issueIDMap := make(map[uint]uint)   // legacy requirement/bug ID → issue ID
	commentIDMap := make(map[uint]uint) // legacy bug comment ID → issue comment ID

	if tx.Migrator().HasTable("requirements") {
		var requirements []requirementMigrationModel
		if err := tx.Unscoped().Find(&requirements).Error; err != nil {
			return err
		}
		for _, r := range requirements {
			issue := issueMigrationModel{
				ProjectID: r.ProjectID, Type: "requirement",
				Title: r.Title, Description: r.Description, Status: r.Status,
				Priority: r.Priority, AssigneeID: r.AssigneeID, RepositoryID: r.RepositoryID,
				Tags: r.Tags, CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy,
				CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
			}
			if err := tx.Create(&issue).Error; err != nil {
				return err
			}
			issueIDMap[r.ID] = issue.ID
			// Requirements had no activity log; synthesize a create record so
			// the unified timeline and burndown computation have an origin.
			create := issueActivityMigrationModel{
				IssueID: issue.ID, Action: "create", ToStatus: r.Status,
				Comment: "创建需求（迁移合并）", CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
			}
			if err := tx.Create(&create).Error; err != nil {
				return err
			}
		}
	}

	if tx.Migrator().HasTable("project_bugs") {
		var bugs []projectBugMigrationModel
		if err := tx.Unscoped().Find(&bugs).Error; err != nil {
			return err
		}
		for _, b := range bugs {
			issue := issueMigrationModel{
				ProjectID: b.ProjectID, Type: "bug",
				Title: b.Title, Description: b.Description, Status: b.Status,
				Severity: b.Severity, Priority: b.Priority,
				AssigneeID: b.AssigneeID, RepositoryID: b.RepositoryID, Branch: b.Branch,
				CreatedBy: b.CreatedBy, UpdatedBy: b.UpdatedBy,
				CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, DeletedAt: b.DeletedAt,
			}
			if err := tx.Create(&issue).Error; err != nil {
				return err
			}
			issueIDMap[b.ID] = issue.ID
		}
	}

	if tx.Migrator().HasTable("requirement_comments") {
		var comments []requirementCommentMigrationModel
		if err := tx.Unscoped().Find(&comments).Error; err != nil {
			return err
		}
		for _, c := range comments {
			issueID, ok := issueIDMap[c.RequirementID]
			if !ok {
				continue
			}
			comment := issueCommentMigrationModel{
				IssueID: issueID, Content: c.Content, CreatedBy: c.CreatedBy,
				CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt,
			}
			if err := tx.Create(&comment).Error; err != nil {
				return err
			}
		}
	}

	if tx.Migrator().HasTable("project_bug_comments") {
		var comments []projectBugCommentMigrationModel
		if err := tx.Unscoped().Find(&comments).Error; err != nil {
			return err
		}
		for _, c := range comments {
			issueID, ok := issueIDMap[c.BugID]
			if !ok {
				continue
			}
			comment := issueCommentMigrationModel{
				IssueID: issueID, Content: c.Content, CreatedBy: c.CreatedBy,
				CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt,
			}
			if err := tx.Create(&comment).Error; err != nil {
				return err
			}
			commentIDMap[c.ID] = comment.ID
		}
	}

	if tx.Migrator().HasTable("requirement_attachments") {
		var atts []requirementAttachmentMigrationModel
		if err := tx.Unscoped().Find(&atts).Error; err != nil {
			return err
		}
		for _, a := range atts {
			issueID, ok := issueIDMap[a.RequirementID]
			if !ok {
				continue
			}
			att := issueAttachmentMigrationModel{
				IssueID: issueID, StorageObjectID: a.StorageObjectID,
				Filename: a.Filename, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
			}
			if err := tx.Create(&att).Error; err != nil {
				return err
			}
		}
	}

	if tx.Migrator().HasTable("project_bug_attachments") {
		var atts []legacyBugAttachmentMigrationModel
		if err := tx.Unscoped().Find(&atts).Error; err != nil {
			return err
		}
		for _, a := range atts {
			issueID, ok := issueIDMap[a.BugID]
			if !ok {
				continue
			}
			var commentID *uint
			if a.CommentID != nil {
				if mapped, ok := commentIDMap[*a.CommentID]; ok {
					mapped := mapped
					commentID = &mapped
				} else {
					continue // attachment of a comment lost in migration; skip
				}
			}
			att := issueAttachmentMigrationModel{
				IssueID: issueID, CommentID: commentID, StorageObjectID: a.StorageObjectID,
				Filename: a.Filename, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
			}
			if err := tx.Create(&att).Error; err != nil {
				return err
			}
		}
	}

	if tx.Migrator().HasTable("project_bug_activities") {
		var activities []projectBugActivityMigrationModel
		if err := tx.Unscoped().Find(&activities).Error; err != nil {
			return err
		}
		for _, a := range activities {
			issueID, ok := issueIDMap[a.BugID]
			if !ok {
				continue
			}
			activity := issueActivityMigrationModel{
				IssueID: issueID, Action: a.Action,
				FromStatus: a.FromStatus, ToStatus: a.ToStatus,
				Comment: a.Comment, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt,
			}
			if err := tx.Create(&activity).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// legacyBugAttachmentMigrationModel reads project_bug_attachments including
// the comment_id column added by 000060 (absent from the 000051 model).
type legacyBugAttachmentMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	BugID           uint      `gorm:"not null;index"`
	CommentID       *uint     `gorm:"index"`
	StorageObjectID uint      `gorm:"not null;index"`
	Filename        string    `gorm:"size:500;not null"`
	CreatedBy       uint      `gorm:"index"`
	CreatedAt       time.Time `gorm:""`
}

func (legacyBugAttachmentMigrationModel) TableName() string { return "project_bug_attachments" }

func dropLegacyIssueTables(tx *gorm.DB) error {
	for _, table := range []string{
		"requirements", "requirement_comments", "requirement_attachments",
		"project_bugs", "project_bug_comments", "project_bug_attachments", "project_bug_activities",
	} {
		if tx.Migrator().HasTable(table) {
			if err := tx.Migrator().DropTable(table); err != nil {
				return err
			}
		}
	}
	return nil
}
