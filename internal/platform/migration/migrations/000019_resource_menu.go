package migrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000019_resource_menu", upResourceMenu)
}

// upResourceMenu moves repositories/servers/credentials from the CI/CD menu
// group into a top-level "resource management" (resource) module, renaming permission paths.
func upResourceMenu(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
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
			{"cicd.repositories", "resource.repositories", "代码仓库", "/resource/repositories", 10},
			{"cicd.servers", "resource.servers", "服务器", "/resource/servers", 20},
			{"cicd.credentials", "resource.credentials", "凭证", "/resource/credentials", 30},
		}
		for _, m := range moves {
			if err := renameMenuResource(tx, m.oldPath, m.newPath, m.title, m.route, m.sortKey, resourceID, now); err != nil {
				return err
			}
		}

		// Re-order remaining CI/CD children for fresh readability (best-effort).
		_ = tx.Exec(`UPDATE rbac_resources SET sort_key = 10, updated_at = ? WHERE path = ?`, now, "cicd.build_jobs").Error
		_ = tx.Exec(`UPDATE rbac_resources SET sort_key = 20, updated_at = ? WHERE path = ?`, now, "cicd.build_runs").Error
		return nil
	})
}

func ensureMenuResource(tx *gorm.DB, path, title, route string, sortKey int, parentID *uint, now time.Time) (uint, error) {
	var res struct {
		ID uint
	}
	err := tx.Table("rbac_resources").Select("id").Where("path = ?", path).Take(&res).Error
	if err == nil {
		return res.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("find resource %s: %w", path, err)
	}

	row := map[string]interface{}{
		"path":       path,
		"type":       "menu",
		"parent_id":  parentID,
		"enabled":    true,
		"sort_key":   sortKey,
		"created_at": now,
		"updated_at": now,
	}
	if err := tx.Table("rbac_resources").Create(row).Error; err != nil {
		return 0, fmt.Errorf("create resource %s: %w", path, err)
	}
	if err := tx.Table("rbac_resources").Select("id").Where("path = ?", path).Take(&res).Error; err != nil {
		return 0, fmt.Errorf("reload resource %s: %w", path, err)
	}
	meta := map[string]interface{}{
		"resource_id": res.ID,
		"title":       title,
		"route":       route,
	}
	if err := tx.Table("menu_metadata").Create(meta).Error; err != nil {
		return 0, fmt.Errorf("create menu metadata %s: %w", path, err)
	}
	return res.ID, nil
}

func renameMenuResource(tx *gorm.DB, oldPath, newPath, title, route string, sortKey int, parentID uint, now time.Time) error {
	var res struct {
		ID uint
	}
	err := tx.Table("rbac_resources").Select("id").Where("path = ?", oldPath).Take(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Fresh installs may already seed the new path; nothing to rename.
		return nil
	}
	if err != nil {
		return fmt.Errorf("find resource %s: %w", oldPath, err)
	}

	// If new path already exists (partial apply), drop the old row after copying perms.
	var existing struct{ ID uint }
	err = tx.Table("rbac_resources").Select("id").Where("path = ?", newPath).Take(&existing).Error
	if err == nil {
		if err := tx.Exec(
			`UPDATE role_permissions SET permission = REPLACE(permission, ?, ?) WHERE permission LIKE ?`,
			oldPath+":", newPath+":", oldPath+":%",
		).Error; err != nil {
			return err
		}
		_ = tx.Exec(`DELETE FROM menu_metadata WHERE resource_id = ?`, res.ID).Error
		_ = tx.Exec(`DELETE FROM rbac_resources WHERE id = ?`, res.ID).Error
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find resource %s: %w", newPath, err)
	}

	if err := tx.Exec(
		`UPDATE rbac_resources SET path = ?, parent_id = ?, sort_key = ?, updated_at = ? WHERE id = ?`,
		newPath, parentID, sortKey, now, res.ID,
	).Error; err != nil {
		return fmt.Errorf("rename resource %s -> %s: %w", oldPath, newPath, err)
	}
	if err := tx.Exec(
		`UPDATE menu_metadata SET title = ?, route = ? WHERE resource_id = ?`,
		title, route, res.ID,
	).Error; err != nil {
		return fmt.Errorf("update menu metadata %s: %w", newPath, err)
	}
	if err := tx.Exec(
		`UPDATE role_permissions SET permission = REPLACE(permission, ?, ?) WHERE permission LIKE ?`,
		oldPath+":", newPath+":", oldPath+":%",
	).Error; err != nil {
		return fmt.Errorf("rename role permissions %s: %w", oldPath, err)
	}
	return nil
}
