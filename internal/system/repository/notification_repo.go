package repository

import (
	"gorm.io/gorm"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
)

type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) Create(n *model.Notification) error {
	return r.db.Create(n).Error
}

// ListByUser 按用户分页查询通知；isRead 非 nil 时按已读状态过滤。
func (r *NotificationRepository) ListByUser(userID uint, isRead *bool, q pkg.ListQuery) ([]model.Notification, int64, error) {
	var items []model.Notification
	var total int64
	where := func(db *gorm.DB) *gorm.DB {
		db = db.Where("user_id = ?", userID)
		if isRead != nil {
			db = db.Where("is_read = ?", *isRead)
		}
		return db
	}
	if err := where(r.db.Model(&model.Notification{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := where(r.db).
		Order("created_at DESC").
		Offset(q.Offset()).
		Limit(q.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *NotificationRepository) MarkRead(id, userID uint) error {
	return r.db.Model(&model.Notification{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("is_read", true).Error
}

func (r *NotificationRepository) MarkAllRead(userID uint) error {
	return r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Update("is_read", true).Error
}

func (r *NotificationRepository) CountUnread(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Count(&count).Error
	return count, err
}
