package model

import "time"

// PAT scopes (fixed whitelist, DESIGN D17).
const (
	ScopeSkillsRead = "skills:read"
	ScopeAgentsRun  = "agents:run"
	ScopeDocsRead   = "docs:read"
	ScopeDocsWrite  = "docs:write"
)

// PersonalAccessToken stores SHA-256 hash for auth plus AES-GCM ciphertext for owner reveal.
// Reveal API returns TokenCipher (client decrypts); legacy rows may lack it (Copyable=false).
type PersonalAccessToken struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	UserID      uint       `json:"user_id" gorm:"not null;index"`
	Name        string     `json:"name" gorm:"size:100;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"size:16;not null"`
	TokenHash   string     `json:"-" gorm:"size:128;not null;uniqueIndex"`
	TokenCipher string     `json:"-" gorm:"type:text"`
	ScopesJSON  string     `json:"-" gorm:"type:text;not null"`
	Scopes      []string   `json:"scopes" gorm:"-"`
	Copyable    bool       `json:"copyable" gorm:"-"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (PersonalAccessToken) TableName() string { return "personal_access_tokens" }
