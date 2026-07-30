package migrations

import (
	"context"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000033_agent_repo_binding_branch_unique", upAgentRepoBindingBranchUnique)
}

// Expand uidx_agent_repo from (agent_id, repository_id) to
// (agent_id, repository_id, branch) so one agent may bind the same repo on
// multiple branches.
func upAgentRepoBindingBranchUnique(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	m := &aiAgentRepoBindingBranchUniqueMigrationModel{}
	if !db.Migrator().HasTable(m) {
		return nil
	}
	if db.Migrator().HasIndex(m, "uidx_agent_repo") {
		if err := db.Migrator().DropIndex(m, "uidx_agent_repo"); err != nil {
			return err
		}
	}
	return db.Migrator().CreateIndex(m, "uidx_agent_repo")
}

type aiAgentRepoBindingBranchUniqueMigrationModel struct {
	AgentID      uint   `gorm:"not null;uniqueIndex:uidx_agent_repo"`
	RepositoryID uint   `gorm:"not null;uniqueIndex:uidx_agent_repo"`
	Branch       string `gorm:"size:200;not null;default:main;uniqueIndex:uidx_agent_repo"`
}

func (aiAgentRepoBindingBranchUniqueMigrationModel) TableName() string {
	return "ai_agent_repo_bindings"
}
