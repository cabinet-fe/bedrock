package model

import "time"

const (
	JobQueued      = "queued"
	JobRunning     = "running"
	JobSuccess     = "success"
	JobFailed      = "failed"
	JobInterrupted = "interrupted"
	JobCancelled   = "cancelled"
	JobPending     = "pending"
)

const (
	TriggerManual     = "manual"
	TriggerAPI        = "api"
	TriggerCron       = "cron"
	TriggerBuildEvent = "build_event"
	TriggerDocsGen    = "docs_generate"
	TriggerPipeline   = "pipeline"
)

const (
	SkillPublic  = "public"
	SkillPrivate = "private"
)

const (
	SkillSourceUploaded = "uploaded"
	SkillSourceBuiltin  = "builtin"
)

const (
	EventArtifactReady        = "artifact_ready"
	EventDistributionFinished = "distribution_finished"
	EventNone                 = "none"
)

const (
	WorkspacePending = "pending"
	WorkspaceReady   = "ready"
	WorkspaceFailed  = "failed"
)

// RepoBinding is the API projection of an agent↔repository checkout binding.
type RepoBinding struct {
	RepositoryID uint   `json:"repository_id"`
	Branch       string `json:"branch"`
}

// EnvVarView is the API projection of an agent env var (never includes plaintext value).
type EnvVarView struct {
	Key      string `json:"key"`
	HasValue bool   `json:"has_value"`
}

// AgentRepoBinding persists one repository+branch binding for an agent.
type AgentRepoBinding struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	AgentID      uint      `json:"agent_id" gorm:"not null;uniqueIndex:uidx_agent_repo;index"`
	RepositoryID uint      `json:"repository_id" gorm:"not null;uniqueIndex:uidx_agent_repo;index"`
	Branch       string    `json:"branch" gorm:"size:200;not null;default:main;uniqueIndex:uidx_agent_repo"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AgentRepoBinding) TableName() string { return "ai_agent_repo_bindings" }

// AiAgent is a configured agent bound to skills, model selection, and repository checkouts.
type AiAgent struct {
	ID              uint          `json:"id" gorm:"primaryKey"`
	Name            string        `json:"name" gorm:"size:100;not null"`
	Description     string        `json:"description" gorm:"size:500"`
	Enabled         bool          `json:"enabled" gorm:"not null;default:true"`
	ModelProvider   string        `json:"model_provider,omitempty" gorm:"column:model_provider;size:100"`
	ModelID         string        `json:"model_id,omitempty" gorm:"column:model_id;size:200"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty" gorm:"column:reasoning_effort;size:40;not null;default:''"`
	ApprovalMode    string        `json:"approval_mode" gorm:"column:approval_mode;size:20;not null;default:manual"`
	SystemPrompt        string `json:"system_prompt" gorm:"type:text"`
	InjectDefaultPrompt bool   `json:"inject_default_prompt" gorm:"column:inject_default_prompt;not null;default:true"`
	SkillIDsJSON        string `json:"-" gorm:"type:text"`
	SkillIDs        []uint        `json:"skill_ids" gorm:"-"`
	RepoBindings    []RepoBinding `json:"repo_bindings" gorm:"-"`
	EnvVarsCipher   string        `json:"-" gorm:"type:text"`
	EnvVars         []EnvVarView  `json:"env_vars" gorm:"-"`
	OutputDir       string        `json:"output_dir" gorm:"size:200;not null;default:output"`
	StreamOutput    bool          `json:"-" gorm:"not null;default:false"` // legacy column, unused by the session backend
	TimeoutSec      int           `json:"timeout_sec" gorm:"not null;default:600"`
	WorkspaceStatus string        `json:"workspace_status" gorm:"size:20;not null;default:ready"`
	WorkspaceError  string        `json:"workspace_error" gorm:"type:text"`
	CreatedBy       uint          `json:"created_by" gorm:"index"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

func (AiAgent) TableName() string { return "ai_agents" }

// AgentTrigger configures how an agent may be started.
type AgentTrigger struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	AgentID        uint      `json:"agent_id" gorm:"not null;index"`
	Type           string    `json:"type" gorm:"size:40;not null"`
	Enabled        bool      `json:"enabled" gorm:"not null;default:true"`
	CronExpression string    `json:"cron_expression" gorm:"size:100"`
	CronTimezone   string    `json:"cron_timezone" gorm:"size:100;default:UTC"`
	BuildJobID     *uint     `json:"build_job_id" gorm:"index"`
	BuildEvent     string    `json:"build_event" gorm:"size:40"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (AgentTrigger) TableName() string { return "agent_triggers" }

// AgentRun is an independent async execution (DESIGN §5.3).
type AgentRun struct {
	ID              uint   `json:"id" gorm:"primaryKey"`
	AgentID         uint   `json:"agent_id" gorm:"not null;index"`
	TriggerType     string `json:"trigger_type" gorm:"size:40;not null"`
	TriggerID       *uint  `json:"trigger_id" gorm:"index"`
	Status          string `json:"status" gorm:"size:20;not null;default:queued;index"`
	TriggeredBy     uint   `json:"triggered_by" gorm:"index"`
	BuildRunID      *uint  `json:"build_run_id" gorm:"index"`
	ProjectID       *uint  `json:"project_id" gorm:"index"`
	DocNodeID       *uint  `json:"doc_node_id" gorm:"index"`
	UserPrompt      string `json:"user_prompt,omitempty" gorm:"type:text"`
	SnapshotJSON    string `json:"snapshot_json,omitempty" gorm:"type:text"`
	SkillDigestJSON string `json:"skill_digest_json,omitempty" gorm:"type:text"`
	WorkDir         string `json:"work_dir" gorm:"size:500"`
	ArtifactPath    string `json:"artifact_path,omitempty" gorm:"size:500"`
	ArtifactKind    string `json:"artifact_kind,omitempty" gorm:"size:20"` // archive
	LogPath         string `json:"log_path" gorm:"size:500"`
	OutputText      string `json:"output_text,omitempty" gorm:"type:text"`
	FinalOutput     string `json:"final_output,omitempty" gorm:"column:final_output;type:text"`
	ErrorMessage    string `json:"error_message" gorm:"type:text"`
	// HarnessSessionID binds the run to a harness session (NULL on legacy
	// runs rendered from OutputText). HarnessSessionStatus mirrors the
	// session-side state for list queries.
	HarnessSessionID     *string    `json:"harness_session_id,omitempty" gorm:"column:harness_session_id;size:100;uniqueIndex:uidx_agent_runs_harness_session_id"`
	HarnessSessionStatus string     `json:"harness_session_status,omitempty" gorm:"column:harness_session_status;size:40"`
	DurationMs           int64      `json:"duration_ms"`
	StartedAt            *time.Time `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at"`
	CreatedAt            time.Time  `json:"created_at"`

	Agent *AiAgent `json:"agent,omitempty" gorm:"foreignKey:AgentID"`
}

func (AgentRun) TableName() string { return "agent_runs" }

// SkillPackage metadata + content-addressed StorageObject reference.
type SkillPackage struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name" gorm:"size:200;not null"`
	Description     string    `json:"description" gorm:"size:1000"`
	Visibility      string    `json:"visibility" gorm:"size:20;not null;default:private;index"`
	Source          string    `json:"source" gorm:"size:20;not null;default:uploaded;index"` // uploaded | builtin
	StorageObjectID uint      `json:"storage_object_id" gorm:"not null;index"`
	PackageDigest   string    `json:"package_digest" gorm:"size:64;not null;index"`
	SizeBytes       int64     `json:"size_bytes" gorm:"not null"`
	CreatedBy       uint      `json:"created_by" gorm:"index"`
	UpdatedBy       uint      `json:"updated_by" gorm:"index"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Editable is API-only: true when source=uploaded and caller may mutate files.
	Editable bool `json:"editable" gorm:"-"`
}

func (SkillPackage) TableName() string { return "skill_packages" }
