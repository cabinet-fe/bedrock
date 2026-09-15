package service_test

// AgentRun ↔ harness session state-machine matrix: every trigger type ×
// every terminal scenario asserts the persisted run terminal state, the
// session binding (one run ↔ one session), the interrupt path for
// cancel/timeout and the approval-mode wiring (attended manual vs
// unattended auto).

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	"bedrock/internal/ai/service"
	"bedrock/internal/harness/harnesstest"
	"bedrock/internal/harness/provider"
	harnessservice "bedrock/internal/harness/service"
)

// matrixEnv is one matrix cell's freshly wired service stack.
type matrixEnv struct {
	agents  *service.AgentService
	fake    *harnesstest.Fake
	streams *harnessservice.StreamService
}

// newMatrixEnv builds an async-worker agent service on a fake harness.
// pendingTTL configures the bridge pending-request TTL (0 = default).
func newMatrixEnv(t *testing.T, timeoutSec int, pendingTTL time.Duration) *matrixEnv {
	t.Helper()
	root := t.TempDir()
	gdb := openAITestDB(t)
	repo := repository.NewAIRepository(gdb)
	work := filepath.Join(root, "work")
	agents := service.NewAgentService(repo, nil, nil, zap.NewNop(), work,
		filepath.Join(root, "artifacts"), filepath.Join(root, "logs"))
	fake := harnesstest.New()
	sessions := harnessservice.NewSessionService(fake, harnessservice.SessionConfig{
		WorkspaceRoot: work,
	}, nil)
	streams := harnessservice.NewStreamService(fake, harnessservice.StreamConfig{
		ApprovalMode:  harnessservice.ApprovalManual,
		PendingTTL:    pendingTTL,
		SweepInterval: pendingTTL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go streams.Run(ctx)
	agents.SetHarnessBackend(sessions, streams, fake)
	agents.SetHarnessTimers(150*time.Millisecond, 2*time.Second)
	agents.SetSyncWorkspaceInit(true)
	agents.SetInlineExec(false)
	agents.Start()
	t.Cleanup(agents.Shutdown)

	agent, err := agents.CreateAgent(1, service.AgentInput{
		Name: "matrix", SystemPrompt: "sp", TimeoutSec: timeoutSec,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireWorkspaceReady(t, agents, agent.ID)
	return &matrixEnv{agents: agents, fake: fake, streams: streams}
}

// waitRunSession blocks until the run bound a harness session.
func (m *matrixEnv) waitRunSession(t *testing.T, runID uint) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := m.agents.GetRun(runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.HarnessSessionID != nil && *run.HarnessSessionID != "" {
			return *run.HarnessSessionID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d never bound a harness session", runID)
	return ""
}

// waitPendingRequest blocks until the bridge holds a pending request of the
// session and returns its request id.
func (m *matrixEnv) waitPendingRequest(t *testing.T, sessionID string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pending := m.streams.PendingRequests(sessionID)
		if len(pending) > 0 {
			return pending[0].RequestID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session %s never reported a pending request", sessionID)
	return ""
}

func TestAgentRunStateMachineMatrix(t *testing.T) {
	triggers := []struct {
		name     string
		attended bool
		create   func(*service.AgentService, uint) (*model.AgentRun, error)
	}{
		{"manual", true, func(a *service.AgentService, id uint) (*model.AgentRun, error) {
			return a.ManualRun(id, 1, "")
		}},
		{"cron", false, func(a *service.AgentService, id uint) (*model.AgentRun, error) {
			return a.CreateRun(id, service.CreateRunInput{TriggerType: model.TriggerCron, TriggeredBy: 0})
		}},
		{"docs", false, func(a *service.AgentService, id uint) (*model.AgentRun, error) {
			return a.CreateRun(id, service.CreateRunInput{TriggerType: model.TriggerDocsGen, TriggeredBy: 1})
		}},
		{"pipeline", false, func(a *service.AgentService, id uint) (*model.AgentRun, error) {
			return a.CreateRun(id, service.CreateRunInput{TriggerType: model.TriggerPipeline, TriggeredBy: 1})
		}},
		{"build_event", false, func(a *service.AgentService, id uint) (*model.AgentRun, error) {
			return a.CreateRun(id, service.CreateRunInput{TriggerType: model.TriggerBuildEvent, TriggeredBy: 1})
		}},
	}

	scenarios := []struct {
		name       string
		timeoutSec int
		pendingTTL time.Duration
		setup      func(t *testing.T, m *matrixEnv)
		want       string
		wantErr    string
		assert     func(t *testing.T, m *matrixEnv, run *model.AgentRun)
	}{
		{
			name:       "正常",
			timeoutSec: 30,
			want:       model.JobSuccess,
			assert: func(t *testing.T, m *matrixEnv, run *model.AgentRun) {
				if strings.TrimSpace(run.FinalOutput) == "" {
					t.Fatalf("final_output empty: %+v", run)
				}
				if run.OutputText != run.FinalOutput {
					t.Fatalf("output_text=%q final_output=%q want mirror", run.OutputText, run.FinalOutput)
				}
				if got := m.fake.Delivery(*run.HarnessSessionID); got != provider.DeliveryQueue {
					t.Fatalf("delivery=%q want queue", got)
				}
				if n := len(m.fake.Sessions()); n != 1 {
					t.Fatalf("sessions=%d want 1 (one run ↔ one session)", n)
				}
			},
		},
		{
			name:       "拒绝审批",
			timeoutSec: 30,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					_ = f.Emit(provider.Frame{
						SessionID: sess.ID, Kind: provider.FramePermission,
						Permission: &provider.PermissionFrame{RequestID: "perm-1", Action: "bash"},
					})
					f.AwaitReply("perm-1")
					f.Complete(sess.ID, "done after review")
				})
			},
			want: model.JobSuccess,
		},
		{
			name:       "提问",
			timeoutSec: 30,
			pendingTTL: 100 * time.Millisecond,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					_ = f.Emit(provider.Frame{
						SessionID: sess.ID, Kind: provider.FrameQuestion,
						Question: &provider.QuestionFrame{RequestID: "q-1", Questions: []provider.Question{{Question: "continue?"}}},
					})
					// Unanswered questions only get recorded; the session
					// continues once the bridge TTL dismisses the ask.
					time.Sleep(150 * time.Millisecond)
					f.Complete(sess.ID, "recorded and continued")
				})
			},
			want: model.JobSuccess,
		},
		{
			name:       "超时",
			timeoutSec: 1,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					_ = f.Emit(provider.Frame{
						SessionID: sess.ID, Kind: provider.FrameStatus,
						Status: &provider.StatusFrame{Name: provider.StatusStepStarted},
					})
					// never completes: the run timeout must interrupt
				})
			},
			want:    model.JobInterrupted,
			wantErr: "超时",
			assert: func(t *testing.T, m *matrixEnv, run *model.AgentRun) {
				if n := m.fake.Interrupts(*run.HarnessSessionID); n != 1 {
					t.Fatalf("interrupts=%d want 1", n)
				}
			},
		},
		{
			name:       "用户取消",
			timeoutSec: 30,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					_ = f.Emit(provider.Frame{
						SessionID: sess.ID, Kind: provider.FrameStatus,
						Status: &provider.StatusFrame{Name: provider.StatusStepStarted},
					})
					// hangs until the user cancels
				})
			},
			want: model.JobCancelled,
			assert: func(t *testing.T, m *matrixEnv, run *model.AgentRun) {
				// CancelRun persists the terminal status first; the loop's
				// interrupt lands right after — poll for it.
				sessionID := *run.HarnessSessionID
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) && m.fake.Interrupts(sessionID) < 1 {
					time.Sleep(5 * time.Millisecond)
				}
				if n := m.fake.Interrupts(sessionID); n < 1 {
					t.Fatalf("interrupts=%d want >=1 log=%s", n, readRunLog(t, run.LogPath))
				}
			},
		},
		{
			name:       "serve崩溃",
			timeoutSec: 30,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					f.Crash(errors.New("serve exited unexpectedly"))
				})
			},
			want:    model.JobFailed,
			wantErr: "harness-unavailable",
		},
		{
			name:       "模型不可用",
			timeoutSec: 30,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetScript(func(f *harnesstest.Fake, sess *provider.Session, _ string) {
					f.FailSession(sess.ID, "model not available")
				})
			},
			want:    model.JobFailed,
			wantErr: "model not available",
		},
		{
			name:       "prompt被拒",
			timeoutSec: 30,
			setup: func(t *testing.T, m *matrixEnv) {
				m.fake.SetPromptError(errors.New("prompt rejected"))
			},
			want:    model.JobFailed,
			wantErr: "会话提示词被拒",
		},
	}

	for _, trig := range triggers {
		for _, sc := range scenarios {
			t.Run(trig.name+"/"+sc.name, func(t *testing.T) {
				m := newMatrixEnv(t, sc.timeoutSec, sc.pendingTTL)
				if sc.setup != nil {
					sc.setup(t, m)
				}

				run, err := trig.create(m.agents, m.testAgentID(t))
				if err != nil {
					t.Fatal(err)
				}
				sessionID := m.waitRunSession(t, run.ID)

				// Trigger-specific interaction: attended approvals are
				// answered by the "frontend"; unattended ones by the bridge.
				switch sc.name {
				case "拒绝审批":
					if trig.attended {
						reqID := m.waitPendingRequest(t, sessionID)
						if err := m.streams.ReplyPermission(context.Background(), sessionID, reqID, provider.PermissionReject); err != nil {
							t.Fatalf("reply reject: %v", err)
						}
					}
				case "提问":
					if trig.attended {
						reqID := m.waitPendingRequest(t, sessionID)
						if err := m.streams.ReplyQuestion(context.Background(), sessionID, reqID, provider.QuestionAnswers{
							Answers: [][]string{{"yes"}},
						}); err != nil {
							t.Fatalf("reply question: %v", err)
						}
					}
				case "用户取消":
					// Cancel only after the prompt was admitted: the loop
					// is live and must settle via interrupt.
					waitRunPrompt(t, m.agents, m.fake, run.ID)
					if err := m.agents.CancelRun(run.ID); err != nil {
						t.Fatalf("cancel: %v", err)
					}
				}
				finished := waitRunStatus(t, m.agents, run.ID, sc.want)
				if sc.wantErr != "" && !strings.Contains(finished.ErrorMessage, sc.wantErr) {
					t.Fatalf("error_message=%q want contains %q log=%s", finished.ErrorMessage, sc.wantErr, readRunLog(t, finished.LogPath))
				}
				if finished.HarnessSessionID == nil || *finished.HarnessSessionID != sessionID {
					t.Fatalf("harness_session_id=%v want %q", finished.HarnessSessionID, sessionID)
				}
				if sc.assert != nil {
					sc.assert(t, m, finished)
				}

				// Approval wiring: unattended triggers auto-approve
				// (recorded "once"), attended triggers record the human
				// reply ("reject" / "question").
				switch sc.name {
				case "拒绝审批":
					wantReply := provider.PermissionOnce
					if trig.attended {
						wantReply = provider.PermissionReject
					}
					deadline := time.Now().Add(5 * time.Second)
					for time.Now().Before(deadline) && m.fake.Reply("perm-1") == "" {
						time.Sleep(5 * time.Millisecond)
					}
					if got := m.fake.Reply("perm-1"); got != string(wantReply) {
						t.Fatalf("permission reply=%q want %q", got, wantReply)
					}
				case "提问":
					if trig.attended && m.fake.Reply("q-1") != "question" {
						t.Fatalf("question reply=%q want question", m.fake.Reply("q-1"))
					}
				}
			})
		}
	}
}

// testAgentID returns the matrix env's single agent id.
func (m *matrixEnv) testAgentID(t *testing.T) uint {
	t.Helper()
	items, _, err := m.agents.ListAgents(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("matrix env has no agent")
	}
	return items[0].ID
}

// One run re-execution must reuse the persisted session (idempotency).
func TestExecuteRunReusesHarnessSession(t *testing.T) {
	m := newMatrixEnv(t, 30, 0)
	agentID := m.testAgentID(t)
	run, err := m.agents.ManualRun(agentID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitRunStatus(t, m.agents, run.ID, model.JobSuccess)
	if n := len(m.fake.Sessions()); n != 1 {
		t.Fatalf("sessions=%d want 1", n)
	}
	// Re-submitting the terminal run is a no-op (no second session).
	m.agents.ExecuteRun(context.Background(), finished.ID)
	if n := len(m.fake.Sessions()); n != 1 {
		t.Fatalf("sessions=%d want 1 after re-execute", n)
	}
}
