package repository

import (
	"time"

	"gorm.io/gorm"
)

// ScriptWebhookDelivery records processed script-job webhook deliveries for idempotency.
type ScriptWebhookDelivery struct {
	ID          uint      `gorm:"primaryKey"`
	ScriptJobID uint      `gorm:"uniqueIndex:idx_script_wh_delivery;not null"`
	DeliveryKey string    `gorm:"size:200;uniqueIndex:idx_script_wh_delivery;not null"`
	CreatedAt   time.Time `gorm:""`
}

func (ScriptWebhookDelivery) TableName() string { return "script_webhook_deliveries" }

type ScriptWebhookDeliveryRepository struct{ db *gorm.DB }

func NewScriptWebhookDeliveryRepository(db *gorm.DB) *ScriptWebhookDeliveryRepository {
	return &ScriptWebhookDeliveryRepository{db: db}
}

// TryInsert returns true if this delivery is new; false if duplicate.
func (r *ScriptWebhookDeliveryRepository) TryInsert(scriptJobID uint, deliveryKey string) (bool, error) {
	row := &ScriptWebhookDelivery{ScriptJobID: scriptJobID, DeliveryKey: deliveryKey}
	err := r.db.Create(row).Error
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
