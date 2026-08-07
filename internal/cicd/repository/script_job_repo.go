package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type ScriptJobRepository struct{ db *gorm.DB }

func NewScriptJobRepository(db *gorm.DB) *ScriptJobRepository {
	return &ScriptJobRepository{db: db}
}

func (r *ScriptJobRepository) Create(job *model.ScriptJob) error {
	return r.db.Create(job).Error
}

func (r *ScriptJobRepository) Update(job *model.ScriptJob) error {
	return r.db.Save(job).Error
}

func (r *ScriptJobRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("script_job_id = ?", id).Delete(&model.ScriptRun{}).Error; err != nil {
			return err
		}
		if err := tx.Where("script_job_id = ?", id).Delete(&ScriptWebhookDelivery{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ScriptJob{}, id).Error
	})
}

func (r *ScriptJobRepository) FindByID(id uint) (*model.ScriptJob, error) {
	var job model.ScriptJob
	if err := r.db.First(&job, id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *ScriptJobRepository) List(q pkg.ListQuery, keyword string, createdBy *uint, projectID *uint) ([]model.ScriptJob, int64, error) {
	db := r.db.Model(&model.ScriptJob{})
	if projectID != nil && *projectID > 0 {
		db = db.Where("project_id = ?", *projectID)
	}
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
	var items []model.ScriptJob
	err := db.Order("id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *ScriptJobRepository) ListCronEnabled() ([]model.ScriptJob, error) {
	var items []model.ScriptJob
	err := r.db.Where("enabled = ? AND trigger_cron = ? AND cron_expression <> '' AND cron_expression IS NOT NULL", true, true).
		Find(&items).Error
	return items, err
}
