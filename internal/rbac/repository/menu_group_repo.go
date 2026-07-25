package repository

import (
	"bedrock/internal/rbac/model"

	"gorm.io/gorm"
)

type MenuGroupRepository struct {
	db *gorm.DB
}

func NewMenuGroupRepository(db *gorm.DB) *MenuGroupRepository {
	return &MenuGroupRepository{db: db}
}

func (r *MenuGroupRepository) Create(g *model.MenuGroup) error {
	return r.db.Create(g).Error
}

func (r *MenuGroupRepository) FindByID(id uint) (*model.MenuGroup, error) {
	var g model.MenuGroup
	err := r.db.First(&g, id).Error
	return &g, err
}

func (r *MenuGroupRepository) List() ([]model.MenuGroup, error) {
	var items []model.MenuGroup
	err := r.db.Order("sort_key ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *MenuGroupRepository) Update(g *model.MenuGroup) error {
	return r.db.Save(g).Error
}

func (r *MenuGroupRepository) Delete(id uint) error {
	return r.db.Delete(&model.MenuGroup{}, id).Error
}

func (r *MenuGroupRepository) CountMenus(groupID uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.RbacResource{}).
		Where("group_id = ? AND type = ?", groupID, model.ResourceTypeMenu).
		Count(&n).Error
	return n, err
}
