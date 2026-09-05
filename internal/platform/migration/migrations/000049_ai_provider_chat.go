package migrations

import (
	"context"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000049_ai_provider_chat", upAIProviderChat)
}

func upAIProviderChat(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	models := []any{
		&aiProviderMigrationModel{},
		&aiModelMigrationModel{},
		&aiChatSessionMigrationModel{},
		&aiChatMessageMigrationModel{},
	}
	for _, m := range models {
		if db.Migrator().HasTable(m) {
			continue
		}
		if err := db.Migrator().CreateTable(m); err != nil {
			return err
		}
	}
	return nil
}

type aiProviderMigrationModel struct {
	ID           uint      `gorm:"primaryKey"`
	Name         string    `gorm:"size:100;not null;uniqueIndex"`
	APIURL       string    `gorm:"size:500;not null"`
	APIKeyCipher string    `gorm:"type:text"`
	Enabled      bool      `gorm:"not null;default:true"`
	Notes        string    `gorm:"type:text"`
	CreatedBy    uint      `gorm:"index"`
	CreatedAt    time.Time `gorm:""`
	UpdatedAt    time.Time `gorm:""`
}

func (aiProviderMigrationModel) TableName() string { return "ai_providers" }

type aiModelMigrationModel struct {
	ID               uint      `gorm:"primaryKey"`
	ProviderID       uint      `gorm:"not null;index;uniqueIndex:uidx_provider_model_id"`
	Name             string    `gorm:"size:100;not null"`
	ModelID          string    `gorm:"size:100;not null;uniqueIndex:uidx_provider_model_id"`
	Enabled          bool      `gorm:"not null;default:true"`
	SortOrder        int       `gorm:"not null;default:0;index"`
	ReasoningEfforts string    `gorm:"type:text"`
	DefaultParams    string    `gorm:"type:text"`
	Notes            string    `gorm:"type:text"`
	CreatedAt        time.Time `gorm:""`
	UpdatedAt        time.Time `gorm:""`
}

func (aiModelMigrationModel) TableName() string { return "ai_models" }

type aiChatSessionMigrationModel struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;index"`
	Title     string    `gorm:"size:200;not null"`
	ModelID   string    `gorm:"size:100"`
	CreatedAt time.Time `gorm:""`
	UpdatedAt time.Time `gorm:"index"`
}

func (aiChatSessionMigrationModel) TableName() string { return "ai_chat_sessions" }

type aiChatMessageMigrationModel struct {
	ID               uint      `gorm:"primaryKey"`
	SessionID        uint      `gorm:"not null;index"`
	UserID           uint      `gorm:"not null;index"`
	Role             string    `gorm:"size:20;not null"`
	Content          string    `gorm:"type:text;not null"`
	ReasoningContent string    `gorm:"type:text"`
	CreatedAt        time.Time `gorm:""`
	UpdatedAt        time.Time `gorm:""`
}

func (aiChatMessageMigrationModel) TableName() string { return "ai_chat_messages" }
