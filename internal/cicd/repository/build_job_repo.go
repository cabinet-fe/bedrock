package repository

import (
	"bedrock/internal/cicd/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type BuildJobRepository struct{ db *gorm.DB }

func NewBuildJobRepository(db *gorm.DB) *BuildJobRepository {
	return &BuildJobRepository{db: db}
}

func (r *BuildJobRepository) Create(job *model.BuildJob) error {
	return r.db.Create(job).Error
}

func (r *BuildJobRepository) Update(job *model.BuildJob) error {
	return r.db.Save(job).Error
}

func (r *BuildJobRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var runIDs []uint
		if err := tx.Model(&model.BuildRun{}).Where("build_job_id = ?", id).Pluck("id", &runIDs).Error; err != nil {
			return err
		}
		if len(runIDs) > 0 {
			if err := tx.Where("build_run_id IN ?", runIDs).Delete(&model.BuildDeployAttempt{}).Error; err != nil {
				return err
			}
			if err := tx.Where("build_job_id = ?", id).Delete(&model.BuildRun{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("build_job_id = ?", id).Delete(&WebhookDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Where("build_job_id = ?", id).Delete(&model.DeployTarget{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.BuildJob{}, id).Error
	})
}

func (r *BuildJobRepository) FindByID(id uint) (*model.BuildJob, error) {
	var job model.BuildJob
	if err := r.db.Preload("DeployTargets", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).First(&job, id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *BuildJobRepository) List(q pkg.ListQuery, repositoryID *uint, keyword, tag string, createdBy *uint) ([]model.BuildJob, int64, error) {
	db := r.db.Model(&model.BuildJob{})
	if repositoryID != nil && *repositoryID > 0 {
		db = db.Where("repository_id = ?", *repositoryID)
	}
	if createdBy != nil {
		db = db.Where("created_by = ? OR is_public = ?", *createdBy, true)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if tag != "" {
		db = db.Where("tags LIKE ?", "%"+tag+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.BuildJob
	err := db.Preload("DeployTargets", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).Order("id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildJobRepository) ReplaceDeployTargets(jobID uint, targets []model.DeployTarget) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("build_job_id = ?", jobID).Delete(&model.DeployTarget{}).Error; err != nil {
			return err
		}
		for i := range targets {
			targets[i].ID = 0
			targets[i].BuildJobID = jobID
			if err := tx.Create(&targets[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *BuildJobRepository) ListDeployTargets(jobID uint) ([]model.DeployTarget, error) {
	var items []model.DeployTarget
	err := r.db.Where("build_job_id = ?", jobID).Order("sort_order ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *BuildJobRepository) ListCronEnabled() ([]model.BuildJob, error) {
	var items []model.BuildJob
	err := r.db.Where("enabled = ? AND trigger_cron = ? AND cron_expression <> '' AND cron_expression IS NOT NULL", true, true).
		Find(&items).Error
	return items, err
}

func (r *BuildJobRepository) ListByRepositoryID(repositoryID uint) ([]model.BuildJob, error) {
	var items []model.BuildJob
	err := r.db.Where("repository_id = ?", repositoryID).Order("id ASC").Find(&items).Error
	return items, err
}
