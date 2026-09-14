package repository

import (
	"errors"

	"gorm.io/gorm"

	"bedrock/internal/system/model"
)

// MailRepository stores the single-row system-level SMTP config.
type MailRepository struct {
	db *gorm.DB
}

func NewMailRepository(db *gorm.DB) *MailRepository {
	return &MailRepository{db: db}
}

// Find returns the SMTP config row, or nil when not configured yet.
func (r *MailRepository) Find() (*model.MailSMTPConfig, error) {
	var cfg model.MailSMTPConfig
	err := r.db.First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *MailRepository) Create(cfg *model.MailSMTPConfig) error {
	return r.db.Create(cfg).Error
}

func (r *MailRepository) Update(cfg *model.MailSMTPConfig) error {
	return r.db.Save(cfg).Error
}
