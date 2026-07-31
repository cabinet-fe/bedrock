package engine

import (
	"bedrock/internal/cicd/model"
	resourcemodel "bedrock/internal/resource/model"
)

// RunStore is the BuildRun persistence surface used by Pipeline/Scheduler.
type RunStore interface {
	FindByID(id uint) (*model.BuildRun, error)
	UpdateFields(id uint, fields map[string]interface{}) error
	CreateAttempt(a *model.BuildDeployAttempt) error
	UpdateAttempt(a *model.BuildDeployAttempt) error
	NextBatchNo(runID uint) (int, error)
	ListByStatuses(statuses ...string) ([]model.BuildRun, error)
	MarkRunningInterrupted() (int64, error)
	HasNonTerminal(jobID uint) (bool, error)
	ListArtifactsByJob(jobID uint) ([]model.BuildRun, error)
}

// JobStore loads BuildJob + DeployTargets.
type JobStore interface {
	FindByID(id uint) (*model.BuildJob, error)
	ListDeployTargets(jobID uint) ([]model.DeployTarget, error)
	ListCronEnabled() ([]model.BuildJob, error)
	ListByRepositoryID(repositoryID uint) ([]model.BuildJob, error)
}

// RepoStore loads Repository.
type RepoStore interface {
	FindByID(id uint) (*resourcemodel.Repository, error)
}

// ServerStore loads Server.
type ServerStore interface {
	FindByID(id uint) (*resourcemodel.Server, error)
}

// SecretResolver decrypts credentials for git/SSH/agent (never exposed via API).
type SecretResolver interface {
	Resolve(id uint) (typ, username, secret, passphrase string, err error)
}

// RunEnqueuer creates queued BuildRuns (used by Cron/Webhook).
type RunEnqueuer interface {
	EnqueueInternal(jobID, triggeredBy uint, in EnqueueParams) (*model.BuildRun, error)
}

// EnqueueParams is the engine-facing enqueue payload.
type EnqueueParams struct {
	Branch        string
	TriggerType   string
	CommitHash    string
	CommitMessage string
	// EnvOverrides are run-level Key-Value env overrides (e.g. pipeline node
	// config); they win over the job's own env_vars at execution time.
	EnvOverrides map[string]string
}

// RunScheduler submits/cancels runs in the in-memory worker pool.
type RunScheduler interface {
	Submit(runID uint) error
	Cancel(runID uint) bool
}
