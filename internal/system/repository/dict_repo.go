package repository

import (
	"bedrock/internal/pkg"
	"bedrock/internal/system/model"

	"gorm.io/gorm"
)

type DictionaryRepository struct {
	db *gorm.DB
}

func NewDictionaryRepository(db *gorm.DB) *DictionaryRepository {
	return &DictionaryRepository{db: db}
}

func (r *DictionaryRepository) Create(d *model.Dictionary) error {
	return r.db.Create(d).Error
}

func (r *DictionaryRepository) FindByID(id uint) (*model.Dictionary, error) {
	var d model.Dictionary
	err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).First(&d, id).Error
	return &d, err
}

func (r *DictionaryRepository) FindByCode(code string) (*model.Dictionary, error) {
	var d model.Dictionary
	err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).Where("code = ?", code).First(&d).Error
	return &d, err
}

func (r *DictionaryRepository) List(q pkg.ListQuery) ([]model.Dictionary, int64, error) {
	var items []model.Dictionary
	var total int64
	if err := r.db.Model(&model.Dictionary{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).Offset(q.Offset()).Limit(q.PageSize).Order("id DESC").Find(&items).Error
	return items, total, err
}

func (r *DictionaryRepository) Update(d *model.Dictionary) error {
	return r.db.Save(d).Error
}

func (r *DictionaryRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dictionary_id = ?", id).Delete(&model.DictItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Dictionary{}, id).Error
	})
}

func (r *DictionaryRepository) ReplaceItems(dictionaryID uint, items []model.DictItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dictionary_id = ?", dictionaryID).Delete(&model.DictItem{}).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].ID = 0
			items[i].DictionaryID = dictionaryID
			if err := tx.Create(&items[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
