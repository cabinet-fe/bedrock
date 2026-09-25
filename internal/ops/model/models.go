package model

import "time"

const (
	DevEnvBuiltin = "builtin"
	DevEnvCustom  = "custom"
)

const (
	JobQueued      = "queued"
	JobRunning     = "running"
	JobSuccess     = "success"
	JobFailed      = "failed"
	JobInterrupted = "interrupted"
)

// DevEnvironment describes a host language/runtime and the scripts that manage it.
// Custom scripts intentionally execute under the Bedrock process UID; this is
// an administrator-only capability, not a sandbox.
type DevEnvironment struct {
	ID              uint                  `json:"id" gorm:"primaryKey"`
	Name            string                `json:"name" gorm:"size:100;not null;uniqueIndex"`
	Kind            string                `json:"kind" gorm:"size:20;not null;default:builtin"`
	Executable      string                `json:"executable" gorm:"size:200;not null"`
	Description     string                `json:"description" gorm:"size:500"`
	DetectScript    string                `json:"detect_script" gorm:"type:text"`
	InstallScript   string                `json:"install_script" gorm:"type:text"`
	UpgradeScript   string                `json:"upgrade_script" gorm:"type:text"`
	UninstallScript string                `json:"uninstall_script" gorm:"type:text"`
	VersionsScript  string                `json:"versions_script" gorm:"type:text"`
	SwitchScript    string                `json:"switch_script" gorm:"type:text"`
	DefaultVersion  string                `json:"default_version" gorm:"size:100"`
	CreatedBy       uint                  `json:"created_by" gorm:"index"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	Sources         []DevEnvInstallSource `json:"sources,omitempty" gorm:"foreignKey:EnvironmentID"`
}

func (DevEnvironment) TableName() string { return "dev_environments" }

// DevEnvInstallSource is attempted in ascending priority for install-like jobs
// on a single development environment.
type DevEnvInstallSource struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	EnvironmentID uint      `json:"environment_id" gorm:"not null;uniqueIndex:uidx_dev_env_source_name;index"`
	Name          string    `json:"name" gorm:"size:100;not null;uniqueIndex:uidx_dev_env_source_name"`
	BaseURL       string    `json:"base_url" gorm:"size:1000;not null"`
	Priority      int       `json:"priority" gorm:"not null;index"`
	Enabled       bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (DevEnvInstallSource) TableName() string { return "dev_env_install_sources" }

// DevEnvJob persists async execution, including command snapshots and
// append-only textual logs required for troubleshooting source fallback.
type DevEnvJob struct {
	ID               uint                 `json:"id" gorm:"primaryKey"`
	EnvironmentID    uint                 `json:"environment_id" gorm:"not null;index"`
	Operation        string               `json:"operation" gorm:"size:20;not null"`
	RequestedVersion string               `json:"requested_version" gorm:"size:100"`
	Status           string               `json:"status" gorm:"size:20;not null;default:queued;index"`
	SourceID         *uint                `json:"source_id" gorm:"index"`
	CommandSnapshot  string               `json:"command_snapshot" gorm:"type:text"`
	LogText          string               `json:"-" gorm:"type:text"`
	ErrorMessage     string               `json:"error_message" gorm:"type:text"`
	CreatedBy        uint                 `json:"created_by" gorm:"index"`
	StartedAt        *time.Time           `json:"started_at"`
	FinishedAt       *time.Time           `json:"finished_at"`
	CreatedAt        time.Time            `json:"created_at"`
	Environment      DevEnvironment       `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID"`
	Source           *DevEnvInstallSource `json:"source,omitempty" gorm:"foreignKey:SourceID"`
}

func (DevEnvJob) TableName() string { return "dev_env_jobs" }

type ProcessInfo struct {
	PID         int32    `json:"pid"`
	Name        string   `json:"name"`
	CPUPercent  float64  `json:"cpu_percent"`
	MemoryBytes uint64   `json:"memory_bytes"`
	Username    string   `json:"username"`
	NumThreads  int32    `json:"num_threads"`
	Status      string   `json:"status"`
	StartTime   int64    `json:"start_time"`
	Cmdline     string   `json:"cmdline"`
	Ports       []uint32 `json:"ports"`
}

type ProcessListOptions struct {
	Keyword string
	PID     *int32
	Port    *uint32
	// Sort is ProTable style: "<field>@asc" | "<field>@desc"
	// (cpu_percent | memory_bytes | name). Empty keeps PID order.
	Sort string
}
