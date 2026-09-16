package migrations

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"bedrock/internal/platform/migration"
)

func init() {
	migration.Register("000009_ai", upAI)
}

func upAI(ctx context.Context, db *gorm.DB, driver migration.Driver) error {
	_ = ctx
	_ = driver
	models := []interface{}{
		&cliRuntimeDefinitionMigrationModel{},
		&cliInstallSourceMigrationModel{},
		&cliInstallJobMigrationModel{},
		&aiAgentMigrationModel{},
		&agentTriggerMigrationModel{},
		&agentRunMigrationModel{},
		&skillPackageMigrationModel{},
		&personalAccessTokenMigrationModel{},
	}
	for _, item := range models {
		if db.Migrator().HasTable(item) {
			continue
		}
		if err := db.Migrator().CreateTable(item); err != nil {
			return err
		}
	}
	if err := ensureBuildJobAgentID(db); err != nil {
		return err
	}
	return seedAICLIDefinitions(db)
}

func ensureBuildJobAgentID(db *gorm.DB) error {
	if db.Migrator().HasColumn(&buildJobAgentMigrationModel{}, "agent_id") {
		return nil
	}
	return db.Migrator().AddColumn(&buildJobAgentMigrationModel{}, "AgentID")
}

type buildJobAgentMigrationModel struct {
	ID      uint  `gorm:"primaryKey"`
	AgentID *uint `gorm:"index"`
}

func (buildJobAgentMigrationModel) TableName() string { return "build_jobs" }

type cliRuntimeDefinitionMigrationModel struct {
	ID                uint      `gorm:"primaryKey"`
	Key               string    `gorm:"size:40;not null;uniqueIndex"`
	Name              string    `gorm:"size:100;not null"`
	BinaryName        string    `gorm:"size:100;not null"`
	Description       string    `gorm:"size:500"`
	DetectCommand     string    `gorm:"type:text"`
	InstallTemplate   string    `gorm:"type:text"`
	UpgradeTemplate   string    `gorm:"type:text"`
	UninstallTemplate string    `gorm:"type:text"`
	DefaultArgs       string    `gorm:"type:text"`
	EnvTemplateJSON   string    `gorm:"type:text"`
	APIBaseEnv        string    `gorm:"size:100"`
	HealthCommand     string    `gorm:"type:text"`
	InstallStatus     string    `gorm:"size:40;default:unknown"`
	InstalledPath     string    `gorm:"size:500"`
	InstalledVersion  string    `gorm:"size:100"`
	Healthy           bool      `gorm:"not null;default:false"`
	CreatedAt         time.Time `gorm:""`
	UpdatedAt         time.Time `gorm:""`
}

func (cliRuntimeDefinitionMigrationModel) TableName() string { return "cli_runtime_definitions" }

type cliInstallSourceMigrationModel struct {
	ID        uint      `gorm:"primaryKey"`
	CliKey    string    `gorm:"size:40;not null;index"`
	Name      string    `gorm:"size:100;not null"`
	BaseURL   string    `gorm:"size:1000;not null"`
	Priority  int       `gorm:"not null;index"`
	Enabled   bool      `gorm:"not null;default:true"`
	CreatedAt time.Time `gorm:""`
	UpdatedAt time.Time `gorm:""`
}

func (cliInstallSourceMigrationModel) TableName() string { return "cli_install_sources" }

type cliInstallJobMigrationModel struct {
	ID               uint       `gorm:"primaryKey"`
	CliKey           string     `gorm:"size:40;not null;index"`
	Operation        string     `gorm:"size:20;not null"`
	RequestedVersion string     `gorm:"size:100"`
	Status           string     `gorm:"size:20;not null;default:queued;index"`
	SourceID         *uint      `gorm:"index"`
	CommandSnapshot  string     `gorm:"type:text"`
	LogText          string     `gorm:"type:text"`
	ErrorMessage     string     `gorm:"type:text"`
	CreatedBy        uint       `gorm:"index"`
	StartedAt        *time.Time `gorm:""`
	FinishedAt       *time.Time `gorm:""`
	CreatedAt        time.Time  `gorm:""`
}

func (cliInstallJobMigrationModel) TableName() string { return "cli_install_jobs" }

type aiAgentMigrationModel struct {
	ID           uint      `gorm:"primaryKey"`
	Name         string    `gorm:"size:100;not null"`
	Description  string    `gorm:"size:500"`
	Enabled      bool      `gorm:"not null;default:true"`
	SystemPrompt string    `gorm:"type:text"`
	SkillIDsJSON string    `gorm:"type:text"`
	RepositoryID *uint     `gorm:"index"`
	TimeoutSec   int       `gorm:"not null;default:600"`
	CreatedBy    uint      `gorm:"index"`
	CreatedAt    time.Time `gorm:""`
	UpdatedAt    time.Time `gorm:""`
}

func (aiAgentMigrationModel) TableName() string { return "ai_agents" }

type agentTriggerMigrationModel struct {
	ID             uint      `gorm:"primaryKey"`
	AgentID        uint      `gorm:"not null;index"`
	Type           string    `gorm:"size:40;not null"`
	Enabled        bool      `gorm:"not null;default:true"`
	CronExpression string    `gorm:"size:100"`
	CronTimezone   string    `gorm:"size:100;default:UTC"`
	BuildJobID     *uint     `gorm:"index"`
	BuildEvent     string    `gorm:"size:40"`
	CreatedAt      time.Time `gorm:""`
	UpdatedAt      time.Time `gorm:""`
}

func (agentTriggerMigrationModel) TableName() string { return "agent_triggers" }

type agentRunMigrationModel struct {
	ID              uint       `gorm:"primaryKey"`
	AgentID         uint       `gorm:"not null;index"`
	TriggerType     string     `gorm:"size:40;not null"`
	TriggerID       *uint      `gorm:"index"`
	Status          string     `gorm:"size:20;not null;default:queued;index"`
	TriggeredBy     uint       `gorm:"index"`
	BuildRunID      *uint      `gorm:"index"`
	ProjectID       *uint      `gorm:"index"`
	DocNodeID       *uint      `gorm:"index"`
	SnapshotJSON    string     `gorm:"type:text"`
	SkillDigestJSON string     `gorm:"type:text"`
	LogPath         string     `gorm:"size:500"`
	OutputText      string     `gorm:"type:text"`
	ErrorMessage    string     `gorm:"type:text"`
	DurationMs      int64      `gorm:""`
	StartedAt       *time.Time `gorm:""`
	FinishedAt      *time.Time `gorm:""`
	CreatedAt       time.Time  `gorm:""`
}

func (agentRunMigrationModel) TableName() string { return "agent_runs" }

type skillPackageMigrationModel struct {
	ID              uint      `gorm:"primaryKey"`
	Name            string    `gorm:"size:200;not null"`
	Description     string    `gorm:"size:1000"`
	Visibility      string    `gorm:"size:20;not null;default:private;index"`
	StorageObjectID uint      `gorm:"not null;index"`
	PackageDigest   string    `gorm:"size:64;not null;index"`
	SizeBytes       int64     `gorm:"not null"`
	CreatedBy       uint      `gorm:"index"`
	UpdatedBy       uint      `gorm:"index"`
	CreatedAt       time.Time `gorm:""`
	UpdatedAt       time.Time `gorm:""`
}

func (skillPackageMigrationModel) TableName() string { return "skill_packages" }

type personalAccessTokenMigrationModel struct {
	ID          uint       `gorm:"primaryKey"`
	UserID      uint       `gorm:"not null;index"`
	Name        string     `gorm:"size:100;not null"`
	TokenPrefix string     `gorm:"size:16;not null"`
	TokenHash   string     `gorm:"size:128;not null;uniqueIndex"`
	ScopesJSON  string     `gorm:"type:text;not null"`
	ExpiresAt   *time.Time `gorm:""`
	RevokedAt   *time.Time `gorm:""`
	LastUsedAt  *time.Time `gorm:""`
	CreatedAt   time.Time  `gorm:""`
}

func (personalAccessTokenMigrationModel) TableName() string { return "personal_access_tokens" }

func seedAICLIDefinitions(db *gorm.DB) error {
	now := time.Now().UTC()
	defs := []cliRuntimeDefinitionMigrationModel{
		{
			Key: "opencode", Name: "OpenCode", BinaryName: "opencode",
			Description:       "OpenCode CLI（同 UID 执行，无沙箱）",
			DetectCommand:     "command -v opencode && opencode --version",
			HealthCommand:     "opencode --version",
			InstallTemplate:   npmCLIInstallTemplate("opencode-ai", "opencode"),
			UpgradeTemplate:   npmCLIUpgradeTemplate("opencode-ai", "opencode"),
			UninstallTemplate: npmCLIUninstallTemplate("opencode-ai"),
			DefaultArgs:       "run", APIBaseEnv: "OPENCODE_API_BASE",
			EnvTemplateJSON: `{"OPENCODE_API_KEY":""}`,
			InstallStatus:   "unknown", CreatedAt: now, UpdatedAt: now,
		},
		{
			Key: "reasonix", Name: "Reasonix", BinaryName: "reasonix",
			Description:       "Reasonix CLI（同 UID 执行，无沙箱）",
			DetectCommand:     "command -v reasonix && reasonix --version",
			HealthCommand:     "reasonix --version",
			InstallTemplate:   npmCLIInstallTemplate("reasonix", "reasonix"),
			UpgradeTemplate:   npmCLIUpgradeTemplate("reasonix", "reasonix"),
			UninstallTemplate: npmCLIUninstallTemplate("reasonix"),
			DefaultArgs:       "run", APIBaseEnv: "REASONIX_API_BASE",
			EnvTemplateJSON: `{"REASONIX_API_KEY":""}`,
			InstallStatus:   "unknown", CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, def := range defs {
		var existing cliRuntimeDefinitionMigrationModel
		// Map condition: `key` is a MySQL reserved word and must stay driver-quoted.
		err := db.Where(map[string]interface{}{"key": def.Key}).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&def).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	sources := []cliInstallSourceMigrationModel{
		{CliKey: "opencode", Name: "npm registry", BaseURL: "https://registry.npmjs.org", Priority: 10, Enabled: true},
		{CliKey: "opencode", Name: "npm mirror", BaseURL: "https://registry.npmmirror.com", Priority: 20, Enabled: true},
		{CliKey: "reasonix", Name: "npm registry", BaseURL: "https://registry.npmjs.org", Priority: 10, Enabled: true},
		{CliKey: "reasonix", Name: "npm mirror", BaseURL: "https://registry.npmmirror.com", Priority: 20, Enabled: true},
	}
	for _, source := range sources {
		var existing cliInstallSourceMigrationModel
		err := db.Where("cli_key = ? AND name = ?", source.CliKey, source.Name).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&source).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}
