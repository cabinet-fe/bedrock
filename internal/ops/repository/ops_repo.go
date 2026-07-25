package repository

import (
	"bedrock/internal/ops/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type OpsRepository struct{ db *gorm.DB }

func NewOpsRepository(db *gorm.DB) *OpsRepository {
	return &OpsRepository{db: db}
}

func (r *OpsRepository) ListEnvironments() ([]model.DevEnvironment, error) {
	var items []model.DevEnvironment
	err := r.db.Preload("Sources", func(db *gorm.DB) *gorm.DB {
		return db.Order("priority ASC, id ASC")
	}).Order("kind ASC, name ASC").Find(&items).Error
	return items, err
}

func (r *OpsRepository) FindEnvironment(id uint) (*model.DevEnvironment, error) {
	var item model.DevEnvironment
	if err := r.db.Preload("Sources", func(db *gorm.DB) *gorm.DB {
		return db.Order("priority ASC, id ASC")
	}).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpsRepository) CreateEnvironment(item *model.DevEnvironment) error {
	return r.db.Create(item).Error
}

func (r *OpsRepository) UpdateEnvironment(item *model.DevEnvironment) error {
	return r.db.Save(item).Error
}

func (r *OpsRepository) DeleteEnvironment(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("environment_id = ?", id).Delete(&model.DevEnvJob{}).Error; err != nil {
			return err
		}
		if err := tx.Where("environment_id = ?", id).Delete(&model.DevEnvInstallSource{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.DevEnvironment{}, id).Error
	})
}

func (r *OpsRepository) ListSources(environmentID uint) ([]model.DevEnvInstallSource, error) {
	var items []model.DevEnvInstallSource
	err := r.db.Where("environment_id = ?", environmentID).Order("priority ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *OpsRepository) FindSourceInEnvironment(environmentID, sourceID uint) (*model.DevEnvInstallSource, error) {
	var item model.DevEnvInstallSource
	if err := r.db.Where("environment_id = ? AND id = ?", environmentID, sourceID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpsRepository) ListEnabledSources(environmentID uint) ([]model.DevEnvInstallSource, error) {
	var items []model.DevEnvInstallSource
	err := r.db.Where("environment_id = ? AND enabled = ?", environmentID, true).
		Order("priority ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *OpsRepository) CreateSource(item *model.DevEnvInstallSource) error {
	return r.db.Create(item).Error
}

func (r *OpsRepository) UpdateSource(item *model.DevEnvInstallSource) error {
	return r.db.Save(item).Error
}

func (r *OpsRepository) DeleteSource(id uint) error {
	return r.db.Delete(&model.DevEnvInstallSource{}, id).Error
}

func (r *OpsRepository) ListJobs(environmentID uint, q pkg.ListQuery, status string) ([]model.DevEnvJob, int64, error) {
	db := r.db.Model(&model.DevEnvJob{}).Where("environment_id = ?", environmentID)
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.DevEnvJob
	err := db.Preload("Environment").Preload("Source").Order("id DESC").
		Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *OpsRepository) FindJob(id uint) (*model.DevEnvJob, error) {
	var item model.DevEnvJob
	if err := r.db.Preload("Environment").Preload("Source").First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpsRepository) FindJobInEnvironment(environmentID, jobID uint) (*model.DevEnvJob, error) {
	var item model.DevEnvJob
	if err := r.db.Preload("Environment").Preload("Source").
		Where("environment_id = ? AND id = ?", environmentID, jobID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpsRepository) CreateJob(item *model.DevEnvJob) error {
	return r.db.Create(item).Error
}

func (r *OpsRepository) UpdateJob(item *model.DevEnvJob) error {
	return r.db.Save(item).Error
}

func (r *OpsRepository) ListJobsByStatuses(statuses ...string) ([]model.DevEnvJob, error) {
	var items []model.DevEnvJob
	if len(statuses) == 0 {
		return items, nil
	}
	err := r.db.Where("status IN ?", statuses).Order("id ASC").Find(&items).Error
	return items, err
}

func (r *OpsRepository) MarkRunningInterrupted() (int64, error) {
	result := r.db.Model(&model.DevEnvJob{}).Where("status = ?", model.JobRunning).
		Updates(map[string]interface{}{"status": model.JobInterrupted})
	return result.RowsAffected, result.Error
}
