package repository

import (
	"bedrock/internal/rbac"
	"bedrock/internal/rbac/model"

	"gorm.io/gorm"
)

type ResourceRepository struct {
	db *gorm.DB
}

func NewResourceRepository(db *gorm.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) Create(res *model.RbacResource) error {
	return r.db.Create(res).Error
}

func (r *ResourceRepository) FindByID(id uint) (*model.RbacResource, error) {
	var res model.RbacResource
	err := r.db.First(&res, id).Error
	return &res, err
}

func (r *ResourceRepository) FindByFullCode(fullCode string) (*model.RbacResource, error) {
	var res model.RbacResource
	err := r.db.Where("full_code = ?", fullCode).First(&res).Error
	return &res, err
}

func (r *ResourceRepository) ListAll() ([]model.RbacResource, error) {
	var items []model.RbacResource
	err := r.db.Order("sort_key ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *ResourceRepository) ListMenus() ([]model.RbacResource, error) {
	var items []model.RbacResource
	err := r.db.Where("type = ?", model.ResourceTypeMenu).
		Order("sort_key ASC, id ASC").
		Find(&items).Error
	return items, err
}

func (r *ResourceRepository) ListFeatures() ([]model.RbacResource, error) {
	var items []model.RbacResource
	err := r.db.Where("type IN ?", []string{model.ResourceTypeAction, model.ResourceTypeCard}).
		Order("sort_key ASC, id ASC").
		Find(&items).Error
	return items, err
}

func (r *ResourceRepository) ListByFullCodes(fullCodes []string) ([]model.RbacResource, error) {
	if len(fullCodes) == 0 {
		return nil, nil
	}
	var items []model.RbacResource
	err := r.db.Where("full_code IN ?", fullCodes).Find(&items).Error
	return items, err
}

func (r *ResourceRepository) ListByParentID(parentID uint) ([]model.RbacResource, error) {
	var items []model.RbacResource
	err := r.db.Where("parent_id = ?", parentID).
		Order("sort_key ASC, id ASC").
		Find(&items).Error
	return items, err
}

func (r *ResourceRepository) Update(res *model.RbacResource) error {
	return r.db.Save(res).Error
}

func (r *ResourceRepository) Delete(id uint) error {
	return r.db.Delete(&model.RbacResource{}, id).Error
}

func (r *ResourceRepository) CountChildren(parentID uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.RbacResource{}).Where("parent_id = ?", parentID).Count(&n).Error
	return n, err
}

func (r *ResourceRepository) DeleteRolePermissionsByFullCodes(fullCodes []string) error {
	if len(fullCodes) == 0 {
		return nil
	}
	return r.db.Where("permission IN ?", fullCodes).Delete(&model.RolePermission{}).Error
}

// IsSuperAdminOnly reports whether the permission's resource (by full_code) is gated.
// Falls back to looking up the menu code (left of ':') when the exact full_code is missing.
func (r *ResourceRepository) IsSuperAdminOnly(fullCode string) (bool, error) {
	var res model.RbacResource
	err := r.db.Where("full_code = ?", fullCode).First(&res).Error
	if err == nil {
		return res.SuperAdminOnly, nil
	}
	if err != gorm.ErrRecordNotFound {
		return false, err
	}
	// Menu-level gate: feature under a super_admin_only menu.
	menuCode, _, ok := rbac.SplitPermission(fullCode)
	if !ok {
		return false, nil
	}
	err = r.db.Where("full_code = ? AND type = ?", menuCode, model.ResourceTypeMenu).First(&res).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return res.SuperAdminOnly, nil
}
