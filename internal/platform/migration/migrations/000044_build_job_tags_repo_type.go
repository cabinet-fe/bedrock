package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000044_build_job_tags_repo_type", upBuildJobTagsRepoType)
}

// upBuildJobTagsRepoType adds build_jobs.tags and seeds the repo_type dictionary.
func upBuildJobTagsRepoType(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	job := &buildJobTagsMigrationModel{}
	if db.Migrator().HasTable(job) && !db.Migrator().HasColumn(job, "Tags") {
		if err := db.Migrator().AddColumn(job, "Tags"); err != nil {
			return err
		}
	}
	return seedRepoTypeDictionary(db)
}

type buildJobTagsMigrationModel struct {
	ID   uint   `gorm:"primaryKey"`
	Tags string `gorm:"size:500"`
}

func (buildJobTagsMigrationModel) TableName() string { return "build_jobs" }

type repoTypeDictionaryMigrationModel struct {
	ID          uint      `gorm:"primaryKey"`
	Name        string    `gorm:"size:100;not null"`
	Code        string    `gorm:"size:100;uniqueIndex;not null"`
	Description string    `gorm:"size:500"`
	CreatedAt   time.Time `gorm:""`
	UpdatedAt   time.Time `gorm:""`
}

func (repoTypeDictionaryMigrationModel) TableName() string { return "dictionaries" }

type repoTypeItemMigrationModel struct {
	ID           uint      `gorm:"primaryKey"`
	DictionaryID uint      `gorm:"index;not null"`
	Label        string    `gorm:"size:200;not null"`
	Value        string    `gorm:"size:200;not null"`
	SortOrder    int       `gorm:"not null;default:0"`
	Enabled      bool      `gorm:"not null;default:true"`
	CreatedAt    time.Time `gorm:""`
	UpdatedAt    time.Time `gorm:""`
}

func (repoTypeItemMigrationModel) TableName() string { return "dict_items" }

func seedRepoTypeDictionary(db *gorm.DB) error {
	dictionary := repoTypeDictionaryMigrationModel{
		Name: "仓库类型", Code: "repo_type", Description: "构建任务标签/类型选项",
	}
	if err := db.Where("code = ?", dictionary.Code).FirstOrCreate(&dictionary).Error; err != nil {
		return err
	}
	items := []repoTypeItemMigrationModel{
		{DictionaryID: dictionary.ID, Label: "前端", Value: "frontend", SortOrder: 10, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "后端", Value: "backend", SortOrder: 20, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "全栈", Value: "fullstack", SortOrder: 30, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "库/组件", Value: "library", SortOrder: 40, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "移动端", Value: "mobile", SortOrder: 50, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "基础设施", Value: "infra", SortOrder: 60, Enabled: true},
		{DictionaryID: dictionary.ID, Label: "其他", Value: "other", SortOrder: 70, Enabled: true},
	}
	for _, item := range items {
		if err := db.Where("dictionary_id = ? AND value = ?", item.DictionaryID, item.Value).FirstOrCreate(&item).Error; err != nil {
			return err
		}
	}
	return nil
}
