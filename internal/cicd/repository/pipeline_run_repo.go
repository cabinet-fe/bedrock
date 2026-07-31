package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type PipelineRunRepository struct{ db *gorm.DB }

func NewPipelineRunRepository(db *gorm.DB) *PipelineRunRepository {
	return &PipelineRunRepository{db: db}
}

func (r *PipelineRunRepository) Create(run *model.PipelineRun) error {
	return r.db.Create(run).Error
}

func (r *PipelineRunRepository) CreateStages(stages []model.PipelineStageRun) error {
	if len(stages) == 0 {
		return nil
	}
	return r.db.Create(&stages).Error
}

func (r *PipelineRunRepository) FindByID(id uint) (*model.PipelineRun, error) {
	var run model.PipelineRun
	if err := r.db.Preload("Stages", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC")
	}).First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *PipelineRunRepository) List(q pkg.ListQuery, pipelineID *uint, status string, pipelineCreatedBy *uint) ([]model.PipelineRun, int64, error) {
	db := r.db.Model(&model.PipelineRun{})
	if pipelineID != nil && *pipelineID > 0 {
		db = db.Where("build_pipeline_id = ?", *pipelineID)
	}
	if pipelineCreatedBy != nil {
		db = db.Joins("JOIN build_pipelines ON build_pipelines.id = pipeline_runs.build_pipeline_id").
			Where("build_pipelines.created_by = ? OR build_pipelines.is_public = ?", *pipelineCreatedBy, true)
	}
	if status != "" {
		db = db.Where("pipeline_runs.status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := pkg.OrderBy(q.Sort, map[string]string{
		"created_at": "pipeline_runs.created_at",
	}, "pipeline_runs.id", "pipeline_runs.id DESC")
	var items []model.PipelineRun
	err := db.Order(order).Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *PipelineRunRepository) NextRunNumber(pipelineID uint) (int, error) {
	var maxNum *int
	err := r.db.Model(&model.PipelineRun{}).
		Where("build_pipeline_id = ?", pipelineID).
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

func (r *PipelineRunRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.Model(&model.PipelineRun{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PipelineRunRepository) UpdateStageFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.Model(&model.PipelineStageRun{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PipelineRunRepository) FindStageByBuildRunID(buildRunID uint) (*model.PipelineStageRun, error) {
	var stage model.PipelineStageRun
	if err := r.db.Where("build_run_id = ?", buildRunID).First(&stage).Error; err != nil {
		return nil, err
	}
	return &stage, nil
}

func (r *PipelineRunRepository) ListStages(pipelineRunID uint) ([]model.PipelineStageRun, error) {
	var items []model.PipelineStageRun
	err := r.db.Where("pipeline_run_id = ?", pipelineRunID).Order("id ASC").Find(&items).Error
	return items, err
}

func (r *PipelineRunRepository) HasNonTerminal(pipelineID uint) (bool, error) {
	var n int64
	err := r.db.Model(&model.PipelineRun{}).
		Where("build_pipeline_id = ? AND status IN ?", pipelineID, []string{"queued", "running"}).
		Count(&n).Error
	return n > 0, err
}

func (r *PipelineRunRepository) MarkRunningInterrupted() (int64, error) {
	var runIDs []uint
	if err := r.db.Model(&model.PipelineRun{}).
		Where("status = ?", "running").
		Pluck("id", &runIDs).Error; err != nil {
		return 0, err
	}
	if len(runIDs) == 0 {
		return 0, nil
	}
	res := r.db.Model(&model.PipelineRun{}).
		Where("id IN ?", runIDs).
		Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": "server restarted while pipeline was running",
		})
	if res.Error != nil {
		return 0, res.Error
	}
	_ = r.db.Model(&model.PipelineStageRun{}).
		Where("pipeline_run_id IN ? AND status IN ?", runIDs, []string{"queued", "running", "pending"}).
		Updates(map[string]interface{}{
			"status": "skipped",
		})
	return res.RowsAffected, nil
}
