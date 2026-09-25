package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000060_bug_comment_attachments", upBugCommentAttachments)
}

func upBugCommentAttachments(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	att := &projectBugAttachmentCommentIDMigrationModel{}
	if !db.Migrator().HasColumn(att, "comment_id") {
		if err := db.Migrator().AddColumn(att, "CommentID"); err != nil {
			return err
		}
	}
	return nil
}

type projectBugAttachmentCommentIDMigrationModel struct {
	ID        uint
	CommentID *uint `gorm:"index"`
}

func (projectBugAttachmentCommentIDMigrationModel) TableName() string {
	return "project_bug_attachments"
}
