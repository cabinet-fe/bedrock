package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
)

// BuildRunService provides enqueue/cancel/retry/redeploy and artifact paths.
type BuildRunService struct {
	runs      *repository.BuildRunRepository
	jobs      *repository.BuildJobRepository
	scheduler engine.RunScheduler
}

func NewBuildRunService(runs *repository.BuildRunRepository, jobs *repository.BuildJobRepository) *BuildRunService {
	return &BuildRunService{runs: runs, jobs: jobs}
}

func (s *BuildRunService) SetScheduler(sched engine.RunScheduler) {
	s.scheduler = sched
}

type EnqueueRunInput struct {
	Branch        string `json:"branch"`
	TriggerType   string `json:"trigger_type"`
	CommitHash    string `json:"commit_hash"`
	CommitMessage string `json:"commit_message"`
}

type RedeployInput struct {
	TargetIDs []uint `json:"target_ids"`
}

func (s *BuildRunService) List(q pkg.ListQuery, buildJobID *uint, status string) ([]model.BuildRun, int64, error) {
	return s.runs.List(q, buildJobID, status)
}

func (s *BuildRunService) Get(id uint) (*model.BuildRun, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建执行不存在")
	}
	return run, nil
}

func (s *BuildRunService) Enqueue(jobID, triggeredBy uint, in EnqueueRunInput) (*model.BuildRun, error) {
	return s.EnqueueInternal(jobID, triggeredBy, engine.EnqueueParams{
		Branch:        in.Branch,
		TriggerType:   in.TriggerType,
		CommitHash:    in.CommitHash,
		CommitMessage: in.CommitMessage,
	})
}

// EnqueueInternal implements engine.RunEnqueuer.
func (s *BuildRunService) EnqueueInternal(jobID, triggeredBy uint, in engine.EnqueueParams) (*model.BuildRun, error) {
	job, err := s.jobs.FindByID(jobID)
	if err != nil {
		return nil, NewNotFound("构建任务不存在")
	}
	if !job.Enabled {
		return nil, errorsNew("构建任务已禁用")
	}
	decodeEnvNames(job)
	branch := in.Branch
	if branch == "" {
		branch = job.Branch
	}
	trigger := in.TriggerType
	if trigger == "" {
		trigger = "manual"
	}
	num, err := s.runs.NextBuildNumber(jobID)
	if err != nil {
		return nil, err
	}
	targets, _ := s.jobs.ListDeployTargets(jobID)
	scriptHash := sha256.Sum256([]byte(job.BuildScript))
	snapshot := map[string]interface{}{
		"trigger_type":    trigger,
		"branch":          branch,
		"commit_hash":     in.CommitHash,
		"script_sha256":   hex.EncodeToString(scriptHash[:]),
		"env_var_names":   job.EnvVarNames,
		"artifact_format": job.ArtifactFormat,
		"deploy_targets":  targets,
		"triggered_by":    triggeredBy,
		"enqueued_at":     time.Now().UTC().Format(time.RFC3339),
	}
	snapBytes, _ := json.Marshal(snapshot)
	run := &model.BuildRun{
		BuildJobID:          jobID,
		BuildNumber:         num,
		Status:              "queued",
		Stage:               "pending",
		TriggerType:         trigger,
		TriggeredBy:         triggeredBy,
		Branch:              branch,
		CommitHash:          in.CommitHash,
		CommitMessage:       in.CommitMessage,
		DistributionSummary: "none",
		SnapshotJSON:        string(snapBytes),
	}
	if err := s.runs.Create(run); err != nil {
		return nil, err
	}
	if s.scheduler != nil {
		_ = s.scheduler.Submit(run.ID)
	}
	return run, nil
}

func (s *BuildRunService) Cancel(id uint) (*model.BuildRun, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建执行不存在")
	}
	switch run.Status {
	case "queued":
		now := time.Now()
		_ = s.runs.UpdateFields(id, map[string]interface{}{
			"status":      "cancelled",
			"stage":       "idle",
			"finished_at": now,
		})
	case "running":
		if s.scheduler != nil {
			s.scheduler.Cancel(id)
		}
		// Pipeline cancelRun persists terminal state; also mark eagerly if still queued race.
	case "success":
		// Cancel in-flight distribution only.
		if s.scheduler != nil {
			s.scheduler.Cancel(id)
		}
		if run.DistributionSummary == "running" {
			_ = s.runs.UpdateFields(id, map[string]interface{}{
				"distribution_summary": "cancelled",
				"stage":                "idle",
			})
		}
	default:
		return nil, NewConflict("当前状态不可取消: " + run.Status)
	}
	return s.runs.FindByID(id)
}

func (s *BuildRunService) Retry(id, triggeredBy uint) (*model.BuildRun, error) {
	prev, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建执行不存在")
	}
	return s.EnqueueInternal(prev.BuildJobID, triggeredBy, engine.EnqueueParams{
		Branch:        prev.Branch,
		TriggerType:   "retry",
		CommitHash:    "",
		CommitMessage: "",
	})
}

func (s *BuildRunService) Redeploy(id uint, in RedeployInput) (*model.BuildRun, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("构建执行不存在")
	}
	if run.Status != "success" {
		return nil, NewConflict("仅成功的构建可重新分发")
	}
	if strings.TrimSpace(run.ArtifactPath) == "" {
		return nil, NewConflict("无制品可分发")
	}
	// Merge redeploy filter into snapshot (append attempts on same run).
	var snap map[string]interface{}
	if run.SnapshotJSON != "" {
		_ = json.Unmarshal([]byte(run.SnapshotJSON), &snap)
	}
	if snap == nil {
		snap = map[string]interface{}{}
	}
	if len(in.TargetIDs) > 0 {
		snap["redeploy_target_ids"] = in.TargetIDs
	} else {
		delete(snap, "redeploy_target_ids")
	}
	snapBytes, _ := json.Marshal(snap)
	_ = s.runs.UpdateFields(id, map[string]interface{}{
		"trigger_type":         "redeploy",
		"snapshot_json":        string(snapBytes),
		"distribution_summary": "running",
		"stage":                "distributing",
		"status":               "success",
	})
	if s.scheduler != nil {
		_ = s.scheduler.Submit(id)
	}
	return s.runs.FindByID(id)
}

// ArtifactPath returns absolute path for download; empty if unavailable.
func (s *BuildRunService) ArtifactPath(id uint) (path string, filename string, err error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return "", "", NewNotFound("构建执行不存在")
	}
	if run.Status != "success" && run.ArtifactPath == "" {
		return "", "", NewConflict("制品不可用")
	}
	path = strings.TrimSpace(run.ArtifactPath)
	if path == "" {
		return "", "", NewNotFound("制品不存在")
	}
	if _, err := os.Stat(path); err != nil {
		return "", "", NewNotFound("制品文件不存在")
	}
	return path, filepath.Base(path), nil
}

// LogPath returns the build log file path if present.
func (s *BuildRunService) LogPath(id uint) (string, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return "", NewNotFound("构建执行不存在")
	}
	if strings.TrimSpace(run.LogPath) == "" {
		return "", NewNotFound("日志不存在")
	}
	return run.LogPath, nil
}

// Ensure Compile-time interface satisfaction.
var _ engine.RunEnqueuer = (*BuildRunService)(nil)
