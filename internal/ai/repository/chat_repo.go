package repository

import (
	"time"

	"gorm.io/gorm"

	"bedrock/internal/ai/model"
)

type ChatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{db: db}
}

// CreateSession creates a new chat session for a user.
func (r *ChatRepository) CreateSession(s *model.ChatSession) error {
	now := time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	return r.db.Create(s).Error
}

// UpdateSession updates an existing chat session.
func (r *ChatRepository) UpdateSession(s *model.ChatSession) error {
	s.UpdatedAt = time.Now()
	res := r.db.Model(&model.ChatSession{}).
		Where("id = ? AND user_id = ?", s.ID, s.UserID).
		Updates(map[string]any{
			"title":      s.Title,
			"model_id":   s.ModelID,
			"updated_at": s.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteSession cascades deletion of session and its messages within a transaction.
func (r *ChatRepository) DeleteSession(id, userID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Verify session exists and belongs to user
		var s model.ChatSession
		if err := tx.Where("id = ? AND user_id = ?", id, userID).First(&s).Error; err != nil {
			return err
		}

		// Delete messages in this session
		if err := tx.Where("session_id = ?", id).Delete(&model.ChatMessage{}).Error; err != nil {
			return err
		}

		// Delete the session
		if err := tx.Delete(&s).Error; err != nil {
			return err
		}

		return nil
	})
}

// FindSession retrieves a session ensuring it belongs to the given user.
func (r *ChatRepository) FindSession(id, userID uint) (*model.ChatSession, error) {
	var item model.ChatSession
	if err := r.db.Where("id = ? AND user_id = ?", id, userID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ListSessions returns user sessions ordered by updated_at descending.
func (r *ChatRepository) ListSessions(userID uint, page, pageSize int) ([]model.ChatSession, int64, error) {
	q := r.db.Model(&model.ChatSession{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 && pageSize < 1 {
		var items []model.ChatSession
		err := q.Order("updated_at DESC, id DESC").Find(&items).Error
		return items, total, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var items []model.ChatSession
	err := q.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

// CreateMessage saves a single chat message and touches session updated_at.
func (r *ChatRepository) CreateMessage(m *model.ChatMessage) error {
	now := time.Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		return tx.Model(&model.ChatSession{}).
			Where("id = ?", m.SessionID).
			Update("updated_at", now).Error
	})
}

// ListMessagesBySession returns messages in ascending chronological order for a user session.
func (r *ChatRepository) ListMessagesBySession(sessionID, userID uint) ([]model.ChatMessage, error) {
	// First ensure session exists and belongs to the user
	var session model.ChatSession
	if err := r.db.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return nil, err
	}

	var items []model.ChatMessage
	err := r.db.Where("session_id = ? AND user_id = ?", sessionID, userID).
		Order("id ASC").
		Find(&items).Error
	return items, err
}
