package model

import "time"

// Credential is the shared secret store (AES-GCM ciphertext; never echoed by API).
type Credential struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	Name             string    `json:"name" gorm:"size:100;not null;uniqueIndex:idx_cred_name_creator"`
	Type             string    `json:"type" gorm:"size:20;not null"`
	Username         string    `json:"username" gorm:"size:200"`
	SecretCipher     string    `json:"-" gorm:"size:4000"`
	PassphraseCipher string    `json:"-" gorm:"size:2000"`
	Description      string    `json:"description" gorm:"size:500"`
	CreatedBy        uint      `json:"created_by" gorm:"not null;uniqueIndex:idx_cred_name_creator"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	HasSecret     bool `json:"has_secret" gorm:"-"`
	HasPassphrase bool `json:"has_passphrase" gorm:"-"`
}

func (Credential) TableName() string { return "credentials" }

// Repository is a Git source configuration (URL + auth only).
type Repository struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	Name             string     `json:"name" gorm:"size:100;not null;uniqueIndex"`
	Description      string     `json:"description" gorm:"size:500"`
	Tags             string     `json:"tags" gorm:"size:500"`
	RepoURL          string     `json:"repo_url" gorm:"size:500;not null"`
	AuthType         string     `json:"auth_type" gorm:"size:20;not null;default:none"`
	CredentialID     *uint      `json:"credential_id" gorm:"index"`
	BranchesJSON     string     `json:"-" gorm:"type:text"`
	Branches         []string   `json:"branches" gorm:"-"`
	BranchesSyncedAt *time.Time `json:"branches_synced_at"`
	CreatedBy        uint       `json:"created_by" gorm:"index"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (Repository) TableName() string { return "repositories" }

// Server is a deploy host.
// password: AES-GCM in PasswordCipher (API write-only password / has_password).
// ssh_key: host SSH agent (SSH_AUTH_SOCK); no private key stored.
// agent: AgentURL + optional AgentCredentialID (requires resource_credentials:use).
type Server struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	Name              string    `json:"name" gorm:"size:100;not null"`
	Host              string    `json:"host" gorm:"size:200;not null"`
	Port              int       `json:"port" gorm:"default:22"`
	OSType            string    `json:"os_type" gorm:"size:20;not null;default:linux"`
	Username          string    `json:"username" gorm:"size:100"`
	AuthType          string    `json:"auth_type" gorm:"size:20;not null;default:password"`
	CredentialID      *uint     `json:"-" gorm:"index"` // legacy column; unused by service
	PasswordCipher    string    `json:"-" gorm:"type:text"`
	AgentURL          string    `json:"agent_url" gorm:"size:500"`
	AgentCredentialID *uint     `json:"agent_credential_id" gorm:"index"`
	Description       string    `json:"description" gorm:"size:500"`
	Tags              string    `json:"tags" gorm:"size:500"`
	Status            string    `json:"status" gorm:"size:20;default:unknown"`
	CreatedBy         uint      `json:"created_by" gorm:"index"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	HasPassword bool `json:"has_password" gorm:"-"`
}

func (Server) TableName() string { return "servers" }
