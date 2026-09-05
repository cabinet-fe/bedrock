package repository

import (
	"gorm.io/gorm"

	"bedrock/internal/ai/model"
)

type ProviderRepository struct {
	db *gorm.DB
}

func NewProviderRepository(db *gorm.DB) *ProviderRepository {
	return &ProviderRepository{db: db}
}

// CreateProvider creates a new AI provider.
func (r *ProviderRepository) CreateProvider(p *model.AiProvider) error {
	return r.db.Create(p).Error
}

// UpdateProvider updates an existing AI provider.
func (r *ProviderRepository) UpdateProvider(p *model.AiProvider) error {
	return r.db.Save(p).Error
}

// DeleteProvider deletes an AI provider and its associated models in a transaction.
func (r *ProviderRepository) DeleteProvider(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("provider_id = ?", id).Delete(&model.AiModel{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.AiProvider{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// FindProvider retrieves an AI provider by ID.
func (r *ProviderRepository) FindProvider(id uint) (*model.AiProvider, error) {
	var item model.AiProvider
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindProviderByName retrieves an AI provider by name.
func (r *ProviderRepository) FindProviderByName(name string) (*model.AiProvider, error) {
	var item model.AiProvider
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ListProviders returns a paginated list of AI providers.
func (r *ProviderRepository) ListProviders(page, pageSize int) ([]model.AiProvider, int64, error) {
	q := r.db.Model(&model.AiProvider{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var items []model.AiProvider
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// CreateModel creates a new AI model.
func (r *ProviderRepository) CreateModel(m *model.AiModel) error {
	return r.db.Create(m).Error
}

// UpdateModel updates an existing AI model.
func (r *ProviderRepository) UpdateModel(m *model.AiModel) error {
	return r.db.Save(m).Error
}

// DeleteModel deletes an AI model by ID.
func (r *ProviderRepository) DeleteModel(id uint) error {
	res := r.db.Delete(&model.AiModel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FindModel retrieves an AI model by ID.
func (r *ProviderRepository) FindModel(id uint) (*model.AiModel, error) {
	var item model.AiModel
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindModelByProviderAndModelID retrieves an AI model by provider ID and model ID.
func (r *ProviderRepository) FindModelByProviderAndModelID(providerID uint, modelID string) (*model.AiModel, error) {
	var item model.AiModel
	if err := r.db.Where("provider_id = ? AND model_id = ?", providerID, modelID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ListModelsByProvider returns models under a specific provider.
func (r *ProviderRepository) ListModelsByProvider(providerID uint, page, pageSize int) ([]model.AiModel, int64, error) {
	q := r.db.Model(&model.AiModel{}).Where("provider_id = ?", providerID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 && pageSize < 1 {
		var items []model.AiModel
		err := q.Order("sort_order ASC, id ASC").Find(&items).Error
		return items, total, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var items []model.AiModel
	err := q.Order("sort_order ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// ListModels returns all models matching the optional filters.
func (r *ProviderRepository) ListModels(providerID *uint, enabled *bool) ([]model.AiModel, error) {
	q := r.db.Model(&model.AiModel{})
	if providerID != nil {
		q = q.Where("provider_id = ?", *providerID)
	}
	if enabled != nil {
		q = q.Where("enabled = ?", *enabled)
	}
	var items []model.AiModel
	err := q.Order("sort_order ASC, id ASC").Find(&items).Error
	return items, err
}
