package repository

import (
	"time"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"

	"gorm.io/gorm"
)

type OperationLogRepository struct {
	db *gorm.DB
}

func NewOperationLogRepository(db *gorm.DB) *OperationLogRepository {
	return &OperationLogRepository{db: db}
}

func (r *OperationLogRepository) Create(log *model.OperationLog) error {
	return r.db.Create(log).Error
}

type OperationLogFilters struct {
	pkg.ListQuery
	UserID       *uint  `form:"user_id"`
	Action       string `form:"action"`
	ResourceType string `form:"resource_type"`
	From         *time.Time
	To           *time.Time
}

func (r *OperationLogRepository) List(f OperationLogFilters) ([]model.OperationLog, int64, error) {
	q := r.db.Model(&model.OperationLog{})
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ResourceType != "" {
		q = q.Where("resource_type = ?", f.ResourceType)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at <= ?", *f.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := pkg.OrderBy(f.Sort, map[string]string{
		"created_at": "created_at",
	}, "id", "id DESC")
	var items []model.OperationLog
	err := q.Offset(f.Offset()).Limit(f.PageSize).Order(order).Find(&items).Error
	return items, total, err
}
