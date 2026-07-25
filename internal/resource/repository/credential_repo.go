package repository

import (
	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"

	"gorm.io/gorm"
)

type CredentialRepository struct{ db *gorm.DB }

func NewCredentialRepository(db *gorm.DB) *CredentialRepository {
	return &CredentialRepository{db: db}
}

func (r *CredentialRepository) Create(c *model.Credential) error {
	return r.db.Create(c).Error
}

func (r *CredentialRepository) Update(c *model.Credential) error {
	return r.db.Save(c).Error
}

func (r *CredentialRepository) Delete(id uint) error {
	return r.db.Delete(&model.Credential{}, id).Error
}

func (r *CredentialRepository) FindByID(id uint) (*model.Credential, error) {
	var c model.Credential
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CredentialRepository) List(q pkg.ListQuery, keyword string) ([]model.Credential, int64, error) {
	db := r.db.Model(&model.Credential{})
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.Credential
	err := db.Order("id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *CredentialRepository) CountByRepoRefs(id uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.Repository{}).Where("credential_id = ?", id).Count(&n).Error
	return n, err
}

func (r *CredentialRepository) CountByServerRefs(id uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.Server{}).
		Where("credential_id = ? OR agent_credential_id = ?", id, id).
		Count(&n).Error
	return n, err
}
