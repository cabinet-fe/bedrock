package model

import "time"

// SiteLink is one test-environment website with a single account.
// PasswordCipher holds AES-GCM ciphertext; an empty password is an empty string.
// The row is not scoped to a product project.
type SiteLink struct {
	ID             uint   `json:"id" gorm:"primaryKey"`
	Name           string `json:"name" gorm:"size:200;not null"`
	URL            string `json:"url" gorm:"column:url;size:2000;not null"`
	IconBase64     string `json:"icon_base64" gorm:"type:text;not null"`
	IconMime       string `json:"icon_mime" gorm:"size:64;not null;default:''"`
	Account        string `json:"account" gorm:"size:200;not null;default:''"`
	PasswordCipher string `json:"-" gorm:"column:password_cipher;type:text;not null"`
	Remark         string `json:"remark" gorm:"type:text;not null"`
	// No default tag: GORM skips zero values when default is set, which would store false as true.
	Enabled   bool      `json:"enabled" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SiteLink) TableName() string { return "site_links" }
