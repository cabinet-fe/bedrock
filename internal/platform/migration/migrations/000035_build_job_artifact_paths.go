package migrations

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000035_build_job_artifact_paths", upBuildJobArtifactPaths)
}

func upBuildJobArtifactPaths(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver

	job := &buildJobArtifactPathsMigrationModel{}
	if !db.Migrator().HasColumn(job, "artifact_paths_json") {
		if err := db.Migrator().AddColumn(job, "ArtifactPathsJSON"); err != nil {
			return err
		}
	}

	var rows []struct {
		ID        uint
		OutputDir string
	}
	if err := db.Model(job).Select("id", "output_dir").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		dir := strings.TrimSpace(r.OutputDir)
		if dir == "" {
			continue
		}
		b, err := json.Marshal([]string{dir})
		if err != nil {
			return err
		}
		if err := db.Model(job).Where("id = ?", r.ID).
			Update("artifact_paths_json", string(b)).Error; err != nil {
			return err
		}
	}

	run := &buildRunArtifactKindMigrationModel{}
	if !db.Migrator().HasColumn(run, "artifact_kind") {
		if err := db.Migrator().AddColumn(run, "ArtifactKind"); err != nil {
			return err
		}
	}
	return nil
}

type buildJobArtifactPathsMigrationModel struct {
	ID                uint   `gorm:"primaryKey"`
	OutputDir         string `gorm:"size:300"`
	ArtifactPathsJSON string `gorm:"column:artifact_paths_json;type:text"`
}

func (buildJobArtifactPathsMigrationModel) TableName() string { return "build_jobs" }

type buildRunArtifactKindMigrationModel struct {
	ID           uint   `gorm:"primaryKey"`
	ArtifactKind string `gorm:"size:20"`
}

func (buildRunArtifactKindMigrationModel) TableName() string { return "build_runs" }
