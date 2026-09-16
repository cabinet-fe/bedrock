// Package harnesstest provides a scriptable fake harness Provider for tests
// of harness consumers (ai agent runs, handlers, wiring). The fake owns a
// bus channel carrying unified frames; prompts run a configurable script in
// their own goroutine, which emits frames and completes sessions.
package harnesstest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"bedrock/internal/harness/provider"
)

// Fake is a scriptable provider.Provider implementation. It is safe for
// concurrent use.
type Fake struct {
	mu sync.Mutex

	onPrompt  func(*Fake, *provider.Session, string)
	promptErr error
	failCalls error // non-nil: every provider call fails (serve down)

	nextID    int
	seq       int64
	events    int64
	sessions  []provider.Session
	archived  map[string]*time.Time
	prompts   map[string][]string
	delivery  map[string]provider.Delivery
	history   map[string][]provider.Message
	waitDone  map[string]chan struct{}
	waitErr   map[string]error
	interrupt map[string]int
	replies   map[string]string
	replySeen map[string]chan struct{}
	// active is the ActiveSessions answer; nil means "nothing is running".
	active map[string]bool

	models []provider.ModelInfo
	agents []provider.AgentInfo

	bus chan *provider.Frame
}

var _ provider.Provider = (*Fake)(nil)

// New builds a fake whose prompt script completes the session with a short
// assistant text. Customize with SetScript / SetPromptError / Crash.
func New() *Fake {
	return &Fake{
		archived:  map[string]*time.Time{},
		prompts:   map[string][]string{},
		delivery:  map[string]provider.Delivery{},
		history:   map[string][]provider.Message{},
		waitDone:  map[string]chan struct{}{},
		waitErr:   map[string]error{},
		interrupt: map[string]int{},
		replies:   map[string]string{},
		replySeen: map[string]chan struct{}{},
		bus:       make(chan *provider.Frame, 1024),
	}
}

// SetScript replaces the per-prompt script (nil restores the default
// success script). The script runs in its own goroutine per prompt.
func (f *Fake) SetScript(fn func(*Fake, *provider.Session, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onPrompt = fn
}

// SetPromptError makes the next Prompt calls fail (prompt rejected).
func (f *Fake) SetPromptError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promptErr = err
}

// Crash makes every provider call fail with err (serve unavailable). A nil
// err defaults to a connection-refused style error.
func (f *Fake) Crash(err error) {
	if err == nil {
		err = fmt.Errorf("fake serve: connection refused")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCalls = err
}

// SetModels / SetAgents configure the catalog endpoints.
func (f *Fake) SetModels(models []provider.ModelInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.models = models
}

func (f *Fake) SetAgents(agents []provider.AgentInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents = agents
}

// SetActiveSessions pins the ActiveSessions answer (id -> busy). Call with
// an empty/nil map to report everything idle again.
func (f *Fake) SetActiveSessions(active map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = active
}

// Emit sends one frame on the bus, assigning a durable seq or a transient
// event id when the caller set neither.
func (f *Fake) Emit(frame provider.Frame) error {
	f.mu.Lock()
	if frame.Seq == 0 && frame.EventID == "" {
		switch frame.Kind {
		case provider.FrameStatus, provider.FrameMessageText,
			provider.FrameToolCall, provider.FrameToolResult:
			f.seq++
			frame.Seq = f.seq
		default:
			f.events++
			frame.EventID = fmt.Sprintf("evt-%d", f.events)
		}
	}
	f.mu.Unlock()
	select {
	case f.bus <- &frame:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("harnesstest: bus send timed out (no BusStream consumer?)")
	}
}

// Complete records the final assistant text in the session history and
// releases Wait. Terminal for the default script and most scenarios.
func (f *Fake) Complete(sessionID, text string) {
	f.mu.Lock()
	content, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
	f.history[sessionID] = append(f.history[sessionID], provider.Message{
		ID:      fmt.Sprintf("msg-%s-%d", sessionID, len(f.history[sessionID])+1),
		Role:    "assistant",
		Content: content,
	})
	ch := f.takeWaitDoneLocked(sessionID)
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// FailSession emits a session-error status frame and releases Wait.
func (f *Fake) FailSession(sessionID, message string) {
	_ = f.Emit(provider.Frame{
		SessionID: sessionID,
		Kind:      provider.FrameStatus,
		Status:    &provider.StatusFrame{Name: provider.StatusError, Error: message},
	})
	f.mu.Lock()
	ch := f.takeWaitDoneLocked(sessionID)
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// AwaitReply blocks until the request (permission or question) received a
// reply, returning the recorded reply value.
func (f *Fake) AwaitReply(requestID string) string {
	f.mu.Lock()
	ch := f.replySeen[requestID]
	f.mu.Unlock()
	if ch == nil {
		return ""
	}
	<-ch
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replies[requestID]
}

// Sessions returns the created sessions in order.
func (f *Fake) Sessions() []provider.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.Session, len(f.sessions))
	copy(out, f.sessions)
	return out
}

// Prompts returns the prompt texts sent to a session.
func (f *Fake) Prompts(sessionID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.prompts[sessionID]))
	copy(out, f.prompts[sessionID])
	return out
}

// Interrupts returns how many times a session was interrupted.
func (f *Fake) Interrupts(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.interrupt[sessionID]
}

// Delivery returns the delivery mode of the session's last prompt.
func (f *Fake) Delivery(sessionID string) provider.Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delivery[sessionID]
}

// Reply returns the recorded reply for a request id.
func (f *Fake) Reply(requestID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replies[requestID]
}

func (f *Fake) takeWaitDoneLocked(sessionID string) chan struct{} {
	ch, ok := f.waitDone[sessionID]
	if !ok {
		return nil
	}
	delete(f.waitDone, sessionID)
	return ch
}

func (f *Fake) failErr() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failCalls
}

func (f *Fake) signalReply(requestID, reply string) {
	f.mu.Lock()
	f.replies[requestID] = reply
	ch, ok := f.replySeen[requestID]
	if !ok {
		ch = make(chan struct{})
		f.replySeen[requestID] = ch
	}
	f.mu.Unlock()
	close(ch)
}

func (f *Fake) defaultScript(fake *Fake, sess *provider.Session, prompt string) {
	_ = fake.Emit(provider.Frame{
		SessionID: sess.ID,
		Kind:      provider.FrameMessageText,
		MessageText: &provider.MessageText{
			AssistantMessageID: "am-1", TextID: "t-1", Text: "stub-harness-ok",
		},
	})
	fake.Complete(sess.ID, "stub-harness-ok")
}

// --- provider.Provider ---

func (f *Fake) CreateSession(_ context.Context, input provider.CreateSessionInput) (*provider.Session, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("ses-%d", f.nextID)
	sess := &provider.Session{
		ID: id, Directory: input.Directory, Agent: input.Agent, Model: input.Model,
	}
	f.sessions = append(f.sessions, *sess)
	f.waitDone[id] = make(chan struct{})
	return sess, nil
}

func (f *Fake) ListSessions(_ context.Context, directory string) ([]provider.SessionInfo, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	out := make([]provider.SessionInfo, 0, len(f.sessions))
	for _, sess := range f.sessions {
		if directory != "" && sess.Directory != directory {
			continue
		}
		out = append(out, provider.SessionInfo{
			ID: sess.ID, Directory: sess.Directory, Agent: sess.Agent, Model: sess.Model,
			CreatedAt: now, UpdatedAt: now, ArchivedAt: f.archived[sess.ID],
		})
	}
	return out, nil
}

func (f *Fake) GetSession(_ context.Context, sessionID string) (*provider.SessionInfo, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sess := range f.sessions {
		if sess.ID == sessionID {
			now := time.Now()
			return &provider.SessionInfo{
				ID: sess.ID, Directory: sess.Directory, Agent: sess.Agent, Model: sess.Model,
				CreatedAt: now, UpdatedAt: now, ArchivedAt: f.archived[sessionID],
			}, nil
		}
	}
	return nil, fmt.Errorf("fake: session %s not found", sessionID)
}

func (f *Fake) ArchiveSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	f.archived[sessionID] = &now
	return nil
}

func (f *Fake) Prompt(_ context.Context, sessionID string, input provider.PromptInput) (*provider.PromptAck, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	promptErr := f.promptErr
	script := f.onPrompt
	if script == nil {
		script = f.defaultScript
	}
	f.prompts[sessionID] = append(f.prompts[sessionID], input.Text)
	if input.Delivery == "" {
		input.Delivery = provider.DeliveryQueue
	}
	f.delivery[sessionID] = input.Delivery
	f.seq++
	ack := &provider.PromptAck{MessageID: fmt.Sprintf("msg-%s-%d", sessionID, len(f.prompts[sessionID])), AdmittedSeq: f.seq}
	var sess *provider.Session
	for i := range f.sessions {
		if f.sessions[i].ID == sessionID {
			sess = new(f.sessions[i])
			break
		}
	}
	f.mu.Unlock()
	if promptErr != nil {
		return nil, promptErr
	}
	if sess == nil {
		return nil, fmt.Errorf("fake: prompt to unknown session %s", sessionID)
	}
	go script(f, sess, input.Text)
	return ack, nil
}

func (f *Fake) SelectModel(context.Context, string, provider.ModelRef) error { return nil }

func (f *Fake) History(_ context.Context, sessionID string) ([]provider.Message, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.Message, len(f.history[sessionID]))
	copy(out, f.history[sessionID])
	return out, nil
}

func (f *Fake) EventStream(ctx context.Context, sessionID string, after int64) (provider.Stream, error) {
	// The run path consumes the stream bridge (BusStream); per-session event
	// replay is not simulated.
	return idleStream{ctx: ctx}, nil
}

func (f *Fake) BusStream(ctx context.Context) (provider.Stream, error) {
	return chanStream{ch: f.bus, ctx: ctx}, nil
}

// ActiveSessions implements provider.Provider from the pinned set.
func (f *Fake) ActiveSessions(_ context.Context) (map[string]bool, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]bool, len(f.active))
	for id, busy := range f.active {
		out[id] = busy
	}
	return out, nil
}

// Wait polls until the session's script completes (returning its wait
// error), the fake is crashed (returning the crash error), or ctx ends.
func (f *Fake) Wait(ctx context.Context, sessionID string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := f.failErr(); err != nil {
			return err
		}
		f.mu.Lock()
		ch, open := f.waitDone[sessionID]
		err := f.waitErr[sessionID]
		f.mu.Unlock()
		if !open {
			return err // completed earlier: settled result
		}
		select {
		case <-ch:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (f *Fake) Interrupt(_ context.Context, sessionID string) error {
	if err := f.failErr(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupt[sessionID]++
	return nil
}

func (f *Fake) ReplyPermission(_ context.Context, _, requestID string, reply provider.PermissionReply) error {
	f.signalReply(requestID, string(reply))
	return nil
}

func (f *Fake) ReplyQuestion(_ context.Context, _, requestID string, _ provider.QuestionAnswers) error {
	f.signalReply(requestID, "question")
	return nil
}

func (f *Fake) RejectQuestion(_ context.Context, _, requestID string) error {
	f.signalReply(requestID, "dismiss")
	return nil
}

func (f *Fake) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.ModelInfo, len(f.models))
	copy(out, f.models)
	return out, nil
}

func (f *Fake) ListAgents(context.Context, string) ([]provider.AgentInfo, error) {
	if err := f.failErr(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.AgentInfo, len(f.agents))
	copy(out, f.agents)
	return out, nil
}

func (f *Fake) Export(_ context.Context, sessionID string) ([]byte, error) {
	messages, err := f.History(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	var buf []byte
	for _, m := range messages {
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		buf = append(buf, raw...)
		buf = append(buf, '\n')
	}
	return buf, nil
}

// chanStream adapts the fake bus channel to provider.Stream.
type chanStream struct {
	ch  <-chan *provider.Frame
	ctx context.Context
}

func (s chanStream) Next(ctx context.Context) (*provider.Frame, error) {
	select {
	case frame, ok := <-s.ch:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s chanStream) Close() error { return nil }

// idleStream blocks without frames until ctx ends.
type idleStream struct{ ctx context.Context }

func (s idleStream) Next(ctx context.Context) (*provider.Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s idleStream) Close() error { return nil }
