package repository

import (
	"gorm.io/gorm"

	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"
)

type PATRepository struct {
	db *gorm.DB
}

func NewPATRepository(db *gorm.DB) *PATRepository {
	return &PATRepository{db: db}
}

func (r *PATRepository) Create(token *model.PersonalAccessToken) error {
	return r.db.Create(token).Error
}

func (r *PATRepository) FindByHash(hash string) (*model.PersonalAccessToken, error) {
	var token model.PersonalAccessToken
	if err := r.db.Where("token_hash = ?", hash).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *PATRepository) Find(id uint) (*model.PersonalAccessToken, error) {
	var token model.PersonalAccessToken
	if err := r.db.First(&token, id).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *PATRepository) ListByUser(userID uint, q pkg.ListQuery) ([]model.PersonalAccessToken, int64, error) {
	db := r.db.Model(&model.PersonalAccessToken{}).Where("user_id = ?", userID)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.PersonalAccessToken
	err := db.Order("id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func (r *PATRepository) Update(token *model.PersonalAccessToken) error {
	return r.db.Save(token).Error
}

func (r *PATRepository) Delete(id uint) error {
	return r.db.Delete(&model.PersonalAccessToken{}, id).Error
}
