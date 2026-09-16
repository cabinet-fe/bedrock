package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	cicdmodel "bedrock/internal/cicd/model"
	"bedrock/internal/engine"
	"bedrock/internal/harness/provider"
	harnessservice "bedrock/internal/harness/service"
	resourcemodel "bedrock/internal/resource/model"
	"bedrock/internal/ws"
)

// AuditWriter appends operation-log entries (implemented by system AuditService).
type AuditWriter interface {
	Write(userID uint, username, action, resourceType, resourceID, details, ip string) error
}

// DocDraftWriter writes draft content after a docs_generate AgentRun succeeds.
type DocDraftWriter interface {
	WriteDraftFromAgentRun(projectID, nodeID, runID uint, content string, userID uint) error
}

// TerminalNotifier persists + pushes per-user inbox notifications on AgentRun terminal.
type TerminalNotifier interface {
	NotifyAgentRun(userID uint, agentRunID, agentID uint, status string)
}

// AgentFailureMailer fans failed/interrupted AgentRun notices out to mail
// channels (implemented by system MailDispatcher, wired in cmd/server).
type AgentFailureMailer interface {
	DispatchAgentRunFailure(triggeredBy, agentCreatedBy, runID uint, status, message string)
}

// RunTerminalHook is invoked for every AgentRun terminal status
// (success|failed|cancelled|interrupted). Used by PipelineOrchestrator to
// advance in-pipeline agent stages (graph_json v2 allows sync agent nodes).
type RunTerminalHook interface {
	OnAgentRunTerminal(run *model.AgentRun, status string)
}

type AgentService struct {
	repo        *repository.AIRepository
	skills      *SkillService
	hub         *ws.Hub
	logger      *zap.Logger
	workDir     string
	artifactDir string
	logDir      string
	docs        DocDraftWriter
	repos       RepositoryFinder
	secrets     SecretResolver
	gitCheckout GitCheckoutFunc
	audit       AuditWriter
	notifier    TerminalNotifier
	mailer      AgentFailureMailer
	termHook    RunTerminalHook
	wsInitSync  bool
	inlineExec  bool

	// Harness session backend (nil-set = disabled, see SetHarnessBackend).
	harnessSessions    *harnessservice.SessionService
	harnessStreams     *harnessservice.StreamService
	harnessProvider    provider.Provider
	harnessIdleConfirm time.Duration
	harnessNoTerminal  time.Duration
	// harnessConfig renders the BYOK provider config into agent workspaces
	// (nil in tests: SyncAgentWorkspace then only drops stale config files).
	harnessConfig *HarnessConfigService

	runs    chan uint
	stop    chan struct{}
	wg      sync.WaitGroup
	startMu sync.Mutex
	started bool

	cronMu  sync.Mutex
	cron    *cron.Cron
	cronIDs map[uint]cron.EntryID

	wsInitMu  sync.Mutex
	wsInitGen map[uint]uint64
	wsInitWg  sync.WaitGroup

	cancelMu sync.Mutex
	cancels  map[uint]context.CancelFunc

	execCtx    context.Context
	execCancel context.CancelFunc
}

// SetTerminalNotifier wires DESIGN §12 in-app notifications for agent terminal states.
func (s *AgentService) SetTerminalNotifier(n TerminalNotifier) {
	s.notifier = n
}

// SetFailureMailer wires failure-notice mail dispatch for agent terminal states.
func (s *AgentService) SetFailureMailer(m AgentFailureMailer) {
	s.mailer = m
}

// SetTerminalHook wires PipelineOrchestrator on AgentRun terminal.
func (s *AgentService) SetTerminalHook(h RunTerminalHook) {
	s.termHook = h
}

// SetHarnessConfig wires the BYOK provider config renderer used by
// SyncAgentWorkspace (see harness_config.go).
func (s *AgentService) SetHarnessConfig(c *HarnessConfigService) {
	s.harnessConfig = c
}

// WorkspaceRoot returns the bedrock workspace root backing the agent-form
// harness model catalog (GET /ai/models).
func (s *AgentService) WorkspaceRoot() string {
	return s.workDir
}

// SetSyncWorkspaceInit runs workspace init inline instead of a goroutine (tests).
func (s *AgentService) SetSyncWorkspaceInit(v bool) { s.wsInitSync = v }

// SetInlineExec runs ExecuteRun inside submit instead of the async worker (tests).
func (s *AgentService) SetInlineExec(v bool) { s.inlineExec = v }

// locSchedule interprets cron fields in loc (equivalent to cron.WithLocation per trigger).
type locSchedule struct {
	inner cron.Schedule
	loc   *time.Location
}

func (s locSchedule) Next(t time.Time) time.Time {
	return s.inner.Next(t.In(s.loc))
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

var (
	claimableRunStatuses = []string{model.JobQueued, model.JobPending}
	liveRunStatuses      = []string{model.JobQueued, model.JobRunning, model.JobPending}
)

func NewAgentService(
	repo *repository.AIRepository,
	skills *SkillService,
	hub *ws.Hub,
	logger *zap.Logger,
	workDir, artifactDir, logDir string,
	audit ...AuditWriter,
) *AgentService {
	svc := &AgentService{
		repo: repo, skills: skills, hub: hub, logger: logger,
		workDir: workDir, artifactDir: artifactDir, logDir: logDir,
		runs: make(chan uint, 128), stop: make(chan struct{}),
		cronIDs:   make(map[uint]cron.EntryID),
		wsInitGen: make(map[uint]uint64),
		cancels:   make(map[uint]context.CancelFunc),
	}
	if len(audit) > 0 {
		svc.audit = audit[0]
	}
	return svc
}

func (s *AgentService) SetDocDraftWriter(w DocDraftWriter) { s.docs = w }

func (s *AgentService) Start() {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.started {
		return
	}
	s.started = true
	s.execCtx, s.execCancel = context.WithCancel(context.Background())
	s.cron = cron.New(cron.WithLocation(time.UTC), cron.WithParser(cronParser))
	s.wg.Add(1)
	go s.worker()
	_ = s.reloadCronLocked()
	s.cron.Start()
}

func (s *AgentService) Shutdown() {
	s.startMu.Lock()
	if !s.started {
		s.startMu.Unlock()
		return
	}
	s.started = false
	if s.cron != nil {
		s.cron.Stop()
	}
	close(s.stop)
	if s.execCancel != nil {
		s.execCancel()
	}
	s.abortAllRuns()
	s.startMu.Unlock()
	s.wg.Wait()
	s.wsInitWg.Wait()
}

func (s *AgentService) RecoverOnStartup() error {
	if _, err := s.repo.MarkRunningRunsInterrupted(); err != nil {
		return err
	}
	queued, err := s.repo.ListRunsByStatuses(model.JobQueued, model.JobPending)
	if err != nil {
		return err
	}
	for _, run := range queued {
		_ = s.submit(run.ID)
	}
	return nil
}

type AgentInput struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	Enabled      *bool               `json:"enabled"`
	SystemPrompt string              `json:"system_prompt"`
	SkillIDs     []uint              `json:"skill_ids"`
	RepoBindings []model.RepoBinding `json:"repo_bindings"`
	EnvVars      []EnvVarInput       `json:"env_vars"`
	OutputDir    string              `json:"output_dir"`
	TimeoutSec   int                 `json:"timeout_sec"`
	// Session-model override and bridge approval mode (manual | auto).
	ModelProvider string `json:"model_provider"`
	ModelID       string `json:"model_id"`
	// ReasoningEffort is the default reasoning effort passed to the model
	// (valid values come from the chosen model's reasoning_efforts options).
	ReasoningEffort     string `json:"reasoning_effort"`
	ApprovalMode        string `json:"approval_mode"`
	InjectDefaultPrompt *bool  `json:"inject_default_prompt"`
}

// validateAgentModelConfig checks the model override pair, the reasoning
// effort and the approval mode enum. Catalog membership is validated
// client-side against GET /ai/models (the backend create path must also work
// while serve is restarting).
func validateAgentModelConfig(modelProvider, modelID, reasoningEffort, approvalMode string) error {
	switch approvalMode {
	case "", harnessservice.ApprovalManual, harnessservice.ApprovalAuto:
	default:
		return errors.New("approval_mode 必须为 manual 或 auto")
	}
	hasProvider := strings.TrimSpace(modelProvider) != ""
	hasID := strings.TrimSpace(modelID) != ""
	if hasProvider != hasID {
		return errors.New("model_provider 与 model_id 必须同时提供")
	}
	effort := strings.TrimSpace(reasoningEffort)
	if effort != "" && !hasID {
		return errors.New("reasoning_effort 需要同时配置模型")
	}
	if strings.ContainsAny(effort, " \t\r\n") || len(effort) > 40 {
		return errors.New("reasoning_effort 必须为不超过 40 字符的非空格值")
	}
	if hasProvider && !IsBedrockHarnessProvider(modelProvider) {
		return errors.New("model_provider 必须为 bedrock-p* 平台 BYOK 提供商")
	}
	return nil
}

func (s *AgentService) CreateAgent(createdBy uint, in AgentInput) (*model.AiAgent, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("名称不能为空")
	}
	if err := validateAgentModelConfig(in.ModelProvider, in.ModelID, in.ReasoningEffort, in.ApprovalMode); err != nil {
		return nil, err
	}
	agent := &model.AiAgent{
		Name: name, Description: strings.TrimSpace(in.Description),
		Enabled:             boolOr(in.Enabled, true),
		ModelProvider:       strings.TrimSpace(in.ModelProvider),
		ModelID:             strings.TrimSpace(in.ModelID),
		ReasoningEffort:     strings.TrimSpace(in.ReasoningEffort),
		ApprovalMode:        stringOr(in.ApprovalMode, harnessservice.ApprovalManual),
		InjectDefaultPrompt: boolOr(in.InjectDefaultPrompt, true),
		SystemPrompt:        in.SystemPrompt,
		OutputDir:     stringOr(in.OutputDir, "output"),
		TimeoutSec:    intOr(in.TimeoutSec, 600), CreatedBy: createdBy,
		WorkspaceStatus: model.WorkspacePending,
		WorkspaceError:  "",
	}
	if err := encodeSkillIDs(agent, in.SkillIDs); err != nil {
		return nil, err
	}
	bindings, err := s.normalizeRepoBindings(in.RepoBindings)
	if err != nil {
		return nil, err
	}
	if in.EnvVars != nil {
		if err := applyAgentEnvVarsInput(agent, in.EnvVars); err != nil {
			return nil, err
		}
	}
	if err := s.repo.CreateAgent(agent); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceAgentRepoBindings(agent.ID, bindings); err != nil {
		_ = s.repo.DeleteAgent(agent.ID)
		return nil, err
	}
	decodeSkillIDs(agent)
	agent.RepoBindings = bindings
	projectAgentEnvVars(agent)
	s.enqueueWorkspaceInit(agent.ID, createdBy)
	if s.audit != nil {
		_ = s.audit.Write(createdBy, "", "agent_create", "ai_agent", fmt.Sprintf("%d", agent.ID), agent.Name, "")
	}
	return agent, nil
}

func (s *AgentService) UpdateAgent(id, userID uint, in AgentInput) (*model.AiAgent, error) {
	agent, err := s.repo.FindAgent(id)
	if err != nil {
		return nil, err
	}
	if err := validateAgentModelConfig(in.ModelProvider, in.ModelID, in.ReasoningEffort, in.ApprovalMode); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) != "" {
		agent.Name = strings.TrimSpace(in.Name)
	}
	if in.Description != "" {
		agent.Description = strings.TrimSpace(in.Description)
	}
	if in.Enabled != nil {
		agent.Enabled = *in.Enabled
	}
	if strings.TrimSpace(in.ModelProvider) != "" || strings.TrimSpace(in.ModelID) != "" {
		agent.ModelProvider = strings.TrimSpace(in.ModelProvider)
		agent.ModelID = strings.TrimSpace(in.ModelID)
	}
	// Effort always follows the request ("" = model default) so the form can
	// clear it; validation above rejects it without a model override.
	agent.ReasoningEffort = strings.TrimSpace(in.ReasoningEffort)
	if strings.TrimSpace(in.ApprovalMode) != "" {
		agent.ApprovalMode = strings.TrimSpace(in.ApprovalMode)
	}
	if in.InjectDefaultPrompt != nil {
		agent.InjectDefaultPrompt = *in.InjectDefaultPrompt
	}
	if in.SystemPrompt != "" || in.SystemPrompt == "" && in.Name != "" {
		agent.SystemPrompt = in.SystemPrompt
	}
	if in.SkillIDs != nil {
		if err := encodeSkillIDs(agent, in.SkillIDs); err != nil {
			return nil, err
		}
	}
	var bindings []model.RepoBinding
	updateBindings := in.RepoBindings != nil
	if updateBindings {
		var err error
		bindings, err = s.normalizeRepoBindings(in.RepoBindings)
		if err != nil {
			return nil, err
		}
	}
	if in.EnvVars != nil {
		if err := applyAgentEnvVarsInput(agent, in.EnvVars); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(in.OutputDir) != "" {
		agent.OutputDir = strings.TrimSpace(in.OutputDir)
	}
	if in.TimeoutSec > 0 {
		agent.TimeoutSec = in.TimeoutSec
	}
	agent.WorkspaceStatus = model.WorkspacePending
	agent.WorkspaceError = ""
	if err := s.repo.UpdateAgent(agent); err != nil {
		return nil, err
	}
	if updateBindings {
		if err := s.repo.ReplaceAgentRepoBindings(agent.ID, bindings); err != nil {
			return nil, err
		}
		agent.RepoBindings = bindings
	} else if err := s.attachRepoBindings(agent); err != nil {
		return nil, err
	}
	decodeSkillIDs(agent)
	projectAgentEnvVars(agent)
	s.enqueueWorkspaceInit(agent.ID, userID)
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "agent_update", "ai_agent", fmt.Sprintf("%d", agent.ID), agent.Name, "")
	}
	return agent, nil
}

func (s *AgentService) DeleteAgent(id, userID uint) error {
	if err := s.repo.DeleteAgent(id); err != nil {
		return err
	}
	s.removeAgentWorkspace(id)
	s.removeAgentArtifacts(id)
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "agent_delete", "ai_agent", fmt.Sprintf("%d", id), "", "")
	}
	_ = s.ReloadCron()
	return nil
}

func (s *AgentService) GetAgent(id uint) (*model.AiAgent, error) {
	agent, err := s.repo.FindAgent(id)
	if err != nil {
		return nil, err
	}
	decodeSkillIDs(agent)
	if err := s.attachRepoBindings(agent); err != nil {
		return nil, err
	}
	projectAgentEnvVars(agent)
	return agent, nil
}

func (s *AgentService) ListAgents(page, pageSize int) ([]model.AiAgent, int64, error) {
	items, total, err := s.repo.ListAgents(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		decodeSkillIDs(&items[i])
		if err := s.attachRepoBindings(&items[i]); err != nil {
			return nil, 0, err
		}
		projectAgentEnvVars(&items[i])
	}
	return items, total, nil
}

type TriggerInput struct {
	Type           string `json:"type"`
	Enabled        *bool  `json:"enabled"`
	CronExpression string `json:"cron_expression"`
	CronTimezone   string `json:"cron_timezone"`
	BuildJobID     *uint  `json:"build_job_id"`
	BuildEvent     string `json:"build_event"`
}

func (s *AgentService) CreateTrigger(agentID, userID uint, in TriggerInput) (*model.AgentTrigger, error) {
	if _, err := s.repo.FindAgent(agentID); err != nil {
		return nil, errors.New("智能体不存在")
	}
	t := &model.AgentTrigger{
		AgentID: agentID, Type: strings.TrimSpace(in.Type),
		Enabled:        boolOr(in.Enabled, true),
		CronExpression: strings.TrimSpace(in.CronExpression),
		CronTimezone:   stringOr(in.CronTimezone, "UTC"),
		BuildJobID:     in.BuildJobID,
		BuildEvent:     strings.TrimSpace(in.BuildEvent),
	}
	if err := validateTrigger(t); err != nil {
		return nil, err
	}
	if err := s.repo.CreateTrigger(t); err != nil {
		return nil, err
	}
	_ = s.ReloadCron()
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "agent_trigger_create", "agent_trigger", fmt.Sprintf("%d", t.ID),
			fmt.Sprintf("agent_id=%d type=%s", agentID, t.Type), "")
	}
	return t, nil
}

func (s *AgentService) UpdateTrigger(id, userID uint, in TriggerInput) (*model.AgentTrigger, error) {
	t, err := s.repo.FindTrigger(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Type) != "" {
		t.Type = strings.TrimSpace(in.Type)
	}
	if in.Enabled != nil {
		t.Enabled = *in.Enabled
	}
	if in.CronExpression != "" {
		t.CronExpression = strings.TrimSpace(in.CronExpression)
	}
	if in.CronTimezone != "" {
		t.CronTimezone = strings.TrimSpace(in.CronTimezone)
	}
	if in.BuildJobID != nil {
		t.BuildJobID = in.BuildJobID
	}
	if in.BuildEvent != "" {
		t.BuildEvent = strings.TrimSpace(in.BuildEvent)
	}
	if err := validateTrigger(t); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateTrigger(t); err != nil {
		return nil, err
	}
	_ = s.ReloadCron()
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "agent_trigger_update", "agent_trigger", fmt.Sprintf("%d", t.ID),
			fmt.Sprintf("agent_id=%d type=%s", t.AgentID, t.Type), "")
	}
	return t, nil
}

func (s *AgentService) DeleteTrigger(id, userID uint) error {
	if err := s.repo.DeleteTrigger(id); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "agent_trigger_delete", "agent_trigger", fmt.Sprintf("%d", id), "", "")
	}
	return s.ReloadCron()
}

func (s *AgentService) ListTriggers(agentID uint) ([]model.AgentTrigger, error) {
	return s.repo.ListTriggers(agentID)
}

type CreateRunInput struct {
	TriggerType string
	TriggerID   *uint
	TriggeredBy uint
	BuildRunID  *uint
	ProjectID   *uint
	DocNodeID   *uint
	UserPrompt  string
}

func (s *AgentService) CreateRun(agentID uint, in CreateRunInput) (*model.AgentRun, error) {
	agent, err := s.repo.FindAgent(agentID)
	if err != nil {
		return nil, errors.New("智能体不存在")
	}
	if !agent.Enabled {
		return nil, errors.New("智能体未启用")
	}
	if agent.WorkspaceStatus != model.WorkspaceReady {
		return nil, errors.New("智能体工作区未初始化完成")
	}
	if !s.HarnessEnabled() {
		return nil, ErrHarnessDisabled
	}
	decodeSkillIDs(agent)
	if err := s.attachRepoBindings(agent); err != nil {
		return nil, err
	}
	userPrompt := strings.TrimSpace(in.UserPrompt)
	snapshot, _ := json.Marshal(map[string]any{
		"agent_id":       agent.ID,
		"model_provider": agent.ModelProvider,
		"model_id":       agent.ModelID,
		"approval_mode":  agent.ApprovalMode,
		"system_prompt":  agent.SystemPrompt,
		"user_prompt":    userPrompt,
		"skill_ids":      agent.SkillIDs,
		"repo_bindings":  agent.RepoBindings,
		"env_var_keys":   envVarKeys(agent),
		"output_dir":     agent.OutputDir,
		"timeout_sec":    agent.TimeoutSec,
		"context_note":   "persistent agent workspace + harness session + fixed output_dir + skills + repo checkouts",
		"risk_notice":    resourcemodel.RiskNoticeSameUID,
	})
	run := &model.AgentRun{
		AgentID: agentID, TriggerType: in.TriggerType, TriggerID: in.TriggerID,
		Status: model.JobQueued, TriggeredBy: in.TriggeredBy,
		BuildRunID: in.BuildRunID, ProjectID: in.ProjectID, DocNodeID: in.DocNodeID,
		UserPrompt:   userPrompt,
		SnapshotJSON: string(snapshot),
		WorkDir:      s.agentRoot(agentID),
	}
	if err := s.repo.CreateRun(run); err != nil {
		return nil, err
	}
	if err := s.submit(run.ID); err != nil {
		return nil, err
	}
	if s.audit != nil && in.TriggeredBy != 0 {
		_ = s.audit.Write(in.TriggeredBy, "", "agent_run_enqueue", "agent_run", fmt.Sprintf("%d", run.ID),
			fmt.Sprintf("agent_id=%d trigger=%s", agentID, in.TriggerType), "")
	}
	return run, nil
}

func (s *AgentService) ManualRun(agentID, userID uint, userPrompt string) (*model.AgentRun, error) {
	return s.CreateRun(agentID, CreateRunInput{
		TriggerType: model.TriggerManual, TriggeredBy: userID, UserPrompt: userPrompt,
	})
}

func (s *AgentService) APIRun(agentID, userID uint, userPrompt string) (*model.AgentRun, error) {
	return s.CreateRun(agentID, CreateRunInput{
		TriggerType: model.TriggerAPI, TriggeredBy: userID, UserPrompt: userPrompt,
	})
}

func (s *AgentService) DocsGenerateRun(agentID, userID, projectID, nodeID uint) (*model.AgentRun, error) {
	return s.CreateRun(agentID, CreateRunInput{
		TriggerType: model.TriggerDocsGen, TriggeredBy: userID,
		ProjectID: &projectID, DocNodeID: &nodeID,
	})
}

// OnBuildEvent creates AgentRuns asynchronously. Never mutates BuildRun.status.
func (s *AgentService) OnBuildEvent(event string, job *cicdmodel.BuildJob, run *cicdmodel.BuildRun) {
	if job == nil || run == nil || event == "" || event == model.EventNone {
		return
	}
	desired := strings.TrimSpace(job.AgentTriggerEvent)
	if desired == "" {
		desired = model.EventArtifactReady
	}
	if desired == model.EventNone {
		return
	}
	if desired != event {
		return
	}
	if s.inlineExec {
		s.dispatchBuildEvent(event, job, run)
		return
	}
	go s.dispatchBuildEvent(event, job, run)
}

func (s *AgentService) dispatchBuildEvent(event string, job *cicdmodel.BuildJob, run *cicdmodel.BuildRun) {
	// Prefer explicit AgentTrigger rows; also support BuildJob.AgentIDs binding.
	triggers, _ := s.repo.ListBuildEventTriggers(job.ID, event)
	seen := map[uint]bool{}
	for _, t := range triggers {
		if seen[t.AgentID] {
			continue
		}
		seen[t.AgentID] = true
		_, err := s.CreateRun(t.AgentID, CreateRunInput{
			TriggerType: model.TriggerBuildEvent, TriggerID: &t.ID,
			TriggeredBy: run.TriggeredBy, BuildRunID: &run.ID,
		})
		if err != nil && s.logger != nil {
			s.logger.Warn("build event agent run failed", zap.Error(err), zap.Uint("agent_id", t.AgentID))
		}
	}
	for _, agentID := range job.AgentIDs {
		if seen[agentID] {
			continue
		}
		seen[agentID] = true
		_, err := s.CreateRun(agentID, CreateRunInput{
			TriggerType: model.TriggerBuildEvent,
			TriggeredBy: run.TriggeredBy, BuildRunID: &run.ID,
		})
		if err != nil && s.logger != nil {
			s.logger.Warn("build event job agent binding failed", zap.Error(err))
		}
	}
}

func (s *AgentService) GetRun(id uint) (*model.AgentRun, error) {
	return s.repo.FindRun(id)
}

// ArtifactPath returns the success-run snapshot archive for download.
func (s *AgentService) ArtifactPath(id uint) (path string, filename string, err error) {
	run, err := s.repo.FindRun(id)
	if err != nil {
		return "", "", err
	}
	path = strings.TrimSpace(run.ArtifactPath)
	if path == "" {
		return "", "", errors.New("制品不存在")
	}
	if _, err := os.Stat(path); err != nil {
		return "", "", errors.New("制品文件不存在")
	}
	return path, filepath.Base(path), nil
}

func (s *AgentService) ListRuns(page, pageSize int, agentID uint, status string, projectID *uint) ([]model.AgentRun, int64, error) {
	return s.repo.ListRuns(page, pageSize, agentID, status, projectID)
}

func (s *AgentService) CancelRun(id uint) error {
	run, err := s.repo.FindRun(id)
	if err != nil {
		return err
	}
	if run.Status != model.JobQueued && run.Status != model.JobRunning && run.Status != model.JobPending {
		return errors.New("仅 queued/running 可取消")
	}
	n, err := s.repo.UpdateRunFieldsIfStatus(id, liveRunStatuses,
		map[string]any{"status": model.JobCancelled, "finished_at": time.Now().UTC()},
	)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("仅 queued/running 可取消")
	}
	s.abortRun(id)
	return nil
}

func (s *AgentService) trackCancel(id uint, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancels[id] = cancel
	s.cancelMu.Unlock()
}

func (s *AgentService) untrackCancel(id uint) {
	s.cancelMu.Lock()
	delete(s.cancels, id)
	s.cancelMu.Unlock()
}

func (s *AgentService) abortRun(id uint) {
	s.cancelMu.Lock()
	cancel := s.cancels[id]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *AgentService) abortAllRuns() {
	s.cancelMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.cancelMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *AgentService) finishIfCancelled(run *model.AgentRun, writeLog func(string)) bool {
	latest, err := s.repo.FindRun(run.ID)
	if err != nil || latest.Status != model.JobCancelled {
		return false
	}
	if writeLog != nil {
		writeLog("执行已取消")
	}
	s.notifyTerminal(run, model.JobCancelled)
	return true
}

func (s *AgentService) persistTerminal(run *model.AgentRun) bool {
	n, err := s.repo.UpdateRunFieldsIfStatus(run.ID, liveRunStatuses, map[string]any{
		"status":                 run.Status,
		"finished_at":            run.FinishedAt,
		"duration_ms":            run.DurationMs,
		"output_text":            run.OutputText,
		"final_output":           run.FinalOutput,
		"error_message":          run.ErrorMessage,
		"artifact_path":          run.ArtifactPath,
		"artifact_kind":          run.ArtifactKind,
		"skill_digest_json":      run.SkillDigestJSON,
		"harness_session_status": run.Status,
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("agent run terminal persist failed", zap.Uint("run_id", run.ID), zap.Error(err))
		}
		return false
	}
	return n > 0
}

func (s *AgentService) submit(id uint) error {
	if s.inlineExec {
		s.ExecuteRun(context.Background(), id)
		return nil
	}
	s.startMu.Lock()
	started := s.started
	s.startMu.Unlock()
	if !started {
		// Allow enqueue before Start in tests; Start/Recover will pick up queued.
		return nil
	}
	select {
	case s.runs <- id:
		return nil
	default:
		go func() { s.runs <- id }()
		return nil
	}
}

func (s *AgentService) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case id := <-s.runs:
			select {
			case <-s.stop:
				return
			default:
			}
			s.ExecuteRun(s.execCtx, id)
		}
	}
}

func (s *AgentService) ExecuteRun(ctx context.Context, id uint) {
	run, err := s.repo.FindRun(id)
	if err != nil {
		return
	}
	if run.Status != model.JobQueued && run.Status != model.JobPending {
		// A run cancelled while queued never reaches ExecuteRun's normal
		// terminal paths; still notify so consumers (e.g. pipelines) advance.
		if run.Status == model.JobCancelled {
			s.notifyTerminal(run, model.JobCancelled)
		}
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.trackCancel(id, cancel)
	defer func() {
		s.untrackCancel(id)
		cancel()
	}()
	if runCtx.Err() != nil {
		return
	}

	agent, err := s.repo.FindAgent(run.AgentID)
	if err != nil {
		s.failRun(run, err)
		return
	}
	decodeSkillIDs(agent)
	if err := s.attachRepoBindings(agent); err != nil {
		s.failRun(run, err)
		return
	}
	if !s.HarnessEnabled() {
		// Recovered queued runs on a harness-disabled deployment.
		s.failRun(run, ErrHarnessDisabled)
		return
	}

	now := time.Now().UTC()
	run.Status = model.JobRunning
	run.StartedAt = &now
	logDir := filepath.Join(s.logDir, "ai-runs")
	_ = os.MkdirAll(logDir, 0o755)
	run.LogPath = filepath.Join(logDir, fmt.Sprintf("run-%d.log", run.ID))
	agentRoot := s.agentRoot(agent.ID)
	run.WorkDir = agentRoot
	n, err := s.repo.UpdateRunFieldsIfStatus(run.ID, claimableRunStatuses,
		map[string]any{
			"status":     model.JobRunning,
			"started_at": now,
			"log_path":   run.LogPath,
			"work_dir":   agentRoot,
		},
	)
	if err != nil {
		s.failRun(run, err)
		return
	}
	if n == 0 {
		s.finishIfCancelled(run, nil)
		return
	}

	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		s.failRun(run, err)
		return
	}
	defer logFile.Close()
	var logMu sync.Mutex
	writeLog := func(line string) {
		logMu.Lock()
		_, _ = logFile.WriteString(line + "\n")
		logMu.Unlock()
		if s.hub != nil {
			s.hub.BroadcastToChannel(fmt.Sprintf("ai-run:%d", run.ID), []byte(line))
		}
	}
	timeout := time.Duration(agent.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	// Workspace sync (git pull of bound repos) must not consume the CLI timeout.
	writeLog("正在同步工作区")
	digests, repoDirs, err := s.SyncAgentWorkspace(runCtx, agent, run.TriggeredBy, true)
	if err != nil {
		if s.finishIfCancelled(run, writeLog) {
			return
		}
		s.failRun(run, err)
		return
	}
	if len(digests) > 0 {
		b, _ := json.Marshal(digests)
		run.SkillDigestJSON = string(b)
	}

	absRoot, _ := filepath.Abs(agentRoot)
	outputDir, err := resolveAgentOutputDir(agentRoot, agent.OutputDir)
	if err != nil {
		s.failRun(run, err)
		return
	}
	if err := prepareAgentOutputDir(outputDir); err != nil {
		s.failRun(run, err)
		return
	}
	absOutput, _ := filepath.Abs(outputDir)

	// Harness session: compile (already synced above) → create session →
	// prompt (delivery=queue) → event-driven state machine.
	sessionID, err := s.ensureHarnessSession(runCtx, run, agent, run.TriggeredBy, writeLog)
	if err != nil {
		if s.finishIfCancelled(run, writeLog) {
			return
		}
		s.failRun(run, err)
		return
	}
	agentDef := harnessservice.AgentDefName(harnessAgentSpec(agent))
	writeHarnessRunIntro(writeLog, agent, run, agentDef, absRoot, absOutput,
		len(digests), len(repoDirs), timeout, approvalModeForRun(agent, run.TriggerType))

	hint := ""
	if agent.InjectDefaultPrompt {
		hint = agentWorkspaceScopeHint(absOutput)
	}
	var promptText string
	if run.TriggerType == model.TriggerDocsGen {
		promptText = "Generate API documentation based on the workspace. Output Markdown only."
		if agent.InjectDefaultPrompt {
			promptText += " " + hint
		}
	} else {
		promptText = composeRunPrompt(run.UserPrompt, hint)
	}

	outcome := s.runHarnessSession(runCtx, agent, run, sessionID, promptText, timeout, writeLog)
	if outcome.status == model.JobCancelled {
		// CancelRun already persisted the terminal status.
		return
	}

	finished := time.Now().UTC()
	run.FinishedAt = &finished
	if run.StartedAt != nil {
		run.DurationMs = finished.Sub(*run.StartedAt).Milliseconds()
	}
	run.FinalOutput = outcome.final
	run.OutputText = outcome.final
	run.Status = outcome.status
	run.ErrorMessage = outcome.errMsg
	if outcome.status == model.JobFailed {
		writeLog(outcome.errMsg)
		if !s.persistTerminal(run) {
			s.finishIfCancelled(run, writeLog)
			return
		}
		s.notifyTerminal(run, model.JobFailed)
		return
	}

	if outcome.status == model.JobSuccess {
		run.ErrorMessage = ""
		writeLog("执行成功")
	} else {
		writeLog(outcome.errMsg)
	}
	if err := s.archiveRunOutput(run, agent, absOutput, writeLog); err != nil {
		writeLog("制品归档失败: " + err.Error())
		if s.logger != nil {
			s.logger.Warn("agent run artifact archive failed",
				zap.Uint("run_id", run.ID), zap.Uint("agent_id", agent.ID), zap.Error(err))
		}
	}
	if !s.persistTerminal(run) {
		s.finishIfCancelled(run, writeLog)
		return
	}

	if run.TriggerType == model.TriggerDocsGen && run.Status == model.JobSuccess &&
		s.docs != nil && run.ProjectID != nil && run.DocNodeID != nil {
		content := strings.TrimSpace(run.FinalOutput)
		if content == "" {
			content = "# Generated Draft\n\n(empty session output)\n"
		}
		if err := s.docs.WriteDraftFromAgentRun(*run.ProjectID, *run.DocNodeID, run.ID, content, run.TriggeredBy); err != nil {
			writeLog("文档草稿写入失败: " + err.Error())
		} else {
			writeLog("文档内容已写入")
		}
	}
	s.notifyTerminal(run, run.Status)
}

// archiveRunOutput snapshots the agent fixed output_dir into a per-run zip.
// Empty directories skip archiving; failures are returned to the caller (run stays success).
func (s *AgentService) archiveRunOutput(run *model.AgentRun, agent *model.AiAgent, runOutput string, writeLog func(string)) error {
	hasFiles, err := dirHasRegularFiles(runOutput)
	if err != nil {
		return err
	}
	if !hasFiles {
		writeLog("产出目录为空，跳过制品归档")
		return nil
	}
	dir := filepath.Join(s.artifactDir, fmt.Sprintf("agent-%d", agent.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	artifactPath := filepath.Join(dir, fmt.Sprintf("run-%d.zip", run.ID))
	if err := engine.CreateArtifactArchive(artifactPath, runOutput, "zip"); err != nil {
		_ = os.Remove(artifactPath)
		return err
	}
	run.ArtifactPath = artifactPath
	run.ArtifactKind = "archive"
	writeLog("制品已归档: " + artifactPath)
	return nil
}

func (s *AgentService) removeAgentArtifacts(agentID uint) {
	if strings.TrimSpace(s.artifactDir) == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.artifactDir, fmt.Sprintf("agent-%d", agentID)))
}

func (s *AgentService) failRun(run *model.AgentRun, err error) {
	if s.finishIfCancelled(run, nil) {
		return
	}
	finished := time.Now().UTC()
	run.Status = model.JobFailed
	run.FinishedAt = &finished
	run.ErrorMessage = err.Error()
	if run.StartedAt != nil {
		run.DurationMs = finished.Sub(*run.StartedAt).Milliseconds()
	}
	if !s.persistTerminal(run) {
		s.finishIfCancelled(run, nil)
		return
	}
	if run.LogPath != "" {
		f, openErr := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if openErr == nil {
			_, _ = f.WriteString("error: " + err.Error() + "\n")
			_ = f.Close()
		}
	}
	s.notifyTerminal(run, model.JobFailed)
}

func (s *AgentService) notifyTerminal(run *model.AgentRun, status string) {
	if run == nil {
		return
	}
	if s.mailer != nil {
		agentCreatedBy := uint(0)
		if agent, err := s.repo.FindAgent(run.AgentID); err == nil && agent != nil {
			agentCreatedBy = agent.CreatedBy
		}
		s.mailer.DispatchAgentRunFailure(run.TriggeredBy, agentCreatedBy, run.ID, status, run.ErrorMessage)
	}
	if s.termHook != nil {
		s.termHook.OnAgentRunTerminal(run, status)
	}
	if s.notifier != nil && run.TriggeredBy != 0 {
		s.notifier.NotifyAgentRun(run.TriggeredBy, run.ID, run.AgentID, status)
	} else if s.hub != nil && run.TriggeredBy != 0 {
		payload, _ := json.Marshal(map[string]any{
			"type": "agent_run_" + status, "agent_run_id": run.ID, "agent_id": run.AgentID, "status": status,
		})
		s.hub.BroadcastToChannel(fmt.Sprintf("notifications:%d", run.TriggeredBy), payload)
	}
	if s.hub != nil {
		s.hub.BroadcastToChannel(fmt.Sprintf("ai-run:%d", run.ID), []byte("__TERMINAL__:"+status))
	}
}

func (s *AgentService) ReloadCron() error {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	return s.reloadCronLocked()
}

// CronEntries returns a snapshot of scheduled cron entries (for tests).
func (s *AgentService) CronEntries() []cron.Entry {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.cron == nil {
		return nil
	}
	return s.cron.Entries()
}

func (s *AgentService) reloadCronLocked() error {
	if s.cron == nil {
		return nil
	}
	for id, entry := range s.cronIDs {
		s.cron.Remove(entry)
		delete(s.cronIDs, id)
	}
	triggers, err := s.repo.ListCronTriggers()
	if err != nil {
		return err
	}
	for _, t := range triggers {
		trigger := t
		loc, err := time.LoadLocation(stringOr(trigger.CronTimezone, "UTC"))
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("invalid agent cron timezone", zap.Uint("trigger_id", trigger.ID), zap.Error(err))
			}
			continue
		}
		schedule, err := cronParser.Parse(trigger.CronExpression)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("invalid agent cron", zap.Uint("trigger_id", trigger.ID), zap.Error(err))
			}
			continue
		}
		entryID := s.cron.Schedule(locSchedule{inner: schedule, loc: loc}, cron.FuncJob(func() {
			s.fireCron(trigger)
		}))
		s.cronIDs[trigger.ID] = entryID
	}
	return nil
}

func (s *AgentService) fireCron(t model.AgentTrigger) {
	// No overlap: skip if agent already has active run. Missed ticks during downtime are not backfilled.
	n, err := s.repo.CountActiveRuns(t.AgentID)
	if err != nil || n > 0 {
		return
	}
	_, _ = s.CreateRun(t.AgentID, CreateRunInput{
		TriggerType: model.TriggerCron, TriggerID: &t.ID, TriggeredBy: 0,
	})
}

func validateTrigger(t *model.AgentTrigger) error {
	switch t.Type {
	case model.TriggerManual, model.TriggerAPI:
		return nil
	case model.TriggerCron:
		if t.CronExpression == "" {
			return errors.New("cron_expression 不能为空")
		}
		if _, err := time.LoadLocation(stringOr(t.CronTimezone, "UTC")); err != nil {
			return errors.New("无效 IANA 时区")
		}
		if _, err := cronParser.Parse(t.CronExpression); err != nil {
			return errors.New("无效 cron 表达式")
		}
		return nil
	case model.TriggerBuildEvent:
		if t.BuildJobID == nil {
			return errors.New("build_job_id 不能为空")
		}
		ev := t.BuildEvent
		if ev == "" {
			ev = model.EventArtifactReady
			t.BuildEvent = ev
		}
		if ev != model.EventArtifactReady && ev != model.EventDistributionFinished {
			return errors.New("build_event 必须为 artifact_ready 或 distribution_finished")
		}
		return nil
	default:
		return errors.New("不支持的触发器类型")
	}
}

func encodeSkillIDs(agent *model.AiAgent, ids []uint) error {
	if ids == nil {
		ids = []uint{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	agent.SkillIDsJSON = string(b)
	agent.SkillIDs = ids
	return nil
}

func decodeSkillIDs(agent *model.AiAgent) {
	if agent.SkillIDsJSON == "" {
		agent.SkillIDs = []uint{}
		return
	}
	_ = json.Unmarshal([]byte(agent.SkillIDsJSON), &agent.SkillIDs)
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func stringOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}
