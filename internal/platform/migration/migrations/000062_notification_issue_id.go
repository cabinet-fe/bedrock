package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000062_notification_issue_id", upNotificationIssueID)
}

// upNotificationIssueID adds the issue_id link for work-item collaboration
// notifications (DESIGN D39).
func upNotificationIssueID(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	notification := &notificationIssueIDMigrationModel{}
	if !db.Migrator().HasColumn(notification, "issue_id") {
		return db.Migrator().AddColumn(notification, "IssueID")
	}
	return nil
}

type notificationIssueIDMigrationModel struct {
	ID      uint
	IssueID *uint `gorm:"index"`
}

func (notificationIssueIDMigrationModel) TableName() string { return "notifications" }
