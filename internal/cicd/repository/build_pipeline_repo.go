package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type BuildPipelineRepository struct{ db *gorm.DB }

func NewBuildPipelineRepository(db *gorm.DB) *BuildPipelineRepository {
	return &BuildPipelineRepository{db: db}
}

func (r *BuildPipelineRepository) Create(p *model.BuildPipeline) error {
	return r.db.Create(p).Error
}

func (r *BuildPipelineRepository) Update(p *model.BuildPipeline) error {
	return r.db.Save(p).Error
}

func (r *BuildPipelineRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var runIDs []uint
		if err := tx.Model(&model.PipelineRun{}).Where("build_pipeline_id = ?", id).Pluck("id", &runIDs).Error; err != nil {
			return err
		}
		if len(runIDs) > 0 {
			if err := tx.Where("pipeline_run_id IN ?", runIDs).Delete(&model.PipelineStageRun{}).Error; err != nil {
				return err
			}
			if err := tx.Where("build_pipeline_id = ?", id).Delete(&model.PipelineRun{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("build_pipeline_id = ?", id).Delete(&PipelineWebhookDelivery{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.BuildPipeline{}, id).Error
	})
}

func (r *BuildPipelineRepository) FindByID(id uint) (*model.BuildPipeline, error) {
	var p model.BuildPipeline
	if err := r.db.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *BuildPipelineRepository) List(q pkg.ListQuery, keyword string, createdBy *uint) ([]model.BuildPipeline, int64, error) {
	db := r.db.Model(&model.BuildPipeline{})
	if createdBy != nil {
		db = db.Where("created_by = ? OR is_public = ?", *createdBy, true)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.BuildPipeline
	err := db.Order("id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildPipelineRepository) ListCronEnabled() ([]model.BuildPipeline, error) {
	var items []model.BuildPipeline
	err := r.db.Where("enabled = ? AND trigger_cron = ? AND cron_expression <> '' AND cron_expression IS NOT NULL", true, true).
		Find(&items).Error
	return items, err
}
