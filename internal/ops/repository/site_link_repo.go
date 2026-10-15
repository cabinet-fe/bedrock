package repository

import "bedrock/internal/ops/model"

func (r *OpsRepository) ListSiteLinks() ([]model.SiteLink, error) {
	var items []model.SiteLink
	err := r.db.Order("updated_at DESC").Find(&items).Error
	return items, err
}

func (r *OpsRepository) FindSiteLink(id uint) (*model.SiteLink, error) {
	var item model.SiteLink
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpsRepository) CreateSiteLink(item *model.SiteLink) error {
	return r.db.Create(item).Error
}

func (r *OpsRepository) UpdateSiteLink(item *model.SiteLink) error {
	return r.db.Save(item).Error
}

func (r *OpsRepository) DeleteSiteLink(id uint) error {
	return r.db.Delete(&model.SiteLink{}, id).Error
}
