package model

import "time"

// MailSMTPConfig is the system-level SMTP sending account (single row).
// PasswordCipher stores the pkg.Encrypt AES-GCM ciphertext, never plaintext.
type MailSMTPConfig struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	Host           string    `json:"host" gorm:"size:255;not null;default:''"`
	Port           int       `json:"port" gorm:"not null;default:0"`
	Username       string    `json:"username" gorm:"size:255;not null;default:''"`
	PasswordCipher string    `json:"-" gorm:"column:password_cipher;size:1024;not null;default:''"`
	FromAddress    string    `json:"from_address" gorm:"size:255;not null;default:''"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (MailSMTPConfig) TableName() string { return "mail_smtp_configs" }
