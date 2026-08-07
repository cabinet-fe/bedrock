package service

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	aimodel "bedrock/internal/ai/model"
	aiservice "bedrock/internal/ai/service"
	"bedrock/internal/cicd/model"
	"bedrock/internal/cicd/repository"
	"bedrock/internal/engine"
	"bedrock/internal/pkg"
	rbacmodel "bedrock/internal/rbac/model"
)

// AgentRunLauncher abstracts aiservice.AgentService for pipeline agent stages.
// CancelRun intentionally skips terminal-hook callbacks (like BuildRunService
// .CancelInternal): the orchestrator marks stage state itself while finalizing.
type AgentRunLauncher interface {
	GetAgent(id uint) (*aimodel.AiAgent, error)
	CreateRun(agentID uint, in aiservice.CreateRunInput) (*aimodel.AgentRun, error)
	CancelRun(id uint) error
}

// PipelineOrchestrator starts PipelineRuns and advances stages on run terminal.
// Semantics (graph_json v2): exactly one start node; a node fires when every
// predecessor terminal-matches ≥1 incoming edge condition; skipped propagates
// when no incoming edge can fire; reaching an end node finalizes success;
// quiescence without end finalizes failed.
type PipelineOrchestrator struct {
	pipelines  *repository.BuildPipelineRepository
	runs       *repository.PipelineRunRepository
	jobs       *repository.BuildJobRepository
	scriptJobs *repository.ScriptJobRepository
	buildRuns  *BuildRunService
	scriptRuns *ScriptRunService
	agents     AgentRunLauncher
	logger     *zap.Logger

	mu    sync.Mutex
	locks map[uint]*sync.Mutex
}

func NewPipelineOrchestrator(
	pipelines *repository.BuildPipelineRepository,
	runs *repository.PipelineRunRepository,
	jobs *repository.BuildJobRepository,
	scriptJobs *repository.ScriptJobRepository,
	buildRuns *BuildRunService,
	scriptRuns *ScriptRunService,
	agents AgentRunLauncher,
	logger *zap.Logger,
) *PipelineOrchestrator {
	return &PipelineOrchestrator{
		pipelines:  pipelines,
		runs:       runs,
		jobs:       jobs,
		scriptJobs: scriptJobs,
		buildRuns:  buildRuns,
		scriptRuns: scriptRuns,
		agents:     agents,
		logger:     logger,
		locks:      make(map[uint]*sync.Mutex),
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

func (o *PipelineOrchestrator) refChecker() PipelineRefChecker {
	return PipelineRefChecker{
		BuildJobExists: func(id uint) bool {
			_, e := o.jobs.FindByID(id)
			return e == nil
		},
		ScriptJobExists: func(id uint) bool {
			if o.scriptJobs == nil {
				return false
			}
			_, e := o.scriptJobs.FindByID(id)
			return e == nil
		},
		AgentExists: func(id uint) bool {
			if o.agents == nil {
				return false
			}
			_, e := o.agents.GetAgent(id)
			return e == nil
		},
	}
}

type EnqueuePipelineInput struct {
	TriggerType string `json:"trigger_type"`
}

func (o *PipelineOrchestrator) List(q pkg.ListQuery, pipelineID *uint, status string, projectID *uint, userID uint, dataScope string) ([]model.PipelineRun, int64, error) {
	var createdBy *uint
	// D3: 带 project_id 时跳过 created_by/is_public 数据范围过滤
	if projectID == nil && dataScope != rbacmodel.DataScopeAll {
		createdBy = &userID
	}
	items, total, err := o.runs.List(q, pipelineID, status, createdBy, projectID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.PipelineRun, len(items))
	for i := range items {
		items[i].SnapshotJSON = SanitizeGraphEnvVars(items[i].SnapshotJSON)
		out[i] = items[i]
	}
	return out, total, nil
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
	run.SnapshotJSON = SanitizeGraphEnvVars(run.SnapshotJSON)
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

// EnqueueInternal creates a PipelineRun and fires the start node (cron/webhook/manual).
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
	if err := ValidatePipelineDAG(g, o.refChecker()); err != nil {
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
		st := model.PipelineStageRun{
			PipelineRunID: run.ID,
			NodeID:        n.ID,
			NodeType:      n.NodeType(),
			Status:        "pending",
		}
		switch st.NodeType {
		case PipelineNodeBuildJob:
			st.BuildJobID = n.Data.BuildJobID
		case PipelineNodeScriptJob:
			st.ScriptJobID = n.Data.ScriptJobID
		case PipelineNodeAgent:
			st.AgentID = n.Data.AgentID
		}
		stages = append(stages, st)
	}
	if err := o.runs.CreateStages(stages); err != nil {
		return nil, err
	}

	lock := o.runLock(run.ID)
	lock.Lock()
	defer lock.Unlock()
	pr, err := o.runs.FindByID(run.ID)
	if err != nil {
		return nil, err
	}
	if err := o.advanceLocked(pr); err != nil {
		_ = o.failPipelineLocked(run.ID, err.Error())
	}
	return o.runs.FindByID(run.ID)
}

// Cancel stops a non-terminal PipelineRun: cancels in-flight runs, skips never-started stages.
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
	if o == nil || run == nil || run.ID == 0 || run.TriggerType != "pipeline" {
		return
	}
	stage, err := o.runs.FindStageByBuildRunID(run.ID)
	if err != nil {
		return
	}
	o.onStageTerminal(stage.PipelineRunID, stage.ID, status, run.ErrorMessage)
}

// OnScriptRunTerminal implements engine.ScriptRunTerminalHook.
func (o *PipelineOrchestrator) OnScriptRunTerminal(run *model.ScriptRun, status string) {
	if o == nil || run == nil || run.ID == 0 || run.TriggerType != "pipeline" {
		return
	}
	stage, err := o.runs.FindStageByScriptRunID(run.ID)
	if err != nil {
		return
	}
	o.onStageTerminal(stage.PipelineRunID, stage.ID, status, run.ErrorMessage)
}

// OnAgentRunTerminal implements aiservice.RunTerminalHook.
func (o *PipelineOrchestrator) OnAgentRunTerminal(run *aimodel.AgentRun, status string) {
	if o == nil || run == nil || run.ID == 0 || run.TriggerType != aimodel.TriggerPipeline {
		return
	}
	stage, err := o.runs.FindStageByAgentRunID(run.ID)
	if err != nil {
		return
	}
	o.onStageTerminal(stage.PipelineRunID, stage.ID, status, run.ErrorMessage)
}

// onStageTerminal syncs the stage row and advances the DAG under the run lock.
func (o *PipelineOrchestrator) onStageTerminal(pipelineRunID, stageID uint, status, errMsg string) {
	switch status {
	case "success", "failed", "cancelled", "interrupted":
	default:
		return
	}
	lock := o.runLock(pipelineRunID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	// Always sync the stage row (including late terminals after pipeline finalize).
	_ = o.runs.UpdateStageFields(stageID, map[string]interface{}{
		"status":        status,
		"error_message": errMsg,
		"finished_at":   now,
	})

	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil {
		return
	}
	if pr.Status != "running" && pr.Status != "queued" {
		return
	}
	if err := o.advanceLocked(pr); err != nil {
		_ = o.failPipelineLocked(pipelineRunID, err.Error())
	}
}

// advanceLocked fires ready nodes to a fixpoint, propagates skipped, then
// settles the pipeline when nothing is left running. Caller holds the run lock.
func (o *PipelineOrchestrator) advanceLocked(pr *model.PipelineRun) error {
	g, err := ParsePipelineGraph(pr.SnapshotJSON)
	if err != nil {
		return err
	}
	nodeByID := NodeByID(g)
	_, pred := GraphAdjacency(g)
	byNode := stageByNode(pr.Stages)
	statusByNode := make(map[string]string, len(pr.Stages))
	for _, st := range pr.Stages {
		statusByNode[st.NodeID] = st.Status
	}

	for changed := true; changed; {
		changed = false
		if pr.Status != "running" && pr.Status != "queued" {
			return nil // finalized by an end node mid-loop
		}
		for _, n := range g.Nodes {
			st, ok := byNode[n.ID]
			if !ok || statusByNode[n.ID] != "pending" {
				continue
			}
			inEdges := pred[n.ID]
			ready := false
			waiting := false
			if nodeByID[n.ID].NodeType() == PipelineNodeEnd {
				// End nodes are OR-joins: the first matched incoming path reaches
				// the end and succeeds the pipeline, whatever other branches did.
				for _, e := range inEdges {
					ps := statusByNode[e.Source]
					if !isStageTerminal(ps) {
						waiting = true
						continue
					}
					if ps != "skipped" && EdgeConditionMatches(e.Condition(), ps) {
						ready = true
					}
				}
			} else {
				// Task/start nodes are AND-joins: every predecessor must be
				// terminal with ≥1 matching incoming edge.
				ready = true
				for _, e := range inEdges {
					ps := statusByNode[e.Source]
					if !isStageTerminal(ps) {
						waiting = true
						ready = false
						break
					}
					if ps == "skipped" || !EdgeConditionMatches(e.Condition(), ps) {
						ready = false
					}
				}
			}
			if waiting {
				continue
			}
			if !ready {
				// No incoming edge can fire anymore: skip and propagate.
				now := time.Now()
				_ = o.runs.UpdateStageFields(st.ID, map[string]interface{}{
					"status":      "skipped",
					"finished_at": now,
				})
				statusByNode[n.ID] = "skipped"
				changed = true
				continue
			}
			finalized, newStatus, err := o.fireStage(pr, st, nodeByID[n.ID])
			if err != nil {
				return err
			}
			statusByNode[n.ID] = newStatus
			changed = true
			if finalized {
				pr.Status = "success"
				return nil
			}
		}
	}

	// Quiescence: nothing queued/running and no pending node can fire anymore.
	inFlight, pending := false, false
	for _, s := range statusByNode {
		switch s {
		case "queued", "running":
			inFlight = true
		case "pending":
			pending = true
		}
	}
	if inFlight {
		return nil // terminal hooks will re-enter advanceLocked
	}
	if pending {
		return o.failPipelineLocked(pr.ID, "流水线存在无法调度的节点")
	}
	return o.failPipelineLocked(pr.ID, "未到达结束节点")
}

// fireStage starts one ready node by type. Returns (pipelineFinalized, newStageStatus, error).
// Enqueue/decrypt failures become a failed stage (failure edges apply), not a pipeline error.
func (o *PipelineOrchestrator) fireStage(pr *model.PipelineRun, stage model.PipelineStageRun, node PipelineGraphNode) (bool, string, error) {
	now := time.Now()
	markFailed := func(msg string) (bool, string, error) {
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":        "failed",
			"error_message": msg,
			"started_at":    now,
			"finished_at":   now,
		})
		return false, "failed", nil
	}

	switch node.NodeType() {
	case PipelineNodeStart:
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":      "success",
			"started_at":  now,
			"finished_at": now,
		})
		return false, "success", nil

	case PipelineNodeEnd:
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":      "success",
			"started_at":  now,
			"finished_at": now,
		})
		if err := o.finalizePipelineLocked(pr.ID, "success", "reached end node "+node.ID); err != nil {
			return false, "success", err
		}
		return true, "success", nil

	case PipelineNodeBuildJob:
		overrides, err := DecryptNodeEnvVars(node.Data.EnvVars)
		if err != nil {
			return markFailed(err.Error())
		}
		buildRun, err := o.buildRuns.EnqueueInternal(node.Data.BuildJobID, pr.TriggeredBy, engine.EnqueueParams{
			TriggerType:  "pipeline",
			EnvOverrides: overrides,
		})
		if err != nil {
			return markFailed(fmt.Sprintf("enqueue build job %d: %v", node.Data.BuildJobID, err))
		}
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":       "running",
			"started_at":   now,
			"build_run_id": buildRun.ID,
		})
		return false, "running", nil

	case PipelineNodeScriptJob:
		if o.scriptRuns == nil {
			return markFailed("script run service unavailable")
		}
		overrides, err := DecryptNodeEnvVars(node.Data.EnvVars)
		if err != nil {
			return markFailed(err.Error())
		}
		scriptRun, err := o.scriptRuns.EnqueueWithOverrides(node.Data.ScriptJobID, pr.TriggeredBy, "pipeline", overrides)
		if err != nil {
			return markFailed(fmt.Sprintf("enqueue script job %d: %v", node.Data.ScriptJobID, err))
		}
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":        "running",
			"started_at":    now,
			"script_run_id": scriptRun.ID,
		})
		return false, "running", nil

	case PipelineNodeAgent:
		if o.agents == nil {
			return markFailed("agent service unavailable")
		}
		agentRun, err := o.agents.CreateRun(node.Data.AgentID, aiservice.CreateRunInput{
			TriggerType: aimodel.TriggerPipeline,
			TriggeredBy: pr.TriggeredBy,
		})
		if err != nil {
			return markFailed(fmt.Sprintf("enqueue agent %d: %v", node.Data.AgentID, err))
		}
		_ = o.runs.UpdateStageFields(stage.ID, map[string]interface{}{
			"status":       "running",
			"started_at":   now,
			"agent_run_id": agentRun.ID,
		})
		return false, "running", nil
	}
	return markFailed("unknown node type: " + node.NodeType())
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
// cancels non-terminal sibling runs (build/script/agent), and marks those stages
// cancelled. Caller holds the run lock; CancelInternal-style APIs skip hooks.
func (o *PipelineOrchestrator) finalizePipelineLocked(pipelineRunID uint, status, msg string) error {
	pr, err := o.runs.FindByID(pipelineRunID)
	if err != nil {
		return err
	}
	if pr.Status == "success" || pr.Status == "failed" || pr.Status == "cancelled" {
		return nil
	}
	now := time.Now()
	fields := map[string]interface{}{
		"status":      status,
		"finished_at": now,
	}
	if status == "success" {
		fields["error_message"] = "" // success is not an error; keep the banner empty
	} else {
		fields["error_message"] = msg
	}
	_ = o.runs.UpdateFields(pipelineRunID, fields)

	var buildRunIDs, scriptRunIDs, agentRunIDs []uint
	for _, st := range pr.Stages {
		if isStageTerminal(st.Status) {
			continue
		}
		staged := false
		if st.Status == "queued" || st.Status == "running" {
			switch {
			case st.BuildRunID != nil && *st.BuildRunID > 0:
				buildRunIDs = append(buildRunIDs, *st.BuildRunID)
				staged = true
			case st.ScriptRunID != nil && *st.ScriptRunID > 0:
				scriptRunIDs = append(scriptRunIDs, *st.ScriptRunID)
				staged = true
			case st.AgentRunID != nil && *st.AgentRunID > 0:
				agentRunIDs = append(agentRunIDs, *st.AgentRunID)
				staged = true
			}
		}
		fields := map[string]interface{}{
			"status":      "skipped",
			"finished_at": now,
		}
		if staged {
			fields["status"] = "cancelled"
		}
		_ = o.runs.UpdateStageFields(st.ID, fields)
	}
	for _, id := range buildRunIDs {
		o.buildRuns.CancelInternal(id)
	}
	if o.scriptRuns != nil {
		for _, id := range scriptRunIDs {
			o.scriptRuns.CancelInternal(id)
		}
	}
	if o.agents != nil {
		for _, id := range agentRunIDs {
			_ = o.agents.CancelRun(id)
		}
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
