package repository

import (
	"time"

	"gorm.io/gorm"
)

// PipelineWebhookDelivery records processed pipeline webhook deliveries.
type PipelineWebhookDelivery struct {
	ID              uint      `gorm:"primaryKey"`
	BuildPipelineID uint      `gorm:"uniqueIndex:idx_pipeline_wh_delivery;not null"`
	DeliveryKey     string    `gorm:"size:200;uniqueIndex:idx_pipeline_wh_delivery;not null"`
	CreatedAt       time.Time `gorm:""`
}

func (PipelineWebhookDelivery) TableName() string { return "pipeline_webhook_deliveries" }

type PipelineWebhookDeliveryRepository struct{ db *gorm.DB }

func NewPipelineWebhookDeliveryRepository(db *gorm.DB) *PipelineWebhookDeliveryRepository {
	return &PipelineWebhookDeliveryRepository{db: db}
}

// TryInsert returns true if this delivery is new; false if duplicate.
func (r *PipelineWebhookDeliveryRepository) TryInsert(pipelineID uint, deliveryKey string) (bool, error) {
	row := &PipelineWebhookDelivery{BuildPipelineID: pipelineID, DeliveryKey: deliveryKey}
	err := r.db.Create(row).Error
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
