package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
)

// ScriptRunService provides enqueue/cancel/retry for script runs.
type ScriptRunService struct {
	runs      *repository.ScriptRunRepository
	jobs      *repository.ScriptJobRepository
	scheduler engine.RunScheduler
	termHook  engine.ScriptRunTerminalHook
}

func NewScriptRunService(runs *repository.ScriptRunRepository, jobs *repository.ScriptJobRepository) *ScriptRunService {
	return &ScriptRunService{runs: runs, jobs: jobs}
}

func (s *ScriptRunService) SetScheduler(sched engine.RunScheduler) {
	s.scheduler = sched
}

// SetTerminalHook wires PipelineOrchestrator for queued cancels that bypass ScriptPipeline.Execute.
func (s *ScriptRunService) SetTerminalHook(h engine.ScriptRunTerminalHook) {
	s.termHook = h
}

func (s *ScriptRunService) List(q pkg.ListQuery, scriptJobID *uint, status string, projectID *uint, userID uint, dataScope string) ([]model.ScriptRun, int64, error) {
	var jobCreatedBy *uint
	// D3: 带 project_id 时跳过 created_by/is_public 数据范围过滤
	if projectID == nil && dataScope != rbacmodel.DataScopeAll {
		jobCreatedBy = &userID
	}
	return s.runs.List(q, scriptJobID, status, jobCreatedBy, projectID)
}

func (s *ScriptRunService) Get(id uint, userID uint, dataScope string) (*model.ScriptRun, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本执行不存在")
	}
	if err := s.requireRunAccess(run, userID, dataScope); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *ScriptRunService) Enqueue(jobID, triggeredBy uint, dataScope string, triggerType string) (*model.ScriptRun, error) {
	job, err := s.jobs.FindByID(jobID)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if err := requireScriptJobWrite(job, triggeredBy, dataScope); err != nil {
		return nil, err
	}
	if triggerType == "" {
		triggerType = "manual"
	}
	return s.EnqueueInternal(jobID, triggeredBy, triggerType)
}

// EnqueueInternal implements engine.ScriptRunEnqueuer.
func (s *ScriptRunService) EnqueueInternal(jobID, triggeredBy uint, triggerType string) (*model.ScriptRun, error) {
	return s.EnqueueWithOverrides(jobID, triggeredBy, triggerType, nil)
}

// EnqueueWithOverrides enqueues a ScriptRun with optional run-level env
// overrides (pipeline node config); overrides win over job env at execution.
func (s *ScriptRunService) EnqueueWithOverrides(jobID, triggeredBy uint, triggerType string, envOverrides map[string]string) (*model.ScriptRun, error) {
	job, err := s.jobs.FindByID(jobID)
	if err != nil {
		return nil, NewNotFound("脚本任务不存在")
	}
	if !job.Enabled {
		return nil, errorsNew("脚本任务已禁用")
	}
	decodeScriptEnvNames(job)
	if triggerType == "" {
		triggerType = "manual"
	}
	if triggerType == "manual" && !job.TriggerManual {
		return nil, errorsNew("该任务未启用手动触发")
	}
	num, err := s.runs.NextRunNumber(jobID)
	if err != nil {
		return nil, err
	}
	scriptHash := sha256.Sum256([]byte(job.Script))
	snapshot := map[string]interface{}{
		"trigger_type":  triggerType,
		"script_sha256": hex.EncodeToString(scriptHash[:]),
		"script_type":   job.ScriptType,
		"env_var_names": job.EnvVarNames,
		"env_var_keys":  scriptJobEnvVarKeys(job),
		"triggered_by":  triggeredBy,
		"enqueued_at":   time.Now().UTC().Format(time.RFC3339),
	}
	var overridesCipher string
	if len(envOverrides) > 0 {
		cipher, err := encryptJobEnvVars(envOverrides)
		if err != nil {
			return nil, err
		}
		overridesCipher = cipher
		snapshot["env_override_keys"] = sortedKeys(envOverrides)
	}
	snapBytes, _ := json.Marshal(snapshot)
	run := &model.ScriptRun{
		ScriptJobID:        jobID,
		RunNumber:          num,
		Status:             "queued",
		Stage:              "pending",
		TriggerType:        triggerType,
		TriggeredBy:        triggeredBy,
		SnapshotJSON:       string(snapBytes),
		EnvOverridesCipher: overridesCipher,
	}
	if err := s.runs.Create(run); err != nil {
		return nil, err
	}
	if s.scheduler != nil {
		_ = s.scheduler.Submit(run.ID)
	}
	return run, nil
}

func (s *ScriptRunService) Cancel(id uint, userID uint, dataScope string) (*model.ScriptRun, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本执行不存在")
	}
	if err := s.requireRunWrite(run, userID, dataScope); err != nil {
		return nil, err
	}
	switch run.Status {
	case "queued":
		now := time.Now()
		_ = s.runs.UpdateFields(id, map[string]interface{}{
			"status":      "cancelled",
			"stage":       "idle",
			"finished_at": now,
		})
		if s.termHook != nil {
			run.Status = "cancelled"
			s.termHook.OnScriptRunTerminal(run, "cancelled")
		}
	case "running":
		if s.scheduler != nil {
			s.scheduler.Cancel(id)
		}
	default:
		return nil, NewConflict("当前状态不可取消: " + run.Status)
	}
	return s.runs.FindByID(id)
}

// CancelInternal cancels a script run without RBAC or terminal-hook callbacks.
// Used by PipelineOrchestrator while holding the per-pipeline lock.
func (s *ScriptRunService) CancelInternal(id uint) {
	if s == nil || id == 0 {
		return
	}
	run, err := s.runs.FindByID(id)
	if err != nil {
		return
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
	}
}

func (s *ScriptRunService) Retry(id, triggeredBy uint, dataScope string) (*model.ScriptRun, error) {
	prev, err := s.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("脚本执行不存在")
	}
	if err := s.requireRunWrite(prev, triggeredBy, dataScope); err != nil {
		return nil, err
	}
	return s.EnqueueInternal(prev.ScriptJobID, triggeredBy, "retry")
}

func (s *ScriptRunService) LogPath(id uint, userID uint, dataScope string) (string, error) {
	run, err := s.runs.FindByID(id)
	if err != nil {
		return "", NewNotFound("脚本执行不存在")
	}
	if err := s.requireRunAccess(run, userID, dataScope); err != nil {
		return "", err
	}
	if strings.TrimSpace(run.LogPath) == "" {
		return "", NewNotFound("日志不存在")
	}
	return run.LogPath, nil
}

func (s *ScriptRunService) requireRunAccess(run *model.ScriptRun, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll {
		return nil
	}
	job, err := s.jobs.FindByID(run.ScriptJobID)
	if err != nil {
		return NewNotFound("脚本任务不存在")
	}
	return requireScriptJobRead(job, userID, dataScope)
}

func (s *ScriptRunService) requireRunWrite(run *model.ScriptRun, userID uint, dataScope string) error {
	if dataScope == rbacmodel.DataScopeAll {
		return nil
	}
	job, err := s.jobs.FindByID(run.ScriptJobID)
	if err != nil {
		return NewNotFound("脚本任务不存在")
	}
	return requireScriptJobWrite(job, userID, dataScope)
}

var _ engine.ScriptRunEnqueuer = (*ScriptRunService)(nil)
