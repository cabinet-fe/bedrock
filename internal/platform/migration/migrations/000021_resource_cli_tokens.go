package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000021_resource_cli_tokens", upResourceCliTokens)
}

// upResourceCliTokens moves AI CLI and personal access tokens from the AI menu
// group into "resource management" (resource), renaming menu routes and permission paths.
// Table names are unchanged; no legacy path aliases are kept.
func upResourceCliTokens(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	now := time.Now().UTC()

	return db.Transaction(func(tx *gorm.DB) error {
		resourceID, err := ensureMenuResource(tx, "resource", "资源管理", "/resource", 25, nil, now)
		if err != nil {
			return err
		}

		moves := []struct {
			oldPath string
			newPath string
			title   string
			route   string
			sortKey int
		}{
			{"ai.clis", "resource.clis", "CLI", "/resource/clis", 40},
			{"ai.tokens", "resource.tokens", "访问令牌", "/resource/tokens", 50},
		}
		for _, m := range moves {
			if err := renameMenuResource(tx, m.oldPath, m.newPath, m.title, m.route, m.sortKey, resourceID, now); err != nil {
				return err
			}
		}

		// Re-order remaining AI children for fresh readability (best-effort).
		_ = tx.Exec(`UPDATE rbac_resources SET sort_key = 10, updated_at = ? WHERE path = ?`, now, "ai.agents").Error
		_ = tx.Exec(`UPDATE rbac_resources SET sort_key = 20, updated_at = ? WHERE path = ?`, now, "ai.runs").Error
		_ = tx.Exec(`UPDATE rbac_resources SET sort_key = 30, updated_at = ? WHERE path = ?`, now, "ai.skills").Error
		return nil
	})
}
