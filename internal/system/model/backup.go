package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Backup module constants
const (
	BackupModuleDatabase  = "database"
	BackupModuleConfig    = "config"
	BackupModuleStorage   = "storage"
	BackupModuleArtifacts = "artifacts"
	BackupModuleLogs      = "logs"
)

// AllBackupModules lists all supported backup module identifiers.
var AllBackupModules = []string{
	BackupModuleDatabase,
	BackupModuleConfig,
	BackupModuleStorage,
	BackupModuleArtifacts,
	BackupModuleLogs,
}

// IsValidBackupModule checks whether the given module name is supported.
func IsValidBackupModule(module string) bool {
	switch module {
	case BackupModuleDatabase, BackupModuleConfig, BackupModuleStorage, BackupModuleArtifacts, BackupModuleLogs:
		return true
	default:
		return false
	}
}

// Backup status constants
const (
	BackupStatusProcessing = "processing"
	BackupStatusSuccess    = "success"
	BackupStatusFailed     = "failed"
)

// Backup manifest constants
const (
	BackupManifestFilename = "manifest.json"
	BackupManifestVersion  = "1.0"
)

// SystemBackup represents a backup record in the system_backups table.
type SystemBackup struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Filename     string    `json:"filename" gorm:"size:255;not null"`
	FilePath     string    `json:"file_path" gorm:"size:500;not null"`
	FileSize     int64     `json:"file_size" gorm:"not null;default:0"`
	ModulesJSON  string    `json:"-" gorm:"column:modules_json;type:text;not null"`
	Modules      []string  `json:"modules" gorm:"-"`
	Note         string    `json:"note" gorm:"size:500"`
	Status       string    `json:"status" gorm:"size:50;not null;default:processing;index"`
	ErrorMessage string    `json:"error_message,omitempty" gorm:"type:text"`
	CreatedBy    uint      `json:"created_by" gorm:"index"`
	CreatedAt    time.Time `json:"created_at" gorm:"index"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (SystemBackup) TableName() string { return "system_backups" }

// AfterFind unmarshals ModulesJSON into Modules slice.
func (b *SystemBackup) AfterFind(tx *gorm.DB) error {
	b.DecodeModules()
	return nil
}

// BeforeSave marshals Modules into ModulesJSON.
func (b *SystemBackup) BeforeSave(tx *gorm.DB) error {
	return b.EncodeModules()
}

// EncodeModules encodes the Modules slice to ModulesJSON.
func (b *SystemBackup) EncodeModules() error {
	if b.Modules == nil {
		b.Modules = []string{}
	}
	data, err := json.Marshal(b.Modules)
	if err != nil {
		return err
	}
	b.ModulesJSON = string(data)
	return nil
}

// DecodeModules decodes ModulesJSON into the Modules slice.
func (b *SystemBackup) DecodeModules() {
	if b.ModulesJSON != "" {
		var list []string
		if err := json.Unmarshal([]byte(b.ModulesJSON), &list); err == nil {
			b.Modules = list
			return
		}
	}
	if b.Modules == nil {
		b.Modules = []string{}
	}
}

// BackupManifest represents the manifest.json inside the backup archive.
type BackupManifest struct {
	Version    string    `json:"version"`               // Manifest specification version, e.g. "1.0"
	AppVersion string    `json:"app_version,omitempty"` // Bedrock version
	DBDriver   string    `json:"db_driver"`             // Database driver: sqlite / mysql / postgres
	Modules    []string  `json:"modules"`               // Included module list: database, config, storage, artifacts, logs
	CreatedAt  time.Time `json:"created_at"`            // Backup generation timestamp
	Note       string    `json:"note,omitempty"`        // Backup note
}

// SystemBackupCreateRequest represents the request body for creating a backup.
type SystemBackupCreateRequest struct {
	Modules []string `json:"modules" binding:"required"`
	Note    string   `json:"note"`
}

// SystemBackupRestoreRequest represents the request body for restoring a backup.
type SystemBackupRestoreRequest struct {
	BackupID      *uint  `json:"backup_id"`                         // ID of existing backup record
	UploadToken   string `json:"upload_token"`                      // Token from uploaded backup pre-inspection
	AdminPassword string `json:"admin_password" binding:"required"` // Current login admin password
	AutoSnapshot  *bool  `json:"auto_snapshot"`                     // Default true: whether to create snapshot before restoring
}

// ShouldAutoSnapshot returns whether auto snapshot is enabled (defaults to true if nil).
func (r *SystemBackupRestoreRequest) ShouldAutoSnapshot() bool {
	if r.AutoSnapshot == nil {
		return true
	}
	return *r.AutoSnapshot
}

// BackupInspectResult represents the response of backup package pre-inspection.
type BackupInspectResult struct {
	UploadToken string          `json:"upload_token"`
	Manifest    *BackupManifest `json:"manifest"`
	Compatible  bool            `json:"compatible"`
	Message     string          `json:"message,omitempty"`
}

// SystemBackupRestoreResponse represents the response of restore action.
type SystemBackupRestoreResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
