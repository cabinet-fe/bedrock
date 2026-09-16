package service

// Agent-run execution over the harness session backend: session creation
// (one session per run), prompt submission (delivery=queue, unchanged
// assembly rules), the event-driven run state machine and terminal
// write-back. This replaced the former per-CLI subprocess path.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bedrock/internal/ai/model"
	"bedrock/internal/harness/provider"
	harnessservice "bedrock/internal/harness/service"
)

// ErrHarnessDisabled reports that run execution is unavailable because the
// harness session backend is not configured. There is no CLI fallback.
var ErrHarnessDisabled = errors.New("会话底座未启用，无法执行智能体运行")

// Run state-machine timers. idleConfirm is the quiet window after an idle
// frame before reconciling against the harness; noTerminalFallback is the
// prompt-quiet window before the reconcile fallback arms the message-tail
// check (see harness-integration-plan §4.3).
const (
	defaultIdleConfirm        = 3 * time.Second
	defaultNoTerminalFallback = 60 * time.Second
	interruptTimeout          = 10 * time.Second
)

// SetHarnessBackend wires the harness session backend used by run
// execution. Without it CreateRun fails with ErrHarnessDisabled.
func (s *AgentService) SetHarnessBackend(
	sessions *harnessservice.SessionService,
	streams *harnessservice.StreamService,
	prov provider.Provider,
) {
	s.harnessSessions = sessions
	s.harnessStreams = streams
	s.harnessProvider = prov
}

// HarnessEnabled reports whether run execution is available.
func (s *AgentService) HarnessEnabled() bool {
	return s.harnessSessions != nil && s.harnessStreams != nil && s.harnessProvider != nil
}

// SetHarnessTimers overrides the run state-machine timers (tests).
func (s *AgentService) SetHarnessTimers(idleConfirm, noTerminal time.Duration) {
	if idleConfirm > 0 {
		s.harnessIdleConfirm = idleConfirm
	}
	if noTerminal > 0 {
		s.harnessNoTerminal = noTerminal
	}
}

// harnessAgentSpec projects the persisted agent onto the harness domain.
func harnessAgentSpec(agent *model.AiAgent) harnessservice.AgentSpec {
	return harnessservice.AgentSpec{
		ID:            agent.ID,
		Name:          agent.Name,
		Description:   agent.Description,
		SystemPrompt:  agent.SystemPrompt,
		SkillIDs:      agent.SkillIDs,
		ModelProvider: agent.ModelProvider,
		ModelID:       agent.ModelID,
		ApprovalMode:  agent.ApprovalMode,
	}
}

// approvalModeForRun resolves the bridge approval mode. Attended triggers
// (manual/api) inherit the agent configuration (default manual, answered in
// the run-detail UI); unattended triggers (cron, docs generation, pipeline,
// build events) run auto so nothing blocks on a missing answer.
func approvalModeForRun(agent *model.AiAgent, trigger string) string {
	switch trigger {
	case model.TriggerCron, model.TriggerDocsGen, model.TriggerPipeline, model.TriggerBuildEvent:
		return harnessservice.ApprovalAuto
	}
	if strings.TrimSpace(agent.ApprovalMode) == harnessservice.ApprovalAuto {
		return harnessservice.ApprovalAuto
	}
	return harnessservice.ApprovalManual
}

// harnessOutcome is the settled terminal state of a harness-backed run.
type harnessOutcome struct {
	status string // success | failed | interrupted | cancelled
	errMsg string
	final  string
}

// ensureHarnessSession returns the run's harness session, creating and
// persisting it on first execution. One run maps to exactly one session:
// retries (e.g. submit racing recovery) reuse the persisted id.
func (s *AgentService) ensureHarnessSession(ctx context.Context, run *model.AgentRun, agent *model.AiAgent, userID uint, writeLog func(string)) (string, error) {
	if run.HarnessSessionID != nil && strings.TrimSpace(*run.HarnessSessionID) != "" {
		return *run.HarnessSessionID, nil
	}
	session, err := s.harnessSessions.CreateAgentSession(ctx, harnessAgentSpec(agent), userID)
	if err != nil {
		return "", fmt.Errorf("创建会话失败: %w", err)
	}
	sessionID := session.ID
	run.HarnessSessionID = &sessionID
	n, err := s.repo.UpdateRunFieldsIfStatus(run.ID, liveRunStatuses, map[string]any{
		"harness_session_id":     sessionID,
		"harness_session_status": model.JobRunning,
	})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", errors.New("运行已进入终态，取消建会话")
	}
	writeLog("会话已创建: " + sessionID)
	return sessionID, nil
}

// frameWatch folds live session frames into run state (last assistant text)
// and log lines. It reports a settled failure when a frame carries a
// session error.
type frameWatch struct {
	lastText string
}

func (w *frameWatch) handle(frame *provider.Frame, writeLog func(string)) (harnessOutcome, bool) {
	switch frame.Kind {
	case provider.FrameStatus:
		if frame.Status == nil {
			return harnessOutcome{}, false
		}
		if frame.Status.Name == provider.StatusError {
			msg := "会话执行出错"
			if frame.Status.Error != "" {
				msg += ": " + frame.Status.Error
			}
			writeLog(msg)
			return harnessOutcome{status: model.JobFailed, errMsg: msg}, true
		}
		writeLog("会话状态: " + string(frame.Status.Name))
	case provider.FrameMessageText:
		if frame.MessageText != nil {
			w.lastText = frame.MessageText.Text
			writeLog(truncLogLine(frame.MessageText.Text))
		}
	case provider.FrameToolCall:
		if frame.ToolCall != nil {
			writeLog("工具调用: " + frame.ToolCall.Tool)
		}
	case provider.FrameToolResult:
		if frame.ToolResult != nil && frame.ToolResult.Error != "" {
			writeLog("工具失败: " + frame.ToolResult.Tool + ": " + truncLogLine(frame.ToolResult.Error))
		}
	case provider.FramePermission:
		if frame.Permission != nil {
			writeLog("等待工具审批: " + frame.Permission.Action + " (" + frame.Permission.RequestID + ")")
		}
	case provider.FrameQuestion:
		if frame.Question != nil {
			writeLog("会话提问: " + frame.Question.RequestID)
		}
	}
	return harnessOutcome{}, false
}

// truncLogLine bounds one log line to keep run logs readable.
func truncLogLine(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(截断)"
}

// runHarnessSession drives one run's session to a terminal state: it
// registers the approval mode, subscribes to the stream bridge, submits the
// prompt (delivery=queue) and waits on events, the backend wait call, the
// run timeout and the no-terminal fallback.
func (s *AgentService) runHarnessSession(
	runCtx context.Context,
	agent *model.AiAgent,
	run *model.AgentRun,
	sessionID, promptText string,
	timeout time.Duration,
	writeLog func(string),
) harnessOutcome {
	mode := approvalModeForRun(agent, run.TriggerType)
	s.harnessStreams.SetApprovalMode(sessionID, mode)

	replay, live, stop := s.harnessStreams.Subscribe(sessionID)
	defer stop()
	watch := &frameWatch{}
	for i := range replay {
		if out, settled := watch.handle(&replay[i], writeLog); settled {
			return out
		}
	}

	if _, err := s.harnessProvider.Prompt(runCtx, sessionID, provider.PromptInput{
		Text: promptText, Delivery: provider.DeliveryQueue,
	}); err != nil {
		msg := fmt.Sprintf("会话提示词被拒: %v", err)
		writeLog(msg)
		return harnessOutcome{status: model.JobFailed, errMsg: msg}
	}
	writeLog("会话提示词已提交（delivery=queue）")

	idleConfirm, noTerminal := s.harnessIdleConfirm, s.harnessNoTerminal
	if idleConfirm <= 0 {
		idleConfirm = defaultIdleConfirm
	}
	if noTerminal <= 0 {
		noTerminal = defaultNoTerminalFallback
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- s.harnessProvider.Wait(runCtx, sessionID) }()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	idleTimer := time.NewTimer(time.Hour)
	idleTimer.Stop()
	defer idleTimer.Stop()
	fallbackTimer := time.NewTimer(noTerminal)
	defer fallbackTimer.Stop()

	reconcile := func() (harnessOutcome, bool) {
		return s.reconcileHarnessRun(runCtx, sessionID, watch, writeLog)
	}

	for {
		select {
		case <-runCtx.Done():
			return s.settleContextDone(run, sessionID, writeLog)

		case <-timeoutTimer.C:
			s.interruptSession(sessionID, writeLog)
			return harnessOutcome{status: model.JobInterrupted, errMsg: "执行超时，已打断会话"}

		case frame, ok := <-live:
			if !ok {
				// Bridge stopped (shutdown); runCtx.Done follows shortly.
				live = nil
				continue
			}
			if out, settled := watch.handle(frame, writeLog); settled {
				return out
			}
			fallbackTimer.Reset(noTerminal)
			if frame.Kind == provider.FrameStatus && frame.Status != nil &&
				frame.Status.Name == provider.StatusIdle {
				idleTimer.Reset(idleConfirm)
			}

		case <-idleTimer.C:
			if out, ok := reconcile(); ok {
				return out
			}

		case waitErr := <-waitCh:
			waitCh = nil // wait answers once; frames and fallback drive on
			switch {
			case errors.Is(waitErr, provider.ErrWaitUnavailable):
				// Backend cannot serve wait; event-driven detection applies.
			case errors.Is(waitErr, context.Canceled):
				return s.settleContextDone(run, sessionID, writeLog)
			case waitErr != nil:
				// Possible serve crash: a consistent History read still
				// settles success; a failing one means harness-unavailable.
				if out, ok := reconcile(); ok {
					return out
				}
				msg := "harness-unavailable: " + waitErr.Error()
				writeLog(msg)
				return harnessOutcome{status: model.JobFailed, errMsg: msg}
			default:
				if out, ok := reconcile(); ok {
					return out
				}
				// Idle without assistant output (prompt not picked up yet):
				// keep waiting on frames and the fallback timer.
			}

		case <-fallbackTimer.C:
			writeLog("长时间无终态，做会话消息尾页核对")
			if out, ok := reconcile(); ok {
				return out
			}
			if len(s.harnessStreams.PendingRequests(sessionID)) > 0 {
				writeLog("仍有待应答的审批/提问，继续等待")
				fallbackTimer.Reset(noTerminal)
				continue
			}
			if s.sessionStillActive(runCtx, sessionID) {
				writeLog("会话仍在执行，继续等待")
				fallbackTimer.Reset(noTerminal)
				continue
			}
			s.interruptSession(sessionID, writeLog)
			return harnessOutcome{status: model.JobInterrupted, errMsg: "长时间无终态，已打断会话"}
		}
	}
}

// sessionStillActive reports whether the backend still runs the session's
// agent loop. A failed probe answers false: the fallback semantics (treat
// as no active session) stay unchanged when the backend is unreachable.
func (s *AgentService) sessionStillActive(ctx context.Context, sessionID string) bool {
	active, err := s.harnessProvider.ActiveSessions(ctx)
	return err == nil && active[sessionID]
}

// reconcileHarnessRun consults the harness for a terminal decision: a
// session the backend still reports running is never settled (its message
// tail would be a mid-turn partial), pending asks leave the run open, and
// the message tail page settles success with the last assistant text as
// final_output. ok=false leaves the run open.
func (s *AgentService) reconcileHarnessRun(ctx context.Context, sessionID string, watch *frameWatch, writeLog func(string)) (harnessOutcome, bool) {
	if s.sessionStillActive(ctx, sessionID) {
		return harnessOutcome{}, false
	}
	if len(s.harnessStreams.PendingRequests(sessionID)) > 0 {
		return harnessOutcome{}, false
	}
	messages, err := s.harnessProvider.History(ctx, sessionID)
	if err != nil {
		writeLog("会话消息核对失败: " + err.Error())
		return harnessOutcome{}, false
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		final := strings.TrimSpace(messages[i].Text())
		if final == "" {
			final = watch.lastText
		}
		return harnessOutcome{status: model.JobSuccess, final: final}, true
	}
	return harnessOutcome{}, false
}

// settleContextDone resolves an aborted context: a user cancel already
// wrote the terminal status (CancelRun); anything else (service shutdown)
// interrupts the session and records interrupted.
func (s *AgentService) settleContextDone(run *model.AgentRun, sessionID string, writeLog func(string)) harnessOutcome {
	if s.finishIfCancelled(run, writeLog) {
		s.interruptSession(sessionID, writeLog)
		return harnessOutcome{status: model.JobCancelled}
	}
	writeLog("服务关闭，打断会话")
	s.interruptSession(sessionID, writeLog)
	return harnessOutcome{status: model.JobInterrupted, errMsg: "服务关闭，执行已打断"}
}

// interruptSession asks the backend to cancel the running session loop (the
// session-service path also opens the bridge settle window, since an aborted
// loop may emit no further frames). It uses its own bounded context: callers
// invoke it while unwinding.
func (s *AgentService) interruptSession(sessionID string, writeLog func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), interruptTimeout)
	defer cancel()
	if err := s.harnessSessions.Interrupt(ctx, sessionID); err != nil {
		writeLog("打断会话失败: " + err.Error())
		return
	}
	writeLog("已请求打断会话")
}

// writeHarnessRunIntro logs the run header (session backend flavor).
func writeHarnessRunIntro(writeLog func(string), agent *model.AiAgent, run *model.AgentRun, agentDef, absRoot, absOutput string, skillCount, repoCount int, timeout time.Duration, mode string) {
	writeLog(fmt.Sprintf("智能体「%s」开始执行（%s，%s）", agent.Name, agentDef, triggerLabel(run.TriggerType)))
	writeLog("工作目录: " + absRoot)
	writeLog("产出目录: " + absOutput)
	writeLog(fmt.Sprintf("Skill %d 个，仓库 %d 个，超时 %d 秒，审批模式 %s", skillCount, repoCount, int(timeout.Seconds()), mode))
}

// triggerLabel renders a trigger type for run logs.
func triggerLabel(v string) string {
	switch v {
	case model.TriggerManual:
		return "手动"
	case model.TriggerAPI:
		return "API"
	case model.TriggerCron:
		return "定时"
	case model.TriggerBuildEvent:
		return "构建事件"
	case model.TriggerDocsGen:
		return "文档生成"
	case model.TriggerPipeline:
		return "流水线"
	default:
		if strings.TrimSpace(v) == "" {
			return "未知"
		}
		return v
	}
}
