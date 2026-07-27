package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000028_role_data_scope", upRoleDataScope)
}

// roles.data_scope: self|all. Column default is self for new roles;
// existing rows are backfilled to all to avoid CI/CD visibility regressions.
func upRoleDataScope(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	role := &roleDataScopeMigrationModel{}
	if !db.Migrator().HasTable(role) {
		return nil
	}
	if db.Migrator().HasColumn(role, "DataScope") {
		return nil
	}
	if err := db.Migrator().AddColumn(role, "DataScope"); err != nil {
		return err
	}
	// AddColumn with default:self already fills existing rows; overwrite to all
	// so upgraded installs keep pre-change CI/CD visibility.
	return db.Exec(`UPDATE roles SET data_scope = ?`, "all").Error
}

type roleDataScopeMigrationModel struct {
	ID        uint   `gorm:"primaryKey"`
	DataScope string `gorm:"size:20;not null;default:self"`
}

func (roleDataScopeMigrationModel) TableName() string { return "roles" }
