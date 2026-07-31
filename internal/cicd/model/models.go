package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// UintList is a []uint persisted as a JSON array in a text column, so it is
// transparently encoded/decoded at every layer (repository included).
type UintList []uint

// Value implements driver.Valuer. Empty lists are stored as NULL.
func (l UintList) Value() (driver.Value, error) {
	if len(l) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (l *UintList) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*l = UintList{}
		return nil
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("UintList: unsupported scan type %T", src)
	}
	if len(b) == 0 {
		*l = UintList{}
		return nil
	}
	var ids []uint
	if err := json.Unmarshal(b, &ids); err != nil {
		return fmt.Errorf("UintList: %w", err)
	}
	*l = ids
	return nil
}

// EnvVarView is the API projection of a build-job Key-Value env var (never includes plaintext).
type EnvVarView struct {
	Key      string `json:"key"`
	HasValue bool   `json:"has_value"`
}

// BuildJob belongs to a Repository (1:N).
type BuildJob struct {
	ID                 uint         `json:"id" gorm:"primaryKey"`
	RepositoryID       uint         `json:"repository_id" gorm:"index;not null"`
	Name               string       `json:"name" gorm:"size:100;not null"`
	Description        string       `json:"description" gorm:"size:500"`
	Enabled            bool         `json:"enabled" gorm:"not null"`                          // no gorm default: must persist false
	Branch             string       `json:"branch" gorm:"size:200;default:main"`
	ShallowClone       bool         `json:"shallow_clone" gorm:"not null"`                     // no gorm default: must persist false
	BuildScriptType    string       `json:"build_script_type" gorm:"size:20;default:bash"`
	BuildScript        string       `json:"build_script" gorm:"type:text"`
	PostBuildScript    string       `json:"post_build_script" gorm:"type:text"`
	WorkDir            string       `json:"work_dir" gorm:"size:300"`
	OutputDir          string       `json:"output_dir" gorm:"size:300"` // deprecated compat: first of artifact_paths
	ArtifactPathsJSON  string       `json:"-" gorm:"column:artifact_paths_json;type:text"`
	ArtifactPaths      []string     `json:"artifact_paths" gorm:"-"`
	CachePaths         string       `json:"cache_paths" gorm:"type:text"`
	EnvVarNamesJSON    string       `json:"-" gorm:"type:text"`
	EnvVarNames        []string     `json:"env_var_names" gorm:"-"`
	EnvVarsCipher      string       `json:"-" gorm:"type:text"`
	EnvVars            []EnvVarView `json:"env_vars" gorm:"-"`
	TriggerManual      bool         `json:"trigger_manual" gorm:"not null"` // no gorm default: must persist false
	TriggerWebhook     bool         `json:"trigger_webhook" gorm:"not null;default:false"`
	TriggerCron        bool         `json:"trigger_cron" gorm:"not null;default:false"`
	WebhookSecret      string       `json:"webhook_secret,omitempty" gorm:"size:64"`
	WebhookType        string       `json:"webhook_type" gorm:"size:20;default:auto"`
	WebhookRefPath     string       `json:"webhook_ref_path" gorm:"size:300"`
	WebhookCommitPath  string       `json:"webhook_commit_path" gorm:"size:300"`
	WebhookMessagePath string       `json:"webhook_message_path" gorm:"size:300"`
	CronExpression     string       `json:"cron_expression" gorm:"size:100"`
	CronTimezone       string       `json:"cron_timezone" gorm:"size:100;default:UTC"`
	MaxArtifacts       int          `json:"max_artifacts" gorm:"default:5"`
	ArtifactFormat     string       `json:"artifact_format" gorm:"size:20;default:gzip"`
	AgentTriggerEvent  string       `json:"agent_trigger_event" gorm:"size:40;default:artifact_ready"`
	AgentIDs           UintList     `json:"agent_ids" gorm:"type:text"`
	IsPublic           bool         `json:"is_public" gorm:"not null;default:false;index"`
	CreatedBy          uint         `json:"created_by" gorm:"index"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`

	// WorkspacePath is absolute checkout dir {workspace}/jobs/job-{id}/ (API-only).
	WorkspacePath string `json:"workspace_path,omitempty" gorm:"-"`

	DeployTargets []DeployTarget `json:"deploy_targets,omitempty" gorm:"foreignKey:BuildJobID"`
}

func (BuildJob) TableName() string { return "build_jobs" }

// DeployTarget is private to a BuildJob (1:N); not shared across jobs.
type DeployTarget struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	BuildJobID       uint      `json:"build_job_id" gorm:"index;not null"`
	ServerID         *uint     `json:"server_id" gorm:"index"`
	RemotePath       string    `json:"remote_path" gorm:"size:500"`
	Method           string    `json:"method" gorm:"size:20;not null;default:rsync"`
	PostDeployScript string    `json:"post_deploy_script" gorm:"type:text"`
	SortOrder        int       `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (DeployTarget) TableName() string { return "deploy_targets" }

// BuildRun status (result) vs stage (activity) — see DESIGN §5.2.
// status: queued|running|success|failed|cancelled|interrupted
// stage: pending|cloning|building|archiving|distributing|idle
// distribution_summary: none|running|all_success|partial|all_failed|cancelled
type BuildRun struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	BuildJobID          uint       `json:"build_job_id" gorm:"uniqueIndex:idx_job_build_num;not null"`
	BuildNumber         int        `json:"build_number" gorm:"uniqueIndex:idx_job_build_num;not null"`
	Status              string     `json:"status" gorm:"size:20;not null;default:queued"`
	Stage               string     `json:"stage" gorm:"size:20;not null;default:pending"`
	TriggerType         string     `json:"trigger_type" gorm:"size:20"`
	TriggeredBy         uint       `json:"triggered_by"`
	Branch              string     `json:"branch" gorm:"size:200"`
	CommitHash          string     `json:"commit_hash" gorm:"size:64"`
	CommitMessage       string     `json:"commit_message" gorm:"size:500"`
	LogPath             string     `json:"log_path" gorm:"size:500"`
	ArtifactPath        string     `json:"artifact_path" gorm:"size:500"`
	ArtifactKind        string     `json:"artifact_kind,omitempty" gorm:"size:20"` // file|archive|bundle
	DurationMs          int64      `json:"duration_ms"`
	ErrorMessage        string     `json:"error_message" gorm:"type:text"`
	DistributionSummary string     `json:"distribution_summary" gorm:"size:30;default:none"`
	SnapshotJSON        string     `json:"snapshot_json,omitempty" gorm:"type:text"`
	StartedAt           *time.Time `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
	CreatedAt           time.Time  `json:"created_at"`

	DeployAttempts []BuildDeployAttempt `json:"deploy_attempts,omitempty" gorm:"foreignKey:BuildRunID"`
}

func (BuildRun) TableName() string { return "build_runs" }

// BuildDeployAttempt is one target row in a distribute/redeploy batch (append-only in Wave 4).
type BuildDeployAttempt struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	BuildRunID         uint       `json:"build_run_id" gorm:"index;not null"`
	BatchNo            int        `json:"batch_no" gorm:"not null;default:1"`
	DeployTargetID     *uint      `json:"deploy_target_id" gorm:"index"`
	TargetSnapshotJSON string     `json:"target_snapshot_json,omitempty" gorm:"type:text"`
	Status             string     `json:"status" gorm:"size:20;not null;default:pending"`
	LogPath            string     `json:"log_path" gorm:"size:500"`
	ErrorMessage       string     `json:"error_message" gorm:"type:text"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (BuildDeployAttempt) TableName() string { return "build_deploy_attempts" }

// ScriptJob is a no-repo, no-artifact, no-deploy script task.
type ScriptJob struct {
	ID              uint         `json:"id" gorm:"primaryKey"`
	Name            string       `json:"name" gorm:"size:100;not null"`
	Description     string       `json:"description" gorm:"size:500"`
	Enabled         bool         `json:"enabled" gorm:"not null"`
	ScriptType      string       `json:"script_type" gorm:"size:20;default:bash"`
	Script          string       `json:"script" gorm:"type:text"`
	WorkDir         string       `json:"work_dir" gorm:"size:300"` // relative to script workspace
	EnvVarNamesJSON string       `json:"-" gorm:"type:text"`
	EnvVarNames     []string     `json:"env_var_names" gorm:"-"`
	EnvVarsCipher   string       `json:"-" gorm:"type:text"`
	EnvVars         []EnvVarView `json:"env_vars" gorm:"-"`
	TriggerManual   bool         `json:"trigger_manual" gorm:"not null"`
	TriggerWebhook  bool         `json:"trigger_webhook" gorm:"not null;default:false"`
	TriggerCron     bool         `json:"trigger_cron" gorm:"not null;default:false"`
	WebhookSecret   string       `json:"webhook_secret,omitempty" gorm:"size:64"`
	WebhookType     string       `json:"webhook_type" gorm:"size:20;default:generic"`
	CronExpression  string       `json:"cron_expression" gorm:"size:100"`
	CronTimezone    string       `json:"cron_timezone" gorm:"size:100;default:UTC"`
	IsPublic        bool         `json:"is_public" gorm:"not null;default:false;index"`
	CreatedBy       uint         `json:"created_by" gorm:"index"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`

	// WorkspacePath is absolute dir {workspace}/scripts/script-{id}/ (API-only).
	WorkspacePath string `json:"workspace_path,omitempty" gorm:"-"`
}

func (ScriptJob) TableName() string { return "script_jobs" }

// ScriptRun is one execution of a ScriptJob.
// status: queued|running|success|failed|cancelled|interrupted
// stage: pending|running|idle
type ScriptRun struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	ScriptJobID  uint       `json:"script_job_id" gorm:"uniqueIndex:idx_script_job_run_num;not null"`
	RunNumber    int        `json:"run_number" gorm:"uniqueIndex:idx_script_job_run_num;not null"`
	Status       string     `json:"status" gorm:"size:20;not null;default:queued"`
	Stage        string     `json:"stage" gorm:"size:20;not null;default:pending"`
	TriggerType  string     `json:"trigger_type" gorm:"size:20"`
	TriggeredBy  uint       `json:"triggered_by"`
	LogPath      string     `json:"log_path" gorm:"size:500"`
	DurationMs   int64      `json:"duration_ms"`
	ErrorMessage string     `json:"error_message" gorm:"type:text"`
	SnapshotJSON string     `json:"snapshot_json,omitempty" gorm:"type:text"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (ScriptRun) TableName() string { return "script_runs" }

// BuildPipeline is a DAG of BuildJobs (graph_json = VueFlow nodes/edges).
// Edges mean: upstream success → unlock downstream. No cross-job artifact passing in v1.
type BuildPipeline struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	Name               string    `json:"name" gorm:"size:100;not null"`
	Description        string    `json:"description" gorm:"size:500"`
	Enabled            bool      `json:"enabled" gorm:"not null"`
	GraphJSON          string    `json:"graph_json" gorm:"type:text"`
	TriggerManual      bool      `json:"trigger_manual" gorm:"not null"`
	TriggerWebhook     bool      `json:"trigger_webhook" gorm:"not null;default:false"`
	TriggerCron        bool      `json:"trigger_cron" gorm:"not null;default:false"`
	WebhookSecret      string    `json:"webhook_secret,omitempty" gorm:"size:64"`
	WebhookType        string    `json:"webhook_type" gorm:"size:20;default:generic"`
	WebhookRefPath     string    `json:"webhook_ref_path" gorm:"size:300"`
	WebhookCommitPath  string    `json:"webhook_commit_path" gorm:"size:300"`
	WebhookMessagePath string    `json:"webhook_message_path" gorm:"size:300"`
	CronExpression     string    `json:"cron_expression" gorm:"size:100"`
	CronTimezone       string    `json:"cron_timezone" gorm:"size:100;default:UTC"`
	IsPublic           bool      `json:"is_public" gorm:"not null;default:false;index"`
	CreatedBy          uint      `json:"created_by" gorm:"index"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (BuildPipeline) TableName() string { return "build_pipelines" }

// PipelineRun is one execution of a BuildPipeline.
// status: queued|running|success|failed|cancelled
type PipelineRun struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	BuildPipelineID uint       `json:"build_pipeline_id" gorm:"uniqueIndex:idx_pipeline_run_num;not null"`
	RunNumber       int        `json:"run_number" gorm:"uniqueIndex:idx_pipeline_run_num;not null"`
	Status          string     `json:"status" gorm:"size:20;not null;default:queued"`
	TriggerType     string     `json:"trigger_type" gorm:"size:20"`
	TriggeredBy     uint       `json:"triggered_by"`
	SnapshotJSON    string     `json:"snapshot_json,omitempty" gorm:"type:text"`
	ErrorMessage    string     `json:"error_message" gorm:"type:text"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	CreatedAt       time.Time  `json:"created_at"`

	Stages []PipelineStageRun `json:"stages,omitempty" gorm:"foreignKey:PipelineRunID"`
}

func (PipelineRun) TableName() string { return "pipeline_runs" }

// PipelineStageRun tracks one graph node within a PipelineRun.
// status: pending|queued|running|success|failed|cancelled|skipped|interrupted
type PipelineStageRun struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	PipelineRunID uint       `json:"pipeline_run_id" gorm:"index;not null"`
	NodeID        string     `json:"node_id" gorm:"size:100;not null"`
	BuildJobID    uint       `json:"build_job_id" gorm:"index;not null"`
	BuildRunID    *uint      `json:"build_run_id" gorm:"index"`
	Status        string     `json:"status" gorm:"size:20;not null;default:pending"`
	ErrorMessage  string     `json:"error_message" gorm:"type:text"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (PipelineStageRun) TableName() string { return "pipeline_stage_runs" }
