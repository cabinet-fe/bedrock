package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type BuildRunRepository struct{ db *gorm.DB }

func NewBuildRunRepository(db *gorm.DB) *BuildRunRepository {
	return &BuildRunRepository{db: db}
}

func (r *BuildRunRepository) Create(run *model.BuildRun) error {
	return r.db.Create(run).Error
}

func (r *BuildRunRepository) FindByID(id uint) (*model.BuildRun, error) {
	var run model.BuildRun
	if err := r.db.Preload("DeployAttempts", func(db *gorm.DB) *gorm.DB {
		return db.Order("batch_no ASC, id ASC")
	}).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *BuildRunRepository) List(q pkg.ListQuery, buildJobID *uint, status string, jobCreatedBy *uint, projectID *uint) ([]model.BuildRun, int64, error) {
	db := r.db.Model(&model.BuildRun{})
	if buildJobID != nil && *buildJobID > 0 {
		db = db.Where("build_job_id = ?", *buildJobID)
	}
	if projectID != nil && *projectID > 0 {
		db = db.Where("build_job_id IN (SELECT id FROM build_jobs WHERE project_id = ?)", *projectID)
	}
	if jobCreatedBy != nil {
		db = db.Joins("JOIN build_jobs ON build_jobs.id = build_runs.build_job_id").
			Where("build_jobs.created_by = ? OR build_jobs.is_public = ?", *jobCreatedBy, true)
	}
	if status != "" {
		db = db.Where("build_runs.status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := pkg.OrderBy(q.Sort, map[string]string{
		"created_at": "build_runs.created_at",
	}, "build_runs.id", "build_runs.id DESC")
	var items []model.BuildRun
	err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildRunRepository) NextBuildNumber(jobID uint) (int, error) {
	var maxNum *int
	err := r.db.Model(&model.BuildRun{}).
		Where("build_job_id = ?", jobID).
		Select("MAX(build_number)").
		Scan(&maxNum).Error
	if err != nil {
		return 0, err
	}
	if maxNum == nil {
		return 1, nil
	}
	return *maxNum + 1, nil
}

func (r *BuildRunRepository) ListAttempts(runID uint) ([]model.BuildDeployAttempt, error) {
	var items []model.BuildDeployAttempt
	err := r.db.Where("build_run_id = ?", runID).Order("batch_no ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *BuildRunRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.Model(&model.BuildRun{}).Where("id = ?", id).Updates(fields).Error
}

func (r *BuildRunRepository) CreateAttempt(a *model.BuildDeployAttempt) error {
	return r.db.Create(a).Error
}

func (r *BuildRunRepository) UpdateAttempt(a *model.BuildDeployAttempt) error {
	return r.db.Save(a).Error
}

func (r *BuildRunRepository) NextBatchNo(runID uint) (int, error) {
	var maxNum *int
	err := r.db.Model(&model.BuildDeployAttempt{}).
		Where("build_run_id = ?", runID).
		Select("MAX(batch_no)").
		Scan(&maxNum).Error
	if err != nil {
		return 0, err
	}
	if maxNum == nil {
		return 1, nil
	}
	return *maxNum + 1, nil
}

func (r *BuildRunRepository) ListByStatuses(statuses ...string) ([]model.BuildRun, error) {
	var items []model.BuildRun
	if len(statuses) == 0 {
		return items, nil
	}
	err := r.db.Where("status IN ?", statuses).Order("id ASC").Find(&items).Error
	return items, err
}

// MarkRunningInterrupted sets running → interrupted (NOT failed) on restart.
func (r *BuildRunRepository) MarkRunningInterrupted() (int64, error) {
	res := r.db.Model(&model.BuildRun{}).
		Where("status = ?", "running").
		Updates(map[string]interface{}{
			"status": "interrupted",
			"stage":  "idle",
		})
	return res.RowsAffected, res.Error
}

func (r *BuildRunRepository) HasNonTerminal(jobID uint) (bool, error) {
	var n int64
	err := r.db.Model(&model.BuildRun{}).
		Where("build_job_id = ? AND status IN ?", jobID, []string{"queued", "running"}).
		Count(&n).Error
	return n > 0, err
}

func (r *BuildRunRepository) ListArtifactsByJob(jobID uint) ([]model.BuildRun, error) {
	var items []model.BuildRun
	err := r.db.Where("build_job_id = ? AND artifact_path <> '' AND artifact_path IS NOT NULL", jobID).
		Order("id DESC").Find(&items).Error
	return items, err
}
