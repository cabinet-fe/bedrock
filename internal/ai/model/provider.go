package model

import "time"

// ReasoningEffortOption represents one reasoning effort tier with value and label.
type ReasoningEffortOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AiProvider represents an AI model provider configuration.
type AiProvider struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Name         string    `json:"name" gorm:"size:100;not null;uniqueIndex"`
	APIURL       string    `json:"api_url" gorm:"size:500;not null"`
	APIKeyCipher string    `json:"-" gorm:"column:api_key_cipher;type:text"`
	HasAPIKey    bool      `json:"has_api_key" gorm:"-"`
	Enabled      bool      `json:"enabled" gorm:"not null;default:true"`
	Notes        string    `json:"notes" gorm:"type:text"`
	CreatedBy    uint      `json:"created_by" gorm:"index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Models []AiModel `json:"models,omitempty" gorm:"foreignKey:ProviderID"`
}

func (AiProvider) TableName() string { return "ai_providers" }

// AiModel represents an AI model under a provider.
type AiModel struct {
	ID                   uint                    `json:"id" gorm:"primaryKey"`
	ProviderID           uint                    `json:"provider_id" gorm:"not null;index;uniqueIndex:uidx_provider_model_id"`
	Name                 string                  `json:"name" gorm:"size:100;not null"`
	ModelID              string                  `json:"model_id" gorm:"size:100;not null;uniqueIndex:uidx_provider_model_id"`
	Enabled              bool                    `json:"enabled" gorm:"not null;default:true"`
	SortOrder            int                     `json:"sort_order" gorm:"not null;default:0;index"`
	ReasoningEfforts     []ReasoningEffortOption `json:"reasoning_efforts" gorm:"-"`
	ReasoningEffortsJSON string                  `json:"-" gorm:"column:reasoning_efforts;type:text"`
	DefaultParams        map[string]any          `json:"default_params" gorm:"-"`
	DefaultParamsJSON    string                  `json:"-" gorm:"column:default_params;type:text"`
	Notes                string                  `json:"notes" gorm:"type:text"`
	CreatedAt            time.Time               `json:"created_at"`
	UpdatedAt            time.Time               `json:"updated_at"`

	Provider *AiProvider `json:"provider,omitempty" gorm:"foreignKey:ProviderID"`
}

func (AiModel) TableName() string { return "ai_models" }

// ProviderInput represents the payload to create or update an AiProvider.
type ProviderInput struct {
	Name    string `json:"name"`
	APIURL  string `json:"api_url"`
	APIKey  string `json:"api_key"`
	Enabled *bool  `json:"enabled"`
	Notes   string `json:"notes"`
}

// ModelInput represents the payload to create or update an AiModel.
type ModelInput struct {
	Name             string                  `json:"name"`
	ModelID          string                  `json:"model_id"`
	Enabled          *bool                   `json:"enabled"`
	SortOrder        *int                    `json:"sort_order"`
	ReasoningEfforts []ReasoningEffortOption `json:"reasoning_efforts"`
	DefaultParams    any                     `json:"default_params"`
	Notes            string                  `json:"notes"`
}
