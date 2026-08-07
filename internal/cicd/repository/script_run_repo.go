package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type ScriptRunRepository struct{ db *gorm.DB }

func NewScriptRunRepository(db *gorm.DB) *ScriptRunRepository {
	return &ScriptRunRepository{db: db}
}

func (r *ScriptRunRepository) Create(run *model.ScriptRun) error {
	return r.db.Create(run).Error
}

func (r *ScriptRunRepository) FindByID(id uint) (*model.ScriptRun, error) {
	var run model.ScriptRun
	if err := r.db.First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *ScriptRunRepository) List(q pkg.ListQuery, scriptJobID *uint, status string, jobCreatedBy *uint, projectID *uint) ([]model.ScriptRun, int64, error) {
	db := r.db.Model(&model.ScriptRun{})
	if scriptJobID != nil && *scriptJobID > 0 {
		db = db.Where("script_job_id = ?", *scriptJobID)
	}
	if projectID != nil && *projectID > 0 {
		db = db.Where("script_job_id IN (SELECT id FROM script_jobs WHERE project_id = ?)", *projectID)
	}
	if jobCreatedBy != nil {
		db = db.Joins("JOIN script_jobs ON script_jobs.id = script_runs.script_job_id").
			Where("script_jobs.created_by = ? OR script_jobs.is_public = ?", *jobCreatedBy, true)
	}
	if status != "" {
		db = db.Where("script_runs.status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := pkg.OrderBy(q.Sort, map[string]string{
		"created_at": "script_runs.created_at",
	}, "script_runs.id", "script_runs.id DESC")
	var items []model.ScriptRun
	err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *ScriptRunRepository) NextRunNumber(jobID uint) (int, error) {
	var maxNum *int
	err := r.db.Model(&model.ScriptRun{}).
		Where("script_job_id = ?", jobID).
		Select("MAX(run_number)").
		Scan(&maxNum).Error
	if err != nil {
		return 0, err
	}
	if maxNum == nil {
		return 1, nil
	}
	return *maxNum + 1, nil
}

func (r *ScriptRunRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.Model(&model.ScriptRun{}).Where("id = ?", id).Updates(fields).Error
}

func (r *ScriptRunRepository) ListByStatuses(statuses ...string) ([]model.ScriptRun, error) {
	var items []model.ScriptRun
	if len(statuses) == 0 {
		return items, nil
	}
	err := r.db.Where("status IN ?", statuses).Order("id ASC").Find(&items).Error
	return items, err
}

// MarkRunningInterrupted sets running → interrupted on process restart.
func (r *ScriptRunRepository) MarkRunningInterrupted() (int64, error) {
	res := r.db.Model(&model.ScriptRun{}).
		Where("status = ?", "running").
		Updates(map[string]interface{}{
			"status": "interrupted",
			"stage":  "idle",
		})
	return res.RowsAffected, res.Error
}

func (r *ScriptRunRepository) HasNonTerminal(jobID uint) (bool, error) {
	var n int64
	err := r.db.Model(&model.ScriptRun{}).
		Where("script_job_id = ? AND status IN ?", jobID, []string{"queued", "running"}).
		Count(&n).Error
	return n > 0, err
}
