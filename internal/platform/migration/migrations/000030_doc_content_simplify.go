package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000030_doc_content_simplify", upDocContentSimplify)
}

// Collapse draft/published/version fields into a single content column.
// Prefer non-empty draft_content; otherwise fall back to published_content.
func upDocContentSimplify(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	node := &apiDocNodeContentMigration{}
	if !db.Migrator().HasTable(node) {
		return nil
	}

	if !db.Migrator().HasColumn(node, "Content") {
		if err := db.Migrator().AddColumn(node, "Content"); err != nil {
			return err
		}
	}

	// Backfill only when legacy columns still exist.
	if db.Migrator().HasColumn(node, "DraftContent") && db.Migrator().HasColumn(node, "PublishedContent") {
		if err := db.Exec(`
			UPDATE api_doc_nodes
			SET content = CASE
				WHEN draft_content IS NOT NULL AND draft_content <> '' THEN draft_content
				ELSE COALESCE(published_content, '')
			END
			WHERE content IS NULL OR content = ''
		`).Error; err != nil {
			return err
		}
	}

	for _, col := range []string{"PublishedContent", "DraftContent", "ContentVersion", "DraftBaseVersion", "DraftUpdatedAt"} {
		if db.Migrator().HasColumn(node, col) {
			if err := db.Migrator().DropColumn(node, col); err != nil {
				return err
			}
		}
	}
	return nil
}

type apiDocNodeContentMigration struct {
	ID               uint   `gorm:"primaryKey"`
	Content          string `gorm:"type:text"`
	PublishedContent string `gorm:"type:text"`
	DraftContent     string `gorm:"type:text"`
	ContentVersion   int    `gorm:"not null;default:0"`
	DraftBaseVersion int    `gorm:"not null;default:0"`
	DraftUpdatedAt   *time.Time
}

func (apiDocNodeContentMigration) TableName() string { return "api_doc_nodes" }
