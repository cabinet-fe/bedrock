package service

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
)

// PipelineOrchestrator starts PipelineRuns and advances stages on BuildRun terminal.
type PipelineOrchestrator struct {
	pipelines *repository.BuildPipelineRepository
	runs      *repository.PipelineRunRepository
	jobs      *repository.BuildJobRepository
	buildRuns *BuildRunService
	logger    *zap.Logger

	mu    sync.Mutex
	locks map[uint]*sync.Mutex
}

func NewPipelineOrchestrator(
	pipelines *repository.BuildPipelineRepository,
	runs *repository.PipelineRunRepository,
	jobs *repository.BuildJobRepository,
	buildRuns *BuildRunService,
	logger *zap.Logger,
) *PipelineOrchestrator {
	return &PipelineOrchestrator{
		pipelines: pipelines,
		runs:      runs,
		jobs:      jobs,
		buildRuns: buildRuns,
		logger:    logger,
		locks:     make(map[uint]*sync.Mutex),
	}
}

func (o *PipelineOrchestrator) runLock(pipelineRunID uint) *sync.Mutex {
	o.mu.Lock()
	defer o.mu.Unlock()
	if l, ok := o.locks[pipelineRunID]; ok {
		return l
	}
	l := &sync.Mutex{}
	o.locks[pipelineRunID] = l
	return l
}

type EnqueuePipelineInput struct {
	TriggerType string `json:"trigger_type"`
}

func (o *PipelineOrchestrator) List(q pkg.ListQuery, pipelineID *uint, status string, userID uint, dataScope string) ([]model.PipelineRun, int64, error) {
	var createdBy *uint
	if dataScope != rbacmodel.DataScopeAll {
		createdBy = &userID
	}
	return o.runs.List(q, pipelineID, status, createdBy)
}

func (o *PipelineOrchestrator) Get(id, userID uint, dataScope string) (*model.PipelineRun, error) {
	run, err := o.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线运行不存在")
	}
	p, err := o.pipelines.FindByID(run.BuildPipelineID)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineRead(p, userID, dataScope); err != nil {
		return nil, err
	}
	return run, nil
}

func (o *PipelineOrchestrator) Enqueue(pipelineID, triggeredBy uint, dataScope string, in EnqueuePipelineInput) (*model.PipelineRun, error) {
	p, err := o.pipelines.FindByID(pipelineID)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, triggeredBy, dataScope); err != nil {
		return nil, err
	}
	trigger := in.TriggerType
	if trigger == "" {
		trigger = "manual"
	}
	return o.EnqueueInternal(pipelineID, triggeredBy, trigger)
}

// EnqueueInternal creates a PipelineRun and starts root stages (cron/webhook/manual).
func (o *PipelineOrchestrator) EnqueueInternal(pipelineID, triggeredBy uint, triggerType string) (*model.PipelineRun, error) {
	p, err := o.pipelines.FindByID(pipelineID)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if !p.Enabled {
		return nil, errorsNew("流水线已禁用")
	}
	if triggerType == "" {
		triggerType = "manual"
	}
	if triggerType == "manual" && !p.TriggerManual {
		return nil, errorsNew("该流水线未启用手动触发")
	}

	g, err := ParsePipelineGraph(p.GraphJSON)
	if err != nil {
		return nil, errorsNew(err.Error())
	}
	if err := ValidatePipelineDAG(g, func(id uint) bool {
		_, e := o.jobs.FindByID(id)
		return e == nil
	}); err != nil {
		return nil, err
	}

	num, err := o.runs.NextRunNumber(pipelineID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	run := &model.PipelineRun{
		BuildPipelineID: pipelineID,
		RunNumber:       num,
		Status:          "running",
		TriggerType:     triggerType,
		TriggeredBy:     triggeredBy,
		SnapshotJSON:    p.GraphJSON,
		StartedAt:       &now,
	}
	if err := o.runs.Create(run); err != nil {
		return nil, err
	}

	stages := make([]model.PipelineStageRun, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		stages = append(stages, model.PipelineStageRun{
			PipelineRunID: run.ID,
			NodeID:        n.ID,
			BuildJobID:    n.Data.BuildJobID,
			Status:        "pending",
		})
	}
	if err := o.runs.CreateStages(stages); err != nil {
		return nil, err
	}

	if err := o.startRootStages(run.ID); err != nil {
		_ = o.failPipeline(run.ID, err.Error())
		return o.runs.FindByID(run.ID)
	}
	return o.runs.FindByID(run.ID)
}

// Cancel stops a non-terminal PipelineRun: cancels in-flight BuildRuns, skips never-started stages.
func (o *PipelineOrchestrator) Cancel(id, userID uint, dataScope string) (*model.PipelineRun, error) {
	run, err := o.runs.FindByID(id)
	if err != nil {
		return nil, NewNotFound("流水线运行不存在")
	}
	p, err := o.pipelines.FindByID(run.BuildPipelineID)
	if err != nil {
		return nil, NewNotFound("流水线不存在")
	}
	if err := requirePipelineWrite(p, userID, dataScope); err != nil {
		return nil, err
	}
	switch run.Status {
	case "queued", "running":
	default:
		return nil, NewConflict("当前状态不可取消: " + run.Status)
	}
	if err := o.cancelPipeline(id, "cancelled by user"); err != nil {
		return nil, err
	}
	return o.runs.FindByID(id)
}

// OnBuildRunTerminal implements engine.BuildRunTerminalHook.
func (o *PipelineOrchestrator) OnBuildRunTerminal(run *model.BuildRun, status string) {
	if o == nil || run == nil || run.ID == 0 {
		return
	}
	if run.TriggerType != "pipeline" {
		return
	}
	stage, err := o.runs.FindStageByBuildRunID(run.ID)
	if err != nil {
		return
	}
	lock := o.runLock(stage.PipelineRunID)
	lock.Lock()
	defer lock.Unlock()

	switch status {
	case "success", "failed", "cancelled", "interrupted":
		// ok
	default:
		return
	}

	now := time.Now()
	// Always sync the stage row (including late terminals after pipeline finalize).
	_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
		"status":        status,
		"error_message": run.ErrorMessage,
		"finished_at":   now,
	})

	pr, err := o.runs.FindByID(stage.PipelineRunID)
	if err != nil {
		return
	}
	if pr.Status != "running" && pr.Status != "queued" {
		return
	}

	if status != "success" {
		_ = o.failPipelineLocked(pr.ID, fmt.Sprintf("stage %s build_run=%d status=%s", stage.NodeID, run.ID, status))
		return
	}

	if err := o.unlockDownstream(pr.ID, stage.NodeID); err != nil {
		_ = o.failPipelineLocked(pr.ID, err.Error())
		return
	}
	o.maybeComplete(pr.ID)
}

func (o *PipelineOrchestrator) startRootStages(pipelineRunID uint) error {
	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil {
		return err
	}
	g, err := ParsePipelineGraph(pr.SnapshotJSON)
	if err != nil {
		return err
	}
	roots := RootNodeIDs(g)
	byNode := stageByNode(pr.Stages)
	for _, nodeID := range roots {
		st, ok := byNode[nodeID]
		if !ok {
			continue
		}
		if err := o.enqueueStage(pr, &st); err != nil {
			return err
		}
	}
	return nil
}

func (o *PipelineOrchestrator) unlockDownstream(pipelineRunID uint, completedNodeID string) error {
	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil {
		return err
	}
	g, err := ParsePipelineGraph(pr.SnapshotJSON)
	if err != nil {
		return err
	}
	succ, pred := GraphAdjacency(g)
	byNode := stageByNode(pr.Stages)
	statusByNode := make(map[string]string, len(pr.Stages))
	for _, st := range pr.Stages {
		statusByNode[st.NodeID] = st.Status
	}
	statusByNode[completedNodeID] = "success"

	for _, nextID := range succ[completedNodeID] {
		st, ok := byNode[nextID]
		if !ok || st.Status != "pending" {
			continue
		}
		ready := true
		for _, pID := range pred[nextID] {
			if statusByNode[pID] != "success" {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		if err := o.enqueueStage(pr, &st); err != nil {
			return err
		}
		statusByNode[nextID] = "queued"
	}
	return nil
}

func (o *PipelineOrchestrator) enqueueStage(pr *model.PipelineRun, stage *model.PipelineStageRun) error {
	now := time.Now()
	_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
		"status":     "queued",
		"started_at": now,
	})
	buildRun, err := o.buildRuns.EnqueueInternal(stage.BuildJobID, pr.TriggeredBy, engine.EnqueueParams{
		TriggerType: "pipeline",
	})
	if err != nil {
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":        "failed",
			"error_message": err.Error(),
			"finished_at":   time.Now(),
		})
		return fmt.Errorf("enqueue build job %d for node %s: %w", stage.BuildJobID, stage.NodeID, err)
	}
	rid := buildRun.ID
	_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
		"status":       "running",
		"build_run_id": rid,
	})
	return nil
}

func (o *PipelineOrchestrator) maybeComplete(pipelineRunID uint) {
	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil || pr.Status != "running" {
		return
	}
	for _, st := range pr.Stages {
		if st.Status != "success" {
			return
		}
	}
	now := time.Now()
	_ = o.runs.UpdateFields(pipelineRunID, map[string]interface{}{
		"status":      "success",
		"finished_at": now,
	})
}

func (o *PipelineOrchestrator) failPipeline(pipelineRunID uint, msg string) error {
	lock := o.runLock(pipelineRunID)
	lock.Lock()
	defer lock.Unlock()
	return o.failPipelineLocked(pipelineRunID, msg)
}

func (o *PipelineOrchestrator) cancelPipeline(pipelineRunID uint, msg string) error {
	lock := o.runLock(pipelineRunID)
	lock.Lock()
	defer lock.Unlock()
	return o.finalizePipelineLocked(pipelineRunID, "cancelled", msg)
}

func (o *PipelineOrchestrator) failPipelineLocked(pipelineRunID uint, msg string) error {
	return o.finalizePipelineLocked(pipelineRunID, "failed", msg)
}

// finalizePipelineLocked marks the pipeline terminal, skips never-started stages,
// cancels non-terminal sibling BuildRuns, and marks those stages cancelled.
func (o *PipelineOrchestrator) finalizePipelineLocked(pipelineRunID uint, status, msg string) error {
	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil {
		return err
	}
	if pr.Status == "success" || pr.Status == "failed" || pr.Status == "cancelled" {
		return nil
	}
	now := time.Now()
	_ = o.runs.UpdateFields(pipelineRunID, map[string]interface{}{
		"status":        status,
		"error_message": msg,
		"finished_at":   now,
	})

	var cancelIDs []uint
	for _, st := range pr.Stages {
		if isStageTerminal(st.Status) {
			continue
		}
		switch st.Status {
		case "pending":
			_ = o.runs.UpdateStageFields(st.ID, map[string]interface{}{
				"status":      "skipped",
				"finished_at": now,
			})
		case "queued", "running":
			if st.BuildRunID != nil && *st.BuildRunID > 0 {
				cancelIDs = append(cancelIDs, *st.BuildRunID)
				_ = o.runs.UpdateStageFields(st.ID, map[string]interface{}{
					"status":      "cancelled",
					"finished_at": now,
				})
			} else {
				_ = o.runs.UpdateStageFields(st.ID, map[string]interface{}{
					"status":      "skipped",
					"finished_at": now,
				})
			}
		default:
			_ = o.runs.UpdateStageFields(st.ID, map[string]interface{}{
				"status":      "skipped",
				"finished_at": now,
			})
		}
	}
	for _, buildRunID := range cancelIDs {
		o.buildRuns.CancelInternal(buildRunID)
	}
	if o.logger != nil {
		o.logger.Info("pipeline finalized",
			zap.Uint("pipeline_run_id", pipelineRunID),
			zap.String("status", status),
			zap.String("msg", msg),
		)
	}
	return nil
}

func isStageTerminal(status string) bool {
	switch status {
	case "success", "failed", "cancelled", "skipped", "interrupted":
		return true
	default:
		return false
	}
}

func stageByNode(stages []model.PipelineStageRun) map[string]model.PipelineStageRun {
	m := make(map[string]model.PipelineStageRun, len(stages))
	for _, st := range stages {
		m[st.NodeID] = st
	}
	return m
}
